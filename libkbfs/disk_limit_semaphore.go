// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/kbfs/kbfssync"
	"golang.org/x/net/context"
)

type diskLimitSemaphore struct {
	s                 *kbfssync.Semaphore
	totalJournalBytes uint64
	availableBytes    uint64
	maxAvailableBytes uint64
}

var _ diskLimiter = (*diskLimitSemaphore)(nil)

func newDiskLimitSemaphore(initialAvailableBytes, maxAvailableBytes uint64) (
	s *diskLimitSemaphore, initialDiskLimit uint64) {
	semaphore := kbfssync.NewSemaphore()
	journalDiskLimit := initialAvailableBytes / 4
	if journalDiskLimit > maxAvailableBytes {
		journalDiskLimit = maxAvailableBytes
	}
	semaphore.Release(int64(journalDiskLimit))
	return &diskLimitSemaphore{
		semaphore, 0, initialAvailableBytes, maxAvailableBytes,
	}, journalDiskLimit
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
