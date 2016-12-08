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
	id1a, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1, half1a)
	require.NoError(t, err)
	id2a, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2, half2a)
	require.NoError(t, err)
	id3a, err := crypto.GetTLFCryptKeyServerHalfID(uid3, key3, half3a)
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

	wKeys := UserDevicePublicKeys{
		uid1: {key1: true},
	}
	rKeys := UserDevicePublicKeys{
		uid3: {key3: true},
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
	tlfCryptKey kbfscrypto.TLFCryptKey,
	serverHalves UserDeviceKeyServerHalves) {
	ctx := context.Background()
	info, ok := wkb.Keys[uid][key]
	require.True(t, ok)
	require.Equal(t, expectedIndex, info.EPubKeyIndex)
	userEPubKey := wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	require.Equal(t, ePubKey, userEPubKey)
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, userEPubKey, info.ClientHalf)
	require.NoError(t, err)
	serverHalf, ok := serverHalves[uid][key]
	require.True(t, ok)
	userTLFCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, tlfCryptKey, userTLFCryptKey)
}

func TestBareRootMetadataV3FillInDevices(t *testing.T) {
	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	uid3 := keybase1.MakeTestUID(3)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")
	privKey3 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3")

	wKeys := UserDevicePublicKeys{
		uid1: {privKey1.GetPublicKey(): true},
		uid2: {privKey2.GetPublicKey(): true},
		uid3: {privKey3.GetPublicKey(): true},
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

	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid}, nil, []keybase1.SocialAssertion{
			{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	extra, err := rmd.AddKeyGeneration(codec, crypto1, nil,
		kbfscrypto.TLFCryptKey{}, tlfCryptKey,
		kbfscrypto.TLFPublicKey{})
	require.NoError(t, err)

	wkb, _, ok := getKeyBundlesV3(extra)
	require.True(t, ok)

	serverHalves, err := rmd.UpdateKeyGeneration(
		crypto1, FirstValidKeyGen, extra,
		wKeys, nil, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
	testKeyBundleCheckKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
	testKeyBundleCheckKeysV3(t, crypto3, uid3, privKey3.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)

	privKey1b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1b")
	wKeys[uid1][privKey1b.GetPublicKey()] = true

	_, _, ePubKey2, ePrivKey2, tlfCryptKey2, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)
	serverHalves2, err := rmd.UpdateKeyGeneration(
		crypto1, FirstValidKeyGen, extra,
		wKeys, nil, ePubKey2, ePrivKey2, tlfCryptKey2)
	require.NoError(t, err)

	crypto1b := NewCryptoLocal(codec, signingKey1, privKey1b)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
	testKeyBundleCheckKeysV3(t, crypto1b, uid1, privKey1b.GetPublicKey(), 1, *wkb, ePubKey2, tlfCryptKey2, serverHalves2)
	testKeyBundleCheckKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
	testKeyBundleCheckKeysV3(t, crypto3, uid3, privKey3.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
}

func testKeyBundleCheckReaderKeysV3(t *testing.T, crypto Crypto, uid keybase1.UID,
	key kbfscrypto.CryptPublicKey, expectedIndex int,
	rkb TLFReaderKeyBundleV3, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverHalves UserDeviceKeyServerHalves) {
	ctx := context.Background()
	info, ok := rkb.Keys[uid][key]
	require.True(t, ok)
	require.Equal(t, expectedIndex, info.EPubKeyIndex)
	userEPubKey := rkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	require.Equal(t, ePubKey, userEPubKey)
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, userEPubKey, info.ClientHalf)
	require.NoError(t, err)
	serverHalf, ok := serverHalves[uid][key]
	require.True(t, ok)
	userTLFCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, tlfCryptKey, userTLFCryptKey)
}

func TestBareRootMetadataV3FillInDevicesNoExtraKeys(t *testing.T) {
	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")

	wKeys := UserDevicePublicKeys{
		uid1: {privKey1.GetPublicKey(): true},
	}

	rKeys := UserDevicePublicKeys{
		uid2: {privKey2.GetPublicKey(): true},
	}

	signingKey1 := kbfscrypto.MakeFakeSigningKeyOrBust("key1")
	signingKey2 := kbfscrypto.MakeFakeSigningKeyOrBust("key2")

	codec := kbfscodec.NewMsgpack()
	crypto1 := NewCryptoLocal(codec, signingKey1, privKey1)
	crypto2 := NewCryptoLocal(codec, signingKey2, privKey2)

	// Fill in the bundle
	_, _, ePubKey, ePrivKey, tlfCryptKey, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)

	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid}, nil, []keybase1.SocialAssertion{
			{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	extra, err := rmd.AddKeyGeneration(codec, crypto1, nil,
		kbfscrypto.TLFCryptKey{}, tlfCryptKey,
		kbfscrypto.TLFPublicKey{})
	require.NoError(t, err)

	wkb, rkb, ok := getKeyBundlesV3(extra)
	require.True(t, ok)

	serverHalves, err := rmd.UpdateKeyGeneration(
		crypto1, FirstValidKeyGen, extra,
		wKeys, rKeys, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	testKeyBundleCheckKeysV3(t, crypto1, uid1, privKey1.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverHalves)
	testKeyBundleCheckReaderKeysV3(t, crypto2, uid2, privKey2.GetPublicKey(), 0, *rkb, ePubKey, tlfCryptKey, serverHalves)

	serverHalves, err = rmd.UpdateKeyGeneration(
		crypto1, FirstValidKeyGen, extra,
		wKeys, rKeys, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	require.Equal(t, 1, len(wkb.TLFEphemeralPublicKeys))
	require.Equal(t, 1, len(rkb.TLFEphemeralPublicKeys))
}

// expectedRekeyInfoV3 contains all the information needed to check a
// rekey run (that doesn't add a generation).
//
// If writerPrivKeys is empty, then writerEPubKeyIndex is ignored, and
// similarly for readerPrivKeys. If both are empty, then ePubKey is
// also ignored.
type expectedRekeyInfoV3 struct {
	writerPrivKeys, readerPrivKeys         userDevicePrivateKeys
	serverHalves                           UserDeviceKeyServerHalves
	writerEPubKeyIndex, readerEPubKeyIndex int
	ePubKey                                kbfscrypto.TLFEphemeralPublicKey
}

// checkGetTLFCryptKeyV3 checks that wkb and rkb contain the info
// necessary to get the TLF crypt key for each user in expected, which
// must all match expectedTLFCryptKey.
func checkGetTLFCryptKeyV3(t *testing.T, expected expectedRekeyInfoV3,
	expectedTLFCryptKey kbfscrypto.TLFCryptKey,
	wkb *TLFWriterKeyBundleV3, rkb *TLFReaderKeyBundleV3) {
	for uid, privKeys := range expected.writerPrivKeys {
		for privKey := range privKeys {
			pubKey := privKey.GetPublicKey()
			serverHalf, ok := expected.serverHalves[uid][pubKey]
			require.True(t, ok, "writer uid=%s, key=%s",
				uid, pubKey)

			dummySigningKey := kbfscrypto.MakeFakeSigningKeyOrBust("dummy")

			codec := kbfscodec.NewMsgpack()
			crypto := NewCryptoLocal(
				codec, dummySigningKey, privKey)

			info, ok := wkb.Keys[uid][pubKey]
			require.True(t, ok)

			require.Equal(t,
				expected.writerEPubKeyIndex, info.EPubKeyIndex)

			ePubKey := wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
			require.Equal(t, expected.ePubKey, ePubKey)

			ctx := context.Background()
			clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
				ctx, ePubKey, info.ClientHalf)
			require.NoError(t, err)

			tlfCryptKey, err := crypto.UnmaskTLFCryptKey(
				serverHalf, clientHalf)
			require.NoError(t, err)
			require.Equal(t, expectedTLFCryptKey, tlfCryptKey)
		}
	}

	for uid, privKeys := range expected.readerPrivKeys {
		for privKey := range privKeys {
			pubKey := privKey.GetPublicKey()
			serverHalf, ok := expected.serverHalves[uid][pubKey]
			require.True(t, ok, "reader uid=%s, key=%s",
				uid, pubKey)

			dummySigningKey := kbfscrypto.MakeFakeSigningKeyOrBust("dummy")

			codec := kbfscodec.NewMsgpack()
			crypto := NewCryptoLocal(
				codec, dummySigningKey, privKey)

			info, ok := rkb.Keys[uid][pubKey]
			require.True(t, ok)

			require.Equal(t,
				expected.readerEPubKeyIndex, info.EPubKeyIndex)

			ePubKey := rkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
			require.Equal(t, expected.ePubKey, ePubKey)

			ctx := context.Background()
			clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
				ctx, ePubKey, info.ClientHalf)
			require.NoError(t, err)

			tlfCryptKey, err := crypto.UnmaskTLFCryptKey(
				serverHalf, clientHalf)
			require.NoError(t, err)
			require.Equal(t, expectedTLFCryptKey, tlfCryptKey)
		}
	}
}

func userDeviceKeyInfoMapV3ToPublicKeys(udkimV3 UserDeviceKeyInfoMapV3) UserDevicePublicKeys {
	pubKeys := make(UserDevicePublicKeys)
	for uid, dkimV3 := range udkimV3 {
		pubKeys[uid] = make(map[kbfscrypto.CryptPublicKey]bool)
		for key := range dkimV3 {
			pubKeys[uid][key] = true
		}
	}
	return pubKeys
}

// checkKeyBundlesV3 checks that wkb and rkb contain exactly the info
// expected from expectedRekeyInfos and expectedPubKey.
func checkKeyBundlesV3(t *testing.T, expectedRekeyInfos []expectedRekeyInfoV3,
	expectedTLFCryptKey kbfscrypto.TLFCryptKey,
	expectedPubKey kbfscrypto.TLFPublicKey,
	wkb *TLFWriterKeyBundleV3, rkb *TLFReaderKeyBundleV3) {
	expectedWriterPubKeys := make(UserDevicePublicKeys)
	expectedReaderPubKeys := make(UserDevicePublicKeys)
	var expectedWriterEPublicKeys,
		expectedReaderEPublicKeys kbfscrypto.TLFEphemeralPublicKeys
	for _, expected := range expectedRekeyInfos {
		expectedWriterPubKeys = accumulatePublicKeys(
			expectedWriterPubKeys,
			expected.writerPrivKeys.toPublicKeys())
		expectedReaderPubKeys = accumulatePublicKeys(
			expectedReaderPubKeys,
			expected.readerPrivKeys.toPublicKeys())

		if len(expected.writerPrivKeys) > 0 {
			require.Equal(t, expected.writerEPubKeyIndex,
				len(expectedWriterEPublicKeys))
			expectedWriterEPublicKeys = append(
				expectedWriterEPublicKeys,
				expected.ePubKey)
		}

		if len(expected.readerPrivKeys) > 0 {
			require.Equal(t, expected.readerEPubKeyIndex,
				len(expectedReaderEPublicKeys))
			expectedReaderEPublicKeys = append(
				expectedReaderEPublicKeys,
				expected.ePubKey)
		}
	}

	writerPubKeys := userDeviceKeyInfoMapV3ToPublicKeys(wkb.Keys)
	readerPubKeys := userDeviceKeyInfoMapV3ToPublicKeys(rkb.Keys)

	require.Equal(t, expectedWriterPubKeys, writerPubKeys)
	require.Equal(t, expectedReaderPubKeys, readerPubKeys)

	require.Equal(t, expectedWriterEPublicKeys, wkb.TLFEphemeralPublicKeys)
	require.Equal(t, expectedReaderEPublicKeys, rkb.TLFEphemeralPublicKeys)

	require.Equal(t, expectedPubKey, wkb.TLFPublicKey)

	for _, expected := range expectedRekeyInfos {
		expectedUserPubKeys := unionPublicKeyUsers(
			expected.writerPrivKeys.toPublicKeys(),
			expected.readerPrivKeys.toPublicKeys())
		userPubKeys := userDeviceServerHalvesToPublicKeys(
			expected.serverHalves)
		require.Equal(t, expectedUserPubKeys, userPubKeys)
		checkGetTLFCryptKeyV3(t,
			expected, expectedTLFCryptKey, wkb, rkb)
	}
}

func TestBareRootMetadataV3UpdateKeyGeneration(t *testing.T) {
	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	uid3 := keybase1.MakeTestUID(3)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")
	privKey3 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3")

	wKeys := UserDevicePublicKeys{
		uid1: {privKey1.GetPublicKey(): true},
		uid2: {privKey2.GetPublicKey(): true},
	}

	rKeys := UserDevicePublicKeys{
		uid3: {privKey3.GetPublicKey(): true},
	}

	tlfID := tlf.FakeID(1, false)

	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid1, uid2}, []keybase1.UID{uid3},
		[]keybase1.SocialAssertion{{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)

	pubKey, _, ePubKey1, ePrivKey1, tlfCryptKey, err :=
		crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	// Add and update first key generation.

	extra, err := rmd.AddKeyGeneration(codec, crypto, nil,
		kbfscrypto.TLFCryptKey{}, tlfCryptKey, pubKey)
	require.NoError(t, err)

	wkb, rkb, ok := getKeyBundlesV3(extra)
	require.True(t, ok)

	var expectedRekeyInfos []expectedRekeyInfoV3
	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	serverHalves1, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey1, ePrivKey1, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo1 := expectedRekeyInfoV3{
		writerPrivKeys: userDevicePrivateKeys{
			uid1: {privKey1: true},
			uid2: {privKey2: true},
		},
		readerPrivKeys: userDevicePrivateKeys{
			uid3: {privKey3: true},
		},
		serverHalves:       serverHalves1,
		writerEPubKeyIndex: 0,
		readerEPubKeyIndex: 0,
		ePubKey:            ePubKey1,
	}
	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo1)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Do again to check idempotency.

	serverHalves1b, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey1, ePrivKey1, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo1b := expectedRekeyInfoV3{
		serverHalves: serverHalves1b,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo1b)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Rekey.

	privKey1b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1b")
	wKeys[uid1][privKey1b.GetPublicKey()] = true

	privKey3b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3b")
	rKeys[uid3][privKey3b.GetPublicKey()] = true

	_, _, ePubKey2, ePrivKey2, _, err := crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	serverHalves2, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey2, ePrivKey2, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo2 := expectedRekeyInfoV3{
		writerPrivKeys: userDevicePrivateKeys{
			uid1: {privKey1b: true},
		},
		readerPrivKeys: userDevicePrivateKeys{
			uid3: {privKey3b: true},
		},
		serverHalves:       serverHalves2,
		writerEPubKeyIndex: 1,
		readerEPubKeyIndex: 1,
		ePubKey:            ePubKey2,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo2)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Do again to check idempotency.

	serverHalves2b, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey2, ePrivKey2, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo2b := expectedRekeyInfoV3{
		serverHalves: serverHalves2b,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo2b)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Rekey writers only.

	privKey1c := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1c")
	wKeys[uid1][privKey1c.GetPublicKey()] = true

	_, _, ePubKey3, ePrivKey3, _, err := crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	serverHalves3, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey3, ePrivKey3, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo3 := expectedRekeyInfoV3{
		writerPrivKeys: userDevicePrivateKeys{
			uid1: {privKey1c: true},
		},
		readerPrivKeys:     nil,
		serverHalves:       serverHalves3,
		writerEPubKeyIndex: 2,
		readerEPubKeyIndex: -1,
		ePubKey:            ePubKey3,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo3)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Do again to check idempotency.

	serverHalves3b, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey3, ePrivKey3, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo3b := expectedRekeyInfoV3{
		serverHalves: serverHalves3b,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo3b)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Reader rekey.

	privKey3c := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3c")
	privKey3d := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3d")
	rKeys[uid3][privKey3c.GetPublicKey()] = true
	rKeys[uid3][privKey3d.GetPublicKey()] = true

	_, _, ePubKey4, ePrivKey4, _, err := crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	rKeysReader := UserDevicePublicKeys{
		uid3: rKeys[uid3],
	}
	serverHalves4, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, nil, rKeysReader, ePubKey4, ePrivKey4, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo4 := expectedRekeyInfoV3{
		writerPrivKeys: nil,
		readerPrivKeys: userDevicePrivateKeys{
			uid3: {privKey3c: true, privKey3d: true},
		},
		serverHalves:       serverHalves4,
		writerEPubKeyIndex: -1,
		readerEPubKeyIndex: 2,
		ePubKey:            ePubKey4,
	}
	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo4)
	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)

	// Do again to check idempotency.

	serverHalves4b, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, nil, rKeysReader, ePubKey4, ePrivKey4, tlfCryptKey)
	require.NoError(t, err)

	expectedRekeyInfo4b := expectedRekeyInfoV3{
		serverHalves: serverHalves4b,
	}

	expectedRekeyInfos = append(expectedRekeyInfos, expectedRekeyInfo4b)

	checkKeyBundlesV3(t, expectedRekeyInfos, tlfCryptKey, pubKey, wkb, rkb)
}
