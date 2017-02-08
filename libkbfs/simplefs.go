// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/protocol/keybase1"
)

// Begin list of items in directory at path
// Retrieve results with readList()
// Can be a single file to get flags/status
func (k *KeybaseServiceBase) SimpleFSList(_ context.Context, arg keybase1.SimpleFSListArg) error {
	return errors.New("not implemented")
}

// Begin recursive list of items in directory at path
func (k *KeybaseServiceBase) SimpleFSListRecursive(_ context.Context, arg keybase1.SimpleFSListRecursiveArg) error {
	return errors.New("not implemented")
}

// Get list of Paths in progress. Can indicate status of pending
// to get more entries.
func (k *KeybaseServiceBase) SimpleFSReadList(context.Context, keybase1.OpID) (keybase1.SfListResult, error) {
	return keybase1.SfListResult{}, errors.New("not implemented")
}

// Begin copy of file or directory
func (k *KeybaseServiceBase) SimpleFSCopy(_ context.Context, arg keybase1.SimpleFSCopyArg) error {
	return errors.New("not implemented")
}

// Begin recursive copy of directory
func (k *KeybaseServiceBase) SimpleFSCopyRecursive(_ context.Context, arg keybase1.SimpleFSCopyRecursiveArg) error {
	return errors.New("not implemented")
}

// Begin move of file or directory, from/to KBFS only
func (k *KeybaseServiceBase) SimpleFSMove(_ context.Context, arg keybase1.SimpleFSMoveArg) error {
	return errors.New("not implemented")
}

// Rename file or directory, KBFS side only
func (k *KeybaseServiceBase) SimpleFSRename(_ context.Context, arg keybase1.SimpleFSRenameArg) error {
	return errors.New("not implemented")
}

// Create/open a file and leave it open
// or create a directory
// Files must be closed afterwards.
func (k *KeybaseServiceBase) SimpleFSOpen(_ context.Context, arg keybase1.SimpleFSOpenArg) error {
	return errors.New("not implemented")
}

// Set/clear file bits - only executable for now
func (k *KeybaseServiceBase) SimpleFSSetStat(_ context.Context, arg keybase1.SimpleFSSetStatArg) error {
	return errors.New("not implemented")
}

// Read (possibly partial) contents of open file,
// up to the amount specified by size.
// Repeat until zero bytes are returned or error.
// If size is zero, read an arbitrary amount.
func (k *KeybaseServiceBase) SimpleFSRead(_ context.Context, arg keybase1.SimpleFSReadArg) (keybase1.FileContent, error) {
	return keybase1.FileContent{}, errors.New("not implemented")
}

// Append content to opened file.
// May be repeated until OpID is closed.
func (k *KeybaseServiceBase) SimpleFSWrite(_ context.Context, arg keybase1.SimpleFSWriteArg) error {
	return errors.New("not implemented")
}

// Remove file or directory from filesystem
func (k *KeybaseServiceBase) SimpleFSRemove(_ context.Context, arg keybase1.SimpleFSRemoveArg) error {
	return errors.New("not implemented")
}

// Get info about file
func (k *KeybaseServiceBase) SimpleFSStat(_ context.Context, path keybase1.Path) (keybase1.Dirent, error) {
	return keybase1.Dirent{}, errors.New("not implemented")
}

// Convenience helper for generating new random value
func (k *KeybaseServiceBase) SimpleFSMakeOpid(_ context.Context) (keybase1.OpID, error) {
	return keybase1.OpID{}, errors.New("not implemented")
}

// Close OpID, cancels any pending operation.
// Must be called after list/copy/remove
func (k *KeybaseServiceBase) SimpleFSClose(_ context.Context, opid keybase1.OpID) error {
	return errors.New("not implemented")
}

// Check progress of pending operation
func (k *KeybaseServiceBase) SimpleFSCheck(_ context.Context, opid keybase1.OpID) (keybase1.Progress, error) {
	return 0, errors.New("not implemented")
}

// Get all the outstanding operations
func (k *KeybaseServiceBase) SimpleFSGetOps(_ context.Context) ([]keybase1.OpDescription, error) {
	return []keybase1.OpDescription{}, errors.New("not implemented")
}

// Blocking wait for the pending operation to finish
func (k *KeybaseServiceBase) SimpleFSWait(_ context.Context, opid keybase1.OpID) error {
	return errors.New("not implemented")
}
