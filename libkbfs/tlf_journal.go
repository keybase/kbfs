// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

// tlfJournalConfig is the subset of the Config interface needed by
// tlfJournal (for ease of testing).
type tlfJournalConfig interface {
	Codec() Codec
	Crypto() Crypto
	KBPKI() KBPKI
	MDServer() MDServer
}

// TLFJournalStatus represents the status of a TLF's journal for
// display in diagnostics. It is suitable for encoding directly as
// JSON.
type TLFJournalStatus struct {
	RevisionStart MetadataRevision
	RevisionEnd   MetadataRevision
	BlockOpCount  uint64
}

// TLFJournalAutoFlushStatus indicates whether a journal should be
// auto-flushing or not.
type TLFJournalAutoFlushStatus int

const (
	// TLFJournalAutoFlushDisabled indicates that the journal should
	// not be auto-flushing.
	TLFJournalAutoFlushDisabled TLFJournalAutoFlushStatus = iota
	// TLFJournalAutoFlushEnabled indicates that the journal should
	// be auto-flushing.
	TLFJournalAutoFlushEnabled
)

func (afs TLFJournalAutoFlushStatus) String() string {
	switch afs {
	case TLFJournalAutoFlushEnabled:
		return "Auto-flush enabled"
	case TLFJournalAutoFlushDisabled:
		return "Auto-flush disabled"
	default:
		return fmt.Sprintf("TLFJournalAutoFlushEnabled(%d)", afs)
	}
}

// A tlfJournal contains all the journals for a TLF and controls the
// synchronization between the objects that are adding to those
// journals (via journalBlockServer or journalMDOps) and a background
// goroutine that flushes journal entries to the servers.
type tlfJournal struct {
	tlfID               TlfID
	config              tlfJournalConfig
	delegateBlockServer BlockServer
	log                 logger.Logger
	deferLog            logger.Logger

	// All the channels below are used as simple on/off
	// signals. They're buffered for one object, and all sends are
	// asynchronous, so multiple sends get collapsed into one
	// signal.
	hasWorkCh      chan struct{}
	needPauseCh    chan struct{}
	needResumeCh   chan struct{}
	needShutdownCh chan struct{}

	// Protects all operations on blockJournal and mdJournal.
	//
	// TODO: Don't let flushing block put operations. See
	// KBFS-1433.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	blockJournal *blockJournal
	mdJournal    *mdJournal
}

func makeTlfJournal(
	ctx context.Context, dir string, tlfID TlfID, config tlfJournalConfig,
	delegateBlockServer BlockServer, log logger.Logger,
	afs TLFJournalAutoFlushStatus) (*tlfJournal, error) {
	tlfDir := filepath.Join(dir, tlfID.String())

	blockJournal, err := makeBlockJournal(
		ctx, config.Codec(), config.Crypto(), tlfDir, log)
	if err != nil {
		return nil, err
	}

	_, uid, err := config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return nil, err
	}

	key, err := config.KBPKI().GetCurrentVerifyingKey(ctx)
	if err != nil {
		return nil, err
	}

	mdJournal, err := makeMDJournal(
		uid, key, config.Codec(), config.Crypto(), tlfDir, log)
	if err != nil {
		return nil, err
	}

	j := &tlfJournal{
		tlfID:               tlfID,
		config:              config,
		delegateBlockServer: delegateBlockServer,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		hasWorkCh:           make(chan struct{}, 1),
		needPauseCh:         make(chan struct{}, 1),
		needResumeCh:        make(chan struct{}, 1),
		needShutdownCh:      make(chan struct{}, 1),
		blockJournal:        blockJournal,
		mdJournal:           mdJournal,
	}

	go j.doBackgroundWork(afs)

	// Signal work to pick up any existing journal entries.
	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	j.log.CDebugf(ctx, "Enabled journal for %s with path %s", tlfID, tlfDir)
	return j, nil
}

// doBackgroundWork is the main function for the background
// goroutine. Currently it just does auto-flushing.
//
// TODO: Handle garbage collection too, somehow.
func (j *tlfJournal) doBackgroundWork(afs TLFJournalAutoFlushStatus) {
	ctx := ctxWithRandomID(
		context.Background(), "journal-auto-flush", "1", j.log)
	for {
		j.log.CDebugf(ctx, "Waiting for events for %s (%s)",
			j.tlfID, afs)
		switch afs {
		case TLFJournalAutoFlushEnabled:
			select {
			case <-j.hasWorkCh:
				j.log.CDebugf(
					ctx, "Got work signal for %s", j.tlfID)
				err := j.flush(ctx)
				if err != nil {
					j.log.CWarningf(ctx,
						"Error when flushing %s: %v",
						j.tlfID, err)
				}

			case <-j.needPauseCh:
				j.log.CDebugf(ctx,
					"Got pause signal for %s", j.tlfID)
				afs = TLFJournalAutoFlushDisabled

			case <-j.needShutdownCh:
				j.log.CDebugf(ctx,
					"Got shutdown signal for %s", j.tlfID)
				return
			}

		case TLFJournalAutoFlushDisabled:
			select {
			case <-j.needResumeCh:
				j.log.CDebugf(ctx,
					"Got resume signal for %s", j.tlfID)
				afs = TLFJournalAutoFlushEnabled

			case <-j.needShutdownCh:
				j.log.CDebugf(ctx,
					"Got shutdown signal for %s", j.tlfID)
				return
			}

		default:
			j.log.CErrorf(
				ctx, "Unknown TLFJournalAutoFlushStatus %s",
				afs)
			return
		}
	}
}

func (j *tlfJournal) pauseAutoFlush() {
	select {
	case j.needPauseCh <- struct{}{}:
	default:
	}
}

func (j *tlfJournal) resumeAutoFlush() {
	select {
	case j.needResumeCh <- struct{}{}:
	default:
	}
}

func (j *tlfJournal) flush(ctx context.Context) (err error) {
	flushedBlockEntries := 0
	flushedMDEntries := 0
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Flushed %d block entries and %d MD entries "+
					"for %s, but got error %v",
				flushedBlockEntries, flushedMDEntries,
				j.tlfID, err)
		}
	}()

	// TODO: Interleave block flushes with their related MD
	// flushes.

	// TODO: Parallelize block puts.

	for {
		flushed, err := j.flushOneBlockOp(ctx)
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedBlockEntries++
	}

	for {
		flushed, err := j.flushOneMDOp(ctx)
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedMDEntries++
	}

	j.log.CDebugf(ctx, "Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, j.tlfID)
	return nil
}

func (j *tlfJournal) flushOneBlockOp(ctx context.Context) (bool, error) {
	j.lock.Lock()
	defer j.lock.Unlock()
	return j.blockJournal.flushOne(ctx, j.delegateBlockServer, j.tlfID)
}

func (j *tlfJournal) flushOneMDOp(ctx context.Context) (bool, error) {
	_, currentUID, err := j.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return false, err
	}

	currentVerifyingKey, err := j.config.KBPKI().GetCurrentVerifyingKey(ctx)
	if err != nil {
		return false, err
	}

	j.lock.Lock()
	defer j.lock.Unlock()
	return j.mdJournal.flushOne(
		ctx, currentUID, currentVerifyingKey, j.config.Crypto(),
		j.config.MDServer())
}

func (j *tlfJournal) getJournalEntryCounts() (
	blockEntryCount, mdEntryCount uint64, err error) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	blockEntryCount, err = j.blockJournal.length()
	if err != nil {
		return 0, 0, err
	}

	mdEntryCount, err = j.mdJournal.length()
	if err != nil {
		return 0, 0, err
	}

	return blockEntryCount, mdEntryCount, nil
}

func (j *tlfJournal) getJournalStatus() (TLFJournalStatus, error) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	earliestRevision, err := j.mdJournal.readEarliestRevision()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	latestRevision, err := j.mdJournal.readLatestRevision()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	blockEntryCount, err := j.blockJournal.length()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	return TLFJournalStatus{
		RevisionStart: earliestRevision,
		RevisionEnd:   latestRevision,
		BlockOpCount:  blockEntryCount,
	}, nil
}

func (j *tlfJournal) shutdown() {
	select {
	case j.needShutdownCh <- struct{}{}:
	default:
	}
}

func (j *tlfJournal) getBlockDataWithContext(
	id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	return j.blockJournal.getDataWithContext(id, context)
}

func (j *tlfJournal) putBlockData(
	ctx context.Context, id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	err := j.blockJournal.putData(ctx, id, context, buf, serverHalf)
	if err != nil {
		return err
	}

	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (j *tlfJournal) addBlockReference(
	ctx context.Context, id BlockID, context BlockContext) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	err := j.blockJournal.addReference(ctx, id, context)
	if err != nil {
		return err
	}

	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (j *tlfJournal) removeBlockReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	j.lock.Lock()
	defer j.lock.Unlock()
	// Don't remove the block data if we remove the last
	// reference; we still need it to flush the initial put
	// operation.
	//
	// TODO: It would be nice if we could detect that case and
	// avoid having to flush the put.
	liveCounts, err = j.blockJournal.removeReferences(
		ctx, contexts, false)
	if err != nil {
		return nil, err
	}

	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	return liveCounts, nil
}

func (j *tlfJournal) archiveBlockReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	err := j.blockJournal.archiveReferences(ctx, contexts)
	if err != nil {
		return err
	}

	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (j *tlfJournal) getMDHead(
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey) (
	ImmutableBareRootMetadata, error) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	return j.mdJournal.getHead(currentUID, currentVerifyingKey)
}

func (j *tlfJournal) getMDRange(
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey,
	start, stop MetadataRevision) (
	[]ImmutableBareRootMetadata, error) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	return j.mdJournal.getRange(
		currentUID, currentVerifyingKey, start, stop)
}

func (j *tlfJournal) putMD(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey, signer cryptoSigner,
	ekg encryptionKeyGetter, bsplit BlockSplitter, rmd *RootMetadata) (
	MdID, error) {
	j.lock.Lock()
	defer j.lock.Unlock()
	mdID, err := j.mdJournal.put(ctx, currentUID, currentVerifyingKey,
		signer, ekg, bsplit, rmd)
	if err != nil {
		return MdID{}, err
	}

	select {
	case j.hasWorkCh <- struct{}{}:
	default:
	}

	return mdID, nil
}

func (j *tlfJournal) clearMDs(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey, bid BranchID) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	// No need to signal work in this case.
	return j.mdJournal.clear(ctx, currentUID, currentVerifyingKey, bid)
}
