// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/keybase/client/go/libkb"

// FolderBranchStatus is a simple data structure describing the
// current status of a particular folder-branch.  It is suitable for
// encoding directly as JSON.
type IFCERFTFolderBranchStatus struct {
	Staged       bool
	HeadWriter   libkb.NormalizedUsername
	DiskUsage    uint64
	RekeyPending bool
	FolderID     string

	// DirtyPaths are files that have been written, but not flushed.
	// They do not represent unstaged changes in your local instance.
	DirtyPaths []string

	// If we're in the staged state, these summaries show the
	// diverging operations per-file
	Unmerged []*IFCERFTCrChainSummary
	Merged   []*IFCERFTCrChainSummary
}

// StatusUpdate is a dummy type used to indicate status has been updated.
type IFCERFTStatusUpdate struct{}
