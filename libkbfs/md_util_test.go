// Copyright 2018 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type testBlockCache struct {
	b Block
}

func (c testBlockCache) Get(ptr BlockPointer) (Block, error) {
	return c.b, nil
}

func (testBlockCache) Put(ptr BlockPointer, tlf tlf.ID, block Block,
	lifetime BlockCacheLifetime) error {
	return errors.New("Shouldn't be called")
}

func TestReembedBlockChanges(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	RegisterOps(codec)

	oldDir := BlockPointer{ID: kbfsblock.FakeID(1)}
	co, err := newCreateOp("file", oldDir, File)
	require.NoError(t, err)

	changes := BlockChanges{
		Ops: opsList{co},
	}

	encodedChanges, err := codec.Encode(changes)
	require.NoError(t, err)
	block := &FileBlock{Contents: encodedChanges}

	ctx := context.Background()
	bcache := testBlockCache{block}
	tlfID := tlf.FakeID(1, tlf.Private)
	mode := modeTest{NewInitModeFromType(InitDefault)}

	ptr := BlockPointer{ID: kbfsblock.FakeID(2)}
	pmd := PrivateMetadata{
		Changes: BlockChanges{
			Info: BlockInfo{
				BlockPointer: ptr,
			},
		},
	}

	// We make the cache always return a block, so we can pass in
	// nil for bops and rmdWithKeys.
	err = reembedBlockChanges(ctx, codec, bcache, nil, mode, tlfID, &pmd, nil, logger.NewTestLogger(t))
	require.NoError(t, err)
}
