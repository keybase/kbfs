// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"

	keybase1 "github.com/keybase/client/go/protocol"
)

// Helper to aid in enforcement that only specified public keys can
// access TLF metadata. mergedMasterHead can be nil, in which case
// true is returned.
func isReader(currentUID keybase1.UID,
	mergedMasterHead *RootMetadataSigned) (bool, error) {
	if mergedMasterHead == nil {
		// TODO: the real mdserver will actually reverse
		// lookup the folder handle and check that the UID is
		// listed.
		return true, nil
	}
	h, err := mergedMasterHead.MD.MakeBareTlfHandle()
	if err != nil {
		return false, err
	}
	return h.IsReader(currentUID), nil
}

// Helper to aid in enforcement that only specified public keys can
// access TLF metadata. mergedMasterHead can be nil, in which case
// true is returned.
func isWriterOrValidRekey(codec IFCERFTCodec, currentUID keybase1.UID,
	mergedMasterHead *RootMetadataSigned,
	newMd *RootMetadataSigned) (bool, error) {
	if mergedMasterHead == nil {
		// TODO: the real mdserver will actually reverse
		// lookup the folder handle and check that the UID is
		// listed.
		return true, nil
	}
	h, err := mergedMasterHead.MD.MakeBareTlfHandle()
	if err != nil {
		return false, err
	}
	if h.IsWriter(currentUID) {
		return true, nil
	}

	if h.IsReader(currentUID) {
		// if this is a reader, are they acting within their
		// restrictions?
		return newMd.MD.IsValidRekeyRequest(
			codec, &mergedMasterHead.MD, currentUID)
	}

	return false, nil
}

// mdServerLocalTruncateLockManager manages the truncate locks for a
// set of TLFs. Note that it is not goroutine-safe.
type mdServerLocalTruncateLockManager struct {
	// TLF ID -> device KID.
	locksDb map[IFCERFTTlfID]keybase1.KID
}

func newMDServerLocalTruncatedLockManager() mdServerLocalTruncateLockManager {
	return mdServerLocalTruncateLockManager{
		locksDb: make(map[IFCERFTTlfID]keybase1.KID),
	}
}

func (m mdServerLocalTruncateLockManager) truncateLock(
	deviceKID keybase1.KID, id IFCERFTTlfID) (bool, error) {
	lockKID, ok := m.locksDb[id]
	if !ok {
		m.locksDb[id] = deviceKID
		return true, nil
	}

	if lockKID == deviceKID {
		// idempotent
		return true, nil
	}

	// Locked by someone else.
	return false, MDServerErrorLocked{}
}

func (m mdServerLocalTruncateLockManager) truncateUnlock(
	deviceKID keybase1.KID, id IFCERFTTlfID) (bool, error) {
	lockKID, ok := m.locksDb[id]
	if !ok {
		// Already unlocked.
		return true, nil
	}

	if lockKID == deviceKID {
		delete(m.locksDb, id)
		return true, nil
	}

	// Locked by someone else.
	return false, MDServerErrorLocked{}
}

// mdServerLocalUpdateManager manages the observers for a set of TLFs
// referenced by multiple mdServerLocal instances sharing the same
// data. It is goroutine-safe.
type mdServerLocalUpdateManager struct {
	// Protects observers and sessionHeads.
	lock         sync.Mutex
	observers    map[IFCERFTTlfID]map[mdServerLocal]chan<- error
	sessionHeads map[IFCERFTTlfID]mdServerLocal
}

func newMDServerLocalUpdateManager() *mdServerLocalUpdateManager {
	return &mdServerLocalUpdateManager{
		observers:    make(map[IFCERFTTlfID]map[mdServerLocal]chan<- error),
		sessionHeads: make(map[IFCERFTTlfID]mdServerLocal),
	}
}

func (m *mdServerLocalUpdateManager) setHead(id IFCERFTTlfID, server mdServerLocal) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.sessionHeads[id] = server

	// now fire all the observers that aren't from this session
	for k, v := range m.observers[id] {
		if k != server {
			v <- nil
			close(v)
			delete(m.observers[id], k)
		}
	}
	if len(m.observers[id]) == 0 {
		delete(m.observers, id)
	}
}

func (m *mdServerLocalUpdateManager) registerForUpdate(
	id IFCERFTTlfID, currHead, currMergedHeadRev MetadataRevision,
	server mdServerLocal) <-chan error {
	m.lock.Lock()
	defer m.lock.Unlock()

	c := make(chan error, 1)
	if currMergedHeadRev > currHead && server != m.sessionHeads[id] {
		c <- nil
		close(c)
		return c
	}

	if _, ok := m.observers[id]; !ok {
		m.observers[id] = make(map[mdServerLocal]chan<- error)
	}

	// Otherwise, this is a legit observer.  This assumes that each
	// client will be using a unique instance of MDServerLocal.
	if _, ok := m.observers[id][server]; ok {
		// If the local node registers something twice, it indicates a
		// fatal bug.  Note that in the real MDServer implementation,
		// we should allow this, in order to make the RPC properly
		// idempotent.
		panic(fmt.Errorf("Attempted double-registration for MDServerLocal %v",
			server))
	}
	m.observers[id][server] = c
	return c
}
