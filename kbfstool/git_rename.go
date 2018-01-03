package main

import (
	"flag"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

const gitRenameUsageStr = `Usage:
  kbfstool git rename oldName newName
`

func gitRename(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs git rename", flag.ContinueOnError)
	err := flags.Parse(args)
	if err != nil {
		printError("git rename", err)
		return 1
	}

	inputs := flags.Args()
	if len(inputs) != 2 {
		fmt.Print(gitRenameUsageStr)
		return 1
	}

	oldName, newName := inputs[0], inputs[1]

	fmt.Printf("%s -> %s\n", oldName, newName)
	return 0
}
