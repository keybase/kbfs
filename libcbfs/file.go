// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"github.com/keybase/kbfs/libcbfs/cbfs"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

// File represents KBFS files.
type File struct {
	FSO
}

func newFile(folder *Folder, node libkbfs.Node, name string, parent libkbfs.Node) *File {
	f := &File{FSO{
		name:   name,
		parent: parent,
		folder: folder,
		node:   node,
	}}
	f.refcount.Increase()
	return f
}

// GetFileInformation for cbfs.
func (f *File) GetFileInformation(ctx context.Context) (a *cbfs.Stat, err error) {
	f.folder.fs.logEnter(ctx, "File GetFileInformation")
	defer func() { f.folder.reportErr(ctx, libkbfs.ReadMode, err) }()

	a, err = eiToStat(f.folder.fs.config.KBFSOps().Stat(ctx, f.node))
	if a != nil {
		f.folder.fs.log.CDebugf(ctx, "File GetFileInformation node=%v => %v", f.node, *a)
	} else {
		f.folder.fs.log.CDebugf(ctx, "File GetFileInformation node=%v => Error %T %v", f.node, err, err)
	}
	return a, err
}

// CanDelete - return just nil
// TODO check for permissions here.
func (f *File) CanDelete(ctx context.Context) error {
	f.folder.fs.logEnterf(ctx, "File CanDelete for %q", f.name)
	return nil
}

func (f *File) Delete(ctx context.Context) error {
	// renameAndDeletionLock should be the first lock to be grabbed in libcbfs.
	f.folder.fs.renameAndDeletionLock.Lock()
	defer f.folder.fs.renameAndDeletionLock.Unlock()
	f.folder.fs.log.CDebugf(ctx, "Removing (Delete) file in cleanup %s", f.name)

	return f.folder.fs.config.KBFSOps().RemoveEntry(ctx, f.parent, f.name)
}

// Cleanup - for CBFS, remember to handle deletions.
// If Cleanup is called with non-nil FileInfo that has IsDeleteOnClose()
// no libcbfs locks should be held prior to the call.
func (f *File) Cleanup(ctx context.Context) {
	var err error
	f.folder.fs.logEnter(ctx, "File Cleanup")
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()

	f.folder.fs.log.CDebugf(ctx, "Cleanup %v", *f)

	if f.refcount.Decrease() {
		f.folder.fs.log.CDebugf(ctx, "Forgetting file node")
		f.folder.forgetNode(ctx, f.node)
		// TODO this should not be needed in future.
		f.folder.fs.config.KBFSOps().Sync(ctx, f.node)
	}
}

// FlushFileBuffers performs a (f)sync.
func (f *File) FlushFileBuffers(ctx context.Context) (err error) {
	f.folder.fs.logEnter(ctx, "File FlushFileBuffers")
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()

	return f.folder.fs.config.KBFSOps().Sync(ctx, f.node)
}

// ReadFile for CBFS reads.
func (f *File) ReadFile(ctx context.Context, bs []byte, offset int64) (n int, err error) {
	f.folder.fs.logEnter(ctx, "ReadFile")
	defer func() { f.folder.reportErr(ctx, libkbfs.ReadMode, err) }()

	var nlarge int64
	nlarge, err = f.folder.fs.config.KBFSOps().Read(ctx, f.node, bs, offset)

	// This is safe since length of slices always fits into an int
	return int(nlarge), err
}

// WriteFile for CBFS writes.
func (f *File) WriteFile(ctx context.Context, bs []byte, offset int64) (n int, err error) {
	f.folder.fs.logEnter(ctx, "WriteFile")
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()

	if offset == -1 {
		ei, err := f.folder.fs.config.KBFSOps().Stat(ctx, f.node)
		if err != nil {
			return 0, err
		}
		offset = int64(ei.Size)
	}

	err = f.folder.fs.config.KBFSOps().Write(ctx, f.node, bs, offset)
	return len(bs), err
}

// SetEndOfFile for CBFS (f)truncates.
func (f *File) SetEndOfFile(ctx context.Context, length int64) (err error) {
	f.folder.fs.logEnter(ctx, "File SetEndOfFile")
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()

	return f.folder.fs.config.KBFSOps().Truncate(ctx, f.node, uint64(length))
}

// SetAllocationSize for CBFS (f)truncates but does not grow
// file size (it may fallocate, but that is not done at the
// moment).
func (f *File) SetAllocationSize(ctx context.Context, newSize int64) (err error) {
	f.folder.fs.logEnter(ctx, "File SetAllocationSize")
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()

	ei, err := f.folder.fs.config.KBFSOps().Stat(ctx, f.node)
	if err != nil {
		return err
	}

	// Refuse to grow the file.
	if int64(ei.Size) <= newSize {
		return nil
	}

	return f.folder.fs.config.KBFSOps().Truncate(ctx, f.node, uint64(newSize))
}

// SetFileAttributes for CBFS.
func (f *File) SetFileAttributes(ctx context.Context, fileAttributes *cbfs.Stat) error {
	f.folder.fs.logEnter(ctx, "File SetFileAttributes")
	// TODO handle attributes for real.
	return nil
}
