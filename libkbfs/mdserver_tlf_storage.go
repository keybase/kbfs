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

	keybase1 "github.com/keybase/client/go/protocol"
)

type mdServerTlfStorage struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well as branchJournals and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock           sync.RWMutex
	branchJournals map[BranchID]mdServerBranchJournal
	isShutdown     bool
}

func makeMDServerTlfStorage(
	codec Codec, crypto cryptoPure, dir string) *mdServerTlfStorage {
	journal := &mdServerTlfStorage{
		codec:          codec,
		crypto:         crypto,
		dir:            dir,
		branchJournals: make(map[BranchID]mdServerBranchJournal),
	}
	return journal
}

// The functions below are for building various non-journal paths.

func (s *mdServerTlfStorage) mdsPath() string {
	return filepath.Join(s.dir, "mds")
}

func (s *mdServerTlfStorage) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(s.mdsPath(), idStr[:4], idStr[4:])
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

func (s *mdServerTlfStorage) getBranchJournalLocked(
	bid BranchID) (mdServerBranchJournal, error) {
	j, ok := s.branchJournals[bid]
	if !ok {
		// TODO: Splay the branch directories?
		dir := filepath.Join(s.dir, bid.String())
		err := os.MkdirAll(dir, 0700)
		if err != nil {
			return mdServerBranchJournal{}, err
		}

		s.branchJournals[bid] = makeMDServerBranchJournal(s.codec, dir)
	}
	return j, nil
}

func (s *mdServerTlfStorage) getHeadForTLFLocked(bid BranchID) (
	rmds *RootMetadataSigned, err error) {
	j, err := s.getBranchJournalLocked(bid)
	if err != nil {
		return nil, err
	}
	headID, err := j.getHead()
	if err != nil {
		return nil, err
	}
	if headID == (MdID{}) {
		return nil, nil
	}
	return s.getMDLocked(headID)
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

	j, err := s.getBranchJournalLocked(bid)
	if err != nil {
		return nil, err
	}

	mdIDs, err := j.getRange(start, stop)
	if err != nil {
		return nil, err
	}
	var rmdses []*RootMetadataSigned
	for _, mdID := range mdIDs {
		rmds, err := s.getMDLocked(mdID)
		if err != nil {
			return nil, MDServerError{err}
		}
		rmdses = append(rmdses, rmds)
	}

	return rmdses, nil
}

var errMDServerTlfStorageShutdown = errors.New("mdServerTlfStorage is shutdown")

func (s *mdServerTlfStorage) journalLength(bid BranchID) (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return 0, errMDServerTlfStorageShutdown
	}

	j, err := s.getBranchJournalLocked(bid)
	if err != nil {
		return 0, err
	}

	return j.journalLength()
}

// All functions below are public functions.

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

	j, err := s.getBranchJournalLocked(bid)
	if err != nil {
		return false, err
	}

	err = j.put(rmds.MD.Revision, id)
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
	s.branchJournals = nil
}
