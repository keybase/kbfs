// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"sync"

	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

type blockRefEntry struct {
	// These fields are exported only for serialization purposes.
	Status  blockRefLocalStatus
	Context BlockContext
}

type blockMemEntry struct {
	tlfID         TlfID
	blockData     []byte
	keyServerHalf BlockCryptKeyServerHalf
	refs          map[BlockRefNonce]blockRefEntry
}

// BlockServerMemory implements the BlockServer interface by just
// storing blocks in memory.
type BlockServerMemory struct {
	crypto Crypto
	log    logger.Logger

	lock sync.RWMutex
	// m is nil after Shutdown() is called.
	m map[BlockID]blockMemEntry
}

var _ BlockServer = (*BlockServerMemory)(nil)

// NewBlockServerMemory constructs a new BlockServerMemory that stores
// its data in memory.
func NewBlockServerMemory(config Config) *BlockServerMemory {
	return &BlockServerMemory{
		config.Crypto(),
		config.MakeLogger("BSM"),
		sync.RWMutex{},
		make(map[BlockID]blockMemEntry),
	}
}

var blockServerMemoryShutdownErr = errors.New("BlockServerMemory is shutdown")

// Get implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerMemory.Get id=%s tlfID=%s context=%s",
		id, tlfID, context)
	b.lock.RLock()
	defer b.lock.RUnlock()

	if b.m == nil {
		return nil, BlockCryptKeyServerHalf{}, blockServerMemoryShutdownErr
	}

	entry, ok := b.m[id]
	if !ok {
		return nil, BlockCryptKeyServerHalf{}, BServerErrorBlockNonExistent{}
	}

	if entry.tlfID != tlfID {
		return nil, BlockCryptKeyServerHalf{},
			fmt.Errorf("TLF ID mismatch: expected %s, got %s",
				entry.tlfID, tlfID)
	}

	refEntry, ok := entry.refs[context.GetRefNonce()]
	if refEntry.Context != context {
		return nil, BlockCryptKeyServerHalf{},
			fmt.Errorf("Context mismatch: expected %s, got %s",
				refEntry.Context, context)
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

	bufID, err := b.crypto.MakePermanentBlockID(buf)
	if err != nil {
		return err
	}

	if bufID != id {
		return fmt.Errorf(
			"Block ID mismatch: expected %s, got %s", bufID, id)
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	if b.m == nil {
		return blockServerMemoryShutdownErr
	}

	if entry, ok := b.m[id]; ok {
		if entry.tlfID != tlfID {
			return fmt.Errorf(
				"TLF ID mismatch: expected %s, got %s",
				entry.tlfID, tlfID)
		}

		// TODO: Is it okay for the creator to change?
		if refEntry, ok := entry.refs[zeroBlockRefNonce]; ok && refEntry.Context != context {
			return fmt.Errorf("Context mismatch: expected %s, got %s",
				refEntry.Context, context)
		}

		// The only thing that could be different now is the
		// key server half.
		//
		// TODO: Figure out whether it can actually be
		// different.
		entry.keyServerHalf = serverHalf
		return nil
	}

	data := make([]byte, len(buf))
	copy(data, buf)
	b.m[id] = blockMemEntry{
		tlfID:     tlfID,
		blockData: data,
		refs: map[BlockRefNonce]blockRefEntry{
			zeroBlockRefNonce: blockRefEntry{
				Status:  liveBlockRef,
				Context: context,
			},
		},
		keyServerHalf: serverHalf,
	}
	return nil
}

// AddBlockReference implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerMemory.AddBlockReference id=%s "+
		"tlfID=%s context=%s", id, tlfID, context)

	b.lock.Lock()
	defer b.lock.Unlock()

	if b.m == nil {
		return blockServerMemoryShutdownErr
	}

	entry, ok := b.m[id]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
			"exist and cannot be referenced.", id)}
	}

	if entry.tlfID != tlfID {
		return fmt.Errorf("TLF ID mismatch: expected %s, got %s",
			entry.tlfID, tlfID)
	}

	// Only add it if there's a non-archived reference.
	hasNonArchivedRef := false
	for _, refEntry := range entry.refs {
		if refEntry.Status == liveBlockRef {
			hasNonArchivedRef = true
			break
		}
	}
	if !hasNonArchivedRef {
		return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
			"been archived and cannot be referenced.", id)}
	}

	refNonce := context.GetRefNonce()
	if refEntry, ok := entry.refs[refNonce]; ok && refEntry.Context != context {
		return fmt.Errorf("Context mismatch: expected %s, got %s",
			refEntry.Context, context)
	}

	entry.refs[refNonce] = blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	}
	return nil
}

func (b *BlockServerMemory) removeBlockReferences(
	id BlockID, tlfID TlfID, contexts []BlockContext) (int, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.m == nil {
		return 0, blockServerMemoryShutdownErr
	}

	entry, ok := b.m[id]
	if !ok {
		// This block is already gone; no error.
		return 0, nil
	}

	if entry.tlfID != tlfID {
		return 0, fmt.Errorf("TLF ID mismatch: expected %s, got %s",
			entry.tlfID, tlfID)
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		// If this check fails, this ref is already gone,
		// which is not an error.
		if refEntry, ok := entry.refs[refNonce]; ok {
			if refEntry.Context != context {
				return 0, fmt.Errorf(
					"Context mismatch: expected %s, got %s",
					refEntry.Context, context)
			}
			delete(entry.refs, refNonce)
		}
	}
	count := len(entry.refs)
	if count == 0 {
		delete(b.m, id)
	}
	return count, nil
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerMemory.RemoveBlockReference "+
		"tlfID=%s contexts=%v", tlfID, contexts)
	liveCounts = make(map[BlockID]int)
	for id, idContexts := range contexts {
		count, err := b.removeBlockReferences(id, tlfID, idContexts)
		if err != nil {
			return nil, err
		}
		liveCounts[id] = count
	}
	return liveCounts, nil
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) archiveBlockReference(
	id BlockID, tlfID TlfID, context BlockContext) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.m == nil {
		return blockServerMemoryShutdownErr
	}

	entry, ok := b.m[id]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
			"exist and cannot be archived.", id)}
	}

	if entry.tlfID != tlfID {
		return fmt.Errorf("TLF ID mismatch: expected %s, got %s",
			entry.tlfID, tlfID)
	}

	refNonce := context.GetRefNonce()
	refEntry, ok := entry.refs[refNonce]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
			"doesn't exist and cannot be archived.", id, refNonce)}
	}

	if refEntry.Context != context {
		return fmt.Errorf("Context mismatch: expected %s, got %s",
			refEntry.Context, context)
	}

	refEntry.Status = archivedBlockRef
	entry.refs[refNonce] = refEntry
	return nil
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerMemory
func (b *BlockServerMemory) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerMemory.ArchiveBlockReferences "+
		"tlfID=%s contexts=%v", tlfID, contexts)

	for id, idContexts := range contexts {
		for _, context := range idContexts {
			err := b.archiveBlockReference(id, tlfID, context)
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

	if b.m == nil {
		return nil, blockServerMemoryShutdownErr
	}

	for id, entry := range b.m {
		if entry.tlfID != tlfID {
			continue
		}
		res[id] = make(map[BlockRefNonce]blockRefLocalStatus)
		for ref, refEntry := range entry.refs {
			res[id][ref] = refEntry.Status
		}
	}
	return res, nil
}

// Shutdown implements the BlockServer interface for BlockServerMemory.
func (b *BlockServerMemory) Shutdown() {
	b.lock.Lock()
	defer b.lock.Unlock()
	// Make further accesses error out.
	b.m = nil
}

// RefreshAuthToken implements the BlockServer interface for BlockServerMemory.
func (b *BlockServerMemory) RefreshAuthToken(_ context.Context) {}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerMemory
func (b *BlockServerMemory) GetUserQuotaInfo(ctx context.Context) (info *UserQuotaInfo, err error) {
	// Return a dummy value here.
	return &UserQuotaInfo{Limit: 0x7FFFFFFFFFFFFFFF}, nil
}
