// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type testBWDelegate struct {
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
	d.stateCh <- bws
}

func (d testBWDelegate) OnShutdown(ctx context.Context) {
	d.shutdownCh <- struct{}{}
}

func (d testBWDelegate) requireNextState(
	ctx context.Context, t *testing.T, expectedState bwState) {
	select {
	case bws := <-d.stateCh:
		require.Equal(t, expectedState, bws)
	case <-ctx.Done():
		require.FailNow(t, ctx.Err().Error())
	}
}

func setupTLFJournalTest(t *testing.T) (
	tempdir string, config Config, ctx context.Context,
	cancel context.CancelFunc, tlfJournal *tlfJournal,
	delegate testBWDelegate) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "tlf_journal")
	require.NoError(t, err)
	config = MakeTestConfigOrBust(t, "test_user")
	log := config.MakeLogger("")

	// Time out individual tests after 10 seconds.
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)

	tlfID := FakeTlfID(1, false)
	delegate = testBWDelegate{
		testCtx:    ctx,
		stateCh:    make(chan bwState),
		shutdownCh: make(chan struct{}),
	}
	tlfJournal, err = makeTLFJournal(
		ctx, tempdir, tlfID, config, config.BlockServer(), log,
		TLFJournalBackgroundWorkEnabled, delegate)
	require.NoError(t, err)

	// Read the state changes triggered by the initial work
	// signal.
	delegate.requireNextState(ctx, t, bwIdle)
	delegate.requireNextState(ctx, t, bwBusy)
	delegate.requireNextState(ctx, t, bwIdle)
	return tempdir, config, ctx, cancel, tlfJournal, delegate
}

func teardownTLFJournalTest(
	t *testing.T, ctx context.Context, cancel context.CancelFunc,
	tlfJournal *tlfJournal, delegate testBWDelegate,
	tempdir string, config Config) {
	// Shutdown first so we don't get the Done() signal (from the
	// cancel() call) spuriously.
	tlfJournal.shutdown()
	select {
	case <-delegate.shutdownCh:
	case <-ctx.Done():
		require.FailNow(t, ctx.Err().Error())
	}

	cancel()

	select {
	case bws := <-delegate.stateCh:
		assert.Fail(t, "Unexpected state %s", bws)
	default:
	}
	CheckConfigAndShutdown(t, config)
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
}

func putBlock(ctx context.Context,
	t *testing.T, config Config, tlfJournal *tlfJournal, data []byte) {
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
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	// Wait for it to be processed.

	delegate.requireNextState(ctx, t, bwBusy)
	delegate.requireNextState(ctx, t, bwIdle)
}

func TestTLFJournalPauseResume(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, t, bwPaused)

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	// Unpause and wait for it to be processed.

	tlfJournal.resumeBackgroundWork()
	delegate.requireNextState(ctx, t, bwIdle)
	delegate.requireNextState(ctx, t, bwBusy)
	delegate.requireNextState(ctx, t, bwIdle)
}

func TestTLFJournalPauseShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, t, bwPaused)

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
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, t, bwBusy)

	// Should still be able to pause while busy.

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, t, bwPaused)
}

func TestTLFJournalBlockOpBusyShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, t, bwBusy)

	// Should still be able to shut down while busy.
}

func TestTLFJournalBlockOpWhileBusy(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	bs := hangingBlockServer{tlfJournal.delegateBlockServer,
		make(chan struct{})}
	tlfJournal.delegateBlockServer = bs

	putBlock(ctx, t, config, tlfJournal, []byte{1, 2, 3, 4})

	bs.waitForPut(ctx, t)
	delegate.requireNextState(ctx, t, bwBusy)

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

func putMD(ctx context.Context, t *testing.T, config Config,
	tlfJournal *tlfJournal, revision MetadataRevision, prevRoot MdID) {
	_, uid, err := config.KBPKI().GetCurrentUserInfo(ctx)
	require.NoError(t, err)
	bh, err := MakeBareTlfHandle([]keybase1.UID{uid}, nil, nil, nil, nil)
	require.NoError(t, err)

	h, err := MakeTlfHandle(ctx, bh, config.KBPKI())
	require.NoError(t, err)

	rmd := NewRootMetadata()
	err = rmd.Update(tlfJournal.tlfID, bh)
	require.NoError(t, err)
	rmd.tlfHandle = h
	rmd.SetRevision(revision)
	rekeyDone, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, rekeyDone)

	_, err = tlfJournal.putMD(
		ctx, config.Crypto(), config.KeyManager(),
		config.BlockSplitter(), rmd)
	require.NoError(t, err)
}

func TestTLFJournalMDServerBusyPause(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	md := hangingMDServer{config.MDServer(), make(chan struct{})}
	config.SetMDServer(md)

	putMD(ctx, t, config, tlfJournal, MetadataRevisionInitial, MdID{})

	md.waitForPut(ctx, t)
	delegate.requireNextState(ctx, t, bwBusy)

	// Should still be able to pause while busy.

	tlfJournal.pauseBackgroundWork()
	delegate.requireNextState(ctx, t, bwPaused)
}

func TestTLFJournalMDServerBusyShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, ctx, cancel, tlfJournal, delegate, tempdir, config)

	md := hangingMDServer{config.MDServer(), make(chan struct{})}
	config.SetMDServer(md)

	putMD(ctx, t, config, tlfJournal, MetadataRevisionInitial, MdID{})

	md.waitForPut(ctx, t)
	delegate.requireNextState(ctx, t, bwBusy)

	// Should still be able to shutdown while busy.
}
