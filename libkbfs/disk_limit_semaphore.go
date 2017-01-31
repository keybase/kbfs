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
	journalSize       uint64
	availableSpace    uint64
	maxAvailableSpace uint64
}

func newDiskLimitSemaphore(
	initialAvailableSpace, maxAvailableSpace uint64) (
	s *diskLimitSemaphore, initialDiskLimit uint64) {
	semaphore := kbfssync.NewSemaphore()
	journalDiskLimit := initialAvailableSpace / 4
	if journalDiskLimit > maxAvailableSpace {
		journalDiskLimit = maxAvailableSpace
	}
	semaphore.Release(int64(journalDiskLimit))
	return &diskLimitSemaphore{
		semaphore, 0, initialAvailableSpace, maxAvailableSpace,
	}, journalDiskLimit
}

func (s diskLimitSemaphore) Acquire(
	ctx context.Context, n int64) (int64, error) {
	return s.s.Acquire(ctx, n)
}

func (s diskLimitSemaphore) Release(n int64) int64 {
	return s.s.Release(n)
}

func (s *diskLimitSemaphore) OnJournalEnable(journalSize int64) int64 {
	return s.s.Release(journalSize)
}

func (s *diskLimitSemaphore) OnJournalDisable(journalSize int64) int64 {
	return s.s.ForceAcquire(journalSize)
}
