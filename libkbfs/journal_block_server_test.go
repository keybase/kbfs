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

func setupJournalBlockServerTest(t *testing.T) (
	tempdir string, config Config, jServer *JournalServer) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "journal_block_server")
	require.NoError(t, err)
	config = MakeTestConfigOrBust(t, "test_user")
	log := config.MakeLogger("")
	jServer = makeJournalServer(
		config, log, tempdir, config.BlockServer(), config.MDOps())
	config.SetBlockServer(jServer.blockServer())
	config.SetMDOps(jServer.mdOps())
	return tempdir, config, jServer
}

func teardownJournalBlockServerTest(
	t *testing.T, tempdir string, config Config) {
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
	CheckConfigAndShutdown(t, config)
}

type shutdownOnlyBlockServer struct{ BlockServer }

func (shutdownOnlyBlockServer) Shutdown() {}

func TestJournalBlockServerPutGetAddReference(t *testing.T) {
	tempdir, config, jServer := setupJournalBlockServerTest(t)
	defer teardownJournalBlockServerTest(t, tempdir, config)

	// Use a shutdown-only BlockServer so that it errors if the
	// journal tries to access it.
	jServer.delegateBlockServer = shutdownOnlyBlockServer{}

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(tlfID)
	require.NoError(t, err)

	ctx := context.Background()
	blockServer := config.BlockServer()
	crypto := config.Crypto()

	uid1 := keybase1.MakeTestUID(1)
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

func TestJournalBlockServerRemoveBlockReferences(t *testing.T) {
	tempdir, config, jServer := setupJournalBlockServerTest(t)
	defer teardownJournalBlockServerTest(t, tempdir, config)

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(tlfID)
	require.NoError(t, err)

	ctx := context.Background()
	blockServer := config.BlockServer()
	crypto := config.Crypto()

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Put a block.
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, bID, tlfID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Add some references.

	uid2 := keybase1.MakeTestUID(2)
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx2)

	require.NoError(t, err)
	nonce2, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx3 := BlockContext{uid1, uid2, nonce2}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx3)
	require.NoError(t, err)

	// Remove the references, including a non-existent one, but
	// leave one.
	nonce3, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx4 := BlockContext{uid1, uid2, nonce3}
	liveCounts, err := blockServer.RemoveBlockReferences(
		ctx, tlfID, map[BlockID][]BlockContext{
			bID: {bCtx, bCtx2, bCtx4},
		})
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 1}, liveCounts)
}

func TestJournalBlockServerArchiveBlockReferences(t *testing.T) {
	tempdir, config, jServer := setupJournalBlockServerTest(t)
	defer teardownJournalBlockServerTest(t, tempdir, config)

	// Use a shutdown-only BlockServer so that it errors if the
	// journal tries to access it.
	jServer.delegateBlockServer = shutdownOnlyBlockServer{}

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(tlfID)
	require.NoError(t, err)

	ctx := context.Background()
	blockServer := config.BlockServer()
	crypto := config.Crypto()

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Put a block.
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, bID, tlfID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Add a reference.
	uid2 := keybase1.MakeTestUID(2)
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx2)

	// Archive the references.
	require.NoError(t, err)
	err = blockServer.ArchiveBlockReferences(
		ctx, tlfID, map[BlockID][]BlockContext{
			bID: {bCtx, bCtx2},
		})
	require.NoError(t, err)
}

func TestJournalBlockServerFlush(t *testing.T) {
	tempdir, config, jServer := setupJournalBlockServerTest(t)
	defer teardownJournalBlockServerTest(t, tempdir, config)

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(tlfID)
	require.NoError(t, err)

	ctx := context.Background()
	blockServer := config.BlockServer()
	crypto := config.Crypto()

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Put a block.
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, bID, tlfID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Add some references.

	uid2 := keybase1.MakeTestUID(2)
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx2)

	require.NoError(t, err)
	nonce2, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx3 := BlockContext{uid1, uid2, nonce2}
	err = blockServer.AddBlockReference(ctx, bID, tlfID, bCtx3)
	require.NoError(t, err)

	// Remove some references.
	liveCounts, err := blockServer.RemoveBlockReferences(
		ctx, tlfID, map[BlockID][]BlockContext{
			bID: {bCtx, bCtx2},
		})
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 1}, liveCounts)

	// Archive the rest.
	require.NoError(t, err)
	err = blockServer.ArchiveBlockReferences(
		ctx, tlfID, map[BlockID][]BlockContext{
			bID: {bCtx3},
		})
	require.NoError(t, err)

	// Then remove them.
	require.NoError(t, err)
	liveCounts, err = blockServer.RemoveBlockReferences(
		ctx, tlfID, map[BlockID][]BlockContext{
			bID: {bCtx3},
		})
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 0}, liveCounts)

	oldBlockServer := jServer.delegateBlockServer
	bundle, ok := jServer.getBundle(tlfID)
	require.True(t, ok)

	// Flush the block put.

	log := config.MakeLogger("")
	flushed, err := bundle.blockJournal.flushOne(
		ctx, oldBlockServer, tlfID, log)
	require.NoError(t, err)
	require.True(t, flushed)

	buf, key, err := oldBlockServer.Get(ctx, bID, tlfID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the reference add.

	flushed, err = bundle.blockJournal.flushOne(
		ctx, oldBlockServer, tlfID, log)
	require.NoError(t, err)
	require.True(t, flushed)

	buf, key, err = oldBlockServer.Get(ctx, bID, tlfID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// TODO: Flush everything else.
}
