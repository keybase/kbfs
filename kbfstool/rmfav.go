// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pkg/errors"

	"github.com/keybase/kbfs/fsrpc"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func rmfavOne(ctx context.Context, config libkbfs.Config, dirPathStr string, verbose bool) error {
	p, err := fsrpc.NewPath(dirPathStr)
	if err != nil {
		return err
	}

	if p.PathType != fsrpc.TLFPathType {
	}

	if p.PathType != fsrpc.TLFPathType || len(p.TLFComponents) > 0 {
		return errors.Errorf("%q is not a favorite", dirPathStr)
	}

	err = config.KBFSOps().DeleteFavorite(ctx, libkbfs.Favorite{
		Name: p.TLFName,
		Type: p.TLFType,
	})
	if err != nil {
		return err
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "rmfav: removed favorite %q\n", dirPathStr)
	}

	return nil
}

func rmfav(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs rmfav", flag.ContinueOnError)
	verbose := flags.Bool("v", false, "Print extra status output.")
	err := flags.Parse(args)
	if err != nil {
		printError("rmfav", err)
		return 1
	}

	nodePaths := flags.Args()
	if len(nodePaths) == 0 {
		printError("rmfav", errAtLeastOnePath)
		return 1
	}

	for _, nodePath := range nodePaths {
		err := rmfavOne(ctx, config, nodePath, *verbose)
		if err != nil {
			printError("rmfav", err)
			exitStatus = 1
		}
	}
	return
}
