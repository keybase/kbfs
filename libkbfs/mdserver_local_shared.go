// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"

	"golang.org/x/net/context"
)

type getTlfHeader interface {
	getHeadForTLF(ctx context.Context, id TlfID,
		bid BranchID, mStatus MergeStatus) (*RootMetadataSigned, error)
}

func checkMDPerms(ctx context.Context, codec Codec, kbpki KBPKI,
	getTlfHeader getTlfHeader, id TlfID,
	checkWrite bool, newMd *RootMetadataSigned) (bool, error) {
	rmds, err := getTlfHeader.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if rmds == nil {
		// TODO: the real mdserver will actually reverse lookup the folder handle
		// and check that the UID is listed.
		return true, nil
	}
	_, user, err := kbpki.GetCurrentUserInfo(ctx)
	if err != nil {
		return false, err
	}
	h, err := rmds.MD.MakeBareTlfHandle()
	if err != nil {
		return false, err
	}
	isWriter := h.IsWriter(user)
	isReader := h.IsReader(user)
	if checkWrite {
		// if this is a reader, are they acting within their restrictions?
		if !isWriter && isReader && newMd != nil {
			return newMd.MD.IsValidRekeyRequest(
				codec, &rmds.MD, user)
		}
		return isWriter, nil
	}
	return isWriter || isReader, nil
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func isReader(ctx context.Context, codec Codec, kbpki KBPKI,
	getTlfHeader getTlfHeader, id TlfID) (bool, error) {
	return checkMDPerms(ctx, codec, kbpki, getTlfHeader, id, false, nil)
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func isWriterOrValidRekey(ctx context.Context, codec Codec, kbpki KBPKI,
	getTlfHeader getTlfHeader, id TlfID, newMd *RootMetadataSigned) (
	bool, error) {
	return checkMDPerms(ctx, codec, kbpki, getTlfHeader, id, true, newMd)
}

type mdServerLocalUpdateManager struct {
	// Protects observers and sessionHeads.
	lock sync.Mutex
	// Multiple local instances of MDServer could share a
	// reference to this map and sessionHead, and we use that to
	// ensure that all observers are fired correctly no matter
	// which local instance gets the Put() call.
	observers    map[TlfID]map[MDServer]chan<- error
	sessionHeads map[TlfID]MDServer
}

func newMDServerLocalUpdateManager() *mdServerLocalUpdateManager {
	return &mdServerLocalUpdateManager{
		observers:    make(map[TlfID]map[MDServer]chan<- error),
		sessionHeads: make(map[TlfID]MDServer),
	}
}

func (s *mdServerLocalUpdateManager) setHead(id TlfID, server MDServer) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.sessionHeads[id] = server

	// now fire all the observers that aren't from this session
	for k, v := range s.observers[id] {
		if k != server {
			v <- nil
			close(v)
			delete(s.observers[id], k)
		}
	}
	if len(s.observers[id]) == 0 {
		delete(s.observers, id)
	}
}

func (s *mdServerLocalUpdateManager) registerForUpdate(
	id TlfID, currHead, currMergedHeadRev MetadataRevision,
	server MDServer) <-chan error {
	s.lock.Lock()
	defer s.lock.Unlock()

	c := make(chan error, 1)
	if currMergedHeadRev > currHead && server != s.sessionHeads[id] {
		c <- nil
		close(c)
		return c
	}

	if _, ok := s.observers[id]; !ok {
		s.observers[id] = make(map[MDServer]chan<- error)
	}

	// Otherwise, this is a legit observer.  This assumes that each
	// client will be using a unique instance of MDServerLocal.
	if _, ok := s.observers[id][server]; ok {
		// If the local node registers something twice, it indicates a
		// fatal bug.  Note that in the real MDServer implementation,
		// we should allow this, in order to make the RPC properly
		// idempotent.
		panic(fmt.Errorf("Attempted double-registration for MDServerLocal %v",
			server))
	}
	s.observers[id][server] = c
	return c
}
