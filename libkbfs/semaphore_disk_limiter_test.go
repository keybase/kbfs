// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSemaphoreDiskLimiterOnUpdateAvailableBytes(t *testing.T) {
	s := newSemaphoreDiskLimiter(2, 100)
	require.Equal(t, int64(100), s.getByteLimit())
	s.onUpdateAvailableBytes(199)
	require.Equal(t, int64(99), s.getByteLimit())
	s.onUpdateAvailableBytes(201)
	require.Equal(t, int64(100), s.getByteLimit())
	s.onUpdateAvailableBytes(math.MaxUint64)
	require.Equal(t, int64(100), s.getByteLimit())

	s.onUpdateAvailableBytes(0)
	require.Equal(t, int64(0), s.getByteLimit())
	s.onJournalEnable(50)
	require.Equal(t, int64(25), s.getByteLimit())
	s.onJournalDisable(50)
	require.Equal(t, int64(0), s.getByteLimit())
}
