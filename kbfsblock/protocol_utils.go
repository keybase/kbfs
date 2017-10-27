// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsblock

import (
	"github.com/keybase/client/go/protocol/keybase1"
)

// MakeIDCombo builds a keybase1.BlockIdCombo from the given id and
// context.
func MakeIDCombo(id ID, context Context) keybase1.BlockIdCombo {
	// ChargedTo is somewhat confusing when this BlockIdCombo is
	// used in a BlockReference -- it just refers to the original
	// creator of the block, i.e. the original user charged for
	// the block.
	//
	// This may all change once we implement groups.
	return keybase1.BlockIdCombo{
		BlockHash: id.String(),
		ChargedTo: context.GetCreator(),
		BlockType: context.GetBlockType(),
	}
}

// MakeReference builds a keybase1.BlockReference from the given id
// and context.
func MakeReference(id ID, context Context) keybase1.BlockReference {
	// Block references to MD blocks are allowed, because they can be
	// deleted in the case of an MD put failing.
	return keybase1.BlockReference{
		Bid: MakeIDCombo(id, context),
		// The actual writer to modify quota for.
		ChargedTo: context.GetWriter(),
		Nonce:     keybase1.BlockRefNonce(context.GetRefNonce()),
	}
}
