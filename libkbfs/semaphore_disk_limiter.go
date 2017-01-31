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
	s                    *kbfssync.Semaphore
	availableByteDivisor int
	maxByteLimit         int64
	byteLimit            int64
	totalJournalBytes    uint64
	availableBytes       uint64
}

var _ diskLimiter = (*semaphoreDiskLimiter)(nil)

func newSemaphoreDiskLimiter(
	availableByteDivisor int, maxByteLimit int64) *semaphoreDiskLimiter {
	s := kbfssync.NewSemaphore()
	s.Release(maxByteLimit)
	return &semaphoreDiskLimiter{
		s:                    s,
		availableByteDivisor: availableByteDivisor,
		byteLimit:            maxByteLimit,
		maxByteLimit:         maxByteLimit,
	}
}

func (s *semaphoreDiskLimiter) onUpdateAvailableBytes(
	availableBytes uint64) int64 {
	byteLimit := int64(availableBytes) / int64(s.availableByteDivisor)
	if byteLimit > s.maxByteLimit {
		byteLimit = s.maxByteLimit
	}

	s.availableBytes = availableBytes
	oldByteLimit := s.byteLimit
	s.byteLimit = byteLimit

	if s.byteLimit > oldByteLimit {
		return s.s.Release(int64(s.byteLimit - oldByteLimit))
	}
	if s.byteLimit < oldByteLimit {
		return s.s.ForceAcquire(int64(byteLimit - s.byteLimit))
	}
	return s.byteLimit
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
