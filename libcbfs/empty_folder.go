// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"github.com/keybase/kbfs/libcbfs/cbfs"
	"golang.org/x/net/context"
)

// EmptyFolder represents an empty, read-only KBFS TLF that has not
// been created by someone with sufficient permissions.
type EmptyFolder struct {
	emptyFile
}

func (ef *EmptyFolder) open(ctx context.Context, oc *openContext, path []string) (f cbfs.File, isDir bool, err error) {
	if len(path) != 0 {
		return nil, false, cbfs.ErrFileNotFound
	}
	return oc.returnDirNoCleanup(ef)
}

// GetFileInformation for cbfs.
func (*EmptyFolder) GetFileInformation(context.Context) (a *cbfs.Stat, err error) {
	return defaultDirectoryInformation()
}

func (*EmptyFolder) IsDirectoryEmpty(context.Context) (bool, error) {
	return true, nil
}

// FindFiles for cbfs.
func (*EmptyFolder) FindFiles(ctx context.Context, ignored string, callback func(*cbfs.NamedStat) error) (err error) {
	return nil
}
