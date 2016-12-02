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
// KID of the corresponding device CryptPublicKey) to the
// TLF's symmetric secret key information.
type DeviceKeyInfoMap map[keybase1.KID]TLFCryptKeyInfo

func (kim DeviceKeyInfoMap) fillInDeviceInfo(crypto Crypto,
	uid keybase1.UID, tlfCryptKey kbfscrypto.TLFCryptKey,
	ePrivKey kbfscrypto.TLFEphemeralPrivateKey, ePubIndex int,
	publicKeys []kbfscrypto.CryptPublicKey) (
	serverMap map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf,
	err error) {
	serverMap = make(map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf)
	// for each device:
	//    * create a new random server half
	//    * mask it with the key to get the client half
	//    * encrypt the client half
	//
	// TODO: parallelize
	for _, k := range publicKeys {
		// Skip existing entries, only fill in new ones
		if _, ok := kim[k.KID()]; ok {
			continue
		}

		var serverHalf kbfscrypto.TLFCryptKeyServerHalf
		serverHalf, err = crypto.MakeRandomTLFCryptKeyServerHalf()
		if err != nil {
			return nil, err
		}

		var clientHalf kbfscrypto.TLFCryptKeyClientHalf
		clientHalf, err = crypto.MaskTLFCryptKey(serverHalf, tlfCryptKey)
		if err != nil {
			return nil, err
		}

		var encryptedClientHalf EncryptedTLFCryptKeyClientHalf
		encryptedClientHalf, err =
			crypto.EncryptTLFCryptKeyClientHalf(ePrivKey, k, clientHalf)
		if err != nil {
			return nil, err
		}

		var serverHalfID TLFCryptKeyServerHalfID
		serverHalfID, err =
			crypto.GetTLFCryptKeyServerHalfID(uid, k.KID(), serverHalf)
		if err != nil {
			return nil, err
		}

		kim[k.KID()] = TLFCryptKeyInfo{
			ClientHalf:   encryptedClientHalf,
			ServerHalfID: serverHalfID,
			EPubKeyIndex: ePubIndex,
		}
		serverMap[k.KID()] = serverHalf
	}

	return serverMap, nil
}

func (kim DeviceKeyInfoMap) deepCopy() DeviceKeyInfoMap {
	kimCopy := make(DeviceKeyInfoMap)
	for kid, info := range kim {
		// TODO: This actually still shares some data (in byte
		// slices). Fix that.
		kimCopy[kid] = info
	}
	return kimCopy
}

// UserDeviceKeyInfoMap maps a user's keybase UID to their DeviceKeyInfoMap
type UserDeviceKeyInfoMap map[keybase1.UID]DeviceKeyInfoMap

func (dkim UserDeviceKeyInfoMap) deepCopy() UserDeviceKeyInfoMap {
	dkimCopy := make(UserDeviceKeyInfoMap)
	for uid, kim := range dkim {
		dkimCopy[uid] = kim.deepCopy()
	}
	return dkimCopy
}

type serverKeyMap map[keybase1.UID]map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf
