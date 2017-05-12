// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import (
	"encoding"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfshash"
)

// MdID is the content-based ID for a metadata block.
type MdID struct {
	h kbfshash.Hash
}

var _ encoding.BinaryMarshaler = MdID{}
var _ encoding.BinaryUnmarshaler = (*MdID)(nil)

// MdIDFromID creates a new MdID from the given BareRootMetadata object.
//
// TODO: Once BareRootMetadata is moved to this package, change the
// type.
func MdIDFromMD(codec kbfscodec.Codec, md interface{}) (MdID, error) {
	buf, err := codec.Encode(md)
	if err != nil {
		return MdID{}, err
	}

	h, err := kbfshash.DefaultHash(buf)
	if err != nil {
		return MdID{}, err
	}

	return MdID{h}, nil
}

// MdIDFromBytes creates a new MdID from the given bytes. If the
// returned error is nil, the returned MdID is valid.
func MdIDFromBytes(data []byte) (MdID, error) {
	h, err := kbfshash.HashFromBytes(data)
	if err != nil {
		return MdID{}, err
	}
	return MdID{h}, nil
}

// Bytes returns the bytes of the MDID.
func (id MdID) Bytes() []byte {
	return id.h.Bytes()
}

func (id MdID) String() string {
	return id.h.String()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// MdID. Returns an error if the MdID is invalid and not the zero
// MdID.
func (id MdID) MarshalBinary() (data []byte, err error) {
	return id.h.MarshalBinary()
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for MdID. Returns an error if the given byte array is non-empty and
// the MdID is invalid.
func (id *MdID) UnmarshalBinary(data []byte) error {
	return id.h.UnmarshalBinary(data)
}
