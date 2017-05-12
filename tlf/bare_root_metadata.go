// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

// BareRootMetadata is a read-only interface to the bare serializeable MD that
// is signed by the reader or writer.
//
// TODO: Move the rest of libkbfs.BareRootMetadata here.
type BareRootMetadata interface {
	// TlfID returns the ID of the TLF this BareRootMetadata is for.
	TlfID() ID
	// GetSerializedPrivateMetadata returns the serialized private metadata as a byte slice.
	GetSerializedPrivateMetadata() []byte
}
