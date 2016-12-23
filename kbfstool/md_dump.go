package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"reflect"

	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscrypto"
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

func mdDumpUnknownFields(handler codec.UnknownFieldSetHandler) {
	if reflect.DeepEqual(handler, codec.UnknownFieldSetHandler{}) {
		return
	}

	fmt.Printf("Unknown fields: %+v\n", handler)
}

func mdDumpWriterFlags(wFlags libkbfs.WriterFlags) {
	fmt.Printf("Unmerged: %t\n", wFlags&libkbfs.MetadataFlagUnmerged != 0)
}

func mdDumpTLFCryptKeyInfo(info libkbfs.TLFCryptKeyInfo) {
	fmt.Printf("      Client half (encryption version=%d):\n",
		info.ClientHalf.Version)
	fmt.Printf("        Encrypted data: %s\n",
		hex.EncodeToString(info.ClientHalf.EncryptedData))
	fmt.Printf("        Nonce: %s\n",
		hex.EncodeToString(info.ClientHalf.Nonce))
	fmt.Printf("      Server half ID: %s\n",
		info.ServerHalfID)
	fmt.Printf("      Ephemeral key index: %d\n",
		info.EPubKeyIndex)
}

func mdDumpUDKIMV2(ctx context.Context, config libkbfs.Config,
	udkimV2 libkbfs.UserDeviceKeyInfoMapV2) {
	for uid, dkimV2 := range udkimV2 {
		fmt.Printf("  User: %s\n", getUserString(ctx, config, uid))
		for kid, info := range dkimV2 {
			fmt.Printf("    Device: %s\n", kid)
			mdDumpTLFCryptKeyInfo(info)
		}
	}
}

func mdDumpEphemeralPublicKeys(ePubKeys kbfscrypto.TLFEphemeralPublicKeys) {
	for i, ePubKey := range ePubKeys {
		fmt.Printf("    %d: %s\n", i, ePubKey)
	}
}

func mdDumpWriterKeyGenerationsV2(ctx context.Context, config libkbfs.Config,
	wKeys libkbfs.TLFWriterKeyGenerationsV2) {
	for i, wkb := range wKeys {
		fmt.Printf("  KeyGen %d:\n", libkbfs.FirstValidKeyGen+libkbfs.KeyGen(i))
		mdDumpUDKIMV2(ctx, config, wkb.WKeys)
		fmt.Printf("  TLF public key: %s\n", wkb.TLFPublicKey)
		mdDumpEphemeralPublicKeys(wkb.TLFEphemeralPublicKeys)
		// TODO: Handle indent.
		mdDumpUnknownFields(wkb.UnknownFieldSetHandler)
	}
}

func mdDumpSocialAssertions(
	name string, assertions []keybase1.SocialAssertion) {
	if len(assertions) > 0 {
		fmt.Printf("%s:\n", name)
		for i, assertion := range assertions {
			fmt.Printf("  %d: %s\n", i, assertion)
		}
	} else {
		fmt.Printf("%s: none\n", name)
	}
}

func mdDumpWMDV2(ctx context.Context, config libkbfs.Config,
	wmd *libkbfs.WriterMetadataV2) {
	fmt.Printf("Serialized private metadata size: %d bytes\n",
		len(wmd.SerializedPrivateMetadata))
	fmt.Printf("Last modifying writer: %s\n",
		getUserString(ctx, config, wmd.LastModifyingWriter))
	fmt.Print("Writers:\n")
	if wmd.ID.IsPublic() {
		for _, writer := range wmd.Writers {
			fmt.Printf("  %s\n", getUserString(ctx, config, writer))
		}
	} else {
		mdDumpWriterKeyGenerationsV2(ctx, config, wmd.WKeys)
	}
	mdDumpSocialAssertions("Unresolved writers",
		wmd.Extra.UnresolvedWriters)
	fmt.Printf("TLF ID: %s\n", wmd.ID)
	fmt.Printf("Branch ID: %s\n", wmd.BID)
	mdDumpWriterFlags(wmd.WFlags)
	fmt.Printf("Disk usage: %d\n", wmd.DiskUsage)
	fmt.Printf("Bytes in new blocks: %d\n", wmd.RefBytes)
	fmt.Printf("Bytes in unreferenced blocks: %d\n", wmd.UnrefBytes)
	mdDumpUnknownFields(wmd.Extra.UnknownFieldSetHandler)
	fmt.Print("\n")
}

func mdDumpBRMDV2(ctx context.Context, config libkbfs.Config,
	rmd *libkbfs.BareRootMetadataV2, extra libkbfs.ExtraMetadata) error {
	bh, err := rmd.MakeBareTlfHandle(extra)
	if err != nil {
		return err
	}

	fmt.Print("Reader/writer metadata\n")
	fmt.Print("----------------------\n")
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
	fmt.Printf("Last modifying user: %s\n",
		getUserString(ctx, config, rmd.LastModifyingUser))
	// TODO: Print flags.
	fmt.Printf("Revision: %s\n", rmd.Revision)
	fmt.Printf("Prev MD ID: %s\n", rmd.PrevRoot)
	fmt.Printf("Reader key bundle ID: %s\n", rmd.GetTLFReaderKeyBundleID())
	// TODO: Print RKeys, unresolved readers, conflict info,
	// finalized info, and unknown fields.
	fmt.Print("\n")

	fmt.Print("Writer metadata\n")
	fmt.Print("---------------\n")
	mdDumpWMDV2(ctx, config, &rmd.WriterMetadataV2)

	return nil
}

func mdDumpBRMDV3(ctx context.Context, config libkbfs.Config,
	rmd *libkbfs.BareRootMetadataV3, extra libkbfs.ExtraMetadata) error {
	bh, err := rmd.MakeBareTlfHandle(extra)
	if err != nil {
		return err
	}

	fmt.Print("Reader/writer metadata\n")
	fmt.Print("----------------------\n")
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
	fmt.Printf("Last modifying user: %s\n",
		getUserString(ctx, config, rmd.LastModifyingUser))
	// TODO: Print flags.
	fmt.Printf("Revision: %s\n", rmd.Revision)
	fmt.Printf("Prev MD ID: %s\n", rmd.PrevRoot)
	fmt.Printf("Reader key bundle ID: %s\n", rmd.GetTLFReaderKeyBundleID())
	// TODO: Print RKeys, unresolved readers, conflict info,
	// finalized info, and unknown fields.
	fmt.Print("\n")

	fmt.Print("Writer metadata\n")
	fmt.Print("---------------\n")
	fmt.Print("Writers:\n")
	for _, writer := range bh.Writers {
		fmt.Printf("  %s\n", getUserString(ctx, config, writer))
	}
	fmt.Printf("Last modifying writer: %s\n",
		getUserString(ctx, config, rmd.LastModifyingWriter()))
	// TODO: Print Writers/WKeys and unresolved writers.
	fmt.Printf("TLF ID: %s\n", rmd.TlfID())
	fmt.Printf("Branch ID: %s\n", rmd.BID())
	fmt.Printf("Writer key bundle ID: %s\n", rmd.GetTLFWriterKeyBundleID())
	// TODO: Print writer flags.
	fmt.Printf("Disk usage: %d\n", rmd.DiskUsage())
	fmt.Printf("Bytes in new blocks: %d\n", rmd.RefBytes())
	fmt.Printf("Bytes in unreferenced blocks: %d\n", rmd.UnrefBytes())
	// TODO: Print unknown fields.
	fmt.Print("\n")

	return nil
}

func mdDumpUDKIMV3(ctx context.Context, config libkbfs.Config,
	udkimV3 libkbfs.UserDeviceKeyInfoMapV3) {
	for uid, dkimV3 := range udkimV3 {
		fmt.Printf("  User: %s\n", getUserString(ctx, config, uid))
		for key, info := range dkimV3 {
			fmt.Printf("    Device: %s\n", key)
			mdDumpTLFCryptKeyInfo(info)
		}
	}
}

func mdDumpExtraV3(ctx context.Context, config libkbfs.Config,
	extraV3 *libkbfs.ExtraMetadataV3) {
	fmt.Print("Type: ExtraMetadataV3\n")
	wkb := extraV3.GetWriterKeyBundle()
	fmt.Print("Writer key bundle:\n")
	mdDumpUDKIMV3(ctx, config, wkb.Keys)
	fmt.Printf("  TLF public key: %s\n", wkb.TLFPublicKey)
	fmt.Print("  Ephemeral writer keys\n")
	mdDumpEphemeralPublicKeys(wkb.TLFEphemeralPublicKeys)
	encryptedHistoricKeys := wkb.EncryptedHistoricTLFCryptKeys
	if encryptedHistoricKeys.Version == 0 {
		fmt.Print("  Encrypted historic TLF crypt keys: none\n")
	} else {
		fmt.Printf("  Encrypted historic TLF crypt keys (encryption version=%d):\n",
			encryptedHistoricKeys.Version)
		fmt.Printf("    Encrypted data: %s\n",
			hex.EncodeToString(encryptedHistoricKeys.EncryptedData))
		fmt.Printf("    Nonce: %s\n",
			hex.EncodeToString(encryptedHistoricKeys.Nonce))
	}
	// TODO: Print unknown fields.
	rkb := extraV3.GetReaderKeyBundle()
	fmt.Print("Reader key bundle\n")
	mdDumpUDKIMV3(ctx, config, rkb.Keys)
	fmt.Print("  Ephemeral reader keys\n")
	mdDumpEphemeralPublicKeys(wkb.TLFEphemeralPublicKeys)
	// TODO: Print unknown fields.
}

func mdDumpPrivateMetadata(ctx context.Context, config libkbfs.Config,
	pmd *libkbfs.PrivateMetadata) {
	// TODO: Clean up output.
	fmt.Printf("Dir: %s\n", pmd.Dir)
	fmt.Print("TLF private key: {32 bytes}\n")
	if pmd.ChangesBlockInfo() != (libkbfs.BlockInfo{}) {
		fmt.Printf("Block changes block: %v\n", pmd.ChangesBlockInfo())
	}
	for i, op := range pmd.Changes.Ops {
		fmt.Printf("Op[%d]: %s", i, op.StringWithRefs(1))
	}
	// TODO: Print unknown fields.
}

func mdDumpReadOnlyRMD(ctx context.Context, config libkbfs.Config,
	rmd libkbfs.ReadOnlyRootMetadata) error {
	brmd := rmd.GetBareRootMetadata()
	buf, err := config.Codec().Encode(brmd)
	if err != nil {
		return err
	}
	fmt.Printf("MD size: %d bytes\nMD version: %s\n\n",
		len(buf), rmd.Version())

	extra := rmd.Extra()

	switch brmd := brmd.(type) {
	case *libkbfs.BareRootMetadataV2:
		err := mdDumpBRMDV2(ctx, config, brmd, extra)
		if err != nil {
			return err
		}
	case *libkbfs.BareRootMetadataV3:
		err := mdDumpBRMDV3(ctx, config, brmd, extra)
		if err != nil {
			return err
		}
	default:
		fmt.Printf("Unknown type: %T\n%+v\n", brmd, brmd)
	}

	fmt.Print("Extra metadata\n")
	fmt.Print("--------------\n")
	switch extra := extra.(type) {
	case nil:
		fmt.Print("Type: nil\n")
	case *libkbfs.ExtraMetadataV3:
		mdDumpExtraV3(ctx, config, extra)
	default:
		fmt.Print("Type: unknown\n")
		fmt.Printf("%+v\n", extra)
	}
	fmt.Print("\n")

	fmt.Print("Private metadata\n")
	fmt.Print("----------------\n")

	mdDumpPrivateMetadata(ctx, config, rmd.Data())

	return nil
}

func mdDumpImmutableRMD(ctx context.Context, config libkbfs.Config,
	rmd libkbfs.ImmutableRootMetadata) error {
	fmt.Printf("MD ID: %s\n", rmd.MdID())

	return mdDumpReadOnlyRMD(ctx, config, rmd.ReadOnly())
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

		err = mdDumpImmutableRMD(ctx, config, irmd)
		if err != nil {
			printError("md dump", err)
			return 1
		}

		fmt.Print("\n")
	}

	return 0
}
