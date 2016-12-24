// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsblock

import (
	"encoding/hex"

	"github.com/keybase/kbfs/kbfscrypto"
)

// RefNonce is a 64-bit unique sequence of bytes for identifying this
// reference of a block ID from other references to the same
// (duplicated) block.
type RefNonce [8]byte

// ZeroRefNonce is a special BlockRefNonce used for the initial
// reference to a block.
var ZeroRefNonce = RefNonce([8]byte{0, 0, 0, 0, 0, 0, 0, 0})

func (nonce RefNonce) String() string {
	return hex.EncodeToString(nonce[:])
}

// MakeRefNonce generates a block reference nonce using a CSPRNG. This
// is used for distinguishing different references to the same
// kbfsblock.ID.
func MakeRefNonce() (RefNonce, error) {
	var nonce RefNonce
	err := kbfscrypto.RandRead(nonce[:])
	if err != nil {
		return ZeroRefNonce, err
	}
	return nonce, nil
}
