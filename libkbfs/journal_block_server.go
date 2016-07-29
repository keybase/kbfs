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
	bundle, ok := j.jServer.getBundle(tlfID)
	if !ok {
		return j.BlockServer.Get(ctx, id, tlfID, context)
	}
	data, serverHalf, err := bundle.blockJournal.getData(id, context)
	if _, ok := err.(BServerErrorBlockNonExistent); ok {
		return j.BlockServer.Get(ctx, id, tlfID, context)
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	return data, serverHalf, nil
}

func (j journalBlockServer) Put(
	ctx context.Context, id BlockID, tlfID TlfID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	bundle, ok := j.jServer.getBundle(tlfID)
	if ok {
		return bundle.blockJournal.putData(
			id, context, buf, serverHalf)
	}

	return j.BlockServer.Put(ctx, id, tlfID, context, buf, serverHalf)
}

func (j journalBlockServer) AddBlockReference(
	ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	bundle, ok := j.jServer.getBundle(tlfID)
	if ok {
		return bundle.blockJournal.addReference(id, context)
	}

	return j.BlockServer.AddBlockReference(ctx, id, tlfID, context)
}

func (j journalBlockServer) RemoveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	_, ok := j.jServer.getBundle(tlfID)
	bundle, ok := j.jServer.getBundle(tlfID)
	if ok {
		for id, idContexts := range contexts {
			liveCount, err := bundle.blockJournal.removeReferences(
				id, idContexts)
			if err != nil {
				return nil, err
			}
			liveCounts[id] = liveCount
		}

		serverCounts, err := j.BlockServer.RemoveBlockReferences(
			ctx, tlfID, contexts)
		if err != nil {
			return nil, err
		}

		for id, serverCount := range serverCounts {
			liveCounts[id] += serverCount
		}
	}

	return j.BlockServer.RemoveBlockReferences(ctx, tlfID, contexts)
}

func (j journalBlockServer) ArchiveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	bundle, ok := j.jServer.getBundle(tlfID)
	if ok {
		for id, idContexts := range contexts {
			err := bundle.blockJournal.archiveReferences(
				id, idContexts)
			if err != nil {
				return err
			}
		}
		return nil
	}

	return j.BlockServer.ArchiveBlockReferences(ctx, tlfID, contexts)
}
