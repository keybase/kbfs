// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"os"
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

func TestDiskJournalClear(t *testing.T) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "disk_journal")
	require.NoError(t, err)
	defer func() {
		err := ioutil.RemoveAll(tempdir)
		assert.NoError(t, err)
	}()

	codec := kbfscodec.NewMsgpack()
	j := makeDiskJournal(codec, tempdir, reflect.TypeOf(testJournalEntry{}))

	o, err := j.appendJournalEntry(nil, testJournalEntry{1})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(0), o)

	o, err = j.appendJournalEntry(nil, testJournalEntry{2})
	require.NoError(t, err)
	require.Equal(t, journalOrdinal(1), o)

	err = j.clear()
	require.NoError(t, err)

	_, err = ioutil.Stat(tempdir)
	require.True(t, ioutil.IsNotExist(err))
}
