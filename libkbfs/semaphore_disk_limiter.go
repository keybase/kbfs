// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/kbfs/kbfssync"
	"golang.org/x/net/context"
)

// semaphoreDiskLimiter is an implementation of diskLimiter that uses
// a semaphore.
type semaphoreDiskLimiter struct {
	s                 *kbfssync.Semaphore
	totalJournalBytes uint64
	availableBytes    uint64
	maxByteLimit      uint64
}

var _ diskLimiter = (*semaphoreDiskLimiter)(nil)

func newSemaphoreDiskLimiter(
	initialAvailableBytes, maxByteLimit, availableByteDivisor uint64) (
	s *semaphoreDiskLimiter, initialByteLimit uint64) {
	semaphore := kbfssync.NewSemaphore()
	byteLimit := initialAvailableBytes / availableByteDivisor
	if byteLimit > maxByteLimit {
		byteLimit = maxByteLimit
	}
	semaphore.Release(int64(byteLimit))
	return &semaphoreDiskLimiter{
		semaphore, 0, initialAvailableBytes, maxByteLimit,
	}, byteLimit
}

func (s semaphoreDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	return s.s.Acquire(ctx, blockBytes)
}

func (s semaphoreDiskLimiter) onBlockPutFail(blockBytes int64) int64 {
	return s.s.Release(blockBytes)
}

func (s semaphoreDiskLimiter) onBlockDelete(blockBytes int64) int64 {
	return s.s.Release(blockBytes)
}

func (s *semaphoreDiskLimiter) onJournalEnable(journalBytes int64) int64 {
	return s.s.ForceAcquire(journalBytes)
}

func (s *semaphoreDiskLimiter) onJournalDisable(journalBytes int64) int64 {
	return s.s.Release(journalBytes)
}
