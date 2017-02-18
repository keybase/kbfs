// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package simplefs

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/libkbfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSimpleFS(config libkbfs.Config) *SimpleFS {
	return &SimpleFS{
		config:  config,
		handles: map[keybase1.OpID]*handle{},
	}
}

func closeSimpleFS(ctx context.Context, t *testing.T, fs *SimpleFS) {
	err := fs.config.Shutdown(ctx)
	require.NoError(t, err)
	libkbfs.CleanupCancellationDelayer(ctx)
}

func newTempRemotePath() (keybase1.Path, error) {
	var bs = make([]byte, 8)
	err := kbfscrypto.RandRead(bs)
	if err != nil {
		return keybase1.Path{}, err
	}

	raw := fmt.Sprintf(`/private/jdoe/%X`, bs)
	return keybase1.NewPathWithKbfs(raw), nil
}

func deleteTempLocalPath(path keybase1.Path) {
	os.RemoveAll(path.Local())
}

// "pending" tells whether we expect the operation to still be
// there, because there is no "none" in AsyncOps
func checkPendingOp(ctx context.Context,
	t *testing.T,
	sfs *SimpleFS,
	opid keybase1.OpID,
	expectedOp keybase1.AsyncOps,
	src keybase1.Path,
	dest keybase1.Path,
	pending bool) {

	// TODO: what do we expect the progress to be?
	_, err := sfs.SimpleFSCheck(ctx, opid)
	if pending {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
	}

	ops, err := sfs.SimpleFSGetOps(ctx)

	if !pending {
		assert.Len(t, ops, 0, "Expected zero pending operations")
		return
	}

	assert.True(t, len(ops) > 0, "Expected at least one pending operation")

	o := ops[0]
	op, err := o.AsyncOp()
	require.NoError(t, err)
	assert.Equal(t, expectedOp, op, "Expected at least one pending operation")

	// TODO: verify read/write arguments
	switch op {
	case keybase1.AsyncOps_LIST:
		list := o.List()
		assert.Equal(t, list.Path, src, "Expected matching path in operation")
	case keybase1.AsyncOps_LIST_RECURSIVE:
		list := o.ListRecursive()
		assert.Equal(t, list.Path, src, "Expected matching path in operation")
	case keybase1.AsyncOps_READ:
		read := o.Read()
		assert.Equal(t, read.Path, src, "Expected matching path in operation")
	case keybase1.AsyncOps_WRITE:
		write := o.Write()
		assert.Equal(t, write.Path, src, "Expected matching path in operation")
	case keybase1.AsyncOps_COPY:
		copy := o.Copy()
		assert.Equal(t, copy.Src, src, "Expected matching path in operation")
		assert.Equal(t, copy.Dest, dest, "Expected matching path in operation")
	case keybase1.AsyncOps_MOVE:
		move := o.Move()
		assert.Equal(t, move.Src, src, "Expected matching path in operation")
		assert.Equal(t, move.Dest, dest, "Expected matching path in operation")
	case keybase1.AsyncOps_REMOVE:
		remove := o.Remove()
		assert.Equal(t, remove.Path, src, "Expected matching path in operation")
	}
}

func TestList(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	// make a temp remote directory + files we will clean up later
	path1 := keybase1.NewPathWithKbfs(`/private/jdoe`)
	writeRemoteFile(ctx, t, sfs, `/private/jdoe/test1.txt`, []byte(`foo`))
	writeRemoteFile(ctx, t, sfs, `/private/jdoe/test2.txt`, []byte(`foo`))
	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	err = sfs.SimpleFSList(ctx, keybase1.SimpleFSListArg{
		OpID: opid,
		Path: path1,
	})
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_LIST, path1, keybase1.Path{}, true)

	listResult, err := sfs.SimpleFSReadList(ctx, opid)
	require.NoError(t, err)

	assert.Len(t, listResult.Entries, 2, "Expected 2 directory entries in listing")

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_LIST, path1, keybase1.Path{}, true)

	// Assume we've exhausted the list now, so expect error
	_, err = sfs.SimpleFSReadList(ctx, opid)
	require.Error(t, err)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_LIST, path1, keybase1.Path{}, false)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)
}

func TestLocalList(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	// not supposed to work on local paths
	path1 := keybase1.NewPathWithLocal(os.TempDir())
	err = sfs.SimpleFSList(ctx, keybase1.SimpleFSListArg{
		OpID: opid,
		Path: path1,
	})
	require.Error(t, err)
}

func TestCopyToLocal(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	// make a temp remote directory + file(s) we will clean up later
	path1 := keybase1.NewPathWithKbfs(`/private/jdoe`)
	writeRemoteFile(ctx, t, sfs, filepath.Join(path1.Kbfs(), "test1.txt"), []byte("foo"))

	// make a temp local dest directory + files we will clean up later
	tempdir2, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir2)
	require.NoError(t, err)
	path2 := keybase1.NewPathWithLocal(tempdir2)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	srcPath := keybase1.NewPathWithKbfs(filepath.Join(path1.Kbfs(), "test1.txt"))
	destPath := keybase1.NewPathWithLocal(filepath.Join(path2.Local(), "test1.txt"))

	err = sfs.SimpleFSCopy(ctx, keybase1.SimpleFSCopyArg{
		OpID: opid,
		Src:  srcPath,
		Dest: destPath,
	})
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_COPY, srcPath, destPath, true)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_COPY, srcPath, destPath, false)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)

	exists, err := libkb.FileExists(filepath.Join(tempdir2, "test1.txt"))
	require.NoError(t, err)
	assert.True(t, exists, "File copy destination must exist")
}

func TestCopyToRemote(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	// make a temp remote directory + file(s) we will clean up later
	path2 := keybase1.NewPathWithKbfs(`/private/jdoe`)

	// make a temp local dest directory + files we will clean up later
	tempdir, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir)
	require.NoError(t, err)
	path1 := keybase1.NewPathWithLocal(tempdir)
	defer deleteTempLocalPath(path1)
	err = ioutil.WriteFile(filepath.Join(path1.Local(), "test1.txt"), []byte("foo"), 0644)
	require.NoError(t, err)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	srcPath := keybase1.NewPathWithKbfs(filepath.Join(path1.Local(), "test1.txt"))
	destPath := keybase1.NewPathWithLocal(filepath.Join(path2.Kbfs(), "test1.txt"))

	err = sfs.SimpleFSCopy(ctx, keybase1.SimpleFSCopyArg{
		OpID: opid,
		Src:  srcPath,
		Dest: destPath,
	})

	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_COPY, srcPath, destPath, true)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_COPY, srcPath, destPath, false)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)

	require.Equal(t, `foo`,
		string(readRemoteFile(ctx, t, sfs, filepath.Join(path2.Kbfs(), "test1.txt"))))
}

func TestMoveToLocal(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	// make a temp remote directory + file(s) we will clean up later
	path1 := keybase1.NewPathWithKbfs(`/private/jdoe`)
	writeRemoteFile(ctx, t, sfs, filepath.Join(path1.Kbfs(), "test1.txt"), []byte("foo"))

	// make a temp local dest directory + files we will clean up later
	tempdir2, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir2)
	require.NoError(t, err)
	path2 := keybase1.NewPathWithLocal(tempdir2)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	srcPath := keybase1.NewPathWithKbfs(filepath.Join(path1.Kbfs(), "test1.txt"))
	destPath := keybase1.NewPathWithLocal(filepath.Join(path2.Local(), "test1.txt"))

	err = sfs.SimpleFSMove(ctx, keybase1.SimpleFSMoveArg{
		OpID: opid,
		Src:  srcPath,
		Dest: destPath,
	})

	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_MOVE, srcPath, destPath, true)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_MOVE, srcPath, destPath, false)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)

	exists, err := libkb.FileExists(filepath.Join(tempdir2, "test1.txt"))
	require.NoError(t, err)
	assert.True(t, exists, "File move destination must exist")

	exists, err = libkb.FileExists(filepath.Join(path1.Kbfs(), "test1.txt"))
	assert.False(t, exists, "File move source must no longer exist")
}

func TestMoveToRemote(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	// make a temp remote directory + file(s) we will clean up later
	path2 := keybase1.NewPathWithKbfs(`/private/jdoe`)

	// make a temp local dest directory + files we will clean up later
	tempdir, err := ioutil.TempDir("", "simpleFstest")
	defer os.RemoveAll(tempdir)
	require.NoError(t, err)
	path1 := keybase1.NewPathWithLocal(tempdir)
	defer deleteTempLocalPath(path1)
	err = ioutil.WriteFile(filepath.Join(path1.Local(), "test1.txt"), []byte("foo"), 0644)
	require.NoError(t, err)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	srcPath := keybase1.NewPathWithKbfs(filepath.Join(path1.Local(), "test1.txt"))
	destPath := keybase1.NewPathWithLocal(filepath.Join(path2.Kbfs(), "test1.txt"))

	err = sfs.SimpleFSMove(ctx, keybase1.SimpleFSMoveArg{
		OpID: opid,
		Src:  srcPath,
		Dest: destPath,
	})

	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_MOVE, srcPath, destPath, true)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_MOVE, srcPath, destPath, false)

	// Verify error on double close
	err = sfs.SimpleFSClose(ctx, opid)
	require.Error(t, err)

	require.Equal(t, `foo`,
		string(readRemoteFile(ctx, t, sfs, filepath.Join(path2.Kbfs(), "test1.txt"))))

	exists, err := libkb.FileExists(filepath.Join(path1.Local(), "test1.txt"))
	assert.False(t, exists, "File copy source must no longer exist")
}

func TestRm(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)
	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	// make a temp remote directory + files we will clean up later
	path1 := keybase1.NewPathWithKbfs(`/private/jdoe/test1.txt`)
	writeRemoteFile(ctx, t, sfs, `/private/jdoe/test1.txt`, []byte(`foo`))

	err = sfs.SimpleFSRemove(ctx, keybase1.SimpleFSRemoveArg{
		OpID: opid,
		Path: path1,
	})

	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_REMOVE, path1, keybase1.Path{}, true)

	err = sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	_, err = sfs.SimpleFSStat(ctx, path1)
	require.Error(t, err) // Right? for a file that's supposed to not be there?

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_REMOVE, path1, keybase1.Path{}, false)
}

func TestMkdir(t *testing.T) {
	ctx := libkbfs.BackgroundContextWithCancellationDelayer()
	sfs := newSimpleFS(libkbfs.MakeTestConfigOrBust(t, "jdoe"))
	defer closeSimpleFS(ctx, t, sfs)

	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	path1 := keybase1.NewPathWithKbfs(`/private/jdoe/testdir`)

	err = sfs.SimpleFSOpen(ctx, keybase1.SimpleFSOpenArg{
		OpID:  opid,
		Dest:  path1,
		Flags: keybase1.OpenFlags_DIRECTORY,
	})

	err = sfs.SimpleFSClose(ctx, opid) // right? we close the op after making the dir?
	require.NoError(t, err)

	de, err := sfs.SimpleFSStat(ctx, path1)
	require.NoError(t, err)

	require.Equal(t, de.DirentType, keybase1.DirentType_DIR, "Created directory element should have type DirentType_DIR")

	// make sure removing a file works
	err = sfs.SimpleFSRemove(ctx, keybase1.SimpleFSRemoveArg{
		OpID: opid,
		Path: path1,
	})
	require.NoError(t, err)

	// test error on double remove
	err = sfs.SimpleFSRemove(ctx, keybase1.SimpleFSRemoveArg{
		OpID: opid,
		Path: path1,
	})
	require.Error(t, err)
}

func writeRemoteFile(ctx context.Context, t *testing.T, sfs *SimpleFS, path string, data []byte) {
	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	err = sfs.SimpleFSOpen(ctx, keybase1.SimpleFSOpenArg{
		OpID:  opid,
		Dest:  keybase1.NewPathWithKbfs(path),
		Flags: keybase1.OpenFlags_REPLACE | keybase1.OpenFlags_WRITE,
	})
	defer sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	err = sfs.SimpleFSWrite(ctx, keybase1.SimpleFSWriteArg{
		OpID:    opid,
		Offset:  0,
		Content: data,
	})

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_WRITE, keybase1.NewPathWithKbfs(path), keybase1.Path{}, true)

	require.NoError(t, err)
}

func readRemoteFile(ctx context.Context, t *testing.T, sfs *SimpleFS, path string) []byte {
	opid, err := sfs.SimpleFSMakeOpid(ctx)
	require.NoError(t, err)

	de, err := sfs.SimpleFSStat(ctx, keybase1.NewPathWithKbfs(path))
	require.NoError(t, err)
	t.Logf("Stat remote %q %d bytes", path, de.Size)

	err = sfs.SimpleFSOpen(ctx, keybase1.SimpleFSOpenArg{
		OpID:  opid,
		Dest:  keybase1.NewPathWithKbfs(path),
		Flags: keybase1.OpenFlags_READ | keybase1.OpenFlags_EXISTING,
	})
	defer sfs.SimpleFSClose(ctx, opid)
	require.NoError(t, err)

	data, err := sfs.SimpleFSRead(ctx, keybase1.SimpleFSReadArg{
		OpID:   opid,
		Offset: 0,
		Size:   de.Size,
	})
	require.NoError(t, err)

	checkPendingOp(ctx, t, sfs, opid, keybase1.AsyncOps_READ, keybase1.NewPathWithKbfs(path), keybase1.Path{}, true)

	return data.Data
}
