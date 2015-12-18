package main

import (
	"errors"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func mdMain(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	if len(args) < 1 {
		// TODO: Print usage.
		printError("md", errors.New("a subcommand needs to be specified"))
		return 1
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "get":
		return mdGet(ctx, config, args)
	default:
		printError("md", fmt.Errorf("unknown command '%s'", cmd))
		return 1
	}
}
