// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "golang.org/x/net/context"

type journalBlockServer struct {
	jServer *JournalServer
	BlockServer
	enableAddBlockReference bool
}

var _ BlockServer = journalBlockServer{}

func (j journalBlockServer) Get(
	ctx context.Context, tlfID TlfID, id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	if bundle, ok := j.jServer.getBundle(tlfID); ok {
		data, serverHalf, err := bundle.getBlockDataWithContext(
			id, context)
		if _, ok := err.(BServerErrorBlockNonExistent); ok {
			return j.BlockServer.Get(ctx, tlfID, id, context)
		} else if err != nil {
			return nil, BlockCryptKeyServerHalf{}, err
		}

		return data, serverHalf, nil
	}

	return j.BlockServer.Get(ctx, tlfID, id, context)
}

func (j journalBlockServer) Put(
	ctx context.Context, tlfID TlfID, id BlockID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	if bundle, ok := j.jServer.getBundle(tlfID); ok {
		return bundle.putBlockData(ctx, id, context, buf, serverHalf)
	}

	return j.BlockServer.Put(ctx, tlfID, id, context, buf, serverHalf)
}

func (j journalBlockServer) AddBlockReference(
	ctx context.Context, tlfID TlfID, id BlockID,
	context BlockContext) error {
	if !j.enableAddBlockReference {
		// TODO: Temporarily return an error until KBFS-1149 is
		// fixed. This is needed despite
		// journalBlockCache.CheckForBlockPtr, since CheckForBlockPtr
		// may be called before journaling is turned on for a TLF.
		return BServerErrorBlockNonExistent{}
	}

	if bundle, ok := j.jServer.getBundle(tlfID); ok {
		return bundle.addBlockReference(ctx, id, context)
	}

	return j.BlockServer.AddBlockReference(ctx, tlfID, id, context)
}

func (j journalBlockServer) RemoveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	if bundle, ok := j.jServer.getBundle(tlfID); ok {
		// TODO: Get server counts without making a
		// RemoveBlockReferences call and merge it.
		return bundle.removeBlockReferences(ctx, contexts)
	}

	return j.BlockServer.RemoveBlockReferences(ctx, tlfID, contexts)
}

func (j journalBlockServer) ArchiveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) error {
	if bundle, ok := j.jServer.getBundle(tlfID); ok {
		return bundle.archiveBlockReferences(ctx, contexts)
	}

	return j.BlockServer.ArchiveBlockReferences(ctx, tlfID, contexts)
}

func (j journalBlockServer) Shutdown() {
	j.jServer.shutdown()
}
