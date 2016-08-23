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

func setupTLFJournalTest(t *testing.T) (
	tempdir string, config Config, tlfJournal *tlfJournal) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "tlf_journal")
	require.NoError(t, err)
	config = MakeTestConfigOrBust(t, "test_user")
	log := config.MakeLogger("")
	ctx := context.Background()
	tlfID := FakeTlfID(1, false)
	tlfJournal, err = makeTLFJournal(
		ctx, tempdir, tlfID, config, config.BlockServer(), log,
		TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)
	return tempdir, config, tlfJournal
}

func teardownTLFJournalTest(
	t *testing.T, tlfJournal *tlfJournal, tempdir string, config Config) {
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
	tlfJournal.shutdown()
	CheckConfigAndShutdown(t, config)
}

func TestTLFJournalBasic(t *testing.T) {
	tempdir, config, tlfJournal := setupTLFJournalTest(t)
	defer teardownTLFJournalTest(t, tlfJournal, tempdir, config)
}
