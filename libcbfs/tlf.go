// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"strings"
	"sync"
	"time"

	"github.com/keybase/kbfs/libcbfs/cbfs"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

// TLF represents the root directory of a TLF. It wraps a lazy-loaded
// Dir.
type TLF struct {
	refcount refcount

	folder *Folder

	dirLock sync.RWMutex
	dir     *Dir

	emptyFile
}

func newTLF(fl *FolderList, h *libkbfs.TlfHandle,
	name libkbfs.PreferredTlfName) *TLF {
	folder := newFolder(fl, h, name)
	tlf := &TLF{
		folder: folder,
	}
	tlf.refcount.Increase()
	return tlf
}

func (tlf *TLF) isPublic() bool {
	return tlf.folder.list.public
}

func (tlf *TLF) getStoredDir() *Dir {
	tlf.dirLock.RLock()
	defer tlf.dirLock.RUnlock()
	return tlf.dir
}

func (tlf *TLF) loadDirHelper(ctx context.Context, info string,
	mode libkbfs.ErrorModeType, filterErr bool) (
	dir *Dir, exitEarly bool, err error) {
	dir = tlf.getStoredDir()
	if dir != nil {
		return dir, false, nil
	}

	tlf.dirLock.Lock()
	defer tlf.dirLock.Unlock()
	// Need to check for nilness again to avoid racing with other
	// calls to loadDir().
	if tlf.dir != nil {
		return tlf.dir, false, nil
	}

	name := tlf.folder.name()

	tlf.folder.fs.log.CDebugf(ctx, "Loading root directory for folder %s "+
		"(public: %t, filter error: %t) for %s",
		name, tlf.isPublic(), filterErr, info)
	defer func() {
		if filterErr {
			exitEarly, err = libfs.FilterTLFEarlyExitError(ctx, err, tlf.folder.fs.log, name)
		}

		tlf.folder.reportErr(ctx, mode, err)
	}()

	handle, err := tlf.folder.resolve(ctx)
	if err != nil {
		return nil, false, err
	}

	var rootNode libkbfs.Node
	if filterErr {
		rootNode, _, err = tlf.folder.fs.config.KBFSOps().GetRootNode(
			ctx, handle, libkbfs.MasterBranch)
		if err != nil {
			return nil, false, err
		}
		// If not fake an empty directory.
		if rootNode == nil {
			return nil, false, libfs.TlfDoesNotExist{}
		}
	} else {
		rootNode, _, err = tlf.folder.fs.config.KBFSOps().GetOrCreateRootNode(
			ctx, handle, libkbfs.MasterBranch)
		if err != nil {
			return nil, false, err
		}
	}

	err = tlf.folder.setFolderBranch(rootNode.GetFolderBranch())
	if err != nil {
		return nil, false, err
	}

	tlf.folder.nodes[rootNode.GetID()] = tlf
	tlf.dir = newDir(tlf.folder, rootNode, string(name), nil)
	// TLFs should be cached.
	tlf.dir.refcount.Increase()
	tlf.folder.lockedAddNode(rootNode, tlf.dir)

	return tlf.dir, false, nil
}

func (tlf *TLF) loadDir(ctx context.Context, info string) (*Dir, error) {
	dir, _, err := tlf.loadDirHelper(ctx, info, libkbfs.WriteMode, false)
	return dir, err
}

// loadDirAllowNonexistent loads a TLF if it's not already loaded.  If
// the TLF doesn't yet exist, it still returns a nil error and
// indicates that the calling function should pretend it's an empty
// folder.
func (tlf *TLF) loadDirAllowNonexistent(ctx context.Context, info string) (
	*Dir, bool, error) {
	return tlf.loadDirHelper(ctx, info, libkbfs.ReadMode, true)
}

// SetFileTime sets mtime for FSOs (File and Dir).
func (tlf *TLF) SetFileTime(ctx context.Context, creation time.Time, lastAccess time.Time, lastWrite time.Time) (err error) {
	tlf.folder.fs.logEnter(ctx, "TLF SetFileTime")

	dir, err := tlf.loadDir(ctx, "TLF SetFileTime")
	if err != nil {
		return err
	}
	return dir.SetFileTime(ctx, creation, lastAccess, lastWrite)
}

// SetFileAttributes for CBFS.
func (tlf *TLF) SetFileAttributes(ctx context.Context, fileAttributes *cbfs.Stat) error {
	tlf.folder.fs.logEnter(ctx, "TLF SetFileAttributes")
	dir, err := tlf.loadDir(ctx, "TLF SetFileAttributes")
	if err != nil {
		return err
	}
	return dir.SetFileAttributes(ctx, fileAttributes)
}

// GetFileInformation for cbfs.
func (tlf *TLF) GetFileInformation(ctx context.Context) (st *cbfs.Stat, err error) {
	dir := tlf.getStoredDir()
	if dir == nil {
		return defaultDirectoryInformation()
	}

	return dir.GetFileInformation(ctx)
}

// open tries to open a file.
func (tlf *TLF) open(ctx context.Context, oc *openContext, path []string) (cbfs.File, bool, error) {
	if len(path) == 0 {
		//		if err := oc.ReturningDirAllowed(); err != nil {
		//			return nil, true, err
		//		}
		tlf.refcount.Increase()
		return tlf, true, nil
	}

	mode := libkbfs.ReadMode
	if oc.isCreation() {
		mode = libkbfs.WriteMode
	}
	// If it is a creation then we need the dir for real.
	dir, exitEarly, err :=
		tlf.loadDirHelper(ctx, "open", mode, !oc.isCreation())
	if err != nil {
		return nil, false, err
	}
	if exitEarly {
		specialNode := handleTLFSpecialFile(lastStr(path), tlf.folder)
		if specialNode != nil {
			return specialNode, false, nil
		}

		return nil, false, cbfs.ErrFileNotFound
	}
	return dir.open(ctx, oc, path)
}

// FindFiles does readdir for cbfs.
func (tlf *TLF) FindFiles(ctx context.Context, pattern string, callback func(*cbfs.NamedStat) error) (err error) {
	tlf.folder.fs.logEnter(ctx, "TLF FindFiles")

	dir, exitEarly, err := tlf.loadDirAllowNonexistent(ctx, "FindFiles")
	if err != nil {
		return errToCBFS(err)
	}
	if exitEarly {
		if isExactMask(pattern) && handleTLFSpecialFile(pattern, tlf.folder) != nil {
			ns := cbfs.NamedStat{Name: pattern}
			st, _ := defaultFileInformation()
			ns.Stat = *st
			return callback(&ns)
		}
		return cbfs.ErrFileNotFound
	}
	return dir.FindFiles(ctx, pattern, callback)
}
func isExactMask(s string) bool {
	return s != "" && !strings.ContainsAny(s, `?*`)
}

// CanDelete - return just nil because tlfs
// can always be removed from favorites.
func (tlf *TLF) CanDelete(ctx context.Context) (err error) {
	return nil
}

func (tlf *TLF) Delete(ctx context.Context) (err error) {
	tlf.folder.handleMu.Lock()
	fav := tlf.folder.h.ToFavorite()
	tlf.folder.handleMu.Unlock()
	tlf.folder.fs.log.CDebugf(ctx, "TLF Removing favorite %q", fav.Name)
	defer func() {
		tlf.folder.reportErr(ctx, libkbfs.WriteMode, err)
	}()
	return tlf.folder.fs.config.KBFSOps().DeleteFavorite(ctx, fav)
}

// Cleanup - forget references, perform deletions etc.
func (tlf *TLF) Cleanup(ctx context.Context) {
	if tlf.refcount.Decrease() {
		dir := tlf.getStoredDir()
		if dir == nil {
			return
		}
		dir.Cleanup(ctx)
	}
}

func (tlf *TLF) IsDirectoryEmpty(ctx context.Context) (bool, error) {
	dir, exitEarly, err := tlf.loadDirAllowNonexistent(ctx, "IsDirectoryEmpty")
	if err != nil {
		return false, errToCBFS(err)
	}
	if exitEarly {
		return true, nil
	}
	return dir.IsDirectoryEmpty(ctx)
}
