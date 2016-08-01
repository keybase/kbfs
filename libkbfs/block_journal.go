// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"

	"github.com/keybase/client/go/logger"
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
// its own subdirectory with its ID as a name.  The block
// subdirectories are splayed over (# of possible hash types) * 256
// subdirectories -- one byte for the hash type (currently only one)
// plus the first byte of the hash data -- using the first four
// characters of the name to keep the number of directories in dir
// itself to a manageable number, similar to git. Each block directory
// has data, which is the raw block data that should hash to the block
// ID, and key_server_half, which contains the raw data for the
// associated key server half.
//
// blockJournal is not goroutine-safe, so any code that uses it must
// guarantee that only one goroutine at a time calls its functions.
type blockJournal struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	log      logger.Logger
	deferLog logger.Logger

	j          diskJournal
	refs       map[BlockID]blockRefMap
	isShutdown bool
}

type bserverOpName string

const (
	blockPutOp    bserverOpName = "blockPut"
	addRefOp      bserverOpName = "addReference"
	removeRefsOp  bserverOpName = "removeReferences"
	archiveRefsOp bserverOpName = "archiveReferences"
)

// A bserverJournalEntry is just the name of the operation and the
// associated block ID and contexts. Fields are exported only for
// serialization.
type bserverJournalEntry struct {
	// Must be one of the four ops above.
	Op bserverOpName
	ID BlockID
	// Must have exactly one entry for blockPutOp and addRefOp.
	Contexts []BlockContext
}

// makeBlockJournal returns a new blockJournal for the given
// directory. Any existing journal entries are read.
func makeBlockJournal(
	codec Codec, crypto cryptoPure, dir string, log logger.Logger) (
	*blockJournal, error) {
	journalPath := filepath.Join(dir, "block_journal")
	deferLog := log.CloneWithAddedDepth(1)
	j := makeDiskJournal(
		codec, journalPath, reflect.TypeOf(bserverJournalEntry{}))
	journal := &blockJournal{
		codec:    codec,
		crypto:   crypto,
		dir:      dir,
		log:      log,
		deferLog: deferLog,
		j:        j,
	}

	refs, err := journal.readJournalLocked()
	if err != nil {
		return nil, err
	}

	journal.refs = refs
	return journal, nil
}

// The functions below are for building various non-journal paths.

func (j *blockJournal) blocksPath() string {
	return filepath.Join(j.dir, "blocks")
}

func (j *blockJournal) blockPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(j.blocksPath(), idStr[:4], idStr[4:])
}

func (j *blockJournal) blockDataPath(id BlockID) string {
	return filepath.Join(j.blockPath(id), "data")
}

func (j *blockJournal) keyServerHalfPath(id BlockID) string {
	return filepath.Join(j.blockPath(id), "key_server_half")
}

// The functions below are for reading and writing journal entries.

func (j *blockJournal) readJournalEntryLocked(o journalOrdinal) (
	bserverJournalEntry, error) {
	entry, err := j.j.readJournalEntry(o)
	if err != nil {
		return bserverJournalEntry{}, err
	}

	return entry.(bserverJournalEntry), nil
}

// readJournalLocked reads the journal and returns a map of all the
// block references in the journal.
func (j *blockJournal) readJournalLocked() (
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

	for i := first; i <= last; i++ {
		e, err := j.readJournalEntryLocked(i)
		if err != nil {
			return nil, err
		}

		blockRefs := refs[e.ID]
		if blockRefs == nil {
			blockRefs = make(blockRefMap)
			refs[e.ID] = blockRefs
		}

		switch e.Op {
		case blockPutOp, addRefOp:
			if len(e.Contexts) != 1 {
				return nil, fmt.Errorf(
					"Op %s for id=%s doesn't have exactly one context: %v",
					e.Op, e.ID, e.Contexts)
			}

			blockRefs.put(e.Contexts[0], liveBlockRef)

		case removeRefsOp:
			for _, context := range e.Contexts {
				delete(blockRefs, context.GetRefNonce())
			}

		case archiveRefsOp:
			for _, context := range e.Contexts {
				blockRefs.put(context, archivedBlockRef)
			}

		default:
			return nil, fmt.Errorf("Unknown op %s", e.Op)
		}
	}
	return refs, nil
}

func (j *blockJournal) writeJournalEntryLocked(
	o journalOrdinal, entry bserverJournalEntry) error {
	return j.j.writeJournalEntry(o, entry)
}

func (j *blockJournal) appendJournalEntryLocked(
	op bserverOpName, id BlockID, contexts []BlockContext) error {
	return j.j.appendJournalEntry(nil, bserverJournalEntry{
		Op:       op,
		ID:       id,
		Contexts: contexts,
	})
}

func (j *blockJournal) length() (uint64, error) {
	return j.j.length()
}

func (j *blockJournal) getRefEntryLocked(
	id BlockID, refNonce BlockRefNonce) (blockRefEntry, error) {
	refs := j.refs[id]
	if refs == nil {
		return blockRefEntry{}, BServerErrorBlockNonExistent{}
	}

	e, ok := refs[refNonce]
	if !ok {
		return blockRefEntry{}, BServerErrorBlockNonExistent{}
	}

	return e, nil
}

// getDataLocked verifies the block data for the given ID and context
// and returns it.
func (j *blockJournal) getDataLocked(id BlockID) (
	[]byte, BlockCryptKeyServerHalf, error) {
	data, err := ioutil.ReadFile(j.blockDataPath(id))
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	keyServerHalfPath := j.keyServerHalfPath(id)
	buf, err := ioutil.ReadFile(keyServerHalfPath)
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	// Check integrity.

	dataID, err := j.crypto.MakePermanentBlockID(data)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	if id != dataID {
		return nil, BlockCryptKeyServerHalf{}, fmt.Errorf(
			"Block ID mismatch: expected %s, got %s", id, dataID)
	}

	var serverHalf BlockCryptKeyServerHalf
	err = serverHalf.UnmarshalBinary(buf)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	return data, serverHalf, nil
}

// getDataLocked verifies the block data for the given ID and context
// and returns it.
func (j *blockJournal) getDataWithContextLocked(
	id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	// Check arguments.

	refEntry, err := j.getRefEntryLocked(id, context.GetRefNonce())
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	err = refEntry.checkContext(context)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	return j.getDataLocked(id)
}

func (j *blockJournal) putRefEntryLocked(
	id BlockID, refEntry blockRefEntry) error {
	existingRefEntry, err := j.getRefEntryLocked(
		id, refEntry.Context.GetRefNonce())
	var exists bool
	switch err.(type) {
	case BServerErrorBlockNonExistent:
		exists = false
	case nil:
		exists = true
	default:
		return err
	}

	if exists {
		err = existingRefEntry.checkContext(refEntry.Context)
		if err != nil {
			return err
		}
	}

	if j.refs[id] == nil {
		j.refs[id] = make(blockRefMap)
	}

	j.refs[id].put(refEntry.Context, refEntry.Status)
	return nil
}

// All functions below are public functions.

var errBlockJournalShutdown = errors.New("blockJournal is shutdown")

func (j *blockJournal) getData(id BlockID) (
	[]byte, BlockCryptKeyServerHalf, error) {
	if j.isShutdown {
		return nil, BlockCryptKeyServerHalf{},
			errBlockJournalShutdown
	}

	return j.getDataLocked(id)
}

func (j *blockJournal) getDataWithContext(
	id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	if j.isShutdown {
		return nil, BlockCryptKeyServerHalf{},
			errBlockJournalShutdown
	}

	return j.getDataWithContextLocked(id, context)
}

func (j *blockJournal) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	if j.isShutdown {
		return nil, errBlockJournalShutdown
	}

	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)

	for id, refs := range j.refs {
		if len(refs) == 0 {
			continue
		}

		res[id] = make(map[BlockRefNonce]blockRefLocalStatus)
		for ref, refEntry := range refs {
			res[id][ref] = refEntry.Status
		}
	}

	return res, nil
}

func (j *blockJournal) putData(
	id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	err := validateBlockServerPut(j.crypto, id, context, buf)
	if err != nil {
		return err
	}

	if j.isShutdown {
		return errBlockJournalShutdown
	}

	_, existingServerHalf, err := j.getDataWithContextLocked(id, context)
	var exists bool
	switch err.(type) {
	case BServerErrorBlockNonExistent:
		exists = false
	case nil:
		exists = true
	default:
		return err
	}

	if exists {
		// If the entry already exists, everything should be
		// the same, except for possibly additional
		// references.

		// We checked that both buf and existingData hash to
		// id, so no need to check that they're both equal.

		if existingServerHalf != serverHalf {
			return fmt.Errorf(
				"key server half mismatch: expected %s, got %s",
				existingServerHalf, serverHalf)
		}
	}

	err = os.MkdirAll(j.blockPath(id), 0700)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(j.blockDataPath(id), buf, 0600)
	if err != nil {
		return err
	}

	// TODO: Add integrity-checking for key server half?

	err = ioutil.WriteFile(
		j.keyServerHalfPath(id), serverHalf.data[:], 0600)
	if err != nil {
		return err
	}

	err = j.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
	if err != nil {
		return err
	}

	return j.appendJournalEntryLocked(
		blockPutOp, id, []BlockContext{context})
}

func (j *blockJournal) addReference(id BlockID, context BlockContext) error {
	if j.isShutdown {
		return errBlockJournalShutdown
	}

	refs := j.refs[id]
	if refs == nil {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
			"doesn't exist and cannot be referenced.", id)}
	}

	// Only add it if there's a non-archived reference.
	hasNonArchivedRef := false
	for _, refEntry := range refs {
		if refEntry.Status == liveBlockRef {
			hasNonArchivedRef = true
			break
		}
	}

	if !hasNonArchivedRef {
		return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
			"been archived and cannot be referenced.", id)}
	}

	// TODO: Figure out if we should allow adding a reference even
	// if all the existing references are archived, or if we have
	// no references at all. Also figure out what to do with an
	// addReference without a preceding Put.

	err := j.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
	if err != nil {
		return err
	}

	return j.appendJournalEntryLocked(
		addRefOp, id, []BlockContext{context})
}

func (j *blockJournal) removeReferences(
	id BlockID, contexts []BlockContext,
	removeBlockIfNoReferences bool) (int, error) {
	if j.isShutdown {
		return 0, errBlockJournalShutdown
	}

	refs := j.refs[id]
	if refs == nil {
		// This block is already gone; no error.
		return 0, nil
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		// If this check fails, this ref is already gone,
		// which is not an error.
		if refEntry, ok := refs[refNonce]; ok {
			err := refEntry.checkContext(context)
			if err != nil {
				return 0, err
			}

			delete(refs, refNonce)
		}
	}

	count := len(refs)
	if count == 0 && removeBlockIfNoReferences {
		err := os.RemoveAll(j.blockPath(id))
		if err != nil {
			return 0, err
		}
	}

	err := j.appendJournalEntryLocked(removeRefsOp, id, contexts)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (j *blockJournal) archiveReferences(
	id BlockID, contexts []BlockContext) error {
	if j.isShutdown {
		return errBlockJournalShutdown
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		refEntry, err := j.getRefEntryLocked(id, refNonce)
		switch err.(type) {
		case BServerErrorBlockNonExistent:
			return BServerErrorBlockNonExistent{
				fmt.Sprintf(
					"Block ID %s (ref %s) doesn't "+
						"exist and cannot be archived.",
					id, refNonce),
			}
		case nil:
			break

		default:
			return err
		}

		err = refEntry.checkContext(context)
		if err != nil {
			return err
		}

		refEntry.Status = archivedBlockRef
		err = j.putRefEntryLocked(id, refEntry)
		if err != nil {
			return err
		}
	}

	return j.appendJournalEntryLocked(archiveRefsOp, id, contexts)
}

func (j *blockJournal) shutdown() {
	j.isShutdown = true

	// Double-check the on-disk journal with the in-memory one.
	refs, err := j.readJournalLocked()
	if err != nil {
		panic(err)
	}

	if !reflect.DeepEqual(refs, j.refs) {
		panic(fmt.Sprintf("refs = %v != j.refs = %v", refs, j.refs))
	}
}

func (j *blockJournal) flushOne(
	ctx context.Context, bserver BlockServer, tlfID TlfID) (bool, error) {
	if j.isShutdown {
		return false, errBlockJournalShutdown
	}

	earliestOrdinal, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	e, err := j.readJournalEntryLocked(earliestOrdinal)
	if err != nil {
		return false, err
	}

	j.log.Debug("Flushing block op %v", e)

	switch e.Op {
	case blockPutOp:
		if len(e.Contexts) != 1 {
			return false, fmt.Errorf(
				"Op %s for id=%s doesn't have exactly one context: %v",
				e.Op, e.ID, e.Contexts)
		}

		data, serverHalf, err := j.getDataLocked(e.ID)
		if err != nil {
			return false, err
		}

		err = bserver.Put(ctx, e.ID, tlfID,
			e.Contexts[0], data, serverHalf)
		if err != nil {
			return false, err
		}

	case addRefOp:
		if len(e.Contexts) != 1 {
			return false, fmt.Errorf(
				"Op %s for id=%s doesn't have exactly one context: %v",
				e.Op, e.ID, e.Contexts)
		}

		bContext := e.Contexts[0]
		err = bserver.AddBlockReference(
			ctx, e.ID, tlfID, bContext)
		if err != nil {
			return false, err
		}

	case removeRefsOp:
		// TODO: Combine multiple remove refs.
		_, err = bserver.RemoveBlockReferences(
			ctx, tlfID, map[BlockID][]BlockContext{
				e.ID: e.Contexts,
			})
		if err != nil {
			return false, err
		}

	case archiveRefsOp:
		// TODO: Combine multiple archive refs.
		err = bserver.ArchiveBlockReferences(
			ctx, tlfID, map[BlockID][]BlockContext{
				e.ID: e.Contexts,
			})
		if err != nil {
			return false, err
		}

	default:
		return false, fmt.Errorf("Unknown op %s", e.Op)
	}

	err = j.j.removeEarliest()
	if err != nil {
		return false, err
	}

	return true, nil
}
