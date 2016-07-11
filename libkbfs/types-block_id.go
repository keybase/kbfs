// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "encoding"

// BlockID is the (usually content-based) ID for a data block.
type IFCERFTBlockID struct {
	h IFCERFTHash
}

var _ encoding.BinaryMarshaler = IFCERFTBlockID{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTBlockID)(nil)

// MaxBlockIDStringLength is the maximum length of the string
// representation of a BlockID.
const IFCERFTMaxBlockIDStringLength = IFCERFTMaxHashStringLength

// BlockIDFromString creates a BlockID from the given string. If the
// returned error is nil, the returned BlockID is valid.
func IFCERFTBlockIDFromString(dataStr string) (IFCERFTBlockID, error) {
	h, err := IFCERFTHashFromString(dataStr)
	if err != nil {
		return IFCERFTBlockID{}, err
	}
	return IFCERFTBlockID{h}, nil
}

// IsValid returns whether the block ID is valid. A zero block ID is
// considered invalid.
func (id IFCERFTBlockID) IsValid() bool {
	return id.h.IsValid()
}

// Bytes returns the bytes of the block ID.
func (id IFCERFTBlockID) Bytes() []byte {
	return id.h.Bytes()
}

func (id IFCERFTBlockID) String() string {
	return id.h.String()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// BlockID. Returns an error if the BlockID is invalid and not the zero
// BlockID.
func (id IFCERFTBlockID) MarshalBinary() (data []byte, err error) {
	return id.h.MarshalBinary()
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for BlockID. Returns an error if the given byte array is non-empty and
// the BlockID is invalid.
func (id *IFCERFTBlockID) UnmarshalBinary(data []byte) error {
	return id.h.UnmarshalBinary(data)
}
