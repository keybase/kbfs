// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfscrypto

import (
	"strings"

	"github.com/keybase/client/go/libkb"
)

// The functions below must be used only in tests.

func makeFakeRandomBytes(seed string, byteCount int) []byte {
	paddingLen := byteCount - len(seed)
	if paddingLen > 0 {
		seed = seed + strings.Repeat("0", paddingLen)
	}
	return []byte(seed[:byteCount])
}

// MakeFakeSigningKeyOrBust makes a new signing key from fake
// randomness made from the given seed.
func MakeFakeSigningKeyOrBust(seed string) SigningKey {
	fakeRandomBytes := makeFakeRandomBytes(
		seed, libkb.NaclSigningKeySecretSize)
	var secret [libkb.NaclSigningKeySecretSize]byte
	copy(secret[:], fakeRandomBytes)
	kp, err := libkb.MakeNaclSigningKeyPairFromSecret(secret)
	if err != nil {
		panic(err)
	}
	return NewSigningKey(kp)
}

// MakeFakeVerifyingKeyOrBust makes a new key suitable for verifying
// signatures made from the fake signing key made with the same seed.
func MakeFakeVerifyingKeyOrBust(seed string) VerifyingKey {
	sk := MakeFakeSigningKeyOrBust(seed)
	return sk.GetVerifyingKey()
}

// CryptPrivateKey is a private key for encryption/decryption.
type CryptPrivateKey struct {
	kp libkb.NaclDHKeyPair
}

// GetPublicKey returns the public key corresponding to this private
// key.
func (k CryptPrivateKey) GetPublicKey() CryptPublicKey {
	return MakeCryptPublicKey(k.kp.Public.GetKID())
}

// MakeFakeCryptPrivateKeyOrBust makes a new crypt private key from
// fake randomness made from the given seed.
func MakeFakeCryptPrivateKeyOrBust(seed string) CryptPrivateKey {
	fakeRandomBytes := makeFakeRandomBytes(seed, libkb.NaclDHKeySecretSize)
	var secret [libkb.NaclDHKeySecretSize]byte
	copy(secret[:], fakeRandomBytes)
	kp, err := libkb.MakeNaclDHKeyPairFromSecret(secret)
	if err != nil {
		panic(err)
	}
	return CryptPrivateKey{kp}
}

// MakeFakeCryptPublicKeyOrBust makes the public key corresponding to
// the crypt private key made with the same seed.
func MakeFakeCryptPublicKeyOrBust(seed string) CryptPublicKey {
	k := MakeFakeCryptPrivateKeyOrBust(seed)
	return k.GetPublicKey()
}
