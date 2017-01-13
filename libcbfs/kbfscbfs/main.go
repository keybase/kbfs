// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// Keybase file system

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/kbfs/env"
	"github.com/keybase/kbfs/libcbfs"
	"github.com/keybase/kbfs/libcbfs/cbfs"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libkbfs"
)

var runtimeDir = flag.String("runtime-dir", os.Getenv("KEYBASE_RUNTIME_DIR"), "runtime directory")
var label = flag.String("label", os.Getenv("KEYBASE_LABEL"), "label to help identify if running as a service")
var mountType = flag.String("mount-type", defaultMountType, "mount type: default, force, none")
var version = flag.Bool("version", false, "Print version")
var servicemount = flag.Bool("mount-from-service", false, "get mount path from service")

const usageFormatStr = `Usage:
  kbfscbfs -version

To run against remote KBFS servers:
  kbfscbfs
    [-runtime-dir=path/to/dir] [-label=label] [-mount-type=force]
    [-mount-flags=n] [-dokan-dll=path/to/dokan.dll]
%s
    -mount-from-service | /path/to/mountpoint

To run in a local testing environment:
  kbfscbfs
    [-runtime-dir=path/to/dir] [-label=label] [-mount-type=force]
    [-mount-flags=n] [-dokan-dll=path/to/dokan.dll]
%s
    -mount-from-service | /path/to/mountpoint

Defaults:
%s
`

func getUsageString(ctx libkbfs.Context) string {
	remoteUsageStr := libkbfs.GetRemoteUsageString()
	localUsageStr := libkbfs.GetLocalUsageString()
	defaultUsageStr := libkbfs.GetDefaultsUsageString(ctx)
	return fmt.Sprintf(usageFormatStr, remoteUsageStr,
		localUsageStr, defaultUsageStr)
}

func start() *libfs.Error {
	err := libkb.SaferDLLLoading()
	if err != nil {
		fmt.Printf("SaferDLLLoading failed: %v\n", err)
	}

	ctx := env.NewContext()
	kbfsParams := libkbfs.AddFlags(flag.CommandLine, ctx)

	flag.Parse()

	if *version {
		fmt.Printf("%s\n", libkbfs.VersionString())
		return nil
	}

	var mountpoint string
	if len(flag.Args()) < 1 {
		if !*servicemount {
			fmt.Print(getUsageString(ctx))
			return libfs.InitError("no mount specified")
		}
	} else {
		mountpoint = flag.Arg(0)
	}

	if len(flag.Args()) > 1 {
		fmt.Print(getUsageString(ctx))
		return libfs.InitError("extra arguments specified (flags go before the first argument)")
	}

	var mounter libcbfs.Mounter
	if *mountType == "force" {
		mounter = libcbfs.NewForceMounter(mountpoint)
	} else if *mountType == "none" {
		mounter = libcbfs.NewNoopMounter()
	} else {
		mounter = libcbfs.NewDefaultMounter(mountpoint)
	}

	options := libcbfs.StartOptions{
		KbfsParams: *kbfsParams,
		RuntimeDir: *runtimeDir,
		Label:      *label,
		CbfsConfig: cbfs.Config{},
	}

	return libcbfs.Start(mounter, options, ctx)
}

func main() {
	err := start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kbfscbfs error: (%d) %s\n", err.Code, err.Message)

		os.Exit(err.Code)
	}
	os.Exit(0)
}
