// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func setupJournalServerTest(t *testing.T) (
	tempdir string, config *ConfigLocal, jServer *JournalServer) {
	config = MakeTestConfigOrBust(t, "test_user1", "test_user2")

	tempdir, err := ioutil.TempDir(os.TempDir(), "journal_server")
	require.NoError(t, err)
	// Clean up the tempdir if anything in the setup fails/panics.
	defer func() {
		if r := recover(); r != nil {
			CheckConfigAndShutdown(t, config)
			err := os.RemoveAll(tempdir)
			if err != nil {
				t.Errorf(err.Error())
			}
		}
	}()

	config.EnableJournaling(tempdir)
	jServer, err = GetJournalServer(config)
	require.NoError(t, err)
	return tempdir, config, jServer
}

func teardownJournalServerTest(
	t *testing.T, tempdir string, config Config) {
	CheckConfigAndShutdown(t, config)
	err := os.RemoveAll(tempdir)
	require.NoError(t, err)
}

func TestJournalServerRestart(t *testing.T) {
	tempdir, config, jServer := setupJournalServerTest(t)
	defer teardownJournalServerTest(t, tempdir, config)

	// Use a shutdown-only BlockServer so that it errors if the
	// journal tries to access it.
	jServer.delegateBlockServer = shutdownOnlyBlockServer{}

	ctx := context.Background()

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(ctx, tlfID, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)

	blockServer := config.BlockServer()
	mdOps := config.MDOps()
	crypto := config.Crypto()

	h, err := ParseTlfHandle(ctx, config.KBPKI(), "test_user1", false)
	require.NoError(t, err)
	uid := h.ResolvedWriters()[0]

	bh, err := h.ToBareHandle()
	require.NoError(t, err)

	// Put a block.

	bCtx := BlockContext{uid, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, tlfID, bID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Put an MD.

	rmd := NewRootMetadata()
	err = rmd.Update(tlfID, bh)
	require.NoError(t, err)
	rmd.tlfHandle = h
	rmd.SetRevision(MetadataRevision(1))
	rekeyDone, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, rekeyDone)

	_, err = mdOps.Put(ctx, rmd)
	require.NoError(t, err)

	// Simulate a restart.

	jServer = makeJournalServer(
		config, jServer.log, tempdir, jServer.delegateBlockCache,
		jServer.delegateDirtyBlockCache,
		jServer.delegateBlockServer, jServer.delegateMDOps, nil, nil)
	uid, verifyingKey, err :=
		getCurrentUIDAndVerifyingKey(ctx, config.KBPKI())
	require.NoError(t, err)
	err = jServer.EnableExistingJournals(
		ctx, uid, verifyingKey, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)
	config.SetBlockCache(jServer.blockCache())
	config.SetBlockServer(jServer.blockServer())
	config.SetMDOps(jServer.mdOps())

	// Get the block.

	buf, key, err := blockServer.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Get the MD.

	head, err := mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, rmd.Revision(), head.Revision())
}

func TestJournalServerLogOutLogIn(t *testing.T) {
	tempdir, config, jServer := setupJournalServerTest(t)
	defer teardownJournalServerTest(t, tempdir, config)

	// Use a shutdown-only BlockServer so that it errors if the
	// journal tries to access it.
	jServer.delegateBlockServer = shutdownOnlyBlockServer{}

	ctx := context.Background()

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(ctx, tlfID, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)

	blockServer := config.BlockServer()
	mdOps := config.MDOps()
	crypto := config.Crypto()

	h, err := ParseTlfHandle(ctx, config.KBPKI(), "test_user1", false)
	require.NoError(t, err)
	uid := h.ResolvedWriters()[0]

	bh, err := h.ToBareHandle()
	require.NoError(t, err)

	// Put a block.

	bCtx := BlockContext{uid, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	require.NoError(t, err)
	serverHalf, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, tlfID, bID, bCtx, data, serverHalf)
	require.NoError(t, err)

	// Put an MD.

	rmd := NewRootMetadata()
	err = rmd.Update(tlfID, bh)
	require.NoError(t, err)
	rmd.tlfHandle = h
	rmd.SetRevision(MetadataRevision(1))
	rekeyDone, _, err := config.KeyManager().Rekey(ctx, rmd, false)
	require.NoError(t, err)
	require.True(t, rekeyDone)

	_, err = mdOps.Put(ctx, rmd)
	require.NoError(t, err)

	// Simulate a log out.

	serviceLoggedOut(ctx, config)

	// Get the block, which should fail.

	_, _, err = blockServer.Get(ctx, tlfID, bID, bCtx)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	// Get the head, which should be empty.

	head, err := mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, ImmutableRootMetadata{}, head)

	serviceLoggedIn(
		ctx, config, "test_user1", TLFJournalBackgroundWorkPaused)

	// Get the block.

	buf, key, err := blockServer.Get(ctx, tlfID, bID, bCtx)
	require.NoError(t, err)
	require.Equal(t, data, buf)
	require.Equal(t, serverHalf, key)

	// Get the MD.

	head, err = mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, rmd.Revision(), head.Revision())
}

func TestJournalServerLogOutDirtyOp(t *testing.T) {
	tempdir, config, jServer := setupJournalServerTest(t)
	defer teardownJournalServerTest(t, tempdir, config)

	testCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Time out this test after 10 seconds.
	ctx, _ := context.WithTimeout(testCtx, 10*time.Second)

	// Spawn separate goroutine to check if timeout is hit, as
	// shutdown code ignores context cancellation.
	go func() {
		select {
		case <-ctx.Done():
			panic("Timeout reached")
		case <-testCtx.Done():
		}
	}()

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(ctx, tlfID, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)

	jServer.dirtyOpStart(tlfID)
	go func() {
		jServer.dirtyOpEnd(tlfID)
	}()
	serviceLoggedOut(ctx, config)

	jServer.dirtyOpsLock.RLock()
	jServer.dirtyOpsLock.RUnlock()
	require.Equal(t, uint(0), jServer.dirtyOps)
}

func TestJournalServerMultiUser(t *testing.T) {
	tempdir, config, jServer := setupJournalServerTest(t)
	defer teardownJournalServerTest(t, tempdir, config)

	// Use a shutdown-only BlockServer so that it errors if the
	// journal tries to access it.
	jServer.delegateBlockServer = shutdownOnlyBlockServer{}

	ctx := context.Background()

	tlfID := FakeTlfID(2, false)
	err := jServer.Enable(ctx, tlfID, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)

	blockServer := config.BlockServer()
	mdOps := config.MDOps()
	crypto := config.Crypto()

	h, err := ParseTlfHandle(
		ctx, config.KBPKI(), "test_user1,test_user2", false)
	require.NoError(t, err)
	uid1 := h.ResolvedWriters()[0]
	uid2 := h.ResolvedWriters()[1]

	bh, err := h.ToBareHandle()
	require.NoError(t, err)

	// Put a block under user 1.

	bCtx1 := BlockContext{uid1, "", zeroBlockRefNonce}
	data1 := []byte{1, 2, 3, 4}
	bID1, err := crypto.MakePermanentBlockID(data1)
	require.NoError(t, err)
	serverHalf1, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, tlfID, bID1, bCtx1, data1, serverHalf1)
	require.NoError(t, err)

	// Put an MD under user 1.

	rmd1 := NewRootMetadata()
	err = rmd1.Update(tlfID, bh)
	require.NoError(t, err)
	rmd1.tlfHandle = h
	rmd1.SetRevision(MetadataRevision(1))
	rmd1.SetLastModifyingWriter(uid1)
	rekeyDone, _, err := config.KeyManager().Rekey(ctx, rmd1, false)
	require.NoError(t, err)
	require.True(t, rekeyDone)

	_, err = mdOps.Put(ctx, rmd1)
	require.NoError(t, err)

	// Log in user 2.

	serviceLoggedOut(ctx, config)

	service := config.KeybaseService().(*KeybaseDaemonLocal)
	service.currentUID = uid2
	SwitchDeviceForLocalUserOrBust(t, config, 0)

	serviceLoggedIn(
		ctx, config, "test_user2", TLFJournalBackgroundWorkPaused)

	err = jServer.Enable(ctx, tlfID, TLFJournalBackgroundWorkPaused)
	require.NoError(t, err)

	// Put a block under user 2.

	bCtx2 := BlockContext{uid2, "", zeroBlockRefNonce}
	data2 := []byte{1, 2, 3, 4, 5}
	bID2, err := crypto.MakePermanentBlockID(data2)
	require.NoError(t, err)
	serverHalf2, err := crypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)
	err = blockServer.Put(ctx, tlfID, bID2, bCtx2, data2, serverHalf2)
	require.NoError(t, err)

	// Put an MD under user 2.

	rmd2 := NewRootMetadata()
	err = rmd2.Update(tlfID, bh)
	require.NoError(t, err)
	rmd2.tlfHandle = h
	rmd2.SetRevision(MetadataRevision(1))
	rmd2.SetLastModifyingWriter(uid2)
	rekeyDone, _, err = config.KeyManager().Rekey(ctx, rmd2, false)
	require.NoError(t, err)
	require.True(t, rekeyDone)

	_, err = mdOps.Put(ctx, rmd2)
	require.NoError(t, err)

	// Log out.

	serviceLoggedOut(ctx, config)

	// No block or MD should be visible.

	_, _, err = blockServer.Get(ctx, tlfID, bID1, bCtx1)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	_, _, err = blockServer.Get(ctx, tlfID, bID2, bCtx2)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	head, err := mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, ImmutableRootMetadata{}, head)

	// Log in user 1.

	service.currentUID = uid1
	SwitchDeviceForLocalUserOrBust(t, config, 0)

	serviceLoggedIn(
		ctx, config, "test_user1", TLFJournalBackgroundWorkPaused)

	// Only user 1's block and MD should be visible.

	buf, key, err := blockServer.Get(ctx, tlfID, bID1, bCtx1)
	require.NoError(t, err)
	require.Equal(t, data1, buf)
	require.Equal(t, serverHalf1, key)

	_, _, err = blockServer.Get(ctx, tlfID, bID2, bCtx2)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	head, err = mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, uid1, head.LastModifyingWriter())

	// Log in user 2.

	serviceLoggedOut(ctx, config)

	service.currentUID = uid2
	SwitchDeviceForLocalUserOrBust(t, config, 0)

	serviceLoggedIn(
		ctx, config, "test_user2", TLFJournalBackgroundWorkPaused)

	// Only user 2's block and MD should be visible.

	_, _, err = blockServer.Get(ctx, tlfID, bID1, bCtx1)
	require.IsType(t, BServerErrorBlockNonExistent{}, err)

	buf, key, err = blockServer.Get(ctx, tlfID, bID2, bCtx2)
	require.NoError(t, err)
	require.Equal(t, data2, buf)
	require.Equal(t, serverHalf2, key)

	head, err = mdOps.GetForTLF(ctx, tlfID)
	require.NoError(t, err)
	require.Equal(t, uid2, head.LastModifyingWriter())
}
