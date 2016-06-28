// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/stretchr/testify/require"
)

// TestMDServerTlfStorageBasic copies TestMDServerBasics, but for a
// single mdServerTlfStorage.
func TestMDServerTlfStorageBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	clock := newTestClockNow()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_storage")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s, err := makeMDServerTlfStorage(codec, clock, crypto, tempdir)
	require.NoError(t, err)
	defer s.shutdown()

	bid := FakeBranchID(1)

	ctx := context.Background()
	var kbpki KBPKI
	head, err := s.getForTLF(ctx, kbpki, bid, Unmerged)
	require.NoError(t, err)
	require.Nil(t, head)
}
