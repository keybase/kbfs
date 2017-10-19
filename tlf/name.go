// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import (
	"strings"
)

const (
	// ReaderSep is the string that separates readers from writers in a
	// TLF name.
	ReaderSep = "#"
)

// SplitName splits a TLF name into components.
func SplitName(name string) (writerNames, readerNames []string,
	extensionSuffix string, err error) {
	names := strings.SplitN(name, HandleExtensionSep, 2)
	if len(names) > 2 {
		return nil, nil, "", BadNameError{name}
	}
	if len(names) > 1 {
		extensionSuffix = names[1]
	}

	splitNames := strings.SplitN(names[0], ReaderSep, 3)
	if len(splitNames) > 2 {
		return nil, nil, "", BadNameError{name}
	}
	writerNames = strings.Split(splitNames[0], ",")
	if len(splitNames) > 1 {
		readerNames = strings.Split(splitNames[1], ",")
	}

	return writerNames, readerNames, extensionSuffix, nil
}
