// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// build !windows

package libkbfs

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// getDiskLimits gets a diskLimits object for the logical disk
// containing the given path.
func getDiskLimits(path string) (diskLimits, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	if err != nil {
		return diskLimits{}, errors.WithStack(err)
	}

	// Bavail is the free block count for an unprivileged user.
	availableBytes := stat.Bavail * uint64(stat.Bsize)

	// TODO: Use stat.Ffree for availableFiles when we add
	// that field.

	return diskLimits{
		availableBytes: availableBytes,
	}, nil
}
