// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"time"

	"github.com/keybase/kbfs/kbfssync"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// backpressureDiskLimiter is an implementation of diskLimiter that uses
// a backpressure.
type backpressureDiskLimiter struct {
	backpressureMinThreshold int64
	backpressureMaxThreshold int64
	byteLimit                int64
	maxDelay                 time.Duration
	s                        *kbfssync.Semaphore
}

var _ diskLimiter = backpressureDiskLimiter{}

func newBackpressureDiskLimiter(
	backpressureMinThreshold, backpressureMaxThreshold, byteLimit int64,
	maxDelay time.Duration) backpressureDiskLimiter {
	if backpressureMinThreshold < 0 {
		panic("backpressureMinThreshold < 0")
	}
	if backpressureMaxThreshold < backpressureMinThreshold {
		panic("backpressureMaxThreshold < backpressureMinThreshold")
	}
	if byteLimit < backpressureMaxThreshold {
		panic("byteLimit < backpressureMaxThreshold")
	}
	s := kbfssync.NewSemaphore()
	s.Release(byteLimit)
	return backpressureDiskLimiter{
		backpressureMinThreshold, backpressureMaxThreshold,
		byteLimit, maxDelay, s,
	}
}

func (s backpressureDiskLimiter) onJournalEnable(journalBytes int64) int64 {
	if journalBytes == 0 {
		// TODO: This is a bit weird. Add a function to get
		// the current semaphore count, or let ForceAcquire
		// take 0.
		return 0
	}
	return s.s.ForceAcquire(journalBytes)
}

func (s backpressureDiskLimiter) onJournalDisable(journalBytes int64) {
	if journalBytes > 0 {
		s.s.Release(journalBytes)
	}
}

func (s backpressureDiskLimiter) getDelay() time.Duration {
	// Slight hack to get backpressure value.
	s.s.ForceAcquire(1)
	availBytes := s.s.Release(1)

	usedBytes := s.byteLimit - availBytes
	if usedBytes <= s.backpressureMinThreshold {
		return 0
	}

	if usedBytes >= s.backpressureMaxThreshold {
		return s.maxDelay
	}

	scale := float64(usedBytes-s.backpressureMinThreshold) /
		float64(s.backpressureMaxThreshold-s.backpressureMinThreshold)
	delayNs := int64(float64(s.maxDelay.Nanoseconds()) * scale)
	return time.Duration(delayNs) * time.Nanosecond
}

func (s backpressureDiskLimiter) beforeBlockPut(
	ctx context.Context, blockBytes int64) (int64, error) {
	if blockBytes == 0 {
		// Better to return an error than to panic in Acquire.
		//
		// TODO: Return current semaphore count.
		return 0, errors.New("beforeBlockPut called with 0 blockBytes")
	}

	delay := s.getDelay()
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		// TODO: Return current semaphore count.
		return 0, errors.WithStack(ctx.Err())
	}

	return s.s.Acquire(ctx, blockBytes)
}

func (s backpressureDiskLimiter) onBlockPutFail(blockBytes int64) {
	s.s.Release(blockBytes)
}

func (s backpressureDiskLimiter) onBlockDelete(blockBytes int64) {
	if blockBytes > 0 {
		s.s.Release(blockBytes)
	}
}
