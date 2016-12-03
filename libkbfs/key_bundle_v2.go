// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"golang.org/x/net/context"
)

// DeviceKeyInfoMapV2 is a map from a user devices (identified by the
// KID of the corresponding device CryptPublicKey) to the
// TLF's symmetric secret key information.
type DeviceKeyInfoMapV2 map[keybase1.KID]TLFCryptKeyInfo

func (dkimV2 DeviceKeyInfoMapV2) fillInDeviceInfo(crypto Crypto,
	uid keybase1.UID, tlfCryptKey kbfscrypto.TLFCryptKey,
	ePrivKey kbfscrypto.TLFEphemeralPrivateKey, ePubIndex int,
	publicKeys []kbfscrypto.CryptPublicKey) (
	serverMap map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf,
	err error) {
	serverMap = make(map[keybase1.KID]kbfscrypto.TLFCryptKeyServerHalf)
	// TODO: parallelize
	for _, k := range publicKeys {
		// Skip existing entries, and only fill in new ones.
		if _, ok := dkimV2[k.KID()]; ok {
			continue
		}

		clientInfo, serverHalf, err := splitTLFCryptKey(
			crypto, uid, tlfCryptKey, ePrivKey, ePubIndex, k)
		if err != nil {
			return nil, err
		}

		dkimV2[k.KID()] = clientInfo
		serverMap[k.KID()] = serverHalf
	}

	return serverMap, nil
}

func (dkimV2 DeviceKeyInfoMapV2) toDKIM(codec kbfscodec.Codec) (
	DeviceKeyInfoMap, error) {
	dkim := make(DeviceKeyInfoMap)
	for kid, info := range dkimV2 {
		var infoCopy TLFCryptKeyInfo
		err := kbfscodec.Update(codec, &infoCopy, info)
		if err != nil {
			return nil, err
		}
		dkim[kbfscrypto.MakeCryptPublicKey(kid)] = infoCopy
	}
	return dkim, nil
}

func dkimToV2(codec kbfscodec.Codec, dkim DeviceKeyInfoMap) (
	DeviceKeyInfoMapV2, error) {
	dkimV2 := make(DeviceKeyInfoMapV2)
	for key, info := range dkim {
		var infoCopy TLFCryptKeyInfo
		err := kbfscodec.Update(codec, &infoCopy, info)
		if err != nil {
			return nil, err
		}
		dkimV2[key.KID()] = infoCopy
	}
	return dkimV2, nil
}

// UserDeviceKeyInfoMapV2 maps a user's keybase UID to their DeviceKeyInfoMapV2
type UserDeviceKeyInfoMapV2 map[keybase1.UID]DeviceKeyInfoMapV2

func (udkimV2 UserDeviceKeyInfoMapV2) toUDKIM(
	codec kbfscodec.Codec) (UserDeviceKeyInfoMap, error) {
	udkim := make(UserDeviceKeyInfoMap)
	for u, dkimV2 := range udkimV2 {
		dkim, err := dkimV2.toDKIM(codec)
		if err != nil {
			return nil, err
		}
		udkim[u] = dkim
	}
	return udkim, nil
}

func udkimToV2(codec kbfscodec.Codec, udkim UserDeviceKeyInfoMap) (
	UserDeviceKeyInfoMapV2, error) {
	udkimV2 := make(UserDeviceKeyInfoMapV2)
	for u, dkim := range udkim {
		dkimV2, err := dkimToV2(codec, dkim)
		if err != nil {
			return nil, err
		}
		udkimV2[u] = dkimV2
	}
	return udkimV2, nil
}

// All section references below are to https://keybase.io/blog/kbfs-crypto
// (version 1.3).

// TLFWriterKeyBundleV2 is a bundle of all the writer keys for a top-level
// folder.
type TLFWriterKeyBundleV2 struct {
	// Maps from each writer to their crypt key bundle.
	WKeys UserDeviceKeyInfoMapV2

	// M_f as described in 4.1.1 of https://keybase.io/blog/kbfs-crypto.
	TLFPublicKey kbfscrypto.TLFPublicKey `codec:"pubKey"`

	// M_e as described in 4.1.1 of https://keybase.io/blog/kbfs-crypto.
	// Because devices can be added into the key generation after it
	// is initially created (so those devices can get access to
	// existing data), we track multiple ephemeral public keys; the
	// one used by a particular device is specified by EPubKeyIndex in
	// its TLFCryptoKeyInfo struct.
	TLFEphemeralPublicKeys kbfscrypto.TLFEphemeralPublicKeys `codec:"ePubKey"`

	codec.UnknownFieldSetHandler
}

// IsWriter returns true if the given user device is in the writer set.
func (tkb TLFWriterKeyBundleV2) IsWriter(user keybase1.UID, deviceKID keybase1.KID) bool {
	_, ok := tkb.WKeys[user][deviceKID]
	return ok
}

// TLFWriterKeyGenerations stores a slice of TLFWriterKeyBundleV2,
// where the last element is the current generation.
type TLFWriterKeyGenerations []TLFWriterKeyBundleV2

// LatestKeyGeneration returns the current key generation for this TLF.
func (tkg TLFWriterKeyGenerations) LatestKeyGeneration() KeyGen {
	return KeyGen(len(tkg))
}

// IsWriter returns whether or not the user+device is an authorized writer
// for the latest generation.
func (tkg TLFWriterKeyGenerations) IsWriter(user keybase1.UID, deviceKID keybase1.KID) bool {
	keyGen := tkg.LatestKeyGeneration()
	if keyGen < 1 {
		return false
	}
	return tkg[keyGen-1].IsWriter(user, deviceKID)
}

// ToTLFWriterKeyBundleV3 converts a TLFWriterKeyGenerations to a TLFWriterKeyBundleV3.
func (tkg TLFWriterKeyGenerations) ToTLFWriterKeyBundleV3(
	ctx context.Context, codec kbfscodec.Codec, crypto cryptoPure, keyManager KeyManager, kmd KeyMetadata) (
	*TLFWriterKeyBundleV3, error) {

	keyGen := tkg.LatestKeyGeneration()
	if keyGen < FirstValidKeyGen {
		return nil, errors.New("No key generations to convert")
	}

	wkbV3 := &TLFWriterKeyBundleV3{}

	// Copy the latest UserDeviceKeyInfoMap.
	wkb := tkg[keyGen-FirstValidKeyGen]
	udkim, err := wkb.WKeys.toUDKIM(codec)
	if err != nil {
		return nil, err
	}
	wkbV3.Keys = userDeviceKeyInfoMapToV3(udkim)

	// Copy all of the TLFEphemeralPublicKeys at this generation.
	wkbV3.TLFEphemeralPublicKeys =
		make(kbfscrypto.TLFEphemeralPublicKeys, len(wkb.TLFEphemeralPublicKeys))
	copy(wkbV3.TLFEphemeralPublicKeys[:], wkb.TLFEphemeralPublicKeys)

	// Copy the current TLFPublicKey.
	wkbV3.TLFPublicKey = wkb.TLFPublicKey

	if keyGen > FirstValidKeyGen {
		// Fetch all of the TLFCryptKeys.
		keys, err := keyManager.GetTLFCryptKeyOfAllGenerations(ctx, kmd)
		if err != nil {
			return nil, err
		}
		// Sanity check.
		if len(keys) != int(keyGen) {
			return nil, fmt.Errorf("expected %d keys, found %d", keyGen, len(keys))
		}
		// Save the current key.
		currKey := keys[len(keys)-1]
		// Get rid of the most current generation as that's in the UserDeviceKeyInfoMap already.
		keys = keys[:len(keys)-1]
		// Encrypt the historic keys with the current key.
		wkbV3.EncryptedHistoricTLFCryptKeys, err = crypto.EncryptTLFCryptKeys(keys, currKey)
		if err != nil {
			return nil, err
		}
	}

	return wkbV3, nil
}

// TLFReaderKeyBundleV2 stores all the user keys with reader
// permissions on a TLF
type TLFReaderKeyBundleV2 struct {
	RKeys UserDeviceKeyInfoMapV2

	// M_e as described in 4.1.1 of https://keybase.io/blog/kbfs-crypto.
	// Because devices can be added into the key generation after it
	// is initially created (so those devices can get access to
	// existing data), we track multiple ephemeral public keys; the
	// one used by a particular device is specified by EPubKeyIndex in
	// its TLFCryptoKeyInfo struct.
	// This list is needed so a reader rekey doesn't modify the writer
	// metadata.
	TLFReaderEphemeralPublicKeys kbfscrypto.TLFEphemeralPublicKeys `codec:"readerEPubKey,omitempty"`

	codec.UnknownFieldSetHandler
}

// IsReader returns true if the given user device is in the reader set.
func (trb TLFReaderKeyBundleV2) IsReader(user keybase1.UID, deviceKID keybase1.KID) bool {
	_, ok := trb.RKeys[user][deviceKID]
	return ok
}

// TLFReaderKeyGenerations stores a slice of TLFReaderKeyBundleV2,
// where the last element is the current generation.
type TLFReaderKeyGenerations []TLFReaderKeyBundleV2

// LatestKeyGeneration returns the current key generation for this TLF.
func (tkg TLFReaderKeyGenerations) LatestKeyGeneration() KeyGen {
	return KeyGen(len(tkg))
}

// IsReader returns whether or not the user+device is an authorized reader
// for the latest generation.
func (tkg TLFReaderKeyGenerations) IsReader(user keybase1.UID, deviceKID keybase1.KID) bool {
	keyGen := tkg.LatestKeyGeneration()
	if keyGen < 1 {
		return false
	}
	return tkg[keyGen-1].IsReader(user, deviceKID)
}

// ToTLFReaderKeyBundleV3 converts a TLFReaderKeyGenerations to a TLFReaderkeyBundleV3.
func (tkg TLFReaderKeyGenerations) ToTLFReaderKeyBundleV3(wkb *TLFWriterKeyBundleV3) (
	*TLFReaderKeyBundleV3, error) {

	keyGen := tkg.LatestKeyGeneration()
	if keyGen < 1 {
		return nil, errors.New("No key generations to convert")
	}

	rkbV3 := &TLFReaderKeyBundleV3{
		Keys: make(UserDeviceKeyInfoMapV3),
	}

	// Copy the latest UserDeviceKeyInfoMap.
	rkb := tkg[keyGen-FirstValidKeyGen]

	// Copy all of the TLFReaderEphemeralPublicKeys.
	copy(rkbV3.TLFEphemeralPublicKeys[:], rkb.TLFReaderEphemeralPublicKeys)

	// Track a mapping of old writer ephemeral pubkey index to new
	// reader ephemeral pubkey index.
	pubKeyIndicesMap := make(map[int]int)

	// We need to copy these in a slightly annoying way to work around
	// the negative index hack. In V3 readers always have their ePubKey
	// in the TLFReaderEphemeralPublicKeys list. In V2 they only do if
	// the index is negative. Otherwise it's in the writer's list.
	for uid, dkim := range rkb.RKeys {
		dkimV3 := make(DeviceKeyInfoMapV3)
		for kid, info := range dkim {
			if info.EPubKeyIndex < 0 {
				// Convert to the real index in the reader list.
				newIndex := -1 - info.EPubKeyIndex
				info.EPubKeyIndex = newIndex
			} else {
				oldIndex := info.EPubKeyIndex
				if oldIndex >= len(wkb.TLFEphemeralPublicKeys) {
					err := fmt.Errorf("Invalid index %d (len: %d)",
						oldIndex, len(wkb.TLFEphemeralPublicKeys))
					return nil, err
				}
				// Map the old index in the writer list to a new index
				// at the end of the reader list.
				if newIndex, ok := pubKeyIndicesMap[oldIndex]; !ok {
					ePubKey := wkb.TLFEphemeralPublicKeys[oldIndex]
					rkbV3.TLFEphemeralPublicKeys =
						append(rkbV3.TLFEphemeralPublicKeys, ePubKey)
					newIndex = len(rkbV3.TLFEphemeralPublicKeys) - 1
					pubKeyIndicesMap[oldIndex] = newIndex
					info.EPubKeyIndex = newIndex
				} else {
					info.EPubKeyIndex = newIndex
				}
			}
			dkimV3[kbfscrypto.MakeCryptPublicKey(kid)] = info
		}
		rkbV3.Keys[uid] = dkimV3
	}
	return rkbV3, nil
}

func fillInDevicesAndServerMapV2(crypto Crypto, newIndex int,
	cryptKeys map[keybase1.UID][]kbfscrypto.CryptPublicKey,
	keyInfoMap UserDeviceKeyInfoMapV2,
	ePubKey kbfscrypto.TLFEphemeralPublicKey,
	ePrivKey kbfscrypto.TLFEphemeralPrivateKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, newServerKeys serverKeyMap) error {
	for u, keys := range cryptKeys {
		if _, ok := keyInfoMap[u]; !ok {
			keyInfoMap[u] = DeviceKeyInfoMapV2{}
		}

		serverMap, err := keyInfoMap[u].fillInDeviceInfo(
			crypto, u, tlfCryptKey, ePrivKey, newIndex, keys)
		if err != nil {
			return err
		}
		if len(serverMap) > 0 {
			newServerKeys[u] = serverMap
		}
	}
	return nil
}

func (udkimV2 UserDeviceKeyInfoMapV2) removeDevicesNotIn(
	keys map[keybase1.UID][]kbfscrypto.CryptPublicKey) ServerHalfRemovalInfo {
	removalInfo := make(ServerHalfRemovalInfo)
	for uid, dkim := range udkimV2 {
		userKIDs := make(map[keybase1.KID]bool)
		for _, key := range keys[uid] {
			userKIDs[key.KID()] = true
		}

		deviceServerHalfIDs := make(map[kbfscrypto.CryptPublicKey][]TLFCryptKeyServerHalfID)

		for kid, info := range dkim {
			if !userKIDs[kid] {
				delete(dkim, kid)
				key := kbfscrypto.MakeCryptPublicKey(kid)
				deviceServerHalfIDs[key] = append(
					deviceServerHalfIDs[key],
					info.ServerHalfID)
			}
		}

		userRemoved := false
		if len(dkim) == 0 {
			// The user was completely removed, which
			// shouldn't happen but might as well make it
			// work just in case.
			delete(udkimV2, uid)
			userRemoved = true
		}

		removalInfo[uid] = userServerHalfRemovalInfo{
			userRemoved:         userRemoved,
			deviceServerHalfIDs: deviceServerHalfIDs,
		}
	}

	return removalInfo
}
