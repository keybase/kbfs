// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/stretchr/testify/require"
)

func TestRootMetadataVersionV2(t *testing.T) {
	tlfID := FakeTlfID(1, false)

	uid := keybase1.MakeTestUID(1)
	bh, err := MakeBareTlfHandle(
		[]keybase1.UID{uid}, nil, []keybase1.SocialAssertion{
			keybase1.SocialAssertion{}},
		nil, nil)
	require.NoError(t, err)

	rmd, err := MakeInitialBareRootMetadata(
		InitialExtraMetadataVer, tlfID, bh)
	require.NoError(t, err)

	require.Equal(t, InitialExtraMetadataVer, rmd.Version())

	/*
		// All other folders should use the pre-extra MD version.
		tlfID2 := FakeTlfID(2, false)
		h2 := parseTlfHandleOrBust(t, config, "alice,charlie", false)
		rmd2, err := makeInitialRootMetadata(
			config.MetadataVersion(), tlfID2, h2)
		require.NoError(t, err)
		rmds2, err := MakeRootMetadataSigned(
			kbfscrypto.SignatureInfo{}, kbfscrypto.SignatureInfo{},
			rmd2.bareMd, time.Time{})
		require.NoError(t, err)
		if g, e := rmds2.Version(), MetadataVer(PreExtraMetadataVer); g != e {
			t.Errorf("MD without unresolved users got wrong version %d, "+
				"expected %d", g, e)
		}

		// ... including if the assertions get resolved.
		AddNewAssertionForTestOrBust(t, config, "bob", "bob@twitter")
		rmd.SetSerializedPrivateMetadata([]byte{1}) // MakeSuccessor requires this
		rmd.FakeInitialRekey(config.Crypto())
		if rmd.GetSerializedPrivateMetadata() == nil {
			t.Fatalf("Nil private MD")
		}
		h3, err := h.ResolveAgain(context.Background(), config.KBPKI())
		if err != nil {
			t.Fatalf("Couldn't resolve again: %v", err)
		}
		rmd3, err := rmd.MakeSuccessor(config.Codec(), fakeMdID(1), true)
		if err != nil {
			t.Fatalf("Couldn't make MD successor: %v", err)
		}
		rmd3.bareMd.FakeInitialRekey(
			config.Crypto(), h3.ToBareHandleOrBust(),
			kbfscrypto.TLFPublicKey{})
		err = rmd3.updateFromTlfHandle(h3)
		if err != nil {
			t.Fatalf("Couldn't update TLF handle: %v", err)
		}
		rmds3, err := MakeRootMetadataSigned(
			kbfscrypto.SignatureInfo{}, kbfscrypto.SignatureInfo{},
			rmd3.bareMd, time.Time{})
		require.NoError(t, err)
		if g, e := rmds3.Version(), MetadataVer(PreExtraMetadataVer); g != e {
			t.Errorf("MD without unresolved users got wrong version %d, "+
				"expected %d", g, e)
		}
	*/
}
