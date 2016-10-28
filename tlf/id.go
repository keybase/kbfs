// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import (
	"encoding"
	"encoding/hex"
	"encoding/json"
)

const (
	// IDByteLen is the number of bytes in a top-level folder ID
	IDByteLen = 16
	// IDStringLen is the number of characters in the string
	// representation of a top-level folder ID
	IDStringLen = 2 * IDByteLen
	// IDSuffix is the last byte of a private top-level folder ID
	IDSuffix = 0x16
	// PubIDSuffix is the last byte of a public top-level folder ID
	PubIDSuffix = 0x17
)

// ID is a top-level folder ID
type ID struct {
	id [IDByteLen]byte
}

var _ encoding.BinaryMarshaler = ID{}
var _ encoding.BinaryUnmarshaler = (*ID)(nil)

var _ json.Marshaler = ID{}
var _ json.Unmarshaler = (*ID)(nil)

// NullID is an empty ID
var NullID = ID{}

// Bytes returns the bytes of the TLF ID.
func (id ID) Bytes() []byte {
	return id.id[:]
}

// String implements the fmt.Stringer interface for ID.
func (id ID) String() string {
	return hex.EncodeToString(id.id[:])
}

// MarshalBinary implements the encoding.BinaryMarshaler interface for ID.
func (id ID) MarshalBinary() (data []byte, err error) {
	suffix := id.id[IDByteLen-1]
	if suffix != IDSuffix && suffix != PubIDSuffix {
		return nil, InvalidID{id.String()}
	}
	return id.id[:], nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
// for ID.
func (id *ID) UnmarshalBinary(data []byte) error {
	if len(data) != IDByteLen {
		return InvalidID{hex.EncodeToString(data)}
	}
	suffix := data[IDByteLen-1]
	if suffix != IDSuffix && suffix != PubIDSuffix {
		return InvalidID{hex.EncodeToString(data)}
	}
	copy(id.id[:], data)
	return nil
}

// MarshalJSON implements the encoding.json.Marshaler interface for
// ID.
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON implements the encoding.json.Unmarshaler interface
// for ID.
func (id *ID) UnmarshalJSON(buf []byte) error {
	var str string
	err := json.Unmarshal(buf, &str)
	if err != nil {
		return err
	}
	newID, err := ParseID(str)
	if err != nil {
		return err
	}
	*id = newID
	return nil
}

// IsPublic returns true if this ID is for a public top-level folder
func (id ID) IsPublic() bool {
	return id.id[IDByteLen-1] == PubIDSuffix
}

// ParseID parses a hex encoded ID. Returns NullID and an
// InvalidID on failure.
func ParseID(s string) (ID, error) {
	if len(s) != IDStringLen {
		return NullID, InvalidID{s}
	}
	bytes, err := hex.DecodeString(s)
	if err != nil {
		return NullID, InvalidID{s}
	}
	var id ID
	err = id.UnmarshalBinary(bytes)
	if err != nil {
		return NullID, InvalidID{s}
	}
	return id, nil
}
