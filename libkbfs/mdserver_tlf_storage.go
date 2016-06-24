// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"sync"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

type mdServerTlfStorage struct {
	codec Codec
	dir   string

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
	codec Codec, dir string) (*mdServerTlfStorage, error) {
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
		dir:      dir,
		mdDb:     mdDb,
		branchDb: branchDb,
	}, nil
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
