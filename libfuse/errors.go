// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"syscall"

	"bazil.org/fuse"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfsmd"
)

type errorWithErrno struct {
	error
	errno syscall.Errno
}

var _ fuse.ErrorNumber = errorWithErrno{}

func (e errorWithErrno) Errno() fuse.Errno {
	return fuse.Errno(e.errno)
}

func filterError(err error) error {
	switch err.(type) {
	case kbfsblock.ServerErrorUnauthorized:
		return errorWithErrno{err, syscall.EACCES}
	case kbfsmd.ServerErrorUnauthorized:
		return errorWithErrno{err, syscall.EACCES}
	case kbfsmd.ServerErrorWriteAccess:
		return errorWithErrno{err, syscall.EACCES}
	case kbfsmd.MetadataIsFinalError:
		return errorWithErrno{err, syscall.EACCES}
	}
	return err
}
