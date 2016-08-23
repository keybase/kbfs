// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type testBWDelegate struct {
	stateCh    chan bwState
	shutdownCh chan struct{}
}

func (d testBWDelegate) OnNewState(bws bwState) {
	d.stateCh <- bws
}

func (d testBWDelegate) OnShutdown() {
	d.shutdownCh <- struct{}{}
}

func setupTLFJournalTest(t *testing.T) (
	tempdir string, config Config, tlfJournal *tlfJournal,
	delegate testBWDelegate) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "tlf_journal")
	require.NoError(t, err)
	config = MakeTestConfigOrBust(t, "test_user")
	log := config.MakeLogger("")
	ctx := context.Background()
	tlfID := FakeTlfID(1, false)
	delegate = testBWDelegate{
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

	return tempdir, config, tlfJournal, delegate
}

func teardownTLFJournalTest(
	t *testing.T, tlfJournal *tlfJournal, delegate testBWDelegate,
	tempdir string, config Config) {
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

func putBlock(t *testing.T, config Config, tlfJournal *tlfJournal) {
	ctx := context.Background()
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
	tempdir, config, tlfJournal, delegate := setupTLFJournalTest(t)

	// Put a block.

	putBlock(t, config, tlfJournal)

	// Wait for it to be processed.

	bws := <-delegate.stateCh
	require.Equal(t, bwBusy, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)

	defer teardownTLFJournalTest(t, tlfJournal, delegate, tempdir, config)
}

func TestTLFJournalPauseResume(t *testing.T) {
	tempdir, config, tlfJournal, delegate := setupTLFJournalTest(t)

	tlfJournal.pauseBackgroundWork()
	bws := <-delegate.stateCh
	require.Equal(t, bwPaused, bws)

	// Put a block.

	putBlock(t, config, tlfJournal)

	// Unpause and wait for it to be processed.

	tlfJournal.resumeBackgroundWork()
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwBusy, bws)
	bws = <-delegate.stateCh
	require.Equal(t, bwIdle, bws)

	defer teardownTLFJournalTest(t, tlfJournal, delegate, tempdir, config)
}
