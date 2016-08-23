// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	keybase1 "github.com/keybase/client/go/protocol"
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
	bws := <-delegate.stateCh
	require.Equal(t, bwIdle, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwBusy, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)

	return tempdir, config, ctx, cancel, tlfJournal, delegate
}

func teardownTLFJournalTest(
	t *testing.T, cancel context.CancelFunc,
	tlfJournal *tlfJournal, delegate testBWDelegate,
	tempdir string, config Config) {
	cancel()
	tlfJournal.shutdown()
	<-delegate.shutdownCh
	select {
	case bws := <-delegate.stateCh:
		assert.Fail(t, "Unexpected state %s", bws)
	default:
	}
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
	CheckConfigAndShutdown(t, config)
}

func putBlock(ctx context.Context,
	t *testing.T, config Config, tlfJournal *tlfJournal) {
	crypto := config.Crypto()

	uid := keybase1.MakeTestUID(1)
	data := []byte{1, 2, 3, 4}
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
		t, cancel, tlfJournal, delegate, tempdir, config)

	putBlock(ctx, t, config, tlfJournal)

	// Wait for it to be processed.

	bws := <-delegate.stateCh
	require.Equal(t, bwBusy, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)
}

func TestTLFJournalPauseResume(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, cancel, tlfJournal, delegate, tempdir, config)

	tlfJournal.pauseBackgroundWork()
	bws := <-delegate.stateCh
	require.Equal(t, bwPaused, bws)

	putBlock(ctx, t, config, tlfJournal)

	// Unpause and wait for it to be processed.

	tlfJournal.resumeBackgroundWork()
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwBusy, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)
}

func TestTLFJournalPauseShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, cancel, tlfJournal, delegate, tempdir, config)

	tlfJournal.pauseBackgroundWork()
	bws := <-delegate.stateCh
	require.Equal(t, bwPaused, bws)

	putBlock(ctx, t, config, tlfJournal)

	// Should still be able to shut down while paused.
}

type hangingBlockServer struct {
	BlockServer
}

func (hangingBlockServer) Put(
	ctx context.Context, tlfID TlfID, id BlockID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	// Hang until the context is cancelled.
	<-ctx.Done()
	return ctx.Err()
}

func TestTLFJournalBusyPause(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, cancel, tlfJournal, delegate, tempdir, config)

	putBlock(ctx, t, config, tlfJournal)

	bws := <-delegate.stateCh
	require.Equal(t, bwBusy, bws)

	// Should still be able to pause while busy.

	tlfJournal.pauseBackgroundWork()
	bws = <-delegate.stateCh
	require.Equal(t, bwPaused, bws)
}

func TestTLFJournalBusyShutdown(t *testing.T) {
	tempdir, config, ctx, cancel, tlfJournal, delegate :=
		setupTLFJournalTest(t)
	defer teardownTLFJournalTest(
		t, cancel, tlfJournal, delegate, tempdir, config)

	putBlock(ctx, t, config, tlfJournal)

	bws := <-delegate.stateCh
	require.Equal(t, bwBusy, bws)

	// Should still be able to shut down while busy.
}
