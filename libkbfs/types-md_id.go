// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "encoding"

// MdID is the content-based ID for a metadata block.
type IFCERFTMdID struct {
	h IFCERFTHash
}

var _ encoding.BinaryMarshaler = IFCERFTMdID{}
var _ encoding.BinaryUnmarshaler = (*IFCERFTMdID)(nil)

// MdIDFromBytes creates a new MdID from the given bytes. If the
// returned error is nil, the returned MdID is valid.
func IFCERFTMdIDFromBytes(data []byte) (IFCERFTMdID, error) {
	h, err := IFCERFTHashFromBytes(data)
	if err != nil {
		return IFCERFTMdID{}, err
	}
	return IFCERFTMdID{h}, nil
}

// Bytes returns the bytes of the MDID.
func (id IFCERFTMdID) Bytes() []byte {
	return id.h.Bytes()
}

func (id IFCERFTMdID) String() string {
	return id.h.String()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// MdID. Returns an error if the MdID is invalid and not the zero
// MdID.
func (id IFCERFTMdID) MarshalBinary() (data []byte, err error) {
	return id.h.MarshalBinary()
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for MdID. Returns an error if the given byte array is non-empty and
// the MdID is invalid.
func (id *IFCERFTMdID) UnmarshalBinary(data []byte) error {
	return id.h.UnmarshalBinary(data)
}
