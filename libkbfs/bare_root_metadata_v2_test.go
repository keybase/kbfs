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

func testKeyBundleCheckKeysV2(t *testing.T, config Config, uid keybase1.UID,
	wkb TLFWriterKeyBundleV2, ePubKey kbfscrypto.TLFEphemeralPublicKey,
	tlfCryptKey kbfscrypto.TLFCryptKey, serverMap serverKeyMap) {
	ctx := context.Background()
	// Check that every user can recover the crypt key
	cryptPublicKey, err := config.KBPKI().GetCurrentCryptPublicKey(ctx)
	if err != nil {
		t.Fatalf("Couldn't get current public key for user %s: %v", uid, err)
	}
	info, ok := wkb.WKeys[uid][cryptPublicKey.KID()]
	if !ok {
		t.Fatalf("Couldn't get current key info for user %s: %v", uid, err)
	}
	if info.EPubKeyIndex < 0 || info.EPubKeyIndex >= len(wkb.TLFEphemeralPublicKeys) {
		t.Fatalf("Error getting ephemeral public key for user %s: %v", uid, err)
	}
	userEPubKey := wkb.TLFEphemeralPublicKeys[info.EPubKeyIndex]
	if g, e := userEPubKey, ePubKey; g != e {
		t.Fatalf("Unexpected ePubKey for user %s: %s vs %s", uid, g, e)
	}
	clientHalf, err := config.Crypto().DecryptTLFCryptKeyClientHalf(
		ctx, userEPubKey, info.ClientHalf)
	if err != nil {
		t.Fatalf("Couldn't decrypt client key half for user %s: %v", uid, err)
	}
	serverHalf, ok := serverMap[uid][cryptPublicKey.KID()]
	if !ok {
		t.Fatalf("No server half for user %s", uid)
	}
	userTLFCryptKey, err :=
		config.Crypto().UnmaskTLFCryptKey(serverHalf, clientHalf)
	if err != nil {
		t.Fatalf("Couldn't unmask TLF key for user %s: %v", uid, err)
	}
	if g, e := userTLFCryptKey, tlfCryptKey; g != e {
		t.Fatalf("TLF crypt key didn't match for user %s: %s vs. %s", uid, g, e)
	}
}

func TestKeyBundleFillInDevicesV2(t *testing.T) {
	config1 := MakeTestConfigOrBust(t, "u1", "u2", "u3")
	defer CheckConfigAndShutdown(t, config1)
	config2 := ConfigAsUser(config1, "u2")
	defer CheckConfigAndShutdown(t, config2)
	config3 := ConfigAsUser(config1, "u3")
	defer CheckConfigAndShutdown(t, config3)

	ctx := context.Background()
	_, u1, err := config1.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatalf("Couldn't get uid for user 1: %v", err)
	}
	_, u2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatalf("Couldn't get uid for user 2: %v", err)
	}
	_, u3, err := config3.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatalf("Couldn't get uid for user 3: %v", err)
	}

	// Make a wkb with empty writer key maps
	wkb := TLFWriterKeyBundleV2{
		WKeys: make(UserDeviceKeyInfoMapV2),
		TLFEphemeralPublicKeys: make(kbfscrypto.TLFEphemeralPublicKeys, 1),
	}

	// Generate keys
	wKeys := make(map[keybase1.UID][]kbfscrypto.CryptPublicKey)

	testKeyBundleGetKeysOrBust(t, config1, u1, wKeys)
	testKeyBundleGetKeysOrBust(t, config1, u2, wKeys)
	testKeyBundleGetKeysOrBust(t, config1, u3, wKeys)

	// Fill in the bundle
	_, _, ePubKey, ePrivKey, tlfCryptKey, err :=
		config1.Crypto().MakeRandomTLFKeys()
	if err != nil {
		t.Fatalf("Couldn't make keys: %v", err)
	}
	// TODO: Fix this hack.
	var md *BareRootMetadataV2
	serverMap, err := md.fillInDevices(
		config1.Crypto(), &wkb, &TLFReaderKeyBundleV2{},
		wKeys, nil, ePubKey, ePrivKey, tlfCryptKey)
	if err != nil {
		t.Fatalf("Fill in devices failed: %v", err)
	}

	testKeyBundleCheckKeysV2(t, config1, u1, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, config2, u2, wkb, ePubKey, tlfCryptKey, serverMap)
	testKeyBundleCheckKeysV2(t, config3, u3, wkb, ePubKey, tlfCryptKey, serverMap)

	// Add a device key for user 1
	devIndex := AddDeviceForLocalUserOrBust(t, config1, u1)
	config1B := ConfigAsUser(config1, "u1")
	defer CheckConfigAndShutdown(t, config1B)
	SwitchDeviceForLocalUserOrBust(t, config1B, devIndex)
	newCryptPublicKey, err := config1B.KBPKI().GetCurrentCryptPublicKey(ctx)
	if err != nil {
		t.Fatalf("Couldn't get new publc device key for user %s: %v", u1, err)
	}
	wKeys[u1] = append(wKeys[u1], newCryptPublicKey)

	// Fill in the bundle again, make sure only the new device key
	// gets a ePubKeyIndex bump
	_, _, ePubKey2, ePrivKey2, _, err := config1.Crypto().MakeRandomTLFKeys()
	if err != nil {
		t.Fatalf("Couldn't make keys: %v", err)
	}
	serverMap2, err := md.fillInDevices(
		config1.Crypto(), &wkb, &TLFReaderKeyBundleV2{},
		wKeys, nil, ePubKey2, ePrivKey2, tlfCryptKey)
	if err != nil {
		t.Fatalf("Fill in devices failed: %v", err)
	}

	testKeyBundleCheckKeysV2(t, config1B, u1, wkb, ePubKey2, tlfCryptKey,
		serverMap2)
	if len(serverMap2) > 1 {
		t.Fatalf("Generated more than one key after device add: %d",
			len(serverMap2))
	}
}
