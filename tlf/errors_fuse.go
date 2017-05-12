// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build !windows

package tlf

import (
	"syscall"

	"bazil.org/fuse"
)

var _ fuse.ErrorNumber = MDServerErrorUnauthorized{}

// Errno implements the fuse.ErrorNumber interface for MDServerErrorUnauthorized.
func (e MDServerErrorUnauthorized) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}

var _ fuse.ErrorNumber = MDServerErrorWriteAccess{}

// Errno implements the fuse.ErrorNumber interface for MDServerErrorWriteAccess.
func (e MDServerErrorWriteAccess) Errno() fuse.Errno {
	return fuse.Errno(syscall.EACCES)
}
