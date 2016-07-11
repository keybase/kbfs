// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/require"
)

func getMDJournalLength(t *testing.T, s *mdServerTlfStorage, bid IFCERFTBranchID) int {
	len, err := s.journalLength(bid)
	require.NoError(t, err)
	return int(len)
}

// TestMDServerTlfStorageBasic copies TestMDServerBasics, but for a
// single mdServerTlfStorage.
func TestMDServerTlfStorageBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_storage")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDServerTlfStorage(codec, crypto, tempdir)
	defer s.shutdown()

	require.Equal(t, 0, getMDJournalLength(t, s, IFCERFTNullBranchID))

	uid := keybase1.MakeTestUID(1)
	deviceKID := keybase1.KID("fake kid")
	id := FakeTlfID(1, false)
	h, err := IFCERFTMakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (1) Validate merged branch is empty.

	head, err := s.getForTLF(uid, deviceKID, IFCERFTNullBranchID)
	require.NoError(t, err)
	require.Nil(t, head)

	require.Equal(t, 0, getMDJournalLength(t, s, IFCERFTNullBranchID))

	// (2) Push some new metadata blocks.

	prevRoot := IFCERFTMdID{}
	middleRoot := IFCERFTMdID{}
	for i := IFCERFTMetadataRevision(1); i <= 10; i++ {
		rmds, err := NewRootMetadataSignedForTest(id, h)
		require.NoError(t, err)

		rmds.MD.SerializedPrivateMetadata = make([]byte, 1)
		rmds.MD.SerializedPrivateMetadata[0] = 0x1
		rmds.MD.Revision = IFCERFTMetadataRevision(i)
		FakeInitialRekey(&rmds.MD, h)
		rmds.MD.ClearCachedMetadataIDForTest()
		if i > 1 {
			rmds.MD.PrevRoot = prevRoot
		}
		recordBranchID, err := s.put(uid, deviceKID, rmds)
		require.NoError(t, err)
		require.False(t, recordBranchID)
		prevRoot, err = rmds.MD.MetadataID(crypto)
		require.NoError(t, err)
		if i == 5 {
			middleRoot = prevRoot
		}
	}

	require.Equal(t, 10, getMDJournalLength(t, s, IFCERFTNullBranchID))

	// (3) Trigger a conflict.

	rmds, err := NewRootMetadataSignedForTest(id, h)
	require.NoError(t, err)
	rmds.MD.Revision = IFCERFTMetadataRevision(10)
	rmds.MD.SerializedPrivateMetadata = make([]byte, 1)
	rmds.MD.SerializedPrivateMetadata[0] = 0x1
	FakeInitialRekey(&rmds.MD, h)
	rmds.MD.PrevRoot = prevRoot
	_, err = s.put(uid, deviceKID, rmds)
	require.IsType(t, MDServerErrorConflictRevision{}, err)

	require.Equal(t, 10, getMDJournalLength(t, s, IFCERFTNullBranchID))

	// (4) Push some new unmerged metadata blocks linking to the
	// middle merged block.

	prevRoot = middleRoot
	bid := FakeBranchID(1)
	for i := IFCERFTMetadataRevision(6); i < 41; i++ {
		rmds, err := NewRootMetadataSignedForTest(id, h)
		require.NoError(t, err)
		rmds.MD.Revision = IFCERFTMetadataRevision(i)
		rmds.MD.SerializedPrivateMetadata = make([]byte, 1)
		rmds.MD.SerializedPrivateMetadata[0] = 0x1
		rmds.MD.PrevRoot = prevRoot
		FakeInitialRekey(&rmds.MD, h)
		rmds.MD.ClearCachedMetadataIDForTest()
		rmds.MD.WFlags |= IFCERFTMetadataFlagUnmerged
		rmds.MD.BID = bid
		recordBranchID, err := s.put(uid, deviceKID, rmds)
		require.NoError(t, err)
		require.Equal(t, i == IFCERFTMetadataRevision(6), recordBranchID)
		prevRoot, err = rmds.MD.MetadataID(crypto)
		require.NoError(t, err)
	}

	require.Equal(t, 10, getMDJournalLength(t, s, IFCERFTNullBranchID))
	require.Equal(t, 35, getMDJournalLength(t, s, bid))

	// (5) Check for proper unmerged head.

	head, err = s.getForTLF(uid, deviceKID, bid)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, IFCERFTMetadataRevision(40), head.MD.Revision)

	require.Equal(t, 10, getMDJournalLength(t, s, IFCERFTNullBranchID))
	require.Equal(t, 35, getMDJournalLength(t, s, bid))

	// (6) Try to get unmerged range.

	rmdses, err := s.getRange(uid, deviceKID, bid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 35, len(rmdses))
	for i := IFCERFTMetadataRevision(6); i < 16; i++ {
		require.Equal(t, i, rmdses[i-6].MD.Revision)
	}

	// Nothing corresponds to (7) - (9) from MDServerTestBasics.

	// (10) Check for proper merged head.

	head, err = s.getForTLF(uid, deviceKID, IFCERFTNullBranchID)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, IFCERFTMetadataRevision(10), head.MD.Revision)

	// (11) Try to get merged range.

	rmdses, err = s.getRange(uid, deviceKID, IFCERFTNullBranchID, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmdses))
	for i := IFCERFTMetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmdses[i-1].MD.Revision)
	}

	require.Equal(t, 10, getMDJournalLength(t, s, IFCERFTNullBranchID))
	require.Equal(t, 35, getMDJournalLength(t, s, bid))
}
