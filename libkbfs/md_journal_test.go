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
	codec Codec, crypto CryptoCommon,
	uid keybase1.UID, id TlfID, h BareTlfHandle,
	signer cryptoSigner, verifyingKey VerifyingKey,
	ekg singleEncryptionKeyGetter, tempdir string, j mdJournal) {
	codec = NewCodecMsgpack()
	crypto = MakeCryptoCommon(codec)

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

	return codec, crypto, uid, id, h, signer, verifyingKey, ekg, tempdir, j
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
	codec, crypto, uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// Should start off as empty.

	head, err := j.getHead(uid)
	require.NoError(t, err)
	require.Equal(t, ImmutableBareRootMetadata{}, head)
	require.Equal(t, 0, getTlfJournalLength(t, j))

	// Push some new metadata blocks.

	ctx := context.Background()

	firstRevision := MetadataRevision(10)
	firstPrevRoot := fakeMdID(1)
	mdCount := 10

	prevRoot := firstPrevRoot
	for i := 0; i < mdCount; i++ {
		revision := firstRevision + MetadataRevision(i)
		md := makeMDForTest(t, id, h, revision, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
		require.NoError(t, err)
		prevRoot = mdID
	}

	require.Equal(t, mdCount, getTlfJournalLength(t, j))

	// Should now be non-empty.

	ibrmds, err := j.getRange(
		uid, 1, firstRevision+MetadataRevision(2*mdCount))
	require.NoError(t, err)
	require.Equal(t, mdCount, len(ibrmds))

	require.Equal(t, firstRevision, ibrmds[0].Revision)
	require.Equal(t, firstPrevRoot, ibrmds[0].PrevRoot)
	err = ibrmds[0].IsValidAndSigned(codec, crypto, uid, verifyingKey)
	require.NoError(t, err)

	for i := 1; i < len(ibrmds); i++ {
		err := ibrmds[i].IsValidAndSigned(
			codec, crypto, uid, verifyingKey)
		require.NoError(t, err)
		err = ibrmds[i-1].CheckValidSuccessor(
			ibrmds[i-1].mdID, ibrmds[i].BareRootMetadata)
		require.NoError(t, err)
	}

	head, err = j.getHead(uid)
	require.NoError(t, err)
	require.Equal(t, ibrmds[len(ibrmds)-1], head)
}

func TestMDJournalBranchConversion(t *testing.T) {
	codec, crypto, uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	ctx := context.Background()

	firstRevision := MetadataRevision(10)
	firstPrevRoot := fakeMdID(1)
	mdCount := 10

	prevRoot := firstPrevRoot
	for i := 0; i < mdCount; i++ {
		revision := firstRevision + MetadataRevision(i)
		md := makeMDForTest(t, id, h, revision, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
		require.NoError(t, err)
		prevRoot = mdID
	}

	log := logger.NewTestLogger(t)
	err := j.convertToBranch(ctx, log, signer, uid, verifyingKey)
	require.NoError(t, err)

	ibrmds, err := j.getRange(
		uid, 1, firstRevision+MetadataRevision(2*mdCount))
	require.NoError(t, err)
	require.Equal(t, mdCount, len(ibrmds))

	require.Equal(t, firstRevision, ibrmds[0].Revision)
	require.Equal(t, firstPrevRoot, ibrmds[0].PrevRoot)
	require.Equal(t, Unmerged, ibrmds[0].MergedStatus())
	err = ibrmds[0].IsValidAndSigned(codec, crypto, uid, verifyingKey)
	bid := ibrmds[0].BID
	require.NotEqual(t, NullBranchID, bid)

	require.NoError(t, err)
	for i := 1; i < len(ibrmds); i++ {
		require.Equal(t, Unmerged, ibrmds[i].MergedStatus())
		require.Equal(t, bid, ibrmds[i].BID)
		err := ibrmds[i].IsValidAndSigned(
			codec, crypto, uid, verifyingKey)
		require.NoError(t, err)
		err = ibrmds[i-1].CheckValidSuccessor(
			ibrmds[i-1].mdID, ibrmds[i].BareRootMetadata)
		require.NoError(t, err)
	}

	require.Equal(t, 10, getTlfJournalLength(t, j))

	head, err := j.getHead(uid)
	require.NoError(t, err)
	require.Equal(t, ibrmds[len(ibrmds)-1], head)
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
	_, crypto, uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// (2) Push some new metadata blocks.

	ctx := context.Background()

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 10; i++ {
		md := makeMDForTest(t, id, h, i, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
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
	_, crypto, uid, id, h, signer, verifyingKey, ekg, tempdir, j :=
		setupMDJournalTest(t)
	defer teardownMDJournalTest(t, tempdir)

	// (2) Push some new metadata blocks.

	ctx := context.Background()

	prevRoot := MdID{}
	for i := MetadataRevision(1); i <= 9; i++ {
		md := makeMDForTest(t, id, h, i, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
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
		md := makeMDForTest(t, id, h, i, uid, prevRoot)
		mdID, err := j.put(ctx, signer, ekg, md, uid, verifyingKey)
		require.IsType(t, MDJournalConflictError{}, err)

		md.WFlags |= MetadataFlagUnmerged
		mdID, err = j.put(ctx, signer, ekg, md, uid, verifyingKey)
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
