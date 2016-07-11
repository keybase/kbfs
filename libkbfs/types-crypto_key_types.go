// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding"
	"encoding/hex"

	"github.com/keybase/client/go/protocol"
)

// All section references below are to https://keybase.io/blog/kbfs-crypto
// (version 1.3).

type IFCERFTKidContainer struct {
	kid keybase1.KID
}

var _ encoding.BinaryMarshaler = IFCERFTKidContainer{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTKidContainer)(nil)

func (k IFCERFTKidContainer) MarshalBinary() (data []byte, err error) {
	if k.kid.IsNil() {
		return nil, nil
	}

	// TODO: Use the more stringent checks from
	// KIDFromStringChecked instead.
	if !k.kid.IsValid() {
		return nil, IFCERFTInvalidKIDError{k.kid}
	}

	return k.kid.ToBytes(), nil
}

func (k *IFCERFTKidContainer) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		*k = IFCERFTKidContainer{}
		return nil
	}

	k.kid = keybase1.KIDFromSlice(data)
	// TODO: Use the more stringent checks from
	// KIDFromStringChecked instead.
	if !k.kid.IsValid() {
		err := IFCERFTInvalidKIDError{k.kid}
		*k = IFCERFTKidContainer{}
		return err
	}

	return nil
}

// Needed by mdserver/server_test.go. TODO: Figure out how to avoid
// this.
func (k IFCERFTKidContainer) KID() keybase1.KID {
	return k.kid
}

func (k IFCERFTKidContainer) String() string {
	return k.kid.String()
}

// A VerifyingKey is a public key that can be used to verify a
// signature created by the corresponding private signing key. In
// particular, VerifyingKeys are used to authenticate home and public
// TLFs. (See 4.2, 4.3.)
//
// These are also sometimes known as sibkeys.
//
// Copies of VerifyingKey objects are deep copies.
type IFCERFTVerifyingKey struct {
	// Should only be used by implementations of Crypto and KBPKI.
	//
	// Even though we currently use NaclSignatures, we use a KID
	// here (which encodes the key type) as we may end up storing
	// other kinds of signatures.
	IFCERFTKidContainer
}

var _ encoding.BinaryMarshaler = IFCERFTVerifyingKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTVerifyingKey)(nil)

// MakeVerifyingKey returns a VerifyingKey containing the given KID.
func IFCERFTMakeVerifyingKey(kid keybase1.KID) IFCERFTVerifyingKey {
	return IFCERFTVerifyingKey{IFCERFTKidContainer{kid}}
}

// IsNil returns true if the VerifyingKey is nil.
func (k IFCERFTVerifyingKey) IsNil() bool {
	return k.kid.IsNil()
}

type byte32Container struct {
	data [32]byte
}

var _ encoding.BinaryMarshaler = byte32Container{}
var _ encoding.BinaryUnmarshaler = (*byte32Container)(nil)

func (c byte32Container) MarshalBinary() (data []byte, err error) {
	return c.data[:], nil
}

func (c *byte32Container) UnmarshalBinary(data []byte) error {
	if len(data) != len(c.data) {
		err := IFCERFTInvalidByte32DataError{data}
		*c = byte32Container{}
		return err
	}

	copy(c.data[:], data)
	return nil
}

func (c byte32Container) String() string {
	return hex.EncodeToString(c.data[:])
}

// A TLFPrivateKey (m_f) is the private half of the permanent
// keypair associated with a TLF. (See 4.1.1, 5.3.)
//
// Copies of TLFPrivateKey objects are deep copies.
type IFCERFTTLFPrivateKey struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFPrivateKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFPrivateKey)(nil)

// MakeTLFPrivateKey returns a TLFPrivateKey containing the given
// data.
func IFCERFTMakeTLFPrivateKey(data [32]byte) IFCERFTTLFPrivateKey {
	return IFCERFTTLFPrivateKey{byte32Container{data}}
}

// A TLFPublicKey (M_f) is the public half of the permanent keypair
// associated with a TLF. It is included in the site-wide private-data
// Merkle tree. (See 4.1.1, 5.3.)
//
// Copies of TLFPublicKey objects are deep copies.
type IFCERFTTLFPublicKey struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFPublicKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFPublicKey)(nil)

// MakeTLFPublicKey returns a TLFPublicKey containing the given
// data.
func IFCERFTMakeTLFPublicKey(data [32]byte) IFCERFTTLFPublicKey {
	return IFCERFTTLFPublicKey{byte32Container{data}}
}

// TLFEphemeralPrivateKey (m_e) is used (with a CryptPublicKey) to
// encrypt TLFCryptKeyClientHalf objects (t_u^{f,0,i}) for non-public
// directories. (See 4.1.1.)
//
// Copies of TLFEphemeralPrivateKey objects are deep copies.
type IFCERFTTLFEphemeralPrivateKey struct {
	// Should only be used by implementations of Crypto. Meant to
	// be converted to libkb.NaclDHKeyPrivate.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFEphemeralPrivateKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFEphemeralPrivateKey)(nil)

// MakeTLFEphemeralPrivateKey returns a TLFEphemeralPrivateKey
// containing the given data.
func IFCERFTMakeTLFEphemeralPrivateKey(data [32]byte) IFCERFTTLFEphemeralPrivateKey {
	return IFCERFTTLFEphemeralPrivateKey{byte32Container{data}}
}

// CryptPublicKey (M_u^i) is used (with a TLFEphemeralPrivateKey) to
// encrypt TLFCryptKeyClientHalf objects (t_u^{f,0,i}) for non-public
// directories. (See 4.1.1.)  These are also sometimes known as
// subkeys.
//
// Copies of CryptPublicKey objects are deep copies.
type IFCERFTCryptPublicKey struct {
	// Should only be used by implementations of Crypto.
	//
	// Even though we currently use nacl/box, we use a KID here
	// (which encodes the key type) as we may end up storing other
	// kinds of keys.
	IFCERFTKidContainer
}

// MakeCryptPublicKey returns a CryptPublicKey containing the given KID.
func IFCERFTMakeCryptPublicKey(kid keybase1.KID) IFCERFTCryptPublicKey {
	return IFCERFTCryptPublicKey{IFCERFTKidContainer{kid}}
}

var _ encoding.BinaryMarshaler = IFCERFTCryptPublicKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTCryptPublicKey)(nil)

// TLFEphemeralPublicKey (M_e) is used along with a crypt private key
// to decrypt TLFCryptKeyClientHalf objects (t_u^{f,0,i}) for
// non-public directories. (See 4.1.1.)
//
// Copies of TLFEphemeralPublicKey objects are deep copies.
type IFCERFTTLFEphemeralPublicKey struct {
	// Should only be used by implementations of Crypto. Meant to
	// be converted to libkb.NaclDHKeyPublic.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFEphemeralPublicKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFEphemeralPublicKey)(nil)

// MakeTLFEphemeralPublicKey returns a TLFEphemeralPublicKey
// containing the given data.
func IFCERFTMakeTLFEphemeralPublicKey(data [32]byte) IFCERFTTLFEphemeralPublicKey {
	return IFCERFTTLFEphemeralPublicKey{byte32Container{data}}
}

// TLFCryptKeyServerHalf (s_u^{f,0,i}) is the masked, server-side half
// of a TLFCryptKey, which can be recovered only with both
// halves. (See 4.1.1.)
//
// Copies of TLFCryptKeyServerHalf objects are deep copies.
type IFCERFTTLFCryptKeyServerHalf struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFCryptKeyServerHalf{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFCryptKeyServerHalf)(nil)

// MakeTLFCryptKeyServerHalf returns a TLFCryptKeyServerHalf
// containing the given data.
func IFCERFTMakeTLFCryptKeyServerHalf(data [32]byte) IFCERFTTLFCryptKeyServerHalf {
	return IFCERFTTLFCryptKeyServerHalf{byte32Container{data}}
}

// TLFCryptKeyClientHalf (t_u^{f,0,i}) is the masked, client-side half
// of a TLFCryptKey, which can be recovered only with both
// halves. (See 4.1.1.)
//
// Copies of TLFCryptKeyClientHalf objects are deep copies.
type IFCERFTTLFCryptKeyClientHalf struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFCryptKeyClientHalf{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFCryptKeyClientHalf)(nil)

// MakeTLFCryptKeyClientHalf returns a TLFCryptKeyClientHalf
// containing the given data.
func IFCERFTMakeTLFCryptKeyClientHalf(data [32]byte) IFCERFTTLFCryptKeyClientHalf {
	return IFCERFTTLFCryptKeyClientHalf{byte32Container{data}}
}

// TLFCryptKey (s^{f,0}) is used to encrypt/decrypt the private
// portion of TLF metadata. It is also used to mask
// BlockCryptKeys. (See 4.1.1, 4.1.2.)
//
// Copies of TLFCryptKey objects are deep copies.
type IFCERFTTLFCryptKey struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTTLFCryptKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTLFCryptKey)(nil)

// MakeTLFCryptKey returns a TLFCryptKey containing the given data.
func IFCERFTMakeTLFCryptKey(data [32]byte) IFCERFTTLFCryptKey {
	return IFCERFTTLFCryptKey{byte32Container{data}}
}

// PublicTLFCryptKey is the TLFCryptKey used for all public TLFs. That
// means that anyone with just the block key for a public TLF can
// decrypt that block. This is not the zero TLFCryptKey so that we can
// distinguish it from an (erroneously?) unset TLFCryptKey.
var PublicTLFCryptKey = IFCERFTMakeTLFCryptKey([32]byte{
	0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18,
	0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18,
	0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18,
	0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18,
})

// BlockCryptKeyServerHalf is a masked version of a BlockCryptKey,
// which can be recovered only with the TLFCryptKey used to mask the
// server half.
//
// Copies of BlockCryptKeyServerHalf objects are deep copies.
type IFCERFTBlockCryptKeyServerHalf struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTBlockCryptKeyServerHalf{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTBlockCryptKeyServerHalf)(nil)

// MakeBlockCryptKeyServerHalf returns a BlockCryptKeyServerHalf
// containing the given data.
func IFCERFTMakeBlockCryptKeyServerHalf(data [32]byte) IFCERFTBlockCryptKeyServerHalf {
	return IFCERFTBlockCryptKeyServerHalf{byte32Container{data}}
}

// ParseBlockCryptKeyServerHalf returns a BlockCryptKeyServerHalf
// containing the given hex-encoded data, or an error.
func IFCERFTParseBlockCryptKeyServerHalf(s string) (IFCERFTBlockCryptKeyServerHalf, error) {
	buf, err := hex.DecodeString(s)
	if err != nil {
		return IFCERFTBlockCryptKeyServerHalf{}, err
	}
	var serverHalf IFCERFTBlockCryptKeyServerHalf
	err = serverHalf.UnmarshalBinary(buf)
	if err != nil {
		return IFCERFTBlockCryptKeyServerHalf{}, err
	}
	return serverHalf, nil
}

// BlockCryptKey is used to encrypt/decrypt block data. (See 4.1.2.)
type IFCERFTBlockCryptKey struct {
	// Should only be used by implementations of Crypto.
	byte32Container
}

var _ encoding.BinaryMarshaler = IFCERFTBlockCryptKey{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTBlockCryptKey)(nil)

// MakeBlockCryptKey returns a BlockCryptKey containing the given
// data.
//
// Copies of BlockCryptKey objects are deep copies.
func IFCERFTMakeBlockCryptKey(data [32]byte) IFCERFTBlockCryptKey {
	return IFCERFTBlockCryptKey{byte32Container{data}}
}
