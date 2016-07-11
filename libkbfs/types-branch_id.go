// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding"
	"encoding/hex"
	"errors"
)

const (
	// BranchIDByteLen is the number of bytes in a per-device per-TLF branch ID.
	IFCERFTBranchIDByteLen = 16
	// BranchIDStringLen is the number of characters in the string
	// representation of a per-device pr-TLF branch ID.
	IFCERFTBranchIDStringLen = 2 * IFCERFTBranchIDByteLen
)

// BranchID encapsulates a per-device per-TLF branch ID.
type IFCERFTBranchID struct {
	id [IFCERFTBranchIDByteLen]byte
}

var _ encoding.BinaryMarshaler = (*IFCERFTBranchID)(nil)
var _ encoding.BinaryUnmarshaler = (*IFCERFTBranchID)(nil)

// NullBranchID is an empty BranchID
var IFCERFTNullBranchID = IFCERFTBranchID{}

// Bytes returns the bytes of the BranchID.
func (id IFCERFTBranchID) Bytes() []byte {
	return id.id[:]
}

// String implements the Stringer interface for BranchID.
func (id IFCERFTBranchID) String() string {
	return hex.EncodeToString(id.id[:])
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for BranchID.
func (id IFCERFTBranchID) MarshalBinary() (data []byte, err error) {
	return id.id[:], nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for BranchID.
func (id *IFCERFTBranchID) UnmarshalBinary(data []byte) error {
	if len(data) != IFCERFTBranchIDByteLen {
		return errors.New("invalid BranchID")
	}
	copy(id.id[:], data)
	return nil
}

// ParseBranchID parses a hex encoded BranchID. Returns NullBranchID on failure.
func IFCERFTParseBranchID(s string) IFCERFTBranchID {
	if len(s) != IFCERFTBranchIDStringLen {
		return IFCERFTNullBranchID
	}
	bytes, err := hex.DecodeString(s)
	if err != nil {
		return IFCERFTNullBranchID
	}
	var id IFCERFTBranchID
	err = id.UnmarshalBinary(bytes)
	if err != nil {
		id = IFCERFTNullBranchID
	}
	return id
}
