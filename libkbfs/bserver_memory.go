// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"

	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

// BlockServerMemory implements the BlockServer interface by just
// storing blocks in memory.
type BlockServerMemory struct {
	log logger.Logger
	s   *bserverMemStorage
}

var _ BlockServer = (*BlockServerMemory)(nil)

// NewBlockServerMemory constructs a new BlockServerMemory that stores
// its data in memory.
func NewBlockServerMemory(config Config) *BlockServerMemory {
	return &BlockServerMemory{
		config.MakeLogger("BSM"),
		makeBserverMemStorage(),
	}
}

// Get implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerMemory.Get id=%s uid=%s",
		id, context.GetWriter())
	entry, err := b.s.get(id)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}
	return entry.BlockData, entry.KeyServerHalf, nil
}

// Put implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) Put(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.log.CDebugf(ctx, "BlockServerMemory.Put id=%s uid=%s",
		id, context.GetWriter())

	if context.GetRefNonce() != zeroBlockRefNonce {
		return fmt.Errorf("Can't Put() a block with a non-zero refnonce.")
	}

	entry := blockEntry{
		TlfID:         tlfID,
		BlockData:     buf,
		Refs:          make(map[BlockRefNonce]blockRefLocalStatus),
		KeyServerHalf: serverHalf,
	}
	entry.Refs[zeroBlockRefNonce] = liveBlockRef
	return b.s.put(id, entry)
}

// AddBlockReference implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	refNonce := context.GetRefNonce()
	b.log.CDebugf(ctx, "BlockServerMemory.AddBlockReference id=%s "+
		"refnonce=%s uid=%s", id,
		refNonce, context.GetWriter())

	return b.s.addReference(id, refNonce)
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerMemory.RemoveBlockReference ")
	liveCounts = make(map[BlockID]int)
	for bid, refs := range contexts {
		for _, ref := range refs {
			count, err := b.s.removeReference(bid, ref.GetRefNonce())
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
// BlockServerMemory
func (b *BlockServerMemory) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	for id, idContexts := range contexts {
		for _, context := range idContexts {
			refNonce := context.GetRefNonce()
			b.log.CDebugf(ctx, "BlockServerMemory.ArchiveBlockReference id=%s "+
				"refnonce=%s", id, refNonce)
			err := b.s.archiveReference(id, refNonce)
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
	return b.s.getAll(tlfID)
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
