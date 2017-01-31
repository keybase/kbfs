// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "testing"

func TestSemaphoreDiskLimiterBasic(t *testing.T) {
	s, _ := newSemaphoreDiskLimiter(100, 100, 2)
	_ = s
}
