// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"os"
	"path"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/kbfs/libfs"
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
}

// Start the filesystem
func Start(mounter Mounter, options StartOptions, kbCtx libkbfs.Context) *libfs.Error {
	// Hook simplefs implementation in.
	options.KbfsParams.CreateSimpleFSInstance = simplefs.NewSimpleFS

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
	interruptFn := func() {}
	config, err := libkbfs.Init(
		kbCtx, options.KbfsParams, nil, func() { interruptFn() }, log)
	if err != nil {
		return libfs.InitError(err.Error())
	}
	defer libkbfs.Shutdown()

	log.Debug("Mounting: %s", mounter.Dir())
	c, err := mounter.Mount()
	if err != nil {
		return libfs.MountError(err.Error())
	}
	defer mounter.Unmount()

	done := make(chan struct{})
	if c != nil { // c can be nil for NoopMounter
		interruptFn = func() {
			if unmountErr := mounter.Unmount(); unmountErr != nil {
				log.Debug("Unmounting error: %v", unmountErr)
			}
		}
	} else {
		interruptFn = func() {
			close(done)
		}
	}

	if c != nil {
		<-c.Ready
		err = c.MountError
		if err != nil {
			return libfs.MountError(err.Error())
		}

		log.Debug("Creating filesystem")
		fs := NewFS(config, c, options.KbfsParams.Debug, options.PlatformParams)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = context.WithValue(ctx, libfs.CtxAppIDKey, fs)
		log.Debug("Serving filesystem")
		if err = fs.Serve(ctx); err != nil {
			return libfs.MountError(err.Error())
		}
	} else {
		<-done
	}

	log.Debug("Ending")
	return nil
}
