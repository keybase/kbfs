// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"golang.org/x/net/context"
)

// blockDiskStore stores a single ordered list of block operations for a
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
// blockDiskStore is not goroutine-safe, so any code that uses it must
// guarantee that only one goroutine at a time calls its functions.
type blockDiskStore struct {
	codec  kbfscodec.Codec
	crypto cryptoPure
	dir    string

	log      logger.Logger
	deferLog logger.Logger

	j diskJournal
}

// A blockDiskStoreEntry is just the name of the operation and the
// associated block ID and contexts. Fields are exported only for
// serialization.
type blockDiskStoreEntry struct {
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
func (e blockDiskStoreEntry) getSingleContext() (
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

// makeBlockDiskStore returns a new blockDiskStore for the given
// directory. Any existing journal entries are read.
func makeBlockDiskStore(
	ctx context.Context, codec kbfscodec.Codec, crypto cryptoPure,
	dir string, log logger.Logger) (*blockDiskStore, error) {
	journalPath := filepath.Join(dir, "block_journal")
	deferLog := log.CloneWithAddedDepth(1)
	j := makeDiskJournal(
		codec, journalPath, reflect.TypeOf(blockDiskStoreEntry{}))
	journal := &blockDiskStore{
		codec:    codec,
		crypto:   crypto,
		dir:      dir,
		log:      log,
		deferLog: deferLog,
		j:        j,
	}

	return journal, nil
}

// The functions below are for building various non-journal paths.

func (j *blockDiskStore) blocksPath() string {
	return filepath.Join(j.dir, "blocks")
}

func (j *blockDiskStore) blockPath(id BlockID) string {
	// Truncate to 34 characters, which corresponds to 16 random
	// bytes (since the first byte is a hash type) or 128 random
	// bits, which means that the expected number of blocks
	// generated before getting a path collision is 2^64 (see
	// https://en.wikipedia.org/wiki/Birthday_problem#Cast_as_a_collision_problem
	// ). The full ID can be recovered just by hashing the data
	// again with the same hash type.
	idStr := id.String()
	return filepath.Join(j.blocksPath(), idStr[:4], idStr[4:34])
}

func (j *blockDiskStore) blockDataPath(id BlockID) string {
	return filepath.Join(j.blockPath(id), "data")
}

func (j *blockDiskStore) keyServerHalfPath(id BlockID) string {
	return filepath.Join(j.blockPath(id), "key_server_half")
}

func (j *blockDiskStore) refsPath(id BlockID) string {
	return filepath.Join(j.blockPath(id), "refs")
}

// The functions below are for reading and writing journal entries.

func (j *blockDiskStore) readJournalEntry(ordinal journalOrdinal) (
	blockDiskStoreEntry, error) {
	entry, err := j.j.readJournalEntry(ordinal)
	if err != nil {
		return blockDiskStoreEntry{}, err
	}

	return entry.(blockDiskStoreEntry), nil
}

// readJournal reads the journal and returns a map of all the block
// references in the journal and the total number of bytes that need
// flushing.
func (j *blockDiskStore) readJournal(ctx context.Context) (
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

			err = blockRefs.put(context, liveBlockRef, i)
			if err != nil {
				return nil, err
			}

			// Only puts count as bytes, on the assumption that the
			// refs won't have to upload any new bytes.  (This might
			// be wrong if all references to a block were deleted
			// since the addref entry was appended.)
			if e.Op == blockPutOp && !e.Ignore {
				// Ignore ENOENT errors, since users like
				// BlockServerDisk can remove block data without
				// deleting the corresponding addRef.
				if err != nil && !os.IsNotExist(err) {
					return nil, err
				}
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
					err := blockRefs.remove(context, nil)
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
						context, archivedBlockRef, i)
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

func (j *blockDiskStore) appendJournalEntry(entry blockDiskStoreEntry) (
	journalOrdinal, error) {
	return j.j.appendJournalEntry(nil, entry)
}

func (j *blockDiskStore) length() (uint64, error) {
	return j.j.length()
}

func (j *blockDiskStore) end() (journalOrdinal, error) {
	last, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	return last + 1, nil
}

func (j *blockDiskStore) getRefs(id BlockID) (blockRefMap, error) {
	data, err := ioutil.ReadFile(j.refsPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var refs blockRefMap
	err = j.codec.Decode(data, &refs)
	if err != nil {
		return nil, err
	}

	return refs, nil
}

func (j *blockDiskStore) putContext(
	id BlockID, context BlockContext,
	status blockRefStatus, ordinal journalOrdinal) error {
	refs, err := j.getRefs(id)
	if err != nil {
		return err
	}

	if refs == nil {
		refs = make(blockRefMap)
	} else {
		// Check existing context, if any.
		_, err := refs.checkExists(context)
		if err != nil {
			return err
		}
	}

	err = refs.put(context, status, ordinal)
	if err != nil {
		return err
	}

	buf, err := j.codec.Encode(refs)
	if err != nil {
		return err
	}

	err = os.MkdirAll(j.blockPath(id), 0700)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(j.refsPath(id), buf, 0600)
}

func (j *blockDiskStore) removeContexts(
	id BlockID, contexts []BlockContext, ordinal *journalOrdinal) (
	liveCount int, err error) {
	refs, err := j.getRefs(id)
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, nil
	}

	var tag interface{}
	if ordinal != nil {
		tag = *ordinal
	}

	for _, context := range contexts {
		err := refs.remove(context, tag)
		if err != nil {
			return 0, err
		}
		if len(refs) == 0 {
			break
		}
	}

	if len(refs) == 0 {
		err = os.Remove(j.refsPath(id))
		if err != nil {
			return 0, err
		}
	} else {
		buf, err := j.codec.Encode(refs)
		if err != nil {
			return 0, err
		}

		err = ioutil.WriteFile(j.refsPath(id), buf, 0600)
		if err != nil {
			return 0, err
		}
	}

	return len(refs), nil
}

func (j *blockDiskStore) getData(id BlockID) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	data, err := ioutil.ReadFile(j.blockDataPath(id))
	if os.IsNotExist(err) {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	} else if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	keyServerHalfPath := j.keyServerHalfPath(id)
	buf, err := ioutil.ReadFile(keyServerHalfPath)
	if os.IsNotExist(err) {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	} else if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	// Check integrity.

	dataID, err := j.crypto.MakePermanentBlockID(data)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	if id != dataID {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, fmt.Errorf(
			"Block ID mismatch: expected %s, got %s", id, dataID)
	}

	var serverHalf kbfscrypto.BlockCryptKeyServerHalf
	err = serverHalf.UnmarshalBinary(buf)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	return data, serverHalf, nil
}

// All functions below are public functions.

func (j *blockDiskStore) hasRef(id BlockID) (bool, error) {
	refs, err := j.getRefs(id)
	if err != nil {
		return false, err
	}

	return len(refs) > 0, nil
}

func (j *blockDiskStore) hasNonArchivedRef(id BlockID) (bool, error) {
	refs, err := j.getRefs(id)
	if err != nil {
		return false, err
	}

	return (refs != nil) && refs.hasNonArchivedRef(), nil
}

func (j *blockDiskStore) hasContext(id BlockID, context BlockContext) (
	bool, error) {
	refs, err := j.getRefs(id)
	if err != nil {
		return false, err
	}

	if len(refs) == 0 {
		return false, nil
	}

	return refs.checkExists(context)
}

func (j *blockDiskStore) getDataWithContext(id BlockID, context BlockContext) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	hasContext, err := j.hasContext(id, context)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}
	if !hasContext {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	}

	return j.getData(id)
}

func (j *blockDiskStore) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefStatus, error) {
	fileInfos, err := ioutil.ReadDir(j.blocksPath())
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	res := make(map[BlockID]map[BlockRefNonce]blockRefStatus)
	for _, fi := range fileInfos {
		name := fi.Name()
		if !fi.IsDir() {
			return nil, fmt.Errorf("Unexpected non-dir %q", name)
		}

		subFileInfos, err := ioutil.ReadDir(filepath.Join(j.blocksPath(), name))
		if err != nil {
			return nil, err
		}

		for _, sfi := range subFileInfos {
			subName := sfi.Name()
			if !sfi.IsDir() {
				return nil, fmt.Errorf("Unexpected non-dir %q",
					subName)
			}

			id, err := BlockIDFromString(name + subName)
			if err != nil {
				return nil, err
			}

			refs, err := j.getRefs(id)
			if err != nil {
				return nil, err
			}

			res[id] = refs.getStatuses()
		}
	}
	return res, nil
}

func (j *blockDiskStore) putData(
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

	err = validateBlockServerPut(j.crypto, id, context, buf)
	if err != nil {
		return err
	}

	// Check the data and retrieve the server half, if they exist.
	_, existingServerHalf, err := j.getDataWithContext(id, context)
	var exists bool
	switch err.(type) {
	case blockNonExistentError:
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

		// We checked that both buf and the existing data hash
		// to id, so no need to check that they're both equal.

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

	data, err := serverHalf.MarshalBinary()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(
		j.keyServerHalfPath(id), data, 0600)
	if err != nil {
		return err
	}

	ordinal, err := j.appendJournalEntry(blockDiskStoreEntry{
		Op:       blockPutOp,
		Contexts: map[BlockID][]BlockContext{id: {context}},
	})
	if err != nil {
		return err
	}

	return j.putContext(id, context, liveBlockRef, ordinal)
}

func (j *blockDiskStore) addReference(
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

	ordinal, err := j.appendJournalEntry(blockDiskStoreEntry{
		Op:       addRefOp,
		Contexts: map[BlockID][]BlockContext{id: {context}},
	})
	if err != nil {
		return err
	}

	return j.putContext(id, context, liveBlockRef, ordinal)
}

// removeReferences fixes up the in-memory reference map to delete the
// given references.
func (j *blockDiskStore) removeReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	j.log.CDebugf(ctx, "Removing references for %v", contexts)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Removing references for %v", contexts, err)
		}
	}()

	liveCounts = make(map[BlockID]int)

	for id, idContexts := range contexts {
		liveCount, err := j.removeContexts(id, idContexts, nil)
		if err != nil {
			return nil, err
		}
		liveCounts[id] = liveCount
	}

	_, err = j.appendJournalEntry(blockDiskStoreEntry{
		Op:       removeRefsOp,
		Contexts: contexts,
	})
	if err != nil {
		return nil, err
	}

	return liveCounts, nil
}

// removeBlockData removes any existing block data for the given
// ID. If there is no data, nil is returned; this can happen when we
// have only non-put references to a block in the journal.
func (j *blockDiskStore) removeBlockData(id BlockID) error {
	hasRef, err := j.hasRef(id)
	if err != nil {
		return err
	}
	if hasRef {
		return fmt.Errorf(
			"Trying to remove data for referenced block %s", id)
	}
	path := j.blockPath(id)

	err = os.RemoveAll(path)
	if err != nil {
		return err
	}

	// Remove the parent (splayed) directory if it exists and is
	// empty.
	err = os.Remove(filepath.Dir(path))
	if os.IsNotExist(err) || isExist(err) {
		err = nil
	}
	return err
}

func (j *blockDiskStore) archiveReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext) (err error) {
	j.log.CDebugf(ctx, "Archiving references for %v", contexts)
	defer func() {
		if err != nil {
			j.deferLog.CDebugf(ctx,
				"Archiving references for %v,", contexts, err)
		}
	}()

	ordinal, err := j.appendJournalEntry(blockDiskStoreEntry{
		Op:       archiveRefsOp,
		Contexts: contexts,
	})
	if err != nil {
		return err
	}

	for id, idContexts := range contexts {
		for _, context := range idContexts {
			err = j.putContext(
				id, context, archivedBlockRef, ordinal)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
