// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func getKeyBundlesForTesting(t *testing.T, c Config, tlf byte, handleStr string) (
	TlfID, TLFWriterKeyBundleID, *TLFWriterKeyBundleV3, TLFReaderKeyBundleID, *TLFReaderKeyBundleV3) {
	tlfID := FakeTlfID(tlf, false)
	h := parseTlfHandleOrBust(t, c, handleStr, false)
	rmd, err := makeInitialRootMetadata(SegregatedKeyBundlesVer, tlfID, h)
	require.NoError(t, err)
	err = rmd.FakeInitialRekey(c.Crypto())
	require.NoError(t, err)
	wkbID := rmd.bareMd.GetTLFWriterKeyBundleID()
	rkbID := rmd.bareMd.GetTLFReaderKeyBundleID()
	wkb, rkb, ok := getKeyBundlesV3(rmd.extra)
	if !ok {
		t.Fatal("Missing key bundles")
	}
	return tlfID, wkbID, wkb, rkbID, rkb
}

func TestKeyBundleCacheBasic(t *testing.T) {
	config := MakeTestConfigOrBust(t, "alice", "bob", "charlie")
	defer config.Shutdown()

	tlf, wkbID, wkb, rkbID, rkb := getKeyBundlesForTesting(t, config, 1, "alice,bob#charlie")
	tlf2, wkbID2, wkb2, rkbID2, rkb2 := getKeyBundlesForTesting(t, config, 2, "bob,charlie#alice")
	tlf3, wkbID3, wkb3, rkbID3, rkb3 := getKeyBundlesForTesting(t, config, 3, "alice,charlie#bob")

	cache := NewKeyBundleCacheStandard(4)

	checkWkb, err := cache.GetTLFWriterKeyBundle(tlf, wkbID)
	require.NoError(t, err)
	require.Nil(t, checkWkb)
	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf2, wkbID2)
	require.NoError(t, err)
	require.Nil(t, checkWkb)
	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf3, wkbID3)
	require.NoError(t, err)
	require.Nil(t, checkWkb)

	cache.PutTLFWriterKeyBundle(tlf, wkbID, wkb)
	// add the same bundle twice
	cache.PutTLFWriterKeyBundle(tlf, wkbID, wkb)
	cache.PutTLFWriterKeyBundle(tlf2, wkbID2, wkb2)

	checkRkb, err := cache.GetTLFReaderKeyBundle(tlf, rkbID)
	require.NoError(t, err)
	require.Nil(t, checkRkb)
	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf2, rkbID2)
	require.NoError(t, err)
	require.Nil(t, checkRkb)
	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf3, rkbID3)
	require.NoError(t, err)
	require.Nil(t, checkRkb)

	cache.PutTLFReaderKeyBundle(tlf, rkbID, rkb)
	// add the same bundle twice
	cache.PutTLFReaderKeyBundle(tlf, rkbID, rkb)
	cache.PutTLFReaderKeyBundle(tlf2, rkbID2, rkb2)

	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf, wkbID)
	require.NoError(t, err)
	require.NotNil(t, checkWkb)
	require.Equal(t, checkWkb, wkb)

	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf2, wkbID2)
	require.NoError(t, err)
	require.NotNil(t, checkWkb)
	require.Equal(t, checkWkb, wkb2)

	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf3, wkbID3)
	require.NoError(t, err)
	require.Nil(t, checkWkb)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf, rkbID)
	require.NoError(t, err)
	require.NotNil(t, checkRkb)
	require.Equal(t, checkRkb, rkb)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf2, rkbID2)
	require.NoError(t, err)
	require.NotNil(t, checkRkb)
	require.Equal(t, checkRkb, rkb2)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf3, rkbID3)
	require.NoError(t, err)
	require.Nil(t, checkRkb)

	cache.PutTLFReaderKeyBundle(tlf3, rkbID3, rkb3)
	cache.PutTLFWriterKeyBundle(tlf3, wkbID3, wkb3)

	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf, wkbID)
	require.NoError(t, err)
	require.Nil(t, checkWkb)
	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf2, wkbID2)
	require.NoError(t, err)
	require.Nil(t, checkWkb)
	checkWkb, err = cache.GetTLFWriterKeyBundle(tlf3, wkbID3)
	require.NoError(t, err)
	require.NotNil(t, checkWkb)
	require.Equal(t, checkWkb, wkb3)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf, rkbID)
	require.NoError(t, err)
	require.NotNil(t, checkRkb)
	require.Equal(t, checkRkb, rkb)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf2, rkbID2)
	require.NoError(t, err)
	require.NotNil(t, checkRkb)
	require.Equal(t, checkRkb, rkb2)

	checkRkb, err = cache.GetTLFReaderKeyBundle(tlf3, rkbID3)
	require.NoError(t, err)
	require.NotNil(t, checkRkb)
	require.Equal(t, checkRkb, rkb3)
}
