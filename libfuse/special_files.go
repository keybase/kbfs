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

func handleTLFSpecialFile(
	name string, folder *Folder, entryValid *time.Duration) fs.Node {
	specialNode := handleCommonSpecialFile(name, folder.fs, entryValid)
	if specialNode != nil {
		return specialNode
	}

	switch name {
	case libfs.StatusFileName:
		folderBranch := folder.getFolderBranch()
		return NewStatusFile(folder.fs, &folderBranch, entryValid)

	case UpdateHistoryFileName:
		return NewUpdateHistoryFile(folder, entryValid)

	case libfs.EditHistoryName:
		folderBranch := folder.getFolderBranch()
		return NewTlfEditHistoryFile(
			folder.fs, folderBranch, entryValid)

	case libfs.UnstageFileName:
		return &UnstageFile{
			folder: folder,
		}

	case libfs.DisableUpdatesFileName:
		return &UpdatesFile{
			folder: folder,
		}

	case libfs.EnableUpdatesFileName:
		return &UpdatesFile{
			folder: folder,
			enable: true,
		}

	case libfs.RekeyFileName:
		return &RekeyFile{
			folder: folder,
		}

	case libfs.ReclaimQuotaFileName:
		return &ReclaimQuotaFile{
			folder: folder,
		}

	case libfs.SyncFromServerFileName:
		return &SyncFromServerFile{
			folder: folder,
		}

	case libfs.EnableJournalFileName:
		return &JournalControlFile{
			folder: folder,
			action: libfs.JournalEnable,
		}

	case libfs.FlushJournalFileName:
		return &JournalControlFile{
			folder: folder,
			action: libfs.JournalFlush,
		}

	case libfs.PauseJournalBackgroundWorkFileName:
		return &JournalControlFile{
			folder: folder,
			action: libfs.JournalPauseBackgroundWork,
		}

	case libfs.ResumeJournalBackgroundWorkFileName:
		return &JournalControlFile{
			folder: folder,
			action: libfs.JournalResumeBackgroundWork,
		}

	case libfs.DisableJournalFileName:
		return &JournalControlFile{
			folder: folder,
			action: libfs.JournalDisable,
		}
	}
	return nil
}
