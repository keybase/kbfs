// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/go-codec/codec"
)

type dirEntryFuture struct {
	IFCERFTDirEntry
	extra
}

func (cof dirEntryFuture) toCurrent() IFCERFTDirEntry {
	return cof.IFCERFTDirEntry
}

func (cof dirEntryFuture) toCurrentStruct() currentStruct {
	return cof.toCurrent()
}

func makeFakeDirEntryFuture(t *testing.T) dirEntryFuture {
	cof := dirEntryFuture{
		IFCERFTDirEntry{
			makeFakeBlockInfo(t),
			IFCERFTEntryInfo{
				IFCERFTDir,
				100,
				"fake sym path",
				101,
				102,
			},
			codec.UnknownFieldSetHandler{},
		},
		makeExtraOrBust("dirEntry", t),
	}
	return cof
}

func TestDirEntryUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeDirEntryFuture(t))
}
