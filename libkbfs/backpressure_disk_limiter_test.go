// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"
	"testing"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

// TestDefaultDoDelayCancel checks that defaultDoDelay respects
// context cancellation.
func TestDefaultDoDelayCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), individualTestTimeout)
	cancel()

	err := defaultDoDelay(ctx, individualTestTimeout)
	require.Equal(t, ctx.Err(), errors.Cause(err))
}

func TestBackpressureConstructorError(t *testing.T) {
	log := logger.NewTestLogger(t)
	fakeErr := errors.New("Fake error")
	_, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 100, 8*time.Second, nil,
		func() (int64, error) {
			return 0, fakeErr
		})
	require.Equal(t, fakeErr, err)
}

// TestBackpressureDiskLimiterCounters checks that various counters
// are updated properly for each public method.
func TestBackpressureDiskLimiterCounters(t *testing.T) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	var fakeFreeBytes int64 = 50
	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 100, 8*time.Second, delayFn,
		func() (int64, error) {
			return fakeFreeBytes, nil
		})
	require.NoError(t, err)

	journalBytes, freeBytes, bytesSemaphoreMax :=
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(0), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(50), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	ctx := context.Background()

	// This should change only journalBytes and bytesSemaphoreMax.
	availBytes := bdl.onJournalEnable(ctx, 10)
	require.Equal(t, int64(50), availBytes)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(10), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(60), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	bdl.onJournalDisable(ctx, 9)

	// So should this.
	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(1), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(51), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	// This should max out bytesSemaphoreMax and cause
	// bytesSemaphore to go negative.
	availBytes = bdl.onJournalEnable(ctx, 110)
	require.Equal(t, int64(-11), availBytes)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(111), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(-11), bdl.bytesSemaphore.Count())

	bdl.onJournalDisable(ctx, 110)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(1), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(51), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	// This should be a no-op.
	availBytes = bdl.onJournalEnable(ctx, 0)
	require.Equal(t, int64(50), availBytes)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(1), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(51), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	// So should this.
	bdl.onJournalDisable(ctx, 0)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(1), journalBytes)
	require.Equal(t, int64(50), freeBytes)
	require.Equal(t, int64(51), bytesSemaphoreMax)
	require.Equal(t, int64(50), bdl.bytesSemaphore.Count())

	// Add more free bytes and put a block successfully.

	fakeFreeBytes = 100

	availBytes, err = bdl.beforeBlockPut(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(89), availBytes)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(1), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(89), bdl.bytesSemaphore.Count())

	bdl.afterBlockPut(ctx, 10, true)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(11), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(89), bdl.bytesSemaphore.Count())

	// Then try to put a block but fail it.

	availBytes, err = bdl.beforeBlockPut(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, int64(80), availBytes)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(11), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(80), bdl.bytesSemaphore.Count())

	bdl.afterBlockPut(ctx, 9, false)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(11), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(89), bdl.bytesSemaphore.Count())

	// Finally, delete a block.

	bdl.onBlockDelete(ctx, 11)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(0), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(100), bdl.bytesSemaphore.Count())

	// This should be a no-op.
	bdl.onBlockDelete(ctx, 0)

	journalBytes, freeBytes, bytesSemaphoreMax =
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(0), journalBytes)
	require.Equal(t, int64(100), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(100), bdl.bytesSemaphore.Count())
}

// TestBackpressureDiskLimiterLargeDiskDelay checks the delays when
// pretending to have a large disk.
func TestBackpressureDiskLimiterLargeDiskDelay(t *testing.T) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 100, 8*time.Second, delayFn,
		func() (int64, error) {
			return math.MaxInt64, nil
		})
	require.NoError(t, err)

	journalBytes, freeBytes, bytesSemaphoreMax :=
		bdl.getLockedVarsForTest()
	require.Equal(t, int64(0), journalBytes)
	require.Equal(t, int64(math.MaxInt64), freeBytes)
	require.Equal(t, int64(100), bytesSemaphoreMax)
	require.Equal(t, int64(100), bdl.bytesSemaphore.Count())

	ctx := context.Background()

	// The first two puts shouldn't encounter any backpressure...

	const blockSize = 10
	var bytesPut int

	for i := 0; i < 2; i++ {
		_, err = bdl.beforeBlockPut(ctx, blockSize)
		require.NoError(t, err)
		require.Equal(t, 0*time.Second, lastDelay)

		journalBytes, freeBytes, bytesSemaphoreMax =
			bdl.getLockedVarsForTest()
		require.Equal(t, int64(bytesPut), journalBytes)
		require.Equal(t, int64(math.MaxInt64), freeBytes)
		require.Equal(t, int64(100), bytesSemaphoreMax)
		require.Equal(t, int64(100-bytesPut-blockSize),
			bdl.bytesSemaphore.Count())

		bdl.afterBlockPut(ctx, blockSize, true)

		journalBytes, freeBytes, bytesSemaphoreMax =
			bdl.getLockedVarsForTest()
		require.Equal(t, int64(bytesPut+blockSize), journalBytes)
		require.Equal(t, int64(math.MaxInt64), freeBytes)
		require.Equal(t, int64(100), bytesSemaphoreMax)
		require.Equal(t, int64(100-bytesPut-blockSize),
			bdl.bytesSemaphore.Count())

		bytesPut += blockSize
	}

	// ...but the next seven should encounter increasing
	// backpressure...

	for i := 1; i < 9; i++ {
		_, err := bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.InEpsilon(t, float64(i), lastDelay.Seconds(),
			0.01, "i=%d", i)
		bdl.afterBlockPut(ctx, 10, true)
	}

	// and the last one should stall completely, if not for the
	// cancelled context.

	ctx2, cancel2 := context.WithCancel(ctx)
	cancel2()
	_, err = bdl.beforeBlockPut(ctx2, 10)
	require.Equal(t, ctx2.Err(), errors.Cause(err))
	require.Equal(t, 8*time.Second, lastDelay)
}

func TestBackpressureDiskLimiterSmallDisk(t *testing.T) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	var journalSize int64
	var otherSize int64 = 100
	var diskSize int64 = 200

	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, math.MaxInt64, 8*time.Second, delayFn,
		func() (int64, error) {
			return diskSize - otherSize - journalSize, nil
		})
	require.NoError(t, err)

	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, 0*time.Second, lastDelay)
		bdl.afterBlockPut(ctx, 10, true)
		journalSize += 10
	}

	for i := 1; i < 9; i++ {
		_, err := bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.InEpsilon(t, float64(i), lastDelay.Seconds(),
			0.01, "i=%d", i)
		bdl.afterBlockPut(ctx, 10, true)
		journalSize += 10
	}
}
