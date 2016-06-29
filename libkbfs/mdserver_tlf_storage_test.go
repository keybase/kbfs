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

	s, err := makeMDServerTlfStorage(codec, crypto, tempdir)
	require.NoError(t, err)
	defer s.shutdown()

	uid := keybase1.MakeTestUID(1)
	deviceKID := keybase1.KID("fake kid")
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	head, err := s.getForTLF(uid, deviceKID, NullBranchID)
	require.NoError(t, err)
	require.Nil(t, head)

	// Push some new metadata blocks.

	prevRoot := MdID{}
	middleRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		rmds, err := NewRootMetadataSignedForTest(id, h)
		require.NoError(t, err)

		rmds.MD.SerializedPrivateMetadata = make([]byte, 1)
		rmds.MD.SerializedPrivateMetadata[0] = 0x1
		rmds.MD.Revision = MetadataRevision(i)
		FakeInitialRekey(&rmds.MD, h)
		rmds.MD.clearCachedMetadataIDForTest()
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

	_ = middleRoot
}
