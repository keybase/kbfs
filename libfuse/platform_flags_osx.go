// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build darwin

package libfuse

import (
	"flag"
	"strings"
)

// PlatformParams contains all platform-specific parameters to be
// passed to New{Default,Force}Mounter.
type PlatformParams struct {
	UseSystemFuse        bool
	NoLocal              bool
	ExperimentalFeatures string

	experimentalFeatures map[string]bool
}

// ProcessPlatformFlags populates p.experimentalFeatures for quick lookup.
func (p PlatformParams) ProcessPlatformFlags() {
	p.experimentalFeatures = make(map[string]bool)
	for _, f := range strings.Split(p.ExperimentalFeatures, ",") {
		p.experimentalFeatures[f] = true
	}
}

func (p PlatformParams) macosRootSpecialFilesEnabled() bool {
	return p.experimentalFeatures["macos_root_special_files"]
}

// GetPlatformUsageString returns a string to be included in a usage
// string corresponding to the flags added by AddPlatformFlags.
func GetPlatformUsageString() string {
	return "[--use-system-fuse] [--no-local] [--experimental-features]\n    "
}

// AddPlatformFlags adds platform-specific flags to the given FlagSet
// and returns a PlatformParams object that will be filled in when the
// given FlagSet is parsed.
func AddPlatformFlags(flags *flag.FlagSet) *PlatformParams {
	var params PlatformParams
	flags.BoolVar(&params.UseSystemFuse, "use-system-fuse", false,
		"Use the system OSXFUSE instead of keybase's OSXFUSE")
	flags.BoolVar(&params.NoLocal, "no-local", false,
		"Do not use 'local' mount option")
	flags.StringVar(&params.ExperimentalFeatures, "experimental-features", "",
		"Enable hacky or experimental features for testing macOS "+
			"apps. Specify features using a comma-separated list. "+
			"Available features include: macos_root_special_files")
	return &params
}
