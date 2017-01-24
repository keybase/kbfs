// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"unsafe"

	"github.com/pkg/errors"
)

// getDiskLimits gets a diskLimits object for the logical disk
// containing the given path.
func getDiskLimits(path string) (diskLimits, error) {
	pathPtr, err := UTF16PtrFromString(path)
	if err != nil {
		return diskLimits{}, errors.WithStack(err)
	}

	var availableBytes uint64
	dll := windows.NewLazySystemDLL("kernel32.dll")
	proc := dll.NewProc("GetDiskFreeSpaceExW")
	r1, _, err := proc.Call(uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&availableBytes)), 0, 0)
	if r1 == 0 {
		return diskLimits{}, errors.WithStack(err)
	}

	// TODO: According to http://superuser.com/a/104224 , on
	// Windows, the available file limit is determined just from
	// the filesystem type. Detect the filesystem type and use
	// that to fill in availableFiles, when we add that field.

	return diskLimits{
		availableBytes: availableBytes,
	}, nil
}
