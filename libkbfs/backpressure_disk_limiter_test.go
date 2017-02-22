// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

// TestBackpressureTrackerCounters checks that the tracker's counters
// are updated properly for each public method.
func TestBackpressureTrackerCounters(t *testing.T) {
	bt, err := newBackpressureTracker(0.1, 0.9, 0.25, 100, 200)
	require.NoError(t, err)

	// semaphoreMax = min(k(U+F), L) = min(0.25(0+200), 100) = 50.
	require.Equal(t, int64(0), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(50), bt.semaphoreMax)
	require.Equal(t, int64(50), bt.semaphore.Count())

	// Increase U by 10, so that increases sM by 0.25*10 = 2.5, so
	// sM is now 52.

	avail := bt.onJournalEnable(10)
	require.Equal(t, int64(42), avail)

	require.Equal(t, int64(10), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(52), bt.semaphoreMax)
	require.Equal(t, int64(42), bt.semaphore.Count())

	// Decrease U by 9, so that decreases sM by 0.25*9 = 2.25, so
	// sM is back to 50.

	bt.onJournalDisable(9)

	require.Equal(t, int64(1), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(50), bt.semaphoreMax)
	require.Equal(t, int64(49), bt.semaphore.Count())

	// Increase U by 440, so that increases sM by 0.25*110 = 110,
	// so sM maxes out at 100, and semaphore should go negative.

	avail = bt.onJournalEnable(440)
	require.Equal(t, int64(-341), avail)

	require.Equal(t, int64(441), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(-341), bt.semaphore.Count())

	// Now revert that increase.

	bt.onJournalDisable(440)

	require.Equal(t, int64(1), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(50), bt.semaphoreMax)
	require.Equal(t, int64(49), bt.semaphore.Count())

	// This should be a no-op.
	avail = bt.onJournalEnable(0)
	require.Equal(t, int64(49), avail)

	require.Equal(t, int64(1), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(50), bt.semaphoreMax)
	require.Equal(t, int64(49), bt.semaphore.Count())

	// So should this.
	bt.onJournalDisable(0)

	require.Equal(t, int64(1), bt.used)
	require.Equal(t, int64(200), bt.free)
	require.Equal(t, int64(50), bt.semaphoreMax)
	require.Equal(t, int64(49), bt.semaphore.Count())

	// Add more free resources and put a block successfully.

	bt.updateFree(400)

	avail, err = bt.beforeBlockPut(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(89), avail)

	require.Equal(t, int64(1), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(89), bt.semaphore.Count())

	bt.afterBlockPut(10, true)

	require.Equal(t, int64(11), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(89), bt.semaphore.Count())

	// Then try to put a block but fail it.

	avail, err = bt.beforeBlockPut(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, int64(80), avail)

	require.Equal(t, int64(11), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(80), bt.semaphore.Count())

	bt.afterBlockPut(9, false)

	require.Equal(t, int64(11), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(89), bt.semaphore.Count())

	// Finally, delete a block.

	bt.onBlockDelete(11)

	require.Equal(t, int64(0), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(100), bt.semaphore.Count())

	// This should be a no-op.
	bt.onBlockDelete(0)

	require.Equal(t, int64(0), bt.used)
	require.Equal(t, int64(400), bt.free)
	require.Equal(t, int64(100), bt.semaphoreMax)
	require.Equal(t, int64(100), bt.semaphore.Count())
}

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
		log, 0.1, 0.9, 0.25, 100, 10, 8*time.Second, nil,
		func() (int64, int64, error) {
			return 0, 0, fakeErr
		})
	require.Equal(t, fakeErr, err)
}

// TestBackpressureDiskLimiterGetDelay tests the delay calculation,
// and makes sure it takes into account the context deadline.
func TestBackpressureDiskLimiterGetDelay(t *testing.T) {
	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 0.25, math.MaxInt64, math.MaxInt64,
		8*time.Second,
		func(ctx context.Context, delay time.Duration) error {
			return nil
		},
		func() (int64, int64, error) {
			return math.MaxInt64, math.MaxInt64, nil
		})
	require.NoError(t, err)

	now := time.Now()

	func() {
		bdl.lock.Lock()
		defer bdl.lock.Unlock()
		// byteDelayScale should be 25/(.25(350 + 25)) = 0.267.
		bdl.byteTracker.used = 25
		bdl.byteTracker.free = 350
		// fileDelayScale should by 50/(.25(350 + 50)) = 0.5.
		bdl.fileTracker.used = 50
		bdl.fileTracker.free = 350
	}()

	ctx := context.Background()
	delay := bdl.getDelayLocked(ctx, now)
	require.InEpsilon(t, float64(4), delay.Seconds(), 0.01)

	deadline := now.Add(5 * time.Second)
	ctx2, cancel2 := context.WithDeadline(ctx, deadline)
	defer cancel2()

	delay = bdl.getDelayLocked(ctx2, now)
	require.InEpsilon(t, float64(2), delay.Seconds(), 0.01)
}

type backpressureTestType int

const (
	byteTest backpressureTestType = iota
	fileTest
)

func (t backpressureTestType) String() string {
	switch t {
	case byteTest:
		return "byteTest"
	case fileTest:
		return "fileTest"
	default:
		return fmt.Sprintf("backpressureTestType(%d)", t)
	}
}

// testBackpressureDiskLimiterLargeDiskDelay checks the delays when
// pretending to have a large disk.
func testBackpressureDiskLimiterLargeDiskDelay(
	t *testing.T, testType backpressureTestType) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	// Set up parameters so that semaphoreMax always has value 100
	// when called in beforeBlockPut, and every block put (of size
	// 0.1 * 100 = 10) beyond the min threshold leads to an
	// increase in timeout of 1 second up to the max.

	const blockBytes = 100
	const blockFiles = 10
	var byteLimit, fileLimit int64
	switch testType {
	case byteTest:
		// Make bytes be the bottleneck.
		byteLimit = 10 * blockBytes
		fileLimit = 20 * blockFiles
	case fileTest:
		// Make files be the bottleneck.
		byteLimit = 20 * blockBytes
		fileLimit = 10 * blockFiles
	default:
		panic(fmt.Sprintf("unknown test type %s", testType))
	}

	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 0.25, byteLimit, fileLimit,
		8*time.Second, delayFn,
		func() (int64, int64, error) {
			return math.MaxInt64, math.MaxInt64, nil
		})
	require.NoError(t, err)

	byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
	require.Equal(t, bdlSnapshot{
		used:  0,
		free:  math.MaxInt64,
		max:   byteLimit,
		count: byteLimit,
	}, byteSnapshot)
	require.Equal(t, bdlSnapshot{
		used:  0,
		free:  math.MaxInt64,
		max:   fileLimit,
		count: fileLimit,
	}, fileSnapshot)

	ctx := context.Background()

	var bytesPut, filesPut int64

	checkCountersAfterBeforeBlockPut := func(i int) {
		byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
		require.Equal(t, bdlSnapshot{
			used:  bytesPut,
			free:  math.MaxInt64,
			max:   byteLimit,
			count: byteLimit - bytesPut - blockBytes,
		}, byteSnapshot, "i=%d", i)
		require.Equal(t, bdlSnapshot{
			used:  filesPut,
			free:  math.MaxInt64,
			max:   fileLimit,
			count: fileLimit - filesPut - blockFiles,
		}, fileSnapshot, "i=%d", i)
	}

	checkCountersAfterBlockPut := func(i int) {
		byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
		require.Equal(t, bdlSnapshot{
			used:  bytesPut,
			free:  math.MaxInt64,
			max:   byteLimit,
			count: byteLimit - bytesPut,
		}, byteSnapshot, "i=%d", i)
		require.Equal(t, bdlSnapshot{
			used:  filesPut,
			free:  math.MaxInt64,
			max:   fileLimit,
			count: fileLimit - filesPut,
		}, fileSnapshot, "i=%d", i)
	}

	// The first two puts shouldn't encounter any backpressure...

	for i := 0; i < 2; i++ {
		_, _, err = bdl.beforeBlockPut(ctx, blockBytes, blockFiles)
		require.NoError(t, err)
		require.Equal(t, 0*time.Second, lastDelay)
		checkCountersAfterBeforeBlockPut(i)

		bdl.afterBlockPut(ctx, blockBytes, blockFiles, true)
		bytesPut += blockBytes
		filesPut += blockFiles
		checkCountersAfterBlockPut(i)
	}

	// ...but the next eight should encounter increasing
	// backpressure...

	for i := 1; i < 9; i++ {
		_, _, err := bdl.beforeBlockPut(ctx, blockBytes, blockFiles)
		require.NoError(t, err)
		require.InEpsilon(t, float64(i), lastDelay.Seconds(),
			0.01, "i=%d", i)
		checkCountersAfterBeforeBlockPut(i)

		bdl.afterBlockPut(ctx, blockBytes, blockFiles, true)
		bytesPut += blockBytes
		filesPut += blockFiles
		checkCountersAfterBlockPut(i)
	}

	// ...and the last one should stall completely, if not for the
	// cancelled context.

	ctx2, cancel2 := context.WithCancel(ctx)
	cancel2()
	_, _, err = bdl.beforeBlockPut(ctx2, blockBytes, blockFiles)
	require.Equal(t, ctx2.Err(), errors.Cause(err))
	require.Equal(t, 8*time.Second, lastDelay)

	// This does the same thing as checkCountersAfterBlockPut(),
	// but only by coincidence; contrast with similar block in
	// TestBackpressureDiskLimiterSmallDisk below.
	byteSnapshot, fileSnapshot = bdl.getSnapshotsForTest()
	require.Equal(t, bdlSnapshot{
		used:  bytesPut,
		free:  math.MaxInt64,
		max:   byteLimit,
		count: byteLimit - bytesPut,
	}, byteSnapshot)
	require.Equal(t, bdlSnapshot{
		used:  filesPut,
		free:  math.MaxInt64,
		max:   fileLimit,
		count: fileLimit - filesPut,
	}, fileSnapshot)
}

func TestBackpressureDiskLimiterLargeDiskDelay(t *testing.T) {
	t.Run(byteTest.String(), func(t *testing.T) {
		testBackpressureDiskLimiterLargeDiskDelay(t, byteTest)
	})
	t.Run(fileTest.String(), func(t *testing.T) {
		testBackpressureDiskLimiterLargeDiskDelay(t, fileTest)
	})
}

// TestBackpressureDiskLimiterSmallDiskDelay checks the delays when
// pretending to have a small disk.
func testBackpressureDiskLimiterSmallDiskDelay(
	t *testing.T, testType backpressureTestType) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	// Set up parameters so that semaphoreMax always has value 80
	// when called in beforeBlockPut, and every block put (of size
	// 0.1 * 80 = 8) beyond the min threshold leads to an increase
	// in timeout of 1 second up to the max.

	const blockBytes = 80
	const blockFiles = 8
	var diskBytes, diskFiles int64
	switch testType {
	case byteTest:
		// Make bytes be the bottleneck.
		diskBytes = 10 * blockBytes
		diskFiles = 100 * blockFiles
	case fileTest:
		// Make files be the bottleneck.
		diskBytes = 100 * blockBytes
		diskFiles = 10 * blockFiles
	default:
		panic(fmt.Sprintf("unknown test type %s", testType))
	}

	var bdl *backpressureDiskLimiter

	getFreeBytesAndFilesFn := func() (int64, int64, error) {
		// When called for the first time from the
		// constructor, bdl will be nil.
		if bdl == nil {
			return diskBytes, diskFiles, nil
		}

		// When called in subsequent times from
		// beforeBlockPut, simulate the journal taking up
		// space.
		return diskBytes - bdl.byteTracker.used,
			diskFiles - bdl.fileTracker.used, nil
	}

	log := logger.NewTestLogger(t)
	bdl, err := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 0.25, math.MaxInt64, math.MaxInt64,
		8*time.Second, delayFn, getFreeBytesAndFilesFn)
	require.NoError(t, err)

	byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
	require.Equal(t, bdlSnapshot{
		used:  0,
		free:  diskBytes,
		max:   diskBytes / 4,
		count: diskBytes / 4,
	}, byteSnapshot)
	require.Equal(t, bdlSnapshot{
		used:  0,
		free:  diskFiles,
		max:   diskFiles / 4,
		count: diskFiles / 4,
	}, fileSnapshot)

	ctx := context.Background()

	var bytesPut, filesPut int64

	checkCountersAfterBeforeBlockPut := func(i int) {
		byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
		require.Equal(t, bdlSnapshot{
			used:  bytesPut,
			free:  diskBytes - bytesPut,
			max:   diskBytes / 4,
			count: diskBytes/4 - bytesPut/4,
		}, byteSnapshot, "i=%d", i)
		require.Equal(t, bdlSnapshot{
			used:  filesPut,
			free:  diskFiles - filesPut,
			max:   diskFiles / 4,
			count: diskFiles/4 - filesPut/4,
		}, fileSnapshot, "i=%d", i)
	}

	checkCountersAfterBlockPut := func(i int) {
		// freeBytes is only updated on beforeBlockPut, so we
		// have to compensate for that.
		byteSnapshot, fileSnapshot := bdl.getSnapshotsForTest()
		require.Equal(t, bdlSnapshot{
			used:  bytesPut,
			free:  diskBytes - bytesPut + blockBytes,
			max:   diskBytes/4 + blockBytes/4,
			count: diskBytes/4 + blockBytes/4 - bytesPut/4,
		}, byteSnapshot, "i=%d", i)
		require.Equal(t, bdlSnapshot{
			used:  filesPut,
			free:  diskFiles - filesPut + blockFiles,
			max:   diskFiles/4 + blockFiles/4,
			count: diskFiles/4 + blockFiles/4 - filesPut/4,
		}, fileSnapshot, "i=%d", i)
	}

	// The first two puts shouldn't encounter any backpressure...

	for i := 0; i < 2; i++ {
		_, _, err = bdl.beforeBlockPut(ctx, blockBytes, blockFiles)
		require.NoError(t, err)
		require.Equal(t, 0*time.Second, lastDelay)
		checkCountersAfterBeforeBlockPut(i)

		bdl.afterBlockPut(ctx, blockBytes, blockFiles, true)
		bytesPut += blockBytes
		filesPut += blockFiles
		checkCountersAfterBlockPut(i)
	}

	// ...but the next eight should encounter increasing
	// backpressure...

	for i := 1; i < 9; i++ {
		_, _, err := bdl.beforeBlockPut(ctx, blockBytes, blockFiles)
		require.NoError(t, err)
		require.InEpsilon(t, float64(i), lastDelay.Seconds(),
			0.01, "i=%d", i)
		checkCountersAfterBeforeBlockPut(i)

		bdl.afterBlockPut(ctx, blockBytes, blockFiles, true)
		bytesPut += blockBytes
		filesPut += blockFiles
		checkCountersAfterBlockPut(i)
	}

	// ...and the last one should stall completely, if not for the
	// cancelled context.

	ctx2, cancel2 := context.WithCancel(ctx)
	cancel2()
	_, _, err = bdl.beforeBlockPut(ctx2, blockBytes, blockFiles)
	require.Equal(t, ctx2.Err(), errors.Cause(err))
	require.Equal(t, 8*time.Second, lastDelay)

	byteSnapshot, fileSnapshot = bdl.getSnapshotsForTest()
	require.Equal(t, bdlSnapshot{
		used:  bytesPut,
		free:  diskBytes - bytesPut,
		max:   diskBytes / 4,
		count: diskBytes/4 - bytesPut,
	}, byteSnapshot)
	require.Equal(t, bdlSnapshot{
		used:  filesPut,
		free:  diskFiles - filesPut,
		max:   diskFiles / 4,
		count: diskFiles/4 - filesPut,
	}, fileSnapshot)
}

func TestBackpressureDiskLimiterSmallDiskDelay(t *testing.T) {
	t.Run(byteTest.String(), func(t *testing.T) {
		testBackpressureDiskLimiterSmallDiskDelay(t, byteTest)
	})
	t.Run(fileTest.String(), func(t *testing.T) {
		testBackpressureDiskLimiterSmallDiskDelay(t, fileTest)
	})
}
