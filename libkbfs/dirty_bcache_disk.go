// Copyright 2018 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"
	"sync"

	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
)

type dirtyBlockCacheDiskConfig interface {
	codecGetter
	cryptoPureGetter
	keyGetterGetter
	blockOpsGetter
}

type dirtyBlockCacheDiskInfo struct {
	tmpPtr BlockPointer
	isDir  bool
}

func (dbcdi dirtyBlockCacheDiskInfo) newBlock() Block {
	if dbcdi.isDir {
		return NewDirBlock()
	}
	return NewFileBlock()
}

type DirtyBlockCacheDisk struct {
	config    dirtyBlockCacheDiskConfig
	diskCache *DiskBlockCacheLocal
	kmd       KeyMetadata
	branch    BranchName

	lock   sync.RWMutex
	blocks map[BlockPointer]dirtyBlockCacheDiskInfo
}

var _ DirtyBlockCacheSimple = (*DirtyBlockCacheDisk)(nil)

func newDirtyBlockCacheDisk(
	config dirtyBlockCacheDiskConfig,
	diskCache *DiskBlockCacheLocal, kmd KeyMetadata,
	branch BranchName) *DirtyBlockCacheDisk {
	return &DirtyBlockCacheDisk{
		config:    config,
		diskCache: diskCache,
		kmd:       kmd,
		branch:    branch,
		blocks:    make(map[BlockPointer]dirtyBlockCacheDiskInfo),
	}
}

func (d *DirtyBlockCacheDisk) getInfo(ptr BlockPointer) (
	dirtyBlockCacheDiskInfo, bool) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	info, ok := d.blocks[ptr]
	return info, ok
}

func (d *DirtyBlockCacheDisk) saveInfo(
	ptr BlockPointer, info dirtyBlockCacheDiskInfo) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.blocks[ptr] = info
}

// Get implements the DirtyBlockCache interface for
// DirtyBlockCacheDisk.
func (d *DirtyBlockCacheDisk) Get(
	ctx context.Context, tlfID tlf.ID, ptr BlockPointer, branch BranchName) (
	Block, error) {
	if branch != d.branch {
		return nil, errors.Errorf(
			"Branch %s doesn't match branch %s", branch, d.branch)
	}

	info, ok := d.getInfo(ptr)
	if !ok {
		return nil, NoSuchBlockError{ptr.ID}
	}

	// Look it up under the temp ID, which is an actual hash that can
	// be verified.
	data, serverHalf, _, err := d.diskCache.Get(ctx, tlfID, info.tmpPtr.ID)
	if err != nil {
		return nil, err
	}

	block := info.newBlock()
	err = assembleBlock(
		ctx, d.config.keyGetter(), d.config.Codec(),
		d.config.cryptoPure(), d.kmd, info.tmpPtr, block, data, serverHalf)
	if err != nil {
		return nil, err
	}
	return block, nil
}

// Put implements the DirtyBlockCache interface for
// DirtyBlockCacheDisk.
func (d *DirtyBlockCacheDisk) Put(
	ctx context.Context, tlfID tlf.ID, ptr BlockPointer, branch BranchName,
	block Block) error {
	if branch != d.branch {
		return errors.Errorf(
			"Branch %s doesn't match branch %s", branch, d.branch)
	}

	id, _, readyBlockData, err := d.config.BlockOps().Ready(ctx, d.kmd, block)
	if err != nil {
		return err
	}

	err = d.diskCache.Put(
		ctx, tlfID, id, readyBlockData.buf, readyBlockData.serverHalf)
	if err != nil {
		return err
	}

	_, isDir := block.(*DirBlock)
	info := dirtyBlockCacheDiskInfo{
		tmpPtr: BlockPointer{ID: id},
		isDir:  isDir,
	}
	d.saveInfo(ptr, info)
	return nil
}
