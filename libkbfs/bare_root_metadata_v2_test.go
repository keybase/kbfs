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

func TestBareRootMetadataVersionV2(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	// Metadata objects with unresolved assertions should have
	// InitialExtraMetadataVer.

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid}, nil, []keybase1.SocialAssertion{
			keybase1.SocialAssertion{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	require.Equal(t, InitialExtraMetadataVer, rmd.Version())

	// All other folders should use PreExtraMetadataVer.
	bh2, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	rmd2, err := MakeInitialBareRootMetadata(
		InitialExtraMetadataVer, tlfID, bh2)
	require.NoError(t, err)

	require.Equal(t, PreExtraMetadataVer, rmd2.Version())

	// ... including if unresolved assertions get resolved.

	rmd.SetUnresolvedWriters(nil)
	require.Equal(t, PreExtraMetadataVer, rmd.Version())
}

func TestIsValidRekeyRequestBasicV2(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	brmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	ctx := context.Background()
	codec := kbfscodec.NewMsgpack()
	signer := kbfscrypto.SigningKeySigner{
		Key: kbfscrypto.MakeFakeSigningKeyOrBust("key1"),
	}

	err = brmd.SignWriterMetadataInternally(ctx, codec, signer)
	require.NoError(t, err)

	newBrmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)
	ok, err := newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), nil, nil)
	require.NoError(t, err)
	// Should fail because the copy bit is unset.
	require.False(t, ok)

	// Set the copy bit; note the writer metadata is the same.
	newBrmd.SetWriterMetadataCopiedBit()

	signer2 := kbfscrypto.SigningKeySigner{
		Key: kbfscrypto.MakeFakeSigningKeyOrBust("key2"),
	}

	err = newBrmd.SignWriterMetadataInternally(ctx, codec, signer2)
	require.NoError(t, err)

	ok, err = newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), nil, nil)
	require.NoError(t, err)
	// Should fail because of mismatched writer metadata siginfo.
	require.False(t, ok)

	// Re-sign to get the same signature.
	err = newBrmd.SignWriterMetadataInternally(ctx, codec, signer)
	require.NoError(t, err)
	ok, err = newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), nil, nil)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRevokeRemovedDevicesV2(t *testing.T) {
	uid1 := keybase1.MakeTestUID(0x1)
	uid2 := keybase1.MakeTestUID(0x2)
	uid3 := keybase1.MakeTestUID(0x3)

	key1 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key1")
	key2 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key2")
	key3 := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key3")

	half1a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x1})
	half1b := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x2})
	half2a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x3})
	half2b := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x4})
	half3a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x5})
	half3b := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x6})

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	id1a, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1.KID(), half1a)
	require.NoError(t, err)
	id1b, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1.KID(), half1b)
	require.NoError(t, err)
	id2a, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2.KID(), half2a)
	require.NoError(t, err)
	id2b, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2.KID(), half2b)
	require.NoError(t, err)
	id3a, err := crypto.GetTLFCryptKeyServerHalfID(uid3, key3.KID(), half3a)
	require.NoError(t, err)
	id3b, err := crypto.GetTLFCryptKeyServerHalfID(uid3, key3.KID(), half3b)
	require.NoError(t, err)

	tlfID := tlf.FakeID(1, false)

	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid1, uid2}, []keybase1.UID{uid3}, nil, nil, nil)
	require.NoError(t, err)

	brmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	brmd.WKeys = TLFWriterKeyGenerationsV2{
		TLFWriterKeyBundleV2{
			WKeys: UserDeviceKeyInfoMapV2{
				uid1: DeviceKeyInfoMapV2{
					key1.KID(): TLFCryptKeyInfo{
						ServerHalfID: id1a,
						EPubKeyIndex: 0,
					},
				},
				uid2: DeviceKeyInfoMapV2{
					key2.KID(): TLFCryptKeyInfo{
						ServerHalfID: id2a,
						EPubKeyIndex: 1,
					},
				},
			},
		},
		TLFWriterKeyBundleV2{
			WKeys: UserDeviceKeyInfoMapV2{
				uid1: DeviceKeyInfoMapV2{
					key1.KID(): TLFCryptKeyInfo{
						ServerHalfID: id1b,
						EPubKeyIndex: 0,
					},
				},
				uid2: DeviceKeyInfoMapV2{
					key2.KID(): TLFCryptKeyInfo{
						ServerHalfID: id2b,
						EPubKeyIndex: 0,
					},
				},
			},
		},
	}

	brmd.RKeys = TLFReaderKeyGenerationsV2{
		TLFReaderKeyBundleV2{
			RKeys: UserDeviceKeyInfoMapV2{
				uid3: DeviceKeyInfoMapV2{
					key3.KID(): TLFCryptKeyInfo{
						ServerHalfID: id3a,
						EPubKeyIndex: 0,
					},
				},
			},
		},
		TLFReaderKeyBundleV2{
			RKeys: UserDeviceKeyInfoMapV2{
				uid3: DeviceKeyInfoMapV2{
					key3.KID(): TLFCryptKeyInfo{
						ServerHalfID: id3b,
						EPubKeyIndex: 0,
					},
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

	removalInfo, err := brmd.RevokeRemovedDevices(wKeys, rKeys, nil)
	require.NoError(t, err)
	require.Equal(t, ServerHalfRemovalInfo{
		uid2: userServerHalfRemovalInfo{
			userRemoved: true,
			deviceServerHalfIDs: deviceServerHalfRemovalInfo{
				key2: []TLFCryptKeyServerHalfID{id2a, id2b},
			},
		},
	}, removalInfo)

	expectedWKeys := TLFWriterKeyGenerationsV2{
		TLFWriterKeyBundleV2{
			WKeys: UserDeviceKeyInfoMapV2{
				uid1: DeviceKeyInfoMapV2{
					key1.KID(): TLFCryptKeyInfo{
						ServerHalfID: id1a,
						EPubKeyIndex: 0,
					},
				},
			},
		},
		TLFWriterKeyBundleV2{
			WKeys: UserDeviceKeyInfoMapV2{
				uid1: DeviceKeyInfoMapV2{
					key1.KID(): TLFCryptKeyInfo{
						ServerHalfID: id1b,
						EPubKeyIndex: 0,
					},
				},
			},
		},
	}
	require.Equal(t, expectedWKeys, brmd.WKeys)

	expectedRKeys := TLFReaderKeyGenerationsV2{
		TLFReaderKeyBundleV2{
			RKeys: UserDeviceKeyInfoMapV2{
				uid3: DeviceKeyInfoMapV2{
					key3.KID(): TLFCryptKeyInfo{
						ServerHalfID: id3a,
						EPubKeyIndex: 0,
					},
				},
			},
		},
		TLFReaderKeyBundleV2{
			RKeys: UserDeviceKeyInfoMapV2{
				uid3: DeviceKeyInfoMapV2{
					key3.KID(): TLFCryptKeyInfo{
						ServerHalfID: id3b,
						EPubKeyIndex: 0,
					},
				},
			},
		},
	}
	require.Equal(t, expectedRKeys, brmd.RKeys)
}

func testKeyBundleGetKeysOrBust(t *testing.T, config Config, uid keybase1.UID,
	keys map[keybase1.UID][]kbfscrypto.CryptPublicKey) {
	publicKeys, err := config.KBPKI().GetCryptPublicKeys(
		context.Background(), uid)
	if err != nil {
		t.Fatalf("Couldn't get keys for %s: %v", uid, err)
	}
	keys[uid] = publicKeys
}

func testKeyBundleCheckKeysV2(t *testing.T, crypto Crypto, uid keybase1.UID,
	key kbfscrypto.CryptPublicKey, expectedIndex int,
	wkb TLFWriterKeyBundleV2, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverMap serverKeyMap) {
	ctx := context.Background()
	info, ok := wkb.WKeys[uid][key.KID()]
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

func TestBareRootMetadataV2FillInDevices(t *testing.T) {
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

	tlfID := tlf.FakeID(1, false)

	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid1, uid2, uid3}, nil,
		[]keybase1.SocialAssertion{keybase1.SocialAssertion{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	extra, err := rmd.AddKeyGeneration(codec, crypto1, nil,
		kbfscrypto.TLFCryptKey{}, kbfscrypto.TLFCryptKey{},
		kbfscrypto.TLFPublicKey{})
	require.NoError(t, err)

	serverMap, err := rmd.fillInDevices(
		crypto1, extra, FirstValidKeyGen,
		wKeys, nil, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	wkb, _, err := rmd.GetTLFKeyBundles(FirstValidKeyGen)
	require.NoError(t, err)

	testKeyBundleCheckKeysV2(t, crypto1, uid1, privKey1.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, crypto2, uid2, privKey2.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, crypto3, uid3, privKey3.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)

	privKey1b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1b")
	wKeys[uid1] = append(wKeys[uid1], privKey1b.GetPublicKey())

	_, _, ePubKey2, ePrivKey2, tlfCryptKey2, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)
	serverMap2, err := rmd.fillInDevices(
		crypto1, extra, FirstValidKeyGen,
		wKeys, nil, ePubKey2, ePrivKey2, tlfCryptKey2)
	require.NoError(t, err)

	crypto1b := NewCryptoLocal(codec, signingKey1, privKey1b)

	testKeyBundleCheckKeysV2(t, crypto1, uid1, privKey1.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, crypto1b, uid1, privKey1b.GetPublicKey(), 1, *wkb, ePubKey2, tlfCryptKey2, serverMap2)
	testKeyBundleCheckKeysV2(t, crypto2, uid2, privKey2.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, crypto3, uid3, privKey3.GetPublicKey(), 0, *wkb, ePubKey, tlfCryptKey, serverMap)
}

func testReaderKeyBundleCheckKeys(t *testing.T, crypto Crypto, uid keybase1.UID,
	key kbfscrypto.CryptPublicKey, expectedIndex int,
	rkb TLFReaderKeyBundleV2, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverMap serverKeyMap) {
	ctx := context.Background()
	info, ok := rkb.RKeys[uid][key.KID()]
	require.True(t, ok)
	require.Equal(t, expectedIndex, info.EPubKeyIndex)
	userEPubKey := rkb.TLFReaderEphemeralPublicKeys[-1-info.EPubKeyIndex]
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

func TestBareRootMetadataV2FillInDevicesReaderRekey(t *testing.T) {
	uid1 := keybase1.MakeTestUID(1)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")

	rKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid1: []kbfscrypto.CryptPublicKey{privKey1.GetPublicKey()},
	}

	signingKey1 := kbfscrypto.MakeFakeSigningKeyOrBust("key1")

	codec := kbfscodec.NewMsgpack()
	crypto1 := NewCryptoLocal(codec, signingKey1, privKey1)

	// Fill in the bundle
	_, _, ePubKey, ePrivKey, tlfCryptKey, err := crypto1.MakeRandomTLFKeys()
	require.NoError(t, err)

	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid}, []keybase1.UID{uid1},
		[]keybase1.SocialAssertion{keybase1.SocialAssertion{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	extra, err := rmd.AddKeyGeneration(codec, crypto1, nil,
		kbfscrypto.TLFCryptKey{}, kbfscrypto.TLFCryptKey{},
		kbfscrypto.TLFPublicKey{})
	require.NoError(t, err)

	serverMap, err := rmd.fillInDevices(
		crypto1, extra, FirstValidKeyGen,
		nil, rKeys, ePubKey, ePrivKey, tlfCryptKey)
	require.NoError(t, err)

	_, rkb, err := rmd.GetTLFKeyBundles(FirstValidKeyGen)
	require.NoError(t, err)

	testReaderKeyBundleCheckKeys(t, crypto1, uid1, privKey1.GetPublicKey(), -1, *rkb, ePubKey, tlfCryptKey, serverMap)
}
