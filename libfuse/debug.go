// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"fmt"
	"strings"

	"github.com/keybase/client/go/logger"
)

// Returns a function that logs its argument to the given log,
// suitable to assign to fuse.Debug.
func MakeFuseDebugFn(
	log logger.Logger, superVerbose bool) func(msg interface{}) {
	return func(msg interface{}) {
		str := fmt.Sprintf("%s", msg)
		// If superVerbose is not set, filter out Statfs and
		// Access messages, since they're spammy on OS X.
		if !superVerbose &&
			(strings.HasPrefix(str, "<- ") ||
				strings.HasPrefix(str, "-> ")) &&
			(strings.Contains(str, " Statfs") ||
				strings.Contains(str, " Access")) {
			return
		}
		log.Debug("%s", str)
	}
}
