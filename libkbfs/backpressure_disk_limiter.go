// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"
	"sync"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/kbfssync"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// backpressureDiskLimiter is an implementation of diskLimiter that
// uses backpressure to slow down block puts before they hit the disk
// limits.
//
// Let J be the (approximate) byte/file usage of the journal and F be
// the free bytes/files on disk. Then we want to enforce
//
//   J <= min(k(J+F), L),
//
// where 0 < k <= 1 is some fraction, and L > 0 is the absolute
// byte/file usage limit. But in addition to that, we want to set
// thresholds 0 <= m <= M <= 1 such that we apply proportional
// backpressure (with a given maximum delay) when
//
//   m <= max(J/(k(J+F)), J/L) <= M,
//
// which is equivalent to
//
//   m <= J/min(k(J+F), L) <= M.
type backpressureDiskLimiter struct {
	log logger.Logger

	maxDelay            time.Duration
	delayFn             func(context.Context, time.Duration) error
	freeBytesAndFilesFn func() (int64, int64, error)

	// lock protects freeX, journalX, xSemaphoreMax, and the
	// (implicit) maximum value of xSemaphore (== xSemaphoreMax),
	// where x = Bytes or Files.
	lock sync.Mutex

	byteTracker, fileTracker backpressureTracker
}

type backpressureTracker struct {
	// minThreshold is m in the above.
	minThreshold float64
	// maxThreshold is M in the above.
	maxThreshold float64
	// limitFrac is k in the above.
	limitFrac float64

	// limit is L in the above.
	limit int64

	// used is J in the above.
	used int64
	// free is F in the above.
	free         int64
	semaphoreMax int64
	semaphore    *kbfssync.Semaphore
}

// getMaxResources returns the resource limit, taking into account the
// amount of free resources left. This is min(k(J+F), L).
func (bt backpressureTracker) getMaxResources() float64 {
	// Calculate k(J+F), converting to float64 first to avoid
	// overflow, although losing some precision in the process.
	usedFloat := float64(bt.used)
	freeFloat := float64(bt.free)
	limit := bt.limitFrac * (usedFloat + freeFloat)
	return math.Min(limit, float64(bt.limit))
}

// updateSemaphoreMax must be called whenever bt.used or bt.free
// changes.
func (bt *backpressureTracker) updateSemaphoreMax() {
	newMax := int64(bt.getMaxResources())
	delta := newMax - bt.semaphoreMax
	// These operations are adjusting the *maximum* value of
	// bt.semaphore.
	if delta > 0 {
		bt.semaphore.Release(delta)
	} else if delta < 0 {
		bt.semaphore.ForceAcquire(-delta)
	}
	bt.semaphoreMax = newMax
}

func (bt *backpressureTracker) onJournalEnable(journalResources int64) (
	availableResources int64) {
	bt.used += journalResources
	bt.updateSemaphoreMax()
	if journalResources == 0 {
		return bt.semaphore.Count()
	}
	return bt.semaphore.ForceAcquire(journalResources)
}

func (bt *backpressureTracker) onJournalDisable(journalResources int64) {
	bt.used -= journalResources
	bt.updateSemaphoreMax()
	if journalResources > 0 {
		bt.semaphore.Release(journalResources)
	}
}

func (bt backpressureTracker) calculateFreeSpaceFrac() float64 {
	return float64(bt.used) / bt.getMaxResources()
}

func (bt backpressureTracker) calculateDelayScale() float64 {
	freeSpaceFrac := bt.calculateFreeSpaceFrac()

	// We want the delay to be 0 if freeSpaceFrac <= m and the
	// max delay if freeSpaceFrac >= M, so linearly interpolate
	// the delay scale.
	m := bt.minThreshold
	M := bt.maxThreshold
	return math.Min(1.0, math.Max(0.0, (freeSpaceFrac-m)/(M-m)))
}

var _ diskLimiter = (*backpressureDiskLimiter)(nil)

// newBackpressureDiskLimiterWithFunctions constructs a new
// backpressureDiskLimiter with the given parameters, and also the
// given delay function, which is overridden in tests.
func newBackpressureDiskLimiterWithFunctions(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold, limitFrac float64,
	byteLimit, fileLimit int64, maxDelay time.Duration,
	delayFn func(context.Context, time.Duration) error,
	freeBytesAndFilesFn func() (int64, int64, error)) (
	*backpressureDiskLimiter, error) {
	if backpressureMinThreshold < 0.0 {
		return nil, errors.Errorf("backpressureMinThreshold=%f < 0.0",
			backpressureMinThreshold)
	}
	if backpressureMaxThreshold < backpressureMinThreshold {
		return nil, errors.Errorf(
			"backpressureMaxThreshold=%f < backpressureMinThreshold=%f",
			backpressureMaxThreshold, backpressureMinThreshold)
	}
	if 1.0 < backpressureMaxThreshold {
		return nil, errors.Errorf("1.0 < backpressureMaxThreshold=%f",
			backpressureMaxThreshold)
	}
	if limitFrac < 0.01 {
		return nil, errors.Errorf("limitFrac=%f < 0.01",
			limitFrac)
	}
	if limitFrac > 1.0 {
		return nil, errors.Errorf("limitFrac=%f > 1.0",
			limitFrac)
	}
	freeBytes, freeFiles, err := freeBytesAndFilesFn()
	if err != nil {
		return nil, err
	}
	bdl := &backpressureDiskLimiter{
		log, maxDelay, delayFn, freeBytesAndFilesFn, sync.Mutex{},
		backpressureTracker{
			backpressureMinThreshold, backpressureMaxThreshold,
			limitFrac, byteLimit, 0, freeBytes, 0,
			kbfssync.NewSemaphore(),
		},
		backpressureTracker{
			backpressureMinThreshold, backpressureMaxThreshold,
			limitFrac, fileLimit, 0, freeFiles, 0,
			kbfssync.NewSemaphore(),
		},
	}
	func() {
		bdl.lock.Lock()
		defer bdl.lock.Unlock()
		bdl.updateBytesSemaphoreMaxLocked()
	}()
	return bdl, nil
}

// defaultDoDelay uses a timer to delay by the given duration.
func defaultDoDelay(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		timer.Stop()
		return errors.WithStack(ctx.Err())
	}
}

func defaultGetFreeBytesAndFiles(path string) (int64, int64, error) {
	// getDiskLimits returns availableBytes and availableFiles,
	// but we want to avoid confusing that with availBytes and
	// availFiles in the sense of the semaphore value.
	freeBytes, freeFiles, err := getDiskLimits(path)
	if err != nil {
		return 0, 0, err
	}

	if freeBytes > uint64(math.MaxInt64) {
		freeBytes = math.MaxInt64
	}
	if freeFiles > uint64(math.MaxInt64) {
		freeFiles = math.MaxInt64
	}
	return int64(freeBytes), int64(freeFiles), nil
}

// newBackpressureDiskLimiter constructs a new backpressureDiskLimiter
// with the given parameters.
func newBackpressureDiskLimiter(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold, limitFrac float64,
	byteLimit, fileLimit int64, maxDelay time.Duration,
	journalPath string) (*backpressureDiskLimiter, error) {
	return newBackpressureDiskLimiterWithFunctions(
		log, backpressureMinThreshold, backpressureMaxThreshold,
		limitFrac, byteLimit, fileLimit, maxDelay,
		defaultDoDelay, func() (int64, int64, error) {
			return defaultGetFreeBytesAndFiles(journalPath)
		})
}

func (bdl *backpressureDiskLimiter) getLockedVarsForTest() (
	journalBytes int64, freeBytes int64, byteSemaphoreMax int64) {
	bdl.lock.Lock()
	defer bdl.lock.Unlock()
	return bdl.byteTracker.used, bdl.byteTracker.free, bdl.byteTracker.semaphoreMax
}

// updateBytesSemaphoreMaxLocked must be called (under s.lock)
// whenever s.journalBytes or s.freeBytes changes.
func (bdl *backpressureDiskLimiter) updateBytesSemaphoreMaxLocked() {
	bdl.byteTracker.updateSemaphoreMax()
}

func (bdl *backpressureDiskLimiter) onJournalEnable(
	ctx context.Context, journalBytes, journalFiles int64) (
	availableBytes, availableFiles int64) {
	bdl.lock.Lock()
	defer bdl.lock.Unlock()
	availableBytes = bdl.byteTracker.onJournalEnable(journalBytes)
	return availableBytes, defaultAvailableFiles
}

func (bdl *backpressureDiskLimiter) onJournalDisable(
	ctx context.Context, journalBytes, journalFiles int64) {
	bdl.lock.Lock()
	defer bdl.lock.Unlock()
	bdl.byteTracker.onJournalDisable(journalBytes)
}

func (bdl *backpressureDiskLimiter) calculateDelayLocked(
	ctx context.Context, now time.Time) time.Duration {
	delayScale := bdl.byteTracker.calculateDelayScale()

	// Set maxDelay to min(bdl.maxDelay, time until deadline - 1s).
	maxDelay := bdl.maxDelay
	if deadline, ok := ctx.Deadline(); ok {
		// Subtract a second to allow for some slack.
		remainingTime := deadline.Sub(now) - time.Second
		if remainingTime < maxDelay {
			maxDelay = remainingTime
		}
	}

	return time.Duration(delayScale * float64(maxDelay))
}

func (bdl *backpressureDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes, blockFiles int64) (
	availableBytes, availableFiles int64, err error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return bdl.byteTracker.semaphore.Count(), defaultAvailableFiles, errors.New(
			"backpressureDiskLimiter.beforeBlockPut called with 0 blockBytes")
	}

	delay, err := func() (time.Duration, error) {
		bdl.lock.Lock()
		defer bdl.lock.Unlock()

		freeBytes, _, err := bdl.freeBytesAndFilesFn()
		if err != nil {
			return 0, err
		}

		bdl.byteTracker.free = freeBytes
		bdl.updateBytesSemaphoreMaxLocked()

		delay := bdl.calculateDelayLocked(ctx, time.Now())
		if delay > 0 {
			bdl.log.CDebugf(ctx, "Delaying block put of %d bytes by %f s ("+
				"journalBytes=%d freeBytes=%d)",
				blockBytes, delay.Seconds(), journalBytes, freeBytes)
		}

		return delay, nil
	}()
	if err != nil {
		return bdl.byteTracker.semaphore.Count(), defaultAvailableFiles, err
	}

	// TODO: Update delay if any variables change (i.e., we
	// suddenly free up a lot of space).
	err = bdl.delayFn(ctx, delay)
	if err != nil {
		return bdl.byteTracker.semaphore.Count(), defaultAvailableFiles, err
	}

	availableFiles, err = bdl.byteTracker.semaphore.Acquire(ctx, blockBytes)
	return availableFiles, defaultAvailableFiles, err
}

func (bdl *backpressureDiskLimiter) afterBlockPut(
	ctx context.Context, blockBytes, blockFiles int64, putData bool) {
	if putData {
		bdl.lock.Lock()
		defer bdl.lock.Unlock()
		bdl.byteTracker.used += blockBytes
		bdl.updateBytesSemaphoreMaxLocked()
	} else {
		bdl.byteTracker.semaphore.Release(blockBytes)
	}
}

func (bdl *backpressureDiskLimiter) onBlockDelete(
	ctx context.Context, blockBytes, blockFiles int64) {
	if blockBytes == 0 {
		return
	}

	bdl.byteTracker.semaphore.Release(blockBytes)

	bdl.lock.Lock()
	defer bdl.lock.Unlock()
	bdl.byteTracker.used -= blockBytes
	bdl.updateBytesSemaphoreMaxLocked()
}

type backpressureDiskLimiterStatus struct {
	Type string

	// Derived numbers.
	FreeSpaceFrac   float64
	ByteUsageFrac   float64
	DelayScale      float64
	CurrentDelaySec float64

	// Constants.
	BackpressureMinThreshold float64
	BackpressureMaxThreshold float64
	ByteLimitFrac            float64
	FixedLimitMB             float64
	MaxDelaySec              float64

	// Raw numbers.
	JournalMB   float64
	FreeMB      float64
	LimitMB     float64
	AvailableMB float64
}

func (bdl *backpressureDiskLimiter) getStatus() interface{} {
	bdl.lock.Lock()
	defer bdl.lock.Unlock()

	freeSpaceFrac := bdl.byteTracker.calculateFreeSpaceFrac()
	delayScale := bdl.byteTracker.calculateDelayScale()
	currentDelay := bdl.calculateDelayLocked(
		context.Background(), time.Now())

	const MB float64 = 1024 * 1024

	limitMB := float64(bdl.byteTracker.semaphoreMax) / MB
	availableMB := float64(bdl.byteTracker.semaphore.Count()) / MB
	byteUsageFrac := 1 - availableMB/limitMB

	return backpressureDiskLimiterStatus{
		Type: "BackpressureDiskLimiter",

		FreeSpaceFrac:   freeSpaceFrac,
		ByteUsageFrac:   byteUsageFrac,
		DelayScale:      delayScale,
		CurrentDelaySec: currentDelay.Seconds(),

		BackpressureMinThreshold: bdl.byteTracker.minThreshold,
		BackpressureMaxThreshold: bdl.byteTracker.maxThreshold,
		ByteLimitFrac:            bdl.byteTracker.limitFrac,
		FixedLimitMB:             float64(bdl.byteTracker.limit) / MB,
		MaxDelaySec:              bdl.maxDelay.Seconds(),

		JournalMB:   float64(bdl.byteTracker.used) / MB,
		FreeMB:      float64(bdl.byteTracker.free) / MB,
		LimitMB:     limitMB,
		AvailableMB: availableMB,
	}
}
