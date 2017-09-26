// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"os"
	"path"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libgit"
	"github.com/keybase/kbfs/libkbfs"
	"github.com/keybase/kbfs/simplefs"
	"golang.org/x/net/context"
)

// StartOptions are options for starting up
type StartOptions struct {
	KbfsParams     libkbfs.InitParams
	PlatformParams PlatformParams
	RuntimeDir     string
	Label          string
	ForceMount     bool
	SkipMount      bool
	MountPoint     string
}

func startMounting(
	ctx context.Context, config libkbfs.Config, options StartOptions,
	log logger.Logger, mi *libfs.MountInterrupter) error {
	log.CDebugf(ctx, "Mounting: %q", options.MountPoint)

	var mounter = &mounter{options: options}
	err := mi.MountAndSetUnmount(mounter)
	if err != nil {
		return err
	}

	log.CDebugf(ctx, "Creating filesystem")
	fs := NewFS(config, mounter.c, options.KbfsParams.Debug, options.PlatformParams)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = context.WithValue(ctx, libfs.CtxAppIDKey, fs)

	go func() {
		select {
		case <-mounter.c.Ready:
			// We wait for the mounter to finish asynchronously with
			// calling fs.Serve() below, for the rare osxfuse case
			// where `mount(2)` makes a blocking STATFS call before
			// completing.  If we aren't listening for the STATFS call
			// when this happens, there will be a deadlock, and the
			// mount will silently fail after two minutes.  See
			// KBFS-2409.
			err = mounter.c.MountError
			if err != nil {
				log.CWarningf(ctx, "Mount error: %+v", err)
				cancel()
				return
			}
			log.CDebugf(ctx, "Mount ready")
		case <-ctx.Done():
		}
	}()

	log.CDebugf(ctx, "Serving filesystem")
	if err = fs.Serve(ctx); err != nil {
		return err
	}

	log.CDebugf(ctx, "Ending")
	return nil
}

// Start the filesystem
func Start(options StartOptions, kbCtx libkbfs.Context) *libfs.Error {
	// Hook simplefs implementation in.
	options.KbfsParams.CreateSimpleFSInstance = simplefs.NewSimpleFS
	// Hook git implementation in.
	options.KbfsParams.CreateGitHandlerInstance =
		func(config libkbfs.Config) keybase1.KBFSGitInterface {
			return libgit.NewRPCHandlerWithCtx(kbCtx, config)
		}

	log, err := libkbfs.InitLog(options.KbfsParams, kbCtx)
	if err != nil {
		return libfs.InitError(err.Error())
	}

	if options.RuntimeDir != "" {
		info := libkb.NewServiceInfo(libkbfs.Version, libkbfs.PrereleaseBuild, options.Label, os.Getpid())
		err := info.WriteFile(path.Join(options.RuntimeDir, "kbfs.info"), log)
		if err != nil {
			return libfs.InitError(err.Error())
		}
	}

	log.Debug("Initializing")
	mi := libfs.NewMountInterrupter(log)
	ctx := context.Background()
	config, err := libkbfs.Init(
		ctx, kbCtx, options.KbfsParams, nil, mi.Done, log)
	if err != nil {
		return libfs.InitError(err.Error())
	}
	defer libkbfs.Shutdown()

	if options.SkipMount {
		log.Debug("Skipping mounting filesystem")
	} else {
		err = startMounting(ctx, config, options, log, mi)
		if err != nil {
			// Abort on error if we were force mounting, otherwise continue.
			if options.ForceMount {
				// If we exit we might want to clean a mount behind us.
				mi.Done()
				return libfs.MountError(err.Error())
			}
		}
	}
	mi.Wait()
	return nil
}
