// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"
)

type tlfJournalBundle struct {
	lock sync.RWMutex

	// TODO: Fill in with a block journal.
	mdJournal *mdJournal
}

// JournalServer is the server that handles write journals. It
// interposes itself in front of BlockServer and MDOps. It uses MDOps
// instead of MDServer because it has to potentially modify the
// RootMetadata passed in, and by the time it hits MDServer it's
// already too late. However, this assumes that all MD ops go through
// MDOps.
type JournalServer struct {
	config Config

	log      logger.Logger
	deferLog logger.Logger

	dir string

	delegateBlockServer BlockServer
	delegateMDOps       MDOps
	delegateMDServer    MDServer

	lock       sync.RWMutex
	tlfBundles map[TlfID]*tlfJournalBundle
}

func (j *JournalServer) getBundle(tlfID TlfID) (*tlfJournalBundle, bool) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	bundle, ok := j.tlfBundles[tlfID]
	return bundle, ok
}

// Enable turns on the write journal for the given TLF.
func (j *JournalServer) Enable(tlfID TlfID) (err error) {
	j.log.Debug("Enabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Error when enabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	_, ok := j.tlfBundles[tlfID]
	if ok {
		j.log.Debug("Journal already enabled for %s", tlfID)
		return nil
	}

	j.log.Debug("Enabled journal for %s", tlfID)

	tlfDir := filepath.Join(j.dir, tlfID.String())
	mdJournal := makeMDJournal(j.config.Codec(), j.config.Crypto(), tlfDir)

	j.tlfBundles[tlfID] = &tlfJournalBundle{
		mdJournal: mdJournal,
	}
	return nil
}

// Flush flushes the write journal for the given TLF.
func (j *JournalServer) Flush(ctx context.Context, tlfID TlfID) (err error) {
	j.log.Debug("Flushing journal for %s", tlfID)
	flushedBlockEntries := 0
	flushedMDEntries := 0
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Flushed %d block entries and %d MD entries "+
					"for %s, but got error %v",
				flushedBlockEntries, flushedMDEntries,
				tlfID, err)
		}
	}()
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		j.log.Debug("Journal not enabled for %s", tlfID)
		return nil
	}

	// TODO: Flush block journal.

	for {
		flushed, err := func() (bool, error) {
			bundle.lock.Lock()
			defer bundle.lock.Unlock()
			return bundle.mdJournal.flushOne(
				ctx, j.config.Crypto(), j.config.MDServer(),
				j.log)
		}()
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedMDEntries++
	}

	j.log.Debug("Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, tlfID)

	return nil
}

// Disable turns off the write journal for the given TLF.
func (j *JournalServer) Disable(tlfID TlfID) (err error) {
	j.log.Debug("Disabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Error when disabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	bundle, ok := j.tlfBundles[tlfID]
	if !ok {
		j.log.Debug("Journal already disabled for %s", tlfID)
		return nil
	}

	bundle.lock.RLock()
	defer bundle.lock.RUnlock()
	length, err := bundle.mdJournal.length()
	if err != nil {
		return err
	}

	if length != 0 {
		return fmt.Errorf("Journal still has %d entries", length)
	}

	j.log.Debug("Disabled journal for %s", tlfID)

	delete(j.tlfBundles, tlfID)
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
	MDOps
	jServer  *JournalServer
	mdServer MDServer
}

var _ MDOps = journalMDOps{}

func (j journalMDOps) GetForHandle(
	ctx context.Context, handle *TlfHandle, mStatus MergeStatus) (
	TlfID, ImmutableRootMetadata, error) {
	tlfID, rmd, err := j.MDOps.GetForHandle(ctx, handle, mStatus)
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}

	if rmd == (ImmutableRootMetadata{}) {
		// If server doesn't know of an RMD, look in the
		// journal.
		bundle, ok := j.jServer.getBundle(tlfID)
		if !ok {
			return tlfID, ImmutableRootMetadata{}, nil
		}

		bundle.lock.RLock()
		defer bundle.lock.RUnlock()

		headID, head, err := bundle.mdJournal.getHead()
		if err != nil {
			return TlfID{}, ImmutableRootMetadata{}, err
		}

		if head == nil {
			return tlfID, ImmutableRootMetadata{}, nil
		}

		if head.MergedStatus() != mStatus {
			return tlfID, ImmutableRootMetadata{}, nil
		}

		rmd := RootMetadata{
			BareRootMetadata: *head,
			tlfHandle:        handle,
		}

		err = decryptMDPrivateData(
			ctx, j.jServer.config, &rmd, rmd.ReadOnly())
		if err != nil {
			return TlfID{}, ImmutableRootMetadata{}, err
		}

		return TlfID{}, MakeImmutableRootMetadata(&rmd, headID), nil
	}

	// If server does know of an RMD, let the journal know (if
	// it's empty).

	bundle, ok := j.jServer.getBundle(rmd.ID)
	if !ok {
		return tlfID, rmd, nil
	}

	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	length, err := bundle.mdJournal.length()
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}

	if length != 0 {
		return tlfID, rmd, nil
	}

	_, head, err := bundle.mdJournal.getHead()
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}

	// Don't override an existing unmerged head with a merged one.
	if head != nil && head.BID != NullBranchID && rmd.BID == NullBranchID {
		return tlfID, rmd, nil
	}

	// TODO: Make a deep copy?
	err = bundle.mdJournal.setHead(rmd.mdID, &rmd.BareRootMetadata)
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}

	return tlfID, rmd, nil
}

func (j journalMDOps) GetForTLF(
	ctx context.Context, id TlfID) (ImmutableRootMetadata, error) {
	rmd, err := j.MDOps.GetForTLF(ctx, id)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	mStatus := Merged

	if rmd == (ImmutableRootMetadata{}) {
		// If server doesn't know of an RMD, look in the
		// journal.
		bundle, ok := j.jServer.getBundle(id)
		if !ok {
			return ImmutableRootMetadata{}, nil
		}

		bundle.lock.RLock()
		defer bundle.lock.RUnlock()

		headID, head, err := bundle.mdJournal.getHead()
		if err != nil {
			return ImmutableRootMetadata{}, err
		}

		if head == nil {
			return ImmutableRootMetadata{}, nil
		}

		if head.MergedStatus() != mStatus {
			return ImmutableRootMetadata{}, nil
		}

		bareHandle, err := head.MakeBareTlfHandle()
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
		handle, err := MakeTlfHandle(ctx, bareHandle, j.jServer.config.KBPKI())
		if err != nil {
			return ImmutableRootMetadata{}, err
		}

		rmd := RootMetadata{
			BareRootMetadata: *head,
			tlfHandle:        handle,
		}

		err = decryptMDPrivateData(
			ctx, j.jServer.config, &rmd, rmd.ReadOnly())
		if err != nil {
			return ImmutableRootMetadata{}, err
		}

		return MakeImmutableRootMetadata(&rmd, headID), nil
	}

	// If server does know of an RMD, let the journal know (if
	// it's empty).

	bundle, ok := j.jServer.getBundle(rmd.ID)
	if !ok {
		return rmd, nil
	}

	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	length, err := bundle.mdJournal.length()
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	if length != 0 {
		return rmd, nil
	}

	_, head, err := bundle.mdJournal.getHead()
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	// Don't override an existing unmerged head with a merged one.
	if head != nil && head.BID != NullBranchID && rmd.BID == NullBranchID {
		return rmd, nil
	}

	// TODO: Make a deep copy?
	err = bundle.mdJournal.setHead(rmd.mdID, &rmd.BareRootMetadata)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	return rmd, nil
}

func (j journalMDOps) GetUnmergedForTLF(
	ctx context.Context, id TlfID, bid BranchID) (
	ImmutableRootMetadata, error) {
	_, ok := j.jServer.getBundle(id)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.GetUnmergedForTLF(ctx, id, bid)
}

func (j journalMDOps) GetRange(
	ctx context.Context, id TlfID, start, stop MetadataRevision) (
	[]ImmutableRootMetadata, error) {
	_, ok := j.jServer.getBundle(id)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.GetRange(ctx, id, start, stop)
}

func (j journalMDOps) GetUnmergedRange(
	ctx context.Context, id TlfID, bid BranchID,
	start, stop MetadataRevision) ([]ImmutableRootMetadata, error) {
	_, ok := j.jServer.getBundle(id)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.GetUnmergedRange(ctx, id, bid, start, stop)
}

func (j journalMDOps) Put(ctx context.Context, rmd *RootMetadata) (
	MdID, error) {
	_, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.Put(ctx, rmd)
}

func (j journalMDOps) PutUnmerged(ctx context.Context, rmd *RootMetadata) (
	MdID, error) {
	_, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.PutUnmerged(ctx, rmd)
}

func (j journalMDOps) PruneBranch(
	ctx context.Context, id TlfID, bid BranchID) error {
	_, ok := j.jServer.getBundle(id)
	if ok {
		// TODO: Delegate to bundle's MD journal.
	}

	return j.MDOps.PruneBranch(ctx, id, bid)
}

func (j *JournalServer) blockServer() journalBlockServer {
	return journalBlockServer{j, j.delegateBlockServer}
}

func (j *JournalServer) mdOps() journalMDOps {
	return journalMDOps{j.delegateMDOps, j, j.delegateMDServer}
}

func makeJournalServer(
	config Config, log logger.Logger,
	dir string, bserver BlockServer,
	mdOps MDOps, mdServer MDServer) *JournalServer {
	jServer := JournalServer{
		config:              config,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		dir:                 dir,
		delegateBlockServer: bserver,
		delegateMDOps:       mdOps,
		delegateMDServer:    mdServer,
		tlfBundles:          make(map[TlfID]*tlfJournalBundle),
	}
	return &jServer
}
