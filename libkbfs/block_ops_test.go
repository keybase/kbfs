// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

// kmdMatcher implements the gomock.Matcher interface to compare
// KeyMetadata objects.
type kmdMatcher struct {
	kmd KeyMetadata
}

func (m kmdMatcher) Matches(x interface{}) bool {
	kmd, ok := x.(KeyMetadata)
	if !ok {
		return false
	}
	return (m.kmd.TlfID() == kmd.TlfID()) &&
		(m.kmd.LatestKeyGeneration() == kmd.LatestKeyGeneration())
}

func (m kmdMatcher) String() string {
	return fmt.Sprintf("Matches KeyMetadata with TlfID=%s and key generation %d",
		m.kmd.TlfID(), m.kmd.LatestKeyGeneration())
}

func expectGetTLFCryptKeyForEncryption(config *ConfigMock, kmd KeyMetadata) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForEncryption(gomock.Any(),
		kmdMatcher{kmd}).Return(kbfscrypto.TLFCryptKey{}, nil)
}

func expectGetTLFCryptKeyForMDDecryption(config *ConfigMock, kmd KeyMetadata) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForMDDecryption(gomock.Any(),
		kmdMatcher{kmd}, kmdMatcher{kmd}).Return(
		kbfscrypto.TLFCryptKey{}, nil)
}

func expectGetTLFCryptKeyForMDDecryptionAtMostOnce(config *ConfigMock,
	kmd KeyMetadata) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForMDDecryption(gomock.Any(),
		kmdMatcher{kmd}, kmdMatcher{kmd}).MaxTimes(1).Return(
		kbfscrypto.TLFCryptKey{}, nil)
}

// TODO: Add test coverage for decryption of blocks with an old key
// generation.

func expectGetTLFCryptKeyForBlockDecryption(
	config *ConfigMock, kmd KeyMetadata, blockPtr BlockPointer) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForBlockDecryption(gomock.Any(),
		kmdMatcher{kmd}, blockPtr).Return(kbfscrypto.TLFCryptKey{}, nil)
}

type TestBlock struct {
	A int
}

func (TestBlock) DataVersion() DataVer {
	return FirstValidDataVer
}

func (tb TestBlock) GetEncodedSize() uint32 {
	return 0
}

func (tb TestBlock) SetEncodedSize(size uint32) {
}

func (tb TestBlock) NewEmpty() Block {
	return &TestBlock{}
}

func (tb *TestBlock) Set(other Block, _ kbfscodec.Codec) {
	otherTb := other.(*TestBlock)
	tb.A = otherTb.A
}

func blockOpsInit(t *testing.T) (mockCtrl *gomock.Controller,
	config *ConfigMock, ctx context.Context) {
	ctr := NewSafeTestReporter(t)
	mockCtrl = gomock.NewController(ctr)
	config = NewConfigMock(mockCtrl, ctr)
	bops := NewBlockOpsStandard(blockOpsConfigAdapter{config},
		testBlockRetrievalWorkerQueueSize)
	config.SetBlockOps(bops)
	ctx = context.Background()
	return
}

func blockOpsShutdown(mockCtrl *gomock.Controller, config *ConfigMock) {
	config.ctr.CheckForFailures()
	config.BlockOps().Shutdown()
	mockCtrl.Finish()
}

func expectBlockEncrypt(config *ConfigMock, kmd KeyMetadata, decData Block, plainSize int, encData []byte, err error) {
	expectGetTLFCryptKeyForEncryption(config, kmd)
	config.mockCrypto.EXPECT().MakeRandomBlockCryptKeyServerHalf().
		Return(kbfscrypto.BlockCryptKeyServerHalf{}, nil)
	encryptedBlock := EncryptedBlock{
		encryptedData{
			EncryptedData: encData,
		},
	}
	config.mockCrypto.EXPECT().EncryptBlock(decData, gomock.Any()).
		Return(plainSize, encryptedBlock, err)
	if err == nil {
		config.mockCodec.EXPECT().Encode(encryptedBlock).Return(encData, nil)
	}
}

func expectBlockDecrypt(config *ConfigMock, kmd KeyMetadata, blockPtr BlockPointer, encData []byte, block *TestBlock) {
	expectGetTLFCryptKeyForBlockDecryption(config, kmd, blockPtr)
	config.mockCodec.EXPECT().Decode(encData, gomock.Any()).Return(nil)
	config.mockCrypto.EXPECT().DecryptBlock(
		gomock.Any(), kbfscrypto.BlockCryptKey{}, gomock.Any()).
		Do(func(encryptedBlock EncryptedBlock,
			key kbfscrypto.BlockCryptKey, b Block) {
			if b != nil {
				tb := b.(*TestBlock)
				*tb = *block
			}
		})
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
	keyGen KeyGen, user keybase1.UID) bool {
	return false
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

func makeKMD() KeyMetadata {
	return emptyKeyMetadata{tlf.FakeID(0, false), 1}
}

type fakeBlockKeyGetter struct {
	keys map[tlf.ID][]kbfscrypto.TLFCryptKey
}

func (kg fakeBlockKeyGetter) GetTLFCryptKeyForEncryption(
	ctx context.Context, kmd KeyMetadata) (kbfscrypto.TLFCryptKey, error) {
	id := kmd.TlfID()
	tlfKeys := kg.keys[id]
	if len(tlfKeys) == 0 {
		return kbfscrypto.TLFCryptKey{}, fmt.Errorf(
			"No keys for %s", id)
	}
	return tlfKeys[len(tlfKeys)-1], nil
}

func (kg fakeBlockKeyGetter) GetTLFCryptKeyForBlockDecryption(
	ctx context.Context, kmd KeyMetadata, blockPtr BlockPointer) (
	kbfscrypto.TLFCryptKey, error) {
	id := kmd.TlfID()
	tlfKeys := kg.keys[id]
	i := int(blockPtr.KeyGen - FirstValidKeyGen)
	if i >= len(tlfKeys) {
		return kbfscrypto.TLFCryptKey{}, fmt.Errorf(
			"No key for %s (key gen=%d)", id, blockPtr.KeyGen)
	}
	return tlfKeys[i], nil
}

type testBlockOpsConfig struct {
	_blockServer BlockServer
	_codec       kbfscodec.Codec
	_crypto      cryptoPure
	_keyGetter   blockKeyGetter
}

func (config testBlockOpsConfig) blockServer() BlockServer {
	return config._blockServer
}

func (config testBlockOpsConfig) codec() kbfscodec.Codec {
	return config._codec
}

func (config testBlockOpsConfig) crypto() cryptoPure {
	return config._crypto
}

func (config testBlockOpsConfig) keyGetter() blockKeyGetter {
	return config._keyGetter
}

func TestBlockOpsReadySuccess2(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	key := kbfscrypto.MakeTLFCryptKey([32]byte{0x5})
	tlfID := tlf.FakeID(0, false)
	var keyGen KeyGen = 5
	kg := fakeBlockKeyGetter{
		keys: map[tlf.ID][]kbfscrypto.TLFCryptKey{
			tlfID: {
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				key,
			},
		},
	}
	blockServer := NewBlockServerMemory(logger.NewTestLogger(t))
	config := testBlockOpsConfig{blockServer, codec, crypto, kg}

	ctx := context.Background()

	block := &FileBlock{
		Contents: []byte{1, 2, 3, 4, 5},
	}

	kmd := emptyKeyMetadata{tlfID, keyGen}

	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, block)
	require.NoError(t, err)
	_ = id
	_ = readyBlockData
}

func TestBlockOpsGetSuccess(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	key := kbfscrypto.MakeTLFCryptKey([32]byte{0x5})
	tlfID := tlf.FakeID(0, false)
	var keyGen KeyGen = 5
	kg := fakeBlockKeyGetter{
		keys: map[tlf.ID][]kbfscrypto.TLFCryptKey{
			tlfID: {
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				key,
			},
		},
	}
	blockServer := NewBlockServerMemory(logger.NewTestLogger(t))
	config := testBlockOpsConfig{blockServer, codec, crypto, kg}

	ctx := context.Background()

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	blockKey := kbfscrypto.UnmaskBlockCryptKey(serverHalf, key)

	block := &FileBlock{
		Contents: []byte{1, 2, 3, 4, 5},
	}
	_, encryptedBlock, err := crypto.EncryptBlock(block, blockKey)
	require.NoError(t, err)

	encodedBlock, err := codec.Encode(encryptedBlock)
	require.NoError(t, err)

	id, err := kbfsblock.MakePermanentID(encodedBlock)
	require.NoError(t, err)

	uid := keybase1.MakeTestUID(1)

	bCtx := kbfsblock.MakeFirstContext(uid)

	err = blockServer.Put(ctx, tlfID, id, bCtx, encodedBlock, serverHalf)
	require.NoError(t, err)

	kmd := emptyKeyMetadata{tlfID, keyGen}

	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	var block2 FileBlock
	err = bops.Get(ctx, kmd, BlockPointer{ID: id, KeyGen: keyGen, Context: bCtx}, &block2)
	require.NoError(t, err)
	block2.cachedEncodedSize = 0
	require.Equal(t, *block, block2)
}

func TestBlockOpsGetFailGet(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	key := kbfscrypto.MakeTLFCryptKey([32]byte{0x5})
	tlfID := tlf.FakeID(0, false)
	var keyGen KeyGen = 5
	kg := fakeBlockKeyGetter{
		keys: map[tlf.ID][]kbfscrypto.TLFCryptKey{
			tlfID: {
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				kbfscrypto.TLFCryptKey{},
				key,
			},
		},
	}
	blockServer := NewBlockServerMemory(logger.NewTestLogger(t))
	config := testBlockOpsConfig{blockServer, codec, crypto, kg}

	ctx := context.Background()

	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	blockKey := kbfscrypto.UnmaskBlockCryptKey(serverHalf, key)

	block := &FileBlock{
		Contents: []byte{1, 2, 3, 4, 5},
	}
	_, encryptedBlock, err := crypto.EncryptBlock(block, blockKey)
	require.NoError(t, err)

	encodedBlock, err := codec.Encode(encryptedBlock)
	require.NoError(t, err)

	id, err := kbfsblock.MakePermanentID(encodedBlock)
	require.NoError(t, err)

	uid := keybase1.MakeTestUID(1)

	bCtx := kbfsblock.MakeFirstContext(uid)

	kmd := emptyKeyMetadata{tlfID, keyGen}

	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	var block2 FileBlock
	err = bops.Get(ctx, kmd, BlockPointer{ID: id, KeyGen: keyGen, Context: bCtx}, &block2)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)
}

func TestBlockOpsGetFailVerify(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	kmd := makeKMD()
	// fail the fetch call
	id := kbfsblock.FakeID(1)
	blockPtr := BlockPointer{ID: id}
	encData := []byte{1, 2, 3}
	config.mockBserv.EXPECT().Get(gomock.Any(), kmd.TlfID(), id, blockPtr.Context).Return(
		encData, kbfscrypto.BlockCryptKeyServerHalf{}, nil)

	var block TestBlock
	err := config.BlockOps().Get(ctx, kmd, blockPtr, &block)
	require.True(t, strings.HasPrefix(err.Error(), "Hash mismatch"))
}

func TestBlockOpsGetFailDecryptBlockData(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)
	codec := kbfscodec.NewMsgpack()
	config.SetCodec(codec)
	crypto := NewCryptoLocal(codec,
		kbfscrypto.MakeFakeSigningKeyOrBust("test"),
		kbfscrypto.MakeFakeCryptPrivateKeyOrBust("test"))
	config.SetCrypto(crypto)

	kmd := makeKMD()
	// expect one call to fetch a block, then fail to decrypt
	encData := []byte{1, 2, 3, 4}
	id, err := kbfsblock.MakePermanentID(encData)
	require.NoError(t, err)
	blockPtr := BlockPointer{ID: id}
	config.mockBserv.EXPECT().Get(gomock.Any(), kmd.TlfID(), id, blockPtr.Context).Return(
		encData, kbfscrypto.BlockCryptKeyServerHalf{}, nil)
	expectGetTLFCryptKeyForBlockDecryption(config, kmd, blockPtr)

	var block TestBlock
	err = config.BlockOps().Get(ctx, kmd, blockPtr, &block)
	require.True(t, strings.HasPrefix(err.Error(), "failed to decode"))
}

func TestBlockOpsReadySuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to encrypt a block, one to hash it
	decData := &TestBlock{42}
	encData := []byte{1, 2, 3, 4}

	kmd := makeKMD()

	expectedPlainSize := 4
	expectBlockEncrypt(config, kmd, decData, expectedPlainSize, encData, nil)
	_, plainSize, readyBlockData, err :=
		config.BlockOps().Ready(ctx, kmd, decData)
	if err != nil {
		t.Errorf("Got error on ready: %v", err)
	} else if plainSize != expectedPlainSize {
		t.Errorf("Expected plainSize %d, got %d", expectedPlainSize, plainSize)
	} else if string(readyBlockData.buf) != string(encData) {
		t.Errorf("Got back wrong data on get: %v", readyBlockData.buf)
	}
}

func TestBlockOpsReadyFailTooLowByteCount(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect just one call to encrypt a block
	decData := &TestBlock{42}
	encData := []byte{1, 2, 3}

	kmd := makeKMD()

	expectBlockEncrypt(config, kmd, decData, 4, encData, nil)

	_, _, _, err := config.BlockOps().Ready(ctx, kmd, decData)
	if _, ok := err.(TooLowByteCountError); !ok {
		t.Errorf("Unexpectedly did not get TooLowByteCountError; "+
			"instead got %v", err)
	}
}

func TestBlockOpsReadyFailEncryptBlockData(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to encrypt a block, one to hash it
	decData := &TestBlock{42}
	err := errors.New("Fake fail")

	kmd := makeKMD()

	expectBlockEncrypt(config, kmd, decData, 0, nil, err)

	if _, _, _, err2 := config.BlockOps().Ready(
		ctx, kmd, decData); err2 != err {
		t.Errorf("Got bad error on ready: %v", err2)
	}
}

func TestBlockOpsDeleteSuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to delete several blocks

	contexts := make(kbfsblock.ContextMap)
	b1 := BlockPointer{ID: kbfsblock.FakeID(1)}
	contexts[b1.ID] = []kbfsblock.Context{b1.Context}
	b2 := BlockPointer{ID: kbfsblock.FakeID(2)}
	contexts[b2.ID] = []kbfsblock.Context{b2.Context}
	blockPtrs := []BlockPointer{b1, b2}
	var liveCounts map[kbfsblock.ID]int
	tlfID := tlf.FakeID(1, false)
	config.mockBserv.EXPECT().RemoveBlockReferences(ctx, tlfID, contexts).
		Return(liveCounts, nil)

	if _, err := config.BlockOps().Delete(
		ctx, tlfID, blockPtrs); err != nil {
		t.Errorf("Got error on delete: %v", err)
	}
}

func TestBlockOpsDeleteFail(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// fail the delete call

	contexts := make(kbfsblock.ContextMap)
	b1 := BlockPointer{ID: kbfsblock.FakeID(1)}
	contexts[b1.ID] = []kbfsblock.Context{b1.Context}
	b2 := BlockPointer{ID: kbfsblock.FakeID(2)}
	contexts[b2.ID] = []kbfsblock.Context{b2.Context}
	blockPtrs := []BlockPointer{b1, b2}
	err := errors.New("Fake fail")
	var liveCounts map[kbfsblock.ID]int
	tlfID := tlf.FakeID(1, false)
	config.mockBserv.EXPECT().RemoveBlockReferences(ctx, tlfID, contexts).
		Return(liveCounts, err)

	if _, err2 := config.BlockOps().Delete(
		ctx, tlfID, blockPtrs); err2 != err {
		t.Errorf("Got bad error on delete: %v", err2)
	}
}
