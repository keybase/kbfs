// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsblock

import "github.com/keybase/kbfs/kbfscrypto"

// Crypto is an interface with methods that usually depend on a
// CSPRNG, but can be overridden for testing.
type Crypto interface {
	// MakeRefNonce generates a block reference nonce using a
	// CSPRNG. This is used for distinguishing different
	// references to the same kbfsblock.ID.
	MakeRefNonce() (RefNonce, error)

	// MakeTemporaryID generates a temporary block ID using a
	// CSPRNG. This is used for indirect blocks before they're
	// committed to the server.
	MakeTemporaryID() (ID, error)

	// MakeRandomBlockCryptKeyServerHalf generates the server-side
	// of a block crypt key.
	MakeRandomBlockCryptKeyServerHalf() (
		kbfscrypto.BlockCryptKeyServerHalf, error)
}

// DefaultCrypto is the implementation of Crypto that calls to the
// real implementations of the functions.
type DefaultCrypto struct{}

var _ Crypto = DefaultCrypto{}

// MakeRefNonce implements Crypto.
func (c DefaultCrypto) MakeRefNonce() (RefNonce, error) {
	return MakeRefNonce()
}

// MakeTemporaryID implements Crypto.
func (c DefaultCrypto) MakeTemporaryID() (ID, error) {
	return MakeTemporaryID()
}

// MakeRandomBlockCryptKeyServerHalf implements Crypto.
func (c DefaultCrypto) MakeRandomBlockCryptKeyServerHalf() (
	kbfscrypto.BlockCryptKeyServerHalf, error) {
	return kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
}
