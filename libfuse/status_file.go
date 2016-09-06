// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"time"

	"golang.org/x/net/context"

	"github.com/keybase/kbfs/libfs"
)

// NewGlobalStatusFile returns a special read file that contains a
// text representation of the status of the current TLF.
func NewGlobalStatusFile(fs *FS, entryValid *time.Duration) *SpecialReadFile {
	*entryValid = 0
	return &SpecialReadFile{
		read: func(ctx context.Context) ([]byte, time.Time, error) {
			return libfs.GetEncodedStatus(ctx, fs.config)
		},
	}
}

func NewTLFStatusFile(
	folder *Folder, entryValid *time.Duration) *SpecialReadFile {
	*entryValid = 0
	return &SpecialReadFile{
		read: func(ctx context.Context) ([]byte, time.Time, error) {
			return libfs.GetEncodedFolderStatus(
				ctx, folder.fs.config, folder.getFolderBranch())
		},
	}
}
