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

	fmt.Printf("MD ID: %s\n\n", mdID)

	fmt.Print("Reader/writer metadata\n")
	fmt.Print("----------------------\n")
	fmt.Printf("Last modifying user: %s\n", rmd.LastModifyingUser)
	// TODO: Print flags.
	fmt.Printf("Revision: %s\n", rmd.Revision)
	fmt.Printf("Prev MD ID: %s\n", rmd.PrevRoot)
	// TODO: Print RKeys, unresolved readers, conflict info,
	// finalized info, and unknown fields.
	fmt.Print("\n")

	fmt.Print("Writer metadata\n")
	fmt.Print("---------------\n")
	fmt.Printf("Last modifying writer: %s\n", rmd.LastModifyingWriter)
	// TODO: Print Writers/WKeys and unresolved writers.
	fmt.Printf("TLF ID: %s\n", rmd.ID)
	fmt.Printf("Branch ID: %s\n", rmd.BID)
	// TODO: Print writer flags.
	fmt.Printf("Disk usage: %d\n", rmd.DiskUsage)
	fmt.Printf("Bytes in new blocks: %d\n", rmd.RefBytes)
	fmt.Printf("Bytes in unreferenced blocks: %d\n", rmd.UnrefBytes)
	// TODO: Print unknown fields.
	fmt.Print("\n")

	fmt.Print("Private metadata\n")
	fmt.Print("----------------\n")
	fmt.Printf("Serialized size: %d bytes\n", len(rmd.SerializedPrivateMetadata))

	data := rmd.Data()
	// TODO: Clean up output.
	fmt.Printf("Dir: %s\n", data.Dir)
	fmt.Print("TLF private key: {32 bytes}\n")
	if data.Changes.Info != (libkbfs.BlockInfo{}) {
		fmt.Printf("Block changes block: %v\n", data.Changes.Info)
	}
	for i, op := range data.Changes.Ops {
		fmt.Printf("Op[%d]: %v\n", i, op)
	}
	// TODO: Print unknown fields.

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
