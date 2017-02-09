// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package simplefs

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListLocal(t *testing.T) {
	ctx := context.Background()
	sfs := &SimpleFS{}

	// make a temp local directory + files we will clean up later
	tempdir, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(tempdir, "test1.txt"), []byte("foo"), 0644)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(tempdir, "test2.txt"), []byte("foo"), 0644)
	require.NoError(t, err)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	err = sfs.SimpleFSList(ctx, keybase1.SimpleFSListArg{
		OpID: opid,
		Path: keybase1.NewPathWithLocal(tempdir),
	})
	require.NoError(t, err)

	listResult, err := sfs.SimpleFSReadList(ctx, opid)
	require.NoError(t, err)

	assert.Len(t, listResult.Entries, 2, "Expected 2 directory entries in listing")

	// Assume we've exhausted the list now, so expect error
	_, err = sfs.SimpleFSReadList(ctx, opid)
	require.Error(t, err)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)
}

func TestCopyLocal(t *testing.T) {
	ctx := context.Background()
	sfs := &SimpleFS{}

	// make a temp local src directory + files we will clean up later
	tempdir, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(tempdir, "test1.txt"), []byte("foo"), 0644)
	require.NoError(t, err)

	// make a temp local dest directory + files we will clean up later
	tempdir2, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir2)
	require.NoError(t, err)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	err = sfs.SimpleFSCopy(ctx, keybase1.SimpleFSCopyArg{
		OpID: opid,
		Src:  keybase1.NewPathWithLocal(filepath.Join(tempdir, "test1.txt")),
		Dest: keybase1.NewPathWithLocal(tempdir2), // TODO: must the dest include a name?
	})
	require.NoError(t, err)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)

	exists, err := libkb.FileExists(filepath.Join(tempdir2, "test1.txt"))
	require.NoError(t, err)
	assert.True(t, exists, "File copy destination must exist")
}
