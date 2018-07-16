// Copyright 2018 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libgit

import (
	"context"
	"path"
	"sync"
	"time"

	"github.com/keybase/kbfs/libkbfs"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
)

// This file contains libkbfs.Node wrappers for implementing the
// .kbfs_autogit directory structure. It breaks down like this:
//
// * `rootWrapper.wrap()` is installed as a root node wrapper, and wraps
//   the root node for each TLF in a `rootNode` instance.
// * `rootNode` allows .kbfs_autogit to be auto-created when it is
//   looked up, and wraps it two ways, as both a `libkbfs.ReadonlyNode`, and
//   an `autogitRootNode`.
// * `autogitRootNode` allows the auto-creation of subdirectories
//   representing TLF types, e.g. .kbfs_autogit/private or
//   .kbfs_autogit/public.  It wraps child nodes two ways, as both a
//   `libkbfs.ReadonlyNode`, and an `tlfTypeNode`.
// * `tlfTypeNode` allows the auto-creation of subdirectories
//   representing TLFs, e.g. .kbfs_autogit/private/max or
//   .kbfs_autogit/team/keybase.  It wraps child nodes in two ways, as
//   a `libkbfs.ReadonlyNode` and a `tlfNode`.
// * `tlfNode` allows auto-creation of subdirectories representing
//   valid repository checkouts of the corresponding TLF, e.g.
//   `.kbfs_autogit/private/chris/dotfiles`.  It wraps child nodes in
//   two ways, as both a `libkbfs.ReadonlyNode` and a `repoNode`.  It allows
//   repo directories to be removed via `RemoveDir` if the caller has
//   write permissions to the TLF where the autogit resides.
// * `repoNode` allow auto-clone and auto-pull of the corresponding
//   repository on its first access.  When the directory corresponding
//   to the node is read for the first time for this KBFS instance,
//   the `repoNode` asks `AutogitManager` to either kick off a clone
//   or a pull for the repository in question, which will checkout a
//   copy of the source repo under that directory.  The operation will
//   block on that clone/pull operation until it is finished, until
//   the given context is canceled, or until 10 seconds is up.  But if
//   it doesn't finish in time, the operation continues in the
//   background and should update the directory asynchronously.  It
//   registers with the `AutogitManager` in order to keep the checkout
//   up-to-date asynchronously if the repo changes.  If the operation
//   is a clone, a "CLONING" file will be visible in the directory
//   until the clone completes.  `repoNode` wraps each child node as a
//   `libkbfs.ReadonlyNode`.

type ctxReadWriteKeyType int
type ctxSkipPopulateKeyType int

const (
	autogitRoot = ".kbfs_autogit"

	autogitWrapTimeout = 10 * time.Second

	ctxSkipPopulateKey ctxSkipPopulateKeyType = 1

	public  = "public"
	private = "private"
	team    = "team"
)

type repoNode struct {
	libkbfs.Node
	am            *AutogitManager
	srcRepoHandle *libkbfs.TlfHandle
	repoName      string

	lock                 sync.Mutex
	populated            bool
	populatingInProgress chan struct{}
}

var _ libkbfs.Node = (*repoNode)(nil)

func newRepoNode(
	n libkbfs.Node, am *AutogitManager, srcRepoHandle *libkbfs.TlfHandle,
	repoName string) *repoNode {
	rn := &repoNode{
		Node:          n,
		am:            am,
		srcRepoHandle: srcRepoHandle,
		repoName:      repoName,
	}
	// We can't rely on a particular repo node being passed back into
	// libkbfs by callers, since they may not keep a reference to it
	// after looking it up, and `WrapChild` makes a new `repoNode` for
	// each call to it, even for the same underlying NodeID.  So we
	// keep the populated state in the AutogitManager.
	rn.populated = am.isRepoNodePopulated(rn)
	return rn
}

// AutogitTLFListDir returns `.kbfs_autogit/<tlf_type>` where <tlf_type> is
// "private", "public", or "team" depending on tlfType.
func AutogitTLFListDir(tlfType tlf.Type) string {
	var typeStr string
	switch tlfType {
	case tlf.Public:
		typeStr = public
	case tlf.Private:
		typeStr = private
	case tlf.SingleTeam:
		typeStr = team
	}
	return path.Join(autogitRoot, typeStr)
}

func autogitDstDir(h *libkbfs.TlfHandle) string {
	return path.Join(AutogitTLFListDir(h.Type()), string(h.GetCanonicalName()))
}

func (rn *repoNode) dstDir() string {
	return autogitDstDir(rn.srcRepoHandle)
}

func (rn *repoNode) populate(ctx context.Context) bool {
	ctx = context.WithValue(ctx, ctxSkipPopulateKey, 1)
	children, err := rn.am.config.KBFSOps().GetDirChildren(ctx, rn)
	if err != nil {
		rn.am.log.CDebugf(ctx, "Error getting children: %+v", err)
		return false
	}

	h, err := rn.am.config.KBFSOps().GetTLFHandle(ctx, rn)
	if err != nil {
		rn.am.log.CDebugf(ctx, "Error getting handle: %+v", err)
		return false
	}

	// Associate this autogit repo node with the node corresponding to
	// the top-level of the source repo.  This means it will detect
	// changes more often then necessary (e.g., when a different
	// branch is updated, or when just objects are updated), but we
	// can't just depend on the branch reference file because a
	// particular branch could be defined in packed-refs as well, and
	// that could change during the lifetime of the repo.
	srcRepoFS, _, err := GetRepoAndID(
		ctx, rn.am.config, rn.srcRepoHandle, rn.repoName, "")
	if err != nil {
		rn.am.log.CDebugf(ctx, "Couldn't get repo: %+v", err)
		return false
	}
	rn.am.registerRepoNode(srcRepoFS.RootNode(), rn)

	// If the directory is empty, clone it.  Otherwise, pull it.
	var doneCh <-chan struct{}
	cloneNeeded := len(children) == 0
	ctx = context.WithValue(ctx, libkbfs.CtxReadWriteKey, struct{}{})
	branch := "master"
	if cloneNeeded {
		doneCh, err = rn.am.Clone(
			ctx, rn.srcRepoHandle, rn.repoName, branch, h, rn.dstDir())
	} else {
		doneCh, err = rn.am.Pull(
			ctx, rn.srcRepoHandle, rn.repoName, branch, h, rn.dstDir())
	}
	if err != nil {
		rn.am.log.CDebugf(ctx, "Error starting population: %+v", err)
		return false
	}

	select {
	case <-doneCh:
		return true
	case <-ctx.Done():
		rn.am.log.CDebugf(ctx, "Error waiting for population: %+v", ctx.Err())
		// If we did a clone, ask for a refresh anyway, so they will
		// see the CLONING file at least.  The clone operation will
		// continue on in the background, so it's ok to consider this
		// node `populated`.
		return cloneNeeded
	}
}

func (rn *repoNode) shouldPopulate() (bool, <-chan struct{}) {
	rn.lock.Lock()
	defer rn.lock.Unlock()
	if rn.populated {
		return false, nil
	}
	if rn.populatingInProgress != nil {
		return false, rn.populatingInProgress
	}
	rn.populatingInProgress = make(chan struct{})
	return true, rn.populatingInProgress
}

func (rn *repoNode) finishPopulate(populated bool) {
	rn.lock.Lock()
	defer rn.lock.Unlock()
	rn.populated = populated
	close(rn.populatingInProgress)
	rn.populatingInProgress = nil
	rn.am.populateDone(rn)
}

func (rn *repoNode) updated(ctx context.Context) {
	h, err := rn.am.config.KBFSOps().GetTLFHandle(ctx, rn)
	if err != nil {
		rn.am.log.CDebugf(ctx, "Error getting handle: %+v", err)
		return
	}

	dstDir := rn.dstDir()
	rn.am.log.CDebugf(
		ctx, "Repo %s/%s/%s updated", h.GetCanonicalPath(), dstDir, rn.repoName)
	_, err = rn.am.Pull(ctx, rn.srcRepoHandle, rn.repoName, "master", h, dstDir)
	if err != nil {
		rn.am.log.CDebugf(ctx, "Error calling pull: %+v", err)
		return
	}
}

// ShouldRetryOnDirRead implements the Node interface for
// repoNode.
func (rn *repoNode) ShouldRetryOnDirRead(ctx context.Context) (
	shouldRetry bool) {
	if ctx.Value(ctxSkipPopulateKey) != nil {
		return false
	}

	// Don't let this operation take more than a fixed amount of time.
	// We should just let the caller see the CLONING file if it takes
	// too long.
	ctx, cancel := context.WithTimeout(ctx, autogitWrapTimeout)
	defer cancel()

	for {
		doPopulate, ch := rn.shouldPopulate()
		if ch == nil {
			return shouldRetry
		}
		// If it wasn't populated on the first check, always force the
		// caller to retry.
		shouldRetry = true

		if doPopulate {
			rn.am.log.CDebugf(ctx, "Populating repo node on first access")
			shouldRetry = rn.populate(ctx)
			rn.finishPopulate(shouldRetry)
			return shouldRetry
		}

		// Wait for the existing populate to succeed or fail.
		rn.am.log.CDebugf(ctx, "Waiting for existing populate to finish")
		select {
		case <-ch:
		case <-ctx.Done():
			rn.am.log.CDebugf(ctx, "Error waiting for populate: %+v", ctx.Err())
			return false
		}
	}
}

type tlfNode struct {
	libkbfs.Node
	am *AutogitManager
	h  *libkbfs.TlfHandle
}

var _ libkbfs.Node = (*tlfNode)(nil)

// ShouldCreateMissedLookup implements the Node interface for
// tlfNode.
func (tn tlfNode) ShouldCreateMissedLookup(
	ctx context.Context, name string) (
	bool, context.Context, libkbfs.EntryType, string) {
	normalizedRepoName := normalizeRepoName(name)

	// Is this a legit repo?
	_, _, err := GetRepoAndID(ctx, tn.am.config, tn.h, name, "")
	if err != nil {
		return false, ctx, libkbfs.File, ""
	}

	ctx = context.WithValue(ctx, libkbfs.CtxReadWriteKey, struct{}{})
	if name != normalizedRepoName {
		return true, ctx, libkbfs.Sym, normalizedRepoName
	}
	return true, ctx, libkbfs.Dir, ""
}

// RemoveDir implements the Node interface for tlfNode.
func (tn tlfNode) RemoveDir(ctx context.Context, name string) (
	removeHandled bool, err error) {
	// Is this a legit repo?
	_, _, err = GetRepoAndID(ctx, tn.am.config, tn.h, name, "")
	if err != nil {
		return false, err
	}

	if normalizeRepoName(name) != name {
		// Can't remove a symlink as if it were a directory.
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, autogitWrapTimeout)
	defer cancel()

	h, err := tn.am.config.KBFSOps().GetTLFHandle(ctx, tn)
	if err != nil {
		return false, err
	}
	doneCh, err := tn.am.Delete(ctx, h, autogitDstDir(tn.h), name, "master")
	if err != nil {
		return false, err
	}

	select {
	case <-doneCh:
	case <-ctx.Done():
		// The delete will continue in the background.
		tn.am.log.CDebugf(ctx,
			"Timeout waiting for background delete: %+v", ctx.Err())
	}
	return true, nil
}

// WrapChild implements the Node interface for tlfNode.
func (tn tlfNode) WrapChild(child libkbfs.Node) libkbfs.Node {
	child = tn.Node.WrapChild(child)
	return newRepoNode(child, tn.am, tn.h, child.GetBasename())
}

// tlfTypeNode represents an autogit subdirectory corresponding to a
// specific TLF type.  It can only contain subdirectories that
// correspond to valid TLF name for the TLF type.
type tlfTypeNode struct {
	libkbfs.Node
	am      *AutogitManager
	tlfType tlf.Type
}

var _ libkbfs.Node = (*tlfTypeNode)(nil)

// ShouldCreateMissedLookup implements the Node interface for
// tlfTypeNode.
func (ttn tlfTypeNode) ShouldCreateMissedLookup(
	ctx context.Context, name string) (
	bool, context.Context, libkbfs.EntryType, string) {
	_, err := libkbfs.ParseTlfHandle(
		ctx, ttn.am.config.KBPKI(), ttn.am.config.MDOps(), name, ttn.tlfType)

	ctx = context.WithValue(ctx, libkbfs.CtxReadWriteKey, struct{}{})
	switch e := errors.Cause(err).(type) {
	case nil:
		return true, ctx, libkbfs.Dir, ""
	case libkbfs.TlfNameNotCanonical:
		return true, ctx, libkbfs.Sym, e.NameToTry
	default:
		ttn.am.log.CDebugf(ctx,
			"Error parsing handle for name %s: %+v", name, err)
		return ttn.Node.ShouldCreateMissedLookup(ctx, name)
	}
}

// WrapChild implements the Node interface for tlfTypeNode.
func (ttn tlfTypeNode) WrapChild(child libkbfs.Node) libkbfs.Node {
	child = ttn.Node.WrapChild(child)
	ctx, cancel := context.WithTimeout(context.Background(), autogitWrapTimeout)
	defer cancel()
	h, err := libkbfs.ParseTlfHandle(
		ctx, ttn.am.config.KBPKI(), ttn.am.config.MDOps(),
		child.GetBasename(), ttn.tlfType)
	if err != nil {
		// If we have a node for the child already, it can't be
		// non-canonical because symlinks don't have Nodes.
		ttn.am.log.CDebugf(ctx,
			"Error parsing handle for tlfTypeNode child: %+v", err)
		return child
	}

	return &tlfNode{child, ttn.am, h}
}

// autogitRootNode represents the .kbfs_autogit folder, and can only
// contain subdirectories corresponding to TLF types.
type autogitRootNode struct {
	libkbfs.Node
	am *AutogitManager
}

var _ libkbfs.Node = (*autogitRootNode)(nil)

// ShouldCreateMissedLookup implements the Node interface for
// autogitRootNode.
func (arn autogitRootNode) ShouldCreateMissedLookup(
	ctx context.Context, name string) (
	bool, context.Context, libkbfs.EntryType, string) {
	switch name {
	case public, private, team:
		ctx = context.WithValue(ctx, libkbfs.CtxReadWriteKey, struct{}{})
		return true, ctx, libkbfs.Dir, ""
	default:
		return arn.Node.ShouldCreateMissedLookup(ctx, name)
	}
}

// WrapChild implements the Node interface for autogitRootNode.
func (arn autogitRootNode) WrapChild(child libkbfs.Node) libkbfs.Node {
	child = arn.Node.WrapChild(child)
	var tlfType tlf.Type
	switch child.GetBasename() {
	case public:
		tlfType = tlf.Public
	case private:
		tlfType = tlf.Private
	case team:
		tlfType = tlf.SingleTeam
	default:
		return child
	}
	return &tlfTypeNode{
		Node:    child,
		am:      arn.am,
		tlfType: tlfType,
	}
}

// rootNode is a Node wrapper around a TLF root node, that causes the
// autogit root to be created when it is accessed.
type rootNode struct {
	libkbfs.Node
	am *AutogitManager
}

var _ libkbfs.Node = (*rootNode)(nil)

// ShouldCreateMissedLookup implements the Node interface for
// rootNode.
func (rn rootNode) ShouldCreateMissedLookup(ctx context.Context, name string) (
	bool, context.Context, libkbfs.EntryType, string) {
	if name == autogitRoot {
		ctx = context.WithValue(ctx, libkbfs.CtxReadWriteKey, struct{}{})
		ctx = context.WithValue(ctx, libkbfs.CtxAllowNameKey, autogitRoot)
		return true, ctx, libkbfs.Dir, ""
	}
	return rn.Node.ShouldCreateMissedLookup(ctx, name)
}

// WrapChild implements the Node interface for rootNode.
func (rn rootNode) WrapChild(child libkbfs.Node) libkbfs.Node {
	child = rn.Node.WrapChild(child)
	if child.GetBasename() == autogitRoot {
		return &autogitRootNode{
			Node: &libkbfs.ReadonlyNode{Node: child},
			am:   rn.am,
		}
	}
	return child
}

// rootWrapper is a struct that manages wrapping root nodes with
// autogit-related context.
type rootWrapper struct {
	am *AutogitManager
}

func (rw rootWrapper) wrap(node libkbfs.Node) libkbfs.Node {
	return &rootNode{node, rw.am}
}
