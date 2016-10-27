// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/stretchr/testify/require"
)

func mdCacheInit(t *testing.T, cap int) (
	mockCtrl *gomock.Controller, config *ConfigMock) {
	ctr := NewSafeTestReporter(t)
	mockCtrl = gomock.NewController(ctr)
	config = NewConfigMock(mockCtrl, ctr)
	mdcache := NewMDCacheStandard(cap)
	config.SetMDCache(mdcache)
	interposeDaemonKBPKI(config, "alice", "bob", "charlie")
	return
}

func mdCacheShutdown(mockCtrl *gomock.Controller, config *ConfigMock) {
	config.ctr.CheckForFailures()
	mockCtrl.Finish()
}

func testMdcachePut(t *testing.T, tlf TlfID, rev MetadataRevision,
	mStatus MergeStatus, bid BranchID, h *TlfHandle, config *ConfigMock) {
	key, err := config.KBPKI().GetCurrentVerifyingKey(context.Background())
	if err != nil {
		t.Fatalf("Couldn't get verifying key: %v", err)
	}

	rmd := MakeRootMetadata(
		&BareRootMetadataV2{
			WriterMetadataV2: WriterMetadataV2{
				ID:    tlf,
				WKeys: make(TLFWriterKeyGenerations, 0, 1),
				BID:   bid,
			},
			WriterMetadataSigInfo: kbfscrypto.SignatureInfo{
				VerifyingKey: key,
			},
			Revision: rev,
			RKeys:    make(TLFReaderKeyGenerations, 1, 1),
		}, nil, h)
	rmd.AddNewKeysForTesting(config.Crypto(),
		NewEmptyUserDeviceKeyInfoMap(), NewEmptyUserDeviceKeyInfoMap())
	if mStatus == Unmerged {
		rmd.SetUnmerged()
	}

	// put the md
	irmd := MakeImmutableRootMetadata(rmd, key, fakeMdID(1), time.Now())
	if err := config.MDCache().Put(irmd); err != nil {
		t.Errorf("Got error on put on md %v: %v", tlf, err)
	}

	// make sure we can get it successfully
	irmd2, err := config.MDCache().Get(tlf, rev, bid)
	require.NoError(t, err)
	require.Equal(t, irmd, irmd2)
}

func TestMdcachePut(t *testing.T) {
	mockCtrl, config := mdCacheInit(t, 100)
	defer mdCacheShutdown(mockCtrl, config)

	id := FakeTlfID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	h.resolvedWriters[keybase1.MakeTestUID(0)] = "test_user0"

	testMdcachePut(t, id, 1, Merged, NullBranchID, h, config)
}

func TestMdcachePutPastCapacity(t *testing.T) {
	mockCtrl, config := mdCacheInit(t, 2)
	defer mdCacheShutdown(mockCtrl, config)

	id0 := FakeTlfID(1, false)
	h0 := parseTlfHandleOrBust(t, config, "alice", false)

	id1 := FakeTlfID(2, false)
	h1 := parseTlfHandleOrBust(t, config, "alice,bob", false)

	id2 := FakeTlfID(3, false)
	h2 := parseTlfHandleOrBust(t, config, "alice,charlie", false)

	testMdcachePut(t, id0, 0, Merged, NullBranchID, h0, config)
	bid := FakeBranchID(1)
	testMdcachePut(t, id1, 0, Unmerged, bid, h1, config)
	testMdcachePut(t, id2, 1, Merged, NullBranchID, h2, config)

	// id 0 should no longer be in the cache
	// make sure we can get it successfully
	expectedErr := NoSuchMDError{id0, 0, NullBranchID}
	if _, err := config.MDCache().Get(id0, 0, NullBranchID); err == nil {
		t.Errorf("No expected error on get")
	} else if err != expectedErr {
		t.Errorf("Got unexpected error on get: %v", err)
	}
}

func TestMdcacheReplace(t *testing.T) {
	mockCtrl, config := mdCacheInit(t, 100)
	defer mdCacheShutdown(mockCtrl, config)

	id := FakeTlfID(1, false)
	h := parseTlfHandleOrBust(t, config, "alice", false)
	h.resolvedWriters[keybase1.MakeTestUID(0)] = "test_user0"

	testMdcachePut(t, id, 1, Merged, NullBranchID, h, config)

	irmd, err := config.MDCache().Get(id, 1, NullBranchID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	// Change the BID
	bid := FakeBranchID(1)
	newRmd, err := irmd.deepCopy(kbfscodec.NewMsgpack())
	if err != nil {
		t.Fatalf("Deep-copy error: %v", err)
	}

	newRmd.SetBranchID(bid)
	err = config.MDCache().Replace(MakeImmutableRootMetadata(newRmd,
		irmd.LastModifyingWriterVerifyingKey(), fakeMdID(2), time.Now()), NullBranchID)
	if err != nil {
		t.Fatalf("Replace error: %v", err)
	}

	_, err = config.MDCache().Get(id, 1, NullBranchID)
	if _, ok := err.(NoSuchMDError); !ok {
		t.Fatalf("Unexpected err after replace: %v", err)
	}
	_, err = config.MDCache().Get(id, 1, bid)
	if err != nil {
		t.Fatalf("Get error after replace: %v", err)
	}
}
