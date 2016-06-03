// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

// BlockServerLocal implements the BlockServer interface by just
// storing blocks in a local leveldb instance
type BlockServerLocal struct {
	config          Config
	log             logger.Logger
	makeStorageFunc func(tlfID TlfID) bserverLocalStorage
	shutdownFunc    func()

	tlfStorageLock sync.RWMutex
	tlfStorage     map[TlfID]bserverLocalStorage
}

var _ BlockServer = (*BlockServerLocal)(nil)

// newBlockServerLocal constructs a new BlockServerLocal that stores
// its data in the given directory.
func newBlockServerLocal(config Config,
	makeStorageFunc func(tlfID TlfID) bserverLocalStorage,
	shutdownFunc func()) (
	*BlockServerLocal, error) {
	bserv := &BlockServerLocal{
		config,
		config.MakeLogger("BSL"),
		makeStorageFunc,
		shutdownFunc,
		sync.RWMutex{},
		make(map[TlfID]bserverLocalStorage),
	}
	return bserv, nil
}

// NewBlockServerDir constructs a new BlockServerLocal that stores
// its data in the given directory.
func NewBlockServerDir(config Config, dirPath string) (
	*BlockServerLocal, error) {
	return newBlockServerLocal(
		config, func(tlfID TlfID) bserverLocalStorage {
			path := filepath.Join(dirPath, tlfID.String())
			return makeBserverFileStorage(config.Codec(), path)
		}, nil)
}

// NewBlockServerTempDir constructs a new BlockServerLocal that stores its
// data in a temp directory which is cleaned up on shutdown.
func NewBlockServerTempDir(config Config) (
	*BlockServerLocal, error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_bserver_tmp")
	if err != nil {
		return nil, err
	}
	return newBlockServerLocal(
		config, func(tlfID TlfID) bserverLocalStorage {
			path := filepath.Join(tempdir, tlfID.String())
			return makeBserverFileStorage(config.Codec(), path)
		}, func() {
			os.RemoveAll(tempdir)
		})
}

// NewBlockServerMemory constructs a new BlockServerLocal that stores
// its data in memory.
func NewBlockServerMemory(config Config) (*BlockServerLocal, error) {
	return newBlockServerLocal(
		config, func(tlfID TlfID) bserverLocalStorage {
			return makeBserverMemStorage()
		}, nil)
}

func (b *BlockServerLocal) getStorage(tlfID TlfID) bserverLocalStorage {
	storage := func() bserverLocalStorage {
		b.tlfStorageLock.RLock()
		defer b.tlfStorageLock.RUnlock()
		return b.tlfStorage[tlfID]
	}()

	if storage != nil {
		return storage
	}

	b.tlfStorageLock.Lock()
	defer b.tlfStorageLock.Unlock()
	storage = b.tlfStorage[tlfID]
	if storage != nil {
		return storage
	}

	storage = b.makeStorageFunc(tlfID)
	b.tlfStorage[tlfID] = storage
	return storage
}

// Get implements the BlockServer interface for BlockServerLocal
func (b *BlockServerLocal) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerLocal.Get id=%s uid=%s",
		id, context.GetWriter())
	entry, err := b.getStorage(tlfID).get(id)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}
	return entry.BlockData, entry.KeyServerHalf, nil
}

// Put implements the BlockServer interface for BlockServerLocal
func (b *BlockServerLocal) Put(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.log.CDebugf(ctx, "BlockServerLocal.Put id=%s uid=%s",
		id, context.GetWriter())

	if context.GetRefNonce() != zeroBlockRefNonce {
		return fmt.Errorf("Can't Put() a block with a non-zero refnonce.")
	}

	entry := blockEntry{
		BlockData:     buf,
		Refs:          make(map[BlockRefNonce]blockRefLocalStatus),
		KeyServerHalf: serverHalf,
	}
	entry.Refs[zeroBlockRefNonce] = liveBlockRef
	return b.getStorage(tlfID).put(id, entry)
}

// AddBlockReference implements the BlockServer interface for BlockServerLocal
func (b *BlockServerLocal) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	refNonce := context.GetRefNonce()
	b.log.CDebugf(ctx, "BlockServerLocal.AddBlockReference id=%s "+
		"refnonce=%s uid=%s", id,
		refNonce, context.GetWriter())

	return b.getStorage(tlfID).addReference(id, refNonce)
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerLocal
func (b *BlockServerLocal) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerLocal.RemoveBlockReference ")
	liveCounts = make(map[BlockID]int)
	for bid, refs := range contexts {
		for _, ref := range refs {
			count, err := b.getStorage(tlfID).removeReference(bid, ref.GetRefNonce())
			if err != nil {
				return liveCounts, err
			}
			existing, ok := liveCounts[bid]
			if !ok || existing > count {
				liveCounts[bid] = count
			}
		}
	}
	return liveCounts, nil
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerLocal
func (b *BlockServerLocal) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	for id, idContexts := range contexts {
		for _, context := range idContexts {
			refNonce := context.GetRefNonce()
			b.log.CDebugf(ctx, "BlockServerLocal.ArchiveBlockReference id=%s "+
				"refnonce=%s", id, refNonce)
			err := b.getStorage(tlfID).archiveReference(id, refNonce)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// getAll returns all the known block references, and should only be
// used during testing.
func (b *BlockServerLocal) getAll(tlfID TlfID) (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	return b.getStorage(tlfID).getAll()
}

// Shutdown implements the BlockServer interface for BlockServerLocal.
func (b *BlockServerLocal) Shutdown() {
	func() {
		b.tlfStorageLock.Lock()
		defer b.tlfStorageLock.Unlock()
		// Make further accesses panic.
		b.tlfStorage = nil
	}()

	if b.shutdownFunc != nil {
		b.shutdownFunc()
	}
}

// RefreshAuthToken implements the BlockServer interface for BlockServerLocal.
func (b *BlockServerLocal) RefreshAuthToken(_ context.Context) {}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerLocal
func (b *BlockServerLocal) GetUserQuotaInfo(ctx context.Context) (info *UserQuotaInfo, err error) {
	// Return a dummy value here.
	return &UserQuotaInfo{Limit: 0x7FFFFFFFFFFFFFFF}, nil
}
