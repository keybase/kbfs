// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/go-codec/codec"
	"github.com/stretchr/testify/require"
)

func makeFakeBlockPointer(t *testing.T) IFCERFTBlockPointer {
	h, err := IFCERFTDefaultHash([]byte("fake buf"))
	require.NoError(t, err)
	return IFCERFTBlockPointer{
		IFCERFTBlockID{h},
		5,
		1,
		IFCERFTBlockContext{
			"fake creator",
			"fake writer",
			IFCERFTBlockRefNonce{0xb},
		},
	}
}

func makeFakeBlockInfo(t *testing.T) IFCERFTBlockInfo {
	return IFCERFTBlockInfo{
		makeFakeBlockPointer(t),
		150,
	}
}

type indirectDirPtrCurrent IFCERFTIndirectDirPtr

type indirectDirPtrFuture struct {
	indirectDirPtrCurrent
	extra
}

func (pf indirectDirPtrFuture) toCurrent() indirectDirPtrCurrent {
	return pf.indirectDirPtrCurrent
}

func (pf indirectDirPtrFuture) toCurrentStruct() currentStruct {
	return pf.toCurrent()
}

func makeFakeIndirectDirPtrFuture(t *testing.T) indirectDirPtrFuture {
	return indirectDirPtrFuture{
		indirectDirPtrCurrent{
			makeFakeBlockInfo(t),
			"offset",
			codec.UnknownFieldSetHandler{},
		},
		makeExtraOrBust("IndirectDirPtr", t),
	}
}

func TestIndirectDirPtrUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeIndirectDirPtrFuture(t))
}

type indirectFilePtrCurrent IFCERFTIndirectFilePtr

type indirectFilePtrFuture struct {
	indirectFilePtrCurrent
	extra
}

func (pf indirectFilePtrFuture) toCurrent() indirectFilePtrCurrent {
	return pf.indirectFilePtrCurrent
}

func (pf indirectFilePtrFuture) toCurrentStruct() currentStruct {
	return pf.toCurrent()
}

func makeFakeIndirectFilePtrFuture(t *testing.T) indirectFilePtrFuture {
	return indirectFilePtrFuture{
		indirectFilePtrCurrent{
			makeFakeBlockInfo(t),
			25,
			false,
			codec.UnknownFieldSetHandler{},
		},
		makeExtraOrBust("IndirectFilePtr", t),
	}
}

func TestIndirectFilePtrUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeIndirectFilePtrFuture(t))
}

type dirBlockCurrent IFCERFTDirBlock

type dirBlockFuture struct {
	dirBlockCurrent
	// Overrides dirBlockCurrent.Children.
	Children map[string]dirEntryFuture `codec:"c,omitempty"`
	// Overrides dirBlockCurrent.IPtrs.
	IPtrs []indirectDirPtrFuture `codec:"i,omitempty"`
	extra
}

func (dbf dirBlockFuture) toCurrent() dirBlockCurrent {
	db := dbf.dirBlockCurrent
	db.Children = make(map[string]IFCERFTDirEntry, len(dbf.Children))
	for k, v := range dbf.Children {
		db.Children[k] = IFCERFTDirEntry(v.toCurrent())
	}
	db.IPtrs = make([]IFCERFTIndirectDirPtr, len(dbf.IPtrs))
	for i, v := range dbf.IPtrs {
		db.IPtrs[i] = IFCERFTIndirectDirPtr(v.toCurrent())
	}
	return db
}

func (dbf dirBlockFuture) toCurrentStruct() currentStruct {
	return dbf.toCurrent()
}

func makeFakeDirBlockFuture(t *testing.T) dirBlockFuture {
	return dirBlockFuture{
		dirBlockCurrent{
			IFCERFTCommonBlock{
				true,
				codec.UnknownFieldSetHandler{},
				0,
			},
			nil,
			nil,
		},
		map[string]dirEntryFuture{
			"child1": makeFakeDirEntryFuture(t),
		},
		[]indirectDirPtrFuture{
			makeFakeIndirectDirPtrFuture(t),
		},
		makeExtraOrBust("DirBlock", t),
	}
}

func TestDirBlockUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeDirBlockFuture(t))
}

type fileBlockCurrent IFCERFTFileBlock

type fileBlockFuture struct {
	fileBlockCurrent
	// Overrides fileBlockCurrent.IPtrs.
	IPtrs []indirectFilePtrFuture `codec:"i,omitempty"`
	extra
}

func (fbf fileBlockFuture) toCurrent() fileBlockCurrent {
	fb := fbf.fileBlockCurrent
	fb.IPtrs = make([]IFCERFTIndirectFilePtr, len(fbf.IPtrs))
	for i, v := range fbf.IPtrs {
		fb.IPtrs[i] = IFCERFTIndirectFilePtr(v.toCurrent())
	}
	return fb
}

func (fbf fileBlockFuture) toCurrentStruct() currentStruct {
	return fbf.toCurrent()
}

func makeFakeFileBlockFuture(t *testing.T) fileBlockFuture {
	return fileBlockFuture{
		fileBlockCurrent{
			IFCERFTCommonBlock{
				false,
				codec.UnknownFieldSetHandler{},
				0,
			},
			[]byte{0xa, 0xb},
			nil,
			nil,
		},
		[]indirectFilePtrFuture{
			makeFakeIndirectFilePtrFuture(t),
		},
		makeExtraOrBust("FileBlock", t),
	}
}

func TestFileBlockUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeFileBlockFuture(t))
}
