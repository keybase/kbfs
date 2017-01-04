// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"context"
	"sync"

	"github.com/pkg/errors"
)

// A waiter represents a goroutine blocked on resource acquisition.
type waiter struct {
	// n is the resource count this waiter wants to acquire.
	//
	// TODO: Actually use this to e.g. avoid waking up waiters
	// when the current resource count is too low.
	n int64
	// onRelease is a channel that is closed when some resources
	// are released.
	onRelease chan<- struct{}
}

// Semaphore implements a counting semaphore; it maintains a resource
// count, and exposes methods to try acquiring those resources --
// waiting if necessary -- and releasing those resources back.
type Semaphore struct {
	lock    sync.Mutex
	count   int64
	waiters []waiter
}

// NewSemaphore returns a new Semaphore with a resource count of
// 0. Use Adjust() to set the initial resource count.
func NewSemaphore() *Semaphore {
	return &Semaphore{}
}

// Adjust atomically adds n to the resource count without waking up
// any waiting acquirers. It is meant for setting the initial resource
// count of the semaphore, or adjusting it. It's okay if adding n
// causes the resource count goes negative, but it must not cause the
// resource count to overflow in either the positive or negative
// direction.
func (s *Semaphore) Adjust(n int64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	// TODO: Check for overflow.
	s.count += n
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
		onRelease := func() <-chan struct{} {
			s.lock.Lock()
			defer s.lock.Unlock()
			if n <= s.count {
				s.count -= n
				return nil
			}

			onRelease := make(chan struct{})
			waiter := waiter{
				n:         n,
				onRelease: onRelease,
			}
			s.waiters = append(s.waiters, waiter)
			return onRelease
		}()

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

// Release atomically adds n (which must be positive) to the resource
// count. It must not cause the resource count to overflow. If there
// are waiting acquirers, it wakes up at least one of them to make
// progress, assuming that no new acquirers arrive in the meantime.
func (s *Semaphore) Release(n int64) {
	if n <= 0 {
		panic("n must be positive")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	// TODO: check for overflow.
	s.count += n
	for _, waiter := range s.waiters {
		close(waiter.onRelease)
	}
	s.waiters = nil
}
