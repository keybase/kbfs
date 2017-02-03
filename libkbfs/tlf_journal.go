// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/keybase/backoff"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/kbfssync"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// tlfJournalConfig is the subset of the Config interface needed by
// tlfJournal (for ease of testing).
type tlfJournalConfig interface {
	BlockSplitter() BlockSplitter
	Clock() Clock
	Codec() kbfscodec.Codec
	Crypto() Crypto
	BlockCache() BlockCache
	BlockOps() BlockOps
	MDCache() MDCache
	MetadataVersion() MetadataVer
	Reporter() Reporter
	encryptionKeyGetter() encryptionKeyGetter
	mdDecryptionKeyGetter() mdDecryptionKeyGetter
	MDServer() MDServer
	usernameGetter() normalizedUsernameGetter
	MakeLogger(module string) logger.Logger
	diskLimitTimeout() time.Duration
}

// tlfJournalConfigWrapper is an adapter for Config objects to the
// tlfJournalConfig interface.
type tlfJournalConfigAdapter struct {
	Config
}

func (ca tlfJournalConfigAdapter) encryptionKeyGetter() encryptionKeyGetter {
	return ca.Config.KeyManager()
}

func (ca tlfJournalConfigAdapter) mdDecryptionKeyGetter() mdDecryptionKeyGetter {
	return ca.Config.KeyManager()
}

func (ca tlfJournalConfigAdapter) usernameGetter() normalizedUsernameGetter {
	return ca.Config.KBPKI()
}

const defaultDiskLimitTimeout = 3 * time.Second

func (ca tlfJournalConfigAdapter) diskLimitTimeout() time.Duration {
	return defaultDiskLimitTimeout
}

const (
	// Maximum number of blocks that can be flushed in a single batch
	// by the journal.  TODO: make this configurable, so that users
	// can choose how much bandwidth is used by the journal.
	maxJournalBlockFlushBatchSize = 25
	// This will be the final entry for unflushed paths if there are
	// too many revisions to process at once.
	incompleteUnflushedPathsMarker = "..."
	// ForcedBranchSquashThreshold is the minimum number of MD
	// revisions in the journal that will trigger an automatic branch
	// conversion (and subsequent resolution).
	ForcedBranchSquashThreshold = 20
	// Maximum number of blocks to delete from the local saved block
	// journal at a time while holding the lock.
	maxSavedBlockRemovalsAtATime = uint64(500)
)

// TLFJournalStatus represents the status of a TLF's journal for
// display in diagnostics. It is suitable for encoding directly as
// JSON.
type TLFJournalStatus struct {
	Dir           string
	RevisionStart MetadataRevision
	RevisionEnd   MetadataRevision
	BranchID      string
	BlockOpCount  uint64
	// The byte counters below are signed because
	// os.FileInfo.Size() is signed.
	StoredBytes    int64
	UnflushedBytes int64
	UnflushedPaths []string
	LastFlushErr   string `json:",omitempty"`
}

// TLFJournalBackgroundWorkStatus indicates whether a journal should
// be doing background work or not.
type TLFJournalBackgroundWorkStatus int

const (
	// TLFJournalBackgroundWorkPaused indicates that the journal
	// should not currently be doing background work.
	TLFJournalBackgroundWorkPaused TLFJournalBackgroundWorkStatus = iota
	// TLFJournalBackgroundWorkEnabled indicates that the journal
	// should be doing background work.
	TLFJournalBackgroundWorkEnabled
)

type tlfJournalPauseType int

const (
	journalPauseConflict tlfJournalPauseType = 1 << iota
	journalPauseCommand
)

func (bws TLFJournalBackgroundWorkStatus) String() string {
	switch bws {
	case TLFJournalBackgroundWorkEnabled:
		return "Background work enabled"
	case TLFJournalBackgroundWorkPaused:
		return "Background work paused"
	default:
		return fmt.Sprintf("TLFJournalBackgroundWorkStatus(%d)", bws)
	}
}

// bwState indicates the state of the background work goroutine.
type bwState int

const (
	bwBusy bwState = iota
	bwIdle
	bwPaused
)

func (bws bwState) String() string {
	switch bws {
	case bwBusy:
		return "bwBusy"
	case bwIdle:
		return "bwIdle"
	case bwPaused:
		return "bwPaused"
	default:
		return fmt.Sprintf("bwState(%d)", bws)
	}
}

// tlfJournalBWDelegate is used by tests to know what the background
// goroutine is doing, and also to enforce a timeout (via the
// context).
type tlfJournalBWDelegate interface {
	GetBackgroundContext() context.Context
	OnNewState(ctx context.Context, bws bwState)
	OnShutdown(ctx context.Context)
}

// A tlfJournal contains all the journals for a (TLF, user, device)
// tuple and controls the synchronization between the objects that are
// adding to those journals (via journalBlockServer or journalMDOps)
// and a background goroutine that flushes journal entries to the
// servers.
//
// The maximum number of characters added to the root dir by a TLF
// journal is 51, which just the max of the block journal and MD
// journal numbers.
type tlfJournal struct {
	uid                 keybase1.UID
	key                 kbfscrypto.VerifyingKey
	tlfID               tlf.ID
	dir                 string
	config              tlfJournalConfig
	delegateBlockServer BlockServer
	log                 logger.Logger
	deferLog            logger.Logger
	onBranchChange      branchChangeListener
	onMDFlush           mdFlushListener

	// Invariant: this tlfJournal acquires exactly
	// blockJournal.getStoredBytes() until shutdown.
	diskLimiter diskLimiter

	// All the channels below are used as simple on/off
	// signals. They're buffered for one object, and all sends are
	// asynchronous, so multiple sends get collapsed into one
	// signal.
	hasWorkCh      chan struct{}
	needPauseCh    chan struct{}
	needResumeCh   chan struct{}
	needShutdownCh chan struct{}

	// Track the ways in which the journal is paused.  We don't allow
	// work to resume unless a resume has come in corresponding to
	// each type of paused that's happened.
	pauseLock sync.Mutex
	pauseType tlfJournalPauseType

	// This channel is closed when background work shuts down.
	backgroundShutdownCh chan struct{}

	// Serializes all flushes.
	flushLock sync.Mutex

	// Tracks background work.
	wg kbfssync.RepeatedWaitGroup

	// Protects all operations on blockJournal and mdJournal.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	journalLock sync.RWMutex
	// both of these are nil after shutdown() is called.
	blockJournal   *blockJournal
	mdJournal      *mdJournal
	disabled       bool
	lastFlushErr   error
	unflushedPaths unflushedPathCache

	bwDelegate tlfJournalBWDelegate
}

func getTLFJournalInfoFilePath(dir string) string {
	return filepath.Join(dir, "info.json")
}

// tlfJournalInfo is the structure stored in
// getTLFJournalInfoFilePath(dir).
type tlfJournalInfo struct {
	UID          keybase1.UID
	VerifyingKey kbfscrypto.VerifyingKey
	TlfID        tlf.ID
}

func readTLFJournalInfoFile(dir string) (
	keybase1.UID, kbfscrypto.VerifyingKey, tlf.ID, error) {
	var info tlfJournalInfo
	err := ioutil.DeserializeFromJSONFile(
		getTLFJournalInfoFilePath(dir), &info)
	if err != nil {
		return keybase1.UID(""), kbfscrypto.VerifyingKey{}, tlf.ID{}, err
	}

	return info.UID, info.VerifyingKey, info.TlfID, nil
}

func writeTLFJournalInfoFile(dir string, uid keybase1.UID,
	key kbfscrypto.VerifyingKey, tlfID tlf.ID) error {
	info := tlfJournalInfo{uid, key, tlfID}
	return ioutil.SerializeToJSONFile(info, getTLFJournalInfoFilePath(dir))
}

func makeTLFJournal(
	ctx context.Context, uid keybase1.UID, key kbfscrypto.VerifyingKey,
	dir string, tlfID tlf.ID, config tlfJournalConfig,
	delegateBlockServer BlockServer, bws TLFJournalBackgroundWorkStatus,
	bwDelegate tlfJournalBWDelegate, onBranchChange branchChangeListener,
	onMDFlush mdFlushListener, diskLimiter diskLimiter) (
	*tlfJournal, error) {
	if uid == keybase1.UID("") {
		return nil, errors.New("Empty user")
	}
	if key == (kbfscrypto.VerifyingKey{}) {
		return nil, errors.New("Empty verifying key")
	}
	if tlfID == (tlf.ID{}) {
		return nil, errors.New("Empty tlf.ID")
	}

	readUID, readKey, readTlfID, err := readTLFJournalInfoFile(dir)
	switch {
	case ioutil.IsNotExist(err):
		// Info file doesn't exist, so write it.
		err := writeTLFJournalInfoFile(dir, uid, key, tlfID)
		if err != nil {
			return nil, err
		}

	case err != nil:
		return nil, err

	default:
		// Info file exists, so it should match passed-in
		// parameters.
		if uid != readUID {
			return nil, errors.Errorf(
				"Expected UID %s, got %s", uid, readUID)
		}

		if key != readKey {
			return nil, errors.Errorf(
				"Expected verifying key %s, got %s",
				key, readKey)
		}

		if tlfID != readTlfID {
			return nil, errors.Errorf(
				"Expected TLF ID %s, got %s", tlfID, readTlfID)
		}
	}

	log := config.MakeLogger("TLFJ")

	blockJournal, err := makeBlockJournal(ctx, config.Codec(), dir, log)
	if err != nil {
		return nil, err
	}

	mdJournal, err := makeMDJournal(
		ctx, uid, key, config.Codec(), config.Crypto(), config.Clock(),
		tlfID, config.MetadataVersion(), dir, log)
	if err != nil {
		return nil, err
	}

	j := &tlfJournal{
		uid:                  uid,
		key:                  key,
		tlfID:                tlfID,
		dir:                  dir,
		config:               config,
		delegateBlockServer:  delegateBlockServer,
		log:                  log,
		deferLog:             log.CloneWithAddedDepth(1),
		onBranchChange:       onBranchChange,
		onMDFlush:            onMDFlush,
		diskLimiter:          diskLimiter,
		hasWorkCh:            make(chan struct{}, 1),
		needPauseCh:          make(chan struct{}, 1),
		needResumeCh:         make(chan struct{}, 1),
		needShutdownCh:       make(chan struct{}, 1),
		backgroundShutdownCh: make(chan struct{}),
		blockJournal:         blockJournal,
		mdJournal:            mdJournal,
		bwDelegate:           bwDelegate,
	}

	if bws == TLFJournalBackgroundWorkPaused {
		j.pauseType |= journalPauseCommand
	}

	isConflict, err := j.isOnConflictBranch()
	if err != nil {
		return nil, err
	}
	if isConflict {
		// Conflict branches must start off paused until the first
		// resolution.
		j.log.CDebugf(ctx, "Journal for %s has a conflict, so starting off "+
			"paused (requested status %s)", tlfID, bws)
		bws = TLFJournalBackgroundWorkPaused
		j.pauseType |= journalPauseConflict
	}
	if bws == TLFJournalBackgroundWorkPaused {
		j.wg.Pause()
	}

	// Do this only once we're sure we won't error.
	storedBytes := j.blockJournal.getStoredBytes()
	if storedBytes > 0 {
		availableBytes := j.diskLimiter.OnJournalEnable(storedBytes)
		j.log.CDebugf(ctx,
			"Force-acquired %d bytes for %s: available=%d",
			storedBytes, tlfID, availableBytes)
	}

	go j.doBackgroundWorkLoop(bws, backoff.NewExponentialBackOff())

	// Signal work to pick up any existing journal entries.
	j.signalWork()

	j.log.CDebugf(ctx, "Enabled journal for %s with path %s", tlfID, dir)
	return j, nil
}

func (j *tlfJournal) signalWork() {
	j.wg.Add(1)
	select {
	case j.hasWorkCh <- struct{}{}:
	default:
		j.wg.Done()
	}
}

// CtxJournalTagKey is the type used for unique context tags within
// background journal work.
type CtxJournalTagKey int

const (
	// CtxJournalIDKey is the type of the tag for unique operation IDs
	// within background journal work.
	CtxJournalIDKey CtxJournalTagKey = iota
)

// CtxJournalOpID is the display name for the unique operation
// enqueued journal ID tag.
const CtxJournalOpID = "JID"

// doBackgroundWorkLoop is the main function for the background
// goroutine. It spawns off a worker goroutine to call
// doBackgroundWork whenever there is work, and can be paused and
// resumed.
func (j *tlfJournal) doBackgroundWorkLoop(
	bws TLFJournalBackgroundWorkStatus, retry backoff.BackOff) {
	ctx := context.Background()
	if j.bwDelegate != nil {
		ctx = j.bwDelegate.GetBackgroundContext()
	}

	// Non-nil when a retry has been scheduled for the future.
	var retryTimer *time.Timer
	defer func() {
		close(j.backgroundShutdownCh)
		if j.bwDelegate != nil {
			j.bwDelegate.OnShutdown(ctx)
		}
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()

	// Below we have a state machine with three states:
	//
	// 1) Idle, where we wait for new work or to be paused;
	// 2) Busy, where we wait for the worker goroutine to
	//    finish, or to be paused;
	// 3) Paused, where we wait to be resumed.
	//
	// We run this state machine until we are shutdown. Also, if
	// we exit the busy state for any reason other than the worker
	// goroutine finished, we stop the worker goroutine (via
	// bwCancel below).

	// errCh and bwCancel are non-nil only when we're in the busy
	// state. errCh is the channel on which we receive the error
	// from the worker goroutine, and bwCancel is the CancelFunc
	// corresponding to the context passed to the worker
	// goroutine.
	var errCh <-chan error
	var bwCancel context.CancelFunc
	// Handle the case where we panic while in the busy state.
	defer func() {
		if bwCancel != nil {
			bwCancel()
		}
	}()

	for {
		ctx := ctxWithRandomIDReplayable(ctx, CtxJournalIDKey, CtxJournalOpID,
			j.log)
		switch {
		case bws == TLFJournalBackgroundWorkEnabled && errCh == nil:
			// 1) Idle.
			if j.bwDelegate != nil {
				j.bwDelegate.OnNewState(ctx, bwIdle)
			}
			j.log.CDebugf(
				ctx, "Waiting for the work signal for %s",
				j.tlfID)
			select {
			case <-j.hasWorkCh:
				j.log.CDebugf(ctx, "Got work signal for %s", j.tlfID)
				if retryTimer != nil {
					retryTimer.Stop()
					retryTimer = nil
				}
				bwCtx, cancel := context.WithCancel(ctx)
				errCh = j.doBackgroundWork(bwCtx)
				bwCancel = cancel

			case <-j.needPauseCh:
				j.log.CDebugf(ctx,
					"Got pause signal for %s", j.tlfID)
				bws = TLFJournalBackgroundWorkPaused

			case <-j.needShutdownCh:
				j.log.CDebugf(ctx,
					"Got shutdown signal for %s", j.tlfID)
				return
			}

		case bws == TLFJournalBackgroundWorkEnabled && errCh != nil:
			// 2) Busy.
			if j.bwDelegate != nil {
				j.bwDelegate.OnNewState(ctx, bwBusy)
			}
			j.log.CDebugf(ctx,
				"Waiting for background work to be done for %s",
				j.tlfID)
			needShutdown := false
			select {
			case err := <-errCh:
				if retryTimer != nil {
					panic("Retry timer should be nil after work is done")
				}

				if err != nil {
					j.log.CWarningf(ctx,
						"Background work error for %s: %+v",
						j.tlfID, err)

					bTime := retry.NextBackOff()
					if bTime != backoff.Stop {
						j.log.CWarningf(ctx, "Retrying in %s", bTime)
						retryTimer = time.AfterFunc(bTime, j.signalWork)
					}
				} else {
					retry.Reset()
				}

			case <-j.needPauseCh:
				j.log.CDebugf(ctx,
					"Got pause signal for %s", j.tlfID)
				bws = TLFJournalBackgroundWorkPaused

			case <-j.needShutdownCh:
				j.log.CDebugf(ctx,
					"Got shutdown signal for %s", j.tlfID)
				needShutdown = true
			}

			// Cancel the worker goroutine as we exit this
			// state.
			bwCancel()
			bwCancel = nil

			// Ensure the worker finishes after being canceled, so it
			// doesn't pick up any new work.  For example, if the
			// worker doesn't check for cancellations before checking
			// the journal for new work, it might process some journal
			// entries before returning an error.
			<-errCh
			errCh = nil

			if needShutdown {
				return
			}

		case bws == TLFJournalBackgroundWorkPaused:
			// 3) Paused
			if j.bwDelegate != nil {
				j.bwDelegate.OnNewState(ctx, bwPaused)
			}
			j.log.CDebugf(
				ctx, "Waiting to resume background work for %s",
				j.tlfID)
			select {
			case <-j.needResumeCh:
				j.log.CDebugf(ctx,
					"Got resume signal for %s", j.tlfID)
				bws = TLFJournalBackgroundWorkEnabled

			case <-j.needShutdownCh:
				j.log.CDebugf(ctx,
					"Got shutdown signal for %s", j.tlfID)
				return
			}

		default:
			j.log.CErrorf(
				ctx, "Unknown TLFJournalBackgroundStatus %s",
				bws)
			return
		}
	}
}

// doBackgroundWork currently only does auto-flushing. It assumes that
// ctx is canceled when the background processing should stop.
//
// TODO: Handle garbage collection too.
func (j *tlfJournal) doBackgroundWork(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	// TODO: Handle panics.
	go func() {
		defer j.wg.Done()
		errCh <- j.flush(ctx)
		close(errCh)
	}()
	return errCh
}

// We don't guarantee that background pause/resume requests will be
// processed in strict FIFO order. In particular, multiple pause
// requests are collapsed into one (also multiple resume requests), so
// it's possible that a pause-resume-pause sequence will be processed
// as pause-resume. But that's okay, since these are just for
// infrequent ad-hoc testing.

func (j *tlfJournal) pause(pauseType tlfJournalPauseType) {
	j.pauseLock.Lock()
	defer j.pauseLock.Unlock()
	oldPauseType := j.pauseType
	j.pauseType |= pauseType

	if oldPauseType > 0 {
		// No signal is needed since someone already called pause.
		return
	}

	j.wg.Pause()
	select {
	case j.needPauseCh <- struct{}{}:
	default:
	}
}

func (j *tlfJournal) pauseBackgroundWork() {
	j.pause(journalPauseCommand)
}

func (j *tlfJournal) resume(pauseType tlfJournalPauseType) {
	j.pauseLock.Lock()
	defer j.pauseLock.Unlock()
	j.pauseType &= ^pauseType

	if j.pauseType != 0 {
		return
	}

	select {
	case j.needResumeCh <- struct{}{}:
		// Resume the wait group right away, so future callers will block
		// even before the background goroutine picks up this signal.
		j.wg.Resume()
	default:
	}
}

func (j *tlfJournal) resumeBackgroundWork() {
	j.resume(journalPauseCommand)
}

func (j *tlfJournal) checkEnabledLocked() error {
	if j.blockJournal == nil || j.mdJournal == nil {
		return errors.WithStack(errTLFJournalShutdown{})
	}
	if j.disabled {
		return errors.WithStack(errTLFJournalDisabled{})
	}
	return nil
}

func (j *tlfJournal) getJournalEnds(ctx context.Context) (
	blockEnd journalOrdinal, mdEnd MetadataRevision, err error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return 0, MetadataRevisionUninitialized, err
	}

	blockEnd, err = j.blockJournal.end()
	if err != nil {
		return 0, 0, err
	}

	mdEnd, err = j.mdJournal.end()
	if err != nil {
		return 0, 0, err
	}

	return blockEnd, mdEnd, nil
}

func (j *tlfJournal) flush(ctx context.Context) (err error) {
	j.flushLock.Lock()
	defer j.flushLock.Unlock()

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
		j.journalLock.Lock()
		j.lastFlushErr = err
		j.journalLock.Unlock()
	}()

	// TODO: Avoid starving flushing MD ops if there are many
	// block ops. See KBFS-1502.

	for {
		select {
		case <-ctx.Done():
			j.log.CDebugf(ctx, "Flush canceled: %+v", ctx.Err())
			return nil
		default:
		}

		isConflict, err := j.isOnConflictBranch()
		if err != nil {
			return err
		}
		if isConflict {
			j.log.CDebugf(ctx, "Ignoring flush while on conflict branch")
			return nil
		}

		converted, err := j.convertMDsToBranchIfOverThreshold(ctx)
		if err != nil {
			return err
		}
		if converted {
			return nil
		}

		blockEnd, mdEnd, err := j.getJournalEnds(ctx)
		if err != nil {
			return err
		}

		if blockEnd == 0 && mdEnd == MetadataRevisionUninitialized {
			j.log.CDebugf(ctx, "Nothing else to flush")
			break
		}

		j.log.CDebugf(ctx, "Flushing up to blockEnd=%d and mdEnd=%d",
			blockEnd, mdEnd)

		// Flush the block journal ops in parallel.
		numFlushed, maxMDRevToFlush, err := j.flushBlockEntries(ctx, blockEnd)
		if err != nil {
			return err
		}
		flushedBlockEntries += numFlushed

		if numFlushed == 0 {
			// There were no blocks to flush, so we can flush all of
			// the remaining MDs.
			maxMDRevToFlush = mdEnd
		}

		// TODO: Flush MDs in batch.

		for {
			flushed, err := j.flushOneMDOp(ctx, mdEnd, maxMDRevToFlush)
			if err != nil {
				return err
			}
			if !flushed {
				break
			}
			flushedMDEntries++
		}
	}

	j.log.CDebugf(ctx, "Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, j.tlfID)
	return nil
}

type errTLFJournalShutdown struct{}

func (e errTLFJournalShutdown) Error() string {
	return "tlfJournal is shutdown"
}

type errTLFJournalDisabled struct{}

func (e errTLFJournalDisabled) Error() string {
	return "tlfJournal is disabled"
}

type errTLFJournalNotEmpty struct{}

func (e errTLFJournalNotEmpty) Error() string {
	return "tlfJournal is not empty"
}

func (j *tlfJournal) getNextBlockEntriesToFlush(
	ctx context.Context, end journalOrdinal) (
	entries blockEntriesToFlush, maxMDRevToFlush MetadataRevision, err error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
	}

	return j.blockJournal.getNextEntriesToFlush(ctx, end,
		maxJournalBlockFlushBatchSize)
}

func (j *tlfJournal) removeFlushedBlockEntries(ctx context.Context,
	entries blockEntriesToFlush) error {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	// Keep the flushed blocks around until we know for sure the MD
	// flush will succeed; otherwise if we become unmerged, conflict
	// resolution will be very expensive.
	err := j.blockJournal.saveBlocksUntilNextMDFlush()
	if err != nil {
		return err
	}

	storedBytesBefore := j.blockJournal.getStoredBytes()

	removedBytes, err := j.blockJournal.removeFlushedEntries(
		ctx, entries, j.tlfID, j.config.Reporter())
	if err != nil {
		return err
	}

	storedBytesAfter := j.blockJournal.getStoredBytes()

	// removedBytes should be zero since we're always saving
	// blocks.
	if removedBytes != 0 {
		panic(fmt.Sprintf("removedBytes=%d unexpectedly non-zero",
			removedBytes))
	}

	// storedBytes shouldn't change since removedBytes is 0.
	if storedBytesAfter != storedBytesBefore {
		panic(fmt.Sprintf(
			"storedBytes unexpectedly changed from %d to %d",
			storedBytesBefore, storedBytesAfter))
	}

	return nil
}

func (j *tlfJournal) flushBlockEntries(
	ctx context.Context, end journalOrdinal) (int, MetadataRevision, error) {
	entries, maxMDRevToFlush, err := j.getNextBlockEntriesToFlush(ctx, end)
	if err != nil {
		return 0, MetadataRevisionUninitialized, err
	}

	if entries.length() == 0 {
		return 0, maxMDRevToFlush, nil
	}

	// TODO: fill this in for logging/error purposes.
	var tlfName CanonicalTlfName
	err = flushBlockEntries(ctx, j.log, j.delegateBlockServer,
		j.config.BlockCache(), j.config.Reporter(),
		j.tlfID, tlfName, entries)
	if err != nil {
		return 0, MetadataRevisionUninitialized, err
	}

	err = j.removeFlushedBlockEntries(ctx, entries)
	if err != nil {
		return 0, MetadataRevisionUninitialized, err
	}

	return entries.length(), maxMDRevToFlush, nil
}

func (j *tlfJournal) getNextMDEntryToFlush(ctx context.Context,
	end MetadataRevision) (MdID, *RootMetadataSigned, ExtraMetadata, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return MdID{}, nil, nil, err
	}

	return j.mdJournal.getNextEntryToFlush(ctx, end, j.config.Crypto())
}

func (j *tlfJournal) convertMDsToBranchLocked(
	ctx context.Context, bid BranchID) error {
	err := j.mdJournal.convertToBranch(
		ctx, bid, j.config.Crypto(), j.config.Codec(), j.tlfID,
		j.config.MDCache())
	if err != nil {
		return err
	}

	// Pause while on a conflict branch.
	j.pause(journalPauseConflict)

	if j.onBranchChange != nil {
		j.onBranchChange.onTLFBranchChange(j.tlfID, bid)
	}

	return nil
}

func (j *tlfJournal) convertMDsToBranch(ctx context.Context) error {
	bid, err := j.config.Crypto().MakeRandomBranchID()
	if err != nil {
		return err
	}

	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	return j.convertMDsToBranchLocked(ctx, bid)
}

func (j *tlfJournal) convertMDsToBranchIfOverThreshold(ctx context.Context) (
	bool, error) {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return false, err
	}

	ok, err := j.mdJournal.atLeastNNonLocalSquashes(ForcedBranchSquashThreshold)
	if err != nil {
		return false, err
	}
	if !ok {
		// Most of what's in the log are already local squashes, so no
		// need to squash it more.
		return false, nil
	}

	j.log.CDebugf(ctx, "Converting journal with more than %d "+
		"non-local-squash entries to a branch", ForcedBranchSquashThreshold)
	err = j.convertMDsToBranchLocked(ctx, PendingLocalSquashBranchID)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (j *tlfJournal) doOnMDFlush(ctx context.Context,
	rmds *RootMetadataSigned) error {
	if j.onMDFlush != nil {
		j.onMDFlush.onMDFlush(rmds.MD.TlfID(), rmds.MD.BID(),
			rmds.MD.RevisionNumber())
	}

	// Remove saved blocks in chunks to avoid starving foreground file
	// system operations that need the lock for too long.
	var lastToRemove journalOrdinal
	for {
		nextLastToRemove, removedBytes, err := func() (journalOrdinal, int64, error) {
			j.journalLock.Lock()
			defer j.journalLock.Unlock()
			if err := j.checkEnabledLocked(); err != nil {
				return 0, 0, err
			}

			storedBytesBefore := j.blockJournal.getStoredBytes()

			nextLastToRemove, removedBytes, err :=
				j.blockJournal.onMDFlush(
					ctx, maxSavedBlockRemovalsAtATime,
					rmds.MD.RevisionNumber(), lastToRemove)
			if err != nil {
				return 0, 0, err
			}

			storedBytesAfter := j.blockJournal.getStoredBytes()

			if storedBytesAfter != (storedBytesBefore - removedBytes) {
				panic(fmt.Sprintf(
					"storedBytes changed from %d to %d, but removedBytes is %d",
					storedBytesBefore, storedBytesAfter,
					removedBytes))
			}

			return nextLastToRemove, removedBytes, nil
		}()
		if err != nil {
			return err
		}
		if removedBytes > 0 {
			j.diskLimiter.Release(removedBytes)
		}
		if nextLastToRemove == 0 {
			break
		}
		lastToRemove = nextLastToRemove
		// Explicitly allow other goroutines (such as foreground file
		// system operations) to grab the lock to avoid starvation.
		// See https://github.com/golang/go/issues/13086.
		runtime.Gosched()
	}

	return nil
}

func (j *tlfJournal) removeFlushedMDEntry(ctx context.Context,
	mdID MdID, rmds *RootMetadataSigned) error {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	if err := j.mdJournal.removeFlushedEntry(ctx, mdID, rmds); err != nil {
		return err
	}

	j.unflushedPaths.removeFromCache(rmds.MD.RevisionNumber())
	return nil
}

func (j *tlfJournal) flushOneMDOp(
	ctx context.Context, end MetadataRevision,
	maxMDRevToFlush MetadataRevision) (flushed bool, err error) {
	j.log.CDebugf(ctx, "Flushing one MD to server")
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx, "Flush failed with %v", err)
		}
	}()

	mdServer := j.config.MDServer()

	mdID, rmds, extra, err := j.getNextMDEntryToFlush(ctx, end)
	if err != nil {
		return false, err
	}
	if mdID == (MdID{}) {
		return false, nil
	}

	// Only flush MDs for which the blocks have been fully flushed.
	if rmds.MD.RevisionNumber() > maxMDRevToFlush {
		j.log.CDebugf(ctx, "Haven't flushed all the blocks for TLF=%s "+
			"with id=%s, rev=%s, bid=%s yet (maxMDRevToFlush=%d)",
			rmds.MD.TlfID(), mdID, rmds.MD.RevisionNumber(), rmds.MD.BID(),
			maxMDRevToFlush)
		return false, nil
	}

	j.log.CDebugf(ctx, "Flushing MD for TLF=%s with id=%s, rev=%s, bid=%s",
		rmds.MD.TlfID(), mdID, rmds.MD.RevisionNumber(), rmds.MD.BID())
	pushErr := mdServer.Put(ctx, rmds, extra)
	if isRevisionConflict(pushErr) {
		headMdID, err := getMdID(ctx, mdServer, j.config.Crypto(),
			rmds.MD.TlfID(), rmds.MD.BID(), rmds.MD.MergedStatus(),
			rmds.MD.RevisionNumber())
		if err != nil {
			j.log.CWarningf(ctx,
				"getMdID failed for TLF %s, BID %s, and revision %d: %v",
				rmds.MD.TlfID(), rmds.MD.BID(), rmds.MD.RevisionNumber(), err)
		} else if headMdID == mdID {
			if headMdID == (MdID{}) {
				panic("nil earliestID and revision conflict error returned by pushEarliestToServer")
			}
			// We must have already flushed this MD, so continue.
			pushErr = nil
		} else if rmds.MD.MergedStatus() == Merged {
			j.log.CDebugf(ctx, "Conflict detected %v", pushErr)
			// Convert MDs to a branch and return -- the journal
			// pauses until the resolution is complete.
			err = j.convertMDsToBranch(ctx)
			if err != nil {
				return false, err
			}
			return false, nil
		}
	}
	if pushErr != nil {
		return false, pushErr
	}

	err = j.doOnMDFlush(ctx, rmds)
	if err != nil {
		return false, err
	}

	err = j.removeFlushedMDEntry(ctx, mdID, rmds)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (j *tlfJournal) getJournalEntryCounts() (
	blockEntryCount, mdEntryCount uint64, err error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return 0, 0, err
	}

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

func (j *tlfJournal) isOnConflictBranch() (bool, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()

	if err := j.checkEnabledLocked(); err != nil {
		return false, err
	}

	return j.mdJournal.getBranchID() != NullBranchID, nil
}

func (j *tlfJournal) getJournalStatusLocked() (TLFJournalStatus, error) {
	if err := j.checkEnabledLocked(); err != nil {
		return TLFJournalStatus{}, err
	}

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
	lastFlushErr := ""
	if j.lastFlushErr != nil {
		lastFlushErr = j.lastFlushErr.Error()
	}
	storedBytes := j.blockJournal.getStoredBytes()
	unflushedBytes := j.blockJournal.getUnflushedBytes()
	return TLFJournalStatus{
		Dir:            j.dir,
		BranchID:       j.mdJournal.getBranchID().String(),
		RevisionStart:  earliestRevision,
		RevisionEnd:    latestRevision,
		BlockOpCount:   blockEntryCount,
		StoredBytes:    storedBytes,
		UnflushedBytes: unflushedBytes,
		LastFlushErr:   lastFlushErr,
	}, nil
}

func (j *tlfJournal) getJournalStatus() (TLFJournalStatus, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	return j.getJournalStatusLocked()
}

// getJournalStatusWithRange returns the journal status, and either a
// non-nil unflushedPathsMap is returned, which can be used directly
// to fill in UnflushedPaths, or a list of ImmutableBareRootMetadatas
// is returned (along with a bool indicating whether that list is
// complete), which can be used to build an unflushedPathsMap. If
// complete is true, then the list may be empty; otherwise, it is
// guaranteed to not be empty.
func (j *tlfJournal) getJournalStatusWithRange() (
	jStatus TLFJournalStatus, unflushedPaths unflushedPathsMap,
	ibrmds []ImmutableBareRootMetadata, complete bool, err error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	jStatus, err = j.getJournalStatusLocked()
	if err != nil {
		return TLFJournalStatus{}, nil, nil, false, err
	}

	unflushedPaths = j.unflushedPaths.getUnflushedPaths()
	if unflushedPaths != nil {
		return jStatus, unflushedPaths, nil, true, nil
	}

	if jStatus.RevisionEnd == MetadataRevisionUninitialized {
		return jStatus, nil, nil, true, nil
	}

	complete = true
	stop := jStatus.RevisionEnd
	if stop > jStatus.RevisionStart+1000 {
		stop = jStatus.RevisionStart + 1000
		complete = false
	}
	// It would be nice to avoid getting this range if we are not
	// the initializer, but at this point we don't know if we'll
	// need to initialize or not.
	ibrmds, err = j.mdJournal.getRange(j.mdJournal.branchID,
		jStatus.RevisionStart, stop)
	if err != nil {
		return TLFJournalStatus{}, nil, nil, false, err
	}
	return jStatus, nil, ibrmds, complete, nil
}

// getUnflushedPathMDInfos converts the given list of bare root
// metadatas into unflushedPathMDInfo objects. The caller must NOT
// hold `j.journalLock`, because blocks from the journal may need to
// be read as part of the decryption.
func (j *tlfJournal) getUnflushedPathMDInfos(ctx context.Context,
	ibrmds []ImmutableBareRootMetadata) ([]unflushedPathMDInfo, error) {
	if len(ibrmds) == 0 {
		return nil, nil
	}

	ibrmdBareHandle, err := ibrmds[0].MakeBareTlfHandleWithExtra()
	if err != nil {
		return nil, err
	}

	handle, err := MakeTlfHandle(
		ctx, ibrmdBareHandle, j.config.usernameGetter())
	if err != nil {
		return nil, err
	}

	mdInfos := make([]unflushedPathMDInfo, 0, len(ibrmds))

	for _, ibrmd := range ibrmds {
		// TODO: Avoid having to do this type assertion and
		// convert to RootMetadata.
		brmd, ok := ibrmd.BareRootMetadata.(MutableBareRootMetadata)
		if !ok {
			return nil, MutableBareRootMetadataNoImplError{}
		}
		rmd := makeRootMetadata(brmd, ibrmd.extra, handle)

		pmd, err := decryptMDPrivateData(
			ctx, j.config.Codec(), j.config.Crypto(),
			j.config.BlockCache(), j.config.BlockOps(),
			j.config.mdDecryptionKeyGetter(), j.uid,
			rmd.GetSerializedPrivateMetadata(), rmd, rmd, j.log)
		if err != nil {
			return nil, err
		}

		mdInfo := unflushedPathMDInfo{
			revision:       ibrmd.RevisionNumber(),
			kmd:            rmd,
			pmd:            pmd,
			localTimestamp: ibrmd.localTimestamp,
		}
		mdInfos = append(mdInfos, mdInfo)
	}
	return mdInfos, nil
}

func (j *tlfJournal) getJournalStatusWithPaths(ctx context.Context,
	cpp chainsPathPopulator) (jStatus TLFJournalStatus, err error) {
	// This loop is limited only by the lifetime of `ctx`.
	var unflushedPaths unflushedPathsMap
	var complete bool
	for {
		var ibrmds []ImmutableBareRootMetadata
		jStatus, unflushedPaths, ibrmds, complete, err =
			j.getJournalStatusWithRange()
		if err != nil {
			return TLFJournalStatus{}, err
		}

		if unflushedPaths != nil {
			break
		}

		// We need to make or initialize the unflushed paths.
		if !complete {
			// Figure out the paths for the truncated MD range,
			// but don't cache it.
			unflushedPaths = make(unflushedPathsMap)
			j.log.CDebugf(ctx, "Making incomplete unflushed path cache")
			mdInfos, err := j.getUnflushedPathMDInfos(ctx, ibrmds)
			if err != nil {
				return TLFJournalStatus{}, err
			}
			err = addUnflushedPaths(ctx, j.uid, j.key,
				j.config.Codec(), j.log, mdInfos, cpp,
				unflushedPaths)
			if err != nil {
				return TLFJournalStatus{}, err
			}
			break
		}

		// We need to init it ourselves, or wait for someone else
		// to do it.
		doInit, err := j.unflushedPaths.startInitializeOrWait(ctx)
		if err != nil {
			return TLFJournalStatus{}, err
		}
		if doInit {
			initSuccess := false
			defer func() {
				if !initSuccess || err != nil {
					j.unflushedPaths.abortInitialization()
				}
			}()
			mdInfos, err := j.getUnflushedPathMDInfos(ctx, ibrmds)
			if err != nil {
				return TLFJournalStatus{}, err
			}
			unflushedPaths, initSuccess, err = j.unflushedPaths.initialize(
				ctx, j.uid, j.key, j.config.Codec(),
				j.log, cpp, mdInfos)
			if err != nil {
				return TLFJournalStatus{}, err
			}
			// All done!
			break
		}

		j.log.CDebugf(ctx, "Waited for unflushed paths initialization, "+
			"trying again to get the status")
	}

	pathsSeen := make(map[string]bool)
	for _, revPaths := range unflushedPaths {
		for path := range revPaths {
			if !pathsSeen[path] {
				jStatus.UnflushedPaths = append(jStatus.UnflushedPaths, path)
				pathsSeen[path] = true
			}
		}
	}

	if !complete {
		jStatus.UnflushedPaths =
			append(jStatus.UnflushedPaths, incompleteUnflushedPathsMarker)
	}
	return jStatus, nil
}

func (j *tlfJournal) getByteCounts() (
	storedBytes, unflushedBytes int64, err error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return 0, 0, err
	}

	return j.blockJournal.getStoredBytes(),
		j.blockJournal.getUnflushedBytes(), nil
}

func (j *tlfJournal) shutdown() {
	select {
	case j.needShutdownCh <- struct{}{}:
	default:
	}

	<-j.backgroundShutdownCh

	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		// Already shutdown.
		return
	}

	// Even if we shut down the journal, its blocks still take up
	// space, but we don't want to double-count them if we start
	// up this journal again, so we need to adjust them here.
	//
	// TODO: If we ever expect to shut down non-empty journals any
	// time other than during shutdown, we should still count
	// shut-down journals against the disk limit.
	storedBytes := j.blockJournal.getStoredBytes()
	if storedBytes > 0 {
		j.diskLimiter.OnJournalDisable(storedBytes)
	}

	// Make further accesses error out.
	j.blockJournal = nil
	j.mdJournal = nil
}

// disable prevents new operations from hitting the journal.  Will
// fail unless the journal is completely empty.
func (j *tlfJournal) disable() (wasEnabled bool, err error) {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	err = j.checkEnabledLocked()
	switch errors.Cause(err).(type) {
	case nil:
		// Continue.
		break
	case errTLFJournalDisabled:
		// Already disabled.
		return false, nil
	default:
		return false, err
	}

	blockEntryCount, err := j.blockJournal.length()
	if err != nil {
		return false, err
	}

	mdEntryCount, err := j.mdJournal.length()
	if err != nil {
		return false, err
	}

	// You can only disable an empty journal.
	if blockEntryCount > 0 || mdEntryCount > 0 {
		return false, errors.WithStack(errTLFJournalNotEmpty{})
	}

	j.disabled = true
	return true, nil
}

func (j *tlfJournal) enable() error {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	err := j.checkEnabledLocked()
	switch errors.Cause(err).(type) {
	case nil:
		// Already enabled.
		return nil
	case errTLFJournalDisabled:
		// Continue.
		break
	default:
		return err
	}

	j.disabled = false
	return nil
}

// All the functions below just do the equivalent blockJournal or
// mdJournal function under j.journalLock.

// getBlockData doesn't take a block context param, unlike the remote
// block server, since we still want to serve blocks even if all local
// references have been deleted (for example, a block that's been
// flushed but is still being and served on disk until the next
// successful MD flush).  This is safe because the journal doesn't
// support removing references for anything other than a flush (see
// the comment in tlfJournal.removeBlockReferences).
func (j *tlfJournal) getBlockData(id kbfsblock.ID) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	return j.blockJournal.getData(id)
}

// ErrDiskLimitTimeout is returned when putBlockData exceeds
// diskLimitTimeout when trying to acquire bytes to put.
type ErrDiskLimitTimeout struct {
	timeout        time.Duration
	requestedBytes int64
	availableBytes int64
	err            error
}

func (e ErrDiskLimitTimeout) Error() string {
	return fmt.Sprintf("Disk limit timeout of %s reached; requested %d bytes, only %d bytes available: %+v",
		e.timeout, e.requestedBytes, e.availableBytes, e.err)
}

func (j *tlfJournal) putBlockData(
	ctx context.Context, id kbfsblock.ID, blockCtx kbfsblock.Context, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf) (err error) {
	// Since Acquire can block, it should happen outside of the
	// journal lock.

	timeout := j.config.diskLimitTimeout()
	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bufLen := int64(len(buf))
	availableBytes, err := j.diskLimiter.Acquire(acquireCtx, bufLen)
	switch errors.Cause(err) {
	case nil:
		// Continue.
	case context.DeadlineExceeded:
		return errors.WithStack(ErrDiskLimitTimeout{
			timeout, bufLen, availableBytes, err,
		})
	default:
		return err
	}

	defer func() {
		if err != nil {
			j.diskLimiter.Release(bufLen)
		}
	}()

	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	storedBytesBefore := j.blockJournal.getStoredBytes()

	err = j.blockJournal.putData(ctx, id, blockCtx, buf, serverHalf)
	if err != nil {
		return err
	}

	storedBytesAfter := j.blockJournal.getStoredBytes()

	if storedBytesAfter != (storedBytesBefore + bufLen) {
		panic(fmt.Sprintf(
			"storedBytes changed from %d to %d, but bufLen is %d",
			storedBytesBefore, storedBytesAfter, bufLen))
	}

	j.config.Reporter().NotifySyncStatus(ctx, &keybase1.FSPathSyncStatus{
		PublicTopLevelFolder: j.tlfID.IsPublic(),
		// Path: TODO,
		// TODO: should this be the complete total for the file/directory,
		// rather than the diff?
		SyncingBytes: int64(len(buf)),
		// SyncingOps: TODO,
	})

	j.signalWork()

	return nil
}

func (j *tlfJournal) addBlockReference(
	ctx context.Context, id kbfsblock.ID, context kbfsblock.Context) error {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	err := j.blockJournal.addReference(ctx, id, context)
	if err != nil {
		return err
	}

	j.signalWork()

	return nil
}

func (j *tlfJournal) removeBlockReferences(
	ctx context.Context, contexts kbfsblock.ContextMap) (
	liveCounts map[kbfsblock.ID]int, err error) {
	// Currently the block journal will still serve block data even if
	// all journal references to a block have been removed (i.e.,
	// because they have all been flushed to the remote server).  If
	// we ever need to support the `BlockServer.RemoveReferences` call
	// in the journal, we might need to change the journal so that it
	// marks blocks as flushed-but-still-readable, so that we can
	// distinguish them from blocks that has had all its references
	// removed and shouldn't be served anymore.  For now, just fail
	// this call to make sure no uses of it creep in.
	return nil, errors.Errorf(
		"Removing block references is currently unsupported in the journal")
}

func (j *tlfJournal) archiveBlockReferences(
	ctx context.Context, contexts kbfsblock.ContextMap) error {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	err := j.blockJournal.archiveReferences(ctx, contexts)
	if err != nil {
		return err
	}

	j.signalWork()

	return nil
}

func (j *tlfJournal) isBlockUnflushed(id kbfsblock.ID) (bool, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return false, err
	}

	return j.blockJournal.isUnflushed(id)
}

func (j *tlfJournal) getBranchID() (BranchID, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return NullBranchID, err
	}

	return j.mdJournal.branchID, nil
}

func (j *tlfJournal) getMDHead(
	ctx context.Context, bid BranchID) (ImmutableBareRootMetadata, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return ImmutableBareRootMetadata{}, err
	}

	return j.mdJournal.getHead(bid)
}

func (j *tlfJournal) getMDRange(
	ctx context.Context, bid BranchID, start, stop MetadataRevision) (
	[]ImmutableBareRootMetadata, error) {
	j.journalLock.RLock()
	defer j.journalLock.RUnlock()
	if err := j.checkEnabledLocked(); err != nil {
		return nil, err
	}

	return j.mdJournal.getRange(bid, start, stop)
}

func (j *tlfJournal) doPutMD(ctx context.Context, rmd *RootMetadata,
	mdInfo unflushedPathMDInfo,
	perRevMap unflushedPathsPerRevMap) (
	mdID MdID, retryPut bool, err error) {
	// Now take the lock and put the MD, merging in the unflushed
	// paths while under the lock.
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return MdID{}, false, err
	}

	if !j.unflushedPaths.appendToCache(mdInfo, perRevMap) {
		return MdID{}, true, nil
	}
	// TODO: remove the revision from the cache on any errors below?
	// Tricky when the append is only queued.

	mdID, err = j.mdJournal.put(ctx, j.config.Crypto(),
		j.config.encryptionKeyGetter(), j.config.BlockSplitter(),
		rmd, false)
	if err != nil {
		return MdID{}, false, err
	}

	err = j.blockJournal.markMDRevision(ctx, rmd.Revision(), false)
	if err != nil {
		return MdID{}, false, err
	}

	j.signalWork()

	return mdID, false, nil
}

// prepAndAddRMDWithRetry prepare the paths without holding the lock,
// as `f` might need to take the lock.  This is a no-op if the
// unflushed path cache is uninitialized.  TODO: avoid doing this if
// we can somehow be sure the cache won't be initialized by the time
// we finish this operation.
func (j *tlfJournal) prepAndAddRMDWithRetry(ctx context.Context,
	rmd *RootMetadata,
	f func(unflushedPathMDInfo, unflushedPathsPerRevMap) (bool, error)) error {
	mdInfo := unflushedPathMDInfo{
		revision: rmd.Revision(),
		kmd:      rmd,
		pmd:      *rmd.Data(),
		// TODO: Plumb through clock?  Though the timestamp doesn't
		// matter for the unflushed path cache.
		localTimestamp: time.Now(),
	}
	perRevMap, err := j.unflushedPaths.prepUnflushedPaths(
		ctx, j.uid, j.key, j.config.Codec(), j.log, mdInfo)
	if err != nil {
		return err
	}

	retry, err := f(mdInfo, perRevMap)
	if err != nil {
		return err
	}

	if retry {
		// The cache was initialized after the last time we tried to
		// prepare the unflushed paths.
		perRevMap, err = j.unflushedPaths.prepUnflushedPaths(
			ctx, j.uid, j.key, j.config.Codec(), j.log, mdInfo)
		if err != nil {
			return err
		}

		retry, err := f(mdInfo, perRevMap)
		if err != nil {
			return err
		}

		if retry {
			return errors.New("Unexpectedly asked to retry " +
				"MD put more than once")
		}
	}
	return nil
}

func (j *tlfJournal) putMD(ctx context.Context, rmd *RootMetadata) (
	MdID, error) {
	var mdID MdID
	err := j.prepAndAddRMDWithRetry(ctx, rmd,
		func(mdInfo unflushedPathMDInfo, perRevMap unflushedPathsPerRevMap) (
			retry bool, err error) {
			mdID, retry, err = j.doPutMD(ctx, rmd, mdInfo, perRevMap)
			return retry, err
		})
	if err != nil {
		return MdID{}, err
	}
	return mdID, nil
}

func (j *tlfJournal) clearMDs(ctx context.Context, bid BranchID) error {
	if j.onBranchChange != nil {
		j.onBranchChange.onTLFBranchChange(j.tlfID, NullBranchID)
	}

	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return err
	}

	err := j.mdJournal.clear(ctx, bid)
	if err != nil {
		return err
	}

	j.resume(journalPauseConflict)
	return nil
}

func (j *tlfJournal) doResolveBranch(ctx context.Context,
	bid BranchID, blocksToDelete []kbfsblock.ID, rmd *RootMetadata,
	extra ExtraMetadata, mdInfo unflushedPathMDInfo,
	perRevMap unflushedPathsPerRevMap) (mdID MdID, retry bool, err error) {
	j.journalLock.Lock()
	defer j.journalLock.Unlock()
	if err := j.checkEnabledLocked(); err != nil {
		return MdID{}, false, err
	}

	// The set of unflushed paths could change as part of the
	// resolution, and the revision numbers definitely change.
	isPendingLocalSquash := bid == PendingLocalSquashBranchID
	if !j.unflushedPaths.reinitializeWithResolution(
		mdInfo, perRevMap, isPendingLocalSquash) {
		return MdID{}, true, nil
	}

	// First write the resolution to a new branch, and swap it with
	// the existing branch, then clear the existing branch.
	mdID, err = j.mdJournal.resolveAndClear(
		ctx, j.config.Crypto(), j.config.encryptionKeyGetter(),
		j.config.BlockSplitter(), j.config.MDCache(), bid, rmd)
	if err != nil {
		return MdID{}, false, err
	}

	// Then go through and mark blocks and md rev markers for ignoring.
	err = j.blockJournal.ignoreBlocksAndMDRevMarkers(ctx, blocksToDelete)
	if err != nil {
		return MdID{}, false, err
	}
	err = j.blockJournal.saveBlocksUntilNextMDFlush()
	if err != nil {
		return MdID{}, false, err
	}

	// Finally, append a new, non-ignored md rev marker for the new revision.
	err = j.blockJournal.markMDRevision(
		ctx, rmd.Revision(), isPendingLocalSquash)
	if err != nil {
		return MdID{}, false, err
	}

	j.resume(journalPauseConflict)
	j.signalWork()

	// TODO: kick off a background goroutine that deletes ignored
	// block data files before the flush gets to them.

	return mdID, false, nil
}

func (j *tlfJournal) resolveBranch(ctx context.Context,
	bid BranchID, blocksToDelete []kbfsblock.ID, rmd *RootMetadata,
	extra ExtraMetadata) (MdID, error) {
	var mdID MdID
	err := j.prepAndAddRMDWithRetry(ctx, rmd,
		func(mdInfo unflushedPathMDInfo, perRevMap unflushedPathsPerRevMap) (
			retry bool, err error) {
			mdID, retry, err = j.doResolveBranch(
				ctx, bid, blocksToDelete, rmd, extra, mdInfo, perRevMap)
			return retry, err
		})
	if err != nil {
		return MdID{}, err
	}
	return mdID, nil
}

func (j *tlfJournal) wait(ctx context.Context) error {
	workLeft, err := j.wg.WaitUnlessPaused(ctx)
	if err != nil {
		return err
	}
	if workLeft {
		j.log.CDebugf(ctx, "Wait completed with work left, "+
			"due to paused journal")
	}
	return nil
}
