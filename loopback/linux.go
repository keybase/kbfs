// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux

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
	a.Atime = time.Unix(s.Atim.Unix())
	a.Mtime = time.Unix(s.Mtim.Unix())
	a.Ctime = time.Unix(s.Ctim.Unix())
	a.Mode = fi.Mode()
	a.Nlink = uint32(s.Nlink)
	a.Uid = s.Uid
	a.Gid = s.Gid
	a.BlockSize = uint32(s.Blksize)
}

func (n *Node) setattrPlatformSpecific(ctx context.Context,
	req *fuse.SetattrRequest, resp *fuse.SetattrResponse) (err error) {
	return nil
}
