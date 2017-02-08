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
// uses backpressure.
//
// Let J be the (approximate) byte usage of the journal and A be the
// available bytes on disk. Then J/(J + A) is the byte usage ratio of
// the journal. We want to set thresholds m and M such that we apply
// proportional backpressure when m <= J/(J+A) <= M. In addition, we
// want to have an absolute byte usage limit L and apply backpressure
// when m <= J/L <= M.
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
	availBytesFn    func() (int64, error)

	bytesLock      sync.RWMutex
	availBytes     int64
	journalBytes   int64
	semaphoreMax   int64
	bytesSemaphore *kbfssync.Semaphore
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
	availBytesFn func() (int64, error)) *backpressureDiskLimiter {
	if backpressureMinThreshold < 0.0 {
		panic("backpressureMinThreshold < 0.0")
	}
	if backpressureMaxThreshold < backpressureMinThreshold {
		panic("backpressureMaxThreshold < backpressureMinThreshold")
	}
	if 1.0 < backpressureMaxThreshold {
		panic("1.0 < backpressureMaxThreshold")
	}
	return &backpressureDiskLimiter{
		log, backpressureMinThreshold, backpressureMaxThreshold,
		maxJournalBytes, maxDelay, delayFn, availBytesFn,
		sync.RWMutex{}, 0, 0, 0, kbfssync.NewSemaphore(),
	}
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

// newBackpressureDiskLimiter constructs a new backpressureDiskLimiter
// with the given parameters.
func newBackpressureDiskLimiter(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold float64,
	byteLimit int64, maxDelay time.Duration,
	journalPath string) *backpressureDiskLimiter {
	return newBackpressureDiskLimiterWithFunctions(
		log, backpressureMinThreshold, backpressureMaxThreshold,
		byteLimit, maxDelay, defaultDoDelay, func() (int64, error) {
			availBytes, err := getDiskLimits(journalPath)
			if err != nil {
				return 0, err
			}

			if availBytes > uint64(math.MaxInt64) {
				return math.MaxInt64, nil
			}
			return int64(availBytes), nil
		})
}

func (s *backpressureDiskLimiter) updateBytesSemaphoreLocked() {
	newMax := s.maxJournalBytes
	if s.availBytes < newMax {
		newMax = s.availBytes + s.journalBytes
	}

	delta := newMax - s.semaphoreMax
	if delta > 0 {
		s.bytesSemaphore.Release(delta)
	} else if delta < 0 {
		s.bytesSemaphore.ForceAcquire(-delta)
	}
	s.semaphoreMax = newMax
}

func (s backpressureDiskLimiter) onJournalEnable(
	ctx context.Context, journalBytes int64) int64 {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes += journalBytes
	s.updateBytesSemaphoreLocked()
	return 0
}

func (s backpressureDiskLimiter) onJournalDisable(
	ctx context.Context, journalBytes int64) {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes -= journalBytes
	s.updateBytesSemaphoreLocked()
}

func (s *backpressureDiskLimiter) getDelay(
	journalBytes, availBytes int64) time.Duration {
	r := math.Max(
		float64(journalBytes)/float64(s.maxJournalBytes),
		float64(journalBytes)/(float64(journalBytes)+float64(availBytes)))
	m := s.backpressureMinThreshold
	M := s.backpressureMaxThreshold
	scale := math.Min(1.0, math.Max(0.0, (r-m)/(M-m)))
	return time.Duration(scale * float64(s.maxDelay))
}

func (s backpressureDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return s.bytesSemaphore.Count(), errors.New(
			"beforeBlockPut called with 0 blockBytes")
	}

	journalBytes, availBytes, err := func() (int64, int64, error) {
		s.bytesLock.Lock()
		defer s.bytesLock.Unlock()

		availBytes, err := s.availBytesFn()
		if err != nil {
			return 0, 0, err
		}

		s.availBytes = availBytes
		s.updateBytesSemaphoreLocked()
		return s.journalBytes, availBytes, nil
	}()
	if err != nil {
		return s.bytesSemaphore.Count(), err
	}

	delay := s.getDelay(journalBytes, availBytes)
	if delay > 0 {
		s.log.CDebugf(ctx, "Delaying block put of %d bytes by %f s",
			blockBytes, delay.Seconds())
	}
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
		s.updateBytesSemaphoreLocked()
	} else {
		s.bytesSemaphore.Release(blockBytes)
	}
}

func (s backpressureDiskLimiter) onBlockDelete(
	ctx context.Context, blockBytes int64) {
	s.bytesLock.Lock()
	defer s.bytesLock.Unlock()
	s.journalBytes -= blockBytes
	s.updateBytesSemaphoreLocked()
}
