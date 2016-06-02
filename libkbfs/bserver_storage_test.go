// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"math/rand"
	"os"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

func makeTestEntries(n int) ([]BlockID, []blockEntry, error) {
	ids := make([]BlockID, n)

	for i := 0; i < n; i++ {
		ids[i] = RandomBlockID()
	}

	numEntries := 5
	entries := make([]blockEntry, numEntries)
	blockSize := 64 * 1024
	for i := 0; i < numEntries; i++ {
		entries[i].BlockData = make([]byte, blockSize)
		err := cryptoRandRead(entries[i].BlockData)
		if err != nil {
			return nil, nil, err
		}
		err = cryptoRandRead(entries[i].KeyServerHalf.data[:])
		if err != nil {
			return nil, nil, err
		}
		err = cryptoRandRead(entries[i].Tlf.id[:TlfIDByteLen-1])
		if err != nil {
			return nil, nil, err
		}
		entries[i].Tlf.id[TlfIDByteLen-1] = TlfIDSuffix
	}

	return ids, entries, nil
}

func doPuts(b *testing.B, ids []BlockID, entries []blockEntry,
	s bserverLocalStorage) {
	for i := 0; i < len(ids); i++ {
		err := s.put(ids[i], entries[i%len(entries)])
		if err != nil {
			b.Fatal(err)
		}
	}
}

func runGetBenchmark(b *testing.B, s bserverLocalStorage) {
	numIDs := b.N
	if numIDs > 500 {
		numIDs = 500
	}
	ids, entries, err := makeTestEntries(numIDs)
	if err != nil {
		b.Fatal(err)
	}

	doPuts(b, ids, entries, s)

	indices := make([]int, b.N)
	for i := 0; i < b.N; i++ {
		indices[i] = rand.Intn(numIDs)
	}

	b.ResetTimer()
	defer b.StopTimer()

	for i := 0; i < b.N; i++ {
		// TODO: Do something to defeat compiler optimizations
		// if necessary.
		_, err := s.get(ids[indices[i]])
		if err != nil {
			b.Fatal(err)
		}
	}
}

type fileFixture struct {
	tempdir string
}

func makeFileFixture() (fixture fileFixture, err error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_file_storage")
	if err != nil {
		return
	}

	fixture = fileFixture{tempdir}
	return
}

func (f fileFixture) cleanup() {
	os.RemoveAll(f.tempdir)
}

type leveldbFixture struct {
	tempdir string
	db      *leveldb.DB
}

func makeLeveldbFixture() (fixture leveldbFixture, err error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_leveldb_storage")
	if err != nil {
		return
	}

	defer func() {
		if err != nil {
			os.RemoveAll(tempdir)
		}
	}()

	db, err := leveldb.OpenFile(tempdir, leveldbOptions)
	if err != nil {
		return
	}

	fixture = leveldbFixture{tempdir, db}
	return
}

func (f leveldbFixture) cleanup() {
	f.db.Close()
	os.RemoveAll(f.tempdir)
}

func BenchmarkMemStorageGet(b *testing.B) {
	s := makeBserverMemStorage()
	runGetBenchmark(b, s)
}

func BenchmarkFileStorageGet(b *testing.B) {
	f, err := makeFileFixture()
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		f.cleanup()
	}()

	s := makeBserverFileStorage(NewCodecMsgpack(), f.tempdir)
	runGetBenchmark(b, s)
}

func BenchmarkLeveldbStorageGet(b *testing.B) {
	f, err := makeLeveldbFixture()
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		f.cleanup()
	}()

	s := makeBserverLeveldbStorage(NewCodecMsgpack(), f.db)
	runGetBenchmark(b, s)
}

func runPutBenchmark(b *testing.B, s bserverLocalStorage) {
	ids, entries, err := makeTestEntries(b.N)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	defer b.StopTimer()

	doPuts(b, ids, entries, s)
}

func BenchmarkMemStoragePut(b *testing.B) {
	s := makeBserverMemStorage()
	runPutBenchmark(b, s)
}

func BenchmarkFileStoragePut(b *testing.B) {
	f, err := makeFileFixture()
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		f.cleanup()
	}()

	s := makeBserverFileStorage(NewCodecMsgpack(), f.tempdir)
	runPutBenchmark(b, s)
}

func BenchmarkLeveldbStoragePut(b *testing.B) {
	f, err := makeLeveldbFixture()
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		f.cleanup()
	}()

	s := makeBserverLeveldbStorage(NewCodecMsgpack(), f.db)
	runPutBenchmark(b, s)
}
