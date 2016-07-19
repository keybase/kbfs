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
	"sync"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol"
)

// mdServerTlfJournal stores a single ordered list of metadata IDs for
// a single TLF, along with the associated metadata objects, in flat
// files on disk.
//
// The directory layout looks like:
//
// dir/md_journal/EARLIEST
// dir/md_journal/LATEST
// dir/md_journal/0...001
// dir/md_journal/0...002
// dir/md_journal/0...fff
// dir/mds/0100/0...01
// ...
// dir/mds/01ff/f...ff
//
// There's a single journal subdirectory; the journal ordinals are
// just MetadataRevisions, and the journal entries are just MdIDs.
//
// The Metadata objects are stored separately in dir/mds. Each block
// has its own subdirectory with its ID as a name. The MD
// subdirectories are splayed over (# of possible hash types) * 256
// subdirectories -- one byte for the hash type (currently only one)
// plus the first byte of the hash data -- using the first four
// characters of the name to keep the number of directories in dir
// itself to a manageable number, similar to git.
type mdServerTlfJournal struct {
	config Config
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well as branchJournals and its contents.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock                 sync.RWMutex
	isShutdown           bool
	j                    mdServerBranchJournal
	justBranchedBranchID BranchID
	justBranchedMdID     MdID
}

func makeMDServerTlfJournal(config Config, dir string) *mdServerTlfJournal {
	journal := &mdServerTlfJournal{
		config: config,
		codec:  config.Codec(),
		crypto: config.Crypto(),
		dir:    dir,
		j:      makeMDServerBranchJournal(config.Codec(), dir),
	}
	return journal
}

// The functions below are for building various paths.

func (s *mdServerTlfJournal) journalPath() string {
	return filepath.Join(s.dir, "md_journal")
}

func (s *mdServerTlfJournal) mdsPath() string {
	return filepath.Join(s.dir, "mds")
}

func (s *mdServerTlfJournal) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(s.mdsPath(), idStr[:4], idStr[4:])
}

// getDataLocked verifies the MD data (but not the signature) for the
// given ID and returns it.
//
// TODO: Verify signature?
func (s *mdServerTlfJournal) getMDReadLocked(id MdID) (
	*BareRootMetadata, error) {
	// Read file.

	path := s.mdPath(id)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rmd BareRootMetadata
	err = s.codec.Decode(data, &rmd)
	if err != nil {
		return nil, err
	}

	// Check integrity.

	mdID, err := s.crypto.MakeMdID(&rmd)
	if err != nil {
		return nil, err
	}

	if id != mdID {
		return nil, fmt.Errorf(
			"Metadata ID mismatch: expected %s, got %s", id, mdID)
	}

	return &rmd, nil
}

func (s *mdServerTlfJournal) putMDLocked(rmd *BareRootMetadata) error {
	id, err := s.crypto.MakeMdID(rmd)
	if err != nil {
		return err
	}

	fmt.Printf("post-putMDLocked ID = %s\n", id)

	_, err = s.getMDReadLocked(id)
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

	buf, err := s.codec.Encode(rmd)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, buf, 0600)
}

func (s *mdServerTlfJournal) getHeadReadLocked() (
	rmd *BareRootMetadata, err error) {
	headID, err := s.j.getHead()
	if err != nil {
		return nil, err
	}
	if headID == (MdID{}) {
		return nil, nil
	}
	return s.getMDReadLocked(headID)
}

func (s *mdServerTlfJournal) checkGetParamsReadLocked(
	currentUID keybase1.UID) error {
	head, err := s.getHeadReadLocked()
	if err != nil {
		return MDServerError{err}
	}

	if head != nil {
		ok, err := isReader(currentUID, head)
		if err != nil {
			return MDServerError{err}
		}
		if !ok {
			return MDServerErrorUnauthorized{}
		}
	}

	return nil
}

func (s *mdServerTlfJournal) getRangeReadLocked(
	currentUID keybase1.UID, start, stop MetadataRevision) (
	[]*BareRootMetadata, error) {
	err := s.checkGetParamsReadLocked(currentUID)
	if err != nil {
		return nil, err
	}

	realStart, mdIDs, err := s.j.getRange(start, stop)
	if err != nil {
		return nil, err
	}
	var rmds []*BareRootMetadata
	for i, mdID := range mdIDs {
		expectedRevision := realStart + MetadataRevision(i)
		rmd, err := s.getMDReadLocked(mdID)
		if err != nil {
			return nil, MDServerError{err}
		}
		if expectedRevision != rmd.Revision {
			panic(fmt.Errorf("expected revision %v, got %v",
				expectedRevision, rmd.Revision))
		}
		rmds = append(rmds, rmd)
	}

	return rmds, nil
}

func (s *mdServerTlfJournal) isShutdownReadLocked() bool {
	return s.isShutdown
}

// All functions below are public functions.

var errMDServerTlfJournalShutdown = errors.New("mdServerTlfJournal is shutdown")

func (s *mdServerTlfJournal) journalLength() (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdownReadLocked() {
		return 0, errMDServerTlfJournalShutdown
	}

	return s.j.journalLength()
}

func (s *mdServerTlfJournal) get(
	currentUID keybase1.UID) (*BareRootMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdownReadLocked() {
		return nil, errMDServerTlfJournalShutdown
	}

	err := s.checkGetParamsReadLocked(currentUID)
	if err != nil {
		return nil, err
	}

	rmd, err := s.getHeadReadLocked()
	if err != nil {
		return nil, MDServerError{err}
	}
	return rmd, nil
}

func (s *mdServerTlfJournal) getRange(
	currentUID keybase1.UID, start, stop MetadataRevision) (
	[]*BareRootMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdownReadLocked() {
		return nil, errMDServerTlfJournalShutdown
	}

	return s.getRangeReadLocked(currentUID, start, stop)
}

func (s *mdServerTlfJournal) put(
	ctx context.Context, currentUID keybase1.UID, rmd *RootMetadata) (err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdownReadLocked() {
		return errMDServerTlfJournalShutdown
	}

	mStatus := rmd.MergedStatus()
	bid := rmd.BID

	if (mStatus == Merged) != (bid == NullBranchID) {
		return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Check permissions

	head, err := s.getHeadReadLocked()
	if err != nil {
		return MDServerError{err}
	}

	// TODO: Figure out nil case.
	if head != nil {
		ok, err := isWriterOrValidRekey(s.codec, currentUID, head, &rmd.BareRootMetadata)
		if err != nil {
			return MDServerError{err}
		}
		if !ok {
			return MDServerErrorUnauthorized{}
		}
	}

	if mStatus == Merged {
		if s.justBranchedBranchID != NullBranchID {
			rmd.WFlags |= MetadataFlagUnmerged
			// head may be nil if the journal is fully
			// flushed.
			rmd.BID = s.justBranchedBranchID
			rmd.PrevRoot = s.justBranchedMdID
			s.justBranchedBranchID = NullBranchID
			s.justBranchedMdID = MdID{}
		} else if head != nil && head.MergedStatus() != Merged {
			// Conflict resolution shouldn't happen.
			return MDServerError{
				Err: errors.New("Shouldn't be doing conflict res"),
			}
		}
	} else {
		if head == nil {
			return MDServerError{
				Err: errors.New("Unexpectedly unmerged while empty"),
			}
		}
	}

	// Consistency checks
	if head != nil {
		headID, err := s.crypto.MakeMdID(head)
		if err != nil {
			return MDServerError{err}
		}
		err = head.CheckValidSuccessorForServer(
			headID, &rmd.BareRootMetadata)
		if err != nil {
			return err
		}
	}

	var brmd BareRootMetadata
	err = encryptMDPrivateData(
		ctx, s.config, currentUID, rmd.ReadOnly(), &brmd)
	if err != nil {
		return MDServerError{err}
	}

	err = s.putMDLocked(&brmd)
	if err != nil {
		return MDServerError{err}
	}

	id, err := s.crypto.MakeMdID(&rmd.BareRootMetadata)
	if err != nil {
		return MDServerError{err}
	}

	err = s.j.append(rmd.Revision, id)
	if err != nil {
		return MDServerError{err}
	}

	return nil
}

func (s *mdServerTlfJournal) convertToBranch(log logger.Logger) (
	BranchID, MdID, error) {
	earliestRevision, err := s.j.readEarliestRevision()
	if err != nil {
		return NullBranchID, MdID{}, err
	}

	latestRevision, err := s.j.readLatestRevision()
	if err != nil {
		return NullBranchID, MdID{}, err
	}

	log.Debug("rewriting MDs %s to %s", earliestRevision, latestRevision)

	_, allMdIDs, err := s.j.getRange(earliestRevision, latestRevision)
	if err != nil {
		return NullBranchID, MdID{}, err
	}

	bid, err := s.crypto.MakeRandomBranchID()
	if err != nil {
		return NullBranchID, MdID{}, err
	}

	log.Debug("New branch ID=%s", bid)

	// TODO: Do the below atomically.

	var prevID MdID

	for i, id := range allMdIDs {
		brmd, err := s.getMDReadLocked(id)
		if err != nil {
			return NullBranchID, MdID{}, err
		}
		brmd.WFlags |= MetadataFlagUnmerged
		brmd.BID = bid

		log.Debug("Old prev root of rev=%s is %s", brmd.Revision, brmd.PrevRoot)

		if i > 0 {
			log.Debug("Changing prev root of rev=%s to %s", brmd.Revision, prevID)
			brmd.PrevRoot = prevID
		}

		err = s.putMDLocked(brmd)
		if err != nil {
			return NullBranchID, MdID{}, err
		}

		o, err := revisionToOrdinal(brmd.Revision)
		if err != nil {
			return NullBranchID, MdID{}, err
		}

		newID, err := s.crypto.MakeMdID(brmd)
		if err != nil {
			return NullBranchID, MdID{}, err
		}

		err = s.j.j.writeJournalEntry(o, newID)
		if err != nil {
			return NullBranchID, MdID{}, err
		}

		prevID = newID

		log.Debug("Changing ID for rev=%s from %s to %s",
			brmd.Revision, id, newID)
	}

	return bid, prevID, err
}

var errMDServerTlfJournalEmpty = errors.New("mdServerTlfJournal is empty")

var errMDServerTlfJournalNeedBranchConversion = errors.New("mdServerTlfJournal needs branch conversion")

func (s *mdServerTlfJournal) putEarliest(
	ctx context.Context, mdserver MDServer, log logger.Logger) error {
	earliestID, err := s.j.getEarliest()
	if err != nil {
		return err
	}
	if earliestID == (MdID{}) {
		return errMDServerTlfJournalEmpty
	}

	rmd, err := s.getMDReadLocked(earliestID)
	if err != nil {
		return err
	}

	log.Debug("Flushing MD put id=%s, rev=%s", earliestID, rmd.Revision)

	var rmds RootMetadataSigned
	rmds.MD = *rmd
	err = signMD(ctx, s.config, &rmds)
	if err != nil {
		return err
	}
	err = mdserver.Put(ctx, &rmds)
	if isRevisionConflict(err) && rmd.MergedStatus() == Merged {
		return errMDServerTlfJournalNeedBranchConversion
	} else if err != nil {
		return err
	}

	return nil
}

func (s *mdServerTlfJournal) flushOne(
	ctx context.Context, mdserver MDServer, log logger.Logger) (bool, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdownReadLocked() {
		return false, errMDServerTlfJournalShutdown
	}

	err := s.putEarliest(ctx, mdserver, log)
	switch err {
	case errMDServerTlfJournalEmpty:
		return false, nil

	case errMDServerTlfJournalNeedBranchConversion:
		log.Debug("Conflict detected %v", err)

		bid, latestID, err := s.convertToBranch(log)
		if err != nil {
			return false, err
		}

		s.justBranchedBranchID = bid
		s.justBranchedMdID = latestID

		err = s.putEarliest(ctx, mdserver, log)
		if err != nil {
			return false, err
		}

	case nil:
		break

	default:
		return false, err
	}

	err = s.j.removeEarliest()
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *mdServerTlfJournal) shutdown() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.isShutdown = true
}
