// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type testBWDelegate struct {
	shutdownCh chan struct{}
}

func (d testBWDelegate) OnNewState(
	bws TLFJournalBackgroundWorkStatus, doingWork bool) {
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
		shutdownCh: make(chan struct{}),
	}
	tlfJournal, err = makeTLFJournal(
		ctx, tempdir, tlfID, config, config.BlockServer(), log,
		TLFJournalBackgroundWorkPaused, delegate)
	require.NoError(t, err)
	return tempdir, config, tlfJournal, delegate
}

func teardownTLFJournalTest(
	t *testing.T, tlfJournal *tlfJournal, delegate testBWDelegate,
	tempdir string, config Config) {
	tlfJournal.shutdown()
	<-delegate.shutdownCh
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
	CheckConfigAndShutdown(t, config)
}

func TestTLFJournalBasic(t *testing.T) {
	tempdir, config, tlfJournal, delegate := setupTLFJournalTest(t)
	defer teardownTLFJournalTest(t, tlfJournal, delegate, tempdir, config)
}
