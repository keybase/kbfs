// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"

	"github.com/keybase/kbfs/kbfssync"
	"golang.org/x/net/context"
)

// semaphoreDiskLimiter is an implementation of diskLimiter that uses
// a semaphore.
type semaphoreDiskLimiter struct {
	s                    *kbfssync.Semaphore
	availableByteDivisor int64
	maxByteLimit         int64

	lock sync.RWMutex
	// byteLimit is min(maxByteLimit,
	// (availableBytes + journalBytes) / availableByteDivisor).
	byteLimit      int64
	journalBytes   int64
	availableBytes uint64
}

var _ diskLimiter = (*semaphoreDiskLimiter)(nil)

func newSemaphoreDiskLimiter(
	availableByteDivisor, maxByteLimit int64) *semaphoreDiskLimiter {
	if availableByteDivisor < 1 {
		panic("availableByteDivisor must be >= 1")
	}
	if maxByteLimit <= 0 {
		panic("maxByteLimit must be > 0")
	}

	s := kbfssync.NewSemaphore()
	s.Release(maxByteLimit)
	return &semaphoreDiskLimiter{
		s:                    s,
		availableByteDivisor: availableByteDivisor,
		maxByteLimit:         maxByteLimit,

		byteLimit: maxByteLimit,
	}
}

func (s *semaphoreDiskLimiter) getByteLimit() int64 {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.byteLimit
}

func (s *semaphoreDiskLimiter) onUpdateAvailableBytes(
	availableBytes uint64) int64 {
	s.lock.Lock()
	defer s.lock.Unlock()

	dividedAvailableBytes := availableBytes / uint64(s.availableByteDivisor)
	var newByteLimit int64
	if dividedAvailableBytes > uint64(s.maxByteLimit) {
		newByteLimit = s.maxByteLimit
	} else {
		newByteLimit = int64(dividedAvailableBytes)
	}

	s.availableBytes = availableBytes
	oldByteLimit := s.byteLimit
	s.byteLimit = newByteLimit

	if newByteLimit > oldByteLimit {
		return s.s.Release(newByteLimit - oldByteLimit)
	}
	if newByteLimit < oldByteLimit {
		return s.s.ForceAcquire(oldByteLimit - newByteLimit)
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
