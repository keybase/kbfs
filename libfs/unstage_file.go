// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfs

import (
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func UnstageForTesting(ctx context.Context, config libkbfs.Config,
	fb libkbfs.FolderBranch, data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	err := config.KBFSOps().UnstageForTesting(ctx, fb)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
