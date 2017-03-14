// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"
	"testing"
	"time"

	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

const (
	testDiskBlockCacheMaxBytes uint64 = 1 << 20
)

type testDiskBlockCacheConfig struct {
	codecGetter
	logMaker
	*testClockGetter
}

func newTestDiskBlockCacheConfig(t *testing.T) testDiskBlockCacheConfig {
	return testDiskBlockCacheConfig{
		newTestCodecGetter(),
		newTestLogMaker(t),
		newTestClockGetter(),
	}
}

func newDiskBlockCacheStandardForTest(config diskBlockCacheConfig,
	maxBytes uint64) (*DiskBlockCacheStandard, error) {
	blockStorage := storage.NewMemStorage()
	lruStorage := storage.NewMemStorage()
	tlfStorage := storage.NewMemStorage()
	return newDiskBlockCacheStandardFromStorage(config, blockStorage,
		lruStorage, tlfStorage, maxBytes)
}

func initDiskBlockCacheTest(t *testing.T) (*DiskBlockCacheStandard,
	testDiskBlockCacheConfig) {
	config := newTestDiskBlockCacheConfig(t)
	cache, err := newDiskBlockCacheStandardForTest(config,
		testDiskBlockCacheMaxBytes)
	require.NoError(t, err)
	return cache, config
}

func shutdownDiskBlockCacheTest(cache DiskBlockCache) {
	cache.Shutdown(context.Background())
}

func setupBlockForDiskCache(t *testing.T, config diskBlockCacheConfig) (
	kbfsblock.ID, []byte, kbfscrypto.BlockCryptKeyServerHalf) {
	ptr := makeRandomBlockPointer(t)
	block := makeFakeFileBlock(t, false)
	blockEncoded, err := config.Codec().Encode(block)
	require.NoError(t, err)
	serverHalf, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	return ptr.ID, blockEncoded, serverHalf
}

func TestDiskBlockCachePutAndGet(t *testing.T) {
	t.Parallel()
	t.Log("Test that basic disk cache Put and Get operations work.")
	cache, config := initDiskBlockCacheTest(t)
	defer shutdownDiskBlockCacheTest(cache)

	tlf1 := tlf.FakeID(0, false)
	block1Id, block1Encoded, block1ServerHalf := setupBlockForDiskCache(t, config)

	ctx := context.Background()

	t.Log("Put a block into the cache.")
	err := cache.Put(ctx, tlf1, block1Id, block1Encoded, block1ServerHalf)
	require.NoError(t, err)
	putTime, err := cache.getLRU(block1Id)
	require.NoError(t, err)
	config.TestClock().Add(time.Second)

	t.Log("Get that block from the cache. Verify that it's the same.")
	buf, serverHalf, err := cache.Get(ctx, tlf1, block1Id)
	require.NoError(t, err)
	require.Equal(t, block1ServerHalf, serverHalf)
	require.Equal(t, block1Encoded, buf)

	t.Log("Verify that the Get updated the LRU time for the block.")
	getTime, err := cache.getLRU(block1Id)
	require.NoError(t, err)
	require.True(t, getTime.After(putTime))

	t.Log("Attempt to Get a block from the cache that isn't there." +
		" Verify that it fails.")
	ptr2 := makeRandomBlockPointer(t)
	buf, serverHalf, err = cache.Get(ctx, tlf1, ptr2.ID)
	require.EqualError(t, err, NoSuchBlockError{ptr2.ID}.Error())
	require.Equal(t, kbfscrypto.BlockCryptKeyServerHalf{}, serverHalf)
	require.Nil(t, buf)

	t.Log("Verify that the cache returns no LRU time for the missing block.")
	_, err = cache.getLRU(ptr2.ID)
	require.EqualError(t, err, errors.ErrNotFound.Error())
}

func TestDiskBlockCacheDelete(t *testing.T) {
	t.Parallel()
	t.Log("Test that disk cache deletion works.")
	cache, config := initDiskBlockCacheTest(t)
	defer shutdownDiskBlockCacheTest(cache)
	ctx := context.Background()

	t.Log("Seed the cache with some other TLFs")
	fakeTlfs := []byte{0, 1, 2, 4, 5}
	for _, f := range fakeTlfs {
		tlf := tlf.FakeID(f, false)
		blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
		err := cache.Put(ctx, tlf, blockID, blockEncoded, serverHalf)
		require.NoError(t, err)
	}
	tlf1 := tlf.FakeID(3, false)
	block1Id, block1Encoded, block1ServerHalf := setupBlockForDiskCache(t, config)
	block2Id, block2Encoded, block2ServerHalf := setupBlockForDiskCache(t, config)
	block3Id, block3Encoded, block3ServerHalf := setupBlockForDiskCache(t, config)

	t.Log("Put three blocks into the cache.")
	err := cache.Put(ctx, tlf1, block1Id, block1Encoded, block1ServerHalf)
	require.NoError(t, err)
	err = cache.Put(ctx, tlf1, block2Id, block2Encoded, block2ServerHalf)
	require.NoError(t, err)
	err = cache.Put(ctx, tlf1, block3Id, block3Encoded, block3ServerHalf)
	require.NoError(t, err)

	t.Log("Delete two of the blocks from the cache.")
	err = cache.DeleteByTLF(ctx, tlf1, []kbfsblock.ID{
		block1Id, block2Id})
	require.NoError(t, err)

	t.Log("Verify that only the non-deleted block is still in the cache.")
	_, _, err = cache.Get(ctx, tlf1, block1Id)
	require.EqualError(t, err, NoSuchBlockError{block1Id}.Error())
	_, _, err = cache.Get(ctx, tlf1, block2Id)
	require.EqualError(t, err, NoSuchBlockError{block2Id}.Error())
	_, _, err = cache.Get(ctx, tlf1, block3Id)
	require.NoError(t, err)

	t.Log("Verify that the cache returns no LRU time for the missing blocks.")
	_, err = cache.getLRU(block1Id)
	require.EqualError(t, err, errors.ErrNotFound.Error())
	_, err = cache.getLRU(block2Id)
	require.EqualError(t, err, errors.ErrNotFound.Error())
}

func TestDiskBlockCacheEvictFromTLF(t *testing.T) {
	t.Parallel()
	t.Log("Test that disk cache eviction works for a single TLF.")
	cache, config := initDiskBlockCacheTest(t)
	defer shutdownDiskBlockCacheTest(cache)

	tlf1 := tlf.FakeID(3, false)
	ctx := context.Background()
	clock := config.TestClock()
	initialTime := clock.Now()
	t.Log("Seed the cache with some other TLFs.")
	fakeTlfs := []byte{0, 1, 2, 4, 5}
	for _, f := range fakeTlfs {
		tlf := tlf.FakeID(f, false)
		blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
		err := cache.Put(ctx, tlf, blockID, blockEncoded, serverHalf)
		require.NoError(t, err)
		clock.Add(time.Second)
	}
	tlf1NumBlocks := 100
	t.Log("Put 100 blocks into the cache.")
	for i := 0; i < tlf1NumBlocks; i++ {
		blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
		err := cache.Put(ctx, tlf1, blockID, blockEncoded, serverHalf)
		require.NoError(t, err)
		clock.Add(time.Second)
	}

	previousAvgDuration := 50 * time.Second
	averageDifference := float64(0)
	numEvictionDifferences := 0
	expectedCount := tlf1NumBlocks

	t.Log("Incrementally evict all the tlf1 blocks in the cache.")
	// Because the eviction algorithm is probabilistic, we can't rely on the
	// same number of blocks being evicted every time. So we have to be smart
	// about our assertions.
	for expectedCount != 0 {
		t.Log("Evict 10 blocks from the cache.")
		numRemoved, err := cache.evictFromTLFLocked(ctx, tlf1, 10)
		require.NoError(t, err)
		expectedCount -= numRemoved

		blockCount := 0
		var avgDuration time.Duration
		func() {
			tlfBytes := tlf1.Bytes()
			tlf1Range := util.BytesPrefix(tlfBytes)
			iter := cache.tlfDb.NewIterator(tlf1Range, nil)
			defer iter.Release()
			for iter.Next() {
				blockIDBytes := iter.Key()[len(tlfBytes):]
				blockID, err := kbfsblock.IDFromBytes(blockIDBytes)
				require.NoError(t, err)
				putTime, err := cache.getLRU(blockID)
				require.NoError(t, err)
				avgDuration += putTime.Sub(initialTime)
				blockCount++
			}
		}()
		t.Logf("Verify that there are %d blocks in the cache.", expectedCount)
		require.Equal(t, expectedCount, blockCount,
			"Removed %d blocks this round.", numRemoved)
		if expectedCount > 0 {
			avgDuration /= time.Duration(expectedCount)
			t.Logf("Average LRU time of remaining blocks: %.2f",
				avgDuration.Seconds())
			averageDifference += avgDuration.Seconds() -
				previousAvgDuration.Seconds()
			previousAvgDuration = avgDuration
			numEvictionDifferences++
		}
	}
	t.Log("Verify that, on average, the LRU time of the blocks remaining in" +
		" the queue keeps going up.")
	averageDifference /= float64(numEvictionDifferences)
	require.True(t, averageDifference > 3.0,
		"Average overall LRU delta from an eviction: %.2f", averageDifference)
}

func TestDiskBlockCacheEvictOverall(t *testing.T) {
	t.Parallel()
	t.Log("Test that disk cache eviction works overall.")
	cache, config := initDiskBlockCacheTest(t)
	defer shutdownDiskBlockCacheTest(cache)

	ctx := context.Background()
	clock := config.TestClock()
	initialTime := clock.Now()

	numTlfs := 10
	numBlocksPerTlf := 10
	totalBlocks := numTlfs * numBlocksPerTlf

	t.Log("Seed the cache with some other TLFs.")
	for i := byte(0); int(i) < numTlfs; i++ {
		currTlf := tlf.FakeID(i, false)
		for j := 0; j < numBlocksPerTlf; j++ {
			blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
			err := cache.Put(ctx, currTlf, blockID, blockEncoded, serverHalf)
			require.NoError(t, err)
			clock.Add(time.Second)
		}
	}

	// Average LRU will initially be half the total number of blocks, in
	// seconds.
	previousAvgDuration := time.Duration(totalBlocks>>1) * time.Second
	averageDifference := float64(0)
	numEvictionDifferences := 0
	expectedCount := totalBlocks

	t.Log("Incrementally evict all the blocks in the cache.")
	// Because the eviction algorithm is probabilistic, we can't rely on the
	// same number of blocks being evicted every time. So we have to be smart
	// about our assertions.
	for expectedCount != 0 {
		t.Log("Evict 10 blocks from the cache.")
		numRemoved, err := cache.evictLocked(ctx, 10)
		require.NoError(t, err)
		expectedCount -= numRemoved

		blockCount := 0
		var avgDuration time.Duration
		func() {
			iter := cache.metaDb.NewIterator(nil, nil)
			defer iter.Release()
			for iter.Next() {
				metadata := diskBlockCacheMetadata{}
				err = config.Codec().Decode(iter.Value(), &metadata)
				require.NoError(t, err)
				avgDuration += metadata.LRUTime.Sub(initialTime)
				blockCount++
			}
		}()
		t.Logf("Verify that there are %d blocks in the cache.", expectedCount)
		require.Equal(t, expectedCount, blockCount,
			"Removed %d blocks this round.", numRemoved)
		if expectedCount > 0 {
			avgDuration /= time.Duration(expectedCount)
			t.Logf("Average LRU time of remaining blocks: %.2f",
				avgDuration.Seconds())
			averageDifference += avgDuration.Seconds() -
				previousAvgDuration.Seconds()
			previousAvgDuration = avgDuration
			numEvictionDifferences++
		}
	}
	t.Log("Verify that, on average, the LRU time of the blocks remaining in" +
		" the queue keeps going up.")
	averageDifference /= float64(numEvictionDifferences)
	require.True(t, averageDifference > 3.0,
		"Average overall LRU delta from an eviction: %.2f", averageDifference)
}

func TestDiskBlockCacheLimit(t *testing.T) {
	t.Parallel()
	t.Log("Test that disk cache eviction works overall.")
	cache, config := initDiskBlockCacheTest(t)
	defer shutdownDiskBlockCacheTest(cache)

	ctx := context.Background()
	clock := config.TestClock()

	numTlfs := 10
	numBlocksPerTlf := 5

	t.Log("Seed the cache with some blocks.")
	for i := byte(0); int(i) < numTlfs; i++ {
		currTlf := tlf.FakeID(i, false)
		for j := 0; j < numBlocksPerTlf; j++ {
			blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
			err := cache.Put(ctx, currTlf, blockID, blockEncoded, serverHalf)
			require.NoError(t, err)
			clock.Add(time.Second)
		}
	}

	t.Log("Set the cache maximum bytes to the current total.")
	cache.maxBytes = cache.currBytes
	currBytes := cache.maxBytes

	t.Log("Add a block to the cache. Verify that blocks were evicted.")
	blockID, blockEncoded, serverHalf := setupBlockForDiskCache(t, config)
	err := cache.Put(ctx, tlf.FakeID(10, false), blockID, blockEncoded, serverHalf)
	require.NoError(t, err)

	require.True(t, cache.currBytes < currBytes)
	require.Equal(t, 41, cache.numBlocks)
}
