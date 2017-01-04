// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"context"
	"sync"
)

type waiter struct {
	n         int64
	onRelease chan<- struct{}
}

type Semaphore struct {
	lock    sync.Mutex
	n       int64
	waiters []waiter
}

func NewSemaphore() *Semaphore {
	return &Semaphore{}
}

func (s *Semaphore) Release(n int64) {
	if n <= 0 {
		panic("n must be positive")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	// TODO: check for overflow.
	s.n += n
	for _, waiter := range s.waiters {
		close(waiter.onRelease)
	}
	s.waiters = nil
}

func (s *Semaphore) Acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		panic("n must be positive")
	}

	for {
		onRelease := func() <-chan struct{} {
			s.lock.Lock()
			defer s.lock.Unlock()
			if n <= s.n {
				s.n -= n
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
			return ctx.Err()
		}
	}
}
