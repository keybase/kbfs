// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type mdServerTlfStorage struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well all the DBs, j, and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	j tlfJournal

	// TODO: Replace idDb below with a journal.
	idDb *leveldb.DB // [branchId]+[revision] -> MdID

	isShutdown bool
}

// An mdServerJournalEntry is just the parameters for a Put
// operation. Fields are exported only for serialization.
type mdServerJournalEntry struct {
	BranchID   BranchID
	Revision   MetadataRevision
	MetadataID MdID
}

func makeMDServerTlfStorage(codec Codec, crypto cryptoPure, dir string) (
	*mdServerTlfStorage, error) {
	journalPath := filepath.Join(dir, "md_journal")
	j := makeTlfJournal(
		codec, journalPath, reflect.TypeOf(mdServerJournalEntry{}))

	idDb, err := leveldb.OpenFile(
		filepath.Join(dir, "md_id"), leveldbOptions)
	if err != nil {
		return nil, err
	}
	return &mdServerTlfStorage{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
		j:      j,
		idDb:   idDb,
	}, nil
}

// The functions below are for building various non-journal paths.

func (s *mdServerTlfStorage) mdsPath() string {
	return filepath.Join(s.dir, "mds")
}

func (s *mdServerTlfStorage) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(s.mdsPath(), idStr[:4], idStr[4:])
}

// The functions below are for reading and writing journal entries.

func (s *mdServerTlfStorage) readJournalEntryLocked(o journalOrdinal) (
	mdServerJournalEntry, error) {
	entry, err := s.j.readJournalEntry(o)
	if err != nil {
		return mdServerJournalEntry{}, err
	}

	return entry.(mdServerJournalEntry), nil
}

// readJournalLocked reads the journal and returns a map of all the
// MDs in the journal.
func (s *mdServerTlfStorage) readJournalLocked() (map[BranchID][]MdID, error) {
	mds := make(map[BranchID][]MdID)

	first, err := s.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return mds, nil
	} else if err != nil {
		return nil, err
	}
	last, err := s.j.readLatestOrdinal()
	if err != nil {
		return nil, err
	}

	initialRevs := make(map[BranchID]MetadataRevision)

	for i := first; i <= last; i++ {
		e, err := s.readJournalEntryLocked(i)
		if err != nil {
			return nil, err
		}

		branchMDs := mds[e.BranchID]
		initialRev, ok := initialRevs[e.BranchID]
		if ok {
			expectedRevision := MetadataRevision(int(initialRev) + len(branchMDs))
			if expectedRevision != e.Revision {
				return nil, fmt.Errorf(
					"Revision mismatch for branch %s: expected %s, got %s",
					e.BranchID, expectedRevision, e.Revision)
			}
		} else {
			initialRevs[e.BranchID] = e.Revision
		}

		mds[e.BranchID] = append(branchMDs, e.MetadataID)
	}
	return mds, nil
}

func (s *mdServerTlfStorage) writeJournalEntryLocked(
	o journalOrdinal, entry mdServerJournalEntry) error {
	return s.j.writeJournalEntry(o, entry)
}

func (s *mdServerTlfStorage) appendJournalEntryLocked(BranchID BranchID,
	Revision MetadataRevision, MetadataID MdID) error {
	return s.j.appendJournalEntry(mdServerJournalEntry{
		BranchID:   BranchID,
		Revision:   Revision,
		MetadataID: MetadataID,
	})
}

func (s *mdServerTlfStorage) journalLength() (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.j.journalLength()
}

// getDataLocked verifies the MD data (but not the signature) for the
// given ID and returns it.
//
// TODO: Verify signature?
func (s *mdServerTlfStorage) getMDLocked(id MdID) (*RootMetadataSigned, error) {
	// Read file.

	path := s.mdPath(id)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rmds RootMetadataSigned
	err = s.codec.Decode(data, &rmds)
	if err != nil {
		return nil, err
	}

	// Check integrity.

	mdID, err := rmds.MD.MetadataID(s.crypto)
	if err != nil {
		return nil, err
	}

	if id != mdID {
		return nil, fmt.Errorf(
			"Metadata ID mismatch: expected %s, got %s", id, mdID)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	rmds.untrustedServerTimestamp = fileInfo.ModTime()

	return &rmds, nil
}

func (s *mdServerTlfStorage) putMDLocked(rmds *RootMetadataSigned) error {
	id, err := rmds.MD.MetadataID(s.crypto)
	if err != nil {
		return err
	}

	_, err = s.getMDLocked(id)
	if os.IsNotExist(err) {
		// Continue on.
	} else if err != nil {
		return err
	} else {
		// Entry exists, so nothing else to do.
		return nil
	}

	path := s.mdPath(id)

	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return err
	}

	buf, err := s.codec.Encode(rmds)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, buf, 0600)
}

func (s *mdServerTlfStorage) getMDKey(revision MetadataRevision,
	bid BranchID) ([]byte, error) {
	// short-cut
	if revision == MetadataRevisionUninitialized && bid == NullBranchID {
		return nil, nil
	}
	buf := &bytes.Buffer{}

	// this order is significant for range fetches.  we want
	// increments in revision number to only affect the least
	// significant bits of the key.
	if bid != NullBranchID {
		// add branch ID
		_, err := buf.Write(bid.Bytes())
		if err != nil {
			return nil, err
		}
	}

	if revision >= MetadataRevisionInitial {
		// add revision
		err := binary.Write(buf, binary.BigEndian, revision.Number())
		if err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (s *mdServerTlfStorage) getHeadForTLFLocked(bid BranchID) (
	rmds *RootMetadataSigned, err error) {
	key, err := s.getMDKey(0, bid)
	if err != nil {
		return nil, err
	}
	buf, err := s.idDb.Get(key[:], nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	id, err := MdIDFromBytes(buf)
	if err != nil {
		return nil, err
	}

	return s.getMDLocked(id)
}

func (s *mdServerTlfStorage) checkGetParamsLocked(
	currentUID keybase1.UID, deviceKID keybase1.KID, bid BranchID) error {
	// Check permissions

	mergedMasterHead, err := s.getHeadForTLFLocked(NullBranchID)
	if err != nil {
		return MDServerError{err}
	}

	ok, err := isReader(currentUID, mergedMasterHead)
	if err != nil {
		return MDServerError{err}
	}
	if !ok {
		return MDServerErrorUnauthorized{}
	}

	return nil
}

func (s *mdServerTlfStorage) getRangeLocked(
	currentUID keybase1.UID, deviceKID keybase1.KID,
	bid BranchID, start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	err := s.checkGetParamsLocked(currentUID, deviceKID, bid)
	if err != nil {
		return nil, err
	}

	var rmdses []*RootMetadataSigned
	startKey, err := s.getMDKey(start, bid)
	if err != nil {
		return rmdses, MDServerError{err}
	}
	stopKey, err := s.getMDKey(stop+1, bid)
	if err != nil {
		return rmdses, MDServerError{err}
	}

	iter := s.idDb.NewIterator(&util.Range{Start: startKey, Limit: stopKey}, nil)
	defer iter.Release()
	for iter.Next() {
		id, err := MdIDFromBytes(iter.Value())
		if err != nil {
			return nil, MDServerError{err}
		}

		rmds, err := s.getMDLocked(id)
		if err != nil {
			return rmdses, MDServerError{err}
		}
		rmdses = append(rmdses, rmds)
	}
	if err := iter.Error(); err != nil {
		return rmdses, MDServerError{err}
	}

	return rmdses, nil
}

// All functions below are public functions.

var errMDServerTlfStorageShutdown = errors.New("mdServerTlfStorage is shutdown")

func (s *mdServerTlfStorage) getForTLF(
	currentUID keybase1.UID, deviceKID keybase1.KID,
	bid BranchID) (*RootMetadataSigned, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, errMDServerTlfStorageShutdown
	}

	err := s.checkGetParamsLocked(currentUID, deviceKID, bid)
	if err != nil {
		return nil, err
	}

	rmds, err := s.getHeadForTLFLocked(bid)
	if err != nil {
		return nil, MDServerError{err}
	}
	return rmds, nil
}

func (s *mdServerTlfStorage) getRange(
	currentUID keybase1.UID, deviceKID keybase1.KID,
	bid BranchID, start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, errMDServerTlfStorageShutdown
	}

	return s.getRangeLocked(currentUID, deviceKID, bid, start, stop)
}

func (s *mdServerTlfStorage) put(
	currentUID keybase1.UID, deviceKID keybase1.KID,
	rmds *RootMetadataSigned) (recordBranchID bool, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return false, errMDServerTlfStorageShutdown
	}

	mStatus := rmds.MD.MergedStatus()
	bid := rmds.MD.BID

	if (mStatus == Merged) != (bid == NullBranchID) {
		return false, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Check permissions

	mergedMasterHead, err := s.getHeadForTLFLocked(NullBranchID)
	if err != nil {
		return false, MDServerError{err}
	}

	ok, err := isWriterOrValidRekey(
		s.codec, currentUID, mergedMasterHead, rmds)
	if err != nil {
		return false, MDServerError{err}
	}
	if !ok {
		return false, MDServerErrorUnauthorized{}
	}

	head, err := s.getHeadForTLFLocked(bid)
	if err != nil {
		return false, MDServerError{err}
	}

	if mStatus == Unmerged && head == nil {
		// currHead for unmerged history might be on the main branch
		prevRev := rmds.MD.Revision - 1
		rmdses, err := s.getRangeLocked(
			currentUID, deviceKID, NullBranchID, prevRev, prevRev)
		if err != nil {
			return false, MDServerError{err}
		}
		if len(rmdses) != 1 {
			return false, MDServerError{
				Err: fmt.Errorf("Expected 1 MD block got %d", len(rmdses)),
			}
		}
		head = rmdses[0]
		recordBranchID = true
	}

	// Consistency checks
	if head != nil {
		err := head.MD.CheckValidSuccessorForServer(s.crypto, &rmds.MD)
		if err != nil {
			return false, err
		}
	}

	err = s.putMDLocked(rmds)
	if err != nil {
		return false, MDServerError{err}
	}

	id, err := rmds.MD.MetadataID(s.crypto)
	if err != nil {
		return false, MDServerError{err}
	}

	// Wrap writes in a batch
	batch := new(leveldb.Batch)

	// Add an entry with the revision key.
	revKey, err := s.getMDKey(rmds.MD.Revision, bid)
	if err != nil {
		return false, MDServerError{err}
	}
	batch.Put(revKey, id.Bytes())

	// Add an entry with the head key.
	headKey, err := s.getMDKey(MetadataRevisionUninitialized, bid)
	if err != nil {
		return false, MDServerError{err}
	}
	batch.Put(headKey, id.Bytes())

	// Write the batch.
	err = s.idDb.Write(batch, nil)
	if err != nil {
		return false, MDServerError{err}
	}

	err = s.appendJournalEntryLocked(bid, rmds.MD.Revision, id)
	if err != nil {
		return false, MDServerError{err}
	}

	return recordBranchID, nil
}

func (s *mdServerTlfStorage) shutdown() {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return
	}
	s.isShutdown = true

	_, err := s.readJournalLocked()
	if err != nil {
		panic(err)
	}

	if s.idDb != nil {
		s.idDb.Close()
		s.idDb = nil
	}
}
