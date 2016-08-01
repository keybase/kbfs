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
	"golang.org/x/net/context"
)

// Test that putting a block, and getting it back, works.
func TestJournalBlockServerPutAndGet(t *testing.T) {
	// setup
	tempdir, err := ioutil.TempDir(os.TempDir(), "journal_block_server")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	config := MakeTestConfigOrBust(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	log := config.MakeLogger("")
	jServer := makeJournalServer(
		config, log, tempdir, config.BlockServer(), config.MDOps())
	config.SetBlockServer(jServer.blockServer())
	config.SetMDOps(jServer.mdOps())

	ctx := context.Background()
	blockServer := config.BlockServer()
	crypto := config.Crypto()

	uid1 := keybase1.MakeTestUID(1)
	tlfID := FakeTlfID(2, false)
	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Put a block.
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, bID, tlfID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Now get the same block back.
	buf, key, err := blockServer.Get(ctx, bID, tlfID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Add a reference.
	uid2 := keybase1.MakeTestUID(2)
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx2)
	require.NoError(t, err)

	// Now get the same block back.
	buf, key, err = blockServer.Get(ctx, bID, tlfID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)
}
