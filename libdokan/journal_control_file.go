// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build windows

package libdokan

import (
	"fmt"

	"github.com/keybase/kbfs/dokan"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libkbfs"
)

// JournalControlFile is a special file used to control journal
// settings.
type JournalControlFile struct {
	specialWriteFile
	folder *Folder
	action libfs.JournalAction
}

// Write implements writes for dokan.
func (f *JournalControlFile) WriteFile(
	fi *dokan.FileInfo, bs []byte, offset int64) (n int, err error) {
	ctx, cancel := NewContextWithOpID(
		f.folder.fs,
		fmt.Sprintf("JournalQuotaFile (action=%s) Write", action))
	defer func() { f.folder.reportErr(ctx, libkbfs.WriteMode, err, cancel) }()
	if len(req.Data) == 0 {
		return nil
	}

	jServer, err := libkbfs.GetJournalServer(f.folder.fs.config)
	if err != nil {
		return 0, err
	}

	switch f.action {
	case libfs.JournalEnable:
		err := jServer.Enable(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return 0, err
		}

	case libfs.JournalFlush:
		err := jServer.Flush(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return 0, err
		}

	case libfs.JournalDisable:
		err := jServer.Disable(f.folder.getFolderBranch().Tlf)
		if err != nil {
			return 0, err
		}

	default:
		return 0, fmt.Errorf("Unknown action %s", f.action)
	}

	return len(bs), err
}
