// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/kbfshash"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

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

type fakeKeyMetadata struct {
	tlfID  tlf.ID
	keyGen KeyGen
}

var _ KeyMetadata = fakeKeyMetadata{}

func (kmd fakeKeyMetadata) TlfID() tlf.ID {
	return kmd.tlfID
}

// GetTlfHandle just returns nil. This contradicts the requirements
// for KeyMetadata, but fakeKeyMetadata shouldn't be used in contexts
// that actually use GetTlfHandle().
func (kmd fakeKeyMetadata) GetTlfHandle() *TlfHandle {
	return nil
}

func (kmd fakeKeyMetadata) LatestKeyGeneration() KeyGen {
	return kmd.keyGen
}

func (kmd fakeKeyMetadata) HasKeyForUser(
	keyGen KeyGen, user keybase1.UID) bool {
	return false
}

func (kmd fakeKeyMetadata) GetTLFCryptKeyParams(
	keyGen KeyGen, user keybase1.UID, key kbfscrypto.CryptPublicKey) (
	kbfscrypto.TLFEphemeralPublicKey, EncryptedTLFCryptKeyClientHalf,
	TLFCryptKeyServerHalfID, bool, error) {
	return kbfscrypto.TLFEphemeralPublicKey{},
		EncryptedTLFCryptKeyClientHalf{},
		TLFCryptKeyServerHalfID{}, false, nil
}

func (kmd fakeKeyMetadata) StoresHistoricTLFCryptKeys() bool {
	return false
}

func (kmd fakeKeyMetadata) GetHistoricTLFCryptKey(
	crypto cryptoPure, keyGen KeyGen, key kbfscrypto.TLFCryptKey) (
	kbfscrypto.TLFCryptKey, error) {
	return kbfscrypto.TLFCryptKey{}, nil
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
	bserver    BlockServer
	testCodec  kbfscodec.Codec
	cryptoPure cryptoPure
	kg         fakeBlockKeyGetter
}

func (config testBlockOpsConfig) blockServer() BlockServer {
	return config.bserver
}

func (config testBlockOpsConfig) codec() kbfscodec.Codec {
	return config.testCodec
}

func (config testBlockOpsConfig) crypto() cryptoPure {
	return config.cryptoPure
}

func (config testBlockOpsConfig) keyGetter() blockKeyGetter {
	return config.kg
}

func makeTestBlockOpsConfig(t *testing.T, tlfID tlf.ID,
	latestKeyGen KeyGen) testBlockOpsConfig {
	blockServer := NewBlockServerMemory(logger.NewTestLogger(t))
	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(codec)
	tlfKeys := make([]kbfscrypto.TLFCryptKey, 0,
		latestKeyGen-FirstValidKeyGen+1)
	for keyGen := FirstValidKeyGen; keyGen <= latestKeyGen; keyGen++ {
		tlfKeys = append(tlfKeys,
			kbfscrypto.MakeTLFCryptKey([32]byte{byte(keyGen)}))
	}
	kg := fakeBlockKeyGetter{
		keys: map[tlf.ID][]kbfscrypto.TLFCryptKey{
			tlfID: tlfKeys,
		},
	}
	return testBlockOpsConfig{blockServer, codec, crypto, kg}
}

// TestBlockOpsReadySuccess checks that BlockOpsStandard.Ready()
// encrypts its given block properly.
func TestBlockOpsReadySuccess(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	block := FileBlock{
		Contents: []byte{1, 2, 3, 4, 5},
	}

	encodedBlock, err := config.testCodec.Encode(block)
	require.NoError(t, err)

	ctx := context.Background()
	id, plainSize, readyBlockData, err := bops.Ready(ctx, kmd, &block)
	require.NoError(t, err)

	require.Equal(t, len(encodedBlock), plainSize)

	err = kbfsblock.VerifyID(readyBlockData.buf, id)
	require.NoError(t, err)

	var encryptedBlock EncryptedBlock
	err = config.testCodec.Decode(readyBlockData.buf, &encryptedBlock)
	require.NoError(t, err)

	blockCryptKey := kbfscrypto.UnmaskBlockCryptKey(
		readyBlockData.serverHalf,
		config.kg.keys[tlfID][latestKeyGen-FirstValidKeyGen])

	var decryptedBlock FileBlock
	err = config.cryptoPure.DecryptBlock(
		encryptedBlock, blockCryptKey, &decryptedBlock)
	require.NoError(t, err)
	decryptedBlock.SetEncodedSize(uint32(readyBlockData.GetEncodedSize()))
	require.Equal(t, block, decryptedBlock)
}

// TestBlockOpsReadyFailKeyGet checks that BlockOpsStandard.Ready()
// fails properly if we fail to retrieve the key.
func TestBlockOpsReadyFailKeyGet(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	config := makeTestBlockOpsConfig(t, tlfID, 0)
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, FirstValidKeyGen}

	ctx := context.Background()
	_, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.True(t, strings.HasPrefix(err.Error(), "No keys for"))
}

type badServerHalfMaker struct {
	cryptoPure
}

func (c badServerHalfMaker) MakeRandomBlockCryptKeyServerHalf() (
	kbfscrypto.BlockCryptKeyServerHalf, error) {
	return kbfscrypto.BlockCryptKeyServerHalf{}, errors.New(
		"could not make server half")
}

// TestBlockOpsReadyFailServerHalfGet checks that BlockOpsStandard.Ready()
// fails properly if we fail to generate a  server half.
func TestBlockOpsReadyFailServerHalfGet(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	config := makeTestBlockOpsConfig(t, tlfID, FirstValidKeyGen)
	config.cryptoPure = badServerHalfMaker{config.cryptoPure}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, FirstValidKeyGen}

	ctx := context.Background()
	_, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.EqualError(t, err, "could not make server half")
}

type badBlockEncryptor struct {
	cryptoPure
}

func (c badBlockEncryptor) EncryptBlock(
	block Block, key kbfscrypto.BlockCryptKey) (
	plainSize int, encryptedBlock EncryptedBlock, err error) {
	return 0, EncryptedBlock{}, errors.New("could not encrypt block")
}

// TestBlockOpsReadyFailEncryption checks that BlockOpsStandard.Ready()
// fails properly if we fail to encrypt the block.
func TestBlockOpsReadyFailEncryption(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	config := makeTestBlockOpsConfig(t, tlfID, FirstValidKeyGen)
	config.cryptoPure = badBlockEncryptor{config.cryptoPure}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, FirstValidKeyGen}

	ctx := context.Background()
	_, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.EqualError(t, err, "could not encrypt block")
}

type tooSmallBlockEncryptor struct {
	CryptoCommon
}

func (c tooSmallBlockEncryptor) EncryptBlock(
	block Block, key kbfscrypto.BlockCryptKey) (
	plainSize int, encryptedBlock EncryptedBlock, err error) {
	plainSize, encryptedBlock, err = c.CryptoCommon.EncryptBlock(block, key)
	if err != nil {
		return 0, EncryptedBlock{}, err
	}
	encryptedBlock.EncryptedData = nil
	return plainSize, encryptedBlock, nil
}

type badEncoder struct {
	kbfscodec.Codec
}

func (c badEncoder) Encode(o interface{}) ([]byte, error) {
	return nil, errors.New("could not encode")
}

// TestBlockOpsReadyFailEncode checks that BlockOpsStandard.Ready()
// fails properly if we fail to encode the encrypted block.
func TestBlockOpsReadyFailEncode(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	config := makeTestBlockOpsConfig(t, tlfID, FirstValidKeyGen)
	config.testCodec = badEncoder{config.testCodec}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, FirstValidKeyGen}

	ctx := context.Background()
	_, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.EqualError(t, err, "could not encode")
}

type tooSmallEncoder struct {
	kbfscodec.Codec
}

func (c tooSmallEncoder) Encode(o interface{}) ([]byte, error) {
	return []byte{0x1}, nil
}

// TestBlockOpsReadyTooSmallEncode checks that
// BlockOpsStandard.Ready() fails properly if the encrypted block
// encodes to a too-small buffer.
func TestBlockOpsReadyTooSmallEncode(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	config := makeTestBlockOpsConfig(t, tlfID, FirstValidKeyGen)
	config.testCodec = tooSmallEncoder{config.testCodec}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, FirstValidKeyGen}

	ctx := context.Background()
	_, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.IsType(t, TooLowByteCountError{}, err)
}

// TestBlockOpsReadySuccess checks that BlockOpsStandard.Get()
// retrieves a block properly.
func TestBlockOpsGetSuccess(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	var keyGen KeyGen = 3
	kmd := fakeKeyMetadata{tlfID, keyGen}

	block := FileBlock{
		Contents: []byte{1, 2, 3, 4, 5},
	}

	ctx := context.Background()
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, &block)
	require.NoError(t, err)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	err = config.bserver.Put(ctx, tlfID, id, bCtx,
		readyBlockData.buf, readyBlockData.serverHalf)
	require.NoError(t, err)

	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: keyGen, Context: bCtx},
		&decryptedBlock)
	require.NoError(t, err)
	require.Equal(t, block, decryptedBlock)
}

// TestBlockOpsReadySuccess checks that BlockOpsStandard.Get() fails
// if it can't retrieve the block from the server.
func TestBlockOpsGetFailServerGet(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	ctx := context.Background()
	id, _, _, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.NoError(t, err)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: latestKeyGen, Context: bCtx},
		&decryptedBlock)
	require.IsType(t, kbfsblock.BServerErrorBlockNonExistent{}, err)
}

type badGetBlockServer struct {
	BlockServer
}

func (bserver badGetBlockServer) Get(
	ctx context.Context, tlfID tlf.ID, id kbfsblock.ID,
	context kbfsblock.Context) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	buf, serverHalf, err := bserver.BlockServer.Get(ctx, tlfID, id, context)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, nil
	}

	return append(buf, 0x1), serverHalf, nil
}

// TestBlockOpsReadyFailVerify checks that BlockOpsStandard.Get()
// fails if it can't verify the block retrieved from the server.
func TestBlockOpsGetFailVerify(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	config.bserver = badGetBlockServer{config.bserver}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	ctx := context.Background()
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.NoError(t, err)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	err = config.bserver.Put(ctx, tlfID, id, bCtx,
		readyBlockData.buf, readyBlockData.serverHalf)
	require.NoError(t, err)

	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: latestKeyGen, Context: bCtx},
		&decryptedBlock)
	require.IsType(t, kbfshash.HashMismatchError{}, err)
}

// TestBlockOpsReadyFailKeyGet checks that BlockOpsStandard.Get()
// fails if it can't get the decryption key.
func TestBlockOpsGetFailKeyGet(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	ctx := context.Background()
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.NoError(t, err)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	err = config.bserver.Put(ctx, tlfID, id, bCtx,
		readyBlockData.buf, readyBlockData.serverHalf)
	require.NoError(t, err)

	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: latestKeyGen + 1, Context: bCtx},
		&decryptedBlock)
	require.True(t, strings.HasPrefix(err.Error(), "No key for"))
}

type badDecoder struct {
	kbfscodec.Codec

	errorsLock sync.RWMutex
	errors     map[string]error
}

func (c badDecoder) putError(buf []byte, err error) {
	k := string(buf)
	c.errorsLock.Lock()
	c.errorsLock.Unlock()
	c.errors[k] = err
}

func (c badDecoder) Decode(buf []byte, o interface{}) error {
	k := string(buf)
	err := func() error {
		c.errorsLock.RLock()
		defer c.errorsLock.RUnlock()
		return c.errors[k]
	}()
	if err != nil {
		return err
	}
	return c.Codec.Decode(buf, o)
}

// TestBlockOpsReadyFailDecode checks that BlockOpsStandard.Get()
// fails if it can't decode the encrypted block.
func TestBlockOpsGetFailDecode(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	badDecoder := badDecoder{
		Codec:  config.testCodec,
		errors: make(map[string]error),
	}
	config.testCodec = badDecoder
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	ctx := context.Background()
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.NoError(t, err)

	decodeErr := errors.New("could not decode")
	badDecoder.putError(readyBlockData.buf, decodeErr)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	err = config.bserver.Put(ctx, tlfID, id, bCtx,
		readyBlockData.buf, readyBlockData.serverHalf)
	require.NoError(t, err)

	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: latestKeyGen, Context: bCtx},
		&decryptedBlock)
	require.Equal(t, decodeErr, err)
}

type badBlockDecryptor struct {
	cryptoPure
}

func (c badBlockDecryptor) DecryptBlock(encryptedBlock EncryptedBlock,
	key kbfscrypto.BlockCryptKey, block Block) error {
	return errors.New("could not decrypt block")
}

// TestBlockOpsReadyFailDecrypt checks that BlockOpsStandard.Get()
// fails if it can't decrypt the encrypted block.
func TestBlockOpsGetFailDecrypt(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	var latestKeyGen KeyGen = 5
	config := makeTestBlockOpsConfig(t, tlfID, latestKeyGen)
	config.cryptoPure = badBlockDecryptor{config.cryptoPure}
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	kmd := fakeKeyMetadata{tlfID, latestKeyGen}

	ctx := context.Background()
	id, _, readyBlockData, err := bops.Ready(ctx, kmd, &FileBlock{})
	require.NoError(t, err)

	bCtx := kbfsblock.MakeFirstContext(keybase1.MakeTestUID(1))
	err = config.bserver.Put(ctx, tlfID, id, bCtx,
		readyBlockData.buf, readyBlockData.serverHalf)
	require.NoError(t, err)

	var decryptedBlock FileBlock
	err = bops.Get(ctx, kmd,
		BlockPointer{ID: id, KeyGen: latestKeyGen, Context: bCtx},
		&decryptedBlock)
	require.EqualError(t, err, "could not decrypt block")
}

func TestBlockOpsDeleteSuccess(t *testing.T) {
	tlfID := tlf.FakeID(0, false)
	ctr := NewSafeTestReporter(t)
	mockCtrl := gomock.NewController(ctr)
	defer mockCtrl.Finish()

	bserver := NewMockBlockServer(mockCtrl)
	config := makeTestBlockOpsConfig(t, tlfID, 0)
	config.bserver = bserver
	bops := NewBlockOpsStandard(config, testBlockRetrievalWorkerQueueSize)
	defer bops.Shutdown()

	// expect one call to delete several blocks

	contexts := make(kbfsblock.ContextMap)
	b1 := BlockPointer{ID: kbfsblock.FakeID(1)}
	contexts[b1.ID] = []kbfsblock.Context{b1.Context}
	b2 := BlockPointer{ID: kbfsblock.FakeID(2)}
	contexts[b2.ID] = []kbfsblock.Context{b2.Context}
	blockPtrs := []BlockPointer{b1, b2}
	var liveCounts map[kbfsblock.ID]int
	ctx := context.Background()
	bserver.EXPECT().RemoveBlockReferences(ctx, tlfID, contexts).
		Return(liveCounts, nil)

	_, err := bops.Delete(ctx, tlfID, blockPtrs)
	require.NoError(t, err)
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
