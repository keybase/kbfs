package main

import (
	"flag"
	"fmt"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func getUserString(
	ctx context.Context, config libkbfs.Config, uid keybase1.UID) string {
	username, _, err := config.KeybaseService().Resolve(
		ctx, fmt.Sprintf("uid:%s", uid))
	if err != nil {
		printError("md dump", err)
		return uid.String()
	}
	return fmt.Sprintf("%s (uid:%s)", username, uid)
}

func mdDumpOne(ctx context.Context, config libkbfs.Config,
	rmd libkbfs.ImmutableRootMetadata) error {
	fmt.Printf("MD ID: %s\n", rmd.MdID())

	return mdDumpOneReadOnly(ctx, config, rmd.ReadOnly())
}

func mdDumpOneReadOnly(ctx context.Context, config libkbfs.Config,
	rmd libkbfs.ReadOnlyRootMetadata) error {
	buf, err := config.Codec().Encode(rmd.GetBareRootMetadata())
	if err != nil {
		return err
	}
	fmt.Printf("MD size: %d bytes\nMD version: %s\n\n",
		len(buf), rmd.Version())

	h := rmd.GetTlfHandle()
	bh, err := h.ToBareHandle()
	if err != nil {
		return err
	}

	fmt.Print("Reader/writer metadata\n")
	fmt.Print("----------------------\n")
	fmt.Printf("Last modifying user: %s\n",
		getUserString(ctx, config, rmd.LastModifyingUser()))
	// TODO: Print flags.
	fmt.Printf("Revision: %s\n", rmd.Revision())
	fmt.Printf("Prev MD ID: %s\n", rmd.PrevRoot())
	if rmd.TlfID().IsPublic() {
		fmt.Print("Readers: everybody (public)\n")
	} else if len(bh.Readers) == 0 {
		fmt.Print("Readers: empty\n")
	} else {
		fmt.Print("Readers:\n")
		for _, reader := range bh.Readers {
			fmt.Printf("  %s\n",
				getUserString(ctx, config, reader))
		}
	}
	fmt.Print("Writers:\n")
	for _, writer := range bh.Writers {
		fmt.Printf("  %s\n", getUserString(ctx, config, writer))
	}
	// TODO: Print RKeys, unresolved readers, conflict info,
	// finalized info, and unknown fields.
	fmt.Print("\n")

	fmt.Print("Writer metadata\n")
	fmt.Print("---------------\n")
	fmt.Printf("Last modifying writer: %s\n",
		getUserString(ctx, config, rmd.LastModifyingWriter()))
	// TODO: Print Writers/WKeys and unresolved writers.
	fmt.Printf("TLF ID: %s\n", rmd.TlfID())
	fmt.Printf("Branch ID: %s\n", rmd.BID())
	// TODO: Print writer flags.
	fmt.Printf("Disk usage: %d\n", rmd.DiskUsage())
	fmt.Printf("Bytes in new blocks: %d\n", rmd.RefBytes())
	fmt.Printf("Bytes in unreferenced blocks: %d\n", rmd.UnrefBytes())
	// TODO: Print unknown fields.
	fmt.Print("\n")

	fmt.Print("Extra metadata\n")
	fmt.Print("--------------\n")
	fmt.Printf("%v\n", rmd.Extra())
	fmt.Print("\n")

	fmt.Print("Private metadata\n")
	fmt.Print("----------------\n")
	fmt.Printf("Serialized size: %d bytes\n", len(rmd.GetSerializedPrivateMetadata()))

	data := rmd.Data()
	// TODO: Clean up output.
	fmt.Printf("Dir: %s\n", data.Dir)
	fmt.Print("TLF private key: {32 bytes}\n")
	if data.ChangesBlockInfo() != (libkbfs.BlockInfo{}) {
		fmt.Printf("Block changes block: %v\n", data.ChangesBlockInfo())
	}
	for i, op := range data.Changes.Ops {
		fmt.Printf("Op[%d]: %s", i, op.StringWithRefs(1))
	}
	// TODO: Print unknown fields.

	return nil
}

const mdDumpUsageStr = `Usage:
  kbfstool md dump input [inputs...]

Each input must be in the following format:

  TLF
  TLF:Branch
  TLF^Revision
  TLF:Branch^Revision

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

and Revision can be:

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

	for _, input := range inputs {
		irmd, err := mdParseAndGet(ctx, config, input)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		if irmd == (libkbfs.ImmutableRootMetadata{}) {
			fmt.Printf("No result found for %q\n\n", input)
			continue
		}

		fmt.Printf("Result for %q:\n\n", input)

		err = mdDumpOne(ctx, config, irmd)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		fmt.Print("\n")
	}

	return 0
}
