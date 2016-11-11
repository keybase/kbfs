// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBlockDiskStoreTest(t *testing.T) (
	ctx context.Context, tempdir string, j *blockDiskStore) {
	ctx = context.Background()
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	log := logger.NewTestLogger(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)

	// Clean up the tempdir if the rest of the setup fails.
	setupSucceeded := false
	defer func() {
		if !setupSucceeded {
			err := os.RemoveAll(tempdir)
			assert.NoError(t, err)
		}
	}()

	j = makeBlockDiskStore(ctx, codec, crypto, tempdir, log)

	setupSucceeded = true
	return ctx, tempdir, j
}

func teardownBlockDiskStoreTest(t *testing.T, tempdir string, j *blockDiskStore) {
	err := os.RemoveAll(tempdir)
	assert.NoError(t, err)
}

func putBlockDiskData(
	ctx context.Context, t *testing.T, j *blockDiskStore, data []byte) (
	BlockID, BlockContext, kbfscrypto.BlockCryptKeyServerHalf) {
	bID, err := j.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", ZeroBlockRefNonce}
	serverHalf, err := j.crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	err = j.putData(ctx, bID, bCtx, data, serverHalf, nil)
	require.NoError(t, err)

	return bID, bCtx, serverHalf
}

func addBlockDiskRef(
	ctx context.Context, t *testing.T, j *blockDiskStore,
	bID BlockID) BlockContext {
	nonce, err := j.crypto.MakeBlockRefNonce()
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = j.addReference(ctx, bID, bCtx2, nil)
	require.NoError(t, err)
	return bCtx2
}

func getAndCheckBlockDiskData(ctx context.Context, t *testing.T, j *blockDiskStore,
	bID BlockID, bCtx BlockContext, expectedData []byte,
	expectedServerHalf kbfscrypto.BlockCryptKeyServerHalf) {
	data, serverHalf, err := j.getDataWithContext(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, expectedData, data)
	require.Equal(t, expectedServerHalf, serverHalf)
}

func TestBlockDiskStoreBasic(t *testing.T) {
	ctx, tempdir, j := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(ctx, t, j, data)

	// Make sure we get the same block back.
	getAndCheckBlockDiskData(ctx, t, j, bID, bCtx, data, serverHalf)

	// Add a reference.
	bCtx2 := addBlockDiskRef(ctx, t, j, bID)

	// Make sure we get the same block via that reference.
	getAndCheckBlockDiskData(ctx, t, j, bID, bCtx2, data, serverHalf)

	// Shutdown and restart.
	j = makeBlockDiskStore(ctx, j.codec, j.crypto, tempdir, j.log)

	// Make sure we get the same block for both refs.

	getAndCheckBlockDiskData(ctx, t, j, bID, bCtx, data, serverHalf)
	getAndCheckBlockDiskData(ctx, t, j, bID, bCtx2, data, serverHalf)
}

func TestBlockDiskStoreAddReference(t *testing.T) {
	ctx, tempdir, j := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, j)

	data := []byte{1, 2, 3, 4}
	bID, err := j.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Add a reference, which should succeed.
	bCtx := addBlockDiskRef(ctx, t, j, bID)

	// Of course, the block get should still fail.
	_, _, err = j.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)
}

func TestBlockDiskStoreRemoveReferences(t *testing.T) {
	ctx, tempdir, j := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(ctx, t, j, data)

	// Add a reference.
	bCtx2 := addBlockDiskRef(ctx, t, j, bID)

	// Remove references.
	liveCounts, err := j.removeReferences(
		ctx, map[BlockID][]BlockContext{bID: {bCtx, bCtx2}})
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 0}, liveCounts)

	// Make sure the block data is inaccessible.
	_, _, err = j.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)

	// But the actual data should remain (for flushing).
	buf, half, err := j.getData(bID)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, half)
}

func TestBlockDiskStoreArchiveReferences(t *testing.T) {
	ctx, tempdir, j := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(ctx, t, j, data)

	// Add a reference.
	bCtx2 := addBlockDiskRef(ctx, t, j, bID)

	// Archive references.
	err := j.archiveReferences(
		ctx, map[BlockID][]BlockContext{bID: {bCtx, bCtx2}}, nil)
	require.NoError(t, err)

	// Get block should still succeed.
	getAndCheckBlockDiskData(ctx, t, j, bID, bCtx, data, serverHalf)
}

func TestBlockDiskStoreArchiveNonExistentReference(t *testing.T) {
	ctx, tempdir, j := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, j)

	uid1 := keybase1.MakeTestUID(1)

	bCtx := BlockContext{uid1, "", ZeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := j.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Archive references.
	err = j.archiveReferences(
		ctx, map[BlockID][]BlockContext{bID: {bCtx}}, nil)
	require.NoError(t, err)
}
