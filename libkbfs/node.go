// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "runtime"

// nodeCore holds info shared among one or more nodeStandard objects.
type nodeCore struct {
	pathNode *IFCERFTPathNode
	parent   *nodeStandard
	cache    *nodeCacheStandard
	// used only when parent is nil (the object has been unlinked)
	cachedPath IFCERFTPath
}

func newNodeCore(ptr IFCERFTBlockPointer, name string, parent *nodeStandard,
	cache *nodeCacheStandard) *nodeCore {
	return &nodeCore{
		pathNode: &IFCERFTPathNode{
			IFCERFTBlockPointer: ptr,
			Name:                name,
		},
		parent: parent,
		cache:  cache,
	}
}

func (c *nodeCore) ParentID() IFCERFTNodeID {
	if c.parent == nil {
		return nil
	}
	return c.parent.GetID()
}

type nodeStandard struct {
	core *nodeCore
}

var _ IFCERFTNode = (*nodeStandard)(nil)

func nodeStandardFinalizer(n *nodeStandard) {
	n.core.cache.forget(n.core)
}

func makeNodeStandard(core *nodeCore) *nodeStandard {
	n := &nodeStandard{core}
	runtime.SetFinalizer(n, nodeStandardFinalizer)
	return n
}

func (n *nodeStandard) GetID() IFCERFTNodeID {
	return n.core
}

func (n *nodeStandard) GetFolderBranch() IFCERFTFolderBranch {
	return n.core.cache.folderBranch
}

func (n *nodeStandard) GetBasename() string {
	if len(n.core.cachedPath.path) > 0 {
		// Must be unlinked.
		return ""
	}
	return n.core.pathNode.Name
}
