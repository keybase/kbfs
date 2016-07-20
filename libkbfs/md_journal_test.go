// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/require"
)

type singleEncryptionKeyGetter struct {
	k TLFCryptKey
}

func (g singleEncryptionKeyGetter) GetTLFCryptKeyForEncryption(
	ctx context.Context, md ReadOnlyRootMetadata) (TLFCryptKey, error) {
	return g.k, nil
}

func getTlfJournalLength(t *testing.T, s *mdJournal) int {
	len, err := s.length()
	require.NoError(t, err)
	return int(len)
}

// TestMDJournalBasic copies TestMDServerBasics, but for a
// single mdJournal.
func TestMDJournalBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDJournal(codec, crypto, tempdir)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (1) Validate merged branch is empty.

	head, err := s.get(uid)
	require.NoError(t, err)
	require.Nil(t, head)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	// (2) Push some new metadata blocks.

	var signer fakeSigner
	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, signer, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	require.Equal(t, 10, getTlfJournalLength(t, s))

	// (10) Check for proper merged head.

	head, err = s.get(uid)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, MetadataRevision(10), head.Revision)

	// (11) Try to get merged range.

	rmds, err := s.getRange(uid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmds))
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmds[i-1].Revision)
	}

	require.Equal(t, 10, getTlfJournalLength(t, s))
}

func TestMDJournalBranchConversion(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDJournal(codec, crypto, tempdir)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (2) Push some new metadata blocks.

	var signer fakeSigner
	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, signer, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	log := logger.NewTestLogger(t)
	err = s.convertToBranch(log)
	require.NoError(t, err)

	rmds, err := s.getRange(uid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmds))
	prevRoot = MdID{}
	bid := rmds[0].BID
	// TODO: Check first PrevRoot.
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmds[i-1].Revision)
		require.Equal(t, bid, rmds[i-1].BID)
		require.Equal(t, Unmerged, rmds[i-1].MergedStatus())

		if prevRoot != (MdID{}) {
			require.Equal(t, prevRoot, rmds[i-1].PrevRoot)
		}

		currRoot, err := crypto.MakeMdID(rmds[i-1])
		require.NoError(t, err)
		prevRoot = currRoot
	}

	require.Equal(t, 10, getTlfJournalLength(t, s))
}

type fakeSigner struct{}

func (fakeSigner) Sign(ctx context.Context, msg []byte) (sigInfo SignatureInfo, err error) {
	return SignatureInfo{
		Version:      1,
		Signature:    []byte{0x1},
		VerifyingKey: VerifyingKey{},
	}, nil
}

type shimMDServer struct {
	MDServer
	rmdses []*RootMetadataSigned
	err    error
}

func (s *shimMDServer) Put(ctx context.Context, rmds *RootMetadataSigned) error {
	if s.err != nil {
		err := s.err
		s.err = nil
		return err
	}
	s.rmdses = append(s.rmdses, rmds)
	return nil
}

func TestMDJournalFlushBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDJournal(codec, crypto, tempdir)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (2) Push some new metadata blocks.

	var signer fakeSigner
	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, signer, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	ctx := context.Background()
	var mdserver shimMDServer
	log := logger.NewTestLogger(t)
	for {
		flushed, err := s.flushOne(ctx, signer, &mdserver, log)
		require.NoError(t, err)
		if !flushed {
			break
		}
	}

	require.Equal(t, 10, len(mdserver.rmdses))
	var prev *RootMetadataSigned
	var prevID MdID
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, mdserver.rmdses[i-1].MD.Revision)
		if prev != nil {
			err := prev.MD.CheckValidSuccessorForServer(
				prevID, &mdserver.rmdses[i-1].MD)
			require.NoError(t, err, "i=%d", i)
		}
		prev = mdserver.rmdses[i-1]
		var err error
		prevID, err = crypto.MakeMdID(&prev.MD)
		require.NoError(t, err)
	}

	require.Equal(t, 0, getTlfJournalLength(t, s))
}

func TestMDJournalFlushConflict(t *testing.T) {
	codec := NewCodecMsgpack()
	crypto := makeTestCryptoCommon(t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		require.NoError(t, err)
	}()

	s := makeMDJournal(codec, crypto, tempdir)

	require.Equal(t, 0, getTlfJournalLength(t, s))

	uid := keybase1.MakeTestUID(1)
	id := FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	// (2) Push some new metadata blocks.

	var signer fakeSigner
	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 9; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, signer, ekg, uid, &md)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	ctx := context.Background()
	var mdserver shimMDServer
	log := logger.NewTestLogger(t)

	mdserver.err = MDServerErrorConflictRevision{}

	flushed, err := s.flushOne(ctx, signer, &mdserver, log)
	require.NoError(t, err)
	require.True(t, flushed)

	for i := MetadataRevision(10); i <= 10; i++ {
		var md RootMetadata
		err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
		require.NoError(t, err)

		md.SerializedPrivateMetadata = []byte{0x1}
		md.Revision = MetadataRevision(i)
		FakeInitialRekey(&md.BareRootMetadata, h)
		if i > 1 {
			md.PrevRoot = prevRoot
		}
		ctx := context.Background()
		mdID, err := s.put(ctx, signer, ekg, uid, &md)
		require.IsType(t, MDJournalConflictError{}, err)

		md.WFlags |= MetadataFlagUnmerged
		mdID, err = s.put(ctx, signer, ekg, uid, &md)
		require.NoError(t, err)

		prevRoot = mdID
	}

	for {
		flushed, err := s.flushOne(ctx, signer, &mdserver, log)
		require.NoError(t, err)
		if !flushed {
			break
		}
	}

	require.Equal(t, 10, len(mdserver.rmdses))
	var prev *RootMetadataSigned
	var prevID MdID
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, mdserver.rmdses[i-1].MD.Revision)
		if prev != nil {
			err := prev.MD.CheckValidSuccessorForServer(
				prevID, &mdserver.rmdses[i-1].MD)
			require.NoError(t, err, "i=%d", i)
		}
		prev = mdserver.rmdses[i-1]
		var err error
		prevID, err = crypto.MakeMdID(&prev.MD)
		require.NoError(t, err)
	}

	require.Equal(t, 0, getTlfJournalLength(t, s))
}
