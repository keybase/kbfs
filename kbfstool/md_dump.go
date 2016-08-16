package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func mdGet(ctx context.Context, config libkbfs.Config, input string) (
	libkbfs.ImmutableRootMetadata, error) {
	tlfID, err := libkbfs.ParseTlfID(input)
	if err != nil {
		return libkbfs.ImmutableRootMetadata{}, err
	}

	mdOps := config.MDOps()

	rmd, err := mdOps.GetForTLF(ctx, tlfID)
	if err != nil {
		return libkbfs.ImmutableRootMetadata{}, err
	}

	if rmd == (libkbfs.ImmutableRootMetadata{}) {
		return libkbfs.ImmutableRootMetadata{},
			fmt.Errorf("Metadata object for %s not found", input)
	}

	return rmd, nil
}

func dumpMd(rmd libkbfs.ImmutableRootMetadata, config libkbfs.Config) error {
	mdID, err := config.Crypto().MakeMdID(&rmd.BareRootMetadata)
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
	// TODO: Clean up output.
	fmt.Printf("Dir: %s\n", data.Dir)
	fmt.Print("TLF private key: {32 bytes}\n")
	// TODO: Print changes.

	return nil
}

func mdDump(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md dump", flag.ContinueOnError)
	flags.Parse(args)

	inputs := flags.Args()
	if len(inputs) == 0 {
		printError("md dump", errors.New("at least one string must be specified"))
		return 1
	}

	for _, input := range inputs {
		rmd, err := mdGet(ctx, config, input)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		err = dumpMd(rmd, config)
		if err != nil {
			printError("md dump", err)
			return 1
		}
	}

	return 0
}
