// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"

	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

// TODO: Move this somewhere better.
type blockRefLocalStatus int

const (
	noBlockRef blockRefLocalStatus = iota
	liveBlockRef
	archivedBlockRef
)

type blockMemEntry struct {
	tlfID         TlfID
	blockData     []byte
	refs          map[BlockRefNonce]blockRefLocalStatus
	keyServerHalf BlockCryptKeyServerHalf
}

// BlockServerMemory implements the BlockServer interface by just
// storing blocks in memory.
type BlockServerMemory struct {
	log logger.Logger

	lock sync.RWMutex
	m    map[BlockID]blockMemEntry
}

var _ BlockServer = (*BlockServerMemory)(nil)

// NewBlockServerMemory constructs a new BlockServerMemory that stores
// its data in memory.
func NewBlockServerMemory(config Config) *BlockServerMemory {
	return &BlockServerMemory{
		config.MakeLogger("BSM"),
		sync.RWMutex{},
		make(map[BlockID]blockMemEntry),
	}
}

// Get implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerMemory.Get id=%s tlfID=%s context=%s",
		id, tlfID, context)
	b.lock.RLock()
	defer b.lock.RUnlock()

	entry, ok := b.m[id]
	if !ok {
		return nil, BlockCryptKeyServerHalf{}, BServerErrorBlockNonExistent{}
	}
	return entry.blockData, entry.keyServerHalf, nil
}

// Put implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) Put(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.log.CDebugf(ctx, "BlockServerMemory.Put id=%s tlfID=%s context=%s",
		id, tlfID, context)

	if context.GetCreator() != context.GetWriter() {
		return fmt.Errorf("Can't Put() a block with creator=%s != writer=%s",
			context.GetCreator(), context.GetWriter())
	}

	if context.GetRefNonce() != zeroBlockRefNonce {
		return fmt.Errorf("Can't Put() a block with a non-zero refnonce.")
	}

	entry := blockMemEntry{
		tlfID:         tlfID,
		blockData:     buf,
		refs:          make(map[BlockRefNonce]blockRefLocalStatus),
		keyServerHalf: serverHalf,
	}
	entry.refs[zeroBlockRefNonce] = liveBlockRef
	b.lock.Lock()
	defer b.lock.Unlock()

	b.m[id] = entry
	return nil
}

// AddBlockReference implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerMemory.AddBlockReference id=%s "+
		"tlfID=%s context=%s", id, tlfID, context)

	b.lock.Lock()
	defer b.lock.Unlock()

	entry, ok := b.m[id]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
			"exist and cannot be referenced.", id)}
	}

	// only add it if there's a non-archived reference
	for _, status := range entry.refs {
		if status == liveBlockRef {
			entry.refs[context.GetRefNonce()] = liveBlockRef
			b.m[id] = entry
			return nil
		}
	}
	return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
		"been archived and cannot be referenced.", id)}
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerMemory.RemoveBlockReference "+
		"tlfID=%s contexts=%v", tlfID, contexts)
	liveCounts = make(map[BlockID]int)
	for bid, refs := range contexts {
		count := func() int {
			b.lock.Lock()
			defer b.lock.Unlock()
			entry, ok := b.m[bid]
			if !ok {
				// This block is already gone; no error.
				return 0
			}

			for _, ref := range refs {
				delete(entry.refs, ref.GetRefNonce())
			}
			count := len(entry.refs)
			if count == 0 {
				delete(b.m, bid)
			} else {
				b.m[bid] = entry
			}
			return count
		}()
		liveCounts[bid] = count
	}
	return liveCounts, nil
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerMemory.ArchiveBlockReferences "+
		"tlfID=%s contexts=%v", tlfID, contexts)

	for id, idContexts := range contexts {
		for _, context := range idContexts {
			refNonce := context.GetRefNonce()
			err := func() error {
				b.lock.Lock()
				defer b.lock.Unlock()

				entry, ok := b.m[id]
				if !ok {
					return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
						"exist and cannot be archived.", id)}
				}

				_, ok = entry.refs[refNonce]
				if !ok {
					return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
						"doesn't exist and cannot be archived.", id, refNonce)}
				}

				entry.refs[refNonce] = archivedBlockRef
				b.m[id] = entry
				return nil
			}()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// getAll returns all the known block references, and should only be
// used during testing.
func (b *BlockServerMemory) getAll(tlfID TlfID) (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)
	b.lock.RLock()
	defer b.lock.RUnlock()

	for id, entry := range b.m {
		if entry.tlfID != tlfID {
			continue
		}
		res[id] = make(map[BlockRefNonce]blockRefLocalStatus)
		for ref, status := range entry.refs {
			res[id][ref] = status
		}
	}
	return res, nil
}

// Shutdown implements the BlockServer interface for BlockServerMemory.
func (b *BlockServerMemory) Shutdown() {}

// RefreshAuthToken implements the BlockServer interface for BlockServerMemory.
func (b *BlockServerMemory) RefreshAuthToken(_ context.Context) {}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) GetUserQuotaInfo(ctx context.Context) (info *UserQuotaInfo, err error) {
	// Return a dummy value here.
	return &UserQuotaInfo{Limit: 0x7FFFFFFFFFFFFFFF}, nil
}
