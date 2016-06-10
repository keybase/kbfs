// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/require"
)

func TestBserverTlfStorageBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "bserver_tlf_storage")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		if err != nil {
			t.Log("error removing %s: %s", tempdir, err)
		}
	}()

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	s := makeBserverTlfStorage(codec, &crypto, tempdir)

	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	// Put the block.
	err = s.putData(bID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Make sure we get the same block back.
	buf, key, err := s.getData(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Add a reference.
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	s.addReference(bID, bCtx2)

	// Make sure we get the same block via that reference.
	buf, key, err = s.getData(bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)
}
