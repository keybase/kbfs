// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type mdServerTlfStorage struct {
	codec  Codec
	clock  Clock
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well all the DBs, truncateLockHolder, and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	mdDb     *leveldb.DB // [branchId]+[revision] -> mdBlockLocal
	branchDb *leveldb.DB // deviceKID             -> branchId

	// Store the truncate lock holder just in memory, so it gets
	// wiped after a restart.
	truncateLockHolder *keybase1.KID

	isShutdown bool
}

func makeMDServerTlfStorage(
	codec Codec, clock Clock, crypto cryptoPure, dir string) (
	*mdServerTlfStorage, error) {
	mdPath := filepath.Join(dir, "md")
	branchPath := filepath.Join(dir, "branches")

	mdStorage, err := storage.OpenFile(mdPath)
	if err != nil {
		return nil, err
	}

	branchStorage, err := storage.OpenFile(branchPath)
	if err != nil {
		return nil, err
	}

	mdDb, err := leveldb.Open(mdStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	branchDb, err := leveldb.Open(branchStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	return &mdServerTlfStorage{
		codec:    codec,
		clock:    clock,
		crypto:   crypto,
		dir:      dir,
		mdDb:     mdDb,
		branchDb: branchDb,
	}, nil
}

func (s *mdServerTlfStorage) mdsPath() string {
	return filepath.Join(s.dir, "mds")
}

func (s *mdServerTlfStorage) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(s.mdsPath(), idStr[:4], idStr[4:])
}

func (s *mdServerTlfStorage) getMDLocked(id MdID) (
	*RootMetadataSigned, time.Time, error) {
	if s.isShutdown {
		return nil, time.Time{}, errors.New("MD server already shut down")
	}

	// Read file.

	path := s.mdPath(id)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}

	var rmds RootMetadataSigned
	err = s.codec.Decode(data, &rmds)
	if err != nil {
		return nil, time.Time{}, err
	}

	// Check integrity.

	mdID, err := rmds.MD.MetadataID(s.crypto)
	if err != nil {
		return nil, time.Time{}, err
	}

	if id != mdID {
		return nil, time.Time{}, fmt.Errorf(
			"Metadata ID mismatch: expected %s, got %s", id, mdID)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}

	return &rmds, fileInfo.ModTime(), nil
}

func (s *mdServerTlfStorage) putMDLocked(rmds *RootMetadataSigned) error {
	if s.isShutdown {
		return errors.New("MD server already shut down")
	}

	id, err := rmds.MD.MetadataID(s.crypto)
	if err != nil {
		return err
	}

	_, _, err = s.getMDLocked(id)
	if os.IsNotExist(err) {
		// Continue on.
	} else if err != nil {
		return err
	} else {
		// Entry exists, so nothing else to do.
		return nil
	}

	path := s.mdPath(id)

	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return err
	}

	buf, err := s.codec.Encode(rmds)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path, buf, 0600)
	if err != nil {
		return err
	}

	return nil
}

func (md *mdServerTlfStorage) getBranchKey(ctx context.Context, kbpki KBPKI) ([]byte, error) {
	key, err := kbpki.GetCurrentCryptPublicKey(ctx)
	if err != nil {
		return nil, err
	}
	return key.kid.ToBytes(), nil
}

func (md *mdServerTlfStorage) getBranchID(ctx context.Context, kbpki KBPKI) (BranchID, error) {
	branchKey, err := md.getBranchKey(ctx, kbpki)
	if err != nil {
		return NullBranchID, MDServerError{err}
	}
	buf, err := md.branchDb.Get(branchKey, nil)
	if err == leveldb.ErrNotFound {
		return NullBranchID, nil
	}
	if err != nil {
		return NullBranchID, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}
	var bid BranchID
	err = md.codec.Decode(buf, &bid)
	if err != nil {
		return NullBranchID, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}
	return bid, nil
}

func (md *mdServerTlfStorage) getMDKey(revision MetadataRevision,
	bid BranchID, mStatus MergeStatus) ([]byte, error) {
	// short-cut
	if revision == MetadataRevisionUninitialized && mStatus == Merged {
		return nil, nil
	}
	buf := &bytes.Buffer{}

	// this order is signifcant for range fetches.
	// we want increments in revision number to only affect
	// the least significant bits of the key.
	if mStatus == Unmerged {
		// add branch ID
		_, err := buf.Write(bid.Bytes())
		if err != nil {
			return nil, err
		}
	}

	if revision >= MetadataRevisionInitial {
		// add revision
		err := binary.Write(buf, binary.BigEndian, revision.Number())
		if err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

type mdBlockLocal struct {
	MD        *RootMetadataSigned
	Timestamp time.Time
}

func (md *mdServerTlfStorage) rmdsFromBlockBytes(buf []byte) (
	*RootMetadataSigned, error) {
	block := new(mdBlockLocal)
	err := md.codec.Decode(buf, block)
	if err != nil {
		return nil, err
	}
	block.MD.untrustedServerTimestamp = block.Timestamp
	return block.MD, nil
}

func (md *mdServerTlfStorage) getHeadForTLF(ctx context.Context,
	bid BranchID, mStatus MergeStatus) (rmds *RootMetadataSigned, err error) {
	key, err := md.getMDKey(0, bid, mStatus)
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

func (md *mdServerTlfStorage) getForTLF(ctx context.Context,
	kbpki KBPKI, bid BranchID, mStatus MergeStatus) (
	*RootMetadataSigned, error) {
	md.lock.RLock()
	defer md.lock.RUnlock()

	if md.isShutdown {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	mergedMasterHead, err := md.getHeadForTLF(ctx, NullBranchID, Merged)
	if err != nil {
		return nil, MDServerError{err}
	}

	// Check permissions
	ok, err := isReader(ctx, md.codec, kbpki, mergedMasterHead)
	if err != nil {
		return nil, MDServerError{err}
	}
	if !ok {
		return nil, MDServerErrorUnauthorized{}
	}

	// Lookup the branch ID if not supplied
	if mStatus == Unmerged && bid == NullBranchID {
		bid, err = md.getBranchID(ctx, kbpki)
		if err != nil {
			return nil, err
		}
		if bid == NullBranchID {
			return nil, nil
		}
	}

	rmds, err := md.getHeadForTLF(ctx, bid, mStatus)
	if err != nil {
		return nil, MDServerError{err}
	}
	return rmds, nil
}

func (md *mdServerTlfStorage) getRangeLocked(ctx context.Context,
	kbpki KBPKI, bid BranchID, mStatus MergeStatus,
	start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	if md.isShutdown {
		return nil, errors.New("MD server already shut down")
	}

	if mStatus == Merged && bid != NullBranchID {
		return nil, MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	mergedMasterHead, err := md.getHeadForTLF(ctx, NullBranchID, Merged)
	if err != nil {
		return nil, MDServerError{err}
	}

	// Check permissions
	ok, err := isReader(ctx, md.codec, kbpki, mergedMasterHead)
	if err != nil {
		return nil, MDServerError{err}
	}
	if !ok {
		return nil, MDServerErrorUnauthorized{}
	}

	// Lookup the branch ID if not supplied
	if mStatus == Unmerged && bid == NullBranchID {
		bid, err = md.getBranchID(ctx, kbpki)
		if err != nil {
			return nil, err
		}
		if bid == NullBranchID {
			return nil, nil
		}
	}

	var rmdses []*RootMetadataSigned
	startKey, err := md.getMDKey(start, bid, mStatus)
	if err != nil {
		return rmdses, MDServerError{err}
	}
	stopKey, err := md.getMDKey(stop+1, bid, mStatus)
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

func (md *mdServerTlfStorage) getRange(ctx context.Context,
	kbpki KBPKI, bid BranchID, mStatus MergeStatus,
	start, stop MetadataRevision) (
	[]*RootMetadataSigned, error) {
	md.lock.RLock()
	defer md.lock.RUnlock()

	if md.isShutdown {
		return nil, errors.New("MD server already shut down")
	}

	return md.getRangeLocked(ctx, kbpki, bid, mStatus, start, stop)
}

func (md *mdServerTlfStorage) put(ctx context.Context, kbpki KBPKI, rmds *RootMetadataSigned) error {
	md.lock.Lock()
	defer md.lock.Unlock()

	if md.isShutdown {
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

	mergedMasterHead, err := md.getHeadForTLF(ctx, NullBranchID, Merged)
	if err != nil {
		return MDServerError{err}
	}

	// Check permissions
	ok, err := isWriterOrValidRekey(
		ctx, md.codec, kbpki, mergedMasterHead, rmds)
	if err != nil {
		return MDServerError{err}
	}
	if !ok {
		return MDServerErrorUnauthorized{}
	}

	head, err := md.getHeadForTLF(ctx, bid, mStatus)
	if err != nil {
		return MDServerError{err}
	}

	var recordBranchID bool

	if mStatus == Unmerged && head == nil {
		// currHead for unmerged history might be on the main branch
		prevRev := rmds.MD.Revision - 1
		rmdses, err := md.getRangeLocked(
			ctx, kbpki, NullBranchID, Merged, prevRev, prevRev)
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
		err := head.MD.CheckValidSuccessorForServer(
			md.crypto, &rmds.MD)
		if err != nil {
			return err
		}
	}

	// Record branch ID
	if recordBranchID {
		buf, err := md.codec.Encode(bid)
		if err != nil {
			return MDServerError{err}
		}
		branchKey, err := md.getBranchKey(ctx, kbpki)
		if err != nil {
			return MDServerError{err}
		}
		err = md.branchDb.Put(branchKey, buf, nil)
		if err != nil {
			return MDServerError{err}
		}
	}

	block := &mdBlockLocal{rmds, md.clock.Now()}
	buf, err := md.codec.Encode(block)
	if err != nil {
		return MDServerError{err}
	}

	// Wrap writes in a batch
	batch := new(leveldb.Batch)

	// Add an entry with the revision key.
	revKey, err := md.getMDKey(rmds.MD.Revision, bid, mStatus)
	if err != nil {
		return MDServerError{err}
	}
	batch.Put(revKey, buf)

	// Add an entry with the head key.
	headKey, err := md.getMDKey(MetadataRevisionUninitialized,
		bid, mStatus)
	if err != nil {
		return MDServerError{err}
	}
	batch.Put(headKey, buf)

	// Write the batch.
	err = md.mdDb.Write(batch, nil)
	if err != nil {
		return MDServerError{err}
	}

	return nil
}

// PruneBranch implements the MDServer interface for MDServerDisk.
func (md *mdServerTlfStorage) pruneBranch(
	ctx context.Context, kbpki KBPKI, bid BranchID) error {
	md.lock.Lock()
	defer md.lock.Unlock()

	if md.isShutdown {
		return errors.New("MD server already shut down")
	}

	if bid == NullBranchID {
		return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	currBID, err := md.getBranchID(ctx, kbpki)
	if err != nil {
		return err
	}
	if currBID == NullBranchID || bid != currBID {
		return MDServerErrorBadRequest{Reason: "Invalid branch ID"}
	}

	// Don't actually delete unmerged history. This is intentional to be consistent
	// with the mdserver behavior-- it garbage collects discarded branches in the
	// background.
	branchKey, err := md.getBranchKey(ctx, kbpki)
	if err != nil {
		return MDServerError{err}
	}
	err = md.branchDb.Delete(branchKey, nil)
	if err != nil {
		return MDServerError{err}
	}

	return nil
}

func (md *mdServerTlfStorage) truncateLock(ctx context.Context, kbpki KBPKI) (
	bool, error) {
	md.lock.Lock()
	defer md.lock.Unlock()

	if md.isShutdown {
		return false, errors.New("MD server already shut down")
	}

	key, err := kbpki.GetCurrentCryptPublicKey(ctx)
	if err != nil {
		return false, err
	}

	if md.truncateLockHolder != nil {
		if *md.truncateLockHolder == key.kid {
			// idempotent
			return true, nil
		}
		// Locked by someone else.
		return false, MDServerErrorLocked{}
	}

	md.truncateLockHolder = &key.kid
	return true, nil
}

func (md *mdServerTlfStorage) truncateUnlock(ctx context.Context, kbpki KBPKI) (
	bool, error) {
	md.lock.Lock()
	defer md.lock.Unlock()

	if md.isShutdown {
		return false, errors.New("MD server already shut down")
	}

	if md.truncateLockHolder == nil {
		// Already unlocked.
		return true, nil
	}

	key, err := kbpki.GetCurrentCryptPublicKey(ctx)
	if err != nil {
		return false, err
	}

	if *md.truncateLockHolder != key.kid {
		// Locked by someone else.
		return false, MDServerErrorLocked{}
	}

	md.truncateLockHolder = nil
	return true, nil
}

func (md *mdServerTlfStorage) shutdown() {
	md.lock.Lock()
	defer md.lock.Unlock()

	if md.isShutdown {
		return
	}
	md.isShutdown = true

	if md.mdDb != nil {
		md.mdDb.Close()
	}
	if md.branchDb != nil {
		md.branchDb.Close()
	}
}
