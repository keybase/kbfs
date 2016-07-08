// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"golang.org/x/net/context"
)

// rmdMatcher implements the gomock.Matcher interface to compare
// RootMetadata objects. We can't just compare pointers as copies are
// made for mutations.
type rmdMatcher struct {
	rmd *IFCERFTRootMetadata
}

// Matches returns whether x is a *RootMetadata and it has the same ID
// and latest key generation as m.rmd.
func (m rmdMatcher) Matches(x interface{}) bool {
	rmd, ok := x.(*IFCERFTRootMetadata)
	if !ok {
		return false
	}
	return (rmd.ID == m.rmd.ID) && (rmd.LatestKeyGeneration() == m.rmd.LatestKeyGeneration())
}

// String implements the Matcher interface for rmdMatcher.
func (m rmdMatcher) String() string {
	return fmt.Sprintf("Matches RMD %v", m.rmd)
}

func expectGetTLFCryptKeyForEncryption(config *ConfigMock, rmd *IFCERFTRootMetadata) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForEncryption(gomock.Any(),
		rmdMatcher{rmd}).Return(IFCERFTTLFCryptKey{}, nil)
}

func expectGetTLFCryptKeyForMDDecryption(config *ConfigMock, rmd *IFCERFTRootMetadata) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForMDDecryption(gomock.Any(),
		rmdMatcher{rmd}, rmdMatcher{rmd}).Return(IFCERFTTLFCryptKey{}, nil)
}

// TODO: Add test coverage for decryption of blocks with an old key
// generation.

func expectGetTLFCryptKeyForBlockDecryption(
	config *ConfigMock, rmd *IFCERFTRootMetadata, blockPtr IFCERFTBlockPointer) {
	config.mockKeyman.EXPECT().GetTLFCryptKeyForBlockDecryption(gomock.Any(),
		rmdMatcher{rmd}, blockPtr).Return(IFCERFTTLFCryptKey{}, nil)
}

type TestBlock struct {
	A int
}

func (TestBlock) DataVersion() IFCERFTDataVer {
	return FirstValidDataVer
}

func (tb TestBlock) GetEncodedSize() uint32 {
	return 0
}

func (tb TestBlock) SetEncodedSize(size uint32) {
}

func blockOpsInit(t *testing.T) (mockCtrl *gomock.Controller,
	config *ConfigMock, ctx context.Context) {
	ctr := NewSafeTestReporter(t)
	mockCtrl = gomock.NewController(ctr)
	config = NewConfigMock(mockCtrl, ctr)
	bops := &BlockOpsStandard{config}
	config.SetBlockOps(bops)
	ctx = context.Background()
	return
}

func blockOpsShutdown(mockCtrl *gomock.Controller, config *ConfigMock) {
	config.ctr.CheckForFailures()
	mockCtrl.Finish()
}

func expectBlockEncrypt(config *ConfigMock, rmd *IFCERFTRootMetadata, decData IFCERFTBlock, plainSize int, encData []byte, err error) {
	expectGetTLFCryptKeyForEncryption(config, rmd)
	config.mockCrypto.EXPECT().MakeRandomBlockCryptKeyServerHalf().
		Return(IFCERFTBlockCryptKeyServerHalf{}, nil)
	config.mockCrypto.EXPECT().UnmaskBlockCryptKey(
		IFCERFTBlockCryptKeyServerHalf{}, IFCERFTTLFCryptKey{}).Return(BlockCryptKey{}, nil)
	encryptedBlock := IFCERFTEncryptedBlock{
		EncryptedData: encData,
	}
	config.mockCrypto.EXPECT().EncryptBlock(decData, BlockCryptKey{}).
		Return(plainSize, encryptedBlock, err)
	if err == nil {
		config.mockCodec.EXPECT().Encode(encryptedBlock).Return(encData, nil)
	}
}

func expectBlockDecrypt(config *ConfigMock, rmd *IFCERFTRootMetadata, blockPtr IFCERFTBlockPointer, encData []byte, block TestBlock, err error) {
	config.mockCrypto.EXPECT().VerifyBlockID(encData, blockPtr.ID).Return(nil)
	expectGetTLFCryptKeyForBlockDecryption(config, rmd, blockPtr)
	config.mockCrypto.EXPECT().UnmaskBlockCryptKey(gomock.Any(), gomock.Any()).
		Return(BlockCryptKey{}, nil)
	config.mockCodec.EXPECT().Decode(encData, gomock.Any()).Return(nil)
	config.mockCrypto.EXPECT().DecryptBlock(gomock.Any(), BlockCryptKey{}, gomock.Any()).
		Do(func(encryptedBlock IFCERFTEncryptedBlock, key BlockCryptKey, b IFCERFTBlock) {
			if b != nil {
				tb := b.(*TestBlock)
				*tb = block
			}
		}).Return(err)
}

func makeRMD() *IFCERFTRootMetadata {
	tlfID := FakeTlfID(0, false)
	return &IFCERFTRootMetadata{WriterMetadata: WriterMetadata{ID: tlfID}}
}

func TestBlockOpsGetSuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	rmd := makeRMD()

	// expect one call to fetch a block, and one to decrypt it
	id := fakeBlockID(1)
	encData := []byte{1, 2, 3, 4}
	blockPtr := IFCERFTBlockPointer{ID: id}
	config.mockBserv.EXPECT().Get(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext).Return(
		encData, IFCERFTBlockCryptKeyServerHalf{}, nil)
	decData := TestBlock{42}

	expectBlockDecrypt(config, rmd, blockPtr, encData, decData, nil)

	var gotBlock TestBlock
	err := config.BlockOps().Get(ctx, rmd, blockPtr, &gotBlock)
	if err != nil {
		t.Fatalf("Got error on get: %v", err)
	}

	if gotBlock != decData {
		t.Errorf("Got back wrong block data on get: %v", gotBlock)
	}
}

func TestBlockOpsGetFailGet(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	rmd := makeRMD()
	// fail the fetch call
	id := fakeBlockID(1)
	err := errors.New("Fake fail")
	blockPtr := IFCERFTBlockPointer{ID: id}
	config.mockBserv.EXPECT().Get(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext).Return(
		nil, IFCERFTBlockCryptKeyServerHalf{}, err)

	if err2 := config.BlockOps().Get(ctx, rmd, blockPtr, nil); err2 != err {
		t.Errorf("Got bad error: %v", err2)
	}
}

func TestBlockOpsGetFailVerify(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	rmd := makeRMD()
	// fail the fetch call
	id := fakeBlockID(1)
	err := errors.New("Fake verification fail")
	blockPtr := IFCERFTBlockPointer{ID: id}
	encData := []byte{1, 2, 3}
	config.mockBserv.EXPECT().Get(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext).Return(
		encData, IFCERFTBlockCryptKeyServerHalf{}, nil)
	config.mockCrypto.EXPECT().VerifyBlockID(encData, id).Return(err)

	if err2 := config.BlockOps().Get(ctx, rmd, blockPtr, nil); err2 != err {
		t.Errorf("Got bad error: %v", err2)
	}
}

func TestBlockOpsGetFailDecryptBlockData(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	rmd := makeRMD()
	// expect one call to fetch a block, then fail to decrypt
	id := fakeBlockID(1)
	encData := []byte{1, 2, 3, 4}
	blockPtr := IFCERFTBlockPointer{ID: id}
	config.mockBserv.EXPECT().Get(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext).Return(
		encData, IFCERFTBlockCryptKeyServerHalf{}, nil)
	err := errors.New("Fake fail")

	expectBlockDecrypt(config, rmd, blockPtr, encData, TestBlock{}, err)

	if err2 := config.BlockOps().Get(ctx, rmd, blockPtr, nil); err2 != err {
		t.Errorf("Got bad error: %v", err2)
	}
}

func TestBlockOpsReadySuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to encrypt a block, one to hash it
	decData := TestBlock{42}
	encData := []byte{1, 2, 3, 4}
	id := fakeBlockID(1)

	rmd := makeRMD()

	expectedPlainSize := 4
	expectBlockEncrypt(config, rmd, decData, expectedPlainSize, encData, nil)
	config.mockCrypto.EXPECT().MakePermanentBlockID(encData).Return(id, nil)

	id2, plainSize, readyBlockData, err :=
		config.BlockOps().Ready(ctx, rmd, decData)
	if err != nil {
		t.Errorf("Got error on ready: %v", err)
	} else if id2 != id {
		t.Errorf("Got back wrong id on ready: %v", id)
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
	decData := TestBlock{42}
	encData := []byte{1, 2, 3}

	rmd := makeRMD()

	expectBlockEncrypt(config, rmd, decData, 4, encData, nil)

	_, _, _, err := config.BlockOps().Ready(ctx, rmd, decData)
	if _, ok := err.(TooLowByteCountError); !ok {
		t.Errorf("Unexpectedly did not get TooLowByteCountError; "+
			"instead got %v", err)
	}
}

func TestBlockOpsReadyFailEncryptBlockData(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to encrypt a block, one to hash it
	decData := TestBlock{42}
	err := errors.New("Fake fail")

	rmd := makeRMD()

	expectBlockEncrypt(config, rmd, decData, 0, nil, err)

	if _, _, _, err2 := config.BlockOps().
		Ready(ctx, rmd, decData); err2 != err {
		t.Errorf("Got bad error on ready: %v", err2)
	}
}

func TestBlockOpsReadyFailMakePermanentBlockID(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to encrypt a block, one to hash it
	decData := TestBlock{42}
	encData := []byte{1, 2, 3, 4}
	err := errors.New("Fake fail")

	rmd := makeRMD()

	expectBlockEncrypt(config, rmd, decData, 4, encData, nil)

	config.mockCrypto.EXPECT().MakePermanentBlockID(encData).Return(fakeBlockID(0), err)

	if _, _, _, err2 := config.BlockOps().
		Ready(ctx, rmd, decData); err2 != err {
		t.Errorf("Got bad error on ready: %v", err2)
	}
}

func TestBlockOpsPutNewBlockSuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to put a block
	id := fakeBlockID(1)
	encData := []byte{1, 2, 3, 4}
	blockPtr := IFCERFTBlockPointer{ID: id}

	rmd := makeRMD()

	readyBlockData := IFCERFTReadyBlockData{
		buf: encData,
	}

	config.mockBserv.EXPECT().Put(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext,
		readyBlockData.buf, readyBlockData.serverHalf).Return(nil)

	if err := config.BlockOps().
		Put(ctx, rmd, blockPtr, readyBlockData); err != nil {
		t.Errorf("Got error on put: %v", err)
	}
}

func TestBlockOpsPutIncRefSuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to put a block
	id := fakeBlockID(1)
	encData := []byte{1, 2, 3, 4}
	nonce := IFCERFTBlockRefNonce([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	blockPtr := IFCERFTBlockPointer{
		ID: id,
		IFCERFTBlockContext: IFCERFTBlockContext{
			RefNonce: nonce,
		},
	}

	rmd := makeRMD()

	readyBlockData := IFCERFTReadyBlockData{
		buf: encData,
	}

	config.mockBserv.EXPECT().AddBlockReference(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext).
		Return(nil)

	if err := config.BlockOps().
		Put(ctx, rmd, blockPtr, readyBlockData); err != nil {
		t.Errorf("Got error on put: %v", err)
	}
}

func TestBlockOpsPutFail(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// fail the put call
	id := fakeBlockID(1)
	encData := []byte{1, 2, 3, 4}
	blockPtr := IFCERFTBlockPointer{ID: id}

	err := errors.New("Fake fail")

	rmd := makeRMD()

	readyBlockData := IFCERFTReadyBlockData{
		buf: encData,
	}

	config.mockBserv.EXPECT().Put(ctx, id, rmd.ID, blockPtr.IFCERFTBlockContext,
		readyBlockData.buf, readyBlockData.serverHalf).Return(err)

	if err2 := config.BlockOps().
		Put(ctx, rmd, blockPtr, readyBlockData); err2 != err {
		t.Errorf("Got bad error on put: %v", err2)
	}
}

func TestBlockOpsDeleteSuccess(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// expect one call to delete several blocks
	rmd := makeRMD()

	contexts := make(map[BlockID][]IFCERFTBlockContext)
	b1 := IFCERFTBlockPointer{ID: fakeBlockID(1)}
	contexts[b1.ID] = []IFCERFTBlockContext{b1.IFCERFTBlockContext}
	b2 := IFCERFTBlockPointer{ID: fakeBlockID(2)}
	contexts[b2.ID] = []IFCERFTBlockContext{b2.IFCERFTBlockContext}
	blockPtrs := []IFCERFTBlockPointer{b1, b2}
	var liveCounts map[BlockID]int
	config.mockBserv.EXPECT().RemoveBlockReference(ctx, rmd.ID, contexts).
		Return(liveCounts, nil)

	if _, err := config.BlockOps().Delete(ctx, rmd, blockPtrs); err != nil {
		t.Errorf("Got error on delete: %v", err)
	}
}

func TestBlockOpsDeleteFail(t *testing.T) {
	mockCtrl, config, ctx := blockOpsInit(t)
	defer blockOpsShutdown(mockCtrl, config)

	// fail the delete call
	rmd := makeRMD()

	contexts := make(map[BlockID][]IFCERFTBlockContext)
	b1 := IFCERFTBlockPointer{ID: fakeBlockID(1)}
	contexts[b1.ID] = []IFCERFTBlockContext{b1.IFCERFTBlockContext}
	b2 := IFCERFTBlockPointer{ID: fakeBlockID(2)}
	contexts[b2.ID] = []IFCERFTBlockContext{b2.IFCERFTBlockContext}
	blockPtrs := []IFCERFTBlockPointer{b1, b2}
	err := errors.New("Fake fail")
	var liveCounts map[BlockID]int
	config.mockBserv.EXPECT().RemoveBlockReference(ctx, rmd.ID, contexts).
		Return(liveCounts, err)

	if _, err2 := config.BlockOps().Delete(ctx, rmd, blockPtrs); err2 != err {
		t.Errorf("Got bad error on delete: %v", err2)
	}
}
