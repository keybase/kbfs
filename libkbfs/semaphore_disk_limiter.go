// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"

	"github.com/keybase/kbfs/kbfssync"
	"golang.org/x/net/context"
)

// semaphoreDiskLimiter is an implementation of diskLimiter that uses
// a semaphore.
type semaphoreDiskLimiter struct {
	s *kbfssync.Semaphore
}

var _ diskLimiter = semaphoreDiskLimiter{}

func newSemaphoreDiskLimiter(byteLimit int64) semaphoreDiskLimiter {
	s := kbfssync.NewSemaphore()
	s.Release(byteLimit)
	return semaphoreDiskLimiter{s}
}

func (s semaphoreDiskLimiter) onJournalEnable(journalBytes int64) {
	if journalBytes > 0 {
		s.s.ForceAcquire(journalBytes)
	}
}

func (s semaphoreDiskLimiter) onJournalDisable(journalBytes int64) {
	if journalBytes > 0 {
		s.s.Release(journalBytes)
	}
}

func (s semaphoreDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) error {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return errors.New("beforeBlockPut called with 0 blockBytes")
	}
	_, err := s.s.Acquire(ctx, blockBytes)
	return err
}

func (s semaphoreDiskLimiter) onBlockPutFail(blockBytes int64) {
	s.s.Release(blockBytes)
}

func (s semaphoreDiskLimiter) onBlockDelete(blockBytes int64) {
	if blockBytes > 0 {
		s.s.Release(blockBytes)
	}
}
