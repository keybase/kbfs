package main

import (
	"flag"
	"fmt"
	"regexp"
	"strconv"

	"github.com/keybase/client/go/protocol"
	"github.com/keybase/kbfs/fsrpc"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

var mdGetRe = regexp.MustCompile("^(.+?)(?::(.*?))?(?:\\^(.*?))?$")

type tlfPart struct {
	id     libkbfs.TlfID
	handle *libkbfs.TlfHandle
}

func parseTlfPart(
	ctx context.Context, config libkbfs.Config, tlfPartStr string) (
	tlfPart, error) {
	tlfID, err := libkbfs.ParseTlfID(tlfPartStr)
	if err == nil {
		return tlfPart{id: tlfID}, nil
	}

	var handle *libkbfs.TlfHandle
	p, err := fsrpc.NewPath(tlfPartStr)
	if err != nil {
		return tlfPart{}, err
	}
	if p.PathType != fsrpc.TLFPathType {
		return tlfPart{}, fmt.Errorf("%q is not a TLF path", tlfPartStr)
	}
	if len(p.TLFComponents) > 0 {
		return tlfPart{}, fmt.Errorf("%q is not the root path of a TLF", tlfPartStr)
	}
	name := p.TLFName
outer:
	for {
		var err error
		handle, err = libkbfs.ParseTlfHandle(
			ctx, config.KBPKI(), name, p.Public)
		switch err := err.(type) {
		case nil:
			// No error.
			break outer

		case libkbfs.TlfNameNotCanonical:
			// Non-canonical name, so try again.
			name = err.NameToTry

		default:
			// Some other error.
			return tlfPart{}, err
		}
	}

	return tlfPart{handle: handle}, nil
}

func mdGet(ctx context.Context, config libkbfs.Config, input string) (
	libkbfs.ImmutableRootMetadata, error) {
	parts := mdGetRe.FindStringSubmatch(input)
	if parts == nil {
		return libkbfs.ImmutableRootMetadata{},
			fmt.Errorf("Could not parse %q", input)
	}

	tlfPartStr := parts[1]
	branchPart := parts[2]
	revPart := parts[3]

	tlfPart, err := parseTlfPart(ctx, config, tlfPartStr)
	if err != nil {
		return libkbfs.ImmutableRootMetadata{}, err
	}

	var branchID *libkbfs.BranchID
	if len(branchPart) > 0 {
		parsedBranchID := libkbfs.ParseBranchID(branchPart)
		if parsedBranchID == libkbfs.NullBranchID && branchPart != "00000000000000000000000000000000" {
			return libkbfs.ImmutableRootMetadata{}, fmt.Errorf("%q is not a valid branch ID", branchPart)
		}
		branchID = &parsedBranchID
	}

	var revision libkbfs.MetadataRevision
	if len(revPart) > 0 {
		// TODO: Figure out when to use base 10 or base 16.
		u, err := strconv.ParseUint(revPart, 10, 64)
		if err != nil {
			return libkbfs.ImmutableRootMetadata{}, err
		}
		revision = libkbfs.MetadataRevision(u)
	}

	mdOps := config.MDOps()

	if tlfPart.id == (libkbfs.TlfID{}) {
		// Use tlfHandle.
		if branchID == nil {
			if revision == libkbfs.MetadataRevisionUninitialized {
				_, irmd, err := mdOps.GetForHandle(
					ctx, tlfPart.handle, libkbfs.Unmerged)
				if err != nil {
					return libkbfs.ImmutableRootMetadata{}, err
				}

				if irmd != (libkbfs.ImmutableRootMetadata{}) {
					return irmd, nil
				}

				_, irmd, err = mdOps.GetForHandle(
					ctx, tlfPart.handle, libkbfs.Merged)
				return irmd, err
			}
			panic("Unimplemented")
		}

		if *branchID == libkbfs.NullBranchID {
			if revision == libkbfs.MetadataRevisionUninitialized {
				panic("Unimplemented")
			}
			panic("Unimplemented")
		}

		if revision == libkbfs.MetadataRevisionUninitialized {
			panic("Unimplemented")
		}
		panic("Unimplemented")
	}

	// Use tlfID.
	if branchID == nil {
		if revision == libkbfs.MetadataRevisionUninitialized {
			irmd, err := mdOps.GetUnmergedForTLF(
				ctx, tlfPart.id, libkbfs.NullBranchID)
			if err != nil {
				return libkbfs.ImmutableRootMetadata{}, err
			}

			if irmd != (libkbfs.ImmutableRootMetadata{}) {
				return irmd, nil
			}

			return mdOps.GetForTLF(ctx, tlfPart.id)
		}

		irmds, err := mdOps.GetUnmergedRange(
			ctx, tlfPart.id, libkbfs.NullBranchID, revision, revision)
		if err != nil {
			return libkbfs.ImmutableRootMetadata{}, err
		}

		if len(irmds) >= 1 {
			return irmds[0], nil
		}

		irmds, err = mdOps.GetRange(ctx, tlfPart.id, revision, revision)
		if err != nil {
			return libkbfs.ImmutableRootMetadata{}, err
		}

		if len(irmds) >= 1 {
			return irmds[0], nil
		}

		return libkbfs.ImmutableRootMetadata{}, nil
	}

	if *branchID == libkbfs.NullBranchID {
		if revision == libkbfs.MetadataRevisionUninitialized {
			return mdOps.GetForTLF(ctx, tlfPart.id)
		}

		irmds, err := mdOps.GetRange(ctx, tlfPart.id, revision, revision)
		if err != nil {
			return libkbfs.ImmutableRootMetadata{}, err
		}

		if len(irmds) >= 1 {
			return irmds[0], nil
		}

		return libkbfs.ImmutableRootMetadata{}, nil
	}

	if revision == libkbfs.MetadataRevisionUninitialized {
		panic("Unimplemented")
	}
	panic("Unimplemented")
}

func getUserString(ctx context.Context, config libkbfs.Config, uid keybase1.UID) string {
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
	mdID, err := config.Crypto().MakeMdID(&rmd.BareRootMetadata)
	if err != nil {
		return err
	}

	fmt.Printf("MD ID: %s\n", mdID)

	buf, err := config.Codec().Encode(&rmd.BareRootMetadata)
	if err != nil {
		return err
	}

	fmt.Printf("MD size: %d bytes\n\n", len(buf))

	fmt.Print("Reader/writer metadata\n")
	fmt.Print("----------------------\n")
	fmt.Printf("Last modifying user: %s\n",
		getUserString(ctx, config, rmd.LastModifyingUser))
	// TODO: Print flags.
	fmt.Printf("Revision: %s\n", rmd.Revision)
	fmt.Printf("Prev MD ID: %s\n", rmd.PrevRoot)
	// TODO: Print RKeys, unresolved readers, conflict info,
	// finalized info, and unknown fields.
	fmt.Print("\n")

	fmt.Print("Writer metadata\n")
	fmt.Print("---------------\n")
	fmt.Printf("Last modifying writer: %s\n",
		getUserString(ctx, config, rmd.LastModifyingWriter))
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
	if data.ChangesBlockInfo() != (libkbfs.BlockInfo{}) {
		fmt.Printf("Block changes block: %v\n", data.ChangesBlockInfo())
	}
	for i, op := range data.Changes.Ops {
		fmt.Printf("Op[%d]: %v\n", i, op)
	}
	// TODO: Print unknown fields.

	return nil
}

const mdDumpUsageStr = `Usage:
  kbfstool md dump input [inputs...]

Each input can be in any of the following formats:

  TlfID
  TlfID:BranchID
  TlfID^Revision
  TlfID:BranchID^Revision

  /keybase/(public|private)/tlfname
  /keybase/(public|private)/tlfname:BranchID
  /keybase/(public|private)/tlfname^Revision
  /keybase/(public|private)/tlfname:BranchID^Revision

If BranchID is omitted, the unmerged branch for the current device is
used, or the master branch if there is no unmerged branch. If Revision
is omitted, the latest revision for the branch is used.

`

func mdDump(ctx context.Context, config libkbfs.Config, args []string) (exitStatus int) {
	flags := flag.NewFlagSet("kbfs md dump", flag.ContinueOnError)
	flags.Parse(args)

	inputs := flags.Args()
	if len(inputs) < 1 {
		fmt.Print(mdDumpUsageStr)
		return 1
	}

	for _, input := range inputs {
		irmd, err := mdGet(ctx, config, input)
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
