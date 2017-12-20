package main

import (
	"flag"
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func mdDumpImmutableRMD(ctx context.Context, config libkbfs.Config,
	irmd libkbfs.ImmutableRootMetadata,
	replacements map[string]string) error {
	err := mdDumpFillReplacements(
		ctx, "md dump", config.Codec(), config.KeybaseService(),
		irmd.GetBareRootMetadata(), irmd.Extra(), replacements)
	if err != nil {
		printError("md dump", err)
	}

	fmt.Print("Immutable metadata\n")
	fmt.Print("------------------\n")

	fmt.Printf("MD ID: %s\n", irmd.MdID())
	fmt.Printf("Local timestamp: %s\n", irmd.LocalTimestamp())
	fmt.Printf("Last modifying device (verifying key): %s\n",
		mdDumpReplaceAll(irmd.LastModifyingWriterVerifyingKey().String(), replacements))
	fmt.Print("\n")

	return mdDumpReadOnlyRMDWithReplacements(
		ctx, config.Codec(), irmd.ReadOnly(), replacements)
}

const mdDumpUsageStr = `Usage:
  kbfstool md dump input [inputs...]

Each input must be in the following format:

  TLF
  TLF:Branch
  TLF^RevisionRange
  TLF:Branch^RevisionRange

where TLF can be:

  - a TLF ID string (32 hex digits),
  - or a keybase TLF path (e.g., "/keybase/public/user1,user2", or
    "/keybase/private/user1,assertion2");

Branch can be:

  - a Branch ID string (32 hex digits),
  - the string "device", which indicates the unmerged branch for the
    current device, or the master branch if there is no unmerged branch,
  - the string "master", which is a shorthand for
    the ID of the master branch "00000000000000000000000000000000", or
  - omitted, in which case it is treated as if it were the string "device";

and RevisionRange can be in the following format:

  Revision
  Revision-Revision

where Revision can be:

  - a hex number prefixed with "0x",
  - a decimal number with no prefix,
  - the string "latest", which indicates the latest revision for the
    branch, or
  - omitted, in which case it is treated as if it were the string "latest".

`

func mdDump(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md dump", flag.ContinueOnError)
	err := flags.Parse(args)
	if err != nil {
		printError("md dump", err)
		return 1
	}

	inputs := flags.Args()
	if len(inputs) < 1 {
		fmt.Print(mdDumpUsageStr)
		return 1
	}

	replacements := make(map[string]string)

	for _, input := range inputs {
		tlfID, branchID, start, stop, err :=
			mdParseInput(ctx, config, input)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		// TODO: Chunk the range between start and stop.
		irmds, _, err := mdGet(ctx, config, tlfID, branchID, start, stop)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		fmt.Printf("%d results for %q:\n\n", len(irmds), input)

		for _, irmd := range irmds {
			err = mdDumpImmutableRMD(ctx, config, irmd, replacements)
			if err != nil {
				printError("md dump", err)
				return 1
			}

			fmt.Print("\n")
		}
	}

	return 0
}
