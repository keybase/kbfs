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

	mdID, err := rmd.MetadataID(config)
	if err != nil {
		return err
	}

	fmt.Print("Public info\n")
	fmt.Print("-----------\n")
	fmt.Printf("MD ID: %s\n", mdID)
	fmt.Printf("Prev: %s\n", rmd.PrevRoot)
	fmt.Printf("TLF ID: %s\n", rmd.ID)
	fmt.Printf("Branch ID: %s\n", rmd.BID)
	fmt.Printf("Revision: %s\n", rmd.Revision)
	// TODO: Print flags.
	fmt.Printf("Disk usage: %d\n", rmd.DiskUsage)
	fmt.Printf("Bytes in new blocks: %d\n", rmd.RefBytes)
	fmt.Printf("Bytes in unreferenced blocks: %d\n", rmd.UnrefBytes)
	// TODO: Print Writers/Keys.

	fmt.Print("\n")
	fmt.Print("Private info\n")
	fmt.Print("------------\n")

	data := rmd.Data()
	// TODO: Print Dir.
	fmt.Printf("Last writer: %s\n", data.LastWriter)
	fmt.Print("TLF private key: {32 bytes}\n")
	// TODO: Print changes.

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
