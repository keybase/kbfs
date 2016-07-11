// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build !windows

package libkbfs

import (
	"syscall"

	"bazil.org/fuse"
)

var _ fuse.ErrorNumber = IFCERFTNoSuchUserError{""}

// Errno implements the fuse.ErrorNumber interface for
// NoSuchUserError
func (e IFCERFTNoSuchUserError) Errno() fuse.Errno {
	return fuse.Errno(syscall.ENOENT)
}

var _ fuse.ErrorNumber = IFCERFTDirNotEmptyError{""}

// Errno implements the fuse.ErrorNumber interface for
// DirNotEmptyError
func (e IFCERFTDirNotEmptyError) Errno() fuse.Errno {
	return fuse.Errno(syscall.ENOTEMPTY)
}

var _ fuse.ErrorNumber = IFCERFTReadAccessError{}

// Errno implements the fuse.ErrorNumber interface for
// ReadAccessError.
func (e IFCERFTReadAccessError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTWriteAccessError{}

// Errno implements the fuse.ErrorNumber interface for
// WriteAccessError.
func (e IFCERFTWriteAccessError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTNeedSelfRekeyError{}

// Errno implements the fuse.ErrorNumber interface for
// NeedSelfRekeyError.
func (e IFCERFTNeedSelfRekeyError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTNeedOtherRekeyError{}

// Errno implements the fuse.ErrorNumber interface for
// NeedOtherRekeyError.
func (e IFCERFTNeedOtherRekeyError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTDisallowedPrefixError{}

// Errno implements the fuse.ErrorNumber interface for
// DisallowedPrefixError.
func (e IFCERFTDisallowedPrefixError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EINVAL)
}

var _ fuse.ErrorNumber = BServerErrorUnauthorized{}

// Errno implements the fuse.ErrorNumber interface for BServerErrorUnauthorized.
func (e BServerErrorUnauthorized) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = MDServerErrorUnauthorized{}

// Errno implements the fuse.ErrorNumber interface for MDServerErrorUnauthorized.
func (e MDServerErrorUnauthorized) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTFileTooBigError{}

// Errno implements the fuse.ErrorNumber interface for FileTooBigError.
func (e IFCERFTFileTooBigError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EFBIG)
}

var _ fuse.ErrorNumber = IFCERFTNameTooLongError{}

// Errno implements the fuse.ErrorNumber interface for NameTooLongError.
func (e IFCERFTNameTooLongError) Errno() fuse.Errno {
	return fuse.Errno(syscall.ENAMETOOLONG)
}

var _ fuse.ErrorNumber = IFCERFTDirTooBigError{}

// Errno implements the fuse.ErrorNumber interface for DirTooBigError.
func (e IFCERFTDirTooBigError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EFBIG)
}

var _ fuse.ErrorNumber = IFCERFTNoCurrentSessionError{}

// Errno implements the fuse.ErrorNumber interface for NoCurrentSessionError.
func (e IFCERFTNoCurrentSessionError) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = MDServerErrorWriteAccess{}

// Errno implements the fuse.ErrorNumber interface for MDServerErrorWriteAccess.
func (e MDServerErrorWriteAccess) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = IFCERFTNoSuchFolderListError{}

// Errno implements the fuse.ErrorNumber interface for
// NoSuchFolderListError
func (e IFCERFTNoSuchFolderListError) Errno() fuse.Errno {
	return fuse.Errno(syscall.ENOENT)
}
