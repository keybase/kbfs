// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/kbfs/kbfssync"
	"golang.org/x/net/context"
)

// diskLimitSemaphore is an implementation of diskLimiter that wraps a
// semaphore.
type diskLimitSemaphore struct {
	s                 *kbfssync.Semaphore
	totalJournalBytes uint64
	availableBytes    uint64
	maxByteLimit      uint64
}

var _ diskLimiter = (*diskLimitSemaphore)(nil)

func newDiskLimitSemaphore(
	initialAvailableBytes, maxByteLimit, availableByteDivisor uint64) (
	s *diskLimitSemaphore, initialByteLimit uint64) {
	semaphore := kbfssync.NewSemaphore()
	byteLimit := initialAvailableBytes / availableByteDivisor
	if byteLimit > maxByteLimit {
		byteLimit = maxByteLimit
	}
	semaphore.Release(int64(byteLimit))
	return &diskLimitSemaphore{
		semaphore, 0, initialAvailableBytes, maxByteLimit,
	}, byteLimit
}

func (s diskLimitSemaphore) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	return s.s.Acquire(ctx, blockBytes)
}

func (s diskLimitSemaphore) onBlockPutFail(blockBytes int64) int64 {
	return s.s.Release(blockBytes)
}

func (s diskLimitSemaphore) onBlockDelete(blockBytes int64) int64 {
	return s.s.Release(blockBytes)
}

func (s *diskLimitSemaphore) onJournalEnable(journalBytes int64) int64 {
	return s.s.ForceAcquire(journalBytes)
}

func (s *diskLimitSemaphore) onJournalDisable(journalBytes int64) int64 {
	return s.s.Release(journalBytes)
}
