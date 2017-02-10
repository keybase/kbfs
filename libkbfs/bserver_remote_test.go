// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-framed-msgpack-rpc/rpc"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type fakeBlockEntry struct {
	folder   string
	buf      []byte
	blockKey string
	refs     map[keybase1.BlockRefNonce]keybase1.BlockReference
}

type fakeBServerClient struct {
	keybase1.BlockInterface
	entries map[keybase1.BlockIdCombo]fakeBlockEntry
}

func (fc *fakeBServerClient) PutBlock(
	ctx context.Context, arg keybase1.PutBlockArg) error {
	var refs map[keybase1.BlockRefNonce]keybase1.BlockReference
	if e, ok := fc.entries[arg.Bid]; ok {
		refs = e.refs
	} else {
		refs = make(map[keybase1.BlockRefNonce]keybase1.BlockReference)
		fc.entries[arg.Bid] = fakeBlockEntry{
			arg.Folder, arg.Buf, arg.BlockKey, refs,
		}
	}
	refs[keybase1.BlockRefNonce{}] = keybase1.BlockReference{
		Bid: arg.Bid,
	}
	return nil
}

func (fc *fakeBServerClient) GetBlock(ctx context.Context, arg keybase1.GetBlockArg) (keybase1.GetBlockRes, error) {
	e, ok := fc.entries[arg.Bid]
	if !ok {
		return keybase1.GetBlockRes{}, kbfsblock.BServerErrorBlockNonExistent{}
	}
	return keybase1.GetBlockRes{
		Buf:      e.buf,
		BlockKey: e.blockKey,
	}, nil
}

func (fc *fakeBServerClient) AddReference(ctx context.Context, arg keybase1.AddReferenceArg) error {
	e, ok := fc.entries[arg.Ref.Bid]
	if !ok {
		return kbfsblock.BServerErrorBlockNonExistent{}
	}
	e.refs[arg.Ref.Nonce] = arg.Ref
	return nil
}

type testBlockServerRemoteConfig struct {
	log            logger.Logger
	codec          kbfscodec.Codec
	signer         kbfscrypto.Signer
	infoGetter     currentInfoGetter
	diskBlockCache DiskBlockCache
}

var _ blockServerRemoteConfig = (*testBlockServerRemoteConfig)(nil)

func (c testBlockServerRemoteConfig) MakeLogger(_ string) logger.Logger {
	return c.log
}

func (c testBlockServerRemoteConfig) Codec() kbfscodec.Codec {
	return c.codec
}

func (c testBlockServerRemoteConfig) Signer() kbfscrypto.Signer {
	return c.signer
}

func (c testBlockServerRemoteConfig) currentInfoGetter() currentInfoGetter {
	return c.infoGetter
}

func (c testBlockServerRemoteConfig) DiskBlockCache() DiskBlockCache {
	return c.diskBlockCache
}

// Test that putting a block, and getting it back, works
func TestBServerRemotePutAndGet(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	currentUID := keybase1.MakeTestUID(1)
	log := logger.NewTestLogger(t)
	fc := fakeBServerClient{
		entries: make(map[keybase1.BlockIdCombo]fakeBlockEntry),
	}
	config := testBlockServerRemoteConfig{log, codec, nil, nil, nil}
	b := newBlockServerRemoteWithClient(config, &fc)

	tlfID := tlf.FakeID(2, false)
	bCtx := kbfsblock.MakeFirstContext(currentUID)
	data := []byte{1, 2, 3, 4}
	bID, err := kbfsblock.MakePermanentID(data)
	require.NoError(t, err)

	serverHalf, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	ctx := context.Background()
	err = b.Put(ctx, tlfID, bID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Now get the same block back.
	buf, sh, err := b.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, sh)

	// Add a reference.
	nonce, err := kbfsblock.MakeRefNonce()
	require.NoError(t, err)
	bCtx2 := kbfsblock.MakeContext(
		currentUID, keybase1.MakeTestUID(2), nonce)
	err = b.AddBlockReference(ctx, tlfID, bID, bCtx2)
	require.NoError(t, err)

	// Now get the same block back.
	buf, sh, err = b.Get(ctx, tlfID, bID, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, sh)
}

// If we cancel the RPC before the RPC returns, the call should error quickly.
func TestBServerRemotePutCanceled(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	currentUID := keybase1.MakeTestUID(1)
	serverConn, conn := rpc.MakeConnectionForTest(t)
	log := logger.NewTestLogger(t)
	config := testBlockServerRemoteConfig{log, codec, nil, nil, nil}
	b := newBlockServerRemoteWithClient(config,
		keybase1.BlockClient{Cli: conn.GetClient()})

	f := func(ctx context.Context) error {
		bID := kbfsblock.FakeID(1)
		tlfID := tlf.FakeID(2, false)
		bCtx := kbfsblock.MakeFirstContext(currentUID)
		data := []byte{1, 2, 3, 4}
		serverHalf := kbfscrypto.MakeBlockCryptKeyServerHalf(
			[32]byte{0x1})
		return b.Put(ctx, tlfID, bID, bCtx, data, serverHalf)
	}
	testRPCWithCanceledContext(t, serverConn, f)
}
