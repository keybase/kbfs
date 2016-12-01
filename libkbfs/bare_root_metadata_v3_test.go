// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"
	"testing"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
)

func TestRootMetadataVersionV3(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	// All V3 objects should have SegregatedKeyBundlesVer.

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	require.Equal(t, SegregatedKeyBundlesVer, rmd.Version())
}

func TestIsValidRekeyRequestBasicV3(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(kbfscodec.NewMsgpack())

	brmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)
	extra := FakeInitialRekey(
		brmd, codec, crypto, bh, kbfscrypto.TLFPublicKey{})

	newBrmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)
	newExtra := FakeInitialRekey(
		newBrmd, codec, crypto, bh, kbfscrypto.TLFPublicKey{})

	ok, err := newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), extra, newExtra)
	require.NoError(t, err)
	// Should fail because the copy bit is unset.
	require.False(t, ok)

	// Set the copy bit; note the writer metadata is the same.
	newBrmd.SetWriterMetadataCopiedBit()

	// There's no internal signature to compare, so this should
	// then work.

	ok, err = newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), extra, newExtra)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRootMetadataPublicVersionV3(t *testing.T) {
	tlfID := tlf.FakeID(1, true)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, []keybase1.UID{keybase1.PublicUID}, nil, nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)
	require.Equal(t, SegregatedKeyBundlesVer, rmd.Version())

	bh2, err := rmd.MakeBareTlfHandle(nil)
	require.Equal(t, bh, bh2)
}

func TestRevokeRemovedDevicesV3(t *testing.T) {
	uid1 := keybase1.MakeTestUID(0x1)
	uid2 := keybase1.MakeTestUID(0x2)
	uid3 := keybase1.MakeTestUID(0x3)

	key1 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key1")
	key2 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key2")
	key3 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key3")

	half1a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x1})
	half2a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x3})
	half3a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x5})

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	id1a, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1.KID(), half1a)
	require.NoError(t, err)
	id2a, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2.KID(), half2a)
	require.NoError(t, err)
	id3a, err := crypto.GetTLFCryptKeyServerHalfID(uid3, key3.KID(), half3a)
	require.NoError(t, err)

	tlfID := tlf.FakeID(1, false)

	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid1, uid2}, []keybase1.UID{uid3}, nil, nil, nil)
	require.NoError(t, err)

	brmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	extra := FakeInitialRekey(
		brmd, codec, crypto, bh, kbfscrypto.TLFPublicKey{})

	wkb, rkb, ok := getKeyBundlesV3(extra)
	require.True(t, ok)

	*wkb = TLFWriterKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{
			uid1: DeviceKeyInfoMapV3{
				key1: TLFCryptKeyInfo{
					ServerHalfID: id1a,
					EPubKeyIndex: 0,
				},
			},
			uid2: DeviceKeyInfoMapV3{
				key2: TLFCryptKeyInfo{
					ServerHalfID: id2a,
					EPubKeyIndex: 0,
				},
			},
		},
	}

	*rkb = TLFReaderKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{
			uid3: DeviceKeyInfoMapV3{
				key3: TLFCryptKeyInfo{
					ServerHalfID: id3a,
					EPubKeyIndex: 0,
				},
			},
		},
	}

	wKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid1: {key1},
	}
	rKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid3: {key3},
	}

	removalInfo, err := brmd.RevokeRemovedDevices(wKeys, rKeys, extra)
	require.NoError(t, err)
	require.Equal(t, ServerHalfRemovalInfo{
		uid2: userServerHalfRemovalInfo{
			userRemoved: true,
			deviceServerHalfIDs: deviceServerHalfRemovalInfo{
				key2: []TLFCryptKeyServerHalfID{id2a},
			},
		},
	}, removalInfo)

	expectedWKB := TLFWriterKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{
			uid1: DeviceKeyInfoMapV3{
				key1: TLFCryptKeyInfo{
					ServerHalfID: id1a,
					EPubKeyIndex: 0,
				},
			},
		},
	}
	require.Equal(t, expectedWKB, *wkb)

	expectedRKB := TLFReaderKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{
			uid3: DeviceKeyInfoMapV3{
				key3: TLFCryptKeyInfo{
					ServerHalfID: id3a,
					EPubKeyIndex: 0,
				},
			},
		},
	}
	require.Equal(t, expectedRKB, *rkb)
}

func testKeyBundleCheckKeysV3(t *testing.T, crypto Crypto, uid keybase1.UID,
	key kbfscrypto.CryptPublicKey, expectedIndex int,
	wkb TLFWriterKeyBundleV3, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverMap serverKeyMap) {
	ctx := context.Background()
	info, ok := wkb.Keys[uid][key]
	require.True(t, ok)
	require.Equal(t, expectedIndex, info.EPubKeyIndex)
	userEPubKey := wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	require.Equal(t, ePubKey, userEPubKey)
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, userEPubKey, info.ClientHalf)
	require.NoError(t, err)
	serverHalf, ok := serverMap[uid][key.KID()]
	require.True(t, ok)
	userTLFCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, tlfCryptKey, userTLFCryptKey)
}

func TestBareRootMetadataV3FillInDevices(t *testing.T) {
	ePubKey1 := kbfscrypto.MakeTLFEphemeralPublicKey([32]byte{0x1})
	// Make a wkb with empty writer key maps
	wkb := TLFWriterKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{},
		TLFEphemeralPublicKeys: kbfscrypto.TLFEphemeralPublicKeys{
			ePubKey1,
		},
	}

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	uid3 := keybase1.MakeTestUID(3)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")
	privKey3 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3")

	wKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid1: []kbfscrypto.CryptPublicKey{privKey1.GetPublicKey()},
		uid2: []kbfscrypto.CryptPublicKey{privKey2.GetPublicKey()},
		uid3: []kbfscrypto.CryptPublicKey{privKey3.GetPublicKey()},
	}

	signingKey1 := kbfscrypto.MakeFakeSigningKeyOrBust("key1")
	signingKey2 := kbfscrypto.MakeFakeSigningKeyOrBust("key2")
	signingKey3 := kbfscrypto.MakeFakeSigningKeyOrBust("key3")

	codec := kbfscodec.NewMsgpack()
	crypto1 := NewCryptoLocal(codec, signingKey1, privKey1)
	crypto2 := NewCryptoLocal(codec, signingKey2, privKey2)
	crypto3 := NewCryptoLocal(codec, signingKey3, privKey3)

	// Fill in the bundle
	_, _, ePubKey, ePrivKey, tlfCryptKey, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)
	var md *BareRootMetadataV3
	serverMap, err := md.fillInDevices(
		crypto1, &wkb, &TLFReaderKeyBundleV3{},
		wKeys, nil, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV3(t, crypto3, uid3, privKey3.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)

	privKey1b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1b")
	wKeys[uid1] = append(wKeys[uid1], privKey1b.GetPublicKey())

	_, _, ePubKey2, ePrivKey2, tlfCryptKey2, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)
	serverMap2, err := md.fillInDevices(
		crypto1, &wkb, &TLFReaderKeyBundleV3{},
		wKeys, nil, ePubKey2, ePrivKey2, tlfCryptKey2)
	require.NoError(t, err)

	crypto1b := NewCryptoLocal(codec, signingKey1, privKey1b)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV3(t, crypto1b, uid1, privKey1b.GetPublicKey(), 2, wkb, ePubKey2, tlfCryptKey2, serverMap2)
	testKeyBundleCheckKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV3(t, crypto3, uid3, privKey3.GetPublicKey(), 1, wkb, ePubKey, tlfCryptKey, serverMap)
}

func testKeyBundleCheckReaderKeysV3(t *testing.T, crypto Crypto, uid keybase1.UID,
	key kbfscrypto.CryptPublicKey, expectedIndex int,
	rkb TLFReaderKeyBundleV3, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverMap serverKeyMap) {
	ctx := context.Background()
	info, ok := rkb.Keys[uid][key]
	require.True(t, ok)
	require.Equal(t, expectedIndex, info.EPubKeyIndex)
	userEPubKey := rkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	require.Equal(t, ePubKey, userEPubKey)
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, userEPubKey, info.ClientHalf)
	require.NoError(t, err)
	serverHalf, ok := serverMap[uid][key.KID()]
	require.True(t, ok)
	userTLFCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, tlfCryptKey, userTLFCryptKey)
}

func TestBareRootMetadataV3FillInDevicesNoExtraKeys(t *testing.T) {
	wkb := TLFWriterKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{},
	}
	rkb := TLFReaderKeyBundleV3{
		Keys: UserDeviceKeyInfoMapV3{},
	}

	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")

	wKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid1: []kbfscrypto.CryptPublicKey{privKey1.GetPublicKey()},
	}

	rKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid2: []kbfscrypto.CryptPublicKey{privKey2.GetPublicKey()},
	}

	signingKey1 := kbfscrypto.MakeFakeSigningKeyOrBust("key1")
	signingKey2 := kbfscrypto.MakeFakeSigningKeyOrBust("key2")

	codec := kbfscodec.NewMsgpack()
	crypto1 := NewCryptoLocal(codec, signingKey1, privKey1)
	crypto2 := NewCryptoLocal(codec, signingKey2, privKey2)

	// Fill in the bundle
	_, _, ePubKey, ePrivKey, tlfCryptKey, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)
	var md *BareRootMetadataV3
	serverMap, err := md.fillInDevices(
		crypto1, &wkb, &rkb, wKeys, rKeys, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 0, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckReaderKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 0, rkb, ePubKey, tlfCryptKey, serverMap)

	serverMap, err = md.fillInDevices(
		crypto1, &wkb, &rkb, wKeys, rKeys, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	require.Equal(t, 1, len(wkb.TLFEphemeralPublicKeys))
	require.Equal(t, 1, len(rkb.TLFEphemeralPublicKeys))
}
