// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/keybase/kbfs/fsrpc"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func printHeader(p fsrpc.Path) {
	fmt.Printf("%s:\n", p)
}

func computeModeStr(entryType libkbfs.IFCERFTEntryType) string {
	var typeStr string
	switch entryType {
	case libkbfs.IFCERFTFile:
		typeStr = "-"
	case libkbfs.IFCERFTExec:
		typeStr = "-"
	case libkbfs.IFCERFTDir:
		typeStr = "d"
	case libkbfs.IFCERFTSym:
		typeStr = "l"
	default:
		typeStr = "?"
	}

	// TODO: Figure out whether the current user is just a reader,
	// and omit w below if so.
	var modeStr string
	switch entryType {
	case libkbfs.IFCERFTFile:
		modeStr = "rw-"
	case libkbfs.IFCERFTExec:
		modeStr = "rwx"
	case libkbfs.IFCERFTDir:
		modeStr = "rwx"
	case libkbfs.IFCERFTSym:
		modeStr = "rwx"
	default:
		modeStr = "rw-"
	}

	// TODO: Figure out whether this is a public directory.
	return fmt.Sprintf("%s%s%s%s", typeStr, modeStr, modeStr, "---")
}

func printEntry(ctx context.Context, config libkbfs.IFCERFTConfig, dir fsrpc.Path, name string, entryType libkbfs.IFCERFTEntryType, longFormat, useSigil bool) {
	var sigil string
	if useSigil {
		switch entryType {
		case libkbfs.IFCERFTFile:
		case libkbfs.IFCERFTExec:
			sigil = "*"
		case libkbfs.IFCERFTDir:
			sigil = "/"
		case libkbfs.IFCERFTSym:
			sigil = "@"
		default:
			sigil = "?"
		}
	}
	if longFormat {
		p, err := dir.Join(name)
		if err != nil {
			printError("ls", err)
		}
		_, de, err := p.GetNode(ctx, config)
		if err != nil {
			printError("ls", err)
		}

		modeStr := computeModeStr(entryType)
		mtimeStr := time.Unix(0, de.Mtime).Format("Jan 02 15:04")
		var symPathStr string
		if entryType == libkbfs.IFCERFTSym {
			symPathStr = fmt.Sprintf(" -> %s", de.SymPath)
		}
		fmt.Printf("%s\t%d\t%s\t%s%s%s\n", modeStr, de.Size, mtimeStr, name, sigil, symPathStr)
	} else {
		fmt.Printf("%s%s\n", name, sigil)
	}
}

func lsHelper(ctx context.Context, config libkbfs.IFCERFTConfig, p fsrpc.Path, hasMultiple bool, handleEntry func(string, libkbfs.IFCERFTEntryType)) error {
	kbfsOps := config.KBFSOps()

	switch p.PathType {
	case fsrpc.RootPathType:
		if hasMultiple {
			printHeader(p)
		}
		handleEntry(topName, libkbfs.IFCERFTDir)
		return nil

	case fsrpc.KeybasePathType:
		if hasMultiple {
			printHeader(p)
		}
		handleEntry(publicName, libkbfs.IFCERFTDir)
		handleEntry(privateName, libkbfs.IFCERFTDir)
		return nil

	case fsrpc.KeybaseChildPathType:
		favs, err := kbfsOps.GetFavorites(ctx)
		if err != nil {
			return err
		}

		if hasMultiple {
			printHeader(p)
		}
		for _, fav := range favs {
			if p.Public == fav.Public {
				handleEntry(fav.Name, libkbfs.IFCERFTDir)
			}
		}
		return nil

	case fsrpc.TLFPathType:
		n, de, err := p.GetNode(ctx, config)
		if err != nil {
			return err
		}

		if de.Type == libkbfs.IFCERFTDir {
			// GetDirChildren doesn't verify the dir-ness
			// of the node correctly (since it ends up
			// creating a new DirBlock if the node isn't
			// in the cache already).
			//
			// TODO: Fix the above.
			children, err := kbfsOps.GetDirChildren(ctx, n)
			if err != nil {
				return err
			}

			if hasMultiple {
				printHeader(p)
			}
			for name, entryInfo := range children {
				handleEntry(name, entryInfo.Type)
			}
		} else {
			_, name, err := p.DirAndBasename()
			if err != nil {
				return err
			}
			handleEntry(name, de.Type)
		}
		return nil

	default:
		break
	}

	return fmt.Errorf("invalid KBFS path %s", p)
}

func lsOne(ctx context.Context, config libkbfs.IFCERFTConfig, p fsrpc.Path, longFormat, useSigil, recursive, hasMultiple bool, errorFn func(error)) {
	var children []string
	handleEntry := func(name string, entryType libkbfs.IFCERFTEntryType) {
		if recursive && entryType == libkbfs.IFCERFTDir {
			children = append(children, name)
		}
		printEntry(ctx, config, p, name, entryType, longFormat, useSigil)
	}
	err := lsHelper(ctx, config, p, hasMultiple || recursive, handleEntry)
	if err != nil {
		errorFn(err)
		// Fall-through.
	}

	if recursive {
		for _, name := range children {
			childPath, err := p.Join(name)
			if err != nil {
				errorFn(err)
				continue
			}

			fmt.Print("\n")
			lsOne(ctx, config, childPath, longFormat, useSigil, true, true, errorFn)
		}
	}
}

func ls(ctx context.Context, config libkbfs.IFCERFTConfig, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs ls", flag.ContinueOnError)
	longFormat := flags.Bool("l", false, "List in long format.")
	useSigil := flags.Bool("F", false, "Display sigils after each pathname.")
	recursive := flags.Bool("R", false, "Recursively list subdirectories encountered.")
	flags.Parse(args)

	nodePathStrs := flags.Args()
	if len(nodePathStrs) == 0 {
		printError("ls", errAtLeastOnePath)
		exitStatus = 1
		return
	}

	hasMultiple := len(nodePathStrs) > 1
	for i, nodePathStr := range nodePathStrs {
		p, err := fsrpc.NewPath(nodePathStr)
		if err != nil {
			printError("ls", err)
			exitStatus = 1
			continue
		}

		if i > 0 {
			fmt.Print("\n")
		}

		lsOne(ctx, config, p, *longFormat, *useSigil, *recursive, hasMultiple, func(err error) {
			printError("ls", err)
			exitStatus = 1
		})
	}
	return
}
