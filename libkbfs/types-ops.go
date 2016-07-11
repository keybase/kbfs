// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/keybase/go-codec/codec"

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
		IFCERFTCrAction, error)
	// GetDefaultAction should be called on an unmerged op only after
	// all conflicts with the corresponding change have been checked,
	// and it returns the action to take against the merged branch
	// given that there are no conflicts.
	GetDefaultAction(mergedPath IFCERFTPath) IFCERFTCrAction
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

// WriteRange represents a file modification.  Len is 0 for a
// truncate.
type IFCERFTWriteRange struct {
	Off uint64 `codec:"o"`
	Len uint64 `codec:"l,omitempty"` // 0 for truncates

	codec.UnknownFieldSetHandler
}

func (w IFCERFTWriteRange) IsTruncate() bool {
	return w.Len == 0
}

// End returns the index of the largest byte not affected by this
// write.  It only makes sense to call this for non-truncates.
func (w IFCERFTWriteRange) End() uint64 {
	if w.IsTruncate() {
		panic("Truncates don't have an end")
	}
	return w.Off + w.Len
}

// Affects returns true if the regions affected by this write
// operation and `other` overlap in some way.  Specifically, it
// returns true if:
//
// - both operations are writes and their write ranges overlap;
// - one operation is a write and one is a truncate, and the truncate is
//   within the write's range or before it; or
// - both operations are truncates.
func (w IFCERFTWriteRange) Affects(other IFCERFTWriteRange) bool {
	if w.IsTruncate() {
		if other.IsTruncate() {
			return true
		}
		// A truncate affects a write if it lands inside or before the
		// write.
		return other.End() > w.Off
	} else if other.IsTruncate() {
		return w.End() > other.Off
	}
	// Both are writes -- do their ranges overlap?
	return (w.Off <= other.End() && other.End() <= w.End()) ||
		(other.Off <= w.End() && w.End() <= other.End())
}
