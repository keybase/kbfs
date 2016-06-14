// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"
)

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
