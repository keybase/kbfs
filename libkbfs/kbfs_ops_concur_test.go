// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol"
	"github.com/keybase/go-framed-msgpack-rpc"
	"golang.org/x/net/context"
)

// CounterLock keeps track of the number of lock attempts
type CounterLock struct {
	countLock sync.Mutex
	realLock  sync.Mutex
	count     int
}

func (cl *CounterLock) Lock() {
	cl.countLock.Lock()
	cl.count++
	cl.countLock.Unlock()
	cl.realLock.Lock()
}

func (cl *CounterLock) Unlock() {
	cl.realLock.Unlock()
}

func (cl *CounterLock) GetCount() int {
	cl.countLock.Lock()
	defer cl.countLock.Unlock()
	return cl.count
}

func kbfsOpsConcurInit(t *testing.T, users ...libkb.NormalizedUsername) (
	Config, keybase1.UID, context.Context) {
	return kbfsOpsInitNoMocks(t, users...)
}

// Test that only one of two concurrent GetRootMD requests can end up
// fetching the MD from the server.  The second one should wait, and
// then get it from the MD cache.
func TestKBFSOpsConcurDoubleMDGet(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onGetStalledCh := make(chan struct{}, 1)
	getUnstallCh := make(chan struct{})

	stallKey := "requestName"
	getValue := "get"

	config.SetMDOps(&stallingMDOps{
		stallOpName: "GetForTLF",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			getValue: staller{
				stalled: onGetStalledCh,
				unstall: getUnstallCh,
			},
		},
		delegate: config.MDOps(),
	})

	// Initialize the MD using a different config
	c2 := ConfigAsUser(config.(*ConfigLocal), "test_user")
	defer CheckConfigAndShutdown(t, c2)
	rootNode := GetRootNodeOrBust(t, c2, "test_user", false)

	n := 10
	c := make(chan error, n)
	cl := &CounterLock{}
	ops := getOps(config, rootNode.GetFolderBranch().Tlf)
	ops.mdWriterLock.locker = cl
	getCtx := context.WithValue(ctx, stallKey, getValue)
	for i := 0; i < n; i++ {
		go func() {
			_, _, _, err := ops.getRootNode(getCtx)
			c <- err
		}()
	}

	// wait until the first one starts the get
	<-onGetStalledCh
	// make sure that the second goroutine has also started its write
	// call, and thus must be queued behind the first one (since we
	// are guaranteed the first one is currently running, and they
	// both need the same lock).
	for cl.GetCount() < 2 {
		runtime.Gosched()
	}
	// Now let the first one complete.  The second one should find the
	// MD in the cache, and thus never call MDOps.Get().
	close(getUnstallCh)
	for i := 0; i < n; i++ {
		err := <-c
		if err != nil {
			t.Errorf("Got an error doing concurrent MD gets: err=(%s)", err)
		}
	}
}

func setStallingMDOpsForPut(ctx context.Context, config Config,
	stallAfterPut bool) (
	<-chan struct{}, chan<- struct{}, context.Context) {
	onPutStalledCh := make(chan struct{}, 1)
	putUnstallCh := make(chan struct{})

	stallKey := "requestName"
	putValue := "put"
	opName := "Put"
	if stallAfterPut {
		opName = "AfterPut"
	}

	config.SetMDOps(&stallingMDOps{
		stallOpName: opName,
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			putValue: staller{
				stalled: onPutStalledCh,
				unstall: putUnstallCh,
			},
		},
		delegate: config.MDOps(),
	})

	putCtx := context.WithValue(ctx, stallKey, putValue)
	return onPutStalledCh, putUnstallCh, putCtx
}

// Test that a read can happen concurrently with a sync
func TestKBFSOpsConcurReadDuringSync(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onPutStalledCh, putUnstallCh, putCtx :=
		setStallingMDOpsForPut(ctx, config, true)

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	data := []byte{1}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Fatalf("Couldn't write file: %v", err)
	}

	// start the sync
	errChan := make(chan error)
	go func() {
		errChan <- kbfsOps.Sync(putCtx, fileNode)
	}()

	// wait until Sync gets stuck at MDOps.Put()
	<-onPutStalledCh

	// now make sure we can read the file and see the byte we wrote
	buf := make([]byte, 1)
	nr, err := kbfsOps.Read(ctx, fileNode, buf, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v\n", err)
	}
	if nr != 1 || !bytes.Equal(data, buf) {
		t.Errorf("Got wrong data %v; expected %v", buf, data)
	}

	// now unblock Sync and make sure there was no error
	close(putUnstallCh)
	err = <-errChan
	if err != nil {
		t.Errorf("Sync got an error: %v", err)
	}
}

// Test that writes can happen concurrently with a sync
func testKBFSOpsConcurWritesDuringSync(t *testing.T,
	initialWriteBytes int, nOneByteWrites int) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onPutStalledCh, putUnstallCh, putCtx :=
		setStallingMDOpsForPut(ctx, config, true)

	// Use the smallest possible block size.
	bsplitter, err := NewBlockSplitterSimple(20, 8*1024, config.Codec())
	if err != nil {
		t.Fatalf("Couldn't create block splitter: %v", err)
	}
	config.SetBlockSplitter(bsplitter)

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	data := make([]byte, initialWriteBytes)
	for i := 0; i < initialWriteBytes; i++ {
		data[i] = 1
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// start the sync
	errChan := make(chan error)
	go func() {
		errChan <- kbfsOps.Sync(putCtx, fileNode)
	}()

	// wait until Sync gets stuck at MDOps.Put()
	<-onPutStalledCh

	expectedData := make([]byte, len(data))
	copy(expectedData, data)
	for i := 0; i < nOneByteWrites; i++ {
		// now make sure we can write the file and see the new byte we wrote
		newData := []byte{byte(i + 2)}
		err = kbfsOps.Write(ctx, fileNode, newData, int64(i+initialWriteBytes))
		if err != nil {
			t.Errorf("Couldn't write data: %v\n", err)
		}

		// read the data back
		buf := make([]byte, i+1+initialWriteBytes)
		nr, err := kbfsOps.Read(ctx, fileNode, buf, 0)
		if err != nil {
			t.Errorf("Couldn't read data: %v\n", err)
		}
		expectedData = append(expectedData, newData...)
		if nr != int64(i+1+initialWriteBytes) ||
			!bytes.Equal(expectedData, buf) {
			t.Errorf("Got wrong data %v; expected %v", buf, expectedData)
		}
	}

	// now unblock Sync and make sure there was no error
	close(putUnstallCh)
	err = <-errChan
	if err != nil {
		t.Errorf("Sync got an error: %v", err)
	}

	// finally, make sure we can still read it after the sync too
	// (even though the second write hasn't been sync'd yet)
	totalSize := nOneByteWrites + initialWriteBytes
	buf2 := make([]byte, totalSize)
	nr, err := kbfsOps.Read(ctx, fileNode, buf2, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v\n", err)
	}
	if nr != int64(totalSize) ||
		!bytes.Equal(expectedData, buf2) {
		t.Errorf("2nd read: Got wrong data %v; expected %v", buf2, expectedData)
	}

	// there should be 4+n clean blocks at this point: the original
	// root block + 2 modifications (create + write), the empty file
	// block, the n initial modification blocks plus top block (if
	// applicable).
	bcs := config.BlockCache().(*BlockCacheStandard)
	numCleanBlocks := bcs.cleanTransient.Len()
	nFileBlocks := 1 + len(data)/int(bsplitter.maxSize)
	if nFileBlocks > 1 {
		nFileBlocks++ // top indirect block
	}
	if g, e := numCleanBlocks, 4+nFileBlocks; g != e {
		t.Errorf("Unexpected number of cached clean blocks: %d vs %d (%d vs %d)\n", g, e, totalSize, bsplitter.maxSize)
	}

	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Fatalf("Final sync failed: %v", err)
	}

	if ei, err := kbfsOps.Stat(ctx, fileNode); err != nil {
		t.Fatalf("Couldn't stat: %v", err)
	} else if g, e := ei.Size, uint64(totalSize); g != e {
		t.Fatalf("Unexpected size: %d vs %d", g, e)
	}

	// Make sure there are no dirty blocks left at the end of the test.
	dbcs := config.DirtyBlockCache().(*DirtyBlockCacheStandard)
	numDirtyBlocks := len(dbcs.cache)
	if numDirtyBlocks != 0 {
		t.Errorf("%d dirty blocks left after final sync", numDirtyBlocks)
	}
}

// Test that a write can happen concurrently with a sync
func TestKBFSOpsConcurWriteDuringSync(t *testing.T) {
	testKBFSOpsConcurWritesDuringSync(t, 1, 1)
}

// Test that multiple writes can happen concurrently with a sync
// (regression for KBFS-616)
func TestKBFSOpsConcurMultipleWritesDuringSync(t *testing.T) {
	testKBFSOpsConcurWritesDuringSync(t, 1, 10)
}

// Test that multiple indirect writes can happen concurrently with a
// sync (regression for KBFS-661)
func TestKBFSOpsConcurMultipleIndirectWritesDuringSync(t *testing.T) {
	testKBFSOpsConcurWritesDuringSync(t, 25, 50)
}

// Test that writes that happen concurrently with a sync, which write
// to the same block, work correctly.
func TestKBFSOpsConcurDeferredDoubleWritesDuringSync(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onPutStalledCh, putUnstallCh, putCtx :=
		setStallingMDOpsForPut(ctx, config, true)

	// Use the smallest possible block size.
	bsplitter, err := NewBlockSplitterSimple(20, 8*1024, config.Codec())
	if err != nil {
		t.Fatalf("Couldn't create block splitter: %v", err)
	}
	config.SetBlockSplitter(bsplitter)

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	var data []byte
	// Write 2 blocks worth of data
	for i := 0; i < 30; i++ {
		data = append(data, byte(i))
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// Sync the initial two data blocks
	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Fatalf("Initial sync failed: %v", err)
	}

	// Now dirty the first block.
	newData1 := make([]byte, 10)
	copy(newData1, data[20:])
	err = kbfsOps.Write(ctx, fileNode, newData1, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// start the sync
	errChan := make(chan error)
	go func() {
		errChan <- kbfsOps.Sync(putCtx, fileNode)
	}()

	// wait until Sync gets stuck at MDOps.Put()
	<-onPutStalledCh

	// Now dirty the second block, twice.
	newData2 := make([]byte, 10)
	copy(newData2, data[:10])
	err = kbfsOps.Write(ctx, fileNode, newData2, 20)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}
	err = kbfsOps.Write(ctx, fileNode, newData2, 30)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// now unblock Sync and make sure there was no error
	close(putUnstallCh)
	err = <-errChan
	if err != nil {
		t.Errorf("Sync got an error: %v", err)
	}

	expectedData := make([]byte, 40)
	copy(expectedData[:10], newData1)
	copy(expectedData[10:20], data[10:20])
	copy(expectedData[20:30], newData2)
	copy(expectedData[30:40], newData2)

	gotData := make([]byte, 40)
	nr, err := kbfsOps.Read(ctx, fileNode, gotData, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v", err)
	}
	if nr != int64(len(gotData)) {
		t.Errorf("Only read %d bytes", nr)
	}
	if !bytes.Equal(expectedData, gotData) {
		t.Errorf("Read wrong data.  Expected %v, got %v", expectedData, gotData)
	}

	// Final sync
	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Fatalf("Final sync failed: %v", err)
	}

	gotData = make([]byte, 40)
	nr, err = kbfsOps.Read(ctx, fileNode, gotData, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v", err)
	}
	if nr != int64(len(gotData)) {
		t.Errorf("Only read %d bytes", nr)
	}
	if !bytes.Equal(expectedData, gotData) {
		t.Errorf("Read wrong data.  Expected %v, got %v", expectedData, gotData)
	}

	// Make sure there are no dirty blocks left at the end of the test.
	dbcs := config.DirtyBlockCache().(*DirtyBlockCacheStandard)
	numDirtyBlocks := len(dbcs.cache)
	if numDirtyBlocks != 0 {
		t.Errorf("%d dirty blocks left after final sync", numDirtyBlocks)
	}
}

// Test that a block write can happen concurrently with a block
// read. This is a regression test for KBFS-536.
func TestKBFSOpsConcurBlockReadWrite(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer config.Shutdown()

	// Turn off transient block caching.
	config.SetBlockCache(NewBlockCacheStandard(config, 0, 1<<30))

	// Create a file.
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// We only need to know the first time we stall.
	onReadStalledCh := make(chan struct{}, 1)
	onWriteStalledCh := make(chan struct{}, 1)

	readUnstallCh := make(chan struct{})
	writeUnstallCh := make(chan struct{})

	stallKey := "requestName"
	readValue := "read"
	writeValue := "write"

	config.SetBlockOps(&stallingBlockOps{
		stallOpName: "Get",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			readValue: staller{
				stalled: onReadStalledCh,
				unstall: readUnstallCh,
			},
			writeValue: staller{
				stalled: onWriteStalledCh,
				unstall: writeUnstallCh,
			},
		},
		internalDelegate: config.BlockOps(),
	})

	var wg sync.WaitGroup

	// Start the read and wait for it to stall.
	wg.Add(1)
	var readErr error
	go func() {
		defer wg.Done()

		readCtx := context.WithValue(ctx, stallKey, readValue)
		_, readErr = kbfsOps.GetDirChildren(readCtx, rootNode)
	}()
	<-onReadStalledCh

	// Start the write and wait for it to stall.
	wg.Add(1)
	var writeErr error
	go func() {
		defer wg.Done()

		data := []byte{1}
		writeCtx := context.WithValue(ctx, stallKey, writeValue)
		writeErr = kbfsOps.Write(writeCtx, fileNode, data, 0)
	}()
	<-onWriteStalledCh

	// Unstall the read, which shouldn't blow up.
	close(readUnstallCh)

	// Finally, unstall the write.
	close(writeUnstallCh)

	wg.Wait()

	// Do these in the main goroutine since t isn't goroutine
	// safe, and do these after wg.Wait() since we only know
	// they're set after the goroutines exit.
	if readErr != nil {
		t.Errorf("Couldn't get children: %v", readErr)
	}
	if writeErr != nil {
		t.Errorf("Couldn't write file: %v", writeErr)
	}
}

// mdRecordingKeyManager records the last *RootMetadata argument seen
// in its KeyManager methods.
type mdRecordingKeyManager struct {
	lastMD   *RootMetadata
	lastMDMu sync.RWMutex
	delegate KeyManager
}

func (km *mdRecordingKeyManager) getLastMD() *RootMetadata {
	km.lastMDMu.RLock()
	defer km.lastMDMu.RUnlock()
	return km.lastMD
}

func (km *mdRecordingKeyManager) setLastMD(md *RootMetadata) {
	km.lastMDMu.Lock()
	defer km.lastMDMu.Unlock()
	km.lastMD = md
}

func (km *mdRecordingKeyManager) GetTLFCryptKeyForEncryption(
	ctx context.Context, md *RootMetadata) (TLFCryptKey, error) {
	km.setLastMD(md)
	return km.delegate.GetTLFCryptKeyForEncryption(ctx, md)
}

func (km *mdRecordingKeyManager) GetTLFCryptKeyForMDDecryption(
	ctx context.Context, mdToDecrypt, mdWithKeys *RootMetadata) (
	TLFCryptKey, error) {
	km.setLastMD(mdToDecrypt)
	return km.delegate.GetTLFCryptKeyForMDDecryption(ctx,
		mdToDecrypt, mdWithKeys)
}

func (km *mdRecordingKeyManager) GetTLFCryptKeyForBlockDecryption(
	ctx context.Context, md *RootMetadata, blockPtr BlockPointer) (
	TLFCryptKey, error) {
	km.setLastMD(md)
	return km.delegate.GetTLFCryptKeyForBlockDecryption(ctx, md, blockPtr)
}

func (km *mdRecordingKeyManager) Rekey(
	ctx context.Context, md *RootMetadata, promptPaper bool) (
	bool, *TLFCryptKey, error) {
	km.setLastMD(md)
	return km.delegate.Rekey(ctx, md, promptPaper)
}

// Test that a sync can happen concurrently with a write. This is a
// regression test for KBFS-558.
func TestKBFSOpsConcurBlockSyncWrite(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer config.Shutdown()

	km := &mdRecordingKeyManager{delegate: config.KeyManager()}

	config.SetKeyManager(km)

	// Turn off block caching.
	config.SetBlockCache(NewBlockCacheStandard(config, 0, 1<<30))

	// Create a file.
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Write to file to mark it dirty.
	data := []byte{1}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Fatalf("Couldn't write to file: %v", err)
	}

	lState := makeFBOLockState()

	fbo := kbfsOps.(*KBFSOpsStandard).getOpsNoAdd(rootNode.GetFolderBranch())
	if fbo.blocks.GetState(lState) != dirtyState {
		t.Fatal("Unexpectedly not in dirty state")
	}

	// We only need to know the first time we stall.
	onSyncStalledCh := make(chan struct{}, 1)
	syncUnstallCh := make(chan struct{})

	stallKey := "requestName"
	syncValue := "sync"

	config.SetBlockOps(&stallingBlockOps{
		stallOpName: "Get",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			syncValue: staller{
				stalled: onSyncStalledCh,
				unstall: syncUnstallCh,
			},
		},
		internalDelegate: config.BlockOps(),
	})

	var wg sync.WaitGroup

	// Start the sync and wait for it to stall (on getting the dir
	// block).
	wg.Add(1)
	var syncErr error
	go func() {
		defer wg.Done()

		syncCtx := context.WithValue(ctx, stallKey, syncValue)
		syncErr = kbfsOps.Sync(syncCtx, fileNode)
	}()
	<-onSyncStalledCh

	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	deferredWriteCount := fbo.blocks.getDeferredWriteCountForTest(lState)
	if deferredWriteCount != 1 {
		t.Errorf("Unexpected deferred write count %d",
			deferredWriteCount)
	}

	// Unstall the sync.
	close(syncUnstallCh)

	wg.Wait()

	// Do this in the main goroutine since t isn't goroutine safe,
	// and do this after wg.Wait() since we only know it's set
	// after the goroutine exits.
	if syncErr != nil {
		t.Errorf("Couldn't sync: %v", syncErr)
	}

	md, err := fbo.getMDLocked(ctx, lState, mdReadNeedIdentify)
	if err != nil {
		t.Errorf("Couldn't get MD: %v", err)
	}

	lastMD := km.getLastMD()

	if md != lastMD {
		t.Error("Last MD seen by key manager != head")
	}
}

// Test that a sync can happen concurrently with a truncate. This is a
// regression test for KBFS-558.
func TestKBFSOpsConcurBlockSyncTruncate(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	km := &mdRecordingKeyManager{delegate: config.KeyManager()}

	config.SetKeyManager(km)

	// Turn off block caching.
	config.SetBlockCache(NewBlockCacheStandard(config, 0, 1<<30))

	// Create a file.
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Write to file to mark it dirty.
	data := []byte{1}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Fatalf("Couldn't write to file: %v", err)
	}

	lState := makeFBOLockState()

	fbo := kbfsOps.(*KBFSOpsStandard).getOpsNoAdd(rootNode.GetFolderBranch())
	if fbo.blocks.GetState(lState) != dirtyState {
		t.Fatal("Unexpectedly not in dirty state")
	}

	// We only need to know the first time we stall.
	onSyncStalledCh := make(chan struct{}, 1)

	syncUnstallCh := make(chan struct{})

	stallKey := "requestName"
	syncValue := "sync"

	config.SetBlockOps(&stallingBlockOps{
		stallOpName: "Get",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			syncValue: staller{
				stalled: onSyncStalledCh,
				unstall: syncUnstallCh,
			},
		},
		internalDelegate: config.BlockOps(),
	})

	var wg sync.WaitGroup

	// Start the sync and wait for it to stall (on getting the dir
	// block).
	wg.Add(1)
	var syncErr error
	go func() {
		defer wg.Done()

		syncCtx := context.WithValue(ctx, stallKey, syncValue)
		syncErr = kbfsOps.Sync(syncCtx, fileNode)
	}()
	<-onSyncStalledCh

	err = kbfsOps.Truncate(ctx, fileNode, 0)
	if err != nil {
		t.Errorf("Couldn't truncate file: %v", err)
	}

	deferredWriteCount := fbo.blocks.getDeferredWriteCountForTest(lState)
	if deferredWriteCount != 1 {
		t.Errorf("Unexpected deferred write count %d",
			deferredWriteCount)
	}

	// Unstall the sync.
	close(syncUnstallCh)

	wg.Wait()

	// Do this in the main goroutine since t isn't goroutine safe,
	// and do this after wg.Wait() since we only know it's set
	// after the goroutine exits.
	if syncErr != nil {
		t.Errorf("Couldn't sync: %v", syncErr)
	}

	md, err := fbo.getMDLocked(ctx, lState, mdReadNeedIdentify)
	if err != nil {
		t.Errorf("Couldn't get MD: %v", err)
	}

	lastMD := km.getLastMD()

	if md != lastMD {
		t.Error("Last MD seen by key manager != head")
	}
}

// Test that a sync can happen concurrently with a read for a file
// large enough to have indirect blocks without messing anything
// up. This should pass with -race. This is a regression test for
// KBFS-537.
func TestKBFSOpsConcurBlockSyncReadIndirect(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer config.Shutdown()

	// Turn off block caching.
	config.SetBlockCache(NewBlockCacheStandard(config, 0, 1<<30))

	// Use the smallest block size possible.
	bsplitter, err := NewBlockSplitterSimple(20, 8*1024, config.Codec())
	if err != nil {
		t.Fatalf("Couldn't create block splitter: %v", err)
	}
	config.SetBlockSplitter(bsplitter)

	// Create a file.
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	// Write to file to make an indirect block.
	data := make([]byte, bsplitter.maxSize+1)
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Fatalf("Couldn't write to file: %v", err)
	}

	// Decouple the read context from the sync context.
	readCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Read in a loop in a separate goroutine until we encounter
	// an error or the test ends.
	c := make(chan struct{})
	go func() {
		defer close(c)
	outer:
		for {
			_, err := kbfsOps.Read(readCtx, fileNode, data, 0)
			select {
			case <-readCtx.Done():
				break outer
			default:
			}
			if err != nil {
				t.Fatalf("Couldn't read file: %v", err)
				break
			}
		}
	}()

	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Fatalf("Couldn't sync file: %v", err)
	}
	cancel()
	// Wait for the read loop to finish
	<-c
}

// Test that a write can survive a folder BlockPointer update
func TestKBFSOpsConcurWriteDuringFolderUpdate(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer config.Shutdown()

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	data := []byte{1}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// Now update the folder pointer in some other way
	_, _, err = kbfsOps.CreateFile(ctx, rootNode, "b", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Now sync the original file and see make sure the write survived
	if err := kbfsOps.Sync(ctx, fileNode); err != nil {
		t.Fatalf("Couldn't sync: %v", err)
	}

	de, err := kbfsOps.Stat(ctx, fileNode)
	if err != nil {
		t.Errorf("Couldn't stat file: %v", err)
	}
	if g, e := de.Size, len(data); g != uint64(e) {
		t.Errorf("Got wrong size %d; expected %d", g, e)
	}
}

// Test that a write can happen concurrently with a sync when there
// are multiple blocks in the file.
func TestKBFSOpsConcurWriteDuringSyncMultiBlocks(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onPutStalledCh, putUnstallCh, putCtx :=
		setStallingMDOpsForPut(ctx, config, true)

	// make blocks small
	config.BlockSplitter().(*BlockSplitterSimple).maxSize = 5

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	// 2 blocks worth of data
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// sync these initial blocks
	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Errorf("Couldn't do the first sync: %v", err)
	}

	// there should be 7 blocks at this point: the original root block
	// + 2 modifications (create + write), the top indirect file block
	// and a modification (write), and its two children blocks.
	numCleanBlocks := config.BlockCache().(*BlockCacheStandard).cleanTransient.Len()
	if numCleanBlocks != 7 {
		t.Errorf("Unexpected number of cached clean blocks: %d\n",
			numCleanBlocks)
	}

	// write to the first block
	b1data := []byte{11, 12}
	err = kbfsOps.Write(ctx, fileNode, b1data, 0)
	if err != nil {
		t.Errorf("Couldn't write 1st block of file: %v", err)
	}

	// start the sync
	errChan := make(chan error)
	go func() {
		errChan <- kbfsOps.Sync(putCtx, fileNode)
	}()

	// wait until Sync gets stuck at MDOps.Put()
	<-onPutStalledCh

	// now make sure we can write the second block of the file and see
	// the new bytes we wrote
	newData := []byte{20}
	err = kbfsOps.Write(ctx, fileNode, newData, 9)
	if err != nil {
		t.Errorf("Couldn't write data: %v\n", err)
	}

	// read the data back
	buf := make([]byte, 10)
	nr, err := kbfsOps.Read(ctx, fileNode, buf, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v\n", err)
	}
	expectedData := []byte{11, 12, 3, 4, 5, 6, 7, 8, 9, 20}
	if nr != 10 || !bytes.Equal(expectedData, buf) {
		t.Errorf("Got wrong data %v; expected %v", buf, expectedData)
	}

	// now unstall Sync and make sure there was no error
	close(putUnstallCh)
	err = <-errChan
	if err != nil {
		t.Errorf("Sync got an error: %v", err)
	}

	// finally, make sure we can still read it after the sync too
	// (even though the second write hasn't been sync'd yet)
	buf2 := make([]byte, 10)
	nr, err = kbfsOps.Read(ctx, fileNode, buf2, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v\n", err)
	}
	if nr != 10 || !bytes.Equal(expectedData, buf2) {
		t.Errorf("2nd read: Got wrong data %v; expected %v", buf2, expectedData)
	}

	// Final sync to clean up
	if err := kbfsOps.Sync(ctx, fileNode); err != nil {
		t.Errorf("Couldn't sync the final write")
	}
}

// Test that a write consisting of multiple blocks can be canceled
// before all blocks have been written.
func TestKBFSOpsConcurWriteParallelBlocksCanceled(t *testing.T) {
	if maxParallelBlockPuts <= 1 {
		t.Skip("Skipping because we are not putting blocks in parallel.")
	}
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	// give it a remote block server with a fake client
	fc := NewFakeBServerClient(config, nil, nil, nil)
	b := newBlockServerRemoteWithClient(config, fc)
	config.BlockServer().Shutdown()
	config.SetBlockServer(b)

	// make blocks small
	blockSize := int64(5)
	config.BlockSplitter().(*BlockSplitterSimple).maxSize = blockSize

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	// Two initial blocks, then maxParallelBlockPuts blocks that
	// will be processed but discarded, then three extra blocks
	// that will be ignored.
	initialBlocks := 2
	extraBlocks := 3
	totalFileBlocks := initialBlocks + maxParallelBlockPuts + extraBlocks
	var data []byte
	for i := int64(0); i < blockSize*int64(totalFileBlocks); i++ {
		data = append(data, byte(i))
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// now set a control channel, let a couple blocks go in, and then
	// cancel the context
	readyChan := make(chan struct{})
	goChan := make(chan struct{})
	finishChan := make(chan struct{})
	fc.readyChan = readyChan
	fc.goChan = goChan
	fc.finishChan = finishChan

	prevNBlocks := fc.numBlocks()
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		// let the first initialBlocks blocks through.
		for i := 0; i < initialBlocks; i++ {
			<-readyChan
		}

		for i := 0; i < initialBlocks; i++ {
			goChan <- struct{}{}
		}

		for i := 0; i < initialBlocks; i++ {
			<-finishChan
		}

		// Let each parallel block worker block on readyChan.
		for i := 0; i < maxParallelBlockPuts; i++ {
			<-readyChan
		}

		// Make sure all the workers are busy.
		select {
		case <-readyChan:
			t.Error("Worker unexpectedly ready")
		default:
		}

		cancel()
	}()

	err = kbfsOps.Sync(ctx, fileNode)
	if err != context.Canceled {
		t.Errorf("Sync did not get canceled error: %v", err)
	}
	nowNBlocks := fc.numBlocks()
	if nowNBlocks != prevNBlocks+2 {
		t.Errorf("Unexpected number of blocks; prev = %d, now = %d",
			prevNBlocks, nowNBlocks)
	}

	// Now clean up by letting the rest of the blocks through.
	for i := 0; i < maxParallelBlockPuts; i++ {
		<-finishChan
	}

	// Make sure there are no more workers, i.e. the extra blocks
	// aren't sent to the server.
	select {
	case <-readyChan:
		t.Error("Worker unexpectedly ready")
	default:
	}

	// As a regression for KBFS-635, test that a second sync succeeds,
	// and that future operations also succeed.
	fc.readyChan = nil
	fc.goChan = nil
	fc.finishChan = nil
	ctx = context.Background()
	if err := kbfsOps.Sync(ctx, fileNode); err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}

	if _, _, err := kbfsOps.CreateFile(ctx, rootNode, "b", false, NoEXCL); err != nil {
		t.Fatalf("Couldn't create file after sync: %v", err)
	}

	// Avoid checking state when using a fake block server.
	config.MDServer().Shutdown()
}

// Test that, when writing multiple blocks in parallel, one error will
// cancel the remaining puts.
func TestKBFSOpsConcurWriteParallelBlocksError(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	// give it a mock'd block server
	ctr := NewSafeTestReporter(t)
	mockCtrl := gomock.NewController(ctr)
	defer mockCtrl.Finish()
	defer ctr.CheckForFailures()
	b := NewMockBlockServer(mockCtrl)
	config.BlockServer().Shutdown()
	config.SetBlockServer(b)

	// from the folder creation, then 2 for file creation
	c := b.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any()).Times(3).Return(nil)
	b.EXPECT().ArchiveBlockReferences(gomock.Any(), gomock.Any(),
		gomock.Any()).AnyTimes().Return(nil)

	// make blocks small
	blockSize := int64(5)
	config.BlockSplitter().(*BlockSplitterSimple).maxSize = blockSize

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	// 15 blocks
	var data []byte
	fileBlocks := int64(15)
	for i := int64(0); i < blockSize*fileBlocks; i++ {
		data = append(data, byte(i))
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// let two blocks through and fail the third:
	c = b.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any()).Times(2).After(c).Return(nil)
	putErr := errors.New("This is a forced error on put")
	errPtrChan := make(chan BlockPointer)
	c = b.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any()).
		Do(func(ctx context.Context, id BlockID, tlfID TlfID,
			context BlockContext, buf []byte,
			serverHalf BlockCryptKeyServerHalf) {
			errPtrChan <- BlockPointer{
				ID:           id,
				BlockContext: context,
			}
		}).After(c).Return(putErr)
	// let the rest through
	proceedChan := make(chan struct{})
	b.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any()).AnyTimes().
		Do(func(ctx context.Context, id BlockID, tlfID TlfID,
			context BlockContext, buf []byte,
			serverHalf BlockCryptKeyServerHalf) {
			<-proceedChan
		}).After(c).Return(nil)
	b.EXPECT().Shutdown().AnyTimes()

	var errPtr BlockPointer
	go func() {
		errPtr = <-errPtrChan
		close(proceedChan)
	}()

	err = kbfsOps.Sync(ctx, fileNode)
	if err != putErr {
		t.Errorf("Sync did not get the expected error: %v", err)
	}

	// wait for proceedChan to close, so we know the errPtr has been set
	<-proceedChan

	// Make sure the error'd file didn't make it to the actual cache
	// -- it's still in the permanent cache because the file might
	// still be read or sync'd later.
	config.BlockCache().DeletePermanent(errPtr.ID)
	if _, err := config.BlockCache().Get(errPtr); err == nil {
		t.Errorf("Failed block put for %v left block in cache", errPtr)
	}

	// State checking won't happen on the mock block server since we
	// leave ourselves in a dirty state.
}

// Test that writes that happen on a multi-block file concurrently
// with a sync, which has to retry due to an archived block, works
// correctly.  Regression test for KBFS-700.
func TestKBFSOpsMultiBlockWriteDuringRetriedSync(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	// Use the smallest possible block size.
	bsplitter, err := NewBlockSplitterSimple(20, 8*1024, config.Codec())
	if err != nil {
		t.Fatalf("Couldn't create block splitter: %v", err)
	}
	config.SetBlockSplitter(bsplitter)

	// Stall on the first put
	onSyncStalledCh := make(chan struct{}, 1)
	syncUnstallCh := make(chan struct{})

	stallKey := "requestName"
	syncValue := "sync"

	config.SetBlockOps(&stallingBlockOps{
		stallOpName: "Put",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			syncValue: staller{
				stalled: onSyncStalledCh,
				unstall: syncUnstallCh,
			},
		},
		internalDelegate: config.BlockOps(),
	})

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	var data []byte
	// Write 2 blocks worth of data
	for i := 0; i < 30; i++ {
		data = append(data, byte(i))
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	err = kbfsOps.Sync(ctx, fileNode)
	if err != nil {
		t.Fatalf("First sync failed: %v", err)
	}

	// Remove that file, and wait for the archiving to complete
	err = kbfsOps.RemoveEntry(ctx, rootNode, "a")
	if err != nil {
		t.Fatalf("Couldn't remove file: %v", err)
	}

	err = kbfsOps.SyncFromServerForTesting(ctx, rootNode.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %v", err)
	}

	fileNode2, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Now write the identical first block and sync it.
	err = kbfsOps.Write(ctx, fileNode2, data[:20], 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// Sync the initial two data blocks
	errChan := make(chan error)
	// start the sync
	go func() {
		syncCtx := context.WithValue(ctx, stallKey, syncValue)
		errChan <- kbfsOps.Sync(syncCtx, fileNode2)
	}()
	<-onSyncStalledCh

	// Now write the second block.
	err = kbfsOps.Write(ctx, fileNode2, data[20:], 20)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	// Unstall the sync.
	close(syncUnstallCh)
	err = <-errChan
	if err != nil {
		t.Errorf("Sync got an error: %v", err)
	}

	// Final sync
	err = kbfsOps.Sync(ctx, fileNode2)
	if err != nil {
		t.Fatalf("Final sync failed: %v", err)
	}

	gotData := make([]byte, 30)
	nr, err := kbfsOps.Read(ctx, fileNode2, gotData, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v", err)
	}
	if nr != int64(len(gotData)) {
		t.Errorf("Only read %d bytes", nr)
	}
	if !bytes.Equal(data, gotData) {
		t.Errorf("Read wrong data.  Expected %v, got %v", data, gotData)
	}

	// Make sure there are no dirty blocks left at the end of the test.
	dbcs := config.DirtyBlockCache().(*DirtyBlockCacheStandard)
	numDirtyBlocks := len(dbcs.cache)
	if numDirtyBlocks != 0 {
		t.Errorf("%d dirty blocks left after final sync", numDirtyBlocks)
	}
}

// Test that a Sync that is canceled during a successful MD put works.
func TestKBFSOpsConcurCanceledSyncSucceeds(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	onPutStalledCh, putUnstallCh, putCtx :=
		setStallingMDOpsForPut(ctx, config, true)

	// Use the smallest possible block size.
	bsplitter, err := NewBlockSplitterSimple(20, 8*1024, config.Codec())
	if err != nil {
		t.Fatalf("Couldn't create block splitter: %v", err)
	}
	config.SetBlockSplitter(bsplitter)

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}
	data := make([]byte, 30)
	for i := 0; i < 30; i++ {
		data[i] = 1
	}
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	ops := getOps(config, rootNode.GetFolderBranch().Tlf)
	unpauseArchives := make(chan struct{})
	ops.fbm.archivePauseChan <- unpauseArchives

	// start the sync
	errChan := make(chan error)
	cancelCtx, cancel := context.WithCancel(putCtx)
	go func() {
		errChan <- kbfsOps.Sync(cancelCtx, fileNode)
	}()

	// wait until Sync gets stuck at MDOps.Put()
	<-onPutStalledCh
	cancel()
	close(putUnstallCh)

	// We expect a canceled error
	err = <-errChan
	if err != context.Canceled {
		t.Fatalf("No expected canceled error: %v", err)
	}

	// Know that the sync finished by grabbing the lock.
	lState := makeFBOLockState()
	ops.mdWriterLock.Lock(lState)
	ops.mdWriterLock.Unlock(lState)
	if len(ops.fbm.blocksToDeleteAfterError) == 0 {
		t.Fatalf("No blocks to delete after error")
	}

	// Flush the file.  This will result in conflict resolution, and
	// an extra copy of the file, but that's ok for now.
	if err := kbfsOps.Sync(ctx, fileNode); err != nil {
		t.Fatalf("Couldn't sync: %v", err)
	}

	unpauseArchives <- struct{}{}

	// The first put actually succeeded, so
	// SyncFromServerForTesting and make sure it worked.  This
	// should also finish removing any blocks that would be
	// removed.
	err = kbfsOps.SyncFromServerForTesting(ctx, rootNode.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %v", err)
	}

	gotData := make([]byte, 30)
	nr, err := kbfsOps.Read(ctx, fileNode, gotData, 0)
	if err != nil {
		t.Errorf("Couldn't read data: %v", err)
	}
	if nr != int64(len(gotData)) {
		t.Errorf("Only read %d bytes", nr)
	}
	if !bytes.Equal(data, gotData) {
		t.Errorf("Read wrong data.  Expected %v, got %v", data, gotData)
	}

	if len(ops.fbm.blocksToDeleteAfterError) > 0 {
		t.Fatalf("Blocks left to delete after sync: %v",
			ops.fbm.blocksToDeleteAfterError)
	}
}

// Test that truncating a block to a zero-contents block, for which a
// duplicate has previously been archived, works correctly after a
// cancel.  Regression test for KBFS-727.
func TestKBFSOpsTruncateWithDupBlockCanceled(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	// Cancel the first put
	onSyncStalledCh := make(chan struct{}, 1)
	syncUnstallCh := make(chan struct{})

	stallKey := "requestName"
	syncValue := "sync"

	config.SetBlockOps(&stallingBlockOps{
		stallOpName: "Put",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			syncValue: staller{
				stalled: onSyncStalledCh,
				unstall: syncUnstallCh,
			},
		},
		internalDelegate: config.BlockOps(),
	})

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	_, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Remove that file, and wait for the archiving to complete
	err = kbfsOps.RemoveEntry(ctx, rootNode, "a")
	if err != nil {
		t.Fatalf("Couldn't remove file: %v", err)
	}

	err = kbfsOps.SyncFromServerForTesting(ctx, rootNode.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %v", err)
	}

	fileNode2, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	var data []byte
	// Write some data
	for i := 0; i < 30; i++ {
		data = append(data, byte(i))
	}
	err = kbfsOps.Write(ctx, fileNode2, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	err = kbfsOps.Sync(ctx, fileNode2)
	if err != nil {
		t.Fatalf("First sync failed: %v", err)
	}

	// Now truncate and sync, canceling during the block puts
	err = kbfsOps.Truncate(ctx, fileNode2, 0)
	if err != nil {
		t.Errorf("Couldn't truncate file: %v", err)
	}

	// Sync the initial two data blocks
	errChan := make(chan error)
	// start the sync
	cancelCtx, cancel := context.WithCancel(ctx)
	go func() {
		syncCtx := context.WithValue(cancelCtx, stallKey, syncValue)
		errChan <- kbfsOps.Sync(syncCtx, fileNode2)
	}()
	<-onSyncStalledCh

	cancel()
	// Unstall the sync.
	close(syncUnstallCh)
	err = <-errChan
	if err != context.Canceled {
		t.Errorf("Sync got wrong error: %v", err)
	}

	// Final sync
	err = kbfsOps.Sync(ctx, fileNode2)
	if err != nil {
		t.Fatalf("Final sync failed: %v", err)
	}
}

type blockOpsOverQuota struct {
	BlockOps
}

func (booq *blockOpsOverQuota) Put(ctx context.Context, md *RootMetadata,
	blockPtr BlockPointer, readyBlockData ReadyBlockData) error {
	return BServerErrorOverQuota{
		Throttled: true,
	}
}

// Test that a quota error causes deferred writes to error.
// Regression test for KBFS-751.
func TestKBFSOpsErrorOnBlockedWriteDuringSync(t *testing.T) {
	config, _, ctx := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	// Cancel the first put
	onSyncStalledCh := make(chan struct{}, 1)
	syncUnstallCh := make(chan struct{})

	stallKey := "requestName"
	syncValue := "sync"

	realBlockOps := config.BlockOps()
	staller := &stallingBlockOps{
		stallOpName: "Put",
		stallKey:    stallKey,
		stallMap: map[interface{}]staller{
			syncValue: staller{
				stalled: onSyncStalledCh,
				unstall: syncUnstallCh,
			},
		},
		internalDelegate: realBlockOps,
	}

	config.SetBlockOps(staller)

	// create and write to a file
	rootNode := GetRootNodeOrBust(t, config, "test_user", false)

	kbfsOps := config.KBFSOps()
	fileNode, _, err := kbfsOps.CreateFile(ctx, rootNode, "a", false, NoEXCL)
	if err != nil {
		t.Fatalf("Couldn't create file: %v", err)
	}

	// Write over the dirty amount of data.  TODO: make this
	// configurable for a speedier test.
	dbcs := config.DirtyBlockCache().(*DirtyBlockCacheStandard)
	data := make([]byte, dbcs.minSyncBufferSize+1)
	err = kbfsOps.Write(ctx, fileNode, data, 0)
	if err != nil {
		t.Errorf("Couldn't write file: %v", err)
	}

	booq := &blockOpsOverQuota{BlockOps: staller.delegate()}
	staller.setDelegate(booq)

	// Block the Sync
	// Sync the initial two data blocks
	syncErrCh := make(chan error)
	go func() {
		syncCtx := context.WithValue(ctx, stallKey, syncValue)
		syncErrCh <- kbfsOps.Sync(syncCtx, fileNode)
	}()
	<-onSyncStalledCh

	// Now write more data which should get blocked
	newData := make([]byte, 1)
	writeErrCh := make(chan error)
	go func() {
		writeErrCh <- kbfsOps.Write(ctx, fileNode, newData, int64(len(data)))
	}()

	// Wait until the second write is blocked
	ops := getOps(config, rootNode.GetFolderBranch().Tlf)
	func() {
		lState := makeFBOLockState()
		filePath := ops.nodeCache.PathFromNode(fileNode)
		ops.blocks.blockLock.Lock(lState)
		defer ops.blocks.blockLock.Unlock(lState)
		df := ops.blocks.getOrCreateDirtyFileLocked(lState, filePath)
		// TODO: locking
		for len(df.errListeners) != 2 {
			ops.blocks.blockLock.Unlock(lState)
			runtime.Gosched()
			ops.blocks.blockLock.Lock(lState)
		}
	}()

	// Unblock the sync
	close(syncUnstallCh)

	// Both errors should be an OverQuota error
	syncErr := <-syncErrCh
	writeErr := <-writeErrCh
	if _, ok := syncErr.(BServerErrorOverQuota); !ok {
		t.Fatalf("Unexpected sync err: %v", syncErr)
	}
	if writeErr != syncErr {
		t.Fatalf("Unexpected write err: %v", writeErr)
	}
}

func TestKBFSOpsCancelGetFavorites(t *testing.T) {
	config, _, _ := kbfsOpsConcurInit(t, "test_user")
	defer CheckConfigAndShutdown(t, config)

	serverConn, conn := rpc.MakeConnectionForTest(t)
	daemon := newKeybaseDaemonRPCWithClient(
		nil,
		conn.GetClient(),
		logger.NewTestLogger(t))
	config.SetKeybaseDaemon(daemon)

	f := func(ctx context.Context) error {
		_, err := config.KBFSOps().GetFavorites(ctx)
		return err
	}
	testRPCWithCanceledContext(t, serverConn, f)
}
