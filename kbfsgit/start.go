// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsgit

import (
	"context"
	"io"

	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libgit"
	"github.com/keybase/kbfs/libkbfs"
)

// StartOptions are options for starting up.
type StartOptions struct {
	KbfsParams libkbfs.InitParams
	// Remote is the name that the caller's repo (on local disk) has
	// assigned to the KBFS-based repo.
	Remote string
	// Repo is the URL the caller's repo (on local disk) is trying to
	// access, in the form "keybase://private/user/reponame".
	Repo string
	// GitDir is the filepath leading to the .git directory of the
	// caller's local on-disk repo.
	GitDir string
}

// Start starts the kbfsgit logic, and begins listening for git
// commands from `input` and responding to them via `output`.
func Start(ctx context.Context, options StartOptions,
	kbCtx libkbfs.Context, defaultLogPath string,
	input io.Reader, output io.Writer, errput io.Writer) *libfs.Error {
	// Ideally we wouldn't print this if the verbosity is 0, but we
	// don't know that until we start parsing options.  TODO: get rid
	// of this once we integrate with the kbfs daemon.
	errput.Write([]byte("Initializing Keybase... "))
	ctx, config, err := libgit.Init(
		ctx, options.KbfsParams, kbCtx, nil, defaultLogPath)
	if err != nil {
		return libfs.InitError(err.Error())
	}
	defer config.Shutdown(ctx)

	config.MakeLogger("").CDebugf(
		ctx, "Running Git remote helper: remote=%s, repo=%s, storageRoot=%s",
		options.Remote, options.Repo, options.KbfsParams.StorageRoot)
	errput.Write([]byte("done.\n"))

	r, err := newRunner(
		ctx, config, options.Remote, options.Repo, options.GitDir,
		input, output, errput)
	if err != nil {
		return libfs.InitError(err.Error())
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- r.processCommands(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return libfs.InitError(err.Error())
		}
		return nil
	case <-ctx.Done():
		return libfs.InitError(ctx.Err().Error())
	}
}
