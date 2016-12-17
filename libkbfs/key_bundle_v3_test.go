// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/stretchr/testify/require"
)

func TestWKBID(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)

	var wkb1, wkb2 TLFWriterKeyBundleV3
	wkb2.Keys = make(UserDeviceKeyInfoMapV3)

	id1, err := crypto.MakeTLFWriterKeyBundleID(wkb1)
	require.NoError(t, err)

	id2, err := crypto.MakeTLFWriterKeyBundleID(wkb2)
	require.NoError(t, err)

	require.Equal(t, id1, id2)
}

func TestRemoveDevicesNotInV3(t *testing.T) {
	uid1 := keybase1.MakeTestUID(0x1)
	uid2 := keybase1.MakeTestUID(0x2)
	uid3 := keybase1.MakeTestUID(0x3)

	key1a := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key1")
	key1b := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key2")
	key2a := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key3")
	key2b := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key4")
	key2c := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key5")
	key3a := kbfscrypto.MakeFakeCryptPublicKeyOrBust("key6")

	half1a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x1})
	half1b := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x2})
	half2a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x3})
	half2b := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x4})
	half2c := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x5})
	half3a := kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x6})

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	id1a, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1a, half1a)
	require.NoError(t, err)
	id1b, err := crypto.GetTLFCryptKeyServerHalfID(uid1, key1b, half1b)
	require.NoError(t, err)
	id2a, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2a, half2a)
	require.NoError(t, err)
	id2b, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2b, half2b)
	require.NoError(t, err)
	id2c, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key2c, half2c)
	require.NoError(t, err)
	id3a, err := crypto.GetTLFCryptKeyServerHalfID(uid2, key3a, half3a)
	require.NoError(t, err)

	udkimV3 := UserDeviceKeyInfoMapV3{
		uid1: DeviceKeyInfoMapV3{
			key1a: TLFCryptKeyInfo{
				ServerHalfID: id1a,
				EPubKeyIndex: -1,
			},
			key1b: TLFCryptKeyInfo{
				ServerHalfID: id1b,
				EPubKeyIndex: +2,
			},
		},
		uid2: DeviceKeyInfoMapV3{
			key2a: TLFCryptKeyInfo{
				ServerHalfID: id2a,
				EPubKeyIndex: -2,
			},
			key2b: TLFCryptKeyInfo{
				ServerHalfID: id2b,
				EPubKeyIndex: 0,
			},
			key2c: TLFCryptKeyInfo{
				ServerHalfID: id2c,
				EPubKeyIndex: 0,
			},
		},
		uid3: DeviceKeyInfoMapV3{
			key3a: TLFCryptKeyInfo{
				ServerHalfID: id3a,
				EPubKeyIndex: -2,
			},
		},
	}

	removalInfo := udkimV3.removeDevicesNotIn(UserDevicePublicKeys{
		uid2: {key2a: true, key2c: true},
		uid3: {key3a: true},
	})

	require.Equal(t, UserDeviceKeyInfoMapV3{
		uid2: DeviceKeyInfoMapV3{
			key2a: TLFCryptKeyInfo{
				ServerHalfID: id2a,
				EPubKeyIndex: -2,
			},
			key2c: TLFCryptKeyInfo{
				ServerHalfID: id2c,
				EPubKeyIndex: 0,
			},
		},
		uid3: DeviceKeyInfoMapV3{
			key3a: TLFCryptKeyInfo{
				ServerHalfID: id3a,
				EPubKeyIndex: -2,
			},
		},
	}, udkimV3)

	require.Equal(t, ServerHalfRemovalInfo{
		uid1: userServerHalfRemovalInfo{
			userRemoved: true,
			deviceServerHalfIDs: deviceServerHalfRemovalInfo{
				key1a: []TLFCryptKeyServerHalfID{id1a},
				key1b: []TLFCryptKeyServerHalfID{id1b},
			},
		},
		uid2: userServerHalfRemovalInfo{
			userRemoved: false,
			deviceServerHalfIDs: deviceServerHalfRemovalInfo{
				key2b: []TLFCryptKeyServerHalfID{id2b},
			},
		},
	}, removalInfo)
}

type deviceKeyInfoMapV3Future map[kbfscrypto.CryptPublicKey]tlfCryptKeyInfoFuture

func (dkimf deviceKeyInfoMapV3Future) toCurrent() DeviceKeyInfoMapV3 {
	dkim := make(DeviceKeyInfoMapV3, len(dkimf))
	for k, kif := range dkimf {
		ki := kif.toCurrent()
		dkim[k] = TLFCryptKeyInfo(ki)
	}
	return dkim
}

type userDeviceKeyInfoMapV3Future map[keybase1.UID]deviceKeyInfoMapV3Future

func (udkimf userDeviceKeyInfoMapV3Future) toCurrent() UserDeviceKeyInfoMapV3 {
	udkim := make(UserDeviceKeyInfoMapV3)
	for u, dkimf := range udkimf {
		dkim := dkimf.toCurrent()
		udkim[u] = dkim
	}
	return udkim
}

type tlfWriterKeyBundleV3Future struct {
	TLFWriterKeyBundleV3
	// Override TLFWriterKeyBundleV3.Keys.
	Keys userDeviceKeyInfoMapV3Future `codec:"wKeys"`
	kbfscodec.Extra
}

func (wkbf tlfWriterKeyBundleV3Future) toCurrent() TLFWriterKeyBundleV3 {
	wkb := wkbf.TLFWriterKeyBundleV3
	wkb.Keys = wkbf.Keys.toCurrent()
	return wkb
}

func (wkbf tlfWriterKeyBundleV3Future) ToCurrentStruct() kbfscodec.CurrentStruct {
	return wkbf.toCurrent()
}

func makeFakeDeviceKeyInfoMapV3Future(t *testing.T) userDeviceKeyInfoMapV3Future {
	return userDeviceKeyInfoMapV3Future{
		"fake uid": deviceKeyInfoMapV3Future{
			kbfscrypto.MakeFakeCryptPublicKeyOrBust("fake key"): makeFakeTLFCryptKeyInfoFuture(t),
		},
	}
}

func makeFakeTLFWriterKeyBundleV3Future(t *testing.T) tlfWriterKeyBundleV3Future {
	wkb := TLFWriterKeyBundleV3{
		nil,
		kbfscrypto.MakeTLFPublicKey([32]byte{0xa}),
		kbfscrypto.TLFEphemeralPublicKeys{
			kbfscrypto.MakeTLFEphemeralPublicKey([32]byte{0xb}),
		},
		EncryptedTLFCryptKeys{},
		codec.UnknownFieldSetHandler{},
	}
	return tlfWriterKeyBundleV3Future{
		wkb,
		makeFakeDeviceKeyInfoMapV3Future(t),
		kbfscodec.MakeExtraOrBust("TLFWriterKeyBundleV3", t),
	}
}

func TestTLFWriterKeyBundleV3UnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeTLFWriterKeyBundleV3Future(t))
}

type tlfReaderKeyBundleV3Future struct {
	TLFReaderKeyBundleV3
	// Override TLFReaderKeyBundleV3.Keys.
	Keys userDeviceKeyInfoMapV3Future `codec:"rKeys"`
	kbfscodec.Extra
}

func (rkbf tlfReaderKeyBundleV3Future) toCurrent() TLFReaderKeyBundleV3 {
	rkb := rkbf.TLFReaderKeyBundleV3
	rkb.Keys = rkbf.Keys.toCurrent()
	return rkb
}

func (rkbf tlfReaderKeyBundleV3Future) ToCurrentStruct() kbfscodec.CurrentStruct {
	return rkbf.toCurrent()
}

func makeFakeTLFReaderKeyBundleV3Future(
	t *testing.T) tlfReaderKeyBundleV3Future {
	rkb := TLFReaderKeyBundleV3{
		nil,
		kbfscrypto.TLFEphemeralPublicKeys{
			kbfscrypto.MakeTLFEphemeralPublicKey([32]byte{0xc}),
		},
		codec.UnknownFieldSetHandler{},
	}
	return tlfReaderKeyBundleV3Future{
		rkb,
		makeFakeDeviceKeyInfoMapV3Future(t),
		kbfscodec.MakeExtraOrBust("TLFReaderKeyBundleV3", t),
	}
}

func TestTLFReaderKeyBundleV3UnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeTLFReaderKeyBundleV3Future(t))
}
