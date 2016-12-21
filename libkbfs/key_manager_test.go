// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type shimKMCrypto struct {
	Crypto
	pure cryptoPure
}

func (c shimKMCrypto) EncryptTLFCryptKeys(oldKeys []kbfscrypto.TLFCryptKey,
	key kbfscrypto.TLFCryptKey) (EncryptedTLFCryptKeys, error) {
	return c.pure.EncryptTLFCryptKeys(oldKeys, key)
}

func (c shimKMCrypto) DecryptTLFCryptKeys(
	encKeys EncryptedTLFCryptKeys, key kbfscrypto.TLFCryptKey) (
	[]kbfscrypto.TLFCryptKey, error) {
	return c.pure.DecryptTLFCryptKeys(encKeys, key)
}

func (c shimKMCrypto) MakeTLFWriterKeyBundleID(
	wkb TLFWriterKeyBundleV3) (TLFWriterKeyBundleID, error) {
	return c.pure.MakeTLFWriterKeyBundleID(wkb)
}

func (c shimKMCrypto) MakeTLFReaderKeyBundleID(
	wkb TLFReaderKeyBundleV3) (TLFReaderKeyBundleID, error) {
	return c.pure.MakeTLFReaderKeyBundleID(wkb)
}

func keyManagerInit(t *testing.T, ver MetadataVer) (mockCtrl *gomock.Controller,
	config *ConfigMock, ctx context.Context) {
	ctr := NewSafeTestReporter(t)
	mockCtrl = gomock.NewController(ctr)
	config = NewConfigMock(mockCtrl, ctr)
	keyCache := NewKeyCacheStandard(100)
	config.SetKeyCache(keyCache)
	keyman := NewKeyManagerStandard(config)
	config.SetKeyManager(keyman)
	interposeDaemonKBPKI(config, "alice", "bob", "charlie", "dave")
	ctx = context.Background()
	codec := kbfscodec.NewMsgpack()
	config.SetCodec(codec)
	cryptoPure := MakeCryptoCommon(codec)
	config.SetCrypto(shimKMCrypto{config.Crypto(), cryptoPure})
	config.SetMetadataVersion(ver)
	return
}

func keyManagerShutdown(mockCtrl *gomock.Controller, config *ConfigMock) {
	config.ctr.CheckForFailures()
	mockCtrl.Finish()
}

var serverHalf = kbfscrypto.MakeTLFCryptKeyServerHalf([32]byte{0x2})

func expectUncachedGetTLFCryptKey(t *testing.T, config *ConfigMock, tlfID tlf.ID, keyGen, currKeyGen KeyGen,
	uid keybase1.UID, subkey kbfscrypto.CryptPublicKey,
	storesHistoric bool, tlfCryptKey, currTLFCryptKey kbfscrypto.TLFCryptKey) {
	if keyGen == currKeyGen {
		require.Equal(t, tlfCryptKey, currTLFCryptKey)
	}
	if storesHistoric {
		if keyGen < currKeyGen {
			config.mockKbpki.EXPECT().GetCurrentCryptPublicKey(
				gomock.Any()).Return(subkey, nil)
		}

		clientHalf := kbfscrypto.MaskTLFCryptKey(
			serverHalf, currTLFCryptKey)

		// get the xor'd key out of the metadata
		config.mockKbpki.EXPECT().GetCurrentCryptPublicKey(
			gomock.Any()).Return(subkey, nil)
		config.mockCrypto.EXPECT().DecryptTLFCryptKeyClientHalf(
			gomock.Any(), kbfscrypto.TLFEphemeralPublicKey{},
			gomock.Any()).Return(clientHalf, nil)

		// get the server-side half and retrieve the real
		// current secret key
		config.mockKops.EXPECT().GetTLFCryptKeyServerHalf(gomock.Any(),
			gomock.Any(), gomock.Any()).Return(serverHalf, nil)
	} else {
		clientHalf := kbfscrypto.MaskTLFCryptKey(
			serverHalf, tlfCryptKey)

		// get the xor'd key out of the metadata
		config.mockKbpki.EXPECT().GetCurrentCryptPublicKey(
			gomock.Any()).Return(subkey, nil)
		config.mockCrypto.EXPECT().DecryptTLFCryptKeyClientHalf(
			gomock.Any(), kbfscrypto.TLFEphemeralPublicKey{},
			gomock.Any()).Return(clientHalf, nil)

		// get the server-side half and retrieve the real secret key
		config.mockKops.EXPECT().GetTLFCryptKeyServerHalf(gomock.Any(),
			gomock.Any(), gomock.Any()).Return(serverHalf, nil)
	}
}

func expectUncachedGetTLFCryptKeyAnyDevice(
	config *ConfigMock, tlfID tlf.ID, keyGen KeyGen, uid keybase1.UID,
	subkey kbfscrypto.CryptPublicKey, tlfCryptKey kbfscrypto.TLFCryptKey) {
	clientHalf := kbfscrypto.MaskTLFCryptKey(serverHalf, tlfCryptKey)

	// get the xor'd key out of the metadata
	config.mockKbpki.EXPECT().GetCryptPublicKeys(gomock.Any(), uid).
		Return([]kbfscrypto.CryptPublicKey{subkey}, nil)
	config.mockCrypto.EXPECT().DecryptTLFCryptKeyClientHalfAny(gomock.Any(),
		gomock.Any(), false).Return(clientHalf, 0, nil)

	// get the server-side half and retrieve the real secret key
	config.mockKops.EXPECT().GetTLFCryptKeyServerHalf(gomock.Any(),
		gomock.Any(), gomock.Any()).Return(serverHalf, nil)
}

func expectRekey(config *ConfigMock, bh tlf.Handle, numDevices int,
	handleChange, expectNewKeyGen bool,
	tlfCryptKey kbfscrypto.TLFCryptKey) {
	if handleChange {
		// if the handle changes the key manager checks for a conflict
		config.mockMdops.EXPECT().GetLatestHandleForTLF(gomock.Any(), gomock.Any()).
			Return(bh, nil)
	}

	// generate new keys
	config.mockCrypto.EXPECT().MakeRandomTLFEphemeralKeys().Return(
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{}, nil)
	if expectNewKeyGen {
		config.mockCrypto.EXPECT().MakeRandomTLFKeys().Return(
			kbfscrypto.TLFPublicKey{}, kbfscrypto.TLFPrivateKey{},
			tlfCryptKey, nil)
	}
	config.mockCrypto.EXPECT().MakeRandomTLFCryptKeyServerHalf().Return(
		serverHalf, nil).Times(numDevices)

	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	config.mockKbpki.EXPECT().GetCryptPublicKeys(gomock.Any(), gomock.Any()).
		Return([]kbfscrypto.CryptPublicKey{subkey}, nil).Times(numDevices)

	clientHalf := kbfscrypto.MaskTLFCryptKey(serverHalf, tlfCryptKey)

	// make keys for the one device
	config.mockCrypto.EXPECT().EncryptTLFCryptKeyClientHalf(
		kbfscrypto.TLFEphemeralPrivateKey{}, subkey, clientHalf).Return(
		EncryptedTLFCryptKeyClientHalf{}, nil).Times(numDevices)
	config.mockKops.EXPECT().PutTLFCryptKeyServerHalves(gomock.Any(), gomock.Any()).Return(nil)
	config.mockCrypto.EXPECT().GetTLFCryptKeyServerHalfID(gomock.Any(), gomock.Any(), gomock.Any()).Return(TLFCryptKeyServerHalfID{}, nil).Times(numDevices)

	// Ignore Notify and Flush calls for now
	config.mockRep.EXPECT().Notify(gomock.Any(), gomock.Any()).AnyTimes()
	config.mockKbs.EXPECT().FlushUserFromLocalCache(gomock.Any(),
		gomock.Any()).AnyTimes()
}

type emptyKeyMetadata struct {
	tlfID  tlf.ID
	keyGen KeyGen
}

var _ KeyMetadata = emptyKeyMetadata{}

func (kmd emptyKeyMetadata) TlfID() tlf.ID {
	return kmd.tlfID
}

// GetTlfHandle just returns nil. This contradicts the requirements
// for KeyMetadata, but emptyKeyMetadata shouldn't be used in contexts
// that actually use GetTlfHandle().
func (kmd emptyKeyMetadata) GetTlfHandle() *TlfHandle {
	return nil
}

func (kmd emptyKeyMetadata) LatestKeyGeneration() KeyGen {
	return kmd.keyGen
}

func (kmd emptyKeyMetadata) HasKeyForUser(
	keyGen KeyGen, user keybase1.UID) (bool, error) {
	return false, nil
}

func (kmd emptyKeyMetadata) GetTLFCryptKeyParams(
	keyGen KeyGen, user keybase1.UID, key kbfscrypto.CryptPublicKey) (
	kbfscrypto.TLFEphemeralPublicKey, EncryptedTLFCryptKeyClientHalf,
	TLFCryptKeyServerHalfID, bool, error) {
	return kbfscrypto.TLFEphemeralPublicKey{},
		EncryptedTLFCryptKeyClientHalf{},
		TLFCryptKeyServerHalfID{}, false, nil
}

func (kmd emptyKeyMetadata) StoresHistoricTLFCryptKeys() bool {
	return false
}

func (kmd emptyKeyMetadata) GetHistoricTLFCryptKey(
	crypto cryptoPure, keyGen KeyGen, key kbfscrypto.TLFCryptKey) (
	kbfscrypto.TLFCryptKey, error) {
	return kbfscrypto.TLFCryptKey{}, nil
}

func testKeyManagerPublicTLFCryptKey(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, true)
	kmd := emptyKeyMetadata{id, 1}

	tlfCryptKey, err := config.KeyManager().
		GetTLFCryptKeyForEncryption(ctx, kmd)
	if err != nil {
		t.Error(err)
	}

	if tlfCryptKey != kbfscrypto.PublicTLFCryptKey {
		t.Errorf("got %v, expected %v",
			tlfCryptKey, kbfscrypto.PublicTLFCryptKey)
	}

	tlfCryptKey, err = config.KeyManager().
		GetTLFCryptKeyForMDDecryption(ctx, kmd, kmd)
	if err != nil {
		t.Error(err)
	}

	if tlfCryptKey != kbfscrypto.PublicTLFCryptKey {
		t.Errorf("got %v, expected %v",
			tlfCryptKey, kbfscrypto.PublicTLFCryptKey)
	}

	tlfCryptKey, err = config.KeyManager().
		GetTLFCryptKeyForBlockDecryption(ctx, kmd, BlockPointer{})
	if err != nil {
		t.Error(err)
	}

	if tlfCryptKey != kbfscrypto.PublicTLFCryptKey {
		t.Errorf("got %v, expected %v",
			tlfCryptKey, kbfscrypto.PublicTLFCryptKey)
	}
}

func testKeyManagerCachedSecretKeyForEncryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	kmd := emptyKeyMetadata{id, 1}

	cachedTLFCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	config.KeyCache().PutTLFCryptKey(id, 1, cachedTLFCryptKey)

	tlfCryptKey, err := config.KeyManager().
		GetTLFCryptKeyForEncryption(ctx, kmd)
	require.NoError(t, err)
	require.Equal(t, cachedTLFCryptKey, tlfCryptKey)
}

func testKeyManagerCachedSecretKeyForMDDecryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	kmd := emptyKeyMetadata{id, 1}

	cachedTLFCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	config.KeyCache().PutTLFCryptKey(id, 1, cachedTLFCryptKey)

	tlfCryptKey, err := config.KeyManager().
		GetTLFCryptKeyForMDDecryption(ctx, kmd, kmd)
	require.NoError(t, err)
	require.Equal(t, cachedTLFCryptKey, tlfCryptKey)
}

func testKeyManagerCachedSecretKeyForBlockDecryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	kmd := emptyKeyMetadata{id, 2}

	cachedTLFCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	config.KeyCache().PutTLFCryptKey(id, 1, cachedTLFCryptKey)

	tlfCryptKey, err := config.KeyManager().GetTLFCryptKeyForBlockDecryption(
		ctx, kmd, BlockPointer{KeyGen: 1})
	require.NoError(t, err)
	require.Equal(t, cachedTLFCryptKey, tlfCryptKey)
}

// makeDirWKeyInfoMap creates a new user device key info map with a writer key.
func makeDirWKeyInfoMap(uid keybase1.UID,
	cryptPublicKey kbfscrypto.CryptPublicKey) UserDevicePublicKeys {
	return UserDevicePublicKeys{
		uid: {
			cryptPublicKey: true,
		},
	}
}

func testKeyManagerUncachedSecretKeyForEncryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	uid := h.FirstResolvedWriter()
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	storedTLFCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})

	crypto := MakeCryptoCommon(config.Codec())
	_, err = rmd.AddKeyGeneration(config.Codec(), crypto,
		makeDirWKeyInfoMap(uid, subkey), UserDevicePublicKeys{},
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{},
		kbfscrypto.TLFPublicKey{}, kbfscrypto.TLFPrivateKey{},
		kbfscrypto.TLFCryptKey{}, storedTLFCryptKey)
	require.NoError(t, err)

	storesHistoric := rmd.StoresHistoricTLFCryptKeys()
	expectUncachedGetTLFCryptKey(t, config, rmd.TlfID(),
		rmd.LatestKeyGeneration(), rmd.LatestKeyGeneration(), uid,
		subkey, storesHistoric, storedTLFCryptKey, storedTLFCryptKey)

	tlfCryptKey, err := config.KeyManager().
		GetTLFCryptKeyForEncryption(ctx, rmd)
	require.NoError(t, err)
	require.Equal(t, storedTLFCryptKey, tlfCryptKey)
}

func testKeyManagerUncachedSecretKeyForMDDecryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	uid := h.FirstResolvedWriter()
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	storedTLFCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})

	crypto := MakeCryptoCommon(config.Codec())
	_, err = rmd.AddKeyGeneration(config.Codec(), crypto,
		makeDirWKeyInfoMap(uid, subkey), UserDevicePublicKeys{},
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{},
		kbfscrypto.TLFPublicKey{}, kbfscrypto.TLFPrivateKey{},
		kbfscrypto.TLFCryptKey{}, storedTLFCryptKey)
	require.NoError(t, err)

	expectUncachedGetTLFCryptKeyAnyDevice(
		config, rmd.TlfID(), rmd.LatestKeyGeneration(), uid, subkey,
		storedTLFCryptKey)

	tlfCryptKey, err := config.KeyManager().
		GetTLFCryptKeyForMDDecryption(ctx, rmd, rmd)
	require.NoError(t, err)
	require.Equal(t, storedTLFCryptKey, tlfCryptKey)
}

func testKeyManagerUncachedSecretKeyForBlockDecryptionSuccess(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	uid := h.FirstResolvedWriter()
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	storedTLFCryptKey1 := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	storedTLFCryptKey2 := kbfscrypto.MakeTLFCryptKey([32]byte{0x2})

	crypto := MakeCryptoCommon(config.Codec())
	_, err = rmd.AddKeyGeneration(config.Codec(), crypto,
		makeDirWKeyInfoMap(uid, subkey), UserDevicePublicKeys{},
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{},
		kbfscrypto.TLFPublicKey{}, kbfscrypto.TLFPrivateKey{},
		kbfscrypto.TLFCryptKey{}, storedTLFCryptKey1)
	require.NoError(t, err)

	var currCryptKey kbfscrypto.TLFCryptKey
	if rmd.StoresHistoricTLFCryptKeys() {
		currCryptKey = storedTLFCryptKey1
	}
	_, err = rmd.AddKeyGeneration(config.Codec(), crypto,
		makeDirWKeyInfoMap(uid, subkey), UserDevicePublicKeys{},
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{},
		kbfscrypto.TLFPublicKey{}, kbfscrypto.TLFPrivateKey{},
		currCryptKey, storedTLFCryptKey2)
	require.NoError(t, err)

	keyGen := rmd.LatestKeyGeneration() - 1
	storesHistoric := rmd.StoresHistoricTLFCryptKeys()
	expectUncachedGetTLFCryptKey(t, config, rmd.TlfID(),
		keyGen, rmd.LatestKeyGeneration(), uid, subkey,
		storesHistoric, storedTLFCryptKey1, storedTLFCryptKey2)

	tlfCryptKey, err := config.KeyManager().GetTLFCryptKeyForBlockDecryption(
		ctx, rmd, BlockPointer{KeyGen: 1})
	require.NoError(t, err)
	require.Equal(t, storedTLFCryptKey1, tlfCryptKey)
}

func testKeyManagerRekeySuccessPrivate(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	oldKeyGen := rmd.LatestKeyGeneration()

	tlfCryptKey := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	expectRekey(config, h.ToBareHandleOrBust(), 1, false, true, tlfCryptKey)

	if done, _, err := config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Errorf("Got error on rekey: %t, %+v", done, err)
	} else if rmd.LatestKeyGeneration() != oldKeyGen+1 {
		t.Errorf("Bad key generation after rekey: %d", rmd.LatestKeyGeneration())
	}
}

func testKeyManagerRekeyResolveAgainSuccessPublic(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, true)
	h, err := ParseTlfHandle(
		ctx, config.KBPKI(), "alice,bob@twitter", true)
	require.NoError(t, err)
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")

	config.mockMdops.EXPECT().GetLatestHandleForTLF(gomock.Any(), gomock.Any()).
		Return(rmd.tlfHandle.ToBareHandleOrBust(), nil)

	done, cryptKey, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.True(t, done)
	require.Nil(t, cryptKey)
	require.NoError(t, err)

	newH := rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,bob"), newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	oldHandle := rmd.tlfHandle
	rmd.tlfHandle = nil
	newBareH, err := rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
	rmd.tlfHandle = oldHandle

	// Rekey again, which shouldn't do anything.
	done, cryptKey, err = config.KeyManager().Rekey(ctx, rmd, false)
	require.False(t, done)
	require.Nil(t, cryptKey)
	require.NoError(t, err)
}

func testKeyManagerRekeyResolveAgainSuccessPublicSelf(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, true)
	h, err := ParseTlfHandle(
		ctx, config.KBPKI(), "alice@twitter,bob,charlie@twitter", true)
	require.NoError(t, err)
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("alice", "alice@twitter")
	daemon.addNewAssertionForTestOrBust("charlie", "charlie@twitter")

	config.mockMdops.EXPECT().GetLatestHandleForTLF(gomock.Any(), gomock.Any()).
		Return(rmd.tlfHandle.ToBareHandleOrBust(), nil)

	done, cryptKey, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.True(t, done)
	require.Nil(t, cryptKey)
	require.NoError(t, err)

	newH := rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,bob,charlie"), newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	oldHandle := rmd.tlfHandle
	rmd.tlfHandle = nil
	newBareH, err := rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
	rmd.tlfHandle = oldHandle
}

func testKeyManagerRekeyResolveAgainSuccessPrivate(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(
		ctx, config.KBPKI(), "alice,bob@twitter,dave@twitter#charlie@twitter",
		false)
	if err != nil {
		t.Fatal(err)
	}
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	oldKeyGen := rmd.LatestKeyGeneration()

	tlfCryptKey1 := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	expectRekey(config, h.ToBareHandleOrBust(), 3, true, true, tlfCryptKey1)

	// Pretend that {bob,charlie}@twitter now resolve to {bob,charlie}.
	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")
	daemon.addNewAssertionForTestOrBust("charlie", "charlie@twitter")

	if done, _, err := config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen+1 {
		t.Fatalf("Bad key generation after rekey: %d", rmd.LatestKeyGeneration())
	}

	newH := rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,bob,dave@twitter#charlie"),
		newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	oldHandle := rmd.tlfHandle
	rmd.tlfHandle = nil
	newBareH, err := rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
	rmd.tlfHandle = oldHandle

	// Now resolve using only a device addition, which won't bump the
	// generation number.
	daemon.addNewAssertionForTestOrBust("dave", "dave@twitter")
	oldKeyGen = rmd.LatestKeyGeneration()

	tlfCryptKey2 := kbfscrypto.MakeTLFCryptKey([32]byte{0x2})
	config.KeyCache().PutTLFCryptKey(id, 1, tlfCryptKey2)

	expectRekey(config, oldHandle.ToBareHandleOrBust(), 1, true, false, tlfCryptKey2)
	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	config.mockKbpki.EXPECT().GetCryptPublicKeys(gomock.Any(), gomock.Any()).
		Return([]kbfscrypto.CryptPublicKey{subkey}, nil).Times(3)
	if done, _, err :=
		config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen {
		t.Fatalf("Bad key generation after rekey: %d",
			rmd.LatestKeyGeneration())
	}

	newH = rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,bob,dave#charlie"),
		newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	rmd.tlfHandle = nil
	newBareH, err = rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
}

func testKeyManagerPromoteReaderSuccess(t *testing.T, ver MetadataVer) {
	ctx := context.Background()

	config := MakeTestConfigOrBust(t, "alice", "bob")
	defer CheckConfigAndShutdown(ctx, t, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(ctx, config.KBPKI(),
		"alice,bob@twitter#bob", false)
	require.NoError(t, err)

	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	// Make the first key generation.
	done, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, done)

	aliceUID := keybase1.MakeTestUID(1)
	bobUID := keybase1.MakeTestUID(2)

	isWriter, _, err := rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.False(t, isWriter)

	oldKeyGen := rmd.LatestKeyGeneration()

	// Pretend that bob@twitter now resolves to bob.
	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")

	// Make the second key generation.
	done, _, err = config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, done)

	// Reader promotion shouldn't increase the key generation.
	require.Equal(t, oldKeyGen, rmd.LatestKeyGeneration())

	isWriter, _, err = rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.True(t, isWriter)

	newH := rmd.GetTlfHandle()
	require.Equal(t,
		CanonicalTlfName("alice,bob"),
		newH.GetCanonicalName())
}

func testKeyManagerPromoteReaderSelf(t *testing.T, ver MetadataVer) {
	ctx := context.Background()

	config := MakeTestConfigOrBust(t, "alice", "bob")
	defer CheckConfigAndShutdown(ctx, t, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(ctx, config.KBPKI(),
		"alice,bob@twitter#bob", false)
	require.NoError(t, err)

	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	// Make the first key generation.
	done, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, done)

	aliceUID := keybase1.MakeTestUID(1)
	bobUID := keybase1.MakeTestUID(2)

	isWriter, _, err := rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.False(t, isWriter)

	oldKeyGen := rmd.LatestKeyGeneration()

	config2 := ConfigAsUser(config, "bob")

	// Pretend that bob@twitter now resolves to bob.
	daemon := config2.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")

	// Make the second key generation as bob, which should still
	// succeed.
	done, _, err = config2.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, done)

	// Reader promotion shouldn't increase the key generation.
	require.Equal(t, oldKeyGen, rmd.LatestKeyGeneration())

	isWriter, _, err = rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.True(t, isWriter)

	newH := rmd.GetTlfHandle()
	require.Equal(t,
		CanonicalTlfName("alice,bob"),
		newH.GetCanonicalName())
}

func testKeyManagerPromoteReaderFailure2Private(t *testing.T, ver MetadataVer) {
	ctx := context.Background()

	config := MakeTestConfigOrBust(t, "alice", "bob", "charlie")
	defer CheckConfigAndShutdown(ctx, t, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(ctx, config.KBPKI(),
		"bob,charlie@twitter#alice,charlie", false)
	require.NoError(t, err)

	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	// Make the first key generation.
	done, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, done)

	aliceUID := keybase1.MakeTestUID(1)
	bobUID := keybase1.MakeTestUID(2)
	charlieUID := keybase1.MakeTestUID(3)

	isWriter, _, err := rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.False(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(charlieUID)
	require.NoError(t, err)
	require.False(t, isWriter)

	// Pretend that charlie@twitter now resolves to charlie.
	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("charlie", "charlie@twitter")

	AddDeviceForLocalUserOrBust(t, config, aliceUID)

	// Try to make the second key generation, which should success
	// partially.
	done, _, err = config.KeyManager().Rekey(ctx, rmd, false)
	require.IsType(t, err, RekeyIncompleteError{})
	require.True(t, done)

	isWriter, _, err = rmd.GetDevicePublicKeys(aliceUID)
	require.NoError(t, err)
	require.False(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(bobUID)
	require.NoError(t, err)
	require.True(t, isWriter)
	isWriter, _, err = rmd.GetDevicePublicKeys(charlieUID)
	require.NoError(t, err)
	require.False(t, isWriter)
}

func testKeyManagerReaderRekeyResolveAgainSuccessPrivate(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(ctx, config.KBPKI(),
		"alice,dave@twitter#bob@twitter,charlie@twitter", false)
	if err != nil {
		t.Fatal(err)
	}
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	oldKeyGen := rmd.LatestKeyGeneration()

	tlfCryptKey1 := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	expectRekey(config, h.ToBareHandleOrBust(), 1, true, true, tlfCryptKey1)

	// Make the first key generation
	if done, _, err := config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen+1 {
		t.Fatalf("Bad key generation after rekey: %d", rmd.LatestKeyGeneration())
	}

	newH := rmd.GetTlfHandle()
	require.Equal(t,
		CanonicalTlfName("alice,dave@twitter#bob@twitter,charlie@twitter"),
		newH.GetCanonicalName())

	// Now resolve everyone, but have reader bob to do the rekey
	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")
	daemon.addNewAssertionForTestOrBust("charlie", "charlie@twitter")
	daemon.addNewAssertionForTestOrBust("dave", "dave@twitter")

	_, bobUID, err := daemon.Resolve(ctx, "bob")
	daemon.setCurrentUID(bobUID)

	// Now resolve using only a device addition, which won't bump the
	// generation number.
	oldKeyGen = rmd.LatestKeyGeneration()
	// Pretend bob has the key in the cache (in reality it would be
	// decrypted via bob's paper key)

	tlfCryptKey2 := kbfscrypto.MakeTLFCryptKey([32]byte{0x2})
	config.KeyCache().PutTLFCryptKey(rmd.TlfID(), oldKeyGen, tlfCryptKey2)

	expectRekey(config, h.ToBareHandleOrBust(), 1, false, false, tlfCryptKey2)
	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	config.mockKbpki.EXPECT().GetCryptPublicKeys(gomock.Any(), gomock.Any()).
		Return([]kbfscrypto.CryptPublicKey{subkey}, nil)
	if done, _, err :=
		config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen {
		t.Fatalf("Bad key generation after rekey: %d",
			rmd.LatestKeyGeneration())
	}

	// bob shouldn't have been able to resolve other users since he's
	// just a reader.
	newH = rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,dave@twitter#bob,charlie@twitter"),
		newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	rmd.tlfHandle = nil
	newBareH, err := rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
}

func testKeyManagerRekeyResolveAgainNoChangeSuccessPrivate(t *testing.T, ver MetadataVer) {
	mockCtrl, config, ctx := keyManagerInit(t, ver)
	defer keyManagerShutdown(mockCtrl, config)

	id := tlf.FakeID(1, false)
	h, err := ParseTlfHandle(ctx, config.KBPKI(), "alice,bob,bob@twitter",
		false)
	if err != nil {
		t.Fatal(err)
	}
	rmd, err := makeInitialRootMetadata(config.MetadataVersion(), id, h)
	require.NoError(t, err)

	oldKeyGen := rmd.LatestKeyGeneration()

	tlfCryptKey1 := kbfscrypto.MakeTLFCryptKey([32]byte{0x1})
	expectRekey(config, h.ToBareHandleOrBust(), 2, true, true, tlfCryptKey1)

	// Make the first key generation
	if done, _, err := config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen+1 {
		t.Fatalf("Bad key generation after rekey: %d", rmd.LatestKeyGeneration())
	}

	newH := rmd.GetTlfHandle()
	require.Equal(t,
		CanonicalTlfName("alice,bob,bob@twitter"),
		newH.GetCanonicalName())

	// Now resolve everyone, but have reader bob to do the rekey
	daemon := config.KeybaseService().(*KeybaseDaemonLocal)
	daemon.addNewAssertionForTestOrBust("bob", "bob@twitter")

	// Now resolve which gets rid of the unresolved writers, but
	// doesn't otherwise change the handle since bob is already in it.
	oldKeyGen = rmd.LatestKeyGeneration()
	config.mockCrypto.EXPECT().MakeRandomTLFEphemeralKeys().Return(
		kbfscrypto.TLFEphemeralPublicKey{},
		kbfscrypto.TLFEphemeralPrivateKey{}, nil)

	subkey := kbfscrypto.MakeFakeCryptPublicKeyOrBust("crypt public key")
	config.mockKbpki.EXPECT().GetCryptPublicKeys(gomock.Any(), gomock.Any()).
		Return([]kbfscrypto.CryptPublicKey{subkey}, nil).Times(2)
	if done, _, err :=
		config.KeyManager().Rekey(ctx, rmd, false); !done || err != nil {
		t.Fatalf("Got error on rekey: %t, %+v", done, err)
	}

	if rmd.LatestKeyGeneration() != oldKeyGen {
		t.Fatalf("Bad key generation after rekey: %d",
			rmd.LatestKeyGeneration())
	}

	// bob shouldn't have been able to resolve other users since he's
	// just a reader.
	newH = rmd.GetTlfHandle()
	require.Equal(t, CanonicalTlfName("alice,bob"), newH.GetCanonicalName())

	// Also check MakeBareTlfHandle.
	rmd.tlfHandle = nil
	newBareH, err := rmd.MakeBareTlfHandle()
	require.NoError(t, err)
	require.Equal(t, newH.ToBareHandleOrBust(), newBareH)
}

func testKeyManagerRekeyAddAndRevokeDevice(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)
	clock := newTestClockNow()
	config1.SetClock(clock)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a shared folder
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	rootNode2 := GetRootNodeOrBust(ctx, t, config2, name, false)

	kbfsOps2 := config2.KBFSOps()

	// user 2 creates a file
	_, _, err = kbfsOps2.CreateFile(ctx, rootNode2, "b", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	// Now give u2 a new device.  The configs don't share a Keybase
	// Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	// user 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	// Set the KBPKI so we can count the identify calls
	countKBPKI := &identifyCountingKBPKI{
		KBPKI: config1.KBPKI(),
	}
	config1.SetKBPKI(countKBPKI)
	// Force the FBO to forget about its previous identify, so that we
	// can make sure the rekey doesn't trigger a full identify.
	kbfsOps1.(*KBFSOpsStandard).getOpsNoAdd(
		rootNode1.GetFolderBranch()).identifyDone = false

	// now user 1 should rekey
	err = kbfsOps1.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// Only u2 should be identified as part of the rekey.
	if g, e := countKBPKI.getIdentifyCalls(), 1; g != e {
		t.Errorf("Expected %d identify calls, but got %d", e, g)
	}

	// u2 syncs after the rekey
	if err := kbfsOps2.SyncFromServerForTesting(ctx,
		rootNode2.GetFolderBranch()); err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// user 2 creates another file
	_, _, err = kbfsOps2.CreateFile(ctx, rootNode2, "c", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	// add a third device for user 2
	config2Dev3 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev3)
	defer config2Dev3.SetKeyCache(NewKeyCacheStandard(5000))
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	devIndex = AddDeviceForLocalUserOrBust(t, config2Dev3, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev3, devIndex)

	// Now revoke the original user 2 device (the last writer)
	clock.Add(1 * time.Minute)
	RevokeDeviceForLocalUserOrBust(t, config1, uid2, 0)
	RevokeDeviceForLocalUserOrBust(t, config2Dev2, uid2, 0)
	RevokeDeviceForLocalUserOrBust(t, config2Dev3, uid2, 0)

	// First request a rekey from the new device, which will only be
	// able to set the rekey bit (copying the root MD).
	err = config2Dev3.KBFSOps().Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	err = kbfsOps1.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// rekey again
	err = kbfsOps1.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// Only u2 should be identified again as part of the rekey.
	if g, e := countKBPKI.getIdentifyCalls(), 2; g != e {
		t.Errorf("Expected %d identify calls, but got %d", e, g)
	}

	// force re-encryption of the root dir
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "d", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	// this device should be able to read now
	root2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.SyncFromServerForTesting(ctx, root2Dev2.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// device 2 should still work
	rootNode2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	children, err := kbfsOps2Dev2.GetDirChildren(ctx, rootNode2Dev2)
	if _, ok := children["d"]; !ok {
		t.Fatalf("Device 2 couldn't see the new dir entry")
	}

	// But device 1 should now fail to see any updates.  TODO: when a
	// device sees it has been revoked from the TLF, we should delete
	// all its cached data and refuse to serve any more.  (However, in
	// production the device's session would likely be revoked,
	// probably leading to NoCurrentSession errors anyway.)
	err = kbfsOps2.SyncFromServerForTesting(ctx, rootNode2.GetFolderBranch())
	if err == nil {
		// This is not expected to succeed; the node will be unable to
		// deserialize the private MD.
		t.Fatalf("Unexpectedly could sync from server")
	}
	// Should still be seeing the old children, since the updates from
	// the latest revision were never applied.
	children, err = kbfsOps2.GetDirChildren(ctx, rootNode2)
	if _, ok := children["d"]; ok {
		t.Fatalf("Found c unexpectedly: %v", children)
	}

	// meanwhile, device 3 should be able to read both the new and the
	// old files
	rootNode2Dev3 := GetRootNodeOrBust(ctx, t, config2Dev3, name, false)

	kbfsOps2Dev3 := config2Dev3.KBFSOps()
	aNode, _, err := kbfsOps2Dev3.Lookup(ctx, rootNode2Dev3, "a")
	if err != nil {
		t.Fatalf("Device 3 couldn't lookup a: %+v", err)
	}

	buf := []byte{0}
	_, err = kbfsOps2Dev3.Read(ctx, aNode, buf, 0)
	if err != nil {
		t.Fatalf("Device 3 couldn't read a: %+v", err)
	}

	bNode, _, err := kbfsOps2Dev3.Lookup(ctx, rootNode2Dev3, "b")
	if err != nil {
		t.Fatalf("Device 3 couldn't lookup b: %+v", err)
	}

	_, err = kbfsOps2Dev3.Read(ctx, bNode, buf, 0)
	if err != nil {
		t.Fatalf("Device 3 couldn't read b: %+v", err)
	}

	// Make sure the server-side keys for the revoked device are gone
	// for all keygens.
	rmd, err := config1.MDOps().GetForTLF(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't get latest md: %+v", err)
	}
	currKeyGen := rmd.LatestKeyGeneration()
	// clear the key cache
	config2.SetKeyCache(NewKeyCacheStandard(5000))
	km2, ok := config2.KeyManager().(*KeyManagerStandard)
	if !ok {
		t.Fatal("Wrong kind of key manager for config2")
	}
	for keyGen := FirstValidKeyGen; keyGen <= currKeyGen; keyGen++ {
		_, err = km2.getTLFCryptKeyUsingCurrentDevice(ctx, rmd.ReadOnly(), keyGen, true)
		if err == nil {
			t.Errorf("User 2 could still fetch a key for keygen %d", keyGen)
		}
	}
}

func testKeyManagerRekeyAddWriterAndReaderDevice(t *testing.T, ver MetadataVer) {
	var u1, u2, u3 libkb.NormalizedUsername = "u1", "u2", "u3"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2, u3)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)

	config1.SetMetadataVersion(ver)

	// Revoke user 3's device for now, to test the "other" rekey error.
	_, uid3, err := config1.KBPKI().Resolve(ctx, u3.String())
	if err != nil {
		t.Fatalf("Couldn't resolve u3: %+v", err)
	}
	RevokeDeviceForLocalUserOrBust(t, config1, uid3, 0)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a shared folder
	name := u1.String() + "," + u2.String() + ReaderSep + u3.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	config3 := ConfigAsUser(config1, u3)
	defer CheckConfigAndShutdown(ctx, t, config3)

	// Now give u2 and u3 new devices.  The configs don't share a
	// Keybase Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)
	AddDeviceForLocalUserOrBust(t, config1, uid3)
	AddDeviceForLocalUserOrBust(t, config2, uid3)
	devIndex = AddDeviceForLocalUserOrBust(t, config3, uid3)
	t.Logf("Switching to device %d", devIndex)
	SwitchDeviceForLocalUserOrBust(t, config3, devIndex)

	// Users 2 and 3 should be unable to read the data now since its
	// device wasn't registered when the folder was originally
	// created.
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}
	_, err = GetRootNodeForTest(ctx, config3, name, false)
	if _, ok := err.(NeedOtherRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	// Set the KBPKI so we can count the identify calls
	countKBPKI := &identifyCountingKBPKI{
		KBPKI: config1.KBPKI(),
	}
	config1.SetKBPKI(countKBPKI)
	// Force the FBO to forget about its previous identify, so that we
	// can make sure the rekey doesn't trigger a full identify.
	kbfsOps1.(*KBFSOpsStandard).getOpsNoAdd(
		rootNode1.GetFolderBranch()).identifyDone = false

	// now user 1 should rekey
	err = kbfsOps1.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// u2 and u3 should be identified as part of the rekey.
	if g, e := countKBPKI.getIdentifyCalls(), 2; g != e {
		t.Errorf("Expected %d identify calls, but got %d", e, g)
	}

	// The new devices should be able to read now.
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if err != nil {
		t.Fatalf("Got unexpected error after rekey: %+v", err)
	}

	_ = GetRootNodeOrBust(ctx, t, config3, name, false)
}

func testKeyManagerSelfRekeyAcrossDevices(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Create a shared folder")
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	t.Log("User 1 creates a file")
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	t.Log("User 2 adds a device")
	// The configs don't share a Keybase Daemon so we have to do it in all
	// places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2, uid2)

	config2Dev2 := ConfigAsUser(config2, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	t.Log("Check that user 2 device 2 is unable to read the file")
	// user 2 device 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	t.Log("User 2 rekeys from device 1")
	root2dev1 := GetRootNodeOrBust(ctx, t, config2, name, false)

	kbfsOps2 := config2.KBFSOps()
	err = kbfsOps2.Rekey(ctx, root2dev1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	t.Log("User 2 device 2 should be able to read now")
	root2dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	t.Log("User 2 device 2 reads user 1's file")
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	children2, err := kbfsOps2Dev2.GetDirChildren(ctx, root2dev2)
	if _, ok := children2["a"]; !ok {
		t.Fatalf("Device 2 couldn't see user 1's dir entry")
	}

	t.Log("User 2 device 2 creates a file")
	_, _, err = kbfsOps2Dev2.CreateFile(ctx, root2dev2, "b", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	t.Log("User 1 syncs from the server")
	err = kbfsOps1.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	t.Log("User 1 should be able to read the file that user 2 device 2 created")
	children1, err := kbfsOps1.GetDirChildren(ctx, rootNode1)
	if _, ok := children1["b"]; !ok {
		t.Fatalf("Device 1 couldn't see the new dir entry")
	}
}

func testKeyManagerReaderRekey(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)
	_, uid1, err := config1.KBPKI().GetCurrentUserInfo(ctx)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Create a shared folder")
	name := u1.String() + ReaderSep + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	t.Log("User 1 creates a file")
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	t.Log("User 1 adds a device")
	// The configs don't share a Keybase Daemon so we have to do it in all
	// places.
	AddDeviceForLocalUserOrBust(t, config1, uid1)
	devIndex := AddDeviceForLocalUserOrBust(t, config2, uid1)

	config1Dev2 := ConfigAsUser(config2, u1)
	defer CheckConfigAndShutdown(ctx, t, config1Dev2)
	SwitchDeviceForLocalUserOrBust(t, config1Dev2, devIndex)

	t.Log("User 2 adds a device")
	// The configs don't share a Keybase Daemon so we have to do it in all
	// places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config1Dev2, uid2)
	devIndex = AddDeviceForLocalUserOrBust(t, config2, uid2)

	config2Dev2 := ConfigAsUser(config2, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	t.Log("Check that user 2 device 2 is unable to read the file")
	// user 2 device 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	t.Log("User 2 rekeys from device 1")
	root2dev1 := GetRootNodeOrBust(ctx, t, config2, name, false)

	kbfsOps2 := config2.KBFSOps()
	err = kbfsOps2.Rekey(ctx, root2dev1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Expected reader rekey to partially complete. Actual error: %#v", err)
	}

	t.Log("User 2 device 2 should be able to read now")
	root2dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	t.Log("User 1 device 2 should still be unable to read")
	_, err = GetRootNodeForTest(ctx, config1Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	t.Log("User 2 device 2 reads user 1's file")
	children2, err := kbfsOps2Dev2.GetDirChildren(ctx, root2dev2)
	if _, ok := children2["a"]; !ok {
		t.Fatalf("Device 2 couldn't see user 1's dir entry")
	}
}

func testKeyManagerReaderRekeyAndRevoke(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)
	clock := newTestClockNow()
	config1.SetClock(clock)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The reader has a second device at the start.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2, uid2)
	config2Dev2 := ConfigAsUser(config2, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	t.Log("Create a shared folder")
	name := u1.String() + ReaderSep + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	t.Log("User 1 creates a file")
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	t.Log("User 2 adds a device")
	// The configs don't share a Keybase Daemon so we have to do it in all
	// places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex = AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	config2Dev3 := ConfigAsUser(config2, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev3)
	SwitchDeviceForLocalUserOrBust(t, config2Dev3, devIndex)

	// Revoke the original user 2 device
	clock.Add(1 * time.Minute)
	RevokeDeviceForLocalUserOrBust(t, config1, uid2, 0)
	RevokeDeviceForLocalUserOrBust(t, config2Dev2, uid2, 0)
	RevokeDeviceForLocalUserOrBust(t, config2Dev3, uid2, 0)

	t.Log("Check that user 2 device 3 is unable to read the file")
	// user 2 device 3 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	_, err = GetRootNodeForTest(ctx, config2Dev3, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	t.Log("User 2 rekeys from device 2")
	root2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.Rekey(ctx, root2Dev2.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Expected reader rekey to partially complete. "+
			"Actual error: %#v", err)
	}

	t.Log("User 2 device 3 should be able to read now")
	GetRootNodeOrBust(ctx, t, config2Dev3, name, false)

	// A second rekey by the same reader shouldn't change the
	// revision, since the rekey bit is already set, even though a
	// rekey is still needed (due to the revoke, which has to be
	// rekeyed by a writer).
	ops := getOps(config2Dev2, root2Dev2.GetFolderBranch().Tlf)
	rev1 := ops.head.Revision()
	err = kbfsOps2Dev2.Rekey(ctx, root2Dev2.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Expected reader rekey to partially complete. "+
			"Actual error: %#v", err)
	}
	rev2 := ops.head.Revision()
	if rev1 != rev2 {
		t.Fatalf("Reader rekey made two incomplete rekeys in a row.")
	}
}

// This tests 2 variations of the situation where clients w/o the folder key set the rekey bit.
// In one case the client is a writer and in the other a reader. They both blindly copy the existing
// metadata and simply set the rekey bit. Then another participant rekeys the folder and they try to read.

func testKeyManagerRekeyBit(t *testing.T, ver MetadataVer) {
	var u1, u2, u3 libkb.NormalizedUsername = "u1", "u2", "u3"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2, u3)
	doShutdown1 := true
	defer func() {
		if doShutdown1 {
			kbfsConcurTestShutdown(t, config1, ctx, cancel)
		}
	}()

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config2.MDServer().DisableRekeyUpdatesForTesting()

	config3 := ConfigAsUser(config1, u3)
	defer CheckConfigAndShutdown(ctx, t, config3)
	_, uid3, err := config3.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config3.MDServer().DisableRekeyUpdatesForTesting()

	// 2 writers 1 reader
	name := u1.String() + "," + u2.String() + "#" + u3.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	// we don't check the config because this device can't read all of the md blocks.
	defer config2Dev2.Shutdown(ctx)
	config2Dev2.MDServer().DisableRekeyUpdatesForTesting()

	// Now give u2 a new device.  The configs don't share a Keybase
	// Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	AddDeviceForLocalUserOrBust(t, config3, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	// user 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	// now user 2 should set the rekey bit
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// user 1 syncs from server
	err = kbfsOps1.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// user 1 should try to rekey
	err = kbfsOps1.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// user 2 syncs from server
	err = kbfsOps2Dev2.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// this device should be able to read now
	rootNode2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	// look for the file
	aNode, _, err := kbfsOps2Dev2.Lookup(ctx, rootNode2Dev2, "a")
	if err != nil {
		t.Fatalf("Device 2 couldn't lookup a: %+v", err)
	}

	// read it
	buf := []byte{0}
	_, err = kbfsOps2Dev2.Read(ctx, aNode, buf, 0)
	if err != nil {
		t.Fatalf("Device 2 couldn't read a: %+v", err)
	}

	config3Dev2 := ConfigAsUser(config1, u3)
	// we don't check the config because this device can't read all of the md blocks.
	defer config3Dev2.Shutdown(ctx)
	config3Dev2.MDServer().DisableRekeyUpdatesForTesting()

	// Now give u3 a new device.
	AddDeviceForLocalUserOrBust(t, config1, uid3)
	AddDeviceForLocalUserOrBust(t, config2, uid3)
	AddDeviceForLocalUserOrBust(t, config2Dev2, uid3)
	AddDeviceForLocalUserOrBust(t, config3, uid3)
	devIndex = AddDeviceForLocalUserOrBust(t, config3Dev2, uid3)
	SwitchDeviceForLocalUserOrBust(t, config3Dev2, devIndex)

	// user 3 dev 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	_, err = GetRootNodeForTest(ctx, config3Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	// now user 3 dev 2 should set the rekey bit
	kbfsOps3Dev2 := config3Dev2.KBFSOps()
	err = kbfsOps3Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// user 2 dev 2 syncs from server
	err = kbfsOps2Dev2.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// user 2 dev 2 should try to rekey
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// user 3 dev 2 syncs from server
	err = kbfsOps3Dev2.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// this device should be able to read now
	rootNode3Dev2 := GetRootNodeOrBust(ctx, t, config3Dev2, name, false)

	// look for the file
	a2Node, _, err := kbfsOps3Dev2.Lookup(ctx, rootNode3Dev2, "a")
	if err != nil {
		t.Fatalf("Device 3 couldn't lookup a: %+v", err)
	}

	// read it
	buf = []byte{0}
	_, err = kbfsOps3Dev2.Read(ctx, a2Node, buf, 0)
	if err != nil {
		t.Fatalf("Device 3 couldn't read a: %+v", err)
	}

	// Explicitly run the checks with config1 before the deferred shutdowns begin.
	// This way the shared mdserver hasn't been shutdown.
	kbfsConcurTestShutdown(t, config1, ctx, cancel)
	doShutdown1 = false
}

// Two devices conflict when revoking a 3rd device.
// Test that after this both can still read the latest version of the folder.

func testKeyManagerRekeyAddAndRevokeDeviceWithConflict(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)
	clock := newTestClockNow()
	config1.SetClock(clock)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// create a shared folder
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	// give user 2 a new device
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	// user 2 should be unable to read the data now since its device
	// wasn't registered when the folder was originally created.
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	_, err = GetRootNodeForTest(ctx, config2Dev2, name, false)
	if _, ok := err.(NeedSelfRekeyError); !ok {
		t.Fatalf("Got unexpected error when reading with new key: %+v", err)
	}

	// now user 1 should rekey
	err = kbfsOps1.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// this device should be able to read now
	root2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	// Now revoke the original user 2 device
	clock.Add(1 * time.Minute)
	RevokeDeviceForLocalUserOrBust(t, config1, uid2, 0)
	RevokeDeviceForLocalUserOrBust(t, config2Dev2, uid2, 0)

	// Stall user 1's rekey, to ensure a conflict.
	onPutStalledCh, putUnstallCh, putCtx :=
		StallMDOp(ctx, config1, StallableMDPut, 1)

	// Have user 1 also try to rekey but fail due to conflict
	errChan := make(chan error, 1)
	go func() {
		errChan <- kbfsOps1.Rekey(putCtx, rootNode1.GetFolderBranch().Tlf)
	}()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-onPutStalledCh:
	}

	// rekey again but with user 2 device 2
	err = kbfsOps2Dev2.Rekey(ctx, root2Dev2.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Couldn't rekey: %+v", err)
	}

	// Make sure user 1's rekey failed.
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case putUnstallCh <- struct{}{}:
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case err = <-errChan:
	}
	if _, isConflict := err.(RekeyConflictError); !isConflict {
		t.Fatalf("Expected failure due to conflict")
	}

	err = kbfsOps2Dev2.SyncFromServerForTesting(ctx, root2Dev2.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	// force re-encryption of the root dir
	_, _, err = kbfsOps2Dev2.CreateFile(ctx, root2Dev2, "b", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	// device 1 should still work
	err = kbfsOps1.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}

	rootNode1 = GetRootNodeOrBust(ctx, t, config1, name, false)

	children, err := kbfsOps1.GetDirChildren(ctx, rootNode1)
	if _, ok := children["b"]; !ok {
		t.Fatalf("Device 1 couldn't see the new dir entry")
	}
}

// cryptoLocalTrapAny traps every DecryptTLFCryptKeyClientHalfAny
// call, and closes the given channel the first time it receives one
// with promptPaper set to true.
type cryptoLocalTrapAny struct {
	Crypto
	promptPaperChOnce sync.Once
	promptPaperCh     chan<- struct{}
	cryptoToUse       Crypto
}

func (clta *cryptoLocalTrapAny) DecryptTLFCryptKeyClientHalfAny(
	ctx context.Context,
	keys []EncryptedTLFCryptKeyClientAndEphemeral, promptPaper bool) (
	kbfscrypto.TLFCryptKeyClientHalf, int, error) {
	if promptPaper {
		clta.promptPaperChOnce.Do(func() {
			close(clta.promptPaperCh)
		})
	}
	return clta.cryptoToUse.DecryptTLFCryptKeyClientHalfAny(
		ctx, keys, promptPaper)
}

func testKeyManagerRekeyAddDeviceWithPrompt(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a shared folder
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	config2Dev2.SetKeyCache(&dummyNoKeyCache{})

	// Now give u2 a new device.  The configs don't share a Keybase
	// Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	t.Log("Doing first rekey")

	// The new device should be unable to rekey on its own, and will
	// just set the rekey bit.
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("First rekey failed %+v", err)
	}

	ops := getOps(config2Dev2, rootNode1.GetFolderBranch().Tlf)
	rev1 := ops.head.Revision()

	t.Log("Doing second rekey")

	// Do it again, to simulate the mdserver sending back this node's
	// own rekey request.  This shouldn't increase the MD version.
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Second rekey failed %+v", err)
	}
	rev2 := ops.head.Revision()

	if rev1 != rev2 {
		t.Errorf("Revision changed after second rekey: %v vs %v", rev1, rev2)
	}

	// Make sure just the rekey bit is set
	if !ops.head.IsRekeySet() {
		t.Fatalf("Couldn't set rekey bit")
	}

	t.Log("Switching crypto")

	c := make(chan struct{}, 1)
	// Use our other device as a standin for the paper key.
	clta := &cryptoLocalTrapAny{config2Dev2.Crypto(), sync.Once{}, c, config2.Crypto()}
	config2Dev2.SetCrypto(clta)

	ops.rekeyWithPromptTimer.Reset(1 * time.Millisecond)
	select {
	case <-c:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// Take the mdWriterLock to ensure that the rekeyWithPrompt finishes.
	lState := makeFBOLockState()
	ops.mdWriterLock.Lock(lState)
	ops.mdWriterLock.Unlock(lState)

	config2Dev2.SetCrypto(clta.Crypto)

	rootNode2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	kbfsOps2 := config2Dev2.KBFSOps()
	children, err := kbfsOps2.GetDirChildren(ctx, rootNode2Dev2)
	if _, ok := children["a"]; !ok {
		t.Fatalf("Device 2 couldn't see the dir entry after rekey")
	}
	// user 2 creates another file to make a new revision
	_, _, err = kbfsOps2.CreateFile(ctx, rootNode2Dev2, "b", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	// device 1 should be able to read the new file
	err = kbfsOps1.SyncFromServerForTesting(ctx, rootNode1.GetFolderBranch())
	if err != nil {
		t.Fatalf("Couldn't sync from server: %+v", err)
	}
	children, err = kbfsOps1.GetDirChildren(ctx, rootNode1)
	if _, ok := children["b"]; !ok {
		t.Fatalf("Device 2 couldn't see the dir entry after rekey")
	}
}

func testKeyManagerRekeyAddDeviceWithPromptAfterRestart(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, uid1, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)
	clock := newTestClockNow()
	config1.SetClock(clock)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a shared folder
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)

	kbfsOps1 := config1.KBFSOps()

	// user 1 creates a file
	_, _, err = kbfsOps1.CreateFile(ctx, rootNode1, "a", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}

	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	config2Dev2.SetKeyCache(&dummyNoKeyCache{})

	// Now give u2 a new device.  The configs don't share a Keybase
	// Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)
	// Revoke some previous device
	clock.Add(1 * time.Minute)
	RevokeDeviceForLocalUserOrBust(t, config2Dev2, uid1, 0)

	t.Log("Doing first rekey")

	// The new device should be unable to rekey on its own, and will
	// just set the rekey bit.
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("First rekey failed %+v", err)
	}

	ops := getOps(config2Dev2, rootNode1.GetFolderBranch().Tlf)
	rev1 := ops.head.Revision()

	t.Log("Doing second rekey")

	// Do it again, to simulate the mdserver sending back this node's
	// own rekey request.  This shouldn't increase the MD version.
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Second rekey failed %+v", err)
	}
	rev2 := ops.head.Revision()

	if rev1 != rev2 {
		t.Errorf("Revision changed after second rekey: %v vs %v", rev1, rev2)
	}

	// Make sure just the rekey bit is set
	if !ops.head.IsRekeySet() {
		t.Fatalf("Couldn't set rekey bit")
	}

	// Simulate a restart by clearing the timer after the rekey bit was set
	ops.rekeyWithPromptTimer.Stop()
	ops.rekeyWithPromptTimer = nil

	t.Log("Doing third rekey")

	// Try again, which should reset the timer (and so the Reset below
	// will be on a non-nil timer).
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("Third rekey failed %+v", err)
	}

	t.Log("Switching crypto")

	c := make(chan struct{}, 1)
	// Use our other device as a standin for the paper key.
	clta := &cryptoLocalTrapAny{config2Dev2.Crypto(), sync.Once{}, c, config2.Crypto()}
	config2Dev2.SetCrypto(clta)

	ops.rekeyWithPromptTimer.Reset(1 * time.Millisecond)
	select {
	case <-c:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// Take the mdWriterLock to ensure that the rekeyWithPrompt finishes.
	lState := makeFBOLockState()
	ops.mdWriterLock.Lock(lState)
	ops.mdWriterLock.Unlock(lState)

	config2Dev2.SetCrypto(clta.Crypto)

	rootNode2Dev2 := GetRootNodeOrBust(ctx, t, config2Dev2, name, false)

	kbfsOps2 := config2Dev2.KBFSOps()
	children, err := kbfsOps2.GetDirChildren(ctx, rootNode2Dev2)
	if _, ok := children["a"]; !ok {
		t.Fatalf("Device 2 couldn't see the dir entry after rekey")
	}
	// user 2 creates another file to make a new revision
	_, _, err = kbfsOps2.CreateFile(ctx, rootNode2Dev2, "b", false, NoExcl)
	if err != nil {
		t.Fatalf("Couldn't create file: %+v", err)
	}
}

func testKeyManagerRekeyAddDeviceWithPromptViaFolderAccess(t *testing.T, ver MetadataVer) {
	var u1, u2 libkb.NormalizedUsername = "u1", "u2"
	config1, _, ctx, cancel := kbfsOpsConcurInit(t, u1, u2)
	defer kbfsConcurTestShutdown(t, config1, ctx, cancel)

	config1.SetMetadataVersion(ver)

	config2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2)
	_, uid2, err := config2.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a shared folder
	name := u1.String() + "," + u2.String()

	rootNode1 := GetRootNodeOrBust(ctx, t, config1, name, false)
	config2Dev2 := ConfigAsUser(config1, u2)
	defer CheckConfigAndShutdown(ctx, t, config2Dev2)

	config2Dev2.SetKeyCache(&dummyNoKeyCache{})

	// Now give u2 a new device.  The configs don't share a Keybase
	// Daemon so we have to do it in all places.
	AddDeviceForLocalUserOrBust(t, config1, uid2)
	AddDeviceForLocalUserOrBust(t, config2, uid2)
	devIndex := AddDeviceForLocalUserOrBust(t, config2Dev2, uid2)
	SwitchDeviceForLocalUserOrBust(t, config2Dev2, devIndex)

	t.Log("Doing first rekey")

	// The new device should be unable to rekey on its own, and will
	// just set the rekey bit.
	kbfsOps2Dev2 := config2Dev2.KBFSOps()
	err = kbfsOps2Dev2.Rekey(ctx, rootNode1.GetFolderBranch().Tlf)
	if err != nil {
		t.Fatalf("First rekey failed %+v", err)
	}

	ops := getOps(config2Dev2, rootNode1.GetFolderBranch().Tlf)

	// Make sure just the rekey bit is set
	if !ops.head.IsRekeySet() {
		t.Fatalf("Couldn't set rekey bit")
	}

	t.Log("Switching crypto")

	// Allow the prompt rekey attempt to fail by using dev2's crypto
	// (which still isn't keyed for)
	c := make(chan struct{}, 1)
	clta := &cryptoLocalTrapAny{config2Dev2.Crypto(), sync.Once{}, c, config2Dev2.Crypto()}
	config2Dev2.SetCrypto(clta)
	ops.rekeyWithPromptTimer.Reset(1 * time.Millisecond)
	select {
	case <-c:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Make sure the rekey attempt is finished by taking the lock.
	// Keep the lock for a while, to control when the second rekey starts.
	lState := makeFBOLockState()
	func() {
		ops.mdWriterLock.Lock(lState)
		defer ops.mdWriterLock.Unlock(lState)

		// Now cause a paper prompt unlock via a folder access
		errCh := make(chan error, 1)
		go func() {
			_, err := GetRootNodeForTest(ctx, config2Dev2, name, false)
			select {
			case errCh <- err:
			case <-ctx.Done():
				errCh <- errors.WithStack(ctx.Err())
			}
		}()
		select {
		case err = <-errCh:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if _, ok := err.(NeedSelfRekeyError); !ok {
			t.Fatalf("Got unexpected error when reading with new key: %+v", err)
		}

		t.Log("Switching crypto again")

		// Let the background rekeyer decrypt.
		c = make(chan struct{}, 1)
		clta = &cryptoLocalTrapAny{config2Dev2.Crypto(), sync.Once{}, c, config2.Crypto()}
		config2Dev2.SetCrypto(clta)
	}()

	t.Log("Waiting for rekey attempt")

	select {
	case <-c:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Make sure the rekey attempt is finished
	ops.mdWriterLock.Lock(lState)
	ops.mdWriterLock.Unlock(lState)

	t.Log("Getting the root node, which should now succeed")

	GetRootNodeOrBust(ctx, t, config2Dev2, name, false)
}

func TestKeyManager(t *testing.T) {
	tests := []func(*testing.T, MetadataVer){
		testKeyManagerPublicTLFCryptKey,
		testKeyManagerCachedSecretKeyForEncryptionSuccess,
		testKeyManagerCachedSecretKeyForMDDecryptionSuccess,
		testKeyManagerCachedSecretKeyForBlockDecryptionSuccess,
		testKeyManagerUncachedSecretKeyForEncryptionSuccess,
		testKeyManagerUncachedSecretKeyForMDDecryptionSuccess,
		testKeyManagerUncachedSecretKeyForBlockDecryptionSuccess,
		testKeyManagerRekeySuccessPrivate,
		testKeyManagerRekeyResolveAgainSuccessPublic,
		testKeyManagerRekeyResolveAgainSuccessPublicSelf,
		testKeyManagerRekeyResolveAgainSuccessPrivate,
		testKeyManagerPromoteReaderSuccess,
		testKeyManagerPromoteReaderSelf,
		testKeyManagerPromoteReaderFailure2Private,
		testKeyManagerReaderRekeyResolveAgainSuccessPrivate,
		testKeyManagerRekeyResolveAgainNoChangeSuccessPrivate,
		testKeyManagerRekeyAddAndRevokeDevice,
		testKeyManagerRekeyAddWriterAndReaderDevice,
		testKeyManagerSelfRekeyAcrossDevices,
		testKeyManagerReaderRekey,
		testKeyManagerReaderRekeyAndRevoke,
		testKeyManagerRekeyBit,
		testKeyManagerRekeyAddAndRevokeDeviceWithConflict,
		testKeyManagerRekeyAddDeviceWithPrompt,
		testKeyManagerRekeyAddDeviceWithPromptAfterRestart,
		testKeyManagerRekeyAddDeviceWithPromptViaFolderAccess,
	}
	runTestsOverMetadataVers(t, "testKeyManager", tests)
}
