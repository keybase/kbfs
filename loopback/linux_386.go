// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux,386

package main

import (
	"syscall"
	"time"
)

func tToTv(t time.Time) (tv syscall.Timeval) {
	tv.Sec = int32(t.Unix())
	tv.Usec = int32(t.UnixNano() % time.Second.Nanoseconds() /
		time.Microsecond.Nanoseconds())
	return tv
}
