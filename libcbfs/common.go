// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"strings"
	"time"

	"github.com/keybase/kbfs/libcbfs/cbfs"
	"github.com/keybase/kbfs/libkbfs"
)

const (
	// PublicName is the name of the parent of all public top-level folders.
	PublicName = "public"

	// PrivateName is the name of the parent of all private top-level folders.
	PrivateName = "private"

	// CtxOpID is the display name for the unique operation ID tag.
	CtxOpID = "DID"
)

// CtxTagKey is the type used for unique context tags
type CtxTagKey int

const (
	// CtxIDKey is the type of the tag for unique operation IDs.
	CtxIDKey CtxTagKey = iota
)

// eiToStat converts from a libkbfs.EntryInfo and error to a *cbfs.Stat and error.
// Note that handling symlinks to directories requires extra processing not done here.
func eiToStat(ei libkbfs.EntryInfo, err error) (*cbfs.Stat, error) {
	if err != nil {
		return nil, errToCBFS(err)
	}
	st := &cbfs.Stat{}
	fillStat(st, &ei)
	return st, nil
}

// fillStat fill a cbfs.Stat from a libkbfs.DirEntry.
// Note that handling symlinks to directories requires extra processing not done here.
func fillStat(a *cbfs.Stat, de *libkbfs.EntryInfo) {
	a.FileSize = int64(de.Size)
	a.LastWrite = time.Unix(0, de.Mtime)
	a.LastAccess = a.LastWrite
	a.Creation = time.Unix(0, de.Ctime)
	switch de.Type {
	case libkbfs.File, libkbfs.Exec:
		a.FileAttributes = cbfs.FileAttributeNormal
	case libkbfs.Dir:
		a.FileAttributes = cbfs.FileAttributeDirectory
	case libkbfs.Sym:
		a.FileAttributes = cbfs.FileAttributeReparsePoint
		//		a.ReparsePointTag = cbfs.IOReparseTagSymlink
	}
}

// errToCBFS makes some libkbfs errors easier to digest in CBFS. Not needed in most places.
func errToCBFS(err error) error {
	switch err.(type) {
	case libkbfs.NoSuchNameError:
		return cbfs.ErrFileNotFound
	case libkbfs.NoSuchUserError:
		return cbfs.ErrFileNotFound
	case libkbfs.MDServerErrorUnauthorized:
		return cbfs.ErrAccessDenied
	case nil:
		return nil
	}
	return err
}

// defaultDirectoryInformation returns default directory information.
func defaultDirectoryInformation() (*cbfs.Stat, error) {
	var st cbfs.Stat
	st.FileAttributes = cbfs.FileAttributeDirectory
	return &st, nil
}

// defaultFileInformation returns default file information.
func defaultFileInformation() (*cbfs.Stat, error) {
	var st cbfs.Stat
	st.FileAttributes = cbfs.FileAttributeNormal
	return &st, nil
}

// defaultSymlinkFileInformation returns default symlink to file information.
func defaultSymlinkFileInformation() (*cbfs.Stat, error) {
	var st cbfs.Stat
	st.FileAttributes = cbfs.FileAttributeReparsePoint
	//	st.ReparsePointTag = cbfs.IOReparseTagSymlink
	return &st, nil
}

// defaultSymlinkDirInformation returns default symlink to directory information.
func defaultSymlinkDirInformation() (*cbfs.Stat, error) {
	var st cbfs.Stat
	st.FileAttributes = cbfs.FileAttributeReparsePoint | cbfs.FileAttributeDirectory
	//	st.ReparsePointTag = cbfs.IOReparseTagSymlink
	return &st, nil
}

// lowerTranslateCandidate returns whether a path components
// has a (different) lowercase translation.
func lowerTranslateCandidate(oc *openContext, s string) string {
	if !oc.isUppercasePath {
		return ""
	}
	c := strings.ToLower(s)
	if c == s {
		return ""
	}
	return c
}
