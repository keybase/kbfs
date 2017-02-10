// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type blockJournalEntryFuture struct {
	blockJournalEntry
	kbfscodec.Extra
}

func (ef blockJournalEntryFuture) toCurrent() blockJournalEntry {
	return ef.blockJournalEntry
}

func (ef blockJournalEntryFuture) ToCurrentStruct() kbfscodec.CurrentStruct {
	return ef.toCurrent()
}

func makeFakeBlockJournalEntryFuture(t *testing.T) blockJournalEntryFuture {
	ef := blockJournalEntryFuture{
		blockJournalEntry{
			blockPutOp,
			kbfsblock.ContextMap{
				kbfsblock.FakeID(1): {
					makeFakeBlockContext(t),
					makeFakeBlockContext(t),
					makeFakeBlockContext(t),
				},
			},
			MetadataRevisionInitial,
			false,
			false,
			codec.UnknownFieldSetHandler{},
		},
		kbfscodec.MakeExtraOrBust("blockJournalEntry", t),
	}
	return ef
}

func TestBlockJournalEntryUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeBlockJournalEntryFuture(t))
}

func TestSaturateAdd(t *testing.T) {
	var x int64
	saturateAdd(&x, math.MaxInt64-1)
	require.Equal(t, int64(math.MaxInt64-1), x)
	saturateAdd(&x, math.MaxInt64-1)
	require.Equal(t, int64(math.MaxInt64), x)
	saturateAdd(&x, math.MinInt64+2)
	require.Equal(t, int64(1), x)
	saturateAdd(&x, math.MinInt64)
	require.Equal(t, int64(0), x)

	x = math.MinInt64
	saturateAdd(&x, math.MinInt64)
	require.Equal(t, int64(0), x)

	x = -1
	saturateAdd(&x, math.MinInt64)
	require.Equal(t, int64(0), x)

	x = -1
	saturateAdd(&x, 5)
	require.Equal(t, int64(5), x)

	x = -1
	saturateAdd(&x, 0)
	require.Equal(t, int64(0), x)
}

func getBlockJournalLength(t *testing.T, j *blockJournal) int {
	len, err := j.length()
	require.NoError(t, err)
	return int(len)
}

func setupBlockJournalTest(t *testing.T) (
	ctx context.Context, cancel context.CancelFunc, tempdir string,
	log logger.Logger, j *blockJournal) {
	codec := kbfscodec.NewMsgpack()
	log = logger.NewTestLogger(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "block_journal")
	require.NoError(t, err)

	// Clean up the tempdir if the rest of the setup fails.
	setupSucceeded := false
	defer func() {
		if !setupSucceeded {
			err := ioutil.RemoveAll(tempdir)
			assert.NoError(t, err)
		}
	}()

	ctx, cancel = context.WithTimeout(
		context.Background(), individualTestTimeout)

	// Clean up the context if the rest of the setup fails.
	defer func() {
		if !setupSucceeded {
			cancel()
		}
	}()

	j, err = makeBlockJournal(ctx, codec, tempdir, log)
	require.NoError(t, err)
	require.Equal(t, 0, getBlockJournalLength(t, j))

	setupSucceeded = true
	return ctx, cancel, tempdir, log, j
}

func teardownBlockJournalTest(t *testing.T, ctx context.Context,
	cancel context.CancelFunc, tempdir string, j *blockJournal) {
	cancel()

	err := j.checkInSyncForTest()
	assert.NoError(t, err)

	err = ioutil.RemoveAll(tempdir)
	assert.NoError(t, err)
}

func putBlockData(
	ctx context.Context, t *testing.T, j *blockJournal, data []byte) (
	kbfsblock.ID, kbfsblock.Context, kbfscrypto.BlockCryptKeyServerHalf) {
	oldLength := getBlockJournalLength(t, j)

	bID, err := kbfsblock.MakePermanentID(data)
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	bCtx := kbfsblock.MakeFirstContext(uid1)
	serverHalf, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	putData, err := j.putData(ctx, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.True(t, putData)

	require.Equal(t, oldLength+1, getBlockJournalLength(t, j))

	return bID, bCtx, serverHalf
}

func addBlockRef(
	ctx context.Context, t *testing.T, j *blockJournal,
	bID kbfsblock.ID) kbfsblock.Context {
	oldLength := getBlockJournalLength(t, j)

	nonce, err := kbfsblock.MakeRefNonce()
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	bCtx2 := kbfsblock.MakeContext(uid1, uid2, nonce)
	err = j.addReference(ctx, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, oldLength+1, getBlockJournalLength(t, j))
	return bCtx2
}

func getAndCheckBlockData(ctx context.Context, t *testing.T, j *blockJournal,
	bID kbfsblock.ID, bCtx kbfsblock.Context, expectedData []byte,
	expectedServerHalf kbfscrypto.BlockCryptKeyServerHalf) {
	data, serverHalf, err := j.getDataWithContext(bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, expectedData, data)
	require.Equal(t, expectedServerHalf, serverHalf)
}

func TestBlockJournalBasic(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockData(ctx, t, j, data)

	// Make sure we get the same block back.
	getAndCheckBlockData(ctx, t, j, bID, bCtx, data, serverHalf)

	// Add a reference.
	bCtx2 := addBlockRef(ctx, t, j, bID)

	// Make sure we get the same block via that reference.
	getAndCheckBlockData(ctx, t, j, bID, bCtx2, data, serverHalf)

	// Shutdown and restart.
	err := j.checkInSyncForTest()
	require.NoError(t, err)
	j, err = makeBlockJournal(ctx, j.codec, tempdir, j.log)
	require.NoError(t, err)

	require.Equal(t, 2, getBlockJournalLength(t, j))

	// Make sure we get the same block for both refs.

	getAndCheckBlockData(ctx, t, j, bID, bCtx, data, serverHalf)
	getAndCheckBlockData(ctx, t, j, bID, bCtx2, data, serverHalf)
}

func TestBlockJournalDuplicatePut(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	data := []byte{1, 2, 3, 4}

	oldLength := getBlockJournalLength(t, j)

	bID, err := kbfsblock.MakePermanentID(data)
	require.NoError(t, err)

	uid1 := keybase1.MakeTestUID(1)
	bCtx := kbfsblock.MakeFirstContext(uid1)
	serverHalf, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	putData, err := j.putData(ctx, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.True(t, putData)

	require.Equal(t, int64(len(data)), j.getStoredBytes())
	require.Equal(t, int64(len(data)), j.getUnflushedBytes())
	require.Equal(t, int64(filesPerBlockMax), j.getStoredFiles())

	// Put a second time.
	putData, err = j.putData(ctx, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
	require.False(t, putData)

	require.Equal(t, oldLength+2, getBlockJournalLength(t, j))

	// Shouldn't count the block twice.
	require.Equal(t, int64(len(data)), j.getStoredBytes())
	require.Equal(t, int64(len(data)), j.getUnflushedBytes())
	require.Equal(t, int64(filesPerBlockMax), j.getStoredFiles())
}

func TestBlockJournalAddReference(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	data := []byte{1, 2, 3, 4}
	bID, err := kbfsblock.MakePermanentID(data)
	require.NoError(t, err)

	// Add a reference, which should succeed.
	bCtx := addBlockRef(ctx, t, j, bID)

	// Of course, the block get should still fail.
	_, _, err = j.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)
}

func TestBlockJournalArchiveReferences(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockData(ctx, t, j, data)

	// Add a reference.
	bCtx2 := addBlockRef(ctx, t, j, bID)

	// Archive references.
	err := j.archiveReferences(
		ctx, kbfsblock.ContextMap{bID: {bCtx, bCtx2}})
	require.NoError(t, err)
	require.Equal(t, 3, getBlockJournalLength(t, j))

	// Get block should still succeed.
	getAndCheckBlockData(ctx, t, j, bID, bCtx, data, serverHalf)
}

func TestBlockJournalArchiveNonExistentReference(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	uid1 := keybase1.MakeTestUID(1)

	bCtx := kbfsblock.MakeFirstContext(uid1)

	data := []byte{1, 2, 3, 4}
	bID, err := kbfsblock.MakePermanentID(data)
	require.NoError(t, err)

	// Archive references.
	err = j.archiveReferences(
		ctx, kbfsblock.ContextMap{bID: {bCtx}})
	require.NoError(t, err)
}

func TestBlockJournalRemoveReferences(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockData(ctx, t, j, data)

	// Add a reference.
	bCtx2 := addBlockRef(ctx, t, j, bID)

	// Remove references.
	liveCounts, err := j.removeReferences(
		ctx, kbfsblock.ContextMap{bID: {bCtx, bCtx2}})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID: 0}, liveCounts)
	require.Equal(t, 3, getBlockJournalLength(t, j))

	// Make sure the block data is inaccessible.
	_, _, err = j.getDataWithContext(bID, bCtx)
	require.Equal(t, blockNonExistentError{bID}, err)

	// But the actual data should remain (for flushing).
	buf, half, err := j.getData(bID)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, half)
}

func TestBlockJournalDuplicateRemove(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put the block.
	data := []byte{1, 2, 3, 4}
	dataLen := int64(len(data))
	bID, bCtx, _ := putBlockData(ctx, t, j, data)

	require.Equal(t, dataLen, j.getStoredBytes())
	require.Equal(t, dataLen, j.getUnflushedBytes())
	require.Equal(t, int64(filesPerBlockMax), j.getStoredFiles())

	// Remove the only reference to the block, then remove the
	// block.
	liveCounts, err := j.removeReferences(
		ctx, kbfsblock.ContextMap{bID: {bCtx}})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID: 0}, liveCounts)
	removedBytes, removedFiles, err := j.remove(ctx, bID)
	require.NoError(t, err)
	require.Equal(t, dataLen, removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)

	// This violates the invariant that UnflushedBytes <=
	// StoredBytes, but that's because we're manually removing the
	// block -- normally, the block would be flushed first, then
	// removed.
	require.Equal(t, int64(0), j.getStoredBytes())
	require.Equal(t, dataLen, j.getUnflushedBytes())
	require.Equal(t, int64(0), j.getStoredFiles())

	// Remove the block again.
	removedBytes, removedFiles, err = j.remove(ctx, bID)
	require.NoError(t, err)
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	// Shouldn't account for the block again.
	require.Equal(t, int64(0), j.getStoredBytes())
	require.Equal(t, dataLen, j.getUnflushedBytes())
	require.Equal(t, int64(0), j.getStoredFiles())
}

func testBlockJournalGCd(t *testing.T, j *blockJournal) {
	err := filepath.Walk(j.dir,
		func(path string, info os.FileInfo, _ error) error {
			// We should only find the blocks directories and
			// aggregate info file here.
			if path != j.dir && path != j.s.dir && path != j.j.dir && path != aggregateInfoPath(j.dir) {
				t.Errorf("Found unexpected block path: %s", path)
			}
			return nil
		})
	require.NoError(t, err)
}

func TestBlockJournalFlush(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put a block.

	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockData(ctx, t, j, data)

	// Add some references.

	bCtx2 := addBlockRef(ctx, t, j, bID)
	bCtx3 := addBlockRef(ctx, t, j, bID)

	// Archive one of the references.

	err := j.archiveReferences(
		ctx, kbfsblock.ContextMap{
			bID: {bCtx3},
		})
	require.NoError(t, err)

	blockServer := NewBlockServerMemory(log)

	tlfID := tlf.FakeID(1, false)

	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	flush := func() (int64, int64) {
		end, err := j.end()
		require.NoError(t, err)
		if end == 0 {
			return 0, 0
		}

		// Test that the end parameter is respected.
		var partialEntries blockEntriesToFlush
		var rev MetadataRevision
		if end > 1 {
			partialEntries, rev, err = j.getNextEntriesToFlush(ctx, end-1,
				maxJournalBlockFlushBatchSize)
			require.NoError(t, err)
			require.Equal(t, rev, MetadataRevisionUninitialized)
		}

		entries, rev, err := j.getNextEntriesToFlush(ctx, end,
			maxJournalBlockFlushBatchSize)
		require.NoError(t, err)
		require.Equal(t, partialEntries.length()+1, entries.length())
		require.Equal(t, rev, MetadataRevisionUninitialized)

		err = flushBlockEntries(
			ctx, j.log, blockServer, bcache, reporter,
			tlfID, CanonicalTlfName("fake TLF"), entries)
		require.NoError(t, err)

		removedBytes, removedFiles, err := j.removeFlushedEntries(
			ctx, entries, tlfID, reporter)
		require.NoError(t, err)
		return removedBytes, removedFiles
	}

	// Flushing all the reference adds should remove the
	// (now-unreferenced) block.
	removedBytes, removedFiles := flush()
	require.Equal(t, int64(len(data)), removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)

	// Check the Put.
	buf, key, err := blockServer.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Check the AddReference.
	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Check the archiving.
	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Now remove all the references.
	liveCounts, err := j.removeReferences(
		ctx, kbfsblock.ContextMap{
			bID: {bCtx, bCtx2, bCtx3},
		})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID: 0}, liveCounts)

	removedBytes, removedFiles = flush()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	// Check they're all gone.
	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)
	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)
	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)

	length, err := j.length()
	require.NoError(t, err)
	require.Zero(t, length)

	// Make sure the ordinals and blocks are flushed.
	testBlockJournalGCd(t, j)
}

func flushBlockJournalOne(ctx context.Context, t *testing.T,
	j *blockJournal, blockServer BlockServer,
	bcache BlockCache, reporter Reporter, tlfID tlf.ID) (
	removedFiles, removedBytes int64) {
	first, err := j.j.readEarliestOrdinal()
	require.NoError(t, err)
	entries, _, err := j.getNextEntriesToFlush(ctx, first+1,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, 1, entries.length())
	err = flushBlockEntries(ctx, j.log, blockServer,
		bcache, reporter, tlfID, CanonicalTlfName("fake TLF"),
		entries)
	require.NoError(t, err)
	removedBytes, removedFiles, err = j.removeFlushedEntries(
		ctx, entries, tlfID, reporter)
	require.NoError(t, err)
	err = j.checkInSyncForTest()
	require.NoError(t, err)
	return removedBytes, removedFiles
}

func TestBlockJournalFlushInterleaved(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put a block.

	data := []byte{1, 2, 3, 4}
	bID, bCtx, serverHalf := putBlockData(ctx, t, j, data)

	// Add some references.

	bCtx2 := addBlockRef(ctx, t, j, bID)
	bCtx3 := addBlockRef(ctx, t, j, bID)

	// Flush the block put. (Interleave flushes to test
	// checkInSync in intermediate states.)

	blockServer := NewBlockServerMemory(log)

	tlfID := tlf.FakeID(1, false)

	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	flushOne := func() (int64, int64) {
		return flushBlockJournalOne(
			ctx, t, j, blockServer, bcache, reporter, tlfID)
	}

	removedBytes, removedFiles := flushOne()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	buf, key, err := blockServer.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Remove some references.

	liveCounts, err := j.removeReferences(
		ctx, kbfsblock.ContextMap{
			bID: {bCtx, bCtx2},
		})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID: 1}, liveCounts)

	// Flush the reference adds.

	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flushing the last reference add should remove the
	// (now-unreferenced) block.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(len(data)), removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Archive the rest.

	err = j.archiveReferences(
		ctx, kbfsblock.ContextMap{
			bID: {bCtx3},
		})
	require.NoError(t, err)

	// Flush the reference removals.

	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	_, _, err = blockServer.Get(ctx, tlfID, bID, bCtx)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)

	_, _, err = blockServer.Get(ctx, tlfID, bID, bCtx2)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Remove the archived references.

	liveCounts, err = j.removeReferences(
		ctx, kbfsblock.ContextMap{
			bID: {bCtx3},
		})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID: 0}, liveCounts)

	// Flush the reference archival.

	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Flush the last removal.

	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	buf, key, err = blockServer.Get(ctx, tlfID, bID, bCtx3)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)

	end, err := j.end()
	require.NoError(t, err)
	entries, _, err := j.getNextEntriesToFlush(ctx, end,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, 0, entries.length())

	// Make sure the ordinals and blocks are flushed.
	testBlockJournalGCd(t, j)
}

func TestBlockJournalFlushMDRevMarker(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put a block.

	data := []byte{1, 2, 3, 4}
	putBlockData(ctx, t, j, data)

	// Put a revision marker
	rev := MetadataRevision(10)
	err := j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	blockServer := NewBlockServerMemory(log)
	tlfID := tlf.FakeID(1, false)
	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	// Make sure the block journal reports that entries up to `rev`
	// can be flushed.
	last, err := j.j.readLatestOrdinal()
	require.NoError(t, err)
	entries, gotRev, err := j.getNextEntriesToFlush(ctx, last+1,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, rev, gotRev)
	require.Equal(t, 2, entries.length())
	err = flushBlockEntries(ctx, j.log, blockServer,
		bcache, reporter, tlfID, CanonicalTlfName("fake TLF"),
		entries)
	require.NoError(t, err)
	removedBytes, removedFiles, err := j.removeFlushedEntries(
		ctx, entries, tlfID, reporter)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)
	err = j.checkInSyncForTest()
	require.NoError(t, err)
}

func TestBlockJournalFlushMDRevMarkerForPendingLocalSquash(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put some blocks.

	data1 := []byte{1, 2, 3, 4}
	_, _, _ = putBlockData(ctx, t, j, data1)
	data2 := []byte{5, 6, 7, 8}
	id2, _, _ := putBlockData(ctx, t, j, data2)

	// Put a revision marker and say it's from a local squash.
	rev := MetadataRevision(10)
	err := j.markMDRevision(ctx, rev, true)
	require.NoError(t, err)

	// Do another, that isn't from a local squash.
	data3 := []byte{9, 10, 11, 12}
	id3, _, _ := putBlockData(ctx, t, j, data3)
	data4 := []byte{13, 14, 15, 16}
	_, _, _ = putBlockData(ctx, t, j, data4)
	rev++
	err = j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	err = j.ignoreBlocksAndMDRevMarkers(ctx, []kbfsblock.ID{id2, id3}, rev)
	require.NoError(t, err)

	blockServer := NewBlockServerMemory(log)
	tlfID := tlf.FakeID(1, false)
	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	// Make sure the block journal reports that entries up to 10 can
	// be flushed; there should only be two blocks left, and one
	// revision marker (plus 2 ignored blocks and 1 ignored revision
	// marker).
	last, err := j.j.readLatestOrdinal()
	require.NoError(t, err)
	entries, gotRev, err := j.getNextEntriesToFlush(ctx, last+1,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, rev-1, gotRev)
	require.Equal(t, 6, entries.length())
	require.Len(t, entries.puts.blockStates, 2)
	require.Len(t, entries.adds.blockStates, 0)
	require.Len(t, entries.other, 4)

	err = flushBlockEntries(ctx, j.log, blockServer,
		bcache, reporter, tlfID, CanonicalTlfName("fake TLF"),
		entries)
	require.NoError(t, err)
	removedBytes, removedFiles, err := j.removeFlushedEntries(
		ctx, entries, tlfID, reporter)
	require.NoError(t, err)
	require.Equal(t, int64(len(data1)+len(data2)+len(data3)+len(data4)),
		removedBytes)
	require.Equal(t, int64(4*filesPerBlockMax), removedFiles)

	err = j.checkInSyncForTest()
	require.NoError(t, err)
}

func TestBlockJournalIgnoreBlocks(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put a few blocks
	data1 := []byte{1, 2, 3}
	bID1, _, _ := putBlockData(ctx, t, j, data1)

	// Put a revision marker
	rev := MetadataRevision(9)
	firstRev := rev
	err := j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	data2 := []byte{4, 5, 6, 7}
	bID2, _, _ := putBlockData(ctx, t, j, data2)

	// Put a revision marker
	rev = MetadataRevision(10)
	err = j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	data3 := []byte{8, 9, 10, 11, 12}
	bID3, _, _ := putBlockData(ctx, t, j, data3)
	data4 := []byte{13, 14, 15, 16, 17, 18}
	bID4, _, _ := putBlockData(ctx, t, j, data4)

	// Put a revision marker
	rev = MetadataRevision(11)
	err = j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	// Save the blocks to test that ignores make it to the saved
	// journal, too.
	err = j.saveBlocksUntilNextMDFlush()
	require.NoError(t, err)

	err = j.ignoreBlocksAndMDRevMarkers(
		ctx, []kbfsblock.ID{bID2, bID3}, firstRev)
	require.NoError(t, err)

	blockServer := NewBlockServerMemory(log)
	tlfID := tlf.FakeID(1, false)
	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	// Flush and make sure we only flush the non-ignored blocks.
	last, err := j.j.readLatestOrdinal()
	require.NoError(t, err)
	entries, gotRev, err := j.getNextEntriesToFlush(ctx, last+1,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, MetadataRevisionUninitialized, gotRev)
	require.Equal(t, 7, entries.length())
	require.Len(t, entries.puts.blockStates, 2)
	require.Len(t, entries.adds.blockStates, 0)
	require.Len(t, entries.other, 5)
	require.Equal(t, bID1, entries.puts.blockStates[0].blockPtr.ID)
	require.Equal(t, bID4, entries.puts.blockStates[1].blockPtr.ID)
	err = flushBlockEntries(ctx, j.log, blockServer,
		bcache, reporter, tlfID, CanonicalTlfName("fake TLF"),
		entries)
	require.NoError(t, err)
	removedBytes, removedFiles, err := j.removeFlushedEntries(
		ctx, entries, tlfID, reporter)
	require.NoError(t, err)
	// There's a saved journal, so no bytes or files will be removed.
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	// Try flushing up the first MD marker.  Since that marker is
	// ignored, all blocks should be flushed.
	nextLastToRemove, removedBytes, removedFiles, err := j.onMDFlush(
		ctx, 100, rev-1, 0)
	require.NoError(t, err)
	require.Zero(t, nextLastToRemove)
	require.Equal(t, int64(len(data1)+len(data2)+len(data3)+len(data4)),
		removedBytes)
	require.Equal(t, int64(4*filesPerBlockMax), removedFiles)

	err = j.checkInSyncForTest()
	require.NoError(t, err)
}

func TestBlockJournalSaveUntilMDFlush(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// Put a few blocks
	data1 := []byte{1, 2, 3, 4}
	bID1, _, _ := putBlockData(ctx, t, j, data1)
	data2 := []byte{5, 6, 7, 8}
	bID2, _, _ := putBlockData(ctx, t, j, data2)

	// Put a revision marker
	rev := MetadataRevision(10)
	err := j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	data3 := []byte{9, 10, 11, 12}
	bID3, _, _ := putBlockData(ctx, t, j, data3)
	data4 := []byte{13, 14, 15, 16}
	bID4, _, _ := putBlockData(ctx, t, j, data4)

	// Put a revision marker
	rev = MetadataRevision(11)
	err = j.markMDRevision(ctx, rev, false)
	require.NoError(t, err)

	err = j.saveBlocksUntilNextMDFlush()
	require.NoError(t, err)
	savedBlocks := []kbfsblock.ID{bID1, bID2, bID3, bID4}

	blockServer := NewBlockServerMemory(log)
	tlfID := tlf.FakeID(1, false)
	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)

	// Flush all the entries, but they should still remain accessible.
	flushAll := func() (int64, int64) {
		last, err := j.j.readLatestOrdinal()
		require.NoError(t, err)
		entries, _, err := j.getNextEntriesToFlush(ctx, last+1,
			maxJournalBlockFlushBatchSize)
		require.NoError(t, err)
		err = flushBlockEntries(ctx, j.log, blockServer,
			bcache, reporter, tlfID, CanonicalTlfName("fake TLF"),
			entries)
		require.NoError(t, err)
		removedBytes, removedFiles, err := j.removeFlushedEntries(
			ctx, entries, tlfID, reporter)
		require.NoError(t, err)
		return removedBytes, removedFiles
	}
	removedBytes, removedFiles := flushAll()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	// The blocks can still be fetched from the journal.
	for _, bid := range savedBlocks {
		ok, err := j.hasData(bid)
		require.NoError(t, err)
		require.True(t, ok)
	}

	// No more blocks to flush though.
	end, err := j.end()
	require.NoError(t, err)
	entries, gotRev, err := j.getNextEntriesToFlush(ctx, end,
		maxJournalBlockFlushBatchSize)
	require.NoError(t, err)
	require.Equal(t, 0, entries.length())
	require.Equal(t, MetadataRevisionUninitialized, gotRev)

	// Add a few more blocks and save those too.
	data5 := []byte{17, 18, 19, 20}
	bID5, _, _ := putBlockData(ctx, t, j, data5)
	data6 := []byte{21, 22, 23, 24}
	bID6, _, _ := putBlockData(ctx, t, j, data6)
	savedBlocks = append(savedBlocks, bID5, bID6)
	removedBytes, removedFiles = flushAll()
	require.Equal(t, int64(0), removedBytes)
	require.Equal(t, int64(0), removedFiles)

	// Make sure all the blocks still exist, including both the old
	// and the new ones.
	for _, bid := range savedBlocks {
		ok, err := j.hasData(bid)
		require.NoError(t, err)
		require.True(t, ok)
	}

	{
		// Make sure the saved block journal persists after a restart.
		jRestarted, err := makeBlockJournal(ctx, j.codec, j.dir, j.log)
		require.NoError(t, err)
		require.NotNil(t, jRestarted.saveUntilMDFlush)
	}

	// Now remove all the data, one at a time.  Remember there are two
	// revision markers that also need removal.
	lastToRemove := journalOrdinal(0)
	for i := 0; i < len(savedBlocks)-1+2; i++ {
		lastToRemove, removedBytes, removedFiles, err = j.onMDFlush(
			ctx, 1, rev+1, lastToRemove)
		require.NoError(t, err)
		require.NotZero(t, lastToRemove, "Iter %d", i)
		// Set expectedBytes to 4 and expectedFiles to
		// filesPerBlockMax only for the puts of
		// data{1,2,3,4,5,6} (including the onMDFlush call
		// right after the loop).
		var expectedBytes int64
		var expectedFiles int64
		if i%3 != 2 {
			expectedBytes = 4
			expectedFiles = filesPerBlockMax
		}
		require.Equal(t, expectedBytes, removedBytes, "Iter %d", i)
		require.Equal(t, expectedFiles, removedFiles, "Iter %d", i)
		require.NotNil(t, j.saveUntilMDFlush)
	}
	lastToRemove, removedBytes, removedFiles, err =
		j.onMDFlush(ctx, 1, rev+1, lastToRemove)
	require.NoError(t, err)
	require.Zero(t, lastToRemove)
	require.Equal(t, int64(4), removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)
	require.Nil(t, j.saveUntilMDFlush)

	ok, err := j.isUnflushed(bID1)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = j.isUnflushed(bID2)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = j.isUnflushed(bID3)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = j.isUnflushed(bID4)
	require.NoError(t, err)
	require.False(t, ok)

	testBlockJournalGCd(t, j)
}

func TestBlockJournalByteCounters(t *testing.T) {
	ctx, cancel, tempdir, log, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	// In this test, stored bytes and unflushed bytes should
	// change identically.
	requireCounts := func(expectedBytes, expectedFiles int) {
		require.Equal(t, int64(expectedBytes), j.getStoredBytes())
		require.Equal(t, int64(expectedBytes), j.getUnflushedBytes())
		require.Equal(t, int64(expectedFiles), j.getStoredFiles())
		var info aggregateInfo
		err := kbfscodec.DeserializeFromFile(
			j.codec, aggregateInfoPath(j.dir), &info)
		if !ioutil.IsNotExist(err) {
			require.NoError(t, err)
		}
		require.Equal(t, int64(expectedBytes), info.StoredBytes)
		require.Equal(t, int64(expectedBytes), info.UnflushedBytes)
		require.Equal(t, int64(expectedFiles), info.StoredFiles)
	}

	// Prime the cache.
	requireCounts(0, 0)

	data1 := []byte{1, 2, 3, 4}
	bID1, bCtx1, _ := putBlockData(ctx, t, j, data1)

	requireCounts(len(data1), filesPerBlockMax)

	data2 := []byte{1, 2, 3, 4, 5}
	bID2, bCtx2, _ := putBlockData(ctx, t, j, data2)

	expectedSize := len(data1) + len(data2)
	requireCounts(expectedSize, 2*filesPerBlockMax)

	// Adding, archive, or removing references shouldn't change
	// anything.

	bCtx1b := addBlockRef(ctx, t, j, bID1)
	requireCounts(expectedSize, 2*filesPerBlockMax)

	data3 := []byte{1, 2, 3}
	bID3, err := kbfsblock.MakePermanentID(data3)
	require.NoError(t, err)
	_ = addBlockRef(ctx, t, j, bID3)
	require.NoError(t, err)

	err = j.archiveReferences(
		ctx, kbfsblock.ContextMap{bID2: {bCtx2}})
	require.NoError(t, err)
	requireCounts(expectedSize, 2*filesPerBlockMax)

	liveCounts, err := j.removeReferences(
		ctx, kbfsblock.ContextMap{bID1: {bCtx1, bCtx1b}})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID1: 0}, liveCounts)
	requireCounts(expectedSize, 2*filesPerBlockMax)

	liveCounts, err = j.removeReferences(
		ctx, kbfsblock.ContextMap{bID2: {bCtx2}})
	require.NoError(t, err)
	require.Equal(t, map[kbfsblock.ID]int{bID2: 0}, liveCounts)
	requireCounts(expectedSize, 2*filesPerBlockMax)

	blockServer := NewBlockServerMemory(log)
	tlfID := tlf.FakeID(1, false)
	bcache := NewBlockCacheStandard(0, 0)
	reporter := NewReporterSimple(nil, 0)
	flushOne := func() (int64, int64) {
		return flushBlockJournalOne(
			ctx, t, j, blockServer, bcache, reporter, tlfID)
	}

	// Flush the first put. This causes the block to be GCed since
	// the subsequent ops for that block remove the references.
	removedBytes, removedFiles := flushOne()
	require.Equal(t, int64(len(data1)), removedBytes)
	require.Equal(t, int64(filesPerBlockMax), removedFiles)
	expectedSize = len(data2)
	requireCounts(expectedSize, filesPerBlockMax)

	// Flush the second put.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(len(data2)), removedBytes)
	requireCounts(0, 0)

	// Flush the first add ref.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	requireCounts(0, 0)

	// Flush the second add ref, but push the block to the server
	// first.

	uid1 := keybase1.MakeTestUID(1)
	bCtx3 := kbfsblock.MakeFirstContext(uid1)
	serverHalf3, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	err = blockServer.Put(
		context.Background(), tlfID, bID3, bCtx3, data3, serverHalf3)
	require.NoError(t, err)

	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	requireCounts(0, 0)

	// Flush the add archive.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	requireCounts(0, 0)

	// Flush the first remove.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	requireCounts(0, 0)

	// Flush the second remove.
	removedBytes, removedFiles = flushOne()
	require.Equal(t, int64(0), removedBytes)
	requireCounts(0, 0)
}

func TestBlockJournalUnflushedBytesIgnore(t *testing.T) {
	ctx, cancel, tempdir, _, j := setupBlockJournalTest(t)
	defer teardownBlockJournalTest(t, ctx, cancel, tempdir, j)

	requireCounts := func(expectedStoredBytes, expectedUnflushedBytes,
		expectedStoredFiles int) {
		require.Equal(t, int64(expectedStoredBytes), j.getStoredBytes())
		require.Equal(t, int64(expectedUnflushedBytes),
			j.getUnflushedBytes())
		require.Equal(t, int64(expectedStoredFiles), j.getStoredFiles())
	}

	// Prime the cache.
	requireCounts(0, 0, 0)

	data1 := []byte{1, 2, 3, 4}
	bID1, _, _ := putBlockData(ctx, t, j, data1)

	requireCounts(len(data1), len(data1), filesPerBlockMax)

	data2 := []byte{1, 2, 3, 4, 5}
	_, _, _ = putBlockData(ctx, t, j, data2)

	requireCounts(len(data1)+len(data2), len(data1)+len(data2),
		2*filesPerBlockMax)

	err := j.ignoreBlocksAndMDRevMarkers(
		ctx, []kbfsblock.ID{bID1}, MetadataRevision(0))
	require.NoError(t, err)

	requireCounts(len(data1)+len(data2), len(data2), 2*filesPerBlockMax)
}
