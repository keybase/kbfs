// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"time"

	"bazil.org/fuse/fs"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libkbfs"
)

func handleCommonSpecialFile(
	name string, fs *FS, entryValid *time.Duration) fs.Node {
	switch name {
	case libkbfs.ErrorFile:
		return NewErrorFile(fs, entryValid)
	case libfs.MetricsFileName:
		return NewMetricsFile(fs, entryValid)
	case libfs.ProfileListDirName:
		return ProfileList{}
	case libfs.ResetCachesFileName:
		return &ResetCachesFile{fs}
	}

	return nil
}

func handleRootSpecialFile(
	name string, fs *FS, entryValid *time.Duration) fs.Node {
	specialNode := handleCommonSpecialFile(name, fs, entryValid)
	if specialNode != nil {
		return specialNode
	}

	switch name {
	case libfs.StatusFileName:
		return NewStatusFile(fs, nil, entryValid)
	case libfs.HumanErrorFileName, libfs.HumanNoLoginFileName:
		*entryValid = 0
		return &SpecialReadFile{fs.remoteStatus.NewSpecialReadFunc}
	}

	return nil
}

func handleFolderListSpecialFile(
	name string, fs *FS, entryValid *time.Duration) fs.Node {
	return handleCommonSpecialFile(name, fs, entryValid)
}
