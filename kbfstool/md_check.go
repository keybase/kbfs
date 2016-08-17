package main

import (
	"flag"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

const mdCheckUsageStr = `Usage:
  kbfstool md check input [inputs...]

Each input must be in the same format as in md dump.

`

func checkDirBlock(ctx context.Context, config libkbfs.Config,
	kmd libkbfs.KeyMetadata, info libkbfs.BlockInfo) error {
	var dirBlock libkbfs.DirBlock
	err := config.BlockOps().Get(ctx, kmd, info.BlockPointer, &dirBlock)
	if err != nil {
		return err
	}
	return nil
}

func checkFileBlock(ctx context.Context, config libkbfs.Config,
	kmd libkbfs.KeyMetadata, info libkbfs.BlockInfo) error {
	var fileBlock libkbfs.FileBlock
	err := config.BlockOps().Get(ctx, kmd, info.BlockPointer, &fileBlock)
	if err != nil {
		return err
	}
	return nil
}

func mdCheckOne(ctx context.Context, config libkbfs.Config,
	rmd libkbfs.ImmutableRootMetadata) error {
	data := rmd.Data()

	if data.ChangesBlockInfo() == (libkbfs.BlockInfo{}) {
		fmt.Print("No MD changes block to check; skipping\n")
	} else {
		bi := data.ChangesBlockInfo()
		fmt.Printf("Checking MD changes block %v...\n", bi)
		err := checkFileBlock(ctx, config, rmd, bi)
		if err != nil {
			fmt.Printf("Got error while checking MD changes block %v: %v\n",
				bi, err)
		}
	}

	fmt.Printf("Checking dir block %v...\n", data.Dir)
	err := checkDirBlock(ctx, config, rmd, data.Dir.BlockInfo)
	if err != nil {
		fmt.Printf("Got error while checking dir block %v: %v\n",
			data.Dir, err)
	}

	return nil
}

func mdCheck(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md check", flag.ContinueOnError)
	flags.Parse(args)

	inputs := flags.Args()
	if len(inputs) < 1 {
		fmt.Print(mdCheckUsageStr)
		return 1
	}

	for _, input := range inputs {
		irmd, err := mdParseAndGet(ctx, config, input)
		if err != nil {
			printError("md check", err)
			return 1
		}

		if irmd == (libkbfs.ImmutableRootMetadata{}) {
			fmt.Printf("No result found for %q\n\n", input)
			continue
		}

		err = mdCheckOne(ctx, config, irmd)
		if err != nil {
			printError("md check", err)
			return 1
		}

		fmt.Print("\n")
	}

	return 0
}
