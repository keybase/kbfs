// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"
	"testing"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestBackpressureDiskLimiterLargeDisk(t *testing.T) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	log := logger.NewTestLogger(t)
	bdl := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, 100, 8*time.Second, delayFn,
		func() (int64, error) {
			return math.MaxInt64, nil
		})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, 0*time.Second, lastDelay)
		bdl.afterBlockPut(ctx, 10, true)
	}

	for i := 1; i < 9; i++ {
		_, err := bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.InEpsilon(t, float64(i), lastDelay.Seconds(),
			0.01, "i=%d", i)
		bdl.afterBlockPut(ctx, 10, true)
	}

	_, err := bdl.beforeBlockPut(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 8*time.Second, lastDelay)
	bdl.afterBlockPut(ctx, 10, true)
}

func TestBackpressureDiskLimiterSmallDisk(t *testing.T) {
	var lastDelay time.Duration
	delayFn := func(ctx context.Context, delay time.Duration) error {
		lastDelay = delay
		return nil
	}

	var journalSize int64
	var diskSize int64 = 100

	log := logger.NewTestLogger(t)
	bdl := newBackpressureDiskLimiterWithFunctions(
		log, 0.1, 0.9, math.MaxInt64, 8*time.Second, delayFn,
		func() (int64, error) {
			return diskSize - journalSize, nil
		})
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

	_, err := bdl.beforeBlockPut(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 8*time.Second, lastDelay)
	bdl.afterBlockPut(ctx, 10, true)
	journalSize += 10
}
