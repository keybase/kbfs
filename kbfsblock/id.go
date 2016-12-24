// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsblock

import (
	"encoding"

	"github.com/keybase/kbfs/kbfshash"
)

// ID is the (usually content-based) ID for a data block.
type ID struct {
	h kbfshash.Hash
}

var _ encoding.BinaryMarshaler = ID{}
var _ encoding.BinaryUnmarshaler = (*ID)(nil)

var _ encoding.TextMarshaler = ID{}
var _ encoding.TextUnmarshaler = (*ID)(nil)

// MaxIDStringLength is the maximum length of the string
// representation of a ID.
const MaxIDStringLength = kbfshash.MaxHashStringLength

// IDFromString creates a ID from the given string. If the
// returned error is nil, the returned ID is valid.
func IDFromString(dataStr string) (ID, error) {
	h, err := kbfshash.HashFromString(dataStr)
	if err != nil {
		return ID{}, err
	}
	return ID{h}, nil
}

// IsValid returns whether the block ID is valid. A zero block ID is
// considered invalid.
func (id ID) IsValid() bool {
	return id.h.IsValid()
}

// Bytes returns the bytes of the block ID.
func (id ID) Bytes() []byte {
	return id.h.Bytes()
}

func (id ID) String() string {
	return id.h.String()
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for
// ID. Returns an error if the ID is invalid and not the zero
// ID.
func (id ID) MarshalBinary() (data []byte, err error) {
	return id.h.MarshalBinary()
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for ID. Returns an error if the given byte array is non-empty and
// the ID is invalid.
func (id *ID) UnmarshalBinary(data []byte) error {
	return id.h.UnmarshalBinary(data)
}

// MarshalText implements the encoding.TextMarshaler interface for ID.
func (id ID) MarshalText() ([]byte, error) {
	return id.h.MarshalText()
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for
// ID.
func (id *ID) UnmarshalText(buf []byte) error {
	return id.h.UnmarshalText(buf)
}
