package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

const mdCheckUsageStr = `Usage:
  kbfstool md check input [inputs...]

Each input must be in the same format as in md dump.

`

// TODO: The below checks could be sped up by fetching blocks in
// parallel.

// TODO: Factor out common code with StateChecker.findAllBlocksInPath.

func checkDirBlock(ctx context.Context, config libkbfs.Config,
	name string, kmd libkbfs.KeyMetadata, info libkbfs.BlockInfo,
	verbose bool) (err error) {
	if verbose {
		fmt.Printf("Checking %s (dir block %v)...\n", name, info)
	} else {
		fmt.Printf("Checking %s...\n", name)
	}
	defer func() {
		if err != nil {
			fmt.Printf("Got error while checking %s: %v\n",
				name, err)
		}
	}()

	var dirBlock libkbfs.DirBlock
	err = config.BlockOps().Get(ctx, kmd, info.BlockPointer, &dirBlock)
	if err != nil {
		return err
	}

	for entryName, entry := range dirBlock.Children {
		switch entry.Type {
		case libkbfs.File, libkbfs.Exec:
			_ = checkFileBlock(
				ctx, config, filepath.Join(name, entryName),
				kmd, entry.BlockInfo, verbose)
		case libkbfs.Dir:
			_ = checkDirBlock(
				ctx, config, filepath.Join(name, entryName),
				kmd, entry.BlockInfo, verbose)
		case libkbfs.Sym:
			if verbose {
				fmt.Printf("Skipping symlink %s -> %s\n",
					entryName, entry.SymPath)
			}
			continue
		default:
			fmt.Printf("Entry %s has unknown type %s",
				entryName, entry.Type)
		}
	}

	return nil
}

func checkFileBlock(ctx context.Context, config libkbfs.Config,
	name string, kmd libkbfs.KeyMetadata, info libkbfs.BlockInfo,
	verbose bool) (err error) {
	if verbose {
		fmt.Printf("Checking %s (file block %v)...\n", name, info)
	} else {
		fmt.Printf("Checking %s...\n", name)
	}
	defer func() {
		if err != nil {
			fmt.Printf("Got error while checking %s: %v\n",
				name, err)
		}
	}()

	var fileBlock libkbfs.FileBlock
	err = config.BlockOps().Get(ctx, kmd, info.BlockPointer, &fileBlock)
	if err != nil {
		return err
	}

	if fileBlock.IsInd {
		// TODO: Check continuity of off+len if Holes is false
		// for all blocks.
		for _, iptr := range fileBlock.IPtrs {
			_ = checkFileBlock(
				ctx, config,
				fmt.Sprintf("%s (off=%d)", name, iptr.Off),
				kmd, iptr.BlockInfo, verbose)
		}
	}
	return nil
}

func mdCheckMDChain(ctx context.Context, config libkbfs.Config,
	input string, irmd libkbfs.ImmutableRootMetadata, verbose bool) error {
	checkDirMin := libkbfs.MetadataRevisionInitial
	for irmd.Revision() > libkbfs.MetadataRevisionInitial {
		if irmd.Revision() >= checkDirMin {
			fmt.Printf("Checking %d root...\n", irmd.Revision())
			var dirBlock libkbfs.DirBlock
			err := config.BlockOps().Get(
				ctx, irmd, irmd.Data().Dir.BlockPointer, &dirBlock)
			if err != nil {
				fmt.Printf("Got error while checking %d root: %v\n",
					irmd.Revision(), err)
			}
		}

		for _, op := range irmd.Data().Changes.Ops {
			if gcOp, ok := op.(*libkbfs.GCOp); ok {
				fmt.Printf("GC op up to %d found\n",
					gcOp.LatestRev)
				if gcOp.LatestRev+1 > checkDirMin {
					checkDirMin = gcOp.LatestRev + 1
				}
			}
		}

		if verbose {
			fmt.Printf(
				"Fetching revision %d...\n", irmd.Revision()-1)
		}
		irmdPrev, err := mdGet(ctx, config, irmd.TlfID(),
			irmd.BID(), irmd.Revision()-1)
		if err != nil {
			return err
		}

		if verbose {
			fmt.Printf("Checking %d -> %d...\n",
				irmd.Revision()-1, irmd.Revision())
		}
		err = irmdPrev.CheckValidSuccessor(
			irmdPrev.MdID(), irmd.ReadOnly())
		if err != nil {
			fmt.Printf("Got error while checking %d -> %d: %v\n",
				irmd.Revision()-1, irmd.Revision(), err)
		}

		irmd = irmdPrev
	}
	return nil
}

func mdCheckOne(ctx context.Context, config libkbfs.Config,
	input string, irmd libkbfs.ImmutableRootMetadata, verbose bool) error {
	_ = mdCheckMDChain(ctx, config, input, irmd, verbose)

	data := irmd.Data()

	if data.ChangesBlockInfo() == (libkbfs.BlockInfo{}) {
		if verbose {
			fmt.Print("No MD changes block to check; skipping\n")
		}
	} else {
		bi := data.ChangesBlockInfo()
		_ = checkFileBlock(
			ctx, config, fmt.Sprintf("%s MD changes block", input),
			irmd, bi, verbose)
	}

	//	_ = checkDirBlock(ctx, config, input, irmd, data.Dir.BlockInfo, verbose)
	return nil
}

func mdCheck(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md check", flag.ContinueOnError)
	verbose := flags.Bool("v", false, "Print verbose output.")
	flags.Parse(args)

	inputs := flags.Args()
	if len(inputs) < 1 {
		fmt.Print(mdCheckUsageStr)
		return 1
	}

	for _, input := range inputs {
		// The returned RMD is already verified, so we don't
		// have to do anything else.
		//
		// TODO: Check the validity of the entire MD chain.
		irmd, err := mdParseAndGet(ctx, config, input)
		if err != nil {
			printError("md check", err)
			return 1
		}

		if irmd == (libkbfs.ImmutableRootMetadata{}) {
			fmt.Printf("No result found for %q\n\n", input)
			continue
		}

		err = mdCheckOne(ctx, config, input, irmd, *verbose)
		if err != nil {
			printError("md check", err)
			return 1
		}

		fmt.Print("\n")
	}

	return 0
}
