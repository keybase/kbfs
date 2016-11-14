// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"golang.org/x/net/context"
)

// blockJournal stores a single ordered list of block operations for a
// single TLF, along with the associated block data, in flat files in
// a directory on disk.
//
// The directory layout looks like:
//
// dir/block_journal/EARLIEST
// dir/block_journal/LATEST
// dir/block_journal/0...000
// dir/block_journal/0...001
// dir/block_journal/0...fff
// dir/blocks/0100/0...01/data
// dir/blocks/0100/0...01/key_server_half
// ...
// dir/blocks/01ff/f...ff/data
// dir/blocks/01ff/f...ff/key_server_half
//
// Each entry in the journal in dir/block_journal contains the
// mutating operation and arguments for a single operation, except for
// block data. (See diskJournal comments for more details about the
// journal.)
//
// The block data is stored separately in dir/blocks. Each block has
// its own subdirectory with its ID truncated to 17 bytes (34
// characters) as a name. The block subdirectories are splayed over (#
// of possible hash types) * 256 subdirectories -- one byte for the
// hash type (currently only one) plus the first byte of the hash data
// -- using the first four characters of the name to keep the number
// of directories in dir itself to a manageable number, similar to
// git. Each block directory has data, which is the raw block data
// that should hash to the block ID, and key_server_half, which
// contains the raw data for the associated key server half. Future
// versions of the journal might add more files to this directory; if
// any code is written to move blocks around, it should be careful to
// preserve any unknown files in a block directory.
//
// The maximum number of characters added to the root dir by a block
// journal is 59:
//
//   /blocks/01ff/f...(30 characters total)...ff/key_server_half
//
// blockJournal is not goroutine-safe, so any code that uses it must
// guarantee that only one goroutine at a time calls its functions.
type blockJournal struct {
	codec  kbfscodec.Codec
	crypto cryptoPure
	dir    string

	log      logger.Logger
	deferLog logger.Logger

	j                diskJournal
	saveUntilMDFlush *diskJournal

	s *blockDiskStore
}

type blockOpType int

const (
	blockPutOp    blockOpType = 1
	addRefOp      blockOpType = 2
	removeRefsOp  blockOpType = 3
	archiveRefsOp blockOpType = 4
	mdRevMarkerOp blockOpType = 5
)

func (t blockOpType) String() string {
	switch t {
	case blockPutOp:
		return "blockPut"
	case addRefOp:
		return "addReference"
	case removeRefsOp:
		return "removeReferences"
	case archiveRefsOp:
		return "archiveReferences"
	case mdRevMarkerOp:
		return "mdRevisionMarker"
	default:
		return fmt.Sprintf("blockOpType(%d)", t)
	}
}

// A blockJournalEntry is just the name of the operation and the
// associated block ID and contexts. Fields are exported only for
// serialization.
type blockJournalEntry struct {
	// Must be one of the four ops above.
	Op blockOpType
	// Must have exactly one entry with one context for blockPutOp and
	// addRefOp.  Used for all ops except for mdRevMarkerOp.
	Contexts map[BlockID][]BlockContext `codec:",omitempty"`
	// Only used for mdRevMarkerOps.
	Revision MetadataRevision `codec:",omitempty"`
	// Ignore this entry while flushing if this is true.
	Ignore bool `codec:",omitempty"`

	codec.UnknownFieldSetHandler
}

// Get the single context stored in this entry. Only applicable to
// blockPutOp and addRefOp.
func (e blockJournalEntry) getSingleContext() (
	BlockID, BlockContext, error) {
	switch e.Op {
	case blockPutOp, addRefOp:
		if len(e.Contexts) != 1 {
			return BlockID{}, BlockContext{}, fmt.Errorf(
				"Op %s doesn't have exactly one context: %v",
				e.Op, e.Contexts)
		}
		for id, idContexts := range e.Contexts {
			if len(idContexts) != 1 {
				return BlockID{}, BlockContext{}, fmt.Errorf(
					"Op %s doesn't have exactly one context for id=%s: %v",
					e.Op, id, idContexts)
			}
			return id, idContexts[0], nil
		}
	}

	return BlockID{}, BlockContext{}, fmt.Errorf(
		"getSingleContext() erroneously called on op %s", e.Op)
}

func savedBlockJournalDir(dir string) string {
	return filepath.Join(dir, "saved_block_journal")
}

// makeBlockJournal returns a new blockJournal for the given
// directory. Any existing journal entries are read.
func makeBlockJournal(
	ctx context.Context, codec kbfscodec.Codec, crypto cryptoPure,
	dir string, log logger.Logger) (*blockJournal, error) {
	journalPath := filepath.Join(dir, "block_journal")
	deferLog := log.CloneWithAddedDepth(1)
	j := makeDiskJournal(
		codec, journalPath, reflect.TypeOf(blockJournalEntry{}))

	storeDir := filepath.Join(dir, "blocks")
	s := makeBlockDiskStore(codec, crypto, storeDir)
	journal := &blockJournal{
		codec:    codec,
		crypto:   crypto,
		dir:      dir,
		log:      log,
		deferLog: deferLog,
		j:        j,
		s:        s,
	}

	// If a saved block journal exists, we need to remove its entries
	// on the next successful MD flush.
	savedJournalDir := savedBlockJournalDir(dir)
	fi, err := os.Stat(savedJournalDir)
	if err == nil {
		if !fi.IsDir() {
			return nil,
				fmt.Errorf("%s exists, but is not a dir", savedJournalDir)
		}
		log.CDebugf(ctx, "A saved block journal exists at %s", savedJournalDir)
		sj := makeDiskJournal(
			codec, savedJournalDir, reflect.TypeOf(blockJournalEntry{}))
		journal.saveUntilMDFlush = &sj
	}

	return journal, nil
}

// The functions below are for reading and writing journal entries.

func (j *blockJournal) readJournalEntry(ordinal journalOrdinal) (
	blockJournalEntry, error) {
	entry, err := j.j.readJournalEntry(ordinal)
	if err != nil {
		return blockJournalEntry{}, err
	}

	return entry.(blockJournalEntry), nil
}

// readJournal reads the journal and returns a map of all the block
// references in the journal and the total number of bytes that need
// flushing.
func (j *blockJournal) readJournal(ctx context.Context) (
	map[BlockID]blockRefMap, error) {
	refs := make(map[BlockID]blockRefMap)

	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return refs, nil
	} else if err != nil {
		return nil, err
	}
	last, err := j.j.readLatestOrdinal()
	if err != nil {
		return nil, err
	}

	j.log.CDebugf(ctx, "Reading journal entries %d to %d", first, last)

	for i := first; i <= last; i++ {
		e, err := j.readJournalEntry(i)
		if err != nil {
			return nil, err
		}

		// Handle single ops separately.
		switch e.Op {
		case blockPutOp, addRefOp:
			id, context, err := e.getSingleContext()
			if err != nil {
				return nil, err
			}

			blockRefs := refs[id]
			if blockRefs == nil {
				blockRefs = make(blockRefMap)
				refs[id] = blockRefs
			}

			err = blockRefs.put(context, liveBlockRef, i.String())
			if err != nil {
				return nil, err
			}

			continue
		}

		for id, idContexts := range e.Contexts {
			blockRefs := refs[id]

			switch e.Op {
			case removeRefsOp:
				if blockRefs == nil {
					// All refs are already gone,
					// which is not an error.
					continue
				}

				for _, context := range idContexts {
					err := blockRefs.remove(context, "")
					if err != nil {
						return nil, err
					}
				}

				if len(blockRefs) == 0 {
					delete(refs, id)
				}

			case archiveRefsOp:
				if blockRefs == nil {
					blockRefs = make(blockRefMap)
					refs[id] = blockRefs
				}

				for _, context := range idContexts {
					err := blockRefs.put(
						context, archivedBlockRef, i.String())
					if err != nil {
						return nil, err
					}
				}

			case mdRevMarkerOp:
				// Ignore MD revision markers.
				continue

			default:
				return nil, fmt.Errorf("Unknown op %s", e.Op)
			}
		}
	}
	return refs, nil
}

func (j *blockJournal) appendJournalEntry(ctx context.Context,
	entry blockJournalEntry) (
	journalOrdinal, error) {
	ordinal, err := j.j.appendJournalEntry(nil, entry)
	if err != nil {
		return 0, err
	}

	if j.saveUntilMDFlush != nil {
		_, err := j.saveUntilMDFlush.appendJournalEntry(nil, entry)
		if err != nil {
			// TODO: Should we remove it from the main journal and
			// fail the whole append?
			j.log.CWarningf(ctx, "Appending to the saved list failed: %v", err)
		}
	}

	return ordinal, nil
}

func (j *blockJournal) length() (uint64, error) {
	return j.j.length()
}

func (j *blockJournal) end() (journalOrdinal, error) {
	last, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	return last + 1, nil
}

func (j *blockJournal) exists(id BlockID) error {
	_, err := os.Stat(j.s.dataPath(id))
	return err
}

// All functions below are public functions.

func (j *blockJournal) hasAnyRef(id BlockID) (bool, error) {
	return j.s.hasAnyRef(id)
}

func (j *blockJournal) hasNonArchivedRef(id BlockID) (bool, error) {
	return j.s.hasNonArchivedRef(id)
}

func (j *blockJournal) hasContext(id BlockID, context BlockContext) (
	bool, error) {
	return j.s.hasContext(id, context)
}

func (j *blockJournal) getDataWithContext(id BlockID, context BlockContext) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	return j.s.getDataWithContext(id, context)
}

func (j *blockJournal) getUnflushedBytes() int64 {
	// TODO: Figure out what to do here.
	return 0
}

func (j *blockJournal) putData(
	ctx context.Context, id BlockID, context BlockContext, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf) (err error) {
	j.log.CDebugf(ctx, "Putting %d bytes of data for block %s with context %v",
		len(buf), id, context)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Put for block %s with context %v failed with %v",
				id, context, err)
		}
	}()

	var next journalOrdinal
	lo, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		next = 0
	} else if err != nil {
		return err
	} else {
		next = lo + 1
	}

	err = j.s.putData(id, context, buf, serverHalf, next.String())
	if err != nil {
		return err
	}

	_, err = j.appendJournalEntry(ctx, blockJournalEntry{
		Op:       blockPutOp,
		Contexts: map[BlockID][]BlockContext{id: {context}},
	})
	if err != nil {
		return err
	}

	return nil
}

func (j *blockJournal) addReference(
	ctx context.Context, id BlockID, context BlockContext) (
	err error) {
	j.log.CDebugf(ctx, "Adding reference for block %s with context %v",
		id, context)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Adding reference for block %s with context %v failed with %v",
				id, context, err)
		}
	}()

	var next journalOrdinal
	lo, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		next = 0
	} else if err != nil {
		return err
	} else {
		next = lo + 1
	}

	err = j.s.addReference(id, context, next.String())
	if err != nil {
		return err
	}

	_, err = j.appendJournalEntry(ctx, blockJournalEntry{
		Op:       addRefOp,
		Contexts: map[BlockID][]BlockContext{id: {context}},
	})
	if err != nil {
		return err
	}

	return nil
}

// removeReferences fixes up the in-memory reference map to delete the
// given references.
func (j *blockJournal) removeReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	j.log.CDebugf(ctx, "Removing references for %v", contexts)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Removing references for %v", contexts, err)
		}
	}()

	// TODO: Explain why removing refs here is ok.
	liveCounts, err = j.s.removeReferences(contexts, "")
	if err != nil {
		return nil, err
	}

	_, err = j.appendJournalEntry(ctx, blockJournalEntry{
		Op:       removeRefsOp,
		Contexts: contexts,
	})
	if err != nil {
		return nil, err
	}

	return liveCounts, nil
}

func (j *blockJournal) archiveReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (err error) {
	j.log.CDebugf(ctx, "Archiving references for %v", contexts)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Archiving references for %v,", contexts, err)
		}
	}()

	var next journalOrdinal
	lo, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		next = 0
	} else if err != nil {
		return err
	} else {
		next = lo + 1
	}

	err = j.s.archiveReferences(contexts, next.String())
	if err != nil {
		return err
	}

	_, err = j.appendJournalEntry(ctx, blockJournalEntry{
		Op:       archiveRefsOp,
		Contexts: contexts,
	})
	if err != nil {
		return err
	}

	return nil
}

func (j *blockJournal) markMDRevision(ctx context.Context,
	rev MetadataRevision) (err error) {
	j.log.CDebugf(ctx, "Marking MD revision %d in the block journal", rev)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx, "Marking MD revision %d error: %v",
				rev, err)
		}
	}()

	_, err = j.appendJournalEntry(ctx, blockJournalEntry{
		Op:       mdRevMarkerOp,
		Revision: rev,
	})
	if err != nil {
		return err
	}
	return nil
}

// blockEntriesToFlush is an internal data structure for blockJournal;
// its fields shouldn't be accessed outside this file.
type blockEntriesToFlush struct {
	all   []blockJournalEntry
	first journalOrdinal

	puts  *blockPutState
	adds  *blockPutState
	other []blockJournalEntry
}

func (be blockEntriesToFlush) length() int {
	return len(be.all)
}

func (be blockEntriesToFlush) flushNeeded() bool {
	return be.length() > 0
}

// Only entries with ordinals less than the given ordinal (assumed to
// be <= latest ordinal + 1) are returned.  Also returns the maximum
// MD revision that can be merged after the returned entries are
// successfully flushed; if no entries are returned (i.e., the block
// journal is empty) then any MD revision may be flushed even when
// MetadataRevisionUninitialized is returned.
func (j *blockJournal) getNextEntriesToFlush(
	ctx context.Context, end journalOrdinal, maxToFlush int) (
	entries blockEntriesToFlush, maxMDRevToFlush MetadataRevision, err error) {
	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized, nil
	} else if err != nil {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
	}

	if first >= end {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized,
			fmt.Errorf("Trying to flush past the "+
				"start of the journal (first=%d, end=%d)", first, end)
	}

	realEnd, err := j.end()
	if realEnd == 0 {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized,
			fmt.Errorf("There was an earliest "+
				"ordinal %d, but no latest ordinal", first)
	} else if err != nil {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
	}

	if end > realEnd {
		return blockEntriesToFlush{}, MetadataRevisionUninitialized,
			fmt.Errorf("Trying to flush past the "+
				"end of the journal (realEnd=%d, end=%d)", realEnd, end)
	}

	entries.puts = newBlockPutState(int(end - first))
	entries.adds = newBlockPutState(int(end - first))
	maxMDRevToFlush = MetadataRevisionUninitialized

	loopEnd := end
	if first+journalOrdinal(maxToFlush) < end {
		loopEnd = first + journalOrdinal(maxToFlush)
	}

	for ordinal := first; ordinal < loopEnd; ordinal++ {
		entry, err := j.readJournalEntry(ordinal)
		if err != nil {
			return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
		}

		if entry.Ignore {
			if loopEnd < end {
				loopEnd++
			}
			entries.other = append(entries.other, entry)
			entries.all = append(entries.all, entry)
			continue
		}

		var data []byte
		var serverHalf kbfscrypto.BlockCryptKeyServerHalf

		switch entry.Op {
		case blockPutOp:
			id, bctx, err := entry.getSingleContext()
			if err != nil {
				return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
			}

			data, serverHalf, err = j.s.getData(id)
			if err != nil {
				return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
			}

			entries.puts.addNewBlock(
				BlockPointer{ID: id, BlockContext: bctx},
				nil, /* only used by folderBranchOps */
				ReadyBlockData{data, serverHalf}, nil)

		case addRefOp:
			id, bctx, err := entry.getSingleContext()
			if err != nil {
				return blockEntriesToFlush{}, MetadataRevisionUninitialized, err
			}

			entries.adds.addNewBlock(
				BlockPointer{ID: id, BlockContext: bctx},
				nil, /* only used by folderBranchOps */
				ReadyBlockData{}, nil)

		case mdRevMarkerOp:
			if entry.Revision < maxMDRevToFlush {
				return blockEntriesToFlush{}, MetadataRevisionUninitialized,
					fmt.Errorf("Max MD revision decreased in block journal "+
						"from %d to %d", entry.Revision, maxMDRevToFlush)
			}
			maxMDRevToFlush = entry.Revision
			entries.other = append(entries.other, entry)

		default:
			entries.other = append(entries.other, entry)
		}

		entries.all = append(entries.all, entry)
	}
	entries.first = first
	return entries, maxMDRevToFlush, nil
}

// flushNonBPSBlockJournalEntry flushes journal entries that can't be
// parallelized via a blockPutState.
func flushNonBPSBlockJournalEntry(
	ctx context.Context, log logger.Logger,
	bserver BlockServer, tlfID tlf.ID, entry blockJournalEntry) error {
	log.CDebugf(ctx, "Flushing other block op %v", entry)

	switch entry.Op {
	case removeRefsOp:
		_, err := bserver.RemoveBlockReferences(
			ctx, tlfID, entry.Contexts)
		if err != nil {
			return err
		}

	case archiveRefsOp:
		err := bserver.ArchiveBlockReferences(
			ctx, tlfID, entry.Contexts)
		if err != nil {
			return err
		}

	case blockPutOp:
		if !entry.Ignore {
			return errors.New("Trying to flush unignored blockPut as other")
		}
		// Otherwise nothing to do.

	case mdRevMarkerOp:
		// Nothing to do.

	default:
		return fmt.Errorf("Unknown op %s", entry.Op)
	}

	return nil
}

func flushBlockEntries(ctx context.Context, log logger.Logger,
	bserver BlockServer, bcache BlockCache, reporter Reporter, tlfID tlf.ID,
	tlfName CanonicalTlfName, entries blockEntriesToFlush) error {
	if !entries.flushNeeded() {
		// Avoid logging anything when there's nothing to flush.
		return nil
	}

	// Do all the put state stuff first, in parallel.  We need to do
	// the puts strictly before the addRefs, since the latter might
	// reference the former.
	log.CDebugf(ctx, "Putting %d blocks", len(entries.puts.blockStates))
	blocksToRemove, err := doBlockPuts(ctx, bserver, bcache, reporter,
		log, tlfID, tlfName, *entries.puts)
	if err != nil {
		if isRecoverableBlockError(err) {
			log.CWarningf(ctx,
				"Recoverable block error encountered on puts: %v, ptrs=%v",
				err, blocksToRemove)
		}
		return err
	}

	// Next, do the addrefs.
	log.CDebugf(ctx, "Adding %d block references",
		len(entries.adds.blockStates))
	blocksToRemove, err = doBlockPuts(ctx, bserver, bcache, reporter,
		log, tlfID, tlfName, *entries.adds)
	if err != nil {
		if isRecoverableBlockError(err) {
			log.CWarningf(ctx,
				"Recoverable block error encountered on addRefs: %v, ptrs=%v",
				err, blocksToRemove)
		}
		return err
	}

	// Now do all the other, non-put/addref entries.  TODO:
	// parallelize these as well.
	for _, entry := range entries.other {
		err := flushNonBPSBlockJournalEntry(ctx, log, bserver, tlfID, entry)
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *blockJournal) removeFlushedEntry(ctx context.Context,
	ordinal journalOrdinal, entry blockJournalEntry) (
	flushedBytes int64, err error) {
	earliestOrdinal, err := j.j.readEarliestOrdinal()
	if err != nil {
		return 0, err
	}

	if ordinal != earliestOrdinal {
		return 0, fmt.Errorf("Expected ordinal %d, got %d",
			ordinal, earliestOrdinal)
	}

	_, err = j.j.removeEarliest()
	if err != nil {
		return 0, err
	}

	// Remove any of the entry's refs that hasn't been modified by
	// a subsequent block op (i.e., that has earliestOrdinal as a
	// tag).
	liveCounts, err := j.s.removeReferences(
		entry.Contexts, earliestOrdinal.String())
	if err != nil {
		return 0, err
	}
	for id, liveCount := range liveCounts {
		if liveCount == 0 {
			// Garbage-collect the old entry if we are not saving
			// blocks until the next MD flush.  TODO: we'll
			// eventually need a sweeper to clean up entries left
			// behind if we crash here.
			if j.saveUntilMDFlush == nil {
				err = j.s.remove(id)
				if err != nil {
					return 0, err
				}
			}
		}
	}

	return flushedBytes, nil
}

func (j *blockJournal) removeFlushedEntries(ctx context.Context,
	entries blockEntriesToFlush, tlfID tlf.ID, reporter Reporter) error {
	// Remove them all!
	for i, entry := range entries.all {
		flushedBytes, err := j.removeFlushedEntry(
			ctx, entries.first+journalOrdinal(i), entry)
		if err != nil {
			return err
		}

		reporter.NotifySyncStatus(ctx, &keybase1.FSPathSyncStatus{
			PublicTopLevelFolder: tlfID.IsPublic(),
			// Path: TODO,
			// SyncingBytes: TODO,
			// SyncingOps: TODO,
			SyncedBytes: flushedBytes,
		})
	}
	return nil
}

func (j *blockJournal) ignoreBlocksAndMDRevMarkers(ctx context.Context,
	blocksToIgnore []BlockID) error {
	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	last, err := j.j.readLatestOrdinal()
	if err != nil {
		return err
	}

	idsToIgnore := make(map[BlockID]bool)
	for _, id := range blocksToIgnore {
		idsToIgnore[id] = true
	}

	for i := first; i <= last; i++ {
		e, err := j.readJournalEntry(i)
		if err != nil {
			return err
		}

		switch e.Op {
		case blockPutOp, addRefOp:
			id, _, err := e.getSingleContext()
			if err != nil {
				return err
			}

			if !idsToIgnore[id] {
				continue
			}

			e.Ignore = true
			err = j.j.writeJournalEntry(i, e)
			if err != nil {
				return err
			}

		case mdRevMarkerOp:
			e.Ignore = true
			err = j.j.writeJournalEntry(i, e)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (j *blockJournal) saveBlocksUntilNextMDFlush() error {
	if j.saveUntilMDFlush != nil {
		return nil
	}

	// Copy the current journal entries into a new journal.  After the
	// next MD flush, we can use the saved journal to delete the block
	// data for all the entries in the saved journal.
	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	last, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	savedJournalDir := savedBlockJournalDir(j.dir)
	sj := makeDiskJournal(
		j.codec, savedJournalDir, reflect.TypeOf(blockJournalEntry{}))
	savedJournal := &sj

	for i := first; i <= last; i++ {
		e, err := j.readJournalEntry(i)
		if err != nil {
			return err
		}

		savedJournal.appendJournalEntry(nil, e)
	}

	j.saveUntilMDFlush = savedJournal
	return nil
}

func (j *blockJournal) onMDFlush() error {
	if j.saveUntilMDFlush == nil {
		return nil
	}

	// Delete the block data for anything in the saved journal.
	first, err := j.saveUntilMDFlush.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	last, err := j.saveUntilMDFlush.readLatestOrdinal()
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	for i := first; i <= last; i++ {
		e, err := j.saveUntilMDFlush.readJournalEntry(i)
		if err != nil {
			return err
		}

		_, err = j.saveUntilMDFlush.removeEarliest()
		if err != nil {
			return err
		}

		entry, ok := e.(blockJournalEntry)
		if !ok {
			return errors.New("Unexpected block journal entry type in saved")
		}

		j.log.CDebugf(nil, "Removing data for entry %d", i)
		for id := range entry.Contexts {
			hasRef, err := j.hasAnyRef(id)
			if err != nil {
				return err
			}
			if !hasRef {
				// Garbage-collect the old entry.  TODO: we'll
				// eventually need a sweeper to clean up entries left
				// behind if we crash here.
				err = j.s.remove(id)
				if err != nil {
					return err
				}
			}
		}
	}

	err = os.RemoveAll(j.saveUntilMDFlush.dir)
	if err != nil {
		return err
	}

	j.saveUntilMDFlush = nil
	return nil
}

func (j *blockJournal) checkInSync(ctx context.Context) error {
	journalRefs, err := j.readJournal(ctx)
	if err != nil {
		return err
	}

	storeRefs, err := j.s.getAllRefs()
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(journalRefs, storeRefs) {
		return fmt.Errorf("journal refs = %+v != store refs = %+v",
			journalRefs, storeRefs)
	}
	return nil
}
