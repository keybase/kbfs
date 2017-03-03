// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testJournalEntry struct {
	I int
}

// TestDiskJournalOrdinals makes sure the in-memory ordinals stay in
// sync with the on-disk ones.
func TestDiskJournalOrdinals(t *testing.T) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "disk_journal")
	require.NoError(t, err)
	defer func() {
		err := ioutil.RemoveAll(tempdir)
		assert.NoError(t, err)
	}()

	codec := kbfscodec.NewMsgpack()
	j, err := makeDiskJournal(
		codec, tempdir, reflect.TypeOf(testJournalEntry{}))
	require.NoError(t, err)

	readEarliest := func() (journalOrdinal, error) {
		earliest, err := j.readEarliestOrdinal()
		earliestReal, errReal := j.readEarliestOrdinalFromDisk()
		require.Equal(t, earliestReal, earliest)
		if ioutil.IsNotExist(err) && ioutil.IsNotExist(errReal) {
			return earliest, err
		}
		require.NoError(t, err)
		require.NoError(t, errReal)
		return earliest, err
	}

	readLatest := func() (journalOrdinal, error) {
		latest, err := j.readLatestOrdinal()
		latestReal, errReal := j.readLatestOrdinalFromDisk()
		require.Equal(t, latestReal, latest)
		if ioutil.IsNotExist(err) && ioutil.IsNotExist(errReal) {
			return latest, err
		}
		require.NoError(t, err)
		require.NoError(t, errReal)
		return latest, err
	}

	expectEmpty := func() {
		_, err = readEarliest()
		require.True(t, ioutil.IsNotExist(err))
		_, err = readLatest()
		require.True(t, ioutil.IsNotExist(err))
	}

	expectRange := func(
		expectedEarliest, expectedLatest journalOrdinal) {
		earliest, err := readEarliest()
		require.NoError(t, err)
		require.Equal(t, expectedEarliest, earliest)

		latest, err := readLatest()
		require.NoError(t, err)
		require.Equal(t, expectedLatest, latest)
	}

	expectEmpty()

	o, err := j.appendJournalEntry(nil, testJournalEntry{1})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(1), o)

	expectRange(1, 1)

	o, err = j.appendJournalEntry(nil, testJournalEntry{1})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(2), o)

	expectRange(1, 2)

	empty, err := j.removeEarliest()
	require.NoError(t, err)
	require.False(t, empty)

	expectRange(2, 2)

	err = j.clear()
	require.NoError(t, err)

	expectEmpty()
}

func TestDiskJournalClear(t *testing.T) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "disk_journal")
	require.NoError(t, err)
	defer func() {
		err := ioutil.RemoveAll(tempdir)
		assert.NoError(t, err)
	}()

	codec := kbfscodec.NewMsgpack()
	j, err := makeDiskJournal(
		codec, tempdir, reflect.TypeOf(testJournalEntry{}))
	require.NoError(t, err)

	o, err := j.appendJournalEntry(nil, testJournalEntry{1})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(1), o)

	o, err = j.appendJournalEntry(nil, testJournalEntry{2})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(2), o)

	err = j.clear()
	require.NoError(t, err)

	_, err = ioutil.Stat(tempdir)
	require.True(t, ioutil.IsNotExist(err))
}

func TestDiskJournalMoveEmpty(t *testing.T) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "disk_journal")
	require.NoError(t, err)
	defer func() {
		err := ioutil.RemoveAll(tempdir)
		assert.NoError(t, err)
	}()

	oldDir := filepath.Join(tempdir, "journaldir")
	newDir := oldDir + ".new"

	codec := kbfscodec.NewMsgpack()
	j, err := makeDiskJournal(
		codec, oldDir, reflect.TypeOf(testJournalEntry{}))
	require.NoError(t, err)
	require.Equal(t, oldDir, j.dir)

	moveOldDir, err := j.move(newDir)
	require.NoError(t, err)
	require.Equal(t, oldDir, moveOldDir)
	require.Equal(t, newDir, j.dir)
}

func TestDiskJournalMove(t *testing.T) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "disk_journal")
	require.NoError(t, err)
	defer func() {
		err := ioutil.RemoveAll(tempdir)
		assert.NoError(t, err)
	}()

	oldDir := filepath.Join(tempdir, "journaldir")
	newDir := oldDir + ".new"

	codec := kbfscodec.NewMsgpack()
	j, err := makeDiskJournal(
		codec, oldDir, reflect.TypeOf(testJournalEntry{}))
	require.NoError(t, err)
	require.Equal(t, oldDir, j.dir)

	o, err := j.appendJournalEntry(nil, testJournalEntry{1})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(1), o)

	moveOldDir, err := j.move(newDir)
	require.NoError(t, err)
	require.Equal(t, oldDir, moveOldDir)
	require.Equal(t, newDir, j.dir)

	entry, err := j.readJournalEntry(o)
	require.NoError(t, err)
	require.Equal(t, testJournalEntry{1}, entry)
}
