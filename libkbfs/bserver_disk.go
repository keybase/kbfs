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

// BlockServerDisk implements the BlockServer interface by just
// storing blocks in a local leveldb instance
type BlockServerDisk struct {
	codec        Codec
	log          logger.Logger
	dirPath      string
	shutdownFunc func()

	tlfStorageLock sync.RWMutex
	tlfStorage     map[TlfID]*bserverFileStorage
}

var _ BlockServer = (*BlockServerDisk)(nil)

// newBlockServerDisk constructs a new BlockServerDisk that stores
// its data in the given directory.
func newBlockServerDisk(
	config Config, dirPath string, shutdownFunc func()) *BlockServerDisk {
	bserv := &BlockServerDisk{
		config.Codec(),
		config.MakeLogger("BSL"),
		dirPath,
		shutdownFunc,
		sync.RWMutex{},
		make(map[TlfID]*bserverFileStorage),
	}
	return bserv
}

// NewBlockServerDir constructs a new BlockServerDisk that stores
// its data in the given directory.
func NewBlockServerDir(config Config, dirPath string) *BlockServerDisk {
	return newBlockServerDisk(config, dirPath, nil)
}

// NewBlockServerTempDir constructs a new BlockServerDisk that stores its
// data in a temp directory which is cleaned up on shutdown.
func NewBlockServerTempDir(config Config) (*BlockServerDisk, error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_bserver_tmp")
	if err != nil {
		return nil, err
	}
	return newBlockServerDisk(config, tempdir, func() {
		os.RemoveAll(tempdir)
	}), nil
}

func (b *BlockServerDisk) getStorage(tlfID TlfID) *bserverFileStorage {
	storage := func() *bserverFileStorage {
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

	path := filepath.Join(b.dirPath, tlfID.String())
	storage = makeBserverFileStorage(b.codec, path)
	b.tlfStorage[tlfID] = storage
	return storage
}

// Get implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerDisk.Get id=%s uid=%s",
		id, context.GetWriter())
	entry, err := b.getStorage(tlfID).get(id)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}
	return entry.BlockData, entry.KeyServerHalf, nil
}

// Put implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) Put(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.log.CDebugf(ctx, "BlockServerDisk.Put id=%s uid=%s",
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

// AddBlockReference implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	refNonce := context.GetRefNonce()
	b.log.CDebugf(ctx, "BlockServerDisk.AddBlockReference id=%s "+
		"refnonce=%s uid=%s", id,
		refNonce, context.GetWriter())

	return b.getStorage(tlfID).addReference(id, refNonce)
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerDisk
func (b *BlockServerDisk) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerDisk.RemoveBlockReference ")
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
// BlockServerDisk
func (b *BlockServerDisk) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	for id, idContexts := range contexts {
		for _, context := range idContexts {
			refNonce := context.GetRefNonce()
			b.log.CDebugf(ctx, "BlockServerDisk.ArchiveBlockReference id=%s "+
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
func (b *BlockServerDisk) getAll(tlfID TlfID) (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	return b.getStorage(tlfID).getAll(tlfID)
}

// Shutdown implements the BlockServer interface for BlockServerDisk.
func (b *BlockServerDisk) Shutdown() {
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

// RefreshAuthToken implements the BlockServer interface for BlockServerDisk.
func (b *BlockServerDisk) RefreshAuthToken(_ context.Context) {}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) GetUserQuotaInfo(ctx context.Context) (info *UserQuotaInfo, err error) {
	// Return a dummy value here.
	return &UserQuotaInfo{Limit: 0x7FFFFFFFFFFFFFFF}, nil
}
