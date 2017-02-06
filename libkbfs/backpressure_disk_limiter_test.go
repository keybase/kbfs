// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
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
	bdl := newBackpressureDiskLimiterWithDelayFunction(
<<<<<<< 7e9c43bfafa34333b4e3bc6c4142bb360189a2d3
		log, 0.1, 0.9, 110, 9*time.Second, delayFn)
||||||| merged common ancestors
		0.1, 0.9, 110, 9*time.Second, delayFn)
=======
		0.1, 0.9, 100, 9*time.Second, delayFn)
>>>>>>> Fail test
	ctx := context.Background()
	_, err := bdl.beforeBlockPut(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 0*time.Second, lastDelay)

	for i := 0; i < 9; i++ {
		_, err = bdl.beforeBlockPut(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, time.Duration(i)*time.Second, lastDelay)
	}

	_, err = bdl.beforeBlockPut(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 9*time.Second, lastDelay)
}
