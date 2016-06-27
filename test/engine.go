// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package test

import (
	"testing"
	"time"

	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/keybase/kbfs/libkbfs"
)

// User is an implementation-defined object which acts as a handle to a particular user.
type User interface{}

// Node is an implementation-defined object which acts as a handle to a particular filesystem node.
type Node interface{}

type username string

// Engine is the interface to the filesystem to be used by the test harness.
// It may wrap libkbfs directly or it may wrap other users of libkbfs (e.g., libfuse).
type Engine interface {
	// Name returns the name of the engine.
	Name() string
	// Init is called by the test harness once prior to using a KBFS interface implementation.
	Init()
	// InitTest is called by the test harness to initialize user
	// instances and set up the configuration of the test.
	// blockChange indicates the maximum size of each data block.
	// blockChangeSize indicates the maximum size the list of block
	// changes can be in each MD update, before it is written to a
	// dedicated data block instead. If blockSize or blockChangeSize
	// are zero, the engine defaults are used.
	InitTest(t *testing.T, blockSize int64, blockChangeSize int64,
		users []libkb.NormalizedUsername,
		clock libkbfs.Clock) map[libkb.NormalizedUsername]User
	// GetUID is called by the test harness to retrieve a user instance's UID.
	GetUID(u User) keybase1.UID
	// GetFavorites returns the set of all public or private
	// favorites, based on the given bool.
	GetFavorites(u User, public bool) (map[string]bool, error)
	// GetRootDir is called by the test harness to get a handle to a TLF from the given user's
	// perspective
	GetRootDir(u User, tlfName string, isPublic bool, expectedCanonicalTlfName string) (dir Node, err error)
	// CreateDir is called by the test harness to create a directory relative to the passed
	// parent directory for the given user.
	CreateDir(u User, parentDir Node, name string) (dir Node, err error)
	// CreateFile is called by the test harness to create a file in the given directory as
	// the given user.
	CreateFile(u User, parentDir Node, name string) (file Node, err error)
	// CreateLink is called by the test harness to create a symlink in the given directory as
	// the given user.
	CreateLink(u User, parentDir Node, fromName string, toPath string) (err error)
	// WriteFile is called by the test harness to write to the given file as the given user.
	WriteFile(u User, file Node, data string, off int64, sync bool) (err error)
	// TruncateFile is called by the test harness to truncate the given file as the given user, to the given size.
	TruncateFile(u User, file Node, size uint64, sync bool) (err error)
	// RemoveDir is called by the test harness as the given user to remove a subdirectory.
	RemoveDir(u User, dir Node, name string) (err error)
	// RemoveEntry is called by the test harness as the given user to remove a directory entry.
	RemoveEntry(u User, dir Node, name string) (err error)
	// Rename is called by the test harness as the given user to rename a node.
	Rename(u User, srcDir Node, srcName string, dstDir Node, dstName string) (err error)
	// ReadFile is called by the test harness to read from the given file as the given user.
	ReadFile(u User, file Node, off, len int64) (data string, err error)
	// Lookup is called by the test harness to return a node in the given directory by
	// its name for the given user. In the case of a symlink the symPath will be set and
	// the node will be nil.
	Lookup(u User, parentDir Node, name string) (file Node, symPath string, err error)
	// GetDirChildrenTypes is called by the test harness as the given user to return a map of child nodes
	// and their type names.
	GetDirChildrenTypes(u User, parentDir Node) (children map[string]string, err error)
	// SetEx is called by the test harness as the given user to set/unset the executable bit on the
	// given file.
	SetEx(u User, file Node, ex bool) (err error)
	// SetMtime is called by the test harness as the given user to
	// set the mtime on the given file.
	SetMtime(u User, file Node, mtime time.Time) (err error)
	// GetMtime is called by the test harness as the given user to get
	// the mtime of the given file.
	GetMtime(u User, file Node) (mtime time.Time, err error)

	// All functions below don't take nodes so that they can be
	// run before any real FS operations.

	// DisableUpdatesForTesting is called by the test harness as
	// the given user to disable updates to trigger conflict
	// conditions.
	DisableUpdatesForTesting(u User, tlfName string, isPublic bool) (err error)
	// ReenableUpdates is called by the test harness as the given
	// user to resume updates if previously disabled for testing.
	ReenableUpdates(u User, tlfName string, isPublic bool) (err error)
	// SyncFromServerForTesting is called by the test harness as
	// the given user to actively retrieve new metadata for a
	// folder.
	SyncFromServerForTesting(u User, tlfName string, isPublic bool) (err error)
	// ForceQuotaReclamation starts quota reclamation by the given
	// user in the TLF corresponding to the given node.
	ForceQuotaReclamation(u User, tlfName string, isPublic bool) (err error)
	// AddNewAssertion makes newAssertion, which should be a
	// single assertion that doesn't already resolve to anything,
	// resolve to the same UID as oldAssertion, which should be an
	// arbitrary assertion that does already resolve to something.
	// It only applies to the given user.
	AddNewAssertion(u User, oldAssertion, newAssertion string) (err error)
	// Rekey rekeys the given TLF under the given user.
	Rekey(u User, tlfName string, isPublic bool) (err error)
	// Shutdown is called by the test harness when it is done with the
	// given user.
	Shutdown(u User) error
}
