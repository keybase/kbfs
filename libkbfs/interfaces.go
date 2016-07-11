// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

type mdServerLocal interface {
	IFCERFTMDServer
	addNewAssertionForTest(
		uid keybase1.UID, newAssertion keybase1.SocialAssertion) error
	getCurrentMergedHeadRevision(ctx context.Context, id IFCERFTTlfID) (
		rev IFCERFTMetadataRevision, err error)
	isShutdown() bool
	copy(config IFCERFTConfig) mdServerLocal
}

type blockRefLocalStatus int

const (
	liveBlockRef     blockRefLocalStatus = 1
	archivedBlockRef                     = 2
)

// blockServerLocal is the interface for BlockServer implementations
// that store data locally.
type blockServerLocal interface {
	IFCERFTBlockServer
	// getAll returns all the known block references, and should only be
	// used during testing.
	getAll(tlfID IFCERFTTlfID) (map[IFCERFTBlockID]map[IFCERFTBlockRefNonce]blockRefLocalStatus, error)
}

// fileBlockDeepCopier fetches a file block, makes a deep copy of it
// (duplicating pointer for any indirect blocks) and generates a new
// random temporary block ID for it.  It returns the new BlockPointer,
// and internally saves the block for future uses.
type fileBlockDeepCopier func(context.Context, string, IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error)
