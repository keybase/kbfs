// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"
)

type tlfJournalBundle struct {
	// TODO: Fill in with a block journal and an MD journal.
}

type JournalServer struct {
	config Config

	log      logger.Logger
	deferLog logger.Logger

	dir string

	delegateBlockServer BlockServer
	delegateMDOps       MDOps

	lock       sync.RWMutex
	tlfBundles map[TlfID]*tlfJournalBundle
}

func (j *JournalServer) getBundle(tlfID TlfID) (*tlfJournalBundle, bool) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	bundle, ok := j.tlfBundles[tlfID]
	return bundle, ok
}

func (j *JournalServer) Enable(tlfID TlfID) (err error) {
	j.log.Debug("Enabling journal for %s", tlfID)
	defer func() {
		j.deferLog.Debug("Error when enabling journal for %s: %v",
			tlfID, err)
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	_, ok := j.tlfBundles[tlfID]
	if ok {
		j.log.Debug("Journal already enabled for %s", tlfID)
		return nil
	}

	j.tlfBundles[tlfID] = &tlfJournalBundle{}
	return nil
}

func (j *JournalServer) Flush(tlfID TlfID) (err error) {
	j.log.Debug("Flushing journal for %s", tlfID)
	defer func() {
		j.deferLog.Debug("Error when flushing journal for %s: %v",
			tlfID, err)
	}()
	_, ok := j.getBundle(tlfID)
	if !ok {
		j.log.Debug("Journal not enabled for %s", tlfID)
		return nil
	}

	flushedBlockEntries := 0
	// TODO: Flush block journal.

	flushedMDEntries := 0
	// TODO: Flush MD journal.

	j.log.Debug("Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, tlfID)

	return nil
}

type journalBlockServer struct {
	jServer *JournalServer
	BlockServer
}

var _ BlockServer = journalBlockServer{}

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

type journalMDOps struct {
	jServer *JournalServer
	MDOps
}

var _ MDOps = journalMDOps{}

func (j journalMDOps) Put(ctx context.Context, rmd *RootMetadata) error {
	_, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.Put(ctx, rmd)
}

func (j journalMDOps) PutUnmerged(
	ctx context.Context, rmd *RootMetadata, bid BranchID) error {
	_, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.PutUnmerged(ctx, rmd, bid)
}

func (j *JournalServer) blockServer() journalBlockServer {
	return journalBlockServer{j, j.delegateBlockServer}
}

func (j *JournalServer) mdOps() journalMDOps {
	return journalMDOps{j, j.delegateMDOps}
}

func makeJournalServer(
	config Config, log logger.Logger,
	dir string, bserver BlockServer, mdOps MDOps) *JournalServer {
	jServer := JournalServer{
		config:              config,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		dir:                 dir,
		delegateBlockServer: bserver,
		delegateMDOps:       mdOps,
		tlfBundles:          make(map[TlfID]*tlfJournalBundle),
	}
	return &jServer
}
