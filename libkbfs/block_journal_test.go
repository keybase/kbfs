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
	"github.com/stretchr/testify/require"
)

func getBlockJournalLength(t *testing.T, j *blockJournal) int {
	len, err := j.length()
	require.NoError(t, err)
	return int(len)
}

func TestBlockJournalBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	ctx := context.Background()

	log := logger.NewTestLogger(t)
	j, err := makeBlockJournal(ctx, codec, crypto, tempdir, log)
	require.NoError(t, err)
	defer j.shutdown()

	require.Equal(t, 0, getBlockJournalLength(t, j))

	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	// Put the block.
	err = j.putData(
		ctx, MetadataRevisionUninitialized, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.Equal(t, 1, getBlockJournalLength(t, j))

	// Make sure we get the same block back.
	buf, key, err := j.getDataWithContext(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Add a reference.
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = j.addReference(ctx, MetadataRevisionUninitialized, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, 2, getBlockJournalLength(t, j))

	// Make sure we get the same block via that reference.
	buf, key, err = j.getDataWithContext(bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Shutdown and restart.
	j.shutdown()
	j, err = makeBlockJournal(ctx, codec, crypto, tempdir, log)
	require.NoError(t, err)

	require.Equal(t, 2, getBlockJournalLength(t, j))

	// Make sure we get the same block for both refs.

	buf, key, err = j.getDataWithContext(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	buf, key, err = j.getDataWithContext(bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)
}

func TestBlockJournalRemoveReferences(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	ctx := context.Background()

	log := logger.NewTestLogger(t)
	j, err := makeBlockJournal(ctx, codec, crypto, tempdir, log)
	require.NoError(t, err)
	defer j.shutdown()

	require.Equal(t, 0, getBlockJournalLength(t, j))

	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	// Put the block.
	err = j.putData(
		ctx, MetadataRevisionUninitialized, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.Equal(t, 1, getBlockJournalLength(t, j))

	// Add a reference.
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = j.addReference(ctx, MetadataRevisionUninitialized, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, 2, getBlockJournalLength(t, j))

	// Remove references.
	liveCounts, err := j.removeReferences(
		ctx, MetadataRevisionUninitialized,
		map[BlockID][]BlockContext{bID: {bCtx, bCtx2}}, true)
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 0}, liveCounts)
	require.Equal(t, 3, getBlockJournalLength(t, j))

	// Add reference back, which should error.
	err = j.addReference(ctx, MetadataRevisionUninitialized, bID, bCtx2)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)
	require.Equal(t, 3, getBlockJournalLength(t, j))
}

func TestBlockJournalArchiveReferences(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	ctx := context.Background()

	log := logger.NewTestLogger(t)
	j, err := makeBlockJournal(ctx, codec, crypto, tempdir, log)
	require.NoError(t, err)
	defer j.shutdown()

	require.Equal(t, 0, getBlockJournalLength(t, j))

	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}

	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	// Put the block.
	err = j.putData(
		ctx, MetadataRevisionUninitialized, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.Equal(t, 1, getBlockJournalLength(t, j))

	// Add a reference.
	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	err = j.addReference(ctx, MetadataRevisionUninitialized, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, 2, getBlockJournalLength(t, j))

	// Archive references.
	err = j.archiveReferences(
		ctx, MetadataRevisionUninitialized,
		map[BlockID][]BlockContext{bID: {bCtx, bCtx2}})
	require.NoError(t, err)
	require.Equal(t, 3, getBlockJournalLength(t, j))

	// Add reference back, which should error.
	err = j.addReference(ctx, MetadataRevisionUninitialized, bID, bCtx2)
	require.IsType(t, BServerErrorBlockArchived{}, err)
	require.Equal(t, 3, getBlockJournalLength(t, j))
}

func TestBlockJournalFlush(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	ctx := context.Background()

	log := logger.NewTestLogger(t)
	j, err := makeBlockJournal(ctx, codec, crypto, tempdir, log)
	require.NoError(t, err)
	defer j.shutdown()

	// Put a block.

	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)

	bCtx := BlockContext{uid1, "", zeroBlockRefNonce}

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	rev := MetadataRevisionInitial
	err = j.putData(ctx, rev, bID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Add some references.

	nonce, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx2 := BlockContext{uid1, uid2, nonce}
	rev++
	err = j.addReference(ctx, rev, bID, bCtx2)

	require.NoError(t, err)
	nonce2, err := crypto.MakeBlockRefNonce()
	require.NoError(t, err)
	bCtx3 := BlockContext{uid1, uid2, nonce2}
	rev++
	err = j.addReference(ctx, rev, bID, bCtx3)
	require.NoError(t, err)

	// Remove some references.

	rev++
	liveCounts, err := j.removeReferences(
		ctx, rev, map[BlockID][]BlockContext{
			bID: {bCtx, bCtx2},
		}, false)
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 1}, liveCounts)

	// Archive the rest.

	rev++
	err = j.archiveReferences(
		ctx, rev, map[BlockID][]BlockContext{
			bID: {bCtx3},
		})
	require.NoError(t, err)

	// Then remove them.

	rev++
	liveCounts, err = j.removeReferences(
		ctx, rev, map[BlockID][]BlockContext{
			bID: {bCtx3},
		}, false)
	require.NoError(t, err)
	require.Equal(t, map[BlockID]int{bID: 0}, liveCounts)

	blockServer := NewBlockServerMemory(newTestBlockServerLocalConfig(t))

	tlfID := FakeTlfID(1, false)

	nextExpectedRev := MetadataRevisionInitial
	flush := func() {
		o, e, data, serverHalf, err := j.getNextEntryToFlush(ctx)
		require.NoError(t, err)
		require.NotNil(t, e)
		require.Equal(t, nextExpectedRev, e.HeadRevision)
		err = flushBlockJournalEntry(
			ctx, log, blockServer, tlfID, *e, data, serverHalf)
		require.NoError(t, err)
		err = j.removeFlushedEntry(ctx, o, *e)
		require.NoError(t, err)
		nextExpectedRev++
	}

	// Flush the block put.

	flush()

	buf, key, err := blockServer.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the reference adds.

	flush()

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	flush()

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the reference removals.

	flush()

	_, _, err = blockServer.Get(ctx, tlfID, bID, bCtx)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	_, _, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the reference archival.

	flush()

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the last removal.

	flush()

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	_, e, _, _, err := j.getNextEntryToFlush(ctx)
	require.NoError(t, err)
	require.Nil(t, e)
}
