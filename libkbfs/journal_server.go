// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol"
)

type tlfJournalConfig interface {
	Crypto() Crypto
	KBPKI() KBPKI
	MDServer() MDServer
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

func makeTlfJournalBundle(tlfID TlfID, config tlfJournalConfig,
	delegateBlockServer BlockServer, log logger.Logger,
	blockJournal *blockJournal, mdJournal *mdJournal,
	afs JournalAutoFlushStatus) *tlfJournalBundle {
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

	return b
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

// TODO: JournalServer isn't really a server, although it can create
// objects that act as servers. Rename to JournalManager.

// JournalServerStatus represents the overall status of the
// JournalServer for display in diagnostics. It is suitable for
// encoding directly as JSON.
type JournalServerStatus struct {
	RootDir      string
	JournalCount int
}

// TLFJournalStatus represents the status of a TLF's journal for
// display in diagnostics. It is suitable for encoding directly as
// JSON.
type TLFJournalStatus struct {
	RevisionStart MetadataRevision
	RevisionEnd   MetadataRevision
	BlockOpCount  uint64
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

	delegateBlockCache  BlockCache
	delegateBlockServer BlockServer
	delegateMDOps       MDOps

	lock       sync.RWMutex
	tlfBundles map[TlfID]*tlfJournalBundle
}

func makeJournalServer(
	config Config, log logger.Logger, dir string,
	bcache BlockCache, bserver BlockServer, mdOps MDOps) *JournalServer {
	jServer := JournalServer{
		config:              config,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		dir:                 dir,
		delegateBlockCache:  bcache,
		delegateBlockServer: bserver,
		delegateMDOps:       mdOps,
		tlfBundles:          make(map[TlfID]*tlfJournalBundle),
	}
	return &jServer
}

func (j *JournalServer) getBundle(tlfID TlfID) (*tlfJournalBundle, bool) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	bundle, ok := j.tlfBundles[tlfID]
	return bundle, ok
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

// EnableExistingJournals turns on the write journal for all TLFs with
// an existing journal. This must be the first thing done to a
// JournalServer. Any returned error is fatal, and means that the
// JournalServer must not be used.
func (j *JournalServer) EnableExistingJournals(
	ctx context.Context, afs JournalAutoFlushStatus) (err error) {
	j.log.CDebugf(ctx, "Enabling existing journals (%s)", afs)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when enabling existing journals: %v",
				err)
		}
	}()

	fileInfos, err := ioutil.ReadDir(j.dir)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	for _, fi := range fileInfos {
		name := fi.Name()
		if !fi.IsDir() {
			j.log.CDebugf(ctx, "Skipping file %q", name)
			continue
		}
		tlfID, err := ParseTlfID(fi.Name())
		if err != nil {
			j.log.CDebugf(ctx, "Skipping non-TLF dir %q", name)
			continue
		}

		err = j.Enable(ctx, tlfID, afs)
		if err != nil {
			// Don't treat per-TLF errors as fatal.
			j.log.CWarningf(
				ctx, "Error when enabling existing journal for %s: %v",
				tlfID, err)
			continue
		}
	}

	return nil
}

// Enable turns on the write journal for the given TLF.
func (j *JournalServer) Enable(
	ctx context.Context, tlfID TlfID,
	afs JournalAutoFlushStatus) (err error) {
	j.log.CDebugf(ctx, "Enabling journal for %s (%s)", tlfID, afs)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when enabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	_, ok := j.tlfBundles[tlfID]
	if ok {
		j.log.CDebugf(ctx, "Journal already enabled for %s", tlfID)
		return nil
	}

	tlfDir := filepath.Join(j.dir, tlfID.String())
	j.log.CDebugf(ctx, "Enabled journal for %s with path %s", tlfID, tlfDir)

	log := j.config.MakeLogger("")
	blockJournal, err := makeBlockJournal(
		ctx, j.config.Codec(), j.config.Crypto(), tlfDir, log)
	if err != nil {
		return err
	}

	_, uid, err := j.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return err
	}

	key, err := j.config.KBPKI().GetCurrentVerifyingKey(ctx)
	if err != nil {
		return err
	}

	mdJournal, err := makeMDJournal(
		uid, key, j.config.Codec(), j.config.Crypto(), tlfDir, log)
	if err != nil {
		return err
	}

	bundle := makeTlfJournalBundle(tlfID, j.config,
		j.delegateBlockServer, j.log, blockJournal, mdJournal, afs)
	j.tlfBundles[tlfID] = bundle

	return nil
}

// PauseAutoFlush pauses the background auto-flush goroutine, if it's
// not already paused.
func (j *JournalServer) PauseAutoFlush(ctx context.Context, tlfID TlfID) {
	j.log.CDebugf(ctx, "Signaling pause for %s", tlfID)
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		j.log.CDebugf(ctx,
			"Could not find bundle for %s; dropping pause signal",
			tlfID)
	}

	bundle.pauseAutoFlush()
}

// ResumeAutoFlush resumes the background auto-flush goroutine, if it's
// not already resumed.
func (j *JournalServer) ResumeAutoFlush(ctx context.Context, tlfID TlfID) {
	j.log.CDebugf(ctx, "Signaling resume for %s", tlfID)
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		j.log.CDebugf(ctx,
			"Could not find bundle for %s; dropping resume signal",
			tlfID)
	}

	bundle.resumeAutoFlush()
}

// Flush flushes the write journal for the given TLF.
func (j *JournalServer) Flush(ctx context.Context, tlfID TlfID) (err error) {
	j.log.CDebugf(ctx, "Flushing journal for %s", tlfID)
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		j.log.CDebugf(ctx, "Journal not enabled for %s", tlfID)
		return nil
	}

	return bundle.flush(ctx)
}

// Disable turns off the write journal for the given TLF.
func (j *JournalServer) Disable(ctx context.Context, tlfID TlfID) (err error) {
	j.log.CDebugf(ctx, "Disabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when disabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	bundle, ok := j.tlfBundles[tlfID]
	if !ok {
		j.log.CDebugf(ctx, "Journal already disabled for %s", tlfID)
		return nil
	}

	blockEntryCount, mdEntryCount, err := bundle.getJournalEntryCounts()
	if err != nil {
		return err
	}

	if (blockEntryCount != 0) || (mdEntryCount != 0) {
		return fmt.Errorf(
			"Journal still has %d block entries and %d md entries",
			blockEntryCount, mdEntryCount)
	}

	bundle.shutdown()

	j.log.CDebugf(ctx, "Disabled journal for %s", tlfID)

	delete(j.tlfBundles, tlfID)
	return nil
}

func (j *JournalServer) blockCache() journalBlockCache {
	return journalBlockCache{j, j.delegateBlockCache}
}

func (j *JournalServer) blockServer() journalBlockServer {
	return journalBlockServer{j, j.delegateBlockServer, false}
}

func (j *JournalServer) mdOps() journalMDOps {
	return journalMDOps{j.delegateMDOps, j}
}

// Status returns a JournalServerStatus object suitable for
// diagnostics.
func (j *JournalServer) Status() JournalServerStatus {
	journalCount := func() int {
		j.lock.RLock()
		defer j.lock.RUnlock()
		return len(j.tlfBundles)
	}()
	return JournalServerStatus{
		RootDir:      j.dir,
		JournalCount: journalCount,
	}
}

// JournalStatus returns a TLFServerStatus object for the given TLF
// suitable for diagnostics.
func (j *JournalServer) JournalStatus(tlfID TlfID) (TLFJournalStatus, error) {
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		return TLFJournalStatus{}, fmt.Errorf("Journal not enabled for %s", tlfID)
	}

	return bundle.getJournalStatus()
}

func (j *JournalServer) shutdown() {
	j.log.CDebugf(context.Background(), "Shutting down journal")
	j.lock.Lock()
	defer j.lock.Unlock()
	for _, bundle := range j.tlfBundles {
		bundle.shutdown()
	}
}
