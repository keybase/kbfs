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
// semaphores to limit the byte, file, and quota usage.
type semaphoreDiskLimiter struct {
	byteLimit      int64
	byteSemaphore  *kbfssync.Semaphore
	fileLimit      int64
	fileSemaphore  *kbfssync.Semaphore
	quotaLimit     int64
	quotaSemaphore *kbfssync.Semaphore
}

var _ DiskLimiter = semaphoreDiskLimiter{}

func newSemaphoreDiskLimiter(
	byteLimit, fileLimit, quotaLimit int64) semaphoreDiskLimiter {
	byteSemaphore := kbfssync.NewSemaphore()
	byteSemaphore.Release(byteLimit)
	fileSemaphore := kbfssync.NewSemaphore()
	fileSemaphore.Release(fileLimit)
	quotaSemaphore := kbfssync.NewSemaphore()
	quotaSemaphore.Release(quotaLimit)
	return semaphoreDiskLimiter{
		byteLimit, byteSemaphore, fileLimit, fileSemaphore,
		quotaLimit, quotaSemaphore,
	}
}

func (sdl semaphoreDiskLimiter) onJournalEnable(
	ctx context.Context,
	journalStoredBytes, journalUnflushedBytes, journalFiles int64) (
	availableBytes, availableFiles int64) {
	if journalStoredBytes != 0 {
		availableBytes = sdl.byteSemaphore.ForceAcquire(journalStoredBytes)
	} else {
		availableBytes = sdl.byteSemaphore.Count()
	}
	// storedBytes should be >= unflushedBytes. But it's not too
	// bad to let it go through.
	if journalFiles != 0 {
		availableFiles = sdl.fileSemaphore.ForceAcquire(journalFiles)
	} else {
		availableFiles = sdl.fileSemaphore.Count()
	}
	if journalUnflushedBytes != 0 {
		sdl.quotaSemaphore.ForceAcquire(journalUnflushedBytes)
	}
	return availableBytes, availableFiles
}

func (sdl semaphoreDiskLimiter) onJournalDisable(
	ctx context.Context,
	journalStoredBytes, journalUnflushedBytes, journalFiles int64) {
	if journalStoredBytes != 0 {
		sdl.byteSemaphore.Release(journalStoredBytes)
	}
	// As above, storedBytes should be >= unflushedBytes. Let it
	// go through here, too.
	if journalFiles != 0 {
		sdl.fileSemaphore.Release(journalFiles)
	}
	if journalUnflushedBytes != 0 {
		sdl.quotaSemaphore.Release(journalUnflushedBytes)
	}
}

func (sdl semaphoreDiskLimiter) onDiskBlockCacheEnable(
	ctx context.Context, diskCacheBytes int64) {
	if diskCacheBytes != 0 {
		sdl.byteSemaphore.ForceAcquire(diskCacheBytes)
	}
}

func (sdl semaphoreDiskLimiter) onDiskBlockCacheDisable(
	ctx context.Context, diskCacheBytes int64) {
	if diskCacheBytes != 0 {
		sdl.byteSemaphore.Release(diskCacheBytes)
	}
}

func (sdl semaphoreDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes, blockFiles int64) (
	availableBytes, availableFiles int64, err error) {
	// Better to return an error than to panic in Acquire.
	if blockBytes == 0 {
		return sdl.byteSemaphore.Count(), sdl.fileSemaphore.Count(), errors.New(
			"semaphore.DiskLimiter.beforeBlockPut called with 0 blockBytes")
	}
	if blockFiles == 0 {
		return sdl.byteSemaphore.Count(), sdl.fileSemaphore.Count(), errors.New(
			"semaphore.DiskLimiter.beforeBlockPut called with 0 blockFiles")
	}

	availableBytes, err = sdl.byteSemaphore.Acquire(ctx, blockBytes)
	if err != nil {
		return availableBytes, sdl.fileSemaphore.Count(), err
	}
	defer func() {
		if err != nil {
			sdl.byteSemaphore.Release(blockBytes)
			availableBytes = sdl.byteSemaphore.Count()
		}
	}()

	availableFiles, err = sdl.fileSemaphore.Acquire(ctx, blockFiles)
	return availableBytes, availableFiles, err
}

func (sdl semaphoreDiskLimiter) afterBlockPut(
	ctx context.Context, blockBytes, blockFiles int64, putData bool) {
	if putData {
		sdl.quotaSemaphore.Acquire(ctx, blockBytes)
	} else {
		sdl.byteSemaphore.Release(blockBytes)
		sdl.fileSemaphore.Release(blockFiles)
	}
}

func (sdl semaphoreDiskLimiter) onBlocksFlush(
	ctx context.Context, blockBytes int64) {
	if blockBytes != 0 {
		sdl.quotaSemaphore.Release(blockBytes)
	}
}

func (sdl semaphoreDiskLimiter) onBlocksDelete(
	ctx context.Context, blockBytes, blockFiles int64) {
	if blockBytes != 0 {
		sdl.byteSemaphore.Release(blockBytes)
	}
	if blockFiles != 0 {
		sdl.fileSemaphore.Release(blockFiles)
	}
}

func (sdl semaphoreDiskLimiter) onDiskBlockCacheDelete(ctx context.Context,
	blockBytes int64) {
	sdl.onBlocksDelete(ctx, blockBytes, 0)
}

func (sdl semaphoreDiskLimiter) beforeDiskBlockCachePut(ctx context.Context,
	blockBytes int64) (availableBytes int64, err error) {
	if blockBytes == 0 {
		return 0, errors.New("semaphoreDiskLimiter.beforeDiskBlockCachePut" +
			" called with 0 blockBytes")
	}
	return sdl.byteSemaphore.ForceAcquire(blockBytes), nil
}

func (sdl semaphoreDiskLimiter) afterDiskBlockCachePut(ctx context.Context,
	blockBytes int64, putData bool) {
	if !putData {
		sdl.byteSemaphore.Release(blockBytes)
	}
}

func (sdl semaphoreDiskLimiter) getQuotaInfo() (usedQuotaBytes, quotaBytes int64) {
	return sdl.quotaSemaphore.Count(), sdl.quotaLimit
}

type semaphoreDiskLimiterStatus struct {
	Type string

	// Derived numbers.
	ByteUsageFrac  float64
	FileUsageFrac  float64
	QuotaUsageFrac float64

	// Raw numbers.
	LimitMB        float64
	AvailableMB    float64
	LimitFiles     float64
	AvailableFiles float64
	LimitQuota     float64
	AvailableQuota float64
}

func (sdl semaphoreDiskLimiter) getStatus() interface{} {
	const MB float64 = 1024 * 1024

	limitMB := float64(sdl.byteLimit) / MB
	availableMB := float64(sdl.byteSemaphore.Count()) / MB
	byteUsageFrac := 1 - availableMB/limitMB

	limitFiles := float64(sdl.fileLimit) / MB
	availableFiles := float64(sdl.fileSemaphore.Count()) / MB
	fileUsageFrac := 1 - availableFiles/limitFiles

	limitQuota := float64(sdl.quotaLimit) / MB
	availableQuota := float64(sdl.quotaSemaphore.Count()) / MB
	quotaUsageFrac := 1 - availableQuota/limitQuota

	return semaphoreDiskLimiterStatus{
		Type: "SemaphoreDiskLimiter",

		ByteUsageFrac:  byteUsageFrac,
		FileUsageFrac:  fileUsageFrac,
		QuotaUsageFrac: quotaUsageFrac,

		LimitMB:        limitMB,
		AvailableMB:    availableMB,
		LimitFiles:     limitFiles,
		AvailableFiles: availableFiles,
		LimitQuota:     limitQuota,
		AvailableQuota: availableQuota,
	}
}
