// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"path/filepath"
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

func putBlockDisk(
	t *testing.T, s *blockDiskStore, data []byte) (
	bool, BlockID, BlockContext, kbfscrypto.BlockCryptKeyServerHalf) {
	bID, err := s.crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	bCtx := BlockContext{uid1, "", ZeroBlockRefNonce}
	serverHalf, err := s.crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	didPut, err := s.put(bID, bCtx, data, serverHalf, "")
	require.NoError(t, err)

	return didPut, bID, bCtx, serverHalf
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
	_, bID, bCtx, serverHalf := putBlockDisk(t, s, data)

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
	_, bID, bCtx, serverHalf := putBlockDisk(t, s, data)

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
	_, bID, bCtx, serverHalf := putBlockDisk(t, s, data)

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

func TestBlockDiskStoreRemove(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	_, bID, bCtx, _ := putBlockDisk(t, s, data)

	// Should not be removable.
	err := s.remove(bID)
	require.Error(t, err, "Trying to remove data")

	// Remove reference.
	liveCount, err := s.removeReferences(bID, []BlockContext{bCtx}, "")
	require.NoError(t, err)
	require.Equal(t, 0, liveCount)

	// Should now be removable.
	err = s.remove(bID)
	require.NoError(t, err)

	_, _, err = s.getData(bID)
	require.Equal(t, blockNonExistentError{bID}, err)

	err = filepath.Walk(s.dir,
		func(path string, info os.FileInfo, _ error) error {
			// We should only find the blocks directory here.
			if path != s.dir {
				t.Errorf("Found unexpected block path: %s", path)
			}
			return nil
		})
	require.NoError(t, err)
}

func TestBlockDiskStoreTotalDataSize(t *testing.T) {
	tempdir, s := setupBlockDiskStoreTest(t)
	defer teardownBlockDiskStoreTest(t, tempdir, s)

	requireSize := func(expectedSize int) {
		totalSize, err := s.getTotalDataSize()
		require.NoError(t, err)
		require.Equal(t, int64(expectedSize), totalSize)
	}

	requireSize(0)

	data1 := []byte{1, 2, 3, 4}
	_, bID1, bCtx1, _ := putBlockDisk(t, s, data1)

	requireSize(len(data1))

	data2 := []byte{1, 2, 3, 4, 5}
	_, bID2, bCtx2, _ := putBlockDisk(t, s, data2)

	expectedSize := len(data1) + len(data2)
	requireSize(expectedSize)

	// Adding, archive, or removing references shouldn't change
	// anything.

	bCtx1b := addBlockDiskRef(t, s, bID1)
	requireSize(expectedSize)

	err := s.archiveReferences(map[BlockID][]BlockContext{bID2: {bCtx2}}, "")
	require.NoError(t, err)
	requireSize(expectedSize)

	liveCount, err := s.removeReferences(
		bID1, []BlockContext{bCtx1, bCtx1b}, "")
	require.NoError(t, err)
	require.Equal(t, 0, liveCount)
	requireSize(expectedSize)

	liveCount, err = s.removeReferences(
		bID2, []BlockContext{bCtx2, bCtx2}, "")
	require.NoError(t, err)
	require.Equal(t, 0, liveCount)
	requireSize(expectedSize)

	// Removing blocks should cause the total size to change.

	err = s.remove(bID1)
	require.NoError(t, err)
	requireSize(len(data2))

	err = s.remove(bID2)
	require.NoError(t, err)
	requireSize(0)
}
