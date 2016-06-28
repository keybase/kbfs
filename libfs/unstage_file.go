// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfs

import (
	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

// UnstageForTesting unstages all unmerged commits and fast-forwards
// to the current master, if the given data is non-empty. If the given
// data is empty, it does nothing. If the given data has bytes
// "async", the unstaging is done asynchronously, i.e. this function
// returns immediately and the unstaging happens in the
// background. (Other subsequent IO operations may be blocked,
// though.)
func UnstageForTesting(ctx context.Context, log logger.Logger,
	config libkbfs.Config, fb libkbfs.FolderBranch,
	data []byte) (int, error) {
	log.CDebugf(ctx, "UnstageForTesting(%v, %v)", fb, data)
	if len(data) == 0 {
		return 0, nil
	}

	err := config.KBFSOps().UnstageForTesting(ctx, fb)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
