// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"golang.org/x/net/context"
)

// MDServerDisk just stores blocks in local leveldb instances.
type MDServerDisk struct {
	codec    Codec
	clock    Clock
	crypto   Crypto
	kbpki    KBPKI
	handleDb *leveldb.DB // folder handle -> folderId
	log      logger.Logger
	dirPath  string

	tlfStorageLock *sync.RWMutex
	tlfStorage     map[TlfID]*mdServerTlfStorage

	updateManager *mdServerLocalUpdateManager

	shutdownLock *sync.RWMutex
	shutdown     *bool
	shutdownFunc func(logger.Logger)
}

var _ mdServerLocal = (*MDServerDisk)(nil)

func newMDServerDisk(config Config, dirPath string,
	shutdownFunc func(logger.Logger)) (*MDServerDisk, error) {
	handlePath := filepath.Join(dirPath, "handles")

	handleStorage, err := storage.OpenFile(handlePath)
	if err != nil {
		return nil, err
	}

	handleDb, err := leveldb.Open(handleStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	log := config.MakeLogger("")
	mdserv := &MDServerDisk{config.Codec(), config.Clock(),
		config.Crypto(), config.KBPKI(), handleDb, log, dirPath,
		&sync.RWMutex{}, make(map[TlfID]*mdServerTlfStorage),
		newMDServerLocalUpdateManager(),
		&sync.RWMutex{}, new(bool), shutdownFunc}
	return mdserv, nil
}

// NewMDServerDir constructs a new MDServerDisk that stores its data
// in the given directory.
func NewMDServerDir(config Config, dirPath string) (*MDServerDisk, error) {
	return newMDServerDisk(config, dirPath, nil)
}

// NewMDServerTempDir constructs a new MDServerDisk that stores its
// data in a temp directory which is cleaned up on shutdown.
func NewMDServerTempDir(config Config) (*MDServerDisk, error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_mdserver_tmp")
	if err != nil {
		return nil, err
	}
	return newMDServerDisk(config, tempdir, func(log logger.Logger) {
		err := os.RemoveAll(tempdir)
		if err != nil {
			log.Warning("error removing %s: %s", tempdir, err)
		}
	})
}

var errMDServerDiskShutdown = errors.New("MDServerDisk is shutdown")

func (md *MDServerDisk) getStorage(tlfID TlfID) (*mdServerTlfStorage, error) {
	storage, err := func() (*mdServerTlfStorage, error) {
		md.tlfStorageLock.RLock()
		defer md.tlfStorageLock.RUnlock()
		if md.tlfStorage == nil {
			return nil, errMDServerDiskShutdown
		}
		return md.tlfStorage[tlfID], nil
	}()

	if err != nil {
		return nil, err
	}

	if storage != nil {
		return storage, nil
	}

	md.tlfStorageLock.Lock()
	defer md.tlfStorageLock.Unlock()
	if md.tlfStorage == nil {
		return nil, errMDServerDiskShutdown
	}

	storage = md.tlfStorage[tlfID]
	if storage != nil {
		return storage, nil
	}

	path := filepath.Join(md.dirPath, tlfID.String())
	storage, err = makeMDServerTlfStorage(
		md.codec, md.clock, md.crypto, path)
	if err != nil {
		return nil, err
	}

	md.tlfStorage[tlfID] = storage
	return storage, nil
}

// GetForHandle implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) GetForHandle(ctx context.Context, handle BareTlfHandle,
	mStatus MergeStatus) (TlfID, *RootMetadataSigned, error) {
	id := NullTlfID
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return id, nil, errors.New("MD server already shut down")
	}

	handleBytes, err := md.codec.Encode(handle)
	if err != nil {
		return id, nil, err
	}

	buf, err := md.handleDb.Get(handleBytes, nil)
	if err != nil && err != leveldb.ErrNotFound {
		return id, nil, MDServerError{err}
	}
	if err == nil {
		var id TlfID
		err := id.UnmarshalBinary(buf)
		if err != nil {
			return NullTlfID, nil, err
		}
		rmds, err := md.GetForTLF(ctx, id, NullBranchID, mStatus)
		return id, rmds, err
	}

	// Non-readers shouldn't be able to create the dir.
	_, uid, err := md.kbpki.GetCurrentUserInfo(ctx)
	if err != nil {
		return id, nil, err
	}
	if !handle.IsReader(uid) {
		return id, nil, MDServerErrorUnauthorized{}
	}

	// Allocate a new random ID.
	id, err = md.crypto.MakeRandomTlfID(handle.IsPublic())
	if err != nil {
		return id, nil, MDServerError{err}
	}

	err = md.handleDb.Put(handleBytes, id.Bytes(), nil)
	if err != nil {
		return id, nil, MDServerError{err}
	}
	return id, nil, nil
}

// GetForTLF implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) GetForTLF(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus) (*RootMetadataSigned, error) {
	tlfStorage, err := md.getStorage(id)
	if err != nil {
		return nil, err
	}

	return tlfStorage.getForTLF(ctx, md.kbpki, bid, mStatus)
}

// GetRange implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) GetRange(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus, start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	md.log.CDebugf(ctx, "GetRange %d %d (%s)", start, stop, mStatus)

	tlfStorage, err := md.getStorage(id)
	if err != nil {
		return nil, err
	}

	return tlfStorage.getRange(ctx, md.kbpki, bid, mStatus, start, stop)
}

// Put implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) Put(ctx context.Context, rmds *RootMetadataSigned) error {
	tlfStorage, err := md.getStorage(rmds.MD.ID)
	if err != nil {
		return err
	}

	err = tlfStorage.put(ctx, md.kbpki, rmds)
	if err != nil {
		return err
	}

	mStatus := rmds.MD.MergedStatus()
	if mStatus == Merged &&
		// Don't send notifies if it's just a rekey (the real mdserver
		// sends a "folder needs rekey" notification in this case).
		!(rmds.MD.IsRekeySet() && rmds.MD.IsWriterMetadataCopiedSet()) {
		md.updateManager.setHead(rmds.MD.ID, md)
	}

	return nil
}

// PruneBranch implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) PruneBranch(ctx context.Context, id TlfID, bid BranchID) error {
	tlfStorage, err := md.getStorage(id)
	if err != nil {
		return err
	}

	return tlfStorage.pruneBranch(ctx, md.kbpki, bid)
}

func (md *MDServerDisk) getCurrentMergedHeadRevision(
	ctx context.Context, id TlfID) (rev MetadataRevision, err error) {
	head, err := md.GetForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return 0, err
	}
	if head != nil {
		rev = head.MD.Revision
	}
	return
}

// RegisterForUpdate implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) RegisterForUpdate(ctx context.Context, id TlfID,
	currHead MetadataRevision) (<-chan error, error) {
	// are we already past this revision?  If so, fire observer
	// immediately
	currMergedHeadRev, err := md.getCurrentMergedHeadRevision(ctx, id)
	if err != nil {
		return nil, err
	}

	c := md.updateManager.registerForUpdate(id, currHead, currMergedHeadRev, md)
	return c, nil
}

// TruncateLock implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) TruncateLock(ctx context.Context, id TlfID) (
	bool, error) {
	tlfStorage, err := md.getStorage(id)
	if err != nil {
		return false, err
	}

	return tlfStorage.truncateLock(ctx, md.kbpki)
}

// TruncateUnlock implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) TruncateUnlock(ctx context.Context, id TlfID) (
	bool, error) {
	tlfStorage, err := md.getStorage(id)
	if err != nil {
		return false, err
	}

	return tlfStorage.truncateUnlock(ctx, md.kbpki)
}

// Shutdown implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) Shutdown() {
	md.shutdownLock.Lock()
	defer md.shutdownLock.Unlock()
	if *md.shutdown {
		return
	}
	*md.shutdown = true

	tlfStorage := func() map[TlfID]*mdServerTlfStorage {
		md.tlfStorageLock.Lock()
		defer md.tlfStorageLock.Unlock()
		// Make further accesses error out.
		tlfStorage := md.tlfStorage
		md.tlfStorage = nil
		return tlfStorage
	}()

	for _, s := range tlfStorage {
		s.shutdown()
	}

	if md.handleDb != nil {
		md.handleDb.Close()
	}

	if md.shutdownFunc != nil {
		md.shutdownFunc(md.log)
	}
}

// IsConnected implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) IsConnected() bool {
	return !md.isShutdown()
}

// RefreshAuthToken implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) RefreshAuthToken(ctx context.Context) {}

// This should only be used for testing with an in-memory server.
func (md *MDServerDisk) copy(config Config) mdServerLocal {
	// NOTE: observers and sessionHeads are copied shallowly on
	// purpose, so that the MD server that gets a Put will notify all
	// observers correctly no matter where they got on the list.
	log := config.MakeLogger("")
	return &MDServerDisk{md.codec, md.clock, md.crypto, config.KBPKI(),
		md.handleDb, log, md.dirPath, md.tlfStorageLock,
		md.tlfStorage, md.updateManager,
		md.shutdownLock, md.shutdown, md.shutdownFunc}
}

// isShutdown returns whether the logical, shared MDServer instance
// has been shut down.
func (md *MDServerDisk) isShutdown() bool {
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	return *md.shutdown
}

// DisableRekeyUpdatesForTesting implements the MDServer interface.
func (md *MDServerDisk) DisableRekeyUpdatesForTesting() {
	// Nothing to do.
}

// CheckForRekeys implements the MDServer interface.
func (md *MDServerDisk) CheckForRekeys(ctx context.Context) <-chan error {
	// Nothing to do
	c := make(chan error, 1)
	c <- nil
	return c
}

func (md *MDServerDisk) addNewAssertionForTest(uid keybase1.UID,
	newAssertion keybase1.SocialAssertion) error {
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return errors.New("MD server already shut down")
	}

	// Iterate through all the handles, and add handles for ones
	// containing newAssertion to now include the uid.
	iter := md.handleDb.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		handleBytes := iter.Key()
		var handle BareTlfHandle
		err := md.codec.Decode(handleBytes, &handle)
		if err != nil {
			return err
		}
		assertions := map[keybase1.SocialAssertion]keybase1.UID{
			newAssertion: uid,
		}
		newHandle := handle.ResolveAssertions(assertions)
		if reflect.DeepEqual(handle, newHandle) {
			continue
		}
		newHandleBytes, err := md.codec.Encode(newHandle)
		if err != nil {
			return err
		}
		id := iter.Value()
		if err := md.handleDb.Put(newHandleBytes, id, nil); err != nil {
			return err
		}
	}
	return iter.Error()
}

// GetLatestHandleForTLF implements the MDServer interface for MDServerDisk.
func (md *MDServerDisk) GetLatestHandleForTLF(_ context.Context, id TlfID) (
	BareTlfHandle, error) {
	var handle BareTlfHandle
	iter := md.handleDb.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		var dbID TlfID
		idBytes := iter.Value()
		err := dbID.UnmarshalBinary(idBytes)
		if err != nil {
			return BareTlfHandle{}, err
		}
		if id != dbID {
			continue
		}
		handleBytes := iter.Key()
		handle = BareTlfHandle{}
		err = md.codec.Decode(handleBytes, &handle)
		if err != nil {
			return BareTlfHandle{}, err
		}
	}
	return handle, nil
}
