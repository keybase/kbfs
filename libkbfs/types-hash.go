// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"fmt"
)

// See https://keybase.io/admin-docs/hash-format for the design doc
// for the keybase hash format.

const (
	// MinHashByteLength is the minimum number of bytes a valid
	// keybase hash can be, including the 1 byte for the type.
	IFCERFTMinHashByteLength = 33

	// DefaultHashByteLength is the number of bytes in a default
	// keybase hash.
	IFCERFTDefaultHashByteLength = 1 + sha256.Size

	// MaxHashByteLength is the maximum number of bytes a valid
	// keybase hash can be, including the 1 byte for the type.
	MaxHashByteLength = 129

	// MinHashStringLength is the minimum number of characters in
	// the string representation (hex encoding) of a valid keybase
	// hash.
	IFCERFTMinHashStringLength = 2 * IFCERFTMinHashByteLength

	// DefaultHashStringLength is the number of characters in the
	// string representation of a default keybase hash.
	IFCERFTDefaultHashStringLength = 2 * IFCERFTDefaultHashByteLength

	// MaxHashStringLength is the maximum number of characters the
	// string representation of a valid keybase hash can be.
	IFCERFTMaxHashStringLength = 2 * MaxHashByteLength
)

// HashType is the type of a keybase hash.
type IFCERFTHashType byte

const (
	// InvalidHash is the zero HashType value, which is invalid.
	IFCERFTInvalidHash IFCERFTHashType = 0
	// SHA256Hash is the type of a SHA256 hash.
	IFCERFTSHA256Hash IFCERFTHashType = 1
)

func (t IFCERFTHashType) String() string {
	switch t {
	case IFCERFTInvalidHash:
		return "InvalidHash"
	case IFCERFTSHA256Hash:
		return "SHA256Hash"
	default:
		return fmt.Sprintf("HashType(%d)", t)
	}
}

// DefaultHashType is the current default keybase hash type.
const IFCERFTDefaultHashType IFCERFTHashType = IFCERFTSHA256Hash

// DefaultHashNew is a function that creates a new hash.Hash object
// with the default hash.
var IFCERFTDefaultHashNew = sha256.New

// RawDefaultHash is the type for the raw bytes of a default keybase
// hash. This is exposed for use as in-memory keys.
type IFCERFTRawDefaultHash [sha256.Size]byte

// DoRawDefaultHash computes the default keybase hash of the given
// data, and returns the type and the raw hash bytes.
func IFCERFTDoRawDefaultHash(p []byte) (IFCERFTHashType, IFCERFTRawDefaultHash) {
	return IFCERFTDefaultHashType, IFCERFTRawDefaultHash(sha256.Sum256(p))
}

// Hash is the type of a keybase hash.
type IFCERFTHash struct {
	// Stored as a string so that this can be used as a map key.
	h string
}

var _ encoding.BinaryMarshaler = IFCERFTHash{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTHash)(nil)

// HashFromRaw creates a hash from a type and raw hash data. If the
// returned error is nil, the returned Hash is valid.
func IFCERFTHashFromRaw(hashType IFCERFTHashType, rawHash []byte) (IFCERFTHash, error) {
	return IFCERFTHashFromBytes(append([]byte{byte(hashType)}, rawHash...))
}

// HashFromBytes creates a hash from the given byte array. If the
// returned error is nil, the returned Hash is valid.
func IFCERFTHashFromBytes(data []byte) (IFCERFTHash, error) {
	h := IFCERFTHash{string(data)}
	if !h.IsValid() {
		return IFCERFTHash{}, IFCERFTInvalidHashError{h}
	}
	return h, nil
}

// HashFromString creates a hash from the given string. If the
// returned error is nil, the returned Hash is valid.
func IFCERFTHashFromString(dataStr string) (IFCERFTHash, error) {
	data, err := hex.DecodeString(dataStr)
	if err != nil {
		return IFCERFTHash{}, err
	}
	return IFCERFTHashFromBytes(data)
}

// DefaultHash computes the hash of the given data with the default
// hash type.
func IFCERFTDefaultHash(buf []byte) (IFCERFTHash, error) {
	hashType, rawHash := IFCERFTDoRawDefaultHash(buf)
	return IFCERFTHashFromRaw(hashType, rawHash[:])
}

func (h IFCERFTHash) hashType() IFCERFTHashType {
	return IFCERFTHashType(h.h[0])
}

func (h IFCERFTHash) hashData() []byte {
	return []byte(h.h[1:])
}

// IsValid returns whether the hash is valid. Note that a hash with an
// unknown version is still valid.
func (h IFCERFTHash) IsValid() bool {
	if len(h.h) < IFCERFTMinHashByteLength {
		return false
	}
	if len(h.h) > MaxHashByteLength {
		return false
	}

	if h.hashType() == IFCERFTInvalidHash {
		return false
	}

	return true
}

// Bytes returns the bytes of the hash.
func (h IFCERFTHash) Bytes() []byte {
	return []byte(h.h)
}

func (h IFCERFTHash) String() string {
	return hex.EncodeToString([]byte(h.h))
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// Hash. Returns an error if the hash is invalid and not the zero
// hash.
func (h IFCERFTHash) MarshalBinary() (data []byte, err error) {
	if h == (IFCERFTHash{}) {
		return nil, nil
	}

	if !h.IsValid() {
		return nil, IFCERFTInvalidHashError{h}
	}

	return []byte(h.h), nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for Hash. Returns an error if the given byte array is non-empty and
// the hash is invalid.
func (h *IFCERFTHash) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		*h = IFCERFTHash{}
		return nil
	}

	h.h = string(data)
	if !h.IsValid() {
		err := IFCERFTInvalidHashError{*h}
		*h = IFCERFTHash{}
		return err
	}

	return nil
}

// Verify makes sure that the hash matches the given data and returns
// an error otherwise.
func (h IFCERFTHash) Verify(buf []byte) error {
	if !h.IsValid() {
		return IFCERFTInvalidHashError{h}
	}

	// Once we have multiple hash types we'll need to expand this.
	t := h.hashType()
	if t != IFCERFTDefaultHashType {
		return IFCERFTUnknownHashTypeError{t}
	}

	expectedH, err := IFCERFTDefaultHash(buf)
	if err != nil {
		return err
	}
	if h != expectedH {
		return IFCERFTHashMismatchError{expectedH, h}
	}
	return nil
}

// HMAC is the type of a keybase hash that is an HMAC.
//
// All the constants for Hash also apply to HMAC.
type IFCERFTHMAC struct {
	h IFCERFTHash
}

var _ encoding.BinaryMarshaler = IFCERFTHMAC{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTHMAC)(nil)

// DefaultHMAC computes the HMAC with the given key of the given data
// using the default hash.
func IFCERFTDefaultHMAC(key, buf []byte) (IFCERFTHMAC, error) {
	mac := hmac.New(IFCERFTDefaultHashNew, key)
	mac.Write(buf)
	h, err := IFCERFTHashFromRaw(IFCERFTDefaultHashType, mac.Sum(nil))
	if err != nil {
		return IFCERFTHMAC{}, err
	}
	return IFCERFTHMAC{h}, nil
}

func (hmac IFCERFTHMAC) hashType() IFCERFTHashType {
	return hmac.h.hashType()
}

func (hmac IFCERFTHMAC) hashData() []byte {
	return hmac.h.hashData()
}

// IsValid returns whether the HMAC is valid. Note that an HMAC with an
// unknown version is still valid.
func (hmac IFCERFTHMAC) IsValid() bool {
	return hmac.h.IsValid()
}

// Bytes returns the bytes of the HMAC.
func (hmac IFCERFTHMAC) Bytes() []byte {
	return hmac.h.Bytes()
}

func (hmac IFCERFTHMAC) String() string {
	return hmac.h.String()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// HMAC. Returns an error if the HMAC is invalid and not the zero
// HMAC.
func (hmac IFCERFTHMAC) MarshalBinary() (data []byte, err error) {
	return hmac.h.MarshalBinary()
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for HMAC. Returns an error if the given byte array is non-empty and
// the HMAC is invalid.
func (hmac *IFCERFTHMAC) UnmarshalBinary(data []byte) error {
	return hmac.h.UnmarshalBinary(data)
}

// Verify makes sure that the HMAC matches the given data.
func (hmac IFCERFTHMAC) Verify(key, buf []byte) error {
	if !hmac.IsValid() {
		return IFCERFTInvalidHashError{hmac.h}
	}

	// Once we have multiple hash types we'll need to expand this.
	t := hmac.hashType()
	if t != IFCERFTDefaultHashType {
		return IFCERFTUnknownHashTypeError{t}
	}

	expectedHMAC, err := IFCERFTDefaultHMAC(key, buf)
	if err != nil {
		return err
	}
	if hmac != expectedHMAC {
		return IFCERFTHashMismatchError{expectedHMAC.h, hmac.h}
	}
	return nil
}
