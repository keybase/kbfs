// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// TODO: Add a server endpoint to get this data.
var journalingBetaList = map[keybase1.UID]bool{
	"23260c2ce19420f97b58d7d95b68ca00": true, // Chris Coyne "chris"
	"dbb165b7879fe7b1174df73bed0b9500": true, // Max Krohn, "max"
	"ef2e49961eddaa77094b45ed635cfc00": true, // Jeremy Stribling, "strib"
	"41b1f75fb55046d370608425a3208100": true, // Jack O'Connor, "oconnor663"
	"9403ede05906b942fd7361f40a679500": true, // Jinyang Li, "jinyang"
	"b7c2eaddcced7727bcb229751d91e800": true, // Gabriel Handford, "gabrielh"
	"1563ec26dc20fd162a4f783551141200": true, // Patrick Crosby, "patrick"
	"ebbe1d99410ab70123262cf8dfc87900": true, // Fred Akalin, "akalin"
	"8bc0fd2f5fefd30d3ec04452600f4300": true, // Andy Alness, "alness"
	"e0b4166c9c839275cf5633ff65c3e819": true, // Chris Nojima, "chrisnojima"
	"d95f137b3b4a3600bc9e39350adba819": true, // Cécile Boucheron, "cecileb"
	"4c230ae8d2f922dc2ccc1d2f94890700": true, // Marco Polo, "marcopolo"
	"237e85db5d939fbd4b84999331638200": true, // Chris Ball, "cjb"
	"69da56f622a2ac750b8e590c3658a700": true, // John Zila, "jzila"
	"673a740cd20fb4bd348738b16d228219": true, // Steve Sanders, "zanderz"
	"95e88f2087e480cae28f08d81554bc00": true, // Mike Maxim, "mikem"
	"5c2ef2d4eddd2381daa681ac1a901519": true, // Max Goodman, "chromakode"
	"08abe80bd2da8984534b2d8f7b12c700": true, // Song Gao, "songgao"
	"eb08cb06e608ea41bd893946445d7919": true, // Miles Steele, "mlsteele"
}

type journalServerConfig struct {
	// EnableAuto, if true, means the user has explicitly set its
	// value. If false, then either the user turned it on and then
	// off, or the user hasn't turned it on at all.
	EnableAuto bool

	// EnableAutoSetByUser means the user has explicitly set the
	// value of EnableAuto (after this field was added).
	EnableAutoSetByUser bool
}

func (jsc journalServerConfig) getEnableAuto(currentUID keybase1.UID) (
	enableAuto, enableAutoSetByUser bool) {
	// If EnableAuto is true, the user has explicitly set its value.
	if jsc.EnableAuto {
		return true, true
	}

	// Otherwise, if EnableAutoSetByUser is true, it means the
	// user has explicitly set the value of EnableAuto (after that
	// field was added).
	if jsc.EnableAutoSetByUser {
		return false, true
	}

	// Otherwise, either the user turned on journaling and then
	// turned it off before that field was added, or the user
	// hasn't touched the field. In either case, determine the
	// value based on whether the current UID is in the
	// journaling beta list.
	return journalingBetaList[currentUID], false
}

// JournalServerStatus represents the overall status of the
// JournalServer for display in diagnostics. It is suitable for
// encoding directly as JSON.
type JournalServerStatus struct {
	RootDir             string
	Version             int
	CurrentUID          keybase1.UID
	CurrentVerifyingKey kbfscrypto.VerifyingKey
	EnableAuto          bool
	EnableAutoSetByUser bool
	JournalCount        int
	// The byte counters below are signed because
	// os.FileInfo.Size() is signed. The file counter is signed
	// for consistency.
	StoredBytes       int64
	StoredFiles       int64
	UnflushedBytes    int64
	UnflushedPaths    []string
	DiskLimiterStatus interface{}
}

// branchChangeListener describes a caller that will get updates via
// the onTLFBranchChange method call when the journal branch changes
// for the given TlfID.  If a new branch has been created, the given
// BranchID will be something other than NullBranchID.  If the current
// branch was pruned, it will be NullBranchID.  If the implementer
// will be accessing the journal, it must do so from another goroutine
// to avoid deadlocks.
type branchChangeListener interface {
	onTLFBranchChange(tlf.ID, BranchID)
}

// mdFlushListener describes a caller that will ge updates via the
// onMDFlush metod when an MD is flushed.  If the implementer will be
// accessing the journal, it must do so from another goroutine to
// avoid deadlocks.
type mdFlushListener interface {
	onMDFlush(tlf.ID, BranchID, MetadataRevision)
}

// diskLimiter is an interface for limiting disk usage.
type diskLimiter interface {
	// onJournalEnable is called when initializing a TLF journal
	// with that journal's current disk usage. Both journalBytes
	// and journalFiles must be >= 0. The updated available byte
	// and file count must be returned.
	onJournalEnable(
		ctx context.Context, journalBytes, journalFiles int64) (
		availableBytes, availableFiles int64)

	// onJournalDisable is called when shutting down a TLF journal
	// with that journal's current disk usage. Both journalBytes
	// and journalFiles must be >= 0.
	onJournalDisable(ctx context.Context, journalBytes, journalFiles int64)

	// beforeBlockPut is called before putting a block of the
	// given byte and file count, both of which must be > 0. It
	// may block, but must return immediately with a
	// (possibly-wrapped) ctx.Err() if ctx is cancelled. The
	// updated available byte and file count must be returned,
	// even if err is non-nil.
	beforeBlockPut(ctx context.Context,
		blockBytes, blockFiles int64) (
		availableBytes, availableFiles int64, err error)

	// afterBlockPut is called after putting a block of the given
	// byte and file count, which must match the corresponding call to
	// beforeBlockPut. putData reflects whether or not the data
	// was actually put; if it's false, it's either because of an
	// error or because the block already existed.
	afterBlockPut(ctx context.Context,
		blockBytes, blockFiles int64, putData bool)

	// onBlockDelete is called after deleting a block of the given
	// byte and file count, both of which must be >= 0. (Deleting
	// a block with either zero byte or zero file count shouldn't
	// happen, but may as well let it go through.)
	onBlockDelete(ctx context.Context, blockBytes, blockFiles int64)

	// getStatus returns an object that's marshallable into JSON
	// for use in displaying status.
	getStatus() interface{}
}

// TODO: JournalServer isn't really a server, although it can create
// objects that act as servers. Rename to JournalManager.

// JournalServer is the server that handles write journals. It
// interposes itself in front of BlockServer and MDOps. It uses MDOps
// instead of MDServer because it has to potentially modify the
// RootMetadata passed in, and by the time it hits MDServer it's
// already too late. However, this assumes that all MD ops go through
// MDOps.
//
// The maximum number of characters added to the root dir by a journal
// server journal is 108: 51 for the TLF journal, and 57 for
// everything else.
//
//   /v1/de...-...(53 characters total)...ff(/tlf journal)
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

	diskLimiter diskLimiter

	// Protects all fields below.
	lock                sync.RWMutex
	currentUID          keybase1.UID
	currentVerifyingKey kbfscrypto.VerifyingKey
	tlfJournals         map[tlf.ID]*tlfJournal
	dirtyOps            uint
	dirtyOpsDone        *sync.Cond
	serverConfig        journalServerConfig
}

func makeJournalServer(
	config Config, log logger.Logger, dir string,
	bcache BlockCache, dirtyBcache DirtyBlockCache, bserver BlockServer,
	mdOps MDOps, onBranchChange branchChangeListener,
	onMDFlush mdFlushListener, diskLimiter diskLimiter) *JournalServer {
	if len(dir) == 0 {
		panic("journal root path string unexpectedly empty")
	}
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
		tlfJournals:             make(map[tlf.ID]*tlfJournal),
		diskLimiter:             diskLimiter,
	}
	jServer.dirtyOpsDone = sync.NewCond(&jServer.lock)
	return &jServer
}

func (j *JournalServer) rootPath() string {
	return filepath.Join(j.dir, "v1")
}

func (j *JournalServer) configPath() string {
	return filepath.Join(j.rootPath(), "config.json")
}

func (j *JournalServer) readConfig() error {
	return ioutil.DeserializeFromJSONFile(j.configPath(), &j.serverConfig)
}

func (j *JournalServer) writeConfig() error {
	return ioutil.SerializeToJSONFile(j.serverConfig, j.configPath())
}

func (j *JournalServer) tlfJournalPathLocked(tlfID tlf.ID) string {
	if j.currentVerifyingKey == (kbfscrypto.VerifyingKey{}) {
		panic("currentVerifyingKey is zero")
	}

	// We need to generate a unique path for each (UID, device,
	// TLF) tuple. Verifying keys (which are unique to a device)
	// are globally unique, so no need to have the uid in the
	// path. Furthermore, everything after the first two bytes
	// (four characters) is randomly generated, so taking the
	// first 36 characters of the verifying key gives us 16 random
	// bytes (since the first two bytes encode version/type) or
	// 128 random bits, which means that the expected number of
	// devices generated before getting a collision in the first
	// part of the path is 2^64 (see
	// https://en.wikipedia.org/wiki/Birthday_problem#Cast_as_a_collision_problem
	// ).
	//
	// By similar reasoning, for a single device, taking the first
	// 16 characters of the TLF ID gives us 64 random bits, which
	// means that the expected number of TLFs associated to that
	// device before getting a collision in the second part of the
	// path is 2^32.
	shortDeviceIDStr := j.currentVerifyingKey.String()[:36]
	shortTlfIDStr := tlfID.String()[:16]
	dir := fmt.Sprintf("%s-%s", shortDeviceIDStr, shortTlfIDStr)
	return filepath.Join(j.rootPath(), dir)
}

func (j *JournalServer) getEnableAutoLocked() (
	enableAuto, enableAutoSetByUser bool) {
	return j.serverConfig.getEnableAuto(j.currentUID)
}

func (j *JournalServer) getTLFJournal(tlfID tlf.ID) (*tlfJournal, bool) {
	getJournalFn := func() (*tlfJournal, bool, bool, bool) {
		j.lock.RLock()
		defer j.lock.RUnlock()
		tlfJournal, ok := j.tlfJournals[tlfID]
		enableAuto, enableAutoSetByUser := j.getEnableAutoLocked()
		return tlfJournal, enableAuto, enableAutoSetByUser, ok
	}
	tlfJournal, enableAuto, enableAutoSetByUser, ok := getJournalFn()
	if !ok && enableAuto {
		ctx := context.TODO() // plumb through from callers
		j.log.CDebugf(ctx, "Enabling a new journal for %s (enableAuto=%t, set by user=%t)",
			tlfID, enableAuto, enableAutoSetByUser)
		err := j.Enable(ctx, tlfID, TLFJournalBackgroundWorkEnabled)
		if err != nil {
			j.log.CWarningf(ctx, "Couldn't enable journal for %s: %+v", tlfID, err)
			return nil, false
		}
		tlfJournal, _, _, ok = getJournalFn()
	}
	return tlfJournal, ok
}

func (j *JournalServer) hasTLFJournal(tlfID tlf.ID) bool {
	j.lock.RLock()
	defer j.lock.RUnlock()
	_, ok := j.tlfJournals[tlfID]
	return ok
}

// EnableExistingJournals turns on the write journal for all TLFs for
// the given (UID, device) tuple (with the device identified by its
// verifying key) with an existing journal. Any returned error means
// that the JournalServer remains in the same state as it was before.
//
// Once this is called, this must not be called again until
// shutdownExistingJournals is called.
func (j *JournalServer) EnableExistingJournals(
	ctx context.Context, currentUID keybase1.UID,
	currentVerifyingKey kbfscrypto.VerifyingKey,
	bws TLFJournalBackgroundWorkStatus) (err error) {
	j.log.CDebugf(ctx, "Enabling existing journals (%s)", bws)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when enabling existing journals: %+v",
				err)
		}
	}()

	// TODO: We should also look up journals from other
	// users/devices so that we can take into account their
	// journal usage.

	j.lock.Lock()
	defer j.lock.Unlock()

	err = j.readConfig()
	switch {
	case ioutil.IsNotExist(err):
		// Config file doesn't exist, so write it.
		err := j.writeConfig()
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}

	if j.currentUID != keybase1.UID("") {
		return errors.Errorf("Trying to set current UID from %s to %s",
			j.currentUID, currentUID)
	}
	if j.currentVerifyingKey != (kbfscrypto.VerifyingKey{}) {
		return errors.Errorf(
			"Trying to set current verifying key from %s to %s",
			j.currentVerifyingKey, currentVerifyingKey)
	}

	if currentUID == keybase1.UID("") {
		return errors.New("Current UID is empty")
	}
	if currentVerifyingKey == (kbfscrypto.VerifyingKey{}) {
		return errors.New("Current verifying key is empty")
	}

	// Need to set it here since tlfJournalPathLocked and
	// enableLocked depend on it.
	j.currentUID = currentUID
	j.currentVerifyingKey = currentVerifyingKey

	enableSucceeded := false
	defer func() {
		// Revert to a clean state if the enable doesn't
		// succeed, either due to a panic or error.
		if !enableSucceeded {
			j.shutdownExistingJournalsLocked(ctx)
		}
	}()

	fileInfos, err := ioutil.ReadDir(j.rootPath())
	if ioutil.IsNotExist(err) {
		enableSucceeded = true
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

		dir := filepath.Join(j.rootPath(), name)
		uid, key, tlfID, err := readTLFJournalInfoFile(dir)
		if err != nil {
			j.log.CDebugf(
				ctx, "Skipping non-TLF dir %q: %+v", name, err)
			continue
		}

		if uid != currentUID {
			j.log.CDebugf(
				ctx, "Skipping dir %q due to mismatched UID %s",
				name, uid)
			continue
		}

		if key != currentVerifyingKey {
			j.log.CDebugf(
				ctx, "Skipping dir %q due to mismatched key %s",
				name, uid)
			continue
		}

		expectedDir := j.tlfJournalPathLocked(tlfID)
		if dir != expectedDir {
			j.log.CDebugf(
				ctx, "Skipping misnamed dir %q; expected %q",
				dir, expectedDir)
			continue
		}

		// Allow enable even if dirty, since any dirty writes
		// in flight are most likely for another user.
		err = j.enableLocked(ctx, tlfID, bws, true)
		if err != nil {
			// Don't treat per-TLF errors as fatal.
			j.log.CWarningf(
				ctx, "Error when enabling existing journal for %s: %+v",
				tlfID, err)
			continue
		}
	}

	enableSucceeded = true
	return nil
}

func (j *JournalServer) enableLocked(
	ctx context.Context, tlfID tlf.ID, bws TLFJournalBackgroundWorkStatus,
	allowEnableIfDirty bool) (err error) {
	j.log.CDebugf(ctx, "Enabling journal for %s (%s)", tlfID, bws)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when enabling journal for %s: %+v",
				tlfID, err)
		}
	}()

	if j.currentUID == keybase1.UID("") {
		return errors.New("Current UID is empty")
	}
	if j.currentVerifyingKey == (kbfscrypto.VerifyingKey{}) {
		return errors.New("Current verifying key is empty")
	}

	if tlfJournal, ok := j.tlfJournals[tlfID]; ok {
		return tlfJournal.enable()
	}

	err = func() error {
		if j.dirtyOps > 0 {
			return errors.Errorf("Can't enable journal for %s while there "+
				"are outstanding dirty ops", tlfID)
		}
		if j.delegateDirtyBlockCache.IsAnyDirty(tlfID) {
			return errors.Errorf("Can't enable journal for %s while there "+
				"are any dirty blocks outstanding", tlfID)
		}
		return nil
	}()
	if err != nil {
		if !allowEnableIfDirty {
			return err
		}

		j.log.CWarningf(ctx,
			"Got ignorable error on journal enable, and proceeding anyway: %+v", err)
	}

	tlfDir := j.tlfJournalPathLocked(tlfID)
	tlfJournal, err := makeTLFJournal(
		ctx, j.currentUID, j.currentVerifyingKey, tlfDir,
		tlfID, tlfJournalConfigAdapter{j.config}, j.delegateBlockServer,
		bws, nil, j.onBranchChange, j.onMDFlush, j.diskLimiter)
	if err != nil {
		return err
	}

	j.tlfJournals[tlfID] = tlfJournal
	return nil
}

// Enable turns on the write journal for the given TLF.
func (j *JournalServer) Enable(ctx context.Context, tlfID tlf.ID,
	bws TLFJournalBackgroundWorkStatus) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	return j.enableLocked(ctx, tlfID, bws, false)
}

// EnableAuto turns on the write journal for all TLFs, even new ones,
// persistently.
func (j *JournalServer) EnableAuto(ctx context.Context) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	if j.serverConfig.EnableAuto {
		// Nothing to do.
		return nil
	}

	j.log.CDebugf(ctx, "Enabling auto-journaling")
	j.serverConfig.EnableAuto = true
	j.serverConfig.EnableAutoSetByUser = true
	return j.writeConfig()
}

// DisableAuto turns off automatic write journal for any
// newly-accessed TLFs.  Existing journaled TLFs need to be disabled
// manually.
func (j *JournalServer) DisableAuto(ctx context.Context) error {
	j.lock.Lock()
	defer j.lock.Unlock()
	if !j.serverConfig.EnableAuto {
		// Nothing to do.
		return nil
	}

	j.log.CDebugf(ctx, "Disabling auto-journaling")
	j.serverConfig.EnableAuto = false
	j.serverConfig.EnableAutoSetByUser = true
	return j.writeConfig()
}

func (j *JournalServer) dirtyOpStart(tlfID tlf.ID) {
	j.lock.Lock()
	defer j.lock.Unlock()
	j.dirtyOps++
}

func (j *JournalServer) dirtyOpEnd(tlfID tlf.ID) {
	j.lock.Lock()
	defer j.lock.Unlock()
	if j.dirtyOps == 0 {
		panic("Trying to end a dirty op when count is 0")
	}
	j.dirtyOps--
	if j.dirtyOps == 0 {
		j.dirtyOpsDone.Broadcast()
	}
}

// PauseBackgroundWork pauses the background work goroutine, if it's
// not already paused.
func (j *JournalServer) PauseBackgroundWork(ctx context.Context, tlfID tlf.ID) {
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
func (j *JournalServer) ResumeBackgroundWork(ctx context.Context, tlfID tlf.ID) {
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
func (j *JournalServer) Flush(ctx context.Context, tlfID tlf.ID) (err error) {
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
func (j *JournalServer) Wait(ctx context.Context, tlfID tlf.ID) (err error) {
	j.log.CDebugf(ctx, "Waiting on journal for %s", tlfID)
	if tlfJournal, ok := j.getTLFJournal(tlfID); ok {
		return tlfJournal.wait(ctx)
	}

	j.log.CDebugf(ctx, "Journal not enabled for %s", tlfID)
	return nil
}

// Disable turns off the write journal for the given TLF.
func (j *JournalServer) Disable(ctx context.Context, tlfID tlf.ID) (
	wasEnabled bool, err error) {
	j.log.CDebugf(ctx, "Disabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Error when disabling journal for %s: %+v",
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
		return false, errors.Errorf("Can't disable journal for %s while there "+
			"are outstanding dirty ops", tlfID)
	}
	if j.delegateDirtyBlockCache.IsAnyDirty(tlfID) {
		return false, errors.Errorf("Can't disable journal for %s while there "+
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
// diagnostics.  It also returns a list of TLF IDs which have journals
// enabled.
func (j *JournalServer) Status(
	ctx context.Context) (JournalServerStatus, []tlf.ID) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	var totalStoredBytes, totalStoredFiles, totalUnflushedBytes int64
	tlfIDs := make([]tlf.ID, 0, len(j.tlfJournals))
	for _, tlfJournal := range j.tlfJournals {
		storedBytes, storedFiles, unflushedBytes, err :=
			tlfJournal.getByteCounts()
		if err != nil {
			j.log.CWarningf(ctx,
				"Couldn't calculate stored bytes/stored files/unflushed bytes for %s: %+v",
				tlfJournal.tlfID, err)
		}
		totalStoredBytes += storedBytes
		totalStoredFiles += storedFiles
		totalUnflushedBytes += unflushedBytes
		tlfIDs = append(tlfIDs, tlfJournal.tlfID)
	}
	enableAuto, enableAutoSetByUser := j.getEnableAutoLocked()
	return JournalServerStatus{
		RootDir:             j.rootPath(),
		Version:             1,
		CurrentUID:          j.currentUID,
		CurrentVerifyingKey: j.currentVerifyingKey,
		EnableAuto:          enableAuto,
		EnableAutoSetByUser: enableAutoSetByUser,
		JournalCount:        len(tlfIDs),
		StoredBytes:         totalStoredBytes,
		StoredFiles:         totalStoredFiles,
		UnflushedBytes:      totalUnflushedBytes,
		DiskLimiterStatus:   j.diskLimiter.getStatus(),
	}, tlfIDs
}

// JournalStatus returns a TLFServerStatus object for the given TLF
// suitable for diagnostics.
func (j *JournalServer) JournalStatus(tlfID tlf.ID) (
	TLFJournalStatus, error) {
	tlfJournal, ok := j.getTLFJournal(tlfID)
	if !ok {
		return TLFJournalStatus{},
			errors.Errorf("Journal not enabled for %s", tlfID)
	}

	return tlfJournal.getJournalStatus()
}

// JournalStatusWithPaths returns a TLFServerStatus object for the
// given TLF suitable for diagnostics, including paths for all the
// unflushed entries.
func (j *JournalServer) JournalStatusWithPaths(ctx context.Context,
	tlfID tlf.ID, cpp chainsPathPopulator) (TLFJournalStatus, error) {
	tlfJournal, ok := j.getTLFJournal(tlfID)
	if !ok {
		return TLFJournalStatus{},
			errors.Errorf("Journal not enabled for %s", tlfID)
	}

	return tlfJournal.getJournalStatusWithPaths(ctx, cpp)
}

// shutdownExistingJournalsLocked shuts down all write journals, sets
// the current UID and verifying key to zero, and returns once all
// shutdowns are complete. It is safe to call multiple times in a row,
// and once this is called, EnableExistingJournals may be called
// again.
func (j *JournalServer) shutdownExistingJournalsLocked(ctx context.Context) {
	for j.dirtyOps > 0 {
		j.log.CDebugf(ctx,
			"Waiting for %d dirty ops before shutting down existing journals...", j.dirtyOps)
		j.dirtyOpsDone.Wait()
	}

	j.log.CDebugf(ctx, "Shutting down existing journals")

	for _, tlfJournal := range j.tlfJournals {
		tlfJournal.shutdown(ctx)
	}

	j.tlfJournals = make(map[tlf.ID]*tlfJournal)
	j.currentUID = keybase1.UID("")
	j.currentVerifyingKey = kbfscrypto.VerifyingKey{}
}

// shutdownExistingJournals shuts down all write journals, sets the
// current UID and verifying key to zero, and returns once all
// shutdowns are complete. It is safe to call multiple times in a row,
// and once this is called, EnableExistingJournals may be called
// again.
func (j *JournalServer) shutdownExistingJournals(ctx context.Context) {
	j.lock.Lock()
	defer j.lock.Unlock()
	j.shutdownExistingJournalsLocked(ctx)
}

func (j *JournalServer) shutdown(ctx context.Context) {
	j.log.CDebugf(ctx, "Shutting down journal")
	j.lock.Lock()
	defer j.lock.Unlock()
	for _, tlfJournal := range j.tlfJournals {
		tlfJournal.shutdown(ctx)
	}

	// Leave all the tlfJournals in j.tlfJournals, so that any
	// access to them errors out instead of mutating the journal.
}
