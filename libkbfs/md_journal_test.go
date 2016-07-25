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

func getTlfJournalLength(t *testing.T, j mdJournal) int {
	len, err := j.length()
	require.NoError(t, err)
	return int(len)
}

func setupMDJournalTest(t *testing.T) (
	uid keybase1.UID, id TlfID, h BareTlfHandle,
	signer cryptoSigner, verifyingKey VerifyingKey,
	ekg singleEncryptionKeyGetter, tempdir string, j mdJournal) {
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)

	uid = keybase1.MakeTestUID(1)
	id = FakeTlfID(1, false)
	h, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	signingKey := MakeFakeSigningKeyOrBust("fake seed")
	signer = cryptoSignerLocal{signingKey}
	verifyingKey = signingKey.GetVerifyingKey()
	ekg = singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}

	// Do this last so we don't have to worry about cleaning up
	// the tempdir if anything else errors.
	tempdir, err = ioutil.TempDir(os.TempDir(), "mdserver_tlf_journal")
	require.NoError(t, err)

	j = makeMDJournal(codec, crypto, tempdir)

	return uid, id, h, signer, verifyingKey, ekg, tempdir, j
}

func teardownMDJournalTest(t *testing.T, tempdir string) {
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
}

func makeMDForTest(t *testing.T, id TlfID, h BareTlfHandle,
	revision MetadataRevision, uid keybase1.UID,
	prevRoot MdID) *RootMetadata {
	var md RootMetadata
	err := updateNewBareRootMetadata(&md.BareRootMetadata, id, h)
	require.NoError(t, err)
	md.Revision = revision
	FakeInitialRekey(&md.BareRootMetadata, h)
	md.PrevRoot = prevRoot
	return &md
}

func TestMDJournalBasic(t *testing.T) {
	uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// Should start off as empty.

	head, err := j.get(uid)
	require.NoError(t, err)
	require.Nil(t, head)
	require.Equal(t, 0, getTlfJournalLength(t, j))

	// Push some new metadata blocks.

	ctx := context.Background()

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		md := makeMDForTest(t, id, h, i, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
		require.NoError(t, err)
		prevRoot = mdID
	}

	require.Equal(t, 10, getTlfJournalLength(t, j))

	// Should now be non-empty.

	head, err = j.get(uid)
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, MetadataRevision(10), head.Revision)

	rmds, err := j.getRange(uid, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 10, len(rmds))
	for i := MetadataRevision(1); i <= 10; i++ {
		require.Equal(t, i, rmds[i-1].Revision)
	}

	require.Equal(t, 10, getTlfJournalLength(t, j))
}

func TestMDJournalBranchConversion(t *testing.T) {
	uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	ctx := context.Background()

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
		mdID, err := j.put(ctx, signer, ekg, &md, uid, verifyingKey)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	log := logger.NewTestLogger(t)
	err := j.convertToBranch(ctx, log, signer, uid, verifyingKey)
	require.NoError(t, err)

	rmds, err := j.getRange(uid, 1, 100)
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

		require.NoError(t, err)
		prevRoot = rmds[i-1].mdID
	}

	require.Equal(t, 10, getTlfJournalLength(t, j))
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
	uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// (2) Push some new metadata blocks.

	ctx := context.Background()

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
		mdID, err := j.put(ctx, signer, ekg, &md, uid, verifyingKey)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	log := logger.NewTestLogger(t)
	var mdserver shimMDServer
	for {
		flushed, err := j.flushOne(ctx, log, signer, uid, verifyingKey, &mdserver)
		require.NoError(t, err)
		if !flushed {
			break
		}
	}

	require.Equal(t, 10, len(mdserver.rmdses))
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)
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

	require.Equal(t, 0, getTlfJournalLength(t, j))
}

func TestMDJournalFlushConflict(t *testing.T) {
	uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// (2) Push some new metadata blocks.

	ctx := context.Background()

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
		mdID, err := j.put(ctx, signer, ekg, &md, uid, verifyingKey)
		require.NoError(t, err, "i=%d", i)
		prevRoot = mdID
	}

	var mdserver shimMDServer

	mdserver.err = MDServerErrorConflictRevision{}

	log := logger.NewTestLogger(t)
	flushed, err := j.flushOne(ctx, log, signer, uid, verifyingKey, &mdserver)
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
		mdID, err := j.put(ctx, signer, ekg, &md, uid, verifyingKey)
		require.IsType(t, MDJournalConflictError{}, err)

		md.WFlags |= MetadataFlagUnmerged
		mdID, err = j.put(ctx, signer, ekg, &md, uid, verifyingKey)
		require.NoError(t, err)

		prevRoot = mdID
	}

	for {
		flushed, err := j.flushOne(ctx, log, signer, uid, verifyingKey, &mdserver)
		require.NoError(t, err)
		if !flushed {
			break
		}
	}

	require.Equal(t, 10, len(mdserver.rmdses))
	codec := NewCodecMsgpack()
	crypto := MakeCryptoCommon(codec)
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

	require.Equal(t, 0, getTlfJournalLength(t, j))
}
