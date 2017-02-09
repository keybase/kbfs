// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/kbfs/ioutil"
	"github.com/stretchr/testify/require"
)

// TestDiskLimits checks that getDiskLimits() doesn't return an error,
// and returns a non-zero value. This assumes that the partition with
// the root directory actually has free space, which may fail in
// certain weird configs.
func TestDiskLimits(t *testing.T) {
	availableBytes, err := getDiskLimits("/")
	require.NoError(t, err)
	require.NotEqual(t, uint64(0), availableBytes)
}

// TestDiskLimitsNonExistentFile checks that getDiskLimits() returns
// an error for which ioutil.IsNotExist holds if its given file
// doesn't exist.
func TestDiskLimitsNonExistentFile(t *testing.T) {
	// Of course, we're assuming this file doesn't exist.
	_, err := getDiskLimits("/non-existent-file")
	require.True(t, ioutil.IsNotExist(err))
}

func BenchmarkDiskLimits(b *testing.B) {
	for i := 0; i < b.N; i++ {
		getDiskLimits("/")
	}
}
