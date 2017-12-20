package main

import (
	"fmt"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/kbfsmd"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

func mdDumpGetDeviceStringForCryptPublicKey(k kbfscrypto.CryptPublicKey, ui libkbfs.UserInfo) (
	string, bool) {
	deviceName, ok := ui.KIDNames[k.KID()]
	if !ok {
		return "", false
	}

	if revokedTime, ok := ui.RevokedCryptPublicKeys[k]; ok {
		return fmt.Sprintf("%s (revoked %s) (kid:%s)",
			deviceName, revokedTime.Unix.Time(), k), true
	}

	return fmt.Sprintf("%s (kid:%s)", deviceName, k), true
}

func mdDumpGetDeviceStringForVerifyingKey(k kbfscrypto.VerifyingKey, ui libkbfs.UserInfo) (
	string, bool) {
	deviceName, ok := ui.KIDNames[k.KID()]
	if !ok {
		return "", false
	}

	if revokedTime, ok := ui.RevokedVerifyingKeys[k]; ok {
		return fmt.Sprintf("%s (revoked %s) (kid:%s)",
			deviceName, revokedTime.Unix.Time(), k), true
	}

	return fmt.Sprintf("%s (kid:%s)", deviceName, k), true
}

func mdDumpGetReplacements(ctx context.Context, prefix string,
	codec kbfscodec.Codec, service libkbfs.KeybaseService,
	rmd kbfsmd.RootMetadata, extra kbfsmd.ExtraMetadata) (
	map[string]string, error) {
	writers, readers, err := rmd.GetUserDevicePublicKeys(extra)
	if err != nil {
		return nil, err
	}

	// TODO: Add caching for the service calls.

	replacements := make(map[string]string)
	for _, userKeys := range []kbfsmd.UserDevicePublicKeys{writers, readers} {
		for u, deviceKeys := range userKeys {
			if _, ok := replacements[u.String()]; ok {
				continue
			}

			username, _, err := service.Resolve(
				ctx, fmt.Sprintf("uid:%s", u))
			if err == nil {
				replacements[u.String()] = fmt.Sprintf(
					"%s (uid:%s)", username, u)
			} else {
				printError(prefix, err)
			}

			ui, err := service.LoadUserPlusKeys(ctx, u, "")
			if err != nil {
				continue
			}

			for k := range deviceKeys {
				if _, ok := replacements[k.String()]; ok {
					continue
				}

				if deviceStr, ok := mdDumpGetDeviceStringForCryptPublicKey(k, ui); ok {
					replacements[k.String()] = deviceStr
				}
			}

			// The MD doesn't know about verifying keys,
			// so just go through all of them.

			for _, k := range ui.VerifyingKeys {
				if _, ok := replacements[k.String()]; ok {
					continue
				}

				if deviceStr, ok := mdDumpGetDeviceStringForVerifyingKey(k, ui); ok {
					replacements[k.String()] = deviceStr
				}
			}

			for k := range ui.RevokedVerifyingKeys {
				if _, ok := replacements[k.String()]; ok {
					continue
				}

				if deviceStr, ok := mdDumpGetDeviceStringForVerifyingKey(k, ui); ok {
					replacements[k.String()] = deviceStr
				}
			}
		}
	}

	return replacements, nil
}

func mdDumpReplaceAll(s string, replacements map[string]string) string {
	for old, new := range replacements {
		s = strings.Replace(s, old, new, -1)
	}
	return s
}

func mdDumpReadOnlyRMDWithReplacements(
	ctx context.Context, codec kbfscodec.Codec,
	rmd libkbfs.ReadOnlyRootMetadata, replacements map[string]string) error {
	c := spew.NewDefaultConfig()
	c.Indent = "  "
	c.DisablePointerAddresses = true
	c.DisableCapacities = true
	c.SortKeys = true

	fmt.Print("Root metadata\n")
	fmt.Print("-------------\n")

	brmdDump, err := kbfsmd.DumpRootMetadata(codec, rmd.GetBareRootMetadata())
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", mdDumpReplaceAll(brmdDump, replacements))

	fmt.Print("Extra metadata\n")
	fmt.Print("--------------\n")
	extraDump, err := kbfsmd.DumpExtraMetadata(codec, rmd.Extra())
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", mdDumpReplaceAll(extraDump, replacements))

	fmt.Print("Private metadata\n")
	fmt.Print("----------------\n")
	pmdDump, err := libkbfs.DumpPrivateMetadata(
		codec, rmd.GetSerializedPrivateMetadata(), *rmd.Data())
	if err != nil {
		return err
	}
	fmt.Printf("%s", mdDumpReplaceAll(pmdDump, replacements))

	return nil
}

func mdDumpReadOnlyRMD(ctx context.Context, prefix string,
	config libkbfs.Config, rmd libkbfs.ReadOnlyRootMetadata) error {
	replacements, err := mdDumpGetReplacements(
		ctx, prefix, config.Codec(), config.KeybaseService(),
		rmd.GetBareRootMetadata(), rmd.Extra())
	if err != nil {
		printError(prefix, err)
	}

	return mdDumpReadOnlyRMDWithReplacements(
		ctx, config.Codec(), rmd, replacements)
}
