package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func mdGetTlf(ctx context.Context, config libkbfs.Config, tlfIDStr string) error {
	tlfID := libkbfs.ParseTlfID(tlfIDStr)
	if tlfID == libkbfs.NullTlfID {
		return fmt.Errorf("Could not parse `%s' into a TLF ID", tlfIDStr)
	}

	mdOps := config.MDOps()

	rmd, err := mdOps.GetForTLF(ctx, tlfID)
	if err != nil {
		return err
	}

	fmt.Printf("%+v\n", rmd)
	return nil
}

func mdGet(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md get", flag.ContinueOnError)
	flags.Parse(args)

	tlfIDStrs := flags.Args()
	if len(tlfIDStrs) == 0 {
		printError("md get", errors.New("at least one TLF ID must be specified"))
		return 1
	}

	for _, tlfIDStr := range tlfIDStrs {
		err := mdGetTlf(ctx, config, tlfIDStr)
		if err != nil {
			printError("md get", err)
			return 1
		}
	}

	return 0
}
