// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"fmt"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

type journalAction int

const (
	journalEnable journalAction = iota
	journalFlush
	journalDisable
)

func (a journalAction) String() string {
	switch a {
	case journalEnable:
		return "Enable journal"
	case journalFlush:
		return "Flush journal"
	case journalDisable:
		return "Disable journal"
	}
	return fmt.Sprintf("journalAction(%d)", int(a))
}

// JournalControlFile is a special file used to control journal
// settings.
type JournalControlFile struct {
	folder *Folder
	action journalAction
}

var _ fs.Node = (*JournalControlFile)(nil)

// Attr implements the fs.Node interface for JournalControlFile.
func (f *JournalControlFile) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Size = 0
	a.Mode = 0222
	return nil
}

var _ fs.Handle = (*JournalControlFile)(nil)

var _ fs.HandleWriter = (*JournalControlFile)(nil)

// Write implements the fs.HandleWriter interface for JournalControlFile.
func (f *JournalControlFile) Write(ctx context.Context, req *fuse.WriteRequest,
	resp *fuse.WriteResponse) (err error) {
	f.folder.fs.log.CDebugf(ctx, "JournalControlFile (action=%s) Write",
		f.action)
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err) }()
	if len(req.Data) == 0 {
		return nil
	}

	jServer, err := libkbfs.GetJournalServer(f.folder.fs.config)
	if err != nil {
		return err
	}

	switch f.action {
	case journalEnable:
		err := jServer.Enable(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return err
		}

	case journalFlush:
		jServer.Flush(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return err
		}

	case journalDisable:
		jServer.Flush(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unknown action %s", f.action)
	}

	resp.Size = len(req.Data)
	return nil
}
