// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"path/filepath"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

type mdServerTlfStorage struct {
	codec Codec
	dir   string

	// Protects any IO operations in dir or any of its children,
	// as well all the DBs and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock sync.RWMutex

	mdDb     *leveldb.DB // [branchId]+[revision] -> mdBlockLocal
	branchDb *leveldb.DB // deviceKID             -> branchId

	locksDb *leveldb.DB // folderId -> deviceKID

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

	// Always use memory for the lock storage, so it gets wiped after
	// a restart.
	lockStorage := storage.NewMemStorage()

	mdDb, err := leveldb.Open(mdStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	branchDb, err := leveldb.Open(branchStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	locksDb, err := leveldb.Open(lockStorage, leveldbOptions)
	if err != nil {
		return nil, err
	}
	return &mdServerTlfStorage{
		codec:    codec,
		dir:      dir,
		mdDb:     mdDb,
		branchDb: branchDb,
		locksDb:  locksDb,
	}, nil
}
