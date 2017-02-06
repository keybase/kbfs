// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"time"

	"github.com/keybase/client/go/logger"
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

	journalBytesLock sync.RWMutex
	journalBytes     int64
}

var _ diskLimiter = (*backpressureDiskLimiter)(nil)

// newBackpressureDiskLimiterWithDelayFunction constructs a new
// backpressureDiskLimiter with the given parameters, and also the
// given delay function, which is overridden in tests.
func newBackpressureDiskLimiterWithDelayFunction(
	log logger.Logger,
	backpressureMinThreshold, backpressureMaxThreshold float64,
	maxJournalBytes int64, maxDelay time.Duration,
	delayFn func(context.Context, time.Duration) error) *backpressureDiskLimiter {
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
		maxJournalBytes, maxDelay, delayFn, sync.RWMutex{}, 0,
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
	byteLimit int64, maxDelay time.Duration) *backpressureDiskLimiter {
	return newBackpressureDiskLimiterWithDelayFunction(
		log, backpressureMinThreshold, backpressureMaxThreshold,
		byteLimit, maxDelay, defaultDoDelay)
}

func (s backpressureDiskLimiter) onJournalEnable(
	ctx context.Context, journalBytes int64) int64 {
	s.journalBytesLock.Lock()
	defer s.journalBytesLock.Unlock()
	s.journalBytes += journalBytes
	return 0
}

func (s backpressureDiskLimiter) onJournalDisable(
	ctx context.Context, journalBytes int64) {
	s.journalBytesLock.Lock()
	defer s.journalBytesLock.Unlock()
	s.journalBytes -= journalBytes
}

func (s *backpressureDiskLimiter) getDelay() time.Duration {
	journalBytes := func() int64 {
		s.journalBytesLock.RLock()
		defer s.journalBytesLock.RUnlock()
		return s.journalBytes
	}()

	absRatio := float64(journalBytes) / float64(s.maxJournalBytes)
	absScale := float64(absRatio-s.backpressureMinThreshold) /
		float64(s.backpressureMaxThreshold-s.backpressureMinThreshold)
	if absScale < 0 {
		absScale = 0
	}
	if absScale > 1 {
		absScale = 1
	}
	return time.Duration(absScale * float64(s.maxDelay))
}

func (s backpressureDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		return 0, errors.New(
			"beforeBlockPut called with 0 blockBytes")
	}

	delay := s.getDelay()
	if delay > 0 {
		s.log.CDebugf(ctx, "Delaying block put of %d bytes by %f s",
			blockBytes, delay.Seconds())
	}
	err := s.delayFn(ctx, delay)
	if err != nil {
		return 0, err
	}

	return 0, nil
}

func (s backpressureDiskLimiter) afterBlockPut(
	ctx context.Context, blockBytes int64, putData bool) {
}

func (s backpressureDiskLimiter) onBlockDelete(
	ctx context.Context, blockBytes int64) {
}
