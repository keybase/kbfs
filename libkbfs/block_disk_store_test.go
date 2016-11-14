// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBlockDiskStoreTest(t *testing.T) (tempdir string, s *blockDiskStore) {
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_disk_store")
	require.NoError(t, err)

	// Clean up the tempdir if the rest of the setup fails.
	setupSucceeded := false
	defer func() {
		if !setupSucceeded {
			err := os.RemoveAll(tempdir)
			assert.NoError(t, err)
		}
	}()

	s = makeBlockDiskStore(codec, crypto, tempdir)

	setupSucceeded = true
	return tempdir, s
}

func teardownBlockDiskStoreTest(t *testing.T, tempdir string, s *blockDiskStore) {
	err := os.RemoveAll(tempdir)
	assert.NoError(t, err)
}

func putBlockDiskData(
	t *testing.T, s *blockDiskStore, data []byte) (
	BlockID, BlockContext, kbfscrypto.BlockCryptKeyServerHalf) {
	bID, err := s.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", ZeroBlockRefNonce}
	serverHalf, err := s.crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	err = s.putData(bID, bCtx, data, serverHalf, "")
	require.NoError(t, err)

	return bID, bCtx, serverHalf
}

func addBlockDiskRef(
	t *testing.T, s *blockDiskStore, bID BlockID) BlockContext {
	nonce, err := s.crypto.MakeBlockRefNonce()
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = s.addReference(bID, bCtx2, "")
	require.NoError(t, err)
	return bCtx2
}

func getAndCheckBlockDiskData(t *testing.T, s *blockDiskStore,
	bID BlockID, bCtx BlockContext, expectedData []byte,
	expectedServerHalf kbfscrypto.BlockCryptKeyServerHalf) {
	data, serverHalf, err := s.getDataWithContext(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, expectedData, data)
	require.Equal(t, expectedServerHalf, serverHalf)
}

func TestBlockDiskStoreBasic(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(t, s, data)

	// Make sure we get the same block back.
	getAndCheckBlockDiskData(t, s, bID, bCtx, data, serverHalf)

	// Add a reference.
	bCtx2 := addBlockDiskRef(t, s, bID)

	// Make sure we get the same block via that reference.
	getAndCheckBlockDiskData(t, s, bID, bCtx2, data, serverHalf)

	// Shutdown and restart.
	s = makeBlockDiskStore(s.codec, s.crypto, tempdir)

	// Make sure we get the same block for both refs.

	getAndCheckBlockDiskData(t, s, bID, bCtx, data, serverHalf)
	getAndCheckBlockDiskData(t, s, bID, bCtx2, data, serverHalf)
}

func TestBlockDiskStoreAddReference(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	data := []byte{1, 2, 3, 4}
	bID, err := s.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Add a reference, which should succeed.
	bCtx := addBlockDiskRef(t, s, bID)

	// Of course, the block get should still fail.
	_, _, err = s.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)
}

func TestBlockDiskStoreArchiveReferences(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(t, s, data)

	// Add a reference.
	bCtx2 := addBlockDiskRef(t, s, bID)

	// Archive references.
	err := s.archiveReferences(
		map[BlockID][]BlockContext{bID: {bCtx, bCtx2}}, "")
	require.NoError(t, err)

	// Get block should still succeed.
	getAndCheckBlockDiskData(t, s, bID, bCtx, data, serverHalf)
}

func TestBlockDiskStoreArchiveNonExistentReference(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	uid1 := keybase1.MakeTestUID(1)

	bCtx := BlockContext{uid1, "", ZeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := s.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	// Archive references.
	err = s.archiveReferences(map[BlockID][]BlockContext{bID: {bCtx}}, "")
	require.NoError(t, err)
}

func TestBlockDiskStoreRemoveReferences(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockDiskData(t, s, data)

	// Add a reference.
	bCtx2 := addBlockDiskRef(t, s, bID)

	// Remove references.
	liveCount, err := s.removeReferences(
		bID, []BlockContext{bCtx, bCtx2}, "")
	require.NoError(t, err)
	require.Equal(t, 0, liveCount)

	// Make sure the block data is inaccessible.
	_, _, err = s.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)

	// But the actual data should remain.
	buf, half, err := s.getData(bID)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, half)
}
