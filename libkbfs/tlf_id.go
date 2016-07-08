// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding"
	"encoding/hex"
)

const (
	// TlfIDByteLen is the number of bytes in a top-level folder ID
	TlfIDByteLen = 16
	// TlfIDStringLen is the number of characters in the string
	// representation of a top-level folder ID
	TlfIDStringLen = 2 * TlfIDByteLen
	// TlfIDSuffix is the last byte of a private top-level folder ID
	TlfIDSuffix = 0x16
	// PubTlfIDSuffix is the last byte of a public top-level folder ID
	PubTlfIDSuffix = 0x17
)

// TlfID is a top-level folder ID
type IFCERFTTlfID struct {
	id [TlfIDByteLen]byte
}

var _ encoding.BinaryMarshaler = IFCERFTTlfID{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTTlfID)(nil)

// NullTlfID is an empty TlfID
var NullTlfID = IFCERFTTlfID{}

// Bytes returns the bytes of the TLF ID.
func (id IFCERFTTlfID) Bytes() []byte {
	return id.id[:]
}

// String implements the fmt.Stringer interface for TlfID.
func (id IFCERFTTlfID) String() string {
	return hex.EncodeToString(id.id[:])
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for TlfID.
func (id IFCERFTTlfID) MarshalBinary() (data []byte, err error) {
	suffix := id.id[TlfIDByteLen-1]
	if suffix != TlfIDSuffix && suffix != PubTlfIDSuffix {
		return nil, InvalidTlfID{id.String()}
	}
	return id.id[:], nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for TlfID.
func (id *IFCERFTTlfID) UnmarshalBinary(data []byte) error {
	if len(data) != TlfIDByteLen {
		return InvalidTlfID{hex.EncodeToString(data)}
	}
	suffix := data[TlfIDByteLen-1]
	if suffix != TlfIDSuffix && suffix != PubTlfIDSuffix {
		return InvalidTlfID{hex.EncodeToString(data)}
	}
	copy(id.id[:], data)
	return nil
}

// IsPublic returns true if this TlfID is for a public top-level folder
func (id IFCERFTTlfID) IsPublic() bool {
	return id.id[TlfIDByteLen-1] == PubTlfIDSuffix
}

// ParseTlfID parses a hex encoded TlfID. Returns NullTlfID and an
// InvalidTlfID on failure.
func ParseTlfID(s string) (IFCERFTTlfID, error) {
	if len(s) != TlfIDStringLen {
		return NullTlfID, InvalidTlfID{s}
	}
	bytes, err := hex.DecodeString(s)
	if err != nil {
		return NullTlfID, InvalidTlfID{s}
	}
	var id IFCERFTTlfID
	err = id.UnmarshalBinary(bytes)
	if err != nil {
		return NullTlfID, InvalidTlfID{s}
	}
	return id, nil
}
