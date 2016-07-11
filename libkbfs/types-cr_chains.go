// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"

	"golang.org/x/net/context"
)

// crChain represents the set of operations that happened to a
// particular KBFS node (e.g., individual file or directory) over a
// given set of MD updates.  It also tracks the starting and ending
// block pointers for the node.
type IFCERFTCrChain struct {
	ops                  []IFCERFTOps
	original, mostRecent IFCERFTBlockPointer
	file                 bool
}

// collapse finds complementary pairs of operations that cancel each
// other out, and remove the relevant operations from the chain.
// Examples include:
//  * A create followed by a remove for the same name (delete both ops)
//  * A create followed by a create (renamed == true) for the same name
//    (delete the create op)
func (cc *IFCERFTCrChain) Collapse() {
	createsSeen := make(map[string]int)
	indicesToRemove := make(map[int]bool)
	for i, op := range cc.ops {
		switch realOp := op.(type) {
		case *createOp:
			if prevCreateIndex, ok :=
				createsSeen[realOp.NewName]; realOp.renamed && ok {
				// A rename has papered over the first create, so
				// just drop it.
				indicesToRemove[prevCreateIndex] = true
			}
			createsSeen[realOp.NewName] = i
		case *rmOp:
			if prevCreateIndex, ok := createsSeen[realOp.OldName]; ok {
				delete(createsSeen, realOp.OldName)
				// The rm cancels out the create, so remove both.
				indicesToRemove[prevCreateIndex] = true
				indicesToRemove[i] = true
			}
		case *setAttrOp:
			// TODO: Collapse opposite setex pairs
		default:
			// ignore other op types
		}
	}

	if len(indicesToRemove) > 0 {
		ops := make([]IFCERFTOps, 0, len(cc.ops)-len(indicesToRemove))
		for i, op := range cc.ops {
			if !indicesToRemove[i] {
				ops = append(ops, op)
			}
		}
		cc.ops = ops
	}
}

func (cc *IFCERFTCrChain) GetCollapsedWriteRange() []IFCERFTWriteRange {
	if !cc.IsFile() {
		return nil
	}
	var wr []IFCERFTWriteRange
	for _, op := range cc.ops {
		syncOp, ok := op.(*syncOp)
		if !ok {
			continue
		}
		wr = syncOp.collapseWriteRange(wr)
	}
	return wr
}

func (cc *IFCERFTCrChain) GetActionsToMerge(renamer IFCERFTConflictRenamer, mergedPath IFCERFTPath, mergedChain *IFCERFTCrChain) (crActionList, error) {
	var actions crActionList

	// If this is a file, determine whether the unmerged chain
	// could actually have changed the file in some way that it
	// hasn't already been changed.  For example, if they both
	// truncate the file to the same length, and there are no
	// other writes, we can just drop the unmerged syncs.
	toSkip := make(map[int]bool)
	if cc.IsFile() && mergedChain != nil {
		myWriteRange := cc.GetCollapsedWriteRange()
		mergedWriteRange := mergedChain.GetCollapsedWriteRange()

		// If both branches contain no writes, and their truncation
		// points are the same, then there are no unmerged actions to
		// take.
		//
		// TODO: In the future we may be able to do smarter merging
		// here if the write ranges don't overlap, though maybe only
		// for certain file types?
		if len(myWriteRange) == 1 && myWriteRange[0].IsTruncate() &&
			len(mergedWriteRange) == 1 && mergedWriteRange[0].IsTruncate() &&
			myWriteRange[0].Off == mergedWriteRange[0].Off {
			// drop all sync ops
			for i, op := range cc.ops {
				if _, ok := op.(*syncOp); ok {
					actions = append(actions, &dropUnmergedAction{op})
					toSkip[i] = true
				}
			}
		}
	}

	// Check each op against all ops in the corresponding merged
	// chain, looking for conflicts.  If there is a conflict, return
	// it as part of the action list.  If there are no conflicts for
	// that op, return the op's default actions.
	for i, unmergedOp := range cc.ops {
		if toSkip[i] {
			continue
		}
		conflict := false
		if mergedChain != nil {
			for _, mergedOp := range mergedChain.ops {
				action, err :=
					unmergedOp.CheckConflict(renamer, mergedOp, cc.IsFile())
				if err != nil {
					return nil, err
				}
				if action != nil {
					conflict = true
					actions = append(actions, action)
				}
			}
		}
		// no conflicts!
		if !conflict {
			actions = append(actions, unmergedOp.GetDefaultAction(mergedPath))
		}
	}

	return actions, nil
}

func (cc *IFCERFTCrChain) IsFile() bool {
	return cc.file
}

// identifyType figures out whether this chain represents a file or
// directory.  It tries to figure it out based purely on operation
// state, but setAttr(mtime) can apply to either type; in that case,
// we need to fetch the block to figure out the type.
func (cc *IFCERFTCrChain) IdentifyType(ctx context.Context, fbo *folderBlockOps,
	md *IFCERFTRootMetadata, chains *IFCERFTCrChains) error {
	if len(cc.ops) == 0 {
		return nil
	}

	// If any op is setAttr (ex or size) or sync, this is a file
	// chain.  If it only has a setAttr/mtime, we don't know what it
	// is, so fall through and fetch the block unless we come across
	// another op that can determine the type.
	var parentDir IFCERFTBlockPointer
	for _, op := range cc.ops {
		switch realOp := op.(type) {
		case *syncOp:
			cc.file = true
			return nil
		case *setAttrOp:
			if realOp.Attr != mtimeAttr {
				cc.file = true
				return nil
			}
			// We can't tell the file type from an mtimeAttr, so we
			// may have to actually fetch the block to figure it out.
			parentDir = realOp.Dir.Ref
		default:
			return nil
		}
	}

	parentOriginal, ok := chains.originals[parentDir]
	if !ok {
		return IFCERFTNoChainFoundError{parentDir}
	}

	// We have to find the current parent directory block.  If the
	// file has been renamed, that might be different from parentDir
	// above.
	if newParent, _, ok := chains.RenamedParentAndName(cc.original); ok {
		parentOriginal = newParent
	}

	parentMostRecent, err := chains.MostRecentFromOriginalOrSame(parentOriginal)
	if err != nil {
		return err
	}

	// If we get down here, we have an ambiguity, and need to fetch
	// the block to figure out the file type.
	dblock, err := fbo.GetDirBlockForReading(ctx, makeFBOLockState(), md,
		parentMostRecent, fbo.folderBranch.Branch, IFCERFTPath{})
	if err != nil {
		return err
	}
	// We don't have the file name handy, so search for the pointer.
	found := false
	for _, entry := range dblock.Children {
		if entry.IFCERFTBlockPointer != cc.mostRecent {
			continue
		}
		switch entry.Type {
		case IFCERFTDir:
			cc.file = false
		case IFCERFTFile:
			cc.file = true
		case IFCERFTExec:
			cc.file = true
		default:
			return fmt.Errorf("Unexpected chain type: %s", entry.Type)
		}
		found = true
		break
	}

	if !found {
		return fmt.Errorf("Couldn't find directory entry for %v", cc.mostRecent)
	}
	return nil
}

type IFCERFTRenameInfo struct {
	OriginalOldParent IFCERFTBlockPointer
	OldName           string
	OriginalNewParent IFCERFTBlockPointer
	NewName           string
}

func (ri IFCERFTRenameInfo) String() string {
	return fmt.Sprintf(
		"renameInfo{originalOldParent: %s, oldName: %s, originalNewParent: %s, newName: %s}",
		ri.OriginalOldParent, ri.OldName,
		ri.OriginalNewParent, ri.NewName)
}

// crChains contains a crChain for every KBFS node affected by the
// operations over a given set of MD updates.  The chains are indexed
// by both the starting (original) and ending (most recent) pointers.
// It also keeps track of which chain points to the root of the folder.
type IFCERFTCrChains struct {
	byOriginal   map[IFCERFTBlockPointer]*IFCERFTCrChain
	byMostRecent map[IFCERFTBlockPointer]*IFCERFTCrChain
	originalRoot IFCERFTBlockPointer

	// The original blockpointers for nodes that have been
	// unreferenced or initially referenced during this chain.
	deletedOriginals map[IFCERFTBlockPointer]bool
	createdOriginals map[IFCERFTBlockPointer]bool

	// A map from original blockpointer to the full rename operation
	// of the node (from the original location of the node to the
	// final locations).
	renamedOriginals map[IFCERFTBlockPointer]IFCERFTRenameInfo

	// Separately track pointers for unembedded block changes.
	blockChangePointers map[IFCERFTBlockPointer]bool

	// Pointers that should be explicitly cleaned up in the resolution.
	toUnrefPointers map[IFCERFTBlockPointer]bool

	// Also keep a reference to the most recent MD that's part of this
	// chain.
	mostRecentMD *IFCERFTRootMetadata

	// We need to be able to track ANY BlockPointer, at any point in
	// the chain, back to its original.
	originals map[IFCERFTBlockPointer]IFCERFTBlockPointer
}

func (ccs *IFCERFTCrChains) AddOp(ptr IFCERFTBlockPointer, op IFCERFTOps) error {
	currChain, ok := ccs.byMostRecent[ptr]
	if !ok {
		return fmt.Errorf("Could not find chain for most recent ptr %v", ptr)
	}

	currChain.ops = append(currChain.ops, op)
	return nil
}

func (ccs *IFCERFTCrChains) MakeChainForOp(op IFCERFTOps) error {
	// First set the pointers for all updates, and track what's been
	// created and destroyed.
	for _, update := range op.AllUpdates() {
		chain, ok := ccs.byMostRecent[update.Unref]
		if !ok {
			// No matching chain means it's time to start a new chain
			chain = &IFCERFTCrChain{original: update.Unref}
			ccs.byOriginal[update.Unref] = chain
		}
		if chain.mostRecent.IsInitialized() {
			// delete the old most recent pointer, it's no longer needed
			delete(ccs.byMostRecent, chain.mostRecent)
		}
		chain.mostRecent = update.Ref
		ccs.byMostRecent[update.Ref] = chain
		if chain.original != update.Ref {
			// Always be able to track this one back to its original.
			ccs.originals[update.Ref] = chain.original
		}
	}

	for _, ptr := range op.Refs() {
		ccs.createdOriginals[ptr] = true
	}

	for _, ptr := range op.Unrefs() {
		// Look up the original pointer corresponding to this most
		// recent one.
		original := ptr
		if ptrChain, ok := ccs.byMostRecent[ptr]; ok {
			original = ptrChain.original
		}

		ccs.deletedOriginals[original] = true
	}

	// then set the op depending on the actual op type
	switch realOp := op.(type) {
	default:
		panic(fmt.Sprintf("Unrecognized operation: %v", op))
	case *createOp:
		err := ccs.AddOp(realOp.Dir.Ref, op)
		if err != nil {
			return err
		}
	case *rmOp:
		err := ccs.AddOp(realOp.Dir.Ref, op)
		if err != nil {
			return err
		}
	case *renameOp:
		// split rename op into two separate operations, one for
		// remove and one for create
		ro := newRmOp(realOp.OldName, realOp.OldDir.Unref)
		ro.SetWriterInfo(realOp.GetWriterInfo())
		ro.Dir.Ref = realOp.OldDir.Ref
		err := ccs.AddOp(realOp.OldDir.Ref, ro)
		if err != nil {
			return err
		}

		ndu := realOp.NewDir.Unref
		ndr := realOp.NewDir.Ref
		if realOp.NewDir == (IFCERFTBlockUpdate{}) {
			// this is a rename within the same directory
			ndu = realOp.OldDir.Unref
			ndr = realOp.OldDir.Ref
		}

		if len(realOp.Unrefs()) > 0 {
			// Something was overwritten; make an explicit rm for it
			// so we can check for conflicts.
			roOverwrite := newRmOp(realOp.NewName, ndu)
			roOverwrite.SetWriterInfo(realOp.GetWriterInfo())
			roOverwrite.Dir.Ref = ndr
			err = ccs.AddOp(ndr, roOverwrite)
			if err != nil {
				return err
			}
			// Transfer any unrefs over.
			for _, ptr := range realOp.Unrefs() {
				roOverwrite.AddUnrefBlock(ptr)
			}
		}

		co := newCreateOp(realOp.NewName, ndu, realOp.RenamedType)
		co.SetWriterInfo(realOp.GetWriterInfo())
		co.renamed = true
		co.Dir.Ref = ndr
		err = ccs.AddOp(ndr, co)
		if err != nil {
			return err
		}

		// also keep track of the new parent for the renamed node
		if realOp.Renamed.IsInitialized() {
			newParentChain, ok := ccs.byMostRecent[ndr]
			if !ok {
				return fmt.Errorf("While renaming, couldn't find the chain "+
					"for the new parent %v", ndr)
			}
			oldParentChain, ok := ccs.byMostRecent[realOp.OldDir.Ref]
			if !ok {
				return fmt.Errorf("While renaming, couldn't find the chain "+
					"for the old parent %v", ndr)
			}

			renamedOriginal := realOp.Renamed
			if renamedChain, ok := ccs.byMostRecent[realOp.Renamed]; ok {
				renamedOriginal = renamedChain.original
			}
			// Use the previous old info if there is one already,
			// in case this node has been renamed multiple times.
			ri, ok := ccs.renamedOriginals[renamedOriginal]
			if !ok {
				// Otherwise make a new one.
				ri = IFCERFTRenameInfo{
					OriginalOldParent: oldParentChain.original,
					OldName:           realOp.OldName,
				}
			}
			ri.OriginalNewParent = newParentChain.original
			ri.NewName = realOp.NewName
			ccs.renamedOriginals[renamedOriginal] = ri
			// Remember what you create, in case we need to merge
			// directories after a rename.
			co.AddRefBlock(renamedOriginal)
		}
	case *syncOp:
		err := ccs.AddOp(realOp.File.Ref, op)
		if err != nil {
			return err
		}
	case *setAttrOp:
		// Because the attributes apply to the file, which doesn't
		// actually have an updated pointer, we may need to create a
		// new chain.
		_, ok := ccs.byMostRecent[realOp.File]
		if !ok {
			// pointer didn't change, so most recent is the same:
			chain := &IFCERFTCrChain{original: realOp.File, mostRecent: realOp.File}
			ccs.byOriginal[realOp.File] = chain
			ccs.byMostRecent[realOp.File] = chain
		}

		err := ccs.AddOp(realOp.File, op)
		if err != nil {
			return err
		}
	case *resolutionOp:
		// ignore resolution op
	case *rekeyOp:
		// ignore rekey op
	case *gcOp:
		// ignore gc op
	}

	return nil
}

func (ccs *IFCERFTCrChains) MakeChainForNewOpWithUpdate(
	targetPtr IFCERFTBlockPointer, newOp IFCERFTOps, update *IFCERFTBlockUpdate) error {
	oldUnref := update.Unref
	update.Unref = targetPtr
	update.Ref = update.Unref // so that most recent == original
	defer func() {
		// reset the update to its original state before returning.
		update.Unref = oldUnref
		update.Ref = IFCERFTBlockPointer{}
	}()
	err := ccs.MakeChainForOp(newOp)
	if err != nil {
		return err
	}
	return nil
}

// makeChainForNewOp makes a new chain for an op that does not yet
// have its pointers initialized.  It does so by setting Unref and Ref
// to be the same for the duration of this function, and calling the
// usual makeChainForOp method.  This function is not goroutine-safe
// with respect to newOp.  Also note that rename ops will not be split
// into two ops; they will be placed only in the new directory chain.
func (ccs *IFCERFTCrChains) MakeChainForNewOp(targetPtr IFCERFTBlockPointer, newOp IFCERFTOps) error {
	switch realOp := newOp.(type) {
	case *createOp:
		return ccs.MakeChainForNewOpWithUpdate(targetPtr, newOp, &realOp.Dir)
	case *rmOp:
		return ccs.MakeChainForNewOpWithUpdate(targetPtr, newOp, &realOp.Dir)
	case *renameOp:
		// In this case, we don't want to split the rename chain, so
		// just make up a new operation and later overwrite it with
		// the rename op.
		co := newCreateOp(realOp.NewName, realOp.NewDir.Unref, IFCERFTFile)
		err := ccs.MakeChainForNewOpWithUpdate(targetPtr, co, &co.Dir)
		if err != nil {
			return err
		}
		chain, ok := ccs.byMostRecent[targetPtr]
		if !ok {
			return fmt.Errorf("Couldn't find chain for %v after making it",
				targetPtr)
		}
		if len(chain.ops) != 1 {
			return fmt.Errorf("Chain of unexpected length for %v after "+
				"making it", targetPtr)
		}
		chain.ops[0] = realOp
		return nil
	case *setAttrOp:
		return ccs.MakeChainForNewOpWithUpdate(targetPtr, newOp, &realOp.Dir)
	case *syncOp:
		return ccs.MakeChainForNewOpWithUpdate(targetPtr, newOp, &realOp.File)
	default:
		return fmt.Errorf("Couldn't make chain with unknown operation %s",
			newOp)
	}
}

func (ccs *IFCERFTCrChains) MostRecentFromOriginal(original IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error) {
	chain, ok := ccs.byOriginal[original]
	if !ok {
		return IFCERFTBlockPointer{}, IFCERFTNoChainFoundError{original}
	}
	return chain.mostRecent, nil
}

func (ccs *IFCERFTCrChains) MostRecentFromOriginalOrSame(original IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error) {
	ptr, err := ccs.MostRecentFromOriginal(original)
	if err == nil {
		// A satisfactory chain was found.
		return ptr, nil
	} else if _, ok := err.(IFCERFTNoChainFoundError); !ok {
		// An unexpected error!
		return IFCERFTBlockPointer{}, err
	}
	return original, nil
}

func (ccs *IFCERFTCrChains) OriginalFromMostRecent(mostRecent IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error) {
	chain, ok := ccs.byMostRecent[mostRecent]
	if !ok {
		return IFCERFTBlockPointer{}, IFCERFTNoChainFoundError{mostRecent}
	}
	return chain.original, nil
}

func (ccs *IFCERFTCrChains) OriginalFromMostRecentOrSame(mostRecent IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error) {
	ptr, err := ccs.OriginalFromMostRecent(mostRecent)
	if err == nil {
		// A satisfactory chain was found.
		return ptr, nil
	} else if _, ok := err.(IFCERFTNoChainFoundError); !ok {
		// An unexpected error!
		return IFCERFTBlockPointer{}, err
	}
	return mostRecent, nil
}

func (ccs *IFCERFTCrChains) IsCreated(original IFCERFTBlockPointer) bool {
	return ccs.createdOriginals[original]
}

func (ccs *IFCERFTCrChains) IsDeleted(original IFCERFTBlockPointer) bool {
	return ccs.deletedOriginals[original]
}

func (ccs *IFCERFTCrChains) RenamedParentAndName(original IFCERFTBlockPointer) (
	IFCERFTBlockPointer, string, bool) {
	info, ok := ccs.renamedOriginals[original]
	if !ok {
		return IFCERFTBlockPointer{}, "", false
	}
	return info.OriginalNewParent, info.NewName, true
}

func newCRChainsEmpty() *IFCERFTCrChains {
	return &IFCERFTCrChains{
		byOriginal:          make(map[IFCERFTBlockPointer]*IFCERFTCrChain),
		byMostRecent:        make(map[IFCERFTBlockPointer]*IFCERFTCrChain),
		deletedOriginals:    make(map[IFCERFTBlockPointer]bool),
		createdOriginals:    make(map[IFCERFTBlockPointer]bool),
		renamedOriginals:    make(map[IFCERFTBlockPointer]IFCERFTRenameInfo),
		blockChangePointers: make(map[IFCERFTBlockPointer]bool),
		toUnrefPointers:     make(map[IFCERFTBlockPointer]bool),
		originals:           make(map[IFCERFTBlockPointer]IFCERFTBlockPointer),
	}
}

func newCRChains(ctx context.Context, cfg IFCERFTConfig, rmds []*IFCERFTRootMetadata, fbo *folderBlockOps, identifyTypes bool) (
	ccs *IFCERFTCrChains, err error) {
	ccs = newCRChainsEmpty()

	// For each MD update, turn each update in each op into map
	// entries and create chains for the BlockPointers that are
	// affected directly by the operation.
	for _, rmd := range rmds {
		// No new operations in these.
		if rmd.IsWriterMetadataCopiedSet() {
			continue
		}

		winfo, err := newWriterInfo(ctx, cfg, rmd.LastModifyingWriter, rmd.WriterKID())
		if err != nil {
			return nil, err
		}

		if ptr := rmd.data.cachedChanges.Info.IFCERFTBlockPointer; ptr != zeroPtr {
			ccs.blockChangePointers[ptr] = true
		}

		for _, op := range rmd.data.Changes.Ops {
			op.SetWriterInfo(winfo)
			err := ccs.MakeChainForOp(op)
			if err != nil {
				return nil, err
			}
		}

		if !ccs.originalRoot.IsInitialized() {
			// Find the original pointer for the root directory
			if rootChain, ok :=
				ccs.byMostRecent[rmd.data.Dir.IFCERFTBlockPointer]; ok {
				ccs.originalRoot = rootChain.original
			}
		}
	}

	for _, chain := range ccs.byOriginal {
		chain.Collapse()
		// NOTE: even if we've removed all its ops, still keep the
		// chain around so we can see the mapping between the original
		// and most recent pointers.

		// Figure out if this chain is a file or directory.  We don't
		// need to do this for chains that represent a resolution in
		// progress, since in that case all actions are already
		// completed.
		if len(rmds) > 0 && identifyTypes {
			err := chain.IdentifyType(ctx, fbo, rmds[len(rmds)-1], ccs)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(rmds) > 0 {
		ccs.mostRecentMD = rmds[len(rmds)-1]
	}

	return ccs, nil
}

type IFCERFTCrChainSummary struct {
	Path string
	Ops  []string
}

func (ccs *IFCERFTCrChains) Summary(identifyChains *IFCERFTCrChains, nodeCache IFCERFTNodeCache) (res []*IFCERFTCrChainSummary) {
	for _, chain := range ccs.byOriginal {
		summary := &IFCERFTCrChainSummary{}
		res = append(res, summary)

		// first stringify all the ops so they are displayed even if
		// we can't find the path.
		for _, op := range chain.ops {
			summary.Ops = append(summary.Ops, op.String())
		}

		// find the path name using the identified most recent pointer
		n := nodeCache.Get(chain.mostRecent.ref())
		if n == nil {
			summary.Path = fmt.Sprintf("Unknown path: %v", chain.mostRecent)
			continue
		}

		path := nodeCache.PathFromNode(n)
		summary.Path = path.String()
	}

	return res
}

func (ccs *IFCERFTCrChains) RemoveChain(ptr IFCERFTBlockPointer) {
	delete(ccs.byOriginal, ptr)
	delete(ccs.byMostRecent, ptr)
}

// copyOpAndRevertUnrefsToOriginals returns a shallow copy of the op,
// modifying each custom BlockPointer field to reference the original
// version of the corresponding blocks.
func (ccs *IFCERFTCrChains) CopyOpAndRevertUnrefsToOriginals(currOp IFCERFTOps) IFCERFTOps {
	var unrefs []*IFCERFTBlockPointer
	var newOp IFCERFTOps
	switch realOp := currOp.(type) {
	case *createOp:
		newCreateOp := *realOp
		unrefs = append(unrefs, &newCreateOp.Dir.Unref)
		newOp = &newCreateOp
	case *rmOp:
		newRmOp := *realOp
		unrefs = append(unrefs, &newRmOp.Dir.Unref)
		newOp = &newRmOp
	case *renameOp:
		newRenameOp := *realOp
		unrefs = append(unrefs, &newRenameOp.OldDir.Unref,
			&newRenameOp.NewDir.Unref, &newRenameOp.Renamed)
		newOp = &newRenameOp
	case *syncOp:
		newSyncOp := *realOp
		unrefs = append(unrefs, &newSyncOp.File.Unref)
		newOp = &newSyncOp
	case *setAttrOp:
		newSetAttrOp := *realOp
		unrefs = append(unrefs, &newSetAttrOp.Dir.Unref, &newSetAttrOp.File)
		newOp = &newSetAttrOp
	case *gcOp:
		// No need to copy a gcOp, it won't be modified
		newOp = realOp
	}
	for _, unref := range unrefs {
		original, ok := ccs.originals[*unref]
		if ok {
			*unref = original
		}
	}
	return newOp
}

// changeOriginal converts the original of a chain to a different original.
func (ccs *IFCERFTCrChains) ChangeOriginal(oldOriginal IFCERFTBlockPointer, newOriginal IFCERFTBlockPointer) error {
	chain, ok := ccs.byOriginal[oldOriginal]
	if !ok {
		return IFCERFTNoChainFoundError{oldOriginal}
	}
	if _, ok := ccs.byOriginal[newOriginal]; ok {
		return fmt.Errorf("crChains.changeOriginal: New original %v "+
			"already exists", newOriginal)
	}

	delete(ccs.byOriginal, oldOriginal)
	chain.original = newOriginal
	ccs.byOriginal[newOriginal] = chain
	ccs.originals[oldOriginal] = newOriginal
	if chain.mostRecent == oldOriginal {
		chain.mostRecent = newOriginal
		delete(ccs.byMostRecent, oldOriginal)
		ccs.byMostRecent[newOriginal] = chain
	}

	if _, ok := ccs.deletedOriginals[oldOriginal]; ok {
		delete(ccs.deletedOriginals, oldOriginal)
		ccs.deletedOriginals[newOriginal] = true
	}
	if _, ok := ccs.createdOriginals[oldOriginal]; ok {
		delete(ccs.createdOriginals, oldOriginal)
		ccs.createdOriginals[newOriginal] = true
	}
	if ri, ok := ccs.renamedOriginals[oldOriginal]; ok {
		delete(ccs.renamedOriginals, oldOriginal)
		ccs.renamedOriginals[newOriginal] = ri
	}
	return nil
}
