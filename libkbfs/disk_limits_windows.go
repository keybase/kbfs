// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"unsafe"

	"github.com/pkg/errors"
)

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

	return diskLimits{
		availableBytes: availableBytes,
	}, nil
}
