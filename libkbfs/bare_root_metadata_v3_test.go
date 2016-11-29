// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
)

func TestRootMetadataVersionV3(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	// All V3 objects should have SegregatedKeyBundlesVer.

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)

	require.Equal(t, SegregatedKeyBundlesVer, rmd.Version())
}

func TestIsValidRekeyRequestBasicV3(t *testing.T) {
	tlfID := tlf.FakeID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	codec := kbfscodec.NewMsgpack()
	crypto := MakeCryptoCommon(kbfscodec.NewMsgpack())

	brmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	extra := FakeInitialRekey(brmd, crypto, bh, kbfscrypto.TLFPublicKey{})

	newBrmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)
	newExtra := FakeInitialRekey(
		newBrmd, crypto, bh, kbfscrypto.TLFPublicKey{})

	ok, err := newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), extra, newExtra)
	require.NoError(t, err)
	// Should fail because the copy bit is unset.
	require.False(t, ok)

	// Set the copy bit; note the writer metadata is the same.
	newBrmd.SetWriterMetadataCopiedBit()

	// There's no internal signature to compare, so this should
	// then work.

	ok, err = newBrmd.IsValidRekeyRequest(
		codec, brmd, newBrmd.LastModifyingWriter(), extra, newExtra)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRootMetadataPublicVersionV3(t *testing.T) {
	tlfID := tlf.FakeID(1, true)

	uid := keybase1.MakeTestUID(1)
	bh, err := tlf.MakeHandle([]keybase1.UID{uid}, []keybase1.UID{keybase1.PublicUID}, nil, nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadataV3(tlfID, bh)
	require.NoError(t, err)
	require.Equal(t, SegregatedKeyBundlesVer, rmd.Version())

	bh2, err := rmd.MakeBareTlfHandle(nil)
	require.Equal(t, bh, bh2)
}
