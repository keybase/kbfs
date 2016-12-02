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

func checkWKBV2(t *testing.T, wkb *TLFWriterKeyBundleV2, serverMap ServerKeyMap,
	uid keybase1.UID, key kbfscrypto.CryptPublicKey,
	expectedEPubKeyIndex int,
	expectedEPubKey kbfscrypto.TLFEphemeralPublicKey,
	crypto Crypto, expectedTLFCryptKey kbfscrypto.TLFCryptKey) {
	info, ok := wkb.WKeys[uid][key.KID()]
	require.True(t, ok)

	serverHalf, ok := serverMap[uid][key.KID()]
	require.True(t, ok)

	require.Equal(t, expectedEPubKeyIndex, info.EPubKeyIndex)

	ePubKey := wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	require.Equal(t, expectedEPubKey, ePubKey)

	ctx := context.Background()
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, ePubKey, info.ClientHalf)
	require.NoError(t, err)

	tlfCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, expectedTLFCryptKey, tlfCryptKey)
}

func checkRKBV2(t *testing.T, wkb *TLFWriterKeyBundleV2,
	rkb *TLFReaderKeyBundleV2, serverMap ServerKeyMap,
	uid keybase1.UID, key kbfscrypto.CryptPublicKey,
	expectedEPubKeyIndex int,
	expectedEPubKey kbfscrypto.TLFEphemeralPublicKey,
	crypto Crypto, expectedTLFCryptKey kbfscrypto.TLFCryptKey) {
	info, ok := rkb.RKeys[uid][key.KID()]
	require.True(t, ok)

	serverHalf, ok := serverMap[uid][key.KID()]
	require.True(t, ok)

	require.Equal(t, expectedEPubKeyIndex, info.EPubKeyIndex)

	var ePubKey kbfscrypto.TLFEphemeralPublicKey
	if info.EPubKeyIndex >= 0 {
		ePubKey = wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	} else {
		ePubKey = rkb.TLFReaderEphemeralPublicKeys[-1-info.EPubKeyIndex]
	}
	require.Equal(t, expectedEPubKey, ePubKey)

	ctx := context.Background()
	clientHalf, err := crypto.DecryptTLFCryptKeyClientHalf(
		ctx, ePubKey, info.ClientHalf)
	require.NoError(t, err)

	tlfCryptKey, err := crypto.UnmaskTLFCryptKey(serverHalf, clientHalf)
	require.NoError(t, err)
	require.Equal(t, expectedTLFCryptKey, tlfCryptKey)
}

func TestBareRootMetadataV2UpdateKeyGeneration(t *testing.T) {
	uid1 := keybase1.MakeTestUID(1)
	uid2 := keybase1.MakeTestUID(2)
	uid3 := keybase1.MakeTestUID(3)

	privKey1 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1")
	privKey2 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key2")
	privKey3 := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3")

	wKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid1: []kbfscrypto.CryptPublicKey{privKey1.GetPublicKey()},
		uid2: []kbfscrypto.CryptPublicKey{privKey2.GetPublicKey()},
	}

	rKeys := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid3: []kbfscrypto.CryptPublicKey{privKey3.GetPublicKey()},
	}

	tlfID := tlf.FakeID(1, false)

	bh, err := tlf.MakeHandle(
		[]keybase1.UID{uid1, uid2}, []keybase1.UID{uid3},
		[]keybase1.SocialAssertion{keybase1.SocialAssertion{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV2(tlfID, bh)
	require.NoError(t, err)

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)

	pubKey, _, ePubKey1, ePrivKey1, tlfCryptKey1, err :=
		crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	// Add and update first key generation.

	extra, err := rmd.AddKeyGeneration(codec, crypto, nil,
		kbfscrypto.TLFCryptKey{}, kbfscrypto.TLFCryptKey{}, pubKey)
	require.NoError(t, err)

	wkb, rkb, err := rmd.getTLFKeyBundles(FirstValidKeyGen)
	require.NoError(t, err)

	require.Equal(t, TLFWriterKeyBundleV2{
		WKeys:        UserDeviceKeyInfoMapV2{},
		TLFPublicKey: pubKey,
	}, *wkb)
	require.Equal(t, TLFReaderKeyBundleV2{
		RKeys: UserDeviceKeyInfoMapV2{},
	}, *rkb)

	serverMap, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey1, ePrivKey1, tlfCryptKey1)
	require.NoError(t, err)

	require.Equal(t, 2, len(wkb.WKeys))
	require.Equal(t, 1, len(wkb.WKeys[uid1]))
	require.Equal(t, 1, len(wkb.WKeys[uid2]))
	require.Equal(t, pubKey, wkb.TLFPublicKey)
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys{ePubKey1},
		wkb.TLFEphemeralPublicKeys)

	require.Equal(t, 1, len(rkb.RKeys))
	require.Equal(t, 1, len(rkb.RKeys[uid3]))
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys(nil),
		rkb.TLFReaderEphemeralPublicKeys)

	dummySigningKey := kbfscrypto.MakeFakeSigningKeyOrBust("dummy")

	crypto1 := NewCryptoLocal(codec, dummySigningKey, privKey1)
	crypto2 := NewCryptoLocal(codec, dummySigningKey, privKey2)
	crypto3 := NewCryptoLocal(codec, dummySigningKey, privKey3)

	checkWKBV2(t, wkb, serverMap, uid1, privKey1.GetPublicKey(),
		0, ePubKey1, crypto1, tlfCryptKey1)
	checkWKBV2(t, wkb, serverMap, uid2, privKey2.GetPublicKey(),
		0, ePubKey1, crypto2, tlfCryptKey1)

	checkRKBV2(t, wkb, rkb, serverMap, uid3, privKey3.GetPublicKey(),
		0, ePubKey1, crypto3, tlfCryptKey1)

	// Rekey.

	privKey1b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key1b")
	wKeys[uid1] = append(wKeys[uid1], privKey1b.GetPublicKey())

	privKey3b := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3b")
	rKeys[uid3] = append(rKeys[uid3], privKey3b.GetPublicKey())

	_, _, ePubKey2, ePrivKey2, tlfCryptKey2, err :=
		crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	serverMap2, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, wKeys, rKeys, ePubKey2, ePrivKey2, tlfCryptKey2)
	require.NoError(t, err)

	require.Equal(t, 2, len(wkb.WKeys))
	require.Equal(t, 2, len(wkb.WKeys[uid1]))
	require.Equal(t, 1, len(wkb.WKeys[uid2]))
	require.Equal(t, pubKey, wkb.TLFPublicKey)
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys{ePubKey1, ePubKey2},
		wkb.TLFEphemeralPublicKeys)

	require.Equal(t, 1, len(rkb.RKeys))
	require.Equal(t, 2, len(rkb.RKeys[uid3]))
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys(nil),
		rkb.TLFReaderEphemeralPublicKeys)

	checkWKBV2(t, wkb, serverMap, uid1, privKey1.GetPublicKey(),
		0, ePubKey1, crypto1, tlfCryptKey1)
	checkWKBV2(t, wkb, serverMap, uid2, privKey2.GetPublicKey(),
		0, ePubKey1, crypto2, tlfCryptKey1)
	checkRKBV2(t, wkb, rkb, serverMap, uid3, privKey3.GetPublicKey(),
		0, ePubKey1, crypto3, tlfCryptKey1)

	crypto1b := NewCryptoLocal(codec, dummySigningKey, privKey1b)
	crypto3b := NewCryptoLocal(codec, dummySigningKey, privKey3b)

	checkWKBV2(t, wkb, serverMap2, uid1, privKey1b.GetPublicKey(),
		1, ePubKey2, crypto1b, tlfCryptKey2)
	checkRKBV2(t, wkb, rkb, serverMap2, uid3, privKey3b.GetPublicKey(),
		1, ePubKey2, crypto3b, tlfCryptKey2)

	// Reader rekey.

	privKey3c := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3c")
	privKey3d := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("key3d")
	rKeys[uid3] = append(rKeys[uid3],
		privKey3c.GetPublicKey(), privKey3d.GetPublicKey())

	_, _, ePubKey3, ePrivKey3, tlfCryptKey3, err :=
		crypto.MakeRandomTLFKeys()
	require.NoError(t, err)

	rKeysReader := map[keybase1.UID][]kbfscrypto.CryptPublicKey{
		uid3: rKeys[uid3],
	}
	serverMap3, err := rmd.UpdateKeyGeneration(crypto, FirstValidKeyGen,
		extra, nil, rKeysReader, ePubKey3, ePrivKey3, tlfCryptKey3)
	require.NoError(t, err)

	require.Equal(t, 2, len(wkb.WKeys))
	require.Equal(t, 2, len(wkb.WKeys[uid1]))
	require.Equal(t, 1, len(wkb.WKeys[uid2]))
	require.Equal(t, pubKey, wkb.TLFPublicKey)
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys{ePubKey1, ePubKey2},
		wkb.TLFEphemeralPublicKeys)

	require.Equal(t, 1, len(rkb.RKeys))
	require.Equal(t, 4, len(rkb.RKeys[uid3]))
	require.Equal(t, kbfscrypto.TLFEphemeralPublicKeys{ePubKey3},
		rkb.TLFReaderEphemeralPublicKeys)

	checkWKBV2(t, wkb, serverMap, uid1, privKey1.GetPublicKey(),
		0, ePubKey1, crypto1, tlfCryptKey1)
	checkWKBV2(t, wkb, serverMap, uid2, privKey2.GetPublicKey(),
		0, ePubKey1, crypto2, tlfCryptKey1)
	checkRKBV2(t, wkb, rkb, serverMap, uid3, privKey3.GetPublicKey(),
		0, ePubKey1, crypto3, tlfCryptKey1)

	checkWKBV2(t, wkb, serverMap2, uid1, privKey1b.GetPublicKey(),
		1, ePubKey2, crypto1b, tlfCryptKey2)
	checkRKBV2(t, wkb, rkb, serverMap2, uid3, privKey3b.GetPublicKey(),
		1, ePubKey2, crypto3b, tlfCryptKey2)

	crypto3c := NewCryptoLocal(codec, dummySigningKey, privKey3c)
	crypto3d := NewCryptoLocal(codec, dummySigningKey, privKey3d)

	checkRKBV2(t, wkb, rkb, serverMap3, uid3, privKey3c.GetPublicKey(),
		-1, ePubKey3, crypto3c, tlfCryptKey3)
	checkRKBV2(t, wkb, rkb, serverMap3, uid3, privKey3d.GetPublicKey(),
		-1, ePubKey3, crypto3d, tlfCryptKey3)
}
