// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
	"golang.org/x/net/context"
)

type mdBlockLocal struct {
	MD        *RootMetadataSigned
	Timestamp time.Time
}

type mdBranchKey struct {
	tlfID     TlfID
	deviceKID keybase1.KID
}

// MDServerMemory just stores blocks in local leveldb instances.
type MDServerMemory struct {
	config   Config
	log      logger.Logger
	handleDb *leveldb.DB // folder handle                  -> folderId
	mdDb     *leveldb.DB // folderId+[branchId]+[revision] -> mdBlockLocal

	branchMutex *sync.Mutex
	branchDb    map[mdBranchKey]BranchID // TlfID+deviceKID -> BranchID

	locksMutex *sync.Mutex
	locksDb    map[TlfID]keybase1.KID // TlfID -> deviceKID

	updateManager *mdServerLocalUpdateManager

	shutdown     *bool
	shutdownLock *sync.RWMutex
}

var _ mdServerLocal = (*MDServerMemory)(nil)

func newMDServerMemoryWithStorage(config Config, handleStorage, mdStorage,
	branchStorage, lockStorage storage.Storage) (*MDServerMemory, error) {
	handleDb, err := leveldb.Open(handleStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	mdDb, err := leveldb.Open(mdStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	branchDb := make(map[mdBranchKey]BranchID)
	locksDb := make(map[TlfID]keybase1.KID)
	log := config.MakeLogger("")
	mdserv := &MDServerMemory{config, log, handleDb, mdDb,
		&sync.Mutex{}, branchDb, &sync.Mutex{}, locksDb,
		newMDServerLocalUpdateManager(), new(bool), &sync.RWMutex{}}
	return mdserv, nil
}

// NewMDServerMemory constructs a new MDServerMemory object that stores
// all data in-memory.
func NewMDServerMemory(config Config) (*MDServerMemory, error) {
	return newMDServerMemoryWithStorage(config,
		storage.NewMemStorage(), storage.NewMemStorage(),
		storage.NewMemStorage(), storage.NewMemStorage())
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func (md *MDServerMemory) checkPerms(ctx context.Context, id TlfID,
	checkWrite bool, newMd *RootMetadataSigned) (bool, error) {
	rmds, err := md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if rmds == nil {
		// TODO: the real mdserver will actually reverse lookup the folder handle
		// and check that the UID is listed.
		return true, nil
	}
	_, user, err := md.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return false, err
	}
	h, err := rmds.MD.MakeBareTlfHandle()
	if err != nil {
		return false, err
	}
	isWriter := h.IsWriter(user)
	isReader := h.IsReader(user)
	if checkWrite {
		// if this is a reader, are they acting within their restrictions?
		if !isWriter && isReader && newMd != nil {
			return newMd.MD.IsValidRekeyRequest(md.config, &rmds.MD, user)
		}
		return isWriter, nil
	}
	return isWriter || isReader, nil
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func (md *MDServerMemory) isReader(ctx context.Context, id TlfID) (bool, error) {
	return md.checkPerms(ctx, id, false, nil)
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func (md *MDServerMemory) isWriter(ctx context.Context, id TlfID) (bool, error) {
	return md.checkPerms(ctx, id, true, nil)
}

// Helper to aid in enforcement that only specified public keys can access TLF metdata.
func (md *MDServerMemory) isWriterOrValidRekey(ctx context.Context, id TlfID, newMd *RootMetadataSigned) (
	bool, error) {
	return md.checkPerms(ctx, id, true, newMd)
}

// GetForHandle implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetForHandle(ctx context.Context, handle BareTlfHandle,
	mStatus MergeStatus) (TlfID, *RootMetadataSigned, error) {
	id := NullTlfID
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return id, nil, errors.New("MD server already shut down")
	}

	handleBytes, err := md.config.Codec().Encode(handle)
	if err != nil {
		return id, nil, err
	}

	buf, err := md.handleDb.Get(handleBytes, nil)
	if err != nil && err != leveldb.ErrNotFound {
		return id, nil, MDServerError{err}
	}
	if err == nil {
		var id TlfID
		err := id.UnmarshalBinary(buf)
		if err != nil {
			return NullTlfID, nil, err
		}
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

	err = md.handleDb.Put(handleBytes, id.Bytes(), nil)
	if err != nil {
		return id, nil, MDServerError{err}
	}
	return id, nil, nil
}

// GetForTLF implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) GetForTLF(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus) (*RootMetadataSigned, error) {
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Check permissions
	ok, err := md.isReader(ctx, id)
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

func (md *MDServerMemory) rmdsFromBlockBytes(buf []byte) (
	*RootMetadataSigned, error) {
	block := new(mdBlockLocal)
	err := md.config.Codec().Decode(buf, block)
	if err != nil {
		return nil, err
	}
	block.MD.untrustedServerTimestamp = block.Timestamp
	return block.MD, nil
}

func (md *MDServerMemory) getHeadForTLF(ctx context.Context, id TlfID,
	bid BranchID, mStatus MergeStatus) (rmds *RootMetadataSigned, err error) {
	key, err := md.getMDKey(id, 0, bid, mStatus)
	if err != nil {
		return
	}
	buf, err := md.mdDb.Get(key[:], nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			rmds, err = nil, nil
			return
		}
		return
	}
	return md.rmdsFromBlockBytes(buf)
}

func (md *MDServerMemory) getMDKey(id TlfID, revision MetadataRevision,
	bid BranchID, mStatus MergeStatus) ([]byte, error) {
	// short-cut
	if revision == MetadataRevisionUninitialized && mStatus == Merged {
		return id.Bytes(), nil
	}
	buf := &bytes.Buffer{}

	// add folder id
	_, err := buf.Write(id.Bytes())
	if err != nil {
		return []byte{}, err
	}

	// this order is signifcant for range fetches.
	// we want increments in revision number to only affect
	// the least significant bits of the key.
	if mStatus == Unmerged {
		// add branch ID
		_, err = buf.Write(bid.Bytes())
		if err != nil {
			return []byte{}, err
		}
	}

	if revision >= MetadataRevisionInitial {
		// add revision
		err = binary.Write(buf, binary.BigEndian, revision.Number())
		if err != nil {
			return []byte{}, err
		}
	}
	return buf.Bytes(), nil
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
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Check permissions
	ok, err := md.isReader(ctx, id)
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

	var rmdses []*RootMetadataSigned
	startKey, err := md.getMDKey(id, start, bid, mStatus)
	if err != nil {
		return rmdses, MDServerError{err}
	}
	stopKey, err := md.getMDKey(id, stop+1, bid, mStatus)
	if err != nil {
		return rmdses, MDServerError{err}
	}

	iter := md.mdDb.NewIterator(&util.Range{Start: startKey, Limit: stopKey}, nil)
	defer iter.Release()
	for iter.Next() {
		buf := iter.Value()
		rmds, err := md.rmdsFromBlockBytes(buf)
		if err != nil {
			return rmdses, MDServerError{err}
		}
		rmdses = append(rmdses, rmds)
	}
	if err := iter.Error(); err != nil {
		return rmdses, MDServerError{err}
	}

	return rmdses, nil
}

// Put implements the MDServer interface for MDServerMemory.
func (md *MDServerMemory) Put(ctx context.Context, rmds *RootMetadataSigned) error {
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
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

	// Check permissions
	ok, err := md.isWriterOrValidRekey(ctx, id, rmds)
	if err != nil {
		return MDServerError{err}
	}
	if !ok {
		return MDServerErrorUnauthorized{}
	}

	head, err := md.getHeadForTLF(ctx, id, rmds.MD.BID, mStatus)
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
		if head.MD.Revision+1 != rmds.MD.Revision {
			return MDServerErrorConflictRevision{
				Expected: head.MD.Revision + 1,
				Actual:   rmds.MD.Revision,
			}
		}
		expectedHash, err := head.MD.MetadataID(md.config)
		if err != nil {
			return MDServerError{Err: err}
		}
		if rmds.MD.PrevRoot != expectedHash {
			return MDServerErrorConflictPrevRoot{
				Expected: expectedHash,
				Actual:   rmds.MD.PrevRoot,
			}
		}
		expectedUsage := head.MD.DiskUsage
		if !rmds.MD.IsWriterMetadataCopiedSet() {
			expectedUsage += rmds.MD.RefBytes - rmds.MD.UnrefBytes
		}
		if rmds.MD.DiskUsage != expectedUsage {
			return MDServerErrorConflictDiskUsage{
				Expected: expectedUsage,
				Actual:   rmds.MD.DiskUsage,
			}
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

	block := &mdBlockLocal{rmds, md.config.Clock().Now()}
	buf, err := md.config.Codec().Encode(block)
	if err != nil {
		return MDServerError{err}
	}

	// Wrap writes in a batch
	batch := new(leveldb.Batch)

	// Add an entry with the revision key.
	revKey, err := md.getMDKey(id, rmds.MD.Revision, rmds.MD.BID, mStatus)
	if err != nil {
		return MDServerError{err}
	}
	batch.Put(revKey, buf)

	// Add an entry with the head key.
	headKey, err := md.getMDKey(id, MetadataRevisionUninitialized,
		rmds.MD.BID, mStatus)
	if err != nil {
		return MDServerError{err}
	}
	batch.Put(headKey, buf)

	// Write the batch.
	err = md.mdDb.Write(batch, nil)
	if err != nil {
		return MDServerError{err}
	}

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
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
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
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return nil, errors.New("MD server already shut down")
	}

	// are we already past this revision?  If so, fire observer
	// immediately
	head, err := md.getHeadForTLF(ctx, id, NullBranchID, Merged)
	if err != nil {
		return nil, err
	}
	var currMergedHeadRev MetadataRevision
	if head != nil {
		currMergedHeadRev = head.MD.Revision
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

	if md.handleDb != nil {
		md.handleDb.Close()
	}
	if md.mdDb != nil {
		md.mdDb.Close()
	}
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
	return &MDServerMemory{config, log, md.handleDb, md.mdDb,
		md.branchMutex, md.branchDb, md.locksMutex, md.locksDb,
		md.updateManager, md.shutdown, md.shutdownLock}
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
	md.shutdownLock.RLock()
	defer md.shutdownLock.RUnlock()
	if *md.shutdown {
		return errors.New("MD server already shut down")
	}

	// Iterate through all the handles, and add handles for ones
	// containing newAssertion to now include the uid.
	iter := md.handleDb.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		handleBytes := iter.Key()
		var handle BareTlfHandle
		err := md.config.Codec().Decode(handleBytes, &handle)
		if err != nil {
			return err
		}
		assertions := map[keybase1.SocialAssertion]keybase1.UID{
			newAssertion: uid,
		}
		newHandle := handle.ResolveAssertions(assertions)
		if reflect.DeepEqual(handle, newHandle) {
			continue
		}
		newHandleBytes, err := md.config.Codec().Encode(newHandle)
		if err != nil {
			return err
		}
		id := iter.Value()
		if err := md.handleDb.Put(newHandleBytes, id, nil); err != nil {
			return err
		}
	}
	return iter.Error()
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
	var handle BareTlfHandle
	iter := md.handleDb.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		var dbID TlfID
		idBytes := iter.Value()
		err := dbID.UnmarshalBinary(idBytes)
		if err != nil {
			return BareTlfHandle{}, err
		}
		if id != dbID {
			continue
		}
		handleBytes := iter.Key()
		handle = BareTlfHandle{}
		err = md.config.Codec().Decode(handleBytes, &handle)
		if err != nil {
			return BareTlfHandle{}, err
		}
	}
	return handle, nil
}
