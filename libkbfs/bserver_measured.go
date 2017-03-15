// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"time"

	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	metrics "github.com/rcrowley/go-metrics"
	"golang.org/x/net/context"
	"golang.org/x/net/trace"
)

// BlockServerMeasured delegates to another BlockServer instance but
// also keeps track of stats.
type BlockServerMeasured struct {
	delegate                    BlockServer
	getTimer                    metrics.Timer
	putTimer                    metrics.Timer
	addBlockReferenceTimer      metrics.Timer
	removeBlockReferencesTimer  metrics.Timer
	archiveBlockReferencesTimer metrics.Timer
	isUnflushedTimer            metrics.Timer
}

var _ BlockServer = BlockServerMeasured{}

// NewBlockServerMeasured creates and returns a new
// BlockServerMeasured instance with the given delegate and registry.
func NewBlockServerMeasured(delegate BlockServer, r metrics.Registry) BlockServerMeasured {
	getTimer := metrics.GetOrRegisterTimer("BlockServer.Get", r)
	putTimer := metrics.GetOrRegisterTimer("BlockServer.Put", r)
	addBlockReferenceTimer := metrics.GetOrRegisterTimer("BlockServer.AddBlockReference", r)
	removeBlockReferencesTimer := metrics.GetOrRegisterTimer("BlockServer.RemoveBlockReferences", r)
	archiveBlockReferencesTimer := metrics.GetOrRegisterTimer("BlockServer.ArchiveBlockReferences", r)
	isUnflushedTimer := metrics.GetOrRegisterTimer("BlockServer.IsUnflushed", r)
	return BlockServerMeasured{
		delegate:                    delegate,
		getTimer:                    getTimer,
		putTimer:                    putTimer,
		addBlockReferenceTimer:      addBlockReferenceTimer,
		removeBlockReferencesTimer:  removeBlockReferencesTimer,
		archiveBlockReferencesTimer: archiveBlockReferencesTimer,
		isUnflushedTimer:            isUnflushedTimer,
	}
}

func timeAndLog(ctx context.Context,
	name string, timer metrics.Timer, f func()) {
	ts := time.Now()
	f()
	dt := time.Since(ts)
	timer.Update(dt)
	if tr, ok := trace.FromContext(ctx); ok {
		tr.LazyPrintf("%s took %f s", name, dt.Seconds())
	}
}

// Get implements the BlockServer interface for BlockServerMeasured.
func (b BlockServerMeasured) Get(ctx context.Context, tlfID tlf.ID, id kbfsblock.ID,
	context kbfsblock.Context) (
	buf []byte, serverHalf kbfscrypto.BlockCryptKeyServerHalf, err error) {
	timeAndLog(ctx, "BlockServer.Get", b.getTimer, func() {
		buf, serverHalf, err = b.delegate.Get(ctx, tlfID, id, context)
	})
	return buf, serverHalf, err
}

// Put implements the BlockServer interface for BlockServerMeasured.
func (b BlockServerMeasured) Put(ctx context.Context, tlfID tlf.ID, id kbfsblock.ID,
	context kbfsblock.Context, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf) (err error) {
	timeAndLog(ctx, "BlockServer.Put", b.putTimer, func() {
		err = b.delegate.Put(ctx, tlfID, id, context, buf, serverHalf)
	})
	return err
}

// AddBlockReference implements the BlockServer interface for
// BlockServerMeasured.
func (b BlockServerMeasured) AddBlockReference(ctx context.Context, tlfID tlf.ID,
	id kbfsblock.ID, context kbfsblock.Context) (err error) {
	timeAndLog(ctx, "BlockServer.AddBlockReference", b.addBlockReferenceTimer, func() {
		err = b.delegate.AddBlockReference(ctx, tlfID, id, context)
	})
	return err
}

// RemoveBlockReferences implements the BlockServer interface for
// BlockServerMeasured.
func (b BlockServerMeasured) RemoveBlockReferences(ctx context.Context,
	tlfID tlf.ID, contexts kbfsblock.ContextMap) (
	liveCounts map[kbfsblock.ID]int, err error) {
	timeAndLog(ctx, "BlockServer.RemoveBlockReferences", b.removeBlockReferencesTimer, func() {
		liveCounts, err = b.delegate.RemoveBlockReferences(
			ctx, tlfID, contexts)
	})
	return liveCounts, err
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerRemote
func (b BlockServerMeasured) ArchiveBlockReferences(ctx context.Context,
	tlfID tlf.ID, contexts kbfsblock.ContextMap) (err error) {
	timeAndLog(ctx, "BlockServer.ArchiveBlockReferences", b.archiveBlockReferencesTimer, func() {
		err = b.delegate.ArchiveBlockReferences(ctx, tlfID, contexts)
	})
	return err
}

// IsUnflushed implements the BlockServer interface for BlockServerMeasured.
func (b BlockServerMeasured) IsUnflushed(ctx context.Context, tlfID tlf.ID,
	id kbfsblock.ID) (isUnflushed bool, err error) {
	timeAndLog(ctx, "BlockServer.isUnflushed", b.isUnflushedTimer, func() {
		isUnflushed, err = b.delegate.IsUnflushed(ctx, tlfID, id)
	})
	return isUnflushed, err

}

// Shutdown implements the BlockServer interface for
// BlockServerMeasured.
func (b BlockServerMeasured) Shutdown(ctx context.Context) {
	b.delegate.Shutdown(ctx)
}

// RefreshAuthToken implements the BlockServer interface for
// BlockServerMeasured.
func (b BlockServerMeasured) RefreshAuthToken(ctx context.Context) {
	b.delegate.RefreshAuthToken(ctx)
}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerMeasured
func (b BlockServerMeasured) GetUserQuotaInfo(ctx context.Context) (info *kbfsblock.UserQuotaInfo, err error) {
	return b.delegate.GetUserQuotaInfo(ctx)
}
