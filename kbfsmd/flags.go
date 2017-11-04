// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsmd

import (
	"fmt"
	"strings"
)

// MetadataFlags bitfield.
type MetadataFlags byte

// Possible flags set in the MetadataFlags bitfield.
const (
	MetadataFlagRekey MetadataFlags = 1 << iota
	MetadataFlagWriterMetadataCopied
	MetadataFlagFinal
)

func (flags MetadataFlags) String() string {
	var flagStrs []string
	if flags&MetadataFlagRekey != 0 {
		flagStrs = append(flagStrs, "Rekey")
		flags &^= MetadataFlagRekey
	}
	if flags&MetadataFlagWriterMetadataCopied != 0 {
		flagStrs = append(flagStrs, "WriterMetadataCopied")
		flags &^= MetadataFlagWriterMetadataCopied
	}
	if flags&MetadataFlagFinal != 0 {
		flagStrs = append(flagStrs, "Final")
		flags &^= MetadataFlagFinal
	}
	if flags != 0 {
		flagStrs = append(flagStrs, fmt.Sprintf("%b", int(flags)))
	}
	return fmt.Sprintf("MetadataFlags(%s)", strings.Join(flagStrs, " | "))
}

// WriterFlags bitfield.
type WriterFlags byte

// Possible flags set in the WriterFlags bitfield.
const (
	MetadataFlagUnmerged WriterFlags = 1 << iota
)
