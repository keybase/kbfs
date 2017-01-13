// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"github.com/keybase/kbfs/libcbfs/cbfs"
	"golang.org/x/net/context"
)

type specialWriteFile struct {
	emptyFile
}

func (*specialWriteFile) GetFileInformation(context.Context) (*cbfs.Stat, error) {
	return defaultFileInformation()
}
func (*specialWriteFile) SetAllocationSize(ctx context.Context, length int64) error {
	return nil
}
func (*specialWriteFile) SetEndOfFile(ctx context.Context, length int64) error {
	return nil
}
func (*specialWriteFile) SetFileAttributes(ctx context.Context, fileAttributes *cbfs.Stat) error {
	return nil
}
