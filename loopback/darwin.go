// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build darwin

package main

import (
	"context"
	"os"
	"syscall"
	"time"

	"bazil.org/fuse"
)

func fillAttrWithFileInfo(a *fuse.Attr, fi os.FileInfo) {
	s := fi.Sys().(*syscall.Stat_t)
	a.Valid = attrValidDuration
	a.Inode = s.Ino
	a.Size = uint64(s.Size)
	a.Blocks = uint64(s.Blocks)
	a.Atime = time.Unix(s.Atimespec.Unix())
	a.Mtime = time.Unix(s.Mtimespec.Unix())
	a.Ctime = time.Unix(s.Ctimespec.Unix())
	a.Crtime = time.Unix(s.Birthtimespec.Unix())
	a.Mode = fi.Mode()
	a.Nlink = uint32(s.Nlink)
	a.Uid = s.Uid
	a.Gid = s.Gid
	a.Flags = s.Flags
	a.BlockSize = uint32(s.Blksize)
}

func (n *Node) setattrPlatformSpecific(ctx context.Context,
	req *fuse.SetattrRequest, resp *fuse.SetattrResponse) (err error) {
	if req.Valid.Flags() {
		if err = syscall.Chflags(n.realPath, int(req.Flags)); err != nil {
			return err
		}
	}
	return nil
}
