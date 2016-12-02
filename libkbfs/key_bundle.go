// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/kbfshash"
)

// All section references below are to https://keybase.io/blog/kbfs-crypto
// (version 1.3).

// TLFCryptKeyServerHalfID is the identifier type for a server-side key half.
type TLFCryptKeyServerHalfID struct {
	ID kbfshash.HMAC // Exported for serialization.
}

// String implements the Stringer interface for TLFCryptKeyServerHalfID.
func (id TLFCryptKeyServerHalfID) String() string {
	return id.ID.String()
}

// TLFCryptKeyInfo is a per-device key half entry in the
// TLFWriterKeyBundleV2/TLFReaderKeyBundleV2.
type TLFCryptKeyInfo struct {
	ClientHalf   EncryptedTLFCryptKeyClientHalf
	ServerHalfID TLFCryptKeyServerHalfID
	EPubKeyIndex int `codec:"i,omitempty"`

	codec.UnknownFieldSetHandler
}

// DeviceKeyInfoMap is a map from a user devices (identified by the
// corresponding device CryptPublicKey) to the TLF's symmetric secret
// key information.
type DeviceKeyInfoMap map[keybase1.KID]TLFCryptKeyInfo

// UserDeviceKeyInfoMap maps a user's keybase UID to their DeviceKeyInfoMap
type UserDeviceKeyInfoMap map[keybase1.UID]DeviceKeyInfoMap

type serverKeyMap map[keybase1.UID]map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf
