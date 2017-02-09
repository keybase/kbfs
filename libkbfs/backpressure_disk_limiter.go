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
// limit.
//
// Let J be the (approximate) byte usage of the journal and F be the
// free bytes on disk. Then we want to enforce
//
//   J <= min(J+F, L),
//
// where L is the absolute byte usage limit. But in addition to that,
// we want to set thresholds 0 <= m <= M <= 1 such that we apply
// proportional backpressure (with a given maximum delay) when
//
//   m <= max(J/(J+F), J/L) <= M.
type backpressureDiskLimiter struct {
	log logger.Logger
	// backpressureMinThreshold is m in the above.
	backpressureMinThreshold float64
	// backpressureMaxThreshold is M in the above.
	backpressureMaxThreshold float64
	// maxJournalBytes is L in the above.
	maxJournalBytes int64
	maxDelay        time.Duration
	delayFn         func(context.Context, time.Duration) error
	freeBytesFn     func() (int64, error)

	// bytesLock protects freeBytes, journalBytes,
	// bytesSemaphoreMax, and the (implicit) maximum value of
	// bytesSemaphore (== bytesSemaphoreMax).
	bytesLock         sync.Mutex
	journalBytes      int64
	freeBytes         int64
	bytesSemaphoreMax int64
	bytesSemaphore    *kbfssync.Semaphore
}

var _ diskLimiter = (*backpressureDiskLimiter)(nil)

// newBackpressureDiskLimiterWithFunctions constructs a new
// backpressureDiskLimiter with the given parameters, and also the
// given delay function, which is overridden in tests.
func newBackpressureDiskLimiterWithFunctions(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold float64,
	maxJournalBytes int64, maxDelay time.Duration,
	delayFn func(context.Context, time.Duration) error,
	freeBytesFn func() (int64, error)) (
	*backpressureDiskLimiter, error) {
	if backpressureMinThreshold < 0.0 {
		return nil, errors.Errorf("backpressureMinThreshold=%d < 0.0",
			backpressureMinThreshold)
	}
	if backpressureMaxThreshold < backpressureMinThreshold {
		return nil, errors.Errorf(
			"backpressureMaxThreshold=%d < backpressureMinThreshold=%d",
			backpressureMaxThreshold, backpressureMinThreshold)
	}
	if 1.0 < backpressureMaxThreshold {
		return nil, errors.Errorf("1.0 < backpressureMaxThreshold=%d",
			backpressureMaxThreshold)
	}
	freeBytes, err := freeBytesFn()
	if err != nil {
		return nil, err
	}
	bdl := &backpressureDiskLimiter{
		log, backpressureMinThreshold, backpressureMaxThreshold,
		maxJournalBytes, maxDelay, delayFn, freeBytesFn,
		sync.Mutex{}, 0, freeBytes, 0, kbfssync.NewSemaphore(),
	}
	func() {
		bdl.bytesLock.Lock()
		defer bdl.bytesLock.Unlock()
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

func defaultGetFreeBytes(path string) (int64, error) {
	// getDiskLimits returns availableBytes, but we want to avoid
	// confusing that with availBytes in the sense of the
	// semaphore value.
	freeBytes, err := getDiskLimits(path)
	if err != nil {
		return 0, err
	}

	if freeBytes > uint64(math.MaxInt64) {
		return math.MaxInt64, nil
	}
	return int64(freeBytes), nil
}

// newBackpressureDiskLimiter constructs a new backpressureDiskLimiter
// with the given parameters.
func newBackpressureDiskLimiter(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold float64,
	byteLimit int64, maxDelay time.Duration,
	journalPath string) (*backpressureDiskLimiter, error) {
	return newBackpressureDiskLimiterWithFunctions(
		log, backpressureMinThreshold, backpressureMaxThreshold,
		byteLimit, maxDelay, defaultDoDelay,
		func() (int64, error) {
			return defaultGetFreeBytes(journalPath)
		})
}

func (s *backpressureDiskLimiter) getLockedVarsForTest() (
	journalBytes int64, freeBytes int64, bytesSemaphoreMax int64) {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	return s.journalBytes, s.freeBytes, s.bytesSemaphoreMax
}

// updateBytesSemaphoreMaxLocked must be called (under s.bytesLock)
// whenever s.journalBytes or s.freeBytes changes.
func (s *backpressureDiskLimiter) updateBytesSemaphoreMaxLocked() {
	// Set newMax to min(J+F, L).
	freeBytesWithoutJournal := s.journalBytes + s.freeBytes
	newMax := s.maxJournalBytes
	if freeBytesWithoutJournal < newMax {
		newMax = freeBytesWithoutJournal
	}

	delta := newMax - s.bytesSemaphoreMax
	if delta > 0 {
		s.bytesSemaphore.Release(delta)
	} else if delta < 0 {
		s.bytesSemaphore.ForceAcquire(-delta)
	}
	s.bytesSemaphoreMax = newMax
}

func (s backpressureDiskLimiter) onJournalEnable(
	ctx context.Context, journalBytes int64) int64 {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes += journalBytes
	s.updateBytesSemaphoreMaxLocked()
	return s.bytesSemaphore.Count()
}

func (s backpressureDiskLimiter) onJournalDisable(
	ctx context.Context, journalBytes int64) {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes -= journalBytes
	s.updateBytesSemaphoreMaxLocked()
}

func (s *backpressureDiskLimiter) calculateDelay(
	ctx context.Context, journalBytes, freeBytes int64) time.Duration {
	// Convert first to avoid overflow.
	journalBytesFloat := float64(journalBytes)
	maxJournalBytesFloat := float64(s.maxJournalBytes)
	freeBytesFloat := float64(freeBytes)

	// Set r to max(J/(J+F), J/L).
	r := math.Max(journalBytesFloat/(journalBytesFloat+freeBytesFloat),
		journalBytesFloat/maxJournalBytesFloat)

	// We want the delay to be 0 if r <= m and the max delay if r
	// >= M, so linearly interpolate the delay based on r.
	m := s.backpressureMinThreshold
	M := s.backpressureMaxThreshold
	scale := math.Min(1.0, math.Max(0.0, (r-m)/(M-m)))

	// Set maxDelay to min(s.maxDelay, time until deadline - 1s).
	maxDelay := s.maxDelay
	if deadline, ok := ctx.Deadline(); ok {
		// Subtract a second to allow for some slack.
		remainingTime := deadline.Sub(time.Now()) - time.Second
		if remainingTime < maxDelay {
			maxDelay = remainingTime
		}
	}

	return time.Duration(scale * float64(maxDelay))
}

func (s backpressureDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return s.bytesSemaphore.Count(), errors.New(
			"backpressureDiskLimiter.beforeBlockPut called with 0 blockBytes")
	}

	journalBytes, freeBytes, err := func() (int64, int64, error) {
		s.bytesLock.Lock()
		defer s.bytesLock.Unlock()

		freeBytes, err := s.freeBytesFn()
		if err != nil {
			return 0, 0, err
		}

		s.freeBytes = freeBytes
		s.updateBytesSemaphoreMaxLocked()
		return s.journalBytes, s.freeBytes, nil
	}()
	if err != nil {
		return s.bytesSemaphore.Count(), err
	}

	delay := s.calculateDelay(ctx, journalBytes, freeBytes)
	if delay > 0 {
		s.log.CDebugf(ctx, "Delaying block put of %d bytes by %f s",
			blockBytes, delay.Seconds())
	}
	// TODO: Update delay if any variables change (i.e., we
	// suddenly free up a lot of space).
	err = s.delayFn(ctx, delay)
	if err != nil {
		return s.bytesSemaphore.Count(), err
	}

	return s.bytesSemaphore.Acquire(ctx, blockBytes)
}

func (s backpressureDiskLimiter) afterBlockPut(
	ctx context.Context, blockBytes int64, putData bool) {
	if putData {
		s.bytesLock.Lock()
		defer s.bytesLock.Unlock()
		s.journalBytes += blockBytes
		s.updateBytesSemaphoreMaxLocked()
	} else {
		s.bytesSemaphore.Release(blockBytes)

		s.bytesLock.Lock()
		defer s.bytesLock.Unlock()
		s.journalBytes -= blockBytes
		s.updateBytesSemaphoreMaxLocked()
	}
}

func (s backpressureDiskLimiter) onBlockDelete(
	ctx context.Context, blockBytes int64) {
	if blockBytes > 0 {
		s.bytesSemaphore.Release(blockBytes)
	}

	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes -= blockBytes
	s.updateBytesSemaphoreMaxLocked()
}
