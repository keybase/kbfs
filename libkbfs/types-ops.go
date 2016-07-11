// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

// op represents a single file-system remote-sync operation
type IFCERFTOps interface {
	AddRefBlock(ptr IFCERFTBlockPointer)
	AddUnrefBlock(ptr IFCERFTBlockPointer)
	AddUpdate(oldPtr IFCERFTBlockPointer, newPtr IFCERFTBlockPointer)
	SizeExceptUpdates() uint64
	AllUpdates() []IFCERFTBlockUpdate
	Refs() []IFCERFTBlockPointer
	Unrefs() []IFCERFTBlockPointer
	String() string
	SetWriterInfo(IFCERFTWriterInfo)
	GetWriterInfo() IFCERFTWriterInfo
	SetFinalPath(p IFCERFTPath)
	GetFinalPath() IFCERFTPath
	// CheckConflict compares the function's target op with the given
	// op, and returns a resolution if one is needed (or nil
	// otherwise).  The resulting action (if any) assumes that this
	// method's target op is the unmerged op, and the given op is the
	// merged op.
	CheckConflict(renamer IFCERFTConflictRenamer, mergedOp IFCERFTOps, isFile bool) (
		crAction, error)
	// GetDefaultAction should be called on an unmerged op only after
	// all conflicts with the corresponding change have been checked,
	// and it returns the action to take against the merged branch
	// given that there are no conflicts.
	GetDefaultAction(mergedPath IFCERFTPath) crAction
}

// op codes
const (
	IFCERFTCreateOpCode IFCERFTExtCode = iota + IFCERFTExtCodeOpsRangeStart
	IFCERFTRmOpCode
	IFCERFTRenameOpCode
	IFCERFTSyncOpCode
	IFCERFTSetAttrOpCode
	IFCERFTResolutionOpCode
	IFCERFTRekeyOpCode
	IFCERFTGcOpCode // for deleting old blocks during an MD history truncation
)

// blockUpdate represents a block that was updated to have a new
// BlockPointer.
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them.
type IFCERFTBlockUpdate struct {
	Unref IFCERFTBlockPointer `codec:"u,omitempty"`
	Ref   IFCERFTBlockPointer `codec:"r,omitempty"`
}

type IFCERFTOpsList []IFCERFTOps
