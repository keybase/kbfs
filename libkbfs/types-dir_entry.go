// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/keybase/go-codec/codec"

// DirEntry is all the data info a directory know about its child.
type IFCERFTDirEntry struct {
	IFCERFTBlockInfo
	IFCERFTEntryInfo

	codec.UnknownFieldSetHandler
}

// IsInitialized returns true if this DirEntry has been initialized.
func (de *IFCERFTDirEntry) IsInitialized() bool {
	return de.IFCERFTBlockPointer.IsInitialized()
}
