// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// Keybase file system

package main

import (
	"flag"
	"fmt"
	"os"

	"bazil.org/fuse"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/env"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libfuse"
	"github.com/keybase/kbfs/libkbfs"
)

var runtimeDir = flag.String("runtime-dir", os.Getenv("KEYBASE_RUNTIME_DIR"), "runtime directory")
var label = flag.String("label", os.Getenv("KEYBASE_LABEL"), "label to help identify if running as a service")
var mountType = flag.String("mount-type", defaultMountType, "mount type: default, force, none")
var version = flag.Bool("version", false, "Print version")

const usageFormatStr = `Usage:
  kbfsfuse -version

To run against remote KBFS servers:
  kbfsfuse [-debug] [-cpuprofile=path/to/dir]
    [-bserver=host:port] [-mdserver=host:port]
    [-runtime-dir=path/to/dir] [-label=label] [-mount-type=force]
    [-log-to-file] [-log-file=path/to/file] [-clean-bcache-cap=0]
    %s/path/to/mountpoint

To run in a local testing environment:
  kbfsfuse [-debug] [-cpuprofile=path/to/dir]
    [-bserver=[memory|dir:/path/to/dir]] [-mdserver=[memory|dir:/path/to/dir]]
    [-localuser=<user>] [-local-fav-storage=[memory|dir:/path/to/dir]]
    [-runtime-dir=path/to/dir] [-label=label] [-mount-type=force]
    [-log-to-file] [-log-file=path/to/file] [-clean-bcache-cap=0]
    %s/path/to/mountpoint

defaults:
  -bserver=%s -mdserver=%s -localuser=%s -local-fav-storage=%s
`

func getUsageStr(ctx libkbfs.Context) string {
	platformUsageString := libfuse.GetPlatformUsageString()
	defaultBServer := libkbfs.GetDefaultBServer(ctx)
	defaultMDServer := libkbfs.GetDefaultMDServer(ctx)
	defaultLocalUser := libkbfs.GetDefaultLocalUser(ctx)
	defaultLocalFavoriteStorage :=
		libkbfs.GetDefaultLocalFavoriteStorage(ctx)
	return fmt.Sprintf(usageFormatStr, platformUsageString,
		platformUsageString, defaultBServer, defaultMDServer,
		defaultLocalUser, defaultLocalFavoriteStorage)
}

func start() *libfs.Error {
	ctx := env.NewContext()

	kbfsParams := libkbfs.AddFlags(flag.CommandLine, ctx)
	platformParams := libfuse.AddPlatformFlags(flag.CommandLine)

	flag.Parse()

	if *version {
		fmt.Printf("%s\n", libkbfs.VersionString())
		return nil
	}

	if len(flag.Args()) < 1 {
		fmt.Print(getUsageStr(ctx))
		return libfs.InitError("no mount specified")
	}

	if len(flag.Args()) > 1 {
		fmt.Print(getUsageStr(ctx))
		return libfs.InitError("extra arguments specified (flags go before the first argument)")
	}

	if kbfsParams.Debug {
		fuseLog := logger.NewWithCallDepth("FUSE", 1)
		fuseLog.Configure("", true, "")
		fuse.Debug = libfuse.MakeFuseDebugFn(
			fuseLog, false /* superVerbose */)
	}

	mountpoint := flag.Arg(0)
	var mounter libfuse.Mounter
	if *mountType == "force" {
		mounter = libfuse.NewForceMounter(mountpoint, *platformParams)
	} else if *mountType == "none" {
		mounter = libfuse.NewNoopMounter()
	} else {
		mounter = libfuse.NewDefaultMounter(mountpoint, *platformParams)
	}

	options := libfuse.StartOptions{
		KbfsParams:     *kbfsParams,
		PlatformParams: *platformParams,
		RuntimeDir:     *runtimeDir,
		Label:          *label,
	}

	return libfuse.Start(mounter, options, ctx)
}

func main() {
	err := start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kbfsfuse error: (%d) %s\n", err.Code, err.Message)

		os.Exit(err.Code)
	}
	os.Exit(0)
}
