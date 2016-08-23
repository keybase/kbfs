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

// JournalAutoFlushStatus indicates whether a journal should be
// auto-flushing or not.
type JournalAutoFlushStatus int

const (
	// JournalAutoFlushDisabled indicates that the journal should
	// not be auto-flushing.
	JournalAutoFlushDisabled JournalAutoFlushStatus = iota
	// JournalAutoFlushEnabled indicates that the journal should
	// be auto-flushing.
	JournalAutoFlushEnabled
)

func (afs JournalAutoFlushStatus) String() string {
	switch afs {
	case JournalAutoFlushEnabled:
		return "Auto-flush enabled"
	case JournalAutoFlushDisabled:
		return "Auto-flush disabled"
	default:
		return fmt.Sprintf("JournalAutoFlushEnabled(%d)", afs)
	}
}

type tlfJournalBundle struct {
	tlfID               TlfID
	config              tlfJournalConfig
	delegateBlockServer BlockServer
	log                 logger.Logger
	deferLog            logger.Logger

	hasWorkCh  chan struct{}
	pauseCh    chan struct{}
	resumeCh   chan struct{}
	shutdownCh chan struct{}

	// Protects all operations on blockJournal and mdJournal.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	blockJournal *blockJournal
	mdJournal    *mdJournal
}

func makeTlfJournalBundle(
	ctx context.Context, dir string, tlfID TlfID, config tlfJournalConfig,
	delegateBlockServer BlockServer, log logger.Logger,
	afs JournalAutoFlushStatus) (*tlfJournalBundle, error) {
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

	b := &tlfJournalBundle{
		tlfID:               tlfID,
		config:              config,
		delegateBlockServer: delegateBlockServer,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		hasWorkCh:           make(chan struct{}, 1),
		pauseCh:             make(chan struct{}, 1),
		resumeCh:            make(chan struct{}, 1),
		shutdownCh:          make(chan struct{}, 1),
		blockJournal:        blockJournal,
		mdJournal:           mdJournal,
	}

	go b.autoFlush(afs)

	// Signal work to pick up any existing journal entries.
	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	b.log.CDebugf(ctx, "Enabled journal for %s with path %s", tlfID, tlfDir)
	return b, nil
}

func (b *tlfJournalBundle) autoFlush(afs JournalAutoFlushStatus) {
	ctx := ctxWithRandomID(
		context.Background(), "journal-auto-flush", "1", b.log)
	for {
		b.log.CDebugf(ctx, "Waiting for events for %s (%s)", b.tlfID, afs)
		switch afs {
		case JournalAutoFlushEnabled:
			select {
			case <-b.hasWorkCh:
				b.log.CDebugf(
					ctx, "Got work event for %s", b.tlfID)
				err := b.flush(ctx)
				if err != nil {
					b.log.CWarningf(ctx,
						"Error when flushing %s: %v",
						b.tlfID, err)
				}

			case <-b.pauseCh:
				b.log.CDebugf(ctx,
					"Got pause event for %s", b.tlfID)
				afs = JournalAutoFlushDisabled

			case <-b.shutdownCh:
				b.log.CDebugf(ctx,
					"Got shutdown event for %s", b.tlfID)
				return
			}

		case JournalAutoFlushDisabled:
			select {
			case <-b.resumeCh:
				b.log.CDebugf(ctx,
					"Got resume event for %s", b.tlfID)
				afs = JournalAutoFlushEnabled

			case <-b.shutdownCh:
				b.log.CDebugf(ctx,
					"Got shutdown event for %s", b.tlfID)
				return
			}

		default:
			b.log.CErrorf(
				ctx, "Unknown JournalAutoFlushStatus %s", afs)
			return
		}
	}
}

func (b *tlfJournalBundle) flush(ctx context.Context) (err error) {
	flushedBlockEntries := 0
	flushedMDEntries := 0
	defer func() {
		if err != nil {
			b.deferLog.CDebugf(ctx,
				"Flushed %d block entries and %d MD entries "+
					"for %s, but got error %v",
				flushedBlockEntries, flushedMDEntries,
				b.tlfID, err)
		}
	}()

	// TODO: Interleave block flushes with their related MD
	// flushes.

	// TODO: Parallelize block puts.

	for {
		flushed, err := b.flushOneBlockOp(ctx)
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedBlockEntries++
	}

	for {
		flushed, err := b.flushOneMDOp(ctx)
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedMDEntries++
	}

	b.log.CDebugf(ctx, "Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, b.tlfID)
	return nil
}

func (b *tlfJournalBundle) pauseAutoFlush() {
	select {
	case b.pauseCh <- struct{}{}:
	default:
	}
}

func (b *tlfJournalBundle) resumeAutoFlush() {
	select {
	case b.resumeCh <- struct{}{}:
	default:
	}
}

func (b *tlfJournalBundle) getBlockDataWithContext(
	id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.blockJournal.getDataWithContext(id, context)
}

func (b *tlfJournalBundle) putBlockData(
	ctx context.Context, id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	err := b.blockJournal.putData(ctx, id, context, buf, serverHalf)
	if err != nil {
		return err
	}

	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (b *tlfJournalBundle) addBlockReference(
	ctx context.Context, id BlockID, context BlockContext) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	err := b.blockJournal.addReference(ctx, id, context)
	if err != nil {
		return err
	}

	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (b *tlfJournalBundle) removeBlockReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	// Don't remove the block data if we remove the last
	// reference; we still need it to flush the initial put
	// operation.
	//
	// TODO: It would be nice if we could detect that case and
	// avoid having to flush the put.
	liveCounts, err = b.blockJournal.removeReferences(
		ctx, contexts, false)
	if err != nil {
		return nil, err
	}

	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	return liveCounts, nil
}

func (b *tlfJournalBundle) archiveBlockReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	err := b.blockJournal.archiveReferences(ctx, contexts)
	if err != nil {
		return err
	}

	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	return nil
}

func (b *tlfJournalBundle) getMDHead(
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey) (
	ImmutableBareRootMetadata, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.mdJournal.getHead(currentUID, currentVerifyingKey)
}

func (b *tlfJournalBundle) getMDRange(
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey,
	start, stop MetadataRevision) (
	[]ImmutableBareRootMetadata, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.mdJournal.getRange(
		currentUID, currentVerifyingKey, start, stop)
}

func (b *tlfJournalBundle) putMD(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey, signer cryptoSigner,
	ekg encryptionKeyGetter, bsplit BlockSplitter, rmd *RootMetadata) (
	MdID, error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	mdID, err := b.mdJournal.put(ctx, currentUID, currentVerifyingKey,
		signer, ekg, bsplit, rmd)
	if err != nil {
		return MdID{}, err
	}

	select {
	case b.hasWorkCh <- struct{}{}:
	default:
	}

	return mdID, nil
}

func (b *tlfJournalBundle) clearMDs(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey, bid BranchID) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	// No need to signal work in this case.
	return b.mdJournal.clear(ctx, currentUID, currentVerifyingKey, bid)
}

func (b *tlfJournalBundle) flushOneBlockOp(ctx context.Context) (bool, error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.blockJournal.flushOne(ctx, b.delegateBlockServer, b.tlfID)
}

func (b *tlfJournalBundle) flushOneMDOp(ctx context.Context) (bool, error) {
	_, currentUID, err := b.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return false, err
	}

	currentVerifyingKey, err := b.config.KBPKI().GetCurrentVerifyingKey(ctx)
	if err != nil {
		return false, err
	}

	b.lock.Lock()
	defer b.lock.Unlock()
	return b.mdJournal.flushOne(
		ctx, currentUID, currentVerifyingKey, b.config.Crypto(),
		b.config.MDServer())
}

func (b *tlfJournalBundle) getJournalEntryCounts() (
	blockEntryCount, mdEntryCount uint64, err error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	blockEntryCount, err = b.blockJournal.length()
	if err != nil {
		return 0, 0, err
	}

	mdEntryCount, err = b.mdJournal.length()
	if err != nil {
		return 0, 0, err
	}

	return blockEntryCount, mdEntryCount, nil
}

func (b *tlfJournalBundle) getJournalStatus() (TLFJournalStatus, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	earliestRevision, err := b.mdJournal.readEarliestRevision()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	latestRevision, err := b.mdJournal.readLatestRevision()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	blockEntryCount, err := b.blockJournal.length()
	if err != nil {
		return TLFJournalStatus{}, err
	}
	return TLFJournalStatus{
		RevisionStart: earliestRevision,
		RevisionEnd:   latestRevision,
		BlockOpCount:  blockEntryCount,
	}, nil
}

func (b *tlfJournalBundle) shutdown() {
	select {
	case b.shutdownCh <- struct{}{}:
	default:
	}
}
