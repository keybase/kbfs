// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
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
}
