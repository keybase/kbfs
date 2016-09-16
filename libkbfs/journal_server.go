// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"

	"golang.org/x/net/context"
)

// JournalServerStatus represents the overall status of the
// JournalServer for display in diagnostics. It is suitable for
// encoding directly as JSON.
type JournalServerStatus struct {
	RootDir        string
	JournalCount   int
	UnflushedBytes int64 // (signed because os.FileInfo.Size() is signed)
}

// branchChangeListener describes a caller that will get updates via
// the onTLFBranchChange method call when the journal branch changes
// for the given TlfID.  If a new branch has been created, the given
// BranchID will be something other than NullBranchID.  If the current
// branch was pruned, it will be NullBranchID.  If the implementer
// will be accessing the journal, it must do so from another goroutine
// to avoid deadlocks.
type branchChangeListener interface {
	onTLFBranchChange(TlfID, BranchID)
}

// mdFlushListener describes a caller that will ge updates via the
// onMDFlush metod when an MD is flushed.  If the implementer will be
// accessing the journal, it must do so from another goroutine to
// avoid deadlocks.
type mdFlushListener interface {
	onMDFlush(TlfID, BranchID, MetadataRevision)
}

// TODO: JournalServer isn't really a server, although it can create
// objects that act as servers. Rename to JournalManager.

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

	delegateBlockCache      BlockCache
	delegateDirtyBlockCache DirtyBlockCache
	delegateBlockServer     BlockServer
	delegateMDOps           MDOps
	onBranchChange          branchChangeListener
	onMDFlush               mdFlushListener

	lock                sync.RWMutex
	currentUID          keybase1.UID
	currentVerifyingKey VerifyingKey
	tlfJournals         map[TlfID]*tlfJournal
	dirtyOps            uint
}

func makeJournalServer(
	config Config, log logger.Logger, dir string,
	bcache BlockCache, dirtyBcache DirtyBlockCache, bserver BlockServer,
	mdOps MDOps, onBranchChange branchChangeListener,
	onMDFlush mdFlushListener) *JournalServer {
	jServer := JournalServer{
		config:                  config,
		log:                     log,
		deferLog:                log.CloneWithAddedDepth(1),
		dir:                     dir,
		delegateBlockCache:      bcache,
		delegateDirtyBlockCache: dirtyBcache,
		delegateBlockServer:     bserver,
		delegateMDOps:           mdOps,
		onBranchChange:          onBranchChange,
		onMDFlush:               onMDFlush,
		tlfJournals:             make(map[TlfID]*tlfJournal),
	}
	return &jServer
}

func (j *JournalServer) getTLFJournal(tlfID TlfID) (*tlfJournal, bool) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	tlfJournal, ok := j.tlfJournals[tlfID]
	return tlfJournal, ok
}

func (j *JournalServer) hasTLFJournal(tlfID TlfID) bool {
	j.lock.RLock()
	defer j.lock.RUnlock()
	_, ok := j.tlfJournals[tlfID]
	return ok
}

// EnableExistingJournals turns on the write journal for all TLFs with
// an existing journal. Any returned error means that the
// JournalServer may be used, but will pass through everything.
func (j *JournalServer) EnableExistingJournals(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey,
	bws TLFJournalBackgroundWorkStatus) (err error) {
	j.log.CDebugf(ctx, "Enabling existing journals (%s)", bws)
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

	j.lock.Lock()
	defer j.lock.Unlock()

	if j.currentUID != keybase1.UID("") {
		return fmt.Errorf("Trying to set current UID from %s to %s",
			j.currentUID, currentUID)
	}
	if j.currentVerifyingKey != (VerifyingKey{}) {
		return fmt.Errorf(
			"Trying to set current verifying key from %s to %s",
			j.currentVerifyingKey, currentVerifyingKey)
	}

	if currentUID == keybase1.UID("") {
		return errors.New("Current UID is empty")
	}
	if currentVerifyingKey == (VerifyingKey{}) {
		return errors.New("Current verifying key is empty")
	}

	// Need to set it here since enableLocked depends on it.
	//
	// TODO: Revert this on error or panic.
	j.currentUID = currentUID
	j.currentVerifyingKey = currentVerifyingKey

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

		// Force the enable, since any dirty writes in flight
		// are most likely for another user.
		err = j.enableLocked(ctx, tlfID, bws, true)
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

func (j *JournalServer) enableLocked(
	ctx context.Context, tlfID TlfID,
	bws TLFJournalBackgroundWorkStatus, force bool) (err error) {
	j.log.CDebugf(ctx, "Enabling journal for %s (%s)", tlfID, bws)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when enabling journal for %s: %v",
				tlfID, err)
		}
	}()

	if tlfJournal, ok := j.tlfJournals[tlfID]; ok {
		return tlfJournal.enable()
	}

	err = func() error {
		if j.dirtyOps > 0 {
			return fmt.Errorf("Can't enable journal for %s while there "+
				"are outstanding dirty ops", tlfID)
		}
		if j.delegateDirtyBlockCache.IsAnyDirty(tlfID) {
			return fmt.Errorf("Can't enable journal for %s while there "+
				"are any dirty blocks outstanding", tlfID)
		}
		return nil
	}()
	if err != nil {
		if !force {
			return err
		}

		j.log.CWarningf(ctx,
			"Got error on journal enable, but proceeding anyway: %v", err)
	}

	tlfJournal, err := makeTLFJournal(
		ctx, j.currentUID, j.currentVerifyingKey, j.dir, tlfID,
		tlfJournalConfigAdapter{j.config}, j.delegateBlockServer,
		bws, nil, j.onBranchChange, j.onMDFlush)
	if err != nil {
		return err
	}

	j.tlfJournals[tlfID] = tlfJournal
	return nil
}

// Enable turns on the write journal for the given TLF.
func (j *JournalServer) Enable(ctx context.Context, tlfID TlfID,
	bws TLFJournalBackgroundWorkStatus) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	return j.enableLocked(ctx, tlfID, bws, false)
}

func (j *JournalServer) dirtyOpStart(tlfID TlfID) {
	j.lock.Lock()
	defer j.lock.Unlock()
	j.dirtyOps++
}

func (j *JournalServer) dirtyOpEnd(tlfID TlfID) {
	j.lock.Lock()
	defer j.lock.Unlock()
	if j.dirtyOps == 0 {
		panic("Trying to end a dirty op when count is 0")
	}
	j.dirtyOps--
}

// PauseBackgroundWork pauses the background work goroutine, if it's
// not already paused.
func (j *JournalServer) PauseBackgroundWork(ctx context.Context, tlfID TlfID) {
	j.log.CDebugf(ctx, "Signaling pause for %s", tlfID)
	if tlfJournal, ok := j.getTLFJournal(tlfID); ok {
		tlfJournal.pauseBackgroundWork()
		return
	}

	j.log.CDebugf(ctx,
		"Could not find journal for %s; dropping pause signal",
		tlfID)
}

// ResumeBackgroundWork resumes the background work goroutine, if it's
// not already resumed.
func (j *JournalServer) ResumeBackgroundWork(ctx context.Context, tlfID TlfID) {
	j.log.CDebugf(ctx, "Signaling resume for %s", tlfID)
	if tlfJournal, ok := j.getTLFJournal(tlfID); ok {
		tlfJournal.resumeBackgroundWork()
		return
	}

	j.log.CDebugf(ctx,
		"Could not find journal for %s; dropping resume signal",
		tlfID)
}

// Flush flushes the write journal for the given TLF.
func (j *JournalServer) Flush(ctx context.Context, tlfID TlfID) (err error) {
	j.log.CDebugf(ctx, "Flushing journal for %s", tlfID)
	if tlfJournal, ok := j.getTLFJournal(tlfID); ok {
		return tlfJournal.flush(ctx)
	}

	j.log.CDebugf(ctx, "Journal not enabled for %s", tlfID)
	return nil
}

// Wait blocks until the write journal has finished flushing
// everything.  It is essentially the same as Flush() when the journal
// is enabled and unpaused, except that it is safe to cancel the
// context without leaving the journal in a partially-flushed state.
func (j *JournalServer) Wait(ctx context.Context, tlfID TlfID) (err error) {
	j.log.CDebugf(ctx, "Waiting on journal for %s", tlfID)
	if tlfJournal, ok := j.getTLFJournal(tlfID); ok {
		return tlfJournal.wait(ctx)
	}

	j.log.CDebugf(ctx, "Journal not enabled for %s", tlfID)
	return nil
}

// Disable turns off the write journal for the given TLF.
func (j *JournalServer) Disable(ctx context.Context, tlfID TlfID) (
	wasEnabled bool, err error) {
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
	tlfJournal, ok := j.tlfJournals[tlfID]
	if !ok {
		j.log.CDebugf(ctx, "Journal already existed for %s", tlfID)
		return false, nil
	}

	if j.dirtyOps > 0 {
		return false, fmt.Errorf("Can't disable journal for %s while there "+
			"are outstanding dirty ops", tlfID)
	}
	if j.delegateDirtyBlockCache.IsAnyDirty(tlfID) {
		return false, fmt.Errorf("Can't disable journal for %s while there "+
			"are any dirty blocks outstanding", tlfID)
	}

	// Disable the journal.  Note that we don't bother deleting the
	// journal from j.tlfJournals, to avoid cases where something
	// keeps it around doing background work or re-enables it, at the
	// same time JournalServer creates a new journal for the same TLF.
	wasEnabled, err = tlfJournal.disable()
	if err != nil {
		return false, err
	}

	if wasEnabled {
		j.log.CDebugf(ctx, "Disabled journal for %s", tlfID)
	}
	return wasEnabled, nil
}

func (j *JournalServer) blockCache() journalBlockCache {
	return journalBlockCache{j, j.delegateBlockCache}
}

func (j *JournalServer) dirtyBlockCache(
	journalCache DirtyBlockCache) journalDirtyBlockCache {
	return journalDirtyBlockCache{j, j.delegateDirtyBlockCache, journalCache}
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
	journalCount, unflushedBytes := func() (int, int64) {
		j.lock.RLock()
		defer j.lock.RUnlock()
		var unflushedBytes int64
		for _, tlfJournal := range j.tlfJournals {
			unflushedBytes += tlfJournal.getUnflushedBytes()
		}
		return len(j.tlfJournals), unflushedBytes
	}()
	return JournalServerStatus{
		RootDir:        j.dir,
		JournalCount:   journalCount,
		UnflushedBytes: unflushedBytes,
	}
}

// JournalStatus returns a TLFServerStatus object for the given TLF
// suitable for diagnostics.
func (j *JournalServer) JournalStatus(tlfID TlfID) (TLFJournalStatus, error) {
	tlfJournal, ok := j.getTLFJournal(tlfID)
	if !ok {
		return TLFJournalStatus{},
			fmt.Errorf("Journal not enabled for %s", tlfID)
	}

	return tlfJournal.getJournalStatus()
}

func (j *JournalServer) logIn(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey) {
	j.log.CDebugf(context.Background(), "Logging in journal with %s", currentUID)
	// TODO: Preserve background work status.
	j.EnableExistingJournals(ctx, currentUID, currentVerifyingKey,
		TLFJournalBackgroundWorkEnabled)
}

func (j *JournalServer) logOut(ctx context.Context) {
	j.log.CDebugf(context.Background(), "Logging out journal")
	j.lock.Lock()
	defer j.lock.Unlock()
	for _, tlfJournal := range j.tlfJournals {
		tlfJournal.shutdown()
	}

	// Logging out atomically is more important than respecting
	// context deadlines.
	waitCtx := context.Background()
	for tlfID, tlfJournal := range j.tlfJournals {
		err := tlfJournal.wait(waitCtx)
		if err != nil {
			// This shouldn't really happen, since it only
			// happens when wait's passed-in context is
			// cancelled.
			j.log.CDebugf(ctx,
				"Got unexpected error when shutting down journal for %s: %v",
				tlfID, err)

		}
	}

	j.tlfJournals = make(map[TlfID]*tlfJournal)
	j.currentUID = keybase1.UID("")
	j.currentVerifyingKey = VerifyingKey{}
}

func (j *JournalServer) shutdown() {
	j.log.CDebugf(context.Background(), "Shutting down journal")
	j.lock.Lock()
	defer j.lock.Unlock()
	for _, tlfJournal := range j.tlfJournals {
		tlfJournal.shutdown()
	}

	// Leave all the tlfJournals in j.tlfJournals, so that any
	// access to them errors out instead of mutating the journal.
}
