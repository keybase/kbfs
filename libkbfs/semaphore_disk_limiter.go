// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"

	"github.com/keybase/kbfs/kbfssync"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

const defaultAvailableFiles = math.MaxInt64

// semaphoreDiskLimiter is an implementation of diskLimiter that uses
// a semaphore.
//
// TODO: Also do limiting based on file counts.
type semaphoreDiskLimiter struct {
	bytesSemaphore *kbfssync.Semaphore
}

var _ diskLimiter = semaphoreDiskLimiter{}

func newSemaphoreDiskLimiter(maxJournalBytes int64) semaphoreDiskLimiter {
	bytesSemaphore := kbfssync.NewSemaphore()
	bytesSemaphore.Release(maxJournalBytes)
	return semaphoreDiskLimiter{bytesSemaphore}
}

func (sdl semaphoreDiskLimiter) onJournalEnable(
	ctx context.Context, journalBytes, journalFiles int64) (
	availableBytes, availableFiles int64) {
	if journalBytes == 0 {
		return sdl.bytesSemaphore.Count(), defaultAvailableFiles
	}
	availableBytes = sdl.bytesSemaphore.ForceAcquire(journalBytes)
	return availableBytes, defaultAvailableFiles
}

func (sdl semaphoreDiskLimiter) onJournalDisable(
	ctx context.Context, journalBytes, journalFiles int64) {
	if journalBytes > 0 {
		sdl.bytesSemaphore.Release(journalBytes)
	}
}

func (sdl semaphoreDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes, blockFiles int64) (
	availableBytes, availableFiles int64, err error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return sdl.bytesSemaphore.Count(), defaultAvailableFiles, errors.New(
			"semaphore.DiskLimiter.beforeBlockPut called with 0 blockBytes")
	}

	availableBytes, err = sdl.bytesSemaphore.Acquire(ctx, blockBytes)
	return availableBytes, defaultAvailableFiles, err
}

func (sdl semaphoreDiskLimiter) afterBlockPut(
	ctx context.Context, blockBytes, blockFiles int64, putData bool) {
	if !putData {
		sdl.bytesSemaphore.Release(blockBytes)
	}
}

func (sdl semaphoreDiskLimiter) onBlockDelete(
	ctx context.Context, blockBytes, blockFiles int64) {
	if blockBytes > 0 {
		sdl.bytesSemaphore.Release(blockBytes)
	}
}

func (sdl semaphoreDiskLimiter) getStatus() interface{} {
	return nil
}
