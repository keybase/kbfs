// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"runtime/pprof"

	"github.com/keybase/kbfs/libcbfs/cbfs"
	"github.com/keybase/kbfs/libfs"
	"golang.org/x/net/context"
)

// TODO: Also have a file for CPU profiles.

// ProfileList is a node that can list all of the available profiles.
type ProfileList struct {
	fs *FS
	emptyFile
}

// GetFileInformation for cbfs.
func (ProfileList) GetFileInformation(ctx context.Context) (st *cbfs.Stat, err error) {
	return defaultDirectoryInformation()
}

// open tries to open a file.
func (pl ProfileList) open(ctx context.Context, oc *openContext, path []string) (cbfs.File, bool, error) {
	if len(path) == 0 {
		return oc.returnDirNoCleanup(ProfileList{})
	}
	if len(path) > 1 || !libfs.IsSupportedProfileName(path[0]) {
		return nil, false, cbfs.ErrFileNotFound
	}
	f := libfs.ProfileGet(path[0])
	if f == nil {
		return nil, false, cbfs.ErrFileNotFound
	}
	return oc.returnFileNoCleanup(&SpecialReadFile{read: f, fs: pl.fs})
}

// FindFiles does readdir for cbfs.
func (ProfileList) FindFiles(ctx context.Context, ignored string, callback func(*cbfs.NamedStat) error) (err error) {
	profiles := pprof.Profiles()
	var ns cbfs.NamedStat
	ns.FileAttributes = cbfs.FileAttributeReadonly
	for _, p := range profiles {
		ns.Name = p.Name()
		if !libfs.IsSupportedProfileName(ns.Name) {
			continue
		}
		err := callback(&ns)
		if err != nil {
			return err
		}
	}
	return nil
}
