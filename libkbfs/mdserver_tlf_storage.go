// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	keybase1 "github.com/keybase/client/go/protocol"
)

type mdServerTlfStorage struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well all the j, mdIDs, and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	j tlfJournal

	// branch ID -> [revision]MdID
	mdIDs map[BranchID][]MdID

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

	journal := &mdServerTlfStorage{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
		j:      j,
	}

	// Locking here is not strictly necessary, but do it anyway
	// for consistency.
	journal.lock.Lock()
	defer journal.lock.Unlock()
	mdIDs, err := journal.readJournalLocked()
	if err != nil {
		return nil, err
	}

	journal.mdIDs = mdIDs

	return journal, nil
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
	mdIDs := make(map[BranchID][]MdID)

	first, err := s.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return mdIDs, nil
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

		branchMdIDs := mdIDs[e.BranchID]
		initialRev, ok := initialRevs[e.BranchID]
		if ok {
			expectedRevision := MetadataRevision(int(initialRev) + len(branchMdIDs))
			if expectedRevision != e.Revision {
				return nil, fmt.Errorf(
					"Revision mismatch for branch %s: expected %s, got %s",
					e.BranchID, expectedRevision, e.Revision)
			}
		} else {
			initialRevs[e.BranchID] = e.Revision
		}

		mdIDs[e.BranchID] = append(branchMdIDs, e.MetadataID)
	}
	return mdIDs, nil
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

func (s *mdServerTlfStorage) getHeadForTLFLocked(bid BranchID) (
	rmds *RootMetadataSigned, err error) {
	branchMdIDs := s.mdIDs[bid]
	if len(branchMdIDs) == 0 {
		return nil, nil
	}
	return s.getMDLocked(branchMdIDs[len(branchMdIDs)-1])
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

	branchMdIDs := s.mdIDs[bid]

	startI := int(start - MetadataRevisionInitial)
	endI := int(stop - MetadataRevisionInitial + 1)
	if endI > len(branchMdIDs) {
		endI = len(branchMdIDs)
	}

	var rmdses []*RootMetadataSigned
	for i := startI; i < endI; i++ {
		rmds, err := s.getMDLocked(branchMdIDs[i])
		if err != nil {
			return nil, MDServerError{err}
		}
		rmdses = append(rmdses, rmds)
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

	s.mdIDs[bid] = append(s.mdIDs[bid], id)

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

	// Double-check the on-disk journal with the in-memory one.
	mdIDs, err := s.readJournalLocked()
	if err != nil {
		panic(err)
	}

	if !reflect.DeepEqual(mdIDs, s.mdIDs) {
		panic(fmt.Sprintf("mdIDs = %v != s.mdIDs = %v", mdIDs, s.mdIDs))
	}
}
