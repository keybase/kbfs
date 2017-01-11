// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// Semaphore implements a counting semaphore; it maintains a resource
// count, and exposes methods for acquiring those resources -- waiting
// if desired -- and releasing those resources back.
type Semaphore struct {
	lock      sync.RWMutex
	count     int64
	onRelease chan struct{}
}

// NewSemaphore returns a new Semaphore with a resource count of
// 0. Use Release() to set the initial resource count.
func NewSemaphore() *Semaphore {
	return &Semaphore{
		onRelease: make(chan struct{}),
	}
}

// Count returns the current resource count. It should be used only
// for logging or testing.
func (s *Semaphore) Count() int64 {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.count
}

// tryAcquire tries to acquire n resources. If successful, nil is
// returned. Otherwise, a channel which will be closed when new
// resources are available is returned.
func (s *Semaphore) tryAcquire(n int64) <-chan struct{} {
	s.lock.Lock()
	defer s.lock.Unlock()
	if n <= s.count {
		s.count -= n
		return nil
	}

	return s.onRelease
}

// Acquire blocks until it is possible to atomically subtract n (which
// must be positive) from the resource count without causing it to go
// negative, and then returns nil. If the given context is canceled
// first, it instead returns a wrapped ctx.Err() and does not change
// the resource count.
func (s *Semaphore) Acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		panic("n must be positive")
	}

	for {
		onRelease := s.tryAcquire(n)
		if onRelease == nil {
			return nil
		}

		select {
		case <-onRelease:
			// Go to the top of the loop.
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		}
	}
}

// ForceAcquire atomically adds n to the resource count without waking
// up any waiting acquirers. It is meant for correcting the initial
// resource count of the semaphore. It's okay if adding n causes the
// resource count goes negative, but it must not cause the resource
// count to overflow in the negative direction.
func (s *Semaphore) ForceAcquire(n int64) {
	if n <= 0 {
		panic("n must be positive")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	// Check for overflow.
	s.count -= n
}

// Release atomically adds n (which must be positive) to the resource
// count. It must not cause the resource count to overflow. If there
// are waiting acquirers, it wakes up at least one of them to make
// progress, assuming that no new acquirers arrive in the meantime.
func (s *Semaphore) Release(n int64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	// TODO: check for overflow.
	s.count += n
	close(s.onRelease)
	s.onRelease = make(chan struct{})
}
