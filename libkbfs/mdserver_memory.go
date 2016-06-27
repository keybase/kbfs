// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

// An mdHandleKey is an encoded BareTlfHandle.
type mdHandleKey string

type mdBlockKey struct {
	tlfID    TlfID
	branchID BranchID
}

type mdBranchKey struct {
	tlfID     TlfID
	deviceKID keybase1.KID
}

type mdBlockMem struct {
	// An encoded RootMetdataSigned.
	encodedMd []byte
	timestamp time.Time
}

// MDServerMemory just stores blocks in local leveldb instances.
type MDServerMemory struct {
	config Config
	log    logger.Logger

	handleMutex *sync.RWMutex
	// Bare TLF handle -> TLF ID
	handleDb map[mdHandleKey]TlfID
	// TLF ID -> latest bare TLF handle
	latestHandleDb map[TlfID]BareTlfHandle

	mdMutex *sync.Mutex
	// (TLF ID, branch ID) -> [revision]mdBlockMem
	mdDb map[mdBlockKey][]mdBlockMem

	branchMutex *sync.Mutex
	// (TLF ID, device KID) -> branch ID
	branchDb map[mdBranchKey]BranchID

	locksMutex *sync.Mutex
	locksDb    map[TlfID]keybase1.KID // TLF ID -> device KID

	updateManager *mdServerLocalUpdateManager

	shutdownLock *sync.RWMutex
	shutdown     *bool
}

var _ mdServerLocal = (*MDServerMemory)(nil)

// NewMDServerMemory constructs a new MDServerMemory object that stores
// all data in-memory.
func NewMDServerMemory(config Config) (*MDServerMemory, error) {
	handleDb := make(map[mdHandleKey]TlfID)
	latestHandleDb := make(map[TlfID]BareTlfHandle)
	mdDb := make(map[mdBlockKey][]mdBlockMem)
	branchDb := make(map[mdBranchKey]BranchID)
	locksDb := make(map[TlfID]keybase1.KID)
	log := config.MakeLogger("")
	mdserv := &MDServerMemory{config, log, &sync.RWMutex{}, handleDb,
		latestHandleDb, &sync.Mutex{}, mdDb, &sync.Mutex{}, branchDb,
		&sync.Mutex{}, locksDb,
		newMDServerLocalUpdateManager(), &sync.RWMutex{}, new(bool)}
	return mdserv, nil
}

func (md *MDServerMemory) handleDbGet(h BareTlfHandle) (TlfID, bool, error) {
	hBytes, err := md.config.Codec().Encode(h)
	if err != nil {
		return NullTlfID, false, err
	}

	md.handleMutex.RLock()
	defer md.handleMutex.RUnlock()
	id, ok := md.handleDb[mdHandleKey(hBytes)]
	return id, ok, nil
}

func (md *MDServerMemory) handleDbPut(h BareTlfHandle, id TlfID) error {
	hBytes, err := md.config.Codec().Encode(h)
	if err != nil {
		return err
	}

	md.handleMutex.Lock()
	defer md.handleMutex.Unlock()
	md.handleDb[mdHandleKey(hBytes)] = id
	md.latestHandleDb[id] = h
	return nil
}

// GetForHandle implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetForHandle(ctx context.Context, handle BareTlfHandle,
	mStatus MergeStatus) (TlfID, *RootMetadataSigned, error) {
	id := NullTlfID
	if md.isShutdown() {
		return id, nil, errors.New("MD server already shut down")
	}

	id, ok, err := md.handleDbGet(handle)
	if err != nil {
		return id, nil, MDServerError{err}
	}
	if ok {
		rmds, err := md.GetForTLF(ctx, id, NullBranchID, mStatus)
		return id, rmds, err
	}

	// Non-readers shouldn't be able to create the dir.
	_, uid, err := md.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return id, nil, err
	}
	if !handle.IsReader(uid) {
		return id, nil, MDServerErrorUnauthorized{}
	}

	// Allocate a new random ID.
	id, err = md.config.Crypto().MakeRandomTlfID(handle.IsPublic())
	if err != nil {
		return id, nil, MDServerError{err}
	}

	err = md.handleDbPut(handle, id)
	if err != nil {
		return id, nil, MDServerError{err}
	}
	return id, nil, nil
}

// GetForTLF implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetForTLF(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus) (*RootMetadataSigned, error) {
	if md.isShutdown() {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	mergedMasterHead, err :=
		md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return nil, MDServerError{err}
	}

	// Check permissions
	ok, err := isReader(
		ctx, md.config.Codec(), md.config.KBPKI(), mergedMasterHead)
	if err != nil {
		return nil, MDServerError{err}
	}
	if !ok {
		return nil, MDServerErrorUnauthorized{}
	}

	// Lookup the branch ID if not supplied
	if mStatus == Unmerged && bid == NullBranchID {
		bid, err = md.getBranchID(ctx, id)
		if err != nil {
			return nil, err
		}
		if bid == NullBranchID {
			return nil, nil
		}
	}

	rmds, err := md.getHeadForTLF(ctx, id, bid, mStatus)
	if err != nil {
		return nil, MDServerError{err}
	}
	return rmds, nil
}

func (md *MDServerMemory) getHeadForTLF(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus) (*RootMetadataSigned, error) {
	key, err := md.getMDKey(id, bid, mStatus)
	if err != nil {
		return nil, err
	}
	md.mdMutex.Lock()
	defer md.mdMutex.Unlock()
	blocks := md.mdDb[key]
	if len(blocks) == 0 {
		return nil, nil
	}
	var rmds RootMetadataSigned
	err = md.config.Codec().Decode(blocks[len(blocks)-1].encodedMd, &rmds)
	if err != nil {
		return nil, err
	}
	return &rmds, nil
}

func (md *MDServerMemory) getMDKey(
	id TlfID, bid BranchID, mStatus MergeStatus) (mdBlockKey, error) {
	if (mStatus == Merged) != (bid == NullBranchID) {
		return mdBlockKey{},
			fmt.Errorf("mstatus=%v is inconsistent with bid=%v",
				mStatus, bid)
	}
	return mdBlockKey{id, bid}, nil
}

func (md *MDServerMemory) getBranchKey(ctx context.Context, id TlfID) (
	mdBranchKey, error) {
	// add device KID
	deviceKID, err := md.getCurrentDeviceKID(ctx)
	if err != nil {
		return mdBranchKey{}, err
	}
	return mdBranchKey{id, deviceKID}, nil
}

func (md *MDServerMemory) getCurrentDeviceKID(ctx context.Context) (keybase1.KID, error) {
	key, err := md.config.KBPKI().GetCurrentCryptPublicKey(ctx)
	if err != nil {
		return keybase1.KID(""), err
	}
	return key.kid, nil
}

// GetRange implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetRange(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus, start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	md.log.CDebugf(ctx, "GetRange %d %d (%s)", start, stop, mStatus)
	if md.isShutdown() {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	mergedMasterHead, err :=
		md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return nil, MDServerError{err}
	}

	// Check permissions
	ok, err := isReader(
		ctx, md.config.Codec(), md.config.KBPKI(), mergedMasterHead)
	if err != nil {
		return nil, MDServerError{err}
	}
	if !ok {
		return nil, MDServerErrorUnauthorized{}
	}

	// Lookup the branch ID if not supplied
	if mStatus == Unmerged && bid == NullBranchID {
		bid, err = md.getBranchID(ctx, id)
		if err != nil {
			return nil, err
		}
		if bid == NullBranchID {
			return nil, nil
		}
	}

	key, err := md.getMDKey(id, bid, mStatus)
	if err != nil {
		return nil, MDServerError{err}
	}

	md.mdMutex.Lock()
	defer md.mdMutex.Unlock()

	blocks := md.mdDb[key]

	startI := int(start - MetadataRevisionInitial)
	endI := int(stop - MetadataRevisionInitial + 1)
	if endI > len(blocks) {
		endI = len(blocks)
	}

	var rmdses []*RootMetadataSigned
	for i := startI; i < endI; i++ {
		var rmds RootMetadataSigned
		err = md.config.Codec().Decode(blocks[i].encodedMd, &rmds)
		if err != nil {
			return nil, MDServerError{err}
		}
		rmdses = append(rmdses, &rmds)
	}

	return rmdses, nil
}

// Put implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) Put(ctx context.Context, rmds *RootMetadataSigned) error {
	if md.isShutdown() {
		return errors.New("MD server already shut down")
	}

	mStatus := rmds.MD.MergedStatus()
	bid := rmds.MD.BID

	if mStatus == Merged {
		if bid != NullBranchID {
			return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
		}
	} else {
		if bid == NullBranchID {
			return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
		}
	}

	id := rmds.MD.ID

	mergedMasterHead, err :=
		md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return MDServerError{err}
	}

	// Check permissions
	ok, err := isWriterOrValidRekey(
		ctx, md.config.Codec(), md.config.KBPKI(),
		mergedMasterHead, rmds)
	if err != nil {
		return MDServerError{err}
	}
	if !ok {
		return MDServerErrorUnauthorized{}
	}

	head, err := md.getHeadForTLF(ctx, id, bid, mStatus)
	if err != nil {
		return MDServerError{err}
	}

	var recordBranchID bool

	if mStatus == Unmerged && head == nil {
		// currHead for unmerged history might be on the main branch
		prevRev := rmds.MD.Revision - 1
		rmdses, err := md.GetRange(ctx, id, NullBranchID, Merged, prevRev, prevRev)
		if err != nil {
			return MDServerError{err}
		}
		if len(rmdses) != 1 {
			return MDServerError{
				Err: fmt.Errorf("Expected 1 MD block got %d", len(rmdses)),
			}
		}
		head = rmdses[0]
		recordBranchID = true
	}

	// Consistency checks
	if head != nil {
		err := head.MD.CheckValidSuccessorForServer(md.config, &rmds.MD)
		if err != nil {
			return err
		}
	}

	// Record branch ID
	if recordBranchID {
		branchKey, err := md.getBranchKey(ctx, id)
		if err != nil {
			return MDServerError{err}
		}
		func() {
			md.branchMutex.Lock()
			defer md.branchMutex.Unlock()
			md.branchDb[branchKey] = bid
		}()
	}

	encodedMd, err := md.config.Codec().Encode(rmds)
	if err != nil {
		return MDServerError{err}
	}

	block := mdBlockMem{encodedMd, md.config.Clock().Now()}

	// Add an entry with the revision key.
	revKey, err := md.getMDKey(id, bid, mStatus)
	if err != nil {
		return MDServerError{err}
	}

	md.mdMutex.Lock()
	defer md.mdMutex.Unlock()
	md.mdDb[revKey] = append(md.mdDb[revKey], block)

	if mStatus == Merged &&
		// Don't send notifies if it's just a rekey (the real mdserver
		// sends a "folder needs rekey" notification in this case).
		!(rmds.MD.IsRekeySet() && rmds.MD.IsWriterMetadataCopiedSet()) {
		md.updateManager.setHead(id, md)
	}

	return nil
}

// PruneBranch implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) PruneBranch(ctx context.Context, id TlfID, bid BranchID) error {
	if md.isShutdown() {
		return errors.New("MD server already shut down")
	}
	if bid == NullBranchID {
		return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	currBID, err := md.getBranchID(ctx, id)
	if err != nil {
		return err
	}
	if currBID == NullBranchID || bid != currBID {
		return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Don't actually delete unmerged history. This is intentional to be consistent
	// with the mdserver behavior-- it garbage collects discarded branches in the
	// background.
	branchKey, err := md.getBranchKey(ctx, id)
	if err != nil {
		return MDServerError{err}
	}
	md.branchMutex.Lock()
	defer md.branchMutex.Unlock()
	delete(md.branchDb, branchKey)
	return nil
}

func (md *MDServerMemory) getBranchID(ctx context.Context, id TlfID) (BranchID, error) {
	branchKey, err := md.getBranchKey(ctx, id)
	if err != nil {
		return NullBranchID, MDServerError{err}
	}
	md.branchMutex.Lock()
	defer md.branchMutex.Unlock()
	bid, ok := md.branchDb[branchKey]
	if !ok {
		return NullBranchID, nil
	}
	return bid, nil
}

// RegisterForUpdate implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) RegisterForUpdate(ctx context.Context, id TlfID,
	currHead MetadataRevision) (<-chan error, error) {
	if md.isShutdown() {
		return nil, errors.New("MD server already shut down")
	}

	// are we already past this revision?  If so, fire observer
	// immediately
	currMergedHeadRev, err := md.getCurrentMergedHeadRevision(ctx, id)
	if err != nil {
		return nil, err
	}

	c := md.updateManager.registerForUpdate(id, currHead, currMergedHeadRev, md)
	return c, nil
}

func (md *MDServerMemory) getCurrentDeviceKIDBytes(ctx context.Context) (
	[]byte, error) {
	buf := &bytes.Buffer{}
	deviceKID, err := md.getCurrentDeviceKID(ctx)
	if err != nil {
		return []byte{}, err
	}
	_, err = buf.Write(deviceKID.ToBytes())
	if err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
}

// TruncateLock implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) TruncateLock(ctx context.Context, id TlfID) (
	bool, error) {
	md.locksMutex.Lock()
	defer md.locksMutex.Unlock()

	myKID, err := md.getCurrentDeviceKID(ctx)
	if err != nil {
		return false, err
	}

	lockKID, ok := md.locksDb[id]
	if !ok {
		md.locksDb[id] = myKID
		return true, nil
	}

	if lockKID == myKID {
		// idempotent
		return true, nil
	}

	// Locked by someone else.
	return false, MDServerErrorLocked{}
}

// TruncateUnlock implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) TruncateUnlock(ctx context.Context, id TlfID) (
	bool, error) {
	md.locksMutex.Lock()
	defer md.locksMutex.Unlock()

	myKID, err := md.getCurrentDeviceKID(ctx)
	if err != nil {
		return false, err
	}

	lockKID, ok := md.locksDb[id]
	if !ok {
		// Already unlocked.
		return true, nil
	}

	if lockKID == myKID {
		delete(md.locksDb, id)
		return true, nil
	}

	// Locked by someone else.
	return false, MDServerErrorLocked{}
}

// Shutdown implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) Shutdown() {
	md.shutdownLock.Lock()
	defer md.shutdownLock.Unlock()
	if *md.shutdown {
		return
	}
	*md.shutdown = true
}

// IsConnected implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) IsConnected() bool {
	return !md.isShutdown()
}

// RefreshAuthToken implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) RefreshAuthToken(ctx context.Context) {}

// This should only be used for testing with an in-memory server.
func (md *MDServerMemory) copy(config Config) mdServerLocal {
	// NOTE: observers and sessionHeads are copied shallowly on
	// purpose, so that the MD server that gets a Put will notify all
	// observers correctly no matter where they got on the list.
	log := config.MakeLogger("")
	return &MDServerMemory{config, log, md.handleMutex, md.handleDb,
		md.latestHandleDb, md.mdMutex, md.mdDb, md.branchMutex,
		md.branchDb, md.locksMutex, md.locksDb, md.updateManager,
		md.shutdownLock, md.shutdown}
}

// isShutdown returns whether the logical, shared MDServer instance
// has been shut down.
func (md *MDServerMemory) isShutdown() bool {
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	return *md.shutdown
}

// DisableRekeyUpdatesForTesting implements the MDServer interface.
func (md *MDServerMemory) DisableRekeyUpdatesForTesting() {
	// Nothing to do.
}

// CheckForRekeys implements the MDServer interface.
func (md *MDServerMemory) CheckForRekeys(ctx context.Context) <-chan error {
	// Nothing to do
	c := make(chan error, 1)
	c <- nil
	return c
}

func (md *MDServerMemory) addNewAssertionForTest(uid keybase1.UID,
	newAssertion keybase1.SocialAssertion) error {
	if md.isShutdown() {
		return errors.New("MD server already shut down")
	}

	md.handleMutex.Lock()
	defer md.handleMutex.Unlock()
	// Iterate through all the handles, and add handles for ones
	// containing newAssertion to now include the uid.
	for hBytes, id := range md.handleDb {
		var h BareTlfHandle
		err := md.config.Codec().Decode([]byte(hBytes), &h)
		if err != nil {
			return err
		}
		assertions := map[keybase1.SocialAssertion]keybase1.UID{
			newAssertion: uid,
		}
		newH := h.ResolveAssertions(assertions)
		if reflect.DeepEqual(h, newH) {
			continue
		}
		newHBytes, err := md.config.Codec().Encode(newH)
		if err != nil {
			return err
		}
		md.handleDb[mdHandleKey(newHBytes)] = id
	}
	return nil
}

func (md *MDServerMemory) getCurrentMergedHeadRevision(
	ctx context.Context, id TlfID) (rev MetadataRevision, err error) {
	head, err := md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return 0, err
	}
	if head != nil {
		rev = head.MD.Revision
	}
	return
}

// GetLatestHandleForTLF implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetLatestHandleForTLF(_ context.Context, id TlfID) (
	BareTlfHandle, error) {
	md.handleMutex.RLock()
	defer md.handleMutex.RUnlock()
	return md.latestHandleDb[id], nil
}
