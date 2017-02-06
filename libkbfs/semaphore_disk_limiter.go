// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/kbfs/kbfssync"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// semaphoreDiskLimiter is an implementation of diskLimiter that uses
// a semaphore.
type semaphoreDiskLimiter struct {
	backpressureMinThreshold int64
	backpressureMaxThreshold int64
	s                        *kbfssync.Semaphore
}

var _ diskLimiter = semaphoreDiskLimiter{}

func newSemaphoreDiskLimiter(
	backpressureMinThreshold, backpressureMaxThreshold, byteLimit int64) semaphoreDiskLimiter {
	if backpressureMinThreshold < 0 {
		panic("backpressureMinThreshold < 0")
	}
	if backpressureMaxThreshold < backpressureMinThreshold {
		panic("backpressureMaxThreshold < backpressureMinThreshold")
	}
	if byteLimit < backpressureMaxThreshold {
		panic("byteLimit < backpressureMaxThreshold")
	}
	s := kbfssync.NewSemaphore()
	s.Release(byteLimit)
	return semaphoreDiskLimiter{backpressureMinThreshold, backpressureMaxThreshold, s}
}

func (s semaphoreDiskLimiter) onJournalEnable(journalBytes int64) int64 {
	if journalBytes == 0 {
		// TODO: This is a bit weird. Add a function to get
		// the current semaphore count, or let ForceAcquire
		// take 0.
		return 0
	}
	return s.s.ForceAcquire(journalBytes)
}

func (s semaphoreDiskLimiter) onJournalDisable(journalBytes int64) {
	if journalBytes > 0 {
		s.s.Release(journalBytes)
	}
}

func (s semaphoreDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		//
		// TODO: Return the current semaphore count here, too?
		return 0, errors.New("beforeBlockPut called with 0 blockBytes")
	}
	return s.s.Acquire(ctx, blockBytes)
}

func (s semaphoreDiskLimiter) onBlockPutFail(blockBytes int64) {
	s.s.Release(blockBytes)
}

func (s semaphoreDiskLimiter) onBlockDelete(blockBytes int64) {
	if blockBytes > 0 {
		s.s.Release(blockBytes)
	}
}
