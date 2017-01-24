// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

// diskLimits contains information about a particular logical disk's
// limits.
//
// TODO: Also track available files.
type diskLimits struct {
	availableBytes uint64
}
