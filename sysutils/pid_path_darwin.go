// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package sysutils

import (
	"bytes"
	"syscall"
	"unsafe"
)

const (
	proc_pidpathinfo      = 11
	proc_pidpathinfo_size = 1024
	proc_callnum_pidinfo  = 2
)

func GetExecPathFromPID(pid int) (string, error) {
	buf := make([]byte, proc_pidpathinfo_size)
	_, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, proc_callnum_pidinfo, uintptr(pid), proc_pidpathinfo, 0, uintptr(unsafe.Pointer(&buf[0])), proc_pidpathinfo_size)
	if errno != 0 {
		return "", errno
	}
	nonZero := bytes.IndexByte(buf, 0)
	if nonZero <= 0 {
		return "", nil
	}
	return string(buf[:nonZero]), nil
}
