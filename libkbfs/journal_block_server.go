// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "golang.org/x/net/context"

type journalBlockServer struct {
	jServer *JournalServer
	BlockServer
}

var _ BlockServer = journalBlockServer{}

func (j journalBlockServer) Get(
	ctx context.Context, id BlockID, tlfID TlfID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.Get(ctx, id, tlfID, context)
}

func (j journalBlockServer) Put(
	ctx context.Context, id BlockID, tlfID TlfID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.Put(ctx, id, tlfID, context, buf, serverHalf)
}

func (j journalBlockServer) AddBlockReference(
	ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.AddBlockReference(ctx, id, tlfID, context)
}

func (j journalBlockServer) RemoveBlockReference(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.RemoveBlockReference(ctx, tlfID, contexts)
}

func (j journalBlockServer) ArchiveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.ArchiveBlockReferences(ctx, tlfID, contexts)
}
