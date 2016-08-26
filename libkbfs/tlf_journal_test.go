// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type testBWDelegate struct {
	t *testing.T
	// Store a context so that the tlfJournal's background context
	// will also obey the test timeout.
	testCtx    context.Context
	stateCh    chan bwState
	shutdownCh chan struct{}
}

func (d testBWDelegate) GetBackgroundContext() context.Context {
	return d.testCtx
}

func (d testBWDelegate) OnNewState(ctx context.Context, bws bwState) {
	select {
	case d.stateCh <- bws:
	case <-ctx.Done():
		assert.Fail(d.t, ctx.Err().Error())
	}
}

func (d testBWDelegate) OnShutdown(ctx context.Context) {
	select {
	case d.shutdownCh <- struct{}{}:
	case <-ctx.Done():
		assert.Fail(d.t, ctx.Err().Error())
	}
}

func (d testBWDelegate) requireNextState(
	ctx context.Context, expectedState bwState) {
	select {
	case bws := <-d.stateCh:
		require.Equal(d.t, expectedState, bws)
	case <-ctx.Done():
		assert.Fail(d.t, ctx.Err().Error())
	}
}

type testTLFJournalConfig struct {
	t        *testing.T
	tlfID    TlfID
	splitter BlockSplitter
	codec    Codec
	crypto   CryptoLocal
	cig      singleCurrentInfoGetter
	ekg      singleEncryptionKeyGetter
	mdserver MDServer
}

func (c testTLFJournalConfig) BlockSplitter() BlockSplitter {
	return c.splitter
}

func (c testTLFJournalConfig) Codec() Codec {
	return c.codec
}

func (c testTLFJournalConfig) Crypto() Crypto {
	return c.crypto
}

func (c testTLFJournalConfig) cryptoPure() cryptoPure {
	return c.crypto
}

func (c testTLFJournalConfig) currentInfoGetter() currentInfoGetter {
	return c.cig
}

func (c testTLFJournalConfig) encryptionKeyGetter() encryptionKeyGetter {
	return c.ekg
}

func (c testTLFJournalConfig) MDServer() MDServer {
	return c.mdserver
}

func (c testTLFJournalConfig) MakeLogger(module string) logger.Logger {
	return logger.NewTestLogger(c.t)
}

func (c testTLFJournalConfig) makeMDForTest(
	revision MetadataRevision, prevRoot MdID) *RootMetadata {
	h, err := MakeBareTlfHandle(
		[]keybase1.UID{c.cig.uid}, nil, nil, nil, nil)
	require.NoError(c.t, err)
	md := NewRootMetadata()
	err = md.Update(c.tlfID, h)
	require.NoError(c.t, err)
	md.SetRevision(revision)
	md.FakeInitialRekey(h)
	md.SetPrevRoot(prevRoot)
	return md
}

func setupTLFJournalTest(t *testing.T) (
	tempdir string, config *testTLFJournalConfig, ctx context.Context,
	cancel context.CancelFunc, tlfJournal *tlfJournal,
	delegate testBWDelegate) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "tlf_journal")
	require.NoError(t, err)

	bsplitter := &BlockSplitterSimple{64 * 1024, 8 * 1024}
	codec := NewCodecMsgpack()
	signingKey := MakeFakeSigningKeyOrBust("client sign")
	cryptPrivateKey := MakeFakeCryptPrivateKeyOrBust("client crypt private")
	crypto := NewCryptoLocal(codec, signingKey, cryptPrivateKey)
	cig := singleCurrentInfoGetter{
		name:         "fake_user",
		uid:          keybase1.MakeTestUID(1),
		verifyingKey: signingKey.GetVerifyingKey(),
	}
	ekg := singleEncryptionKeyGetter{MakeTLFCryptKey([32]byte{0x1})}
	mdserver, err := NewMDServerMemory(newTestMDServerLocalConfig(t, cig))
	require.NoError(t, err)

	log := logger.NewTestLogger(t)

	// Time out individual tests after 10 seconds.
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)

	config = &testTLFJournalConfig{
		t, FakeTlfID(1, false), bsplitter, codec, crypto,
		cig, ekg, mdserver,
	}

	bserver := NewBlockServerMemory(config)

	delegate = testBWDelegate{
		t:          t,
		testCtx:    ctx,
		stateCh:    make(chan bwState),
		shutdownCh: make(chan struct{}),
	}
	tlfJournal, err = makeTLFJournal(
		ctx, tempdir, config.tlfID, config, bserver, log,
		TLFJournalBackgroundWorkEnabled, delegate)
	require.NoError(t, err)

	// Read the state changes triggered by the initial work
	// signal.
	delegate.requireNextState(ctx, bwIdle)
	delegate.requireNextState(ctx, bwBusy)
	delegate.requireNextState(ctx, bwIdle)
	return tempdir, config, ctx, cancel, tlfJournal, delegate
}

func teardownTLFJournalTest(
	tempdir string, config *testTLFJournalConfig, ctx context.Context,
	cancel context.CancelFunc, tlfJournal *tlfJournal,
	delegate testBWDelegate) {
	// Shutdown first so we don't get the Done() signal (from the
	// cancel() call) spuriously.
	tlfJournal.shutdown()
	select {
	case <-delegate.shutdownCh:
	case <-ctx.Done():
		require.FailNow(config.t, ctx.Err().Error())
	}

	cancel()

	select {
	case bws := <-delegate.stateCh:
		assert.Fail(config.t, "Unexpected state %s", bws)
	default:
	}
	config.mdserver.Shutdown()
	tlfJournal.delegateBlockServer.Shutdown()
	err := os.RemoveAll(tempdir)
	require.NoError(config.t, err)
}

func putBlock(ctx context.Context,
	t *testing.T, config *testTLFJournalConfig,
	tlfJournal *tlfJournal, data []byte) {
	crypto := config.Crypto()
	uid := keybase1.MakeTestUID(1)
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)
	bCtx := BlockContext{uid, "", zeroBlockRefNonce}
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = tlfJournal.putBlockData(ctx, bID, bCtx, data, serverHalf)
	require.NoError(t, err)
}

func TestTLFJournalBasic(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	// Wait for it to be processed.

	delegate.requireNextState(ctx, bwBusy)
	delegate.requireNextState(ctx, bwIdle)
}

func TestTLFJournalPauseResume(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, bwPaused)

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	// Unpause and wait for it to be processed.

	tlfJournal.resumeBackgroundWork()
	delegate.requireNextState(ctx, bwIdle)
	delegate.requireNextState(ctx, bwBusy)
	delegate.requireNextState(ctx, bwIdle)
}

func TestTLFJournalPauseShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, bwPaused)

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	// Should still be able to shut down while paused.
}

type hangingBlockServer struct {
	BlockServer
	// Closed on put.
	onPutCh chan struct{}
}

func (bs hangingBlockServer) Put(
	ctx context.Context, tlfID TlfID, id BlockID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	close(bs.onPutCh)
	// Hang until the context is cancelled.
	<-ctx.Done()
	return ctx.Err()
}

func (bs hangingBlockServer) waitForPut(ctx context.Context, t *testing.T) {
	select {
	case <-bs.onPutCh:
	case <-ctx.Done():
		require.FailNow(t, ctx.Err().Error())
	}
}

func TestTLFJournalBlockOpBusyPause(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to pause while busy.

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, bwPaused)
}

func TestTLFJournalBlockOpBusyShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to shut down while busy.
}

func TestTLFJournalBlockOpWhileBusy(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to put a second block while busy.
	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4, 5})
}

type hangingMDServer struct {
	MDServer
	// Closed on put.
	onPutCh chan struct{}
}

func (md hangingMDServer) Put(
	ctx context.Context, rmds *RootMetadataSigned) error {
	close(md.onPutCh)
	// Hang until the context is cancelled.
	<-ctx.Done()
	return ctx.Err()
}

func (md hangingMDServer) waitForPut(ctx context.Context, t *testing.T) {
	select {
	case <-md.onPutCh:
	case <-ctx.Done():
		require.FailNow(t, ctx.Err().Error())
	}
}

func putMD(ctx context.Context, t *testing.T, config *testTLFJournalConfig,
	tlfJournal *tlfJournal, revision MetadataRevision, prevRoot MdID) {
	_, uid, err := config.cig.GetCurrentUserInfo(ctx)
	require.NoError(t, err)
	bh, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	rmd := NewRootMetadata()
	err = rmd.Update(config.tlfID, bh)
	require.NoError(t, err)
	rmd.SetRevision(revision)
	rmd.FakeInitialRekey(bh)

	_, err = tlfJournal.putMD(ctx, rmd)
	require.NoError(t, err)
}

func TestTLFJournalMDServerBusyPause(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	md := hangingMDServer{config.MDServer(), make(chan struct{})}
	config.mdserver = md

	putMD(ctx, t, config, tlfJournal, MetadataRevisionInitial, MdID{})

	md.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to pause while busy.

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, bwPaused)
}

func TestTLFJournalMDServerBusyShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	md := hangingMDServer{config.MDServer(), make(chan struct{})}
	config.mdserver = md

	putMD(ctx, t, config, tlfJournal, MetadataRevisionInitial, MdID{})

	md.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to shutdown while busy.
}

func TestTLFJournalBlockOpWhileBusyMDOp(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	md := hangingMDServer{config.MDServer(), make(chan struct{})}
	config.mdserver = md

	putMD(ctx, t, config, tlfJournal, MetadataRevisionInitial, MdID{})

	md.waitForPut(ctx, t)
	delegate.requireNextState(ctx, bwBusy)

	// Should still be able to put a block while busy.
	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

}

func TestTLFJournalFlushMDBasic(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		tempdir, config, ctx, cancel, tlfJournal, delegate)

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, bwPaused)

	firstRevision := MetadataRevision(10)
	firstPrevRoot := fakeMdID(1)
	mdCount := 10

	prevRoot := firstPrevRoot
	for i := 0; i < mdCount; i++ {
		revision := firstRevision + MetadataRevision(i)
		md := config.makeMDForTest(revision, prevRoot)
		mdID, err := tlfJournal.putMD(ctx, md)
		require.NoError(t, err)
		prevRoot = mdID
	}

	// Flush all entries.
	var mdserver shimMDServer
	config.mdserver = &mdserver

	for i := 0; i < mdCount; i++ {
		flushed, err := tlfJournal.flushOneMDOp(ctx)
		require.NoError(t, err)
		require.True(t, flushed)
	}
	flushed, err := tlfJournal.flushOneMDOp(ctx)
	require.NoError(t, err)
	require.False(t, flushed)
	// TODO: Fix.
	require.Equal(t, 0, getMDJournalLength(t, tlfJournal.mdJournal))

	rmdses := mdserver.rmdses
	require.Equal(t, mdCount, len(rmdses))

	// Check RMDSes on the server.

	uid := config.cig.uid
	verifyingKey := config.crypto.signingKey.GetVerifyingKey()

	require.Equal(t, firstRevision, rmdses[0].MD.RevisionNumber())
	require.Equal(t, firstPrevRoot, rmdses[0].MD.GetPrevRoot())
	err = rmdses[0].IsValidAndSigned(config.Codec(), config.Crypto())
	require.NoError(t, err)
	err = rmdses[0].IsLastModifiedBy(uid, verifyingKey)
	require.NoError(t, err)

	for i := 1; i < len(rmdses); i++ {
		err := rmdses[i].IsValidAndSigned(
			config.Codec(), config.Crypto())
		require.NoError(t, err)
		err = rmdses[i].IsLastModifiedBy(uid, verifyingKey)
		require.NoError(t, err)
		prevID, err := config.Crypto().MakeMdID(rmdses[i-1].MD)
		require.NoError(t, err)
		err = rmdses[i-1].MD.CheckValidSuccessor(prevID, rmdses[i].MD)
		require.NoError(t, err)
	}
}
