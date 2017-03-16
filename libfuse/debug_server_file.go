// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

// DebugServerFile represents a write-only file where any write of at
// least one byte triggers either disabling or enabling the debug
// server.  It is mainly useful for testing.
type DebugServerFile struct {
	fs     *FS
	enable bool
}

var _ fs.Node = (*DebugServerFile)(nil)

// Attr implements the fs.Node interface for DebugServerFile.
func (f *DebugServerFile) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Size = 0
	a.Mode = 0222
	return nil
}

var _ fs.Handle = (*DebugServerFile)(nil)

var _ fs.HandleWriter = (*DebugServerFile)(nil)

// Write implements the fs.HandleWriter interface for DebugServerFile.
func (f *DebugServerFile) Write(ctx context.Context, req *fuse.WriteRequest,
	resp *fuse.WriteResponse) (err error) {
	f.fs.log.CDebugf(ctx, "DebugServerFile (enable: %t) Write", f.enable)
	defer func() { f.fs.reportErr(ctx, libkbfs.WriteMode, err) }()
	if len(req.Data) == 0 {
		return nil
	}

	if f.enable {
		err := f.fs.enableDebugServer(ctx)
		if err != nil {
			return err
		}
	} else {
		err := f.fs.disableDebugServer(ctx)
		if err != nil {
			return err
		}
	}

	resp.Size = len(req.Data)
	return nil
}
