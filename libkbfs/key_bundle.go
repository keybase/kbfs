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

// TODO: UserDeviceKeyInfoMap and DeviceKeyInfoMap exist only because
// of BareRootMetadata.GetUserDeviceKeyInfoMaps. That will eventually
// go away, so remove these types once that happens.

// DeviceKeyInfoMap is a map from a user devices (identified by the
// corresponding device CryptPublicKey) to the TLF's symmetric secret
// key information.
type DeviceKeyInfoMap map[kbfscrypto.CryptPublicKey]TLFCryptKeyInfo

// UserDeviceKeyInfoMap maps a user's keybase UID to their DeviceKeyInfoMap
type UserDeviceKeyInfoMap map[keybase1.UID]DeviceKeyInfoMap

type serverKeyMap map[keybase1.UID]map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf

func splitTLFCryptKey(crypto Crypto, uid keybase1.UID,
	tlfCryptKey kbfscrypto.TLFCryptKey,
	ePrivKey kbfscrypto.TLFEphemeralPrivateKey, ePubIndex int,
	pubKey kbfscrypto.CryptPublicKey) (
	TLFCryptKeyInfo, kbfscrypto.TLFCryptKeyServerHalf, error) {
	var serverHalf kbfscrypto.TLFCryptKeyServerHalf
	serverHalf, err := crypto.MakeRandomTLFCryptKeyServerHalf()
	if err != nil {
		return TLFCryptKeyInfo{}, kbfscrypto.TLFCryptKeyServerHalf{}, err
	}

	var clientHalf kbfscrypto.TLFCryptKeyClientHalf
	clientHalf, err = crypto.MaskTLFCryptKey(serverHalf, tlfCryptKey)
	if err != nil {
		return TLFCryptKeyInfo{}, kbfscrypto.TLFCryptKeyServerHalf{}, err
	}

	var encryptedClientHalf EncryptedTLFCryptKeyClientHalf
	encryptedClientHalf, err =
		crypto.EncryptTLFCryptKeyClientHalf(ePrivKey, pubKey, clientHalf)
	if err != nil {
		return TLFCryptKeyInfo{}, kbfscrypto.TLFCryptKeyServerHalf{}, err
	}

	var serverHalfID TLFCryptKeyServerHalfID
	serverHalfID, err =
		crypto.GetTLFCryptKeyServerHalfID(uid, pubKey.KID(), serverHalf)
	if err != nil {
		return TLFCryptKeyInfo{}, kbfscrypto.TLFCryptKeyServerHalf{}, err
	}

	clientInfo := TLFCryptKeyInfo{
		ClientHalf:   encryptedClientHalf,
		ServerHalfID: serverHalfID,
		EPubKeyIndex: ePubIndex,
	}
	return clientInfo, serverHalf, nil
}

type userServerHalfRemovalInfo struct {
	userRemoved         bool
	deviceServerHalfIDs map[kbfscrypto.CryptPublicKey][]TLFCryptKeyServerHalfID
}

// ServerHalfRemovalInfo is a map from users and devices to a list of
// server half IDs to remove from the server.
type ServerHalfRemovalInfo map[keybase1.UID]userServerHalfRemovalInfo

func (info ServerHalfRemovalInfo) merge(
	other ServerHalfRemovalInfo) ServerHalfRemovalInfo {
	u := make(ServerHalfRemovalInfo)
	for uid, otherUserRemovalInfo := range other {
		userRemovalInfo := info[uid]
		if userRemovalInfo.deviceServerHalfIDs == nil {
			userRemovalInfo.deviceServerHalfIDs = make(
				map[kbfscrypto.CryptPublicKey][]TLFCryptKeyServerHalfID)
		}
		userRemovalInfo.userRemoved =
			userRemovalInfo.userRemoved ||
				otherUserRemovalInfo.userRemoved
		for key, serverHalfIDs := range otherUserRemovalInfo.deviceServerHalfIDs {
			userRemovalInfo.deviceServerHalfIDs[key] = append(
				userRemovalInfo.deviceServerHalfIDs[key],
				serverHalfIDs...)
		}
	}
	return u
}
