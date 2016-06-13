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
	"strconv"
	"sync"
)

// bserverTlfJournal stores an ordered list of BlockServer mutating
// operations for a single TLF, along with associated block data, in
// flat files in a directory on disk.
//
// The directory layout looks like:
//
// dir/journal/EARLIEST
// dir/journal/LATEST
// dir/journal/0...000
// dir/journal/0...001
// dir/journal/0...fff
// dir/blocks/0100/0...01/data
// dir/blocks/0100/0...01/key_server_half
// ...
// dir/blocks/01ff/f...ff/data
// dir/blocks/01ff/f...ff/key_server_half
//
// Each file in dir/journal is named with an ordinal and contains the
// mutating operation and arguments for a single operation, except for
// block data. The files EARLIEST and LATEST point to the earliest and
// latest valid ordinal, respectively.
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
// TODO: Do all high-level operations atomically on the file-system
// level.
//
// TODO: Make IO ops cancellable.
//
// TODO: Add a mode which doesn't assume that this is the only storage
// for TLF data, i.e. that doesn't remove files when the (known)
// refcount drops to zero, etc.
type bserverTlfJournal struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well as refs and isShutdown.
	//
	// TODO: Consider using https://github.com/pkg/singlefile
	// instead.
	lock       sync.RWMutex
	refs       map[BlockID]blockRefMap
	isShutdown bool
}

func makeBserverTlfJournal(
	codec Codec, crypto cryptoPure, dir string) (*bserverTlfJournal, error) {
	bserver := &bserverTlfJournal{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
	}

	refs, err := bserver.getRefEntriesJournalLocked()
	if err != nil {
		return nil, err
	}

	bserver.refs = refs
	return bserver, nil
}

func (s *bserverTlfJournal) buildBlocksPath() string {
	return filepath.Join(s.dir, "blocks")
}

func (s *bserverTlfJournal) buildPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(s.buildBlocksPath(), idStr[:4], idStr[4:])
}

func (s *bserverTlfJournal) buildDataPath(id BlockID) string {
	return filepath.Join(s.buildPath(id), "data")
}

func (s *bserverTlfJournal) buildKeyServerHalfPath(id BlockID) string {
	return filepath.Join(s.buildPath(id), "key_server_half")
}

func (s *bserverTlfJournal) getJournalPath() string {
	return filepath.Join(s.dir, "journal")
}

func (s *bserverTlfJournal) getEarliestPath() string {
	return filepath.Join(s.getJournalPath(), "EARLIEST")
}

func (s *bserverTlfJournal) getLatestPath() string {
	return filepath.Join(s.getJournalPath(), "LATEST")
}

func (s *bserverTlfJournal) getEntryPath(o uint64) string {
	return filepath.Join(s.getJournalPath(), ordinalToStr(o))
}

func ordinalToStr(o uint64) string {
	return fmt.Sprintf("%016x", o)
}

func strToOrdinal(s string) (uint64, error) {
	if len(s) != 16 {
		return 0, fmt.Errorf("%q has invalid format", s)
	}
	return strconv.ParseUint(s, 16, 64)
}

func (s *bserverTlfJournal) getOrdinalLocked(path string) (uint64, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strToOrdinal(string(buf))
}

func (s *bserverTlfJournal) putOrdinalLocked(path string, o uint64) error {
	str := ordinalToStr(o)
	return ioutil.WriteFile(path, []byte(str), 0600)
}

func (s *bserverTlfJournal) getEarliestOrdinalLocked() (uint64, error) {
	return s.getOrdinalLocked(s.getEarliestPath())
}

func (s *bserverTlfJournal) putEarliestOrdinalLocked(currOrdinal uint64) error {
	return s.putOrdinalLocked(s.getEarliestPath(), currOrdinal)
}

func (s *bserverTlfJournal) getLatestOrdinalLocked() (uint64, error) {
	return s.getOrdinalLocked(s.getLatestPath())
}

func (s *bserverTlfJournal) putLatestOrdinalLocked(currOrdinal uint64) error {
	return s.putOrdinalLocked(s.getLatestPath(), currOrdinal)
}

type bserverJournalEntry struct {
	Op       string
	ID       BlockID
	Contexts []BlockContext
}

func (s *bserverTlfJournal) getJournalEntryLocked(o uint64) (bserverJournalEntry, error) {
	p := s.getEntryPath(o)
	buf, err := ioutil.ReadFile(p)
	if err != nil {
		return bserverJournalEntry{}, err
	}

	var e bserverJournalEntry
	err = s.codec.Decode(buf, &e)
	if err != nil {
		return bserverJournalEntry{}, err
	}

	return e, nil
}

func (s *bserverTlfJournal) putJournalEntryLocked(o uint64, e bserverJournalEntry) error {
	err := os.MkdirAll(s.getJournalPath(), 0700)
	if err != nil {
		return err
	}

	p := s.getEntryPath(o)

	buf, err := s.codec.Encode(e)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(p, buf, 0600)
}

func (s *bserverTlfJournal) addJournalEntryLocked(
	op string, id BlockID, contexts []BlockContext) error {
	var next uint64
	o, err := s.getLatestOrdinalLocked()
	if os.IsNotExist(err) {
		next = 0
	} else if err != nil {
		return err
	} else {
		next = o + 1
	}

	err = s.putJournalEntryLocked(next, bserverJournalEntry{
		Op:       op,
		ID:       id,
		Contexts: contexts,
	})
	if err != nil {
		return err
	}

	_, err = s.getEarliestOrdinalLocked()
	if os.IsNotExist(err) {
		s.putEarliestOrdinalLocked(next)
	} else if err != nil {
		return err
	}
	return s.putLatestOrdinalLocked(next)
}

func (s *bserverTlfJournal) journalLength() (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	first, err := s.getEarliestOrdinalLocked()
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	last, err := s.getLatestOrdinalLocked()
	if err != nil {
		return 0, err
	}
	return last - first + 1, nil
}

func (s *bserverTlfJournal) getRefEntryLocked(
	id BlockID, refNonce BlockRefNonce) (blockRefEntry, error) {
	refs := s.refs[id]
	if refs == nil {
		return blockRefEntry{}, BServerErrorBlockNonExistent{}
	}

	e, ok := refs[refNonce]
	if !ok {
		return blockRefEntry{}, BServerErrorBlockNonExistent{}
	}

	return e, nil
}

var errBserverTlfJournalShutdown = errors.New("bserverTlfJournal is shutdown")

func (s *bserverTlfJournal) getDataLocked(id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	if s.isShutdown {
		return nil, BlockCryptKeyServerHalf{},
			errBserverTlfJournalShutdown
	}

	refEntry, err := s.getRefEntryLocked(id, context.GetRefNonce())
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	err = refEntry.checkContext(context)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	data, err := ioutil.ReadFile(s.buildDataPath(id))
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	dataID, err := s.crypto.MakePermanentBlockID(data)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	if id != dataID {
		return nil, BlockCryptKeyServerHalf{}, fmt.Errorf(
			"Block ID mismatch: expected %s, got %s", id, dataID)
	}

	keyServerHalfPath := s.buildKeyServerHalfPath(id)
	buf, err := ioutil.ReadFile(keyServerHalfPath)
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	var serverHalf BlockCryptKeyServerHalf
	err = serverHalf.UnmarshalBinary(buf)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	return data, serverHalf, nil
}

func (s *bserverTlfJournal) getData(id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.getDataLocked(id, context)
}

func (s *bserverTlfJournal) getRefEntriesJournalLocked() (
	map[BlockID]blockRefMap, error) {
	refs := make(map[BlockID]blockRefMap)

	first, err := s.getEarliestOrdinalLocked()
	if os.IsNotExist(err) {
		return refs, nil
	} else if err != nil {
		return nil, err
	}
	last, err := s.getLatestOrdinalLocked()
	if err != nil {
		return nil, err
	}

	for i := first; i <= last; i++ {
		e, err := s.getJournalEntryLocked(i)
		if err != nil {
			return nil, err
		}

		if refs[e.ID] == nil {
			refs[e.ID] = make(blockRefMap)
		}

		switch e.Op {
		case "put", "addReference":
			refs[e.ID].put(e.Contexts[0], liveBlockRef)

		case "removeReferences":
			for _, context := range e.Contexts {
				delete(refs[e.ID], context.GetRefNonce())
			}

		case "archiveReferences":
			for _, context := range e.Contexts {
				refs[e.ID].put(context, archivedBlockRef)
			}

		default:
			return nil, fmt.Errorf("Unknown op %s", e.Op)
		}
	}
	return refs, nil
}

func (s *bserverTlfJournal) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, errBserverTlfJournalShutdown
	}

	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)

	for id, refs := range s.refs {
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

func (s *bserverTlfJournal) putRefEntryLocked(
	id BlockID, refEntry blockRefEntry) error {
	existingRefEntry, err := s.getRefEntryLocked(
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

	if s.refs[id] == nil {
		s.refs[id] = make(blockRefMap)
	}

	s.refs[id].put(refEntry.Context, refEntry.Status)
	return nil
}

func (s *bserverTlfJournal) putData(
	id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	err := validateBlockServerPut(s.crypto, id, context, buf)
	if err != nil {
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfJournalShutdown
	}

	_, existingServerHalf, err := s.getDataLocked(id, context)
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

	err = os.MkdirAll(s.buildPath(id), 0700)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.buildDataPath(id), buf, 0600)
	if err != nil {
		return err
	}

	// TODO: Add integrity-checking for key server half?

	err = ioutil.WriteFile(s.buildKeyServerHalfPath(id), serverHalf.data[:], 0600)
	if err != nil {
		return err
	}

	err = s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
	if err != nil {
		return err
	}

	return s.addJournalEntryLocked("put", id, []BlockContext{context})
}

func (s *bserverTlfJournal) addReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfJournalShutdown
	}

	refs := s.refs[id]
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

	// We allow adding a reference even if all the existing
	// references are archived, or if we have no references at
	// all. This is because we can't be sure that there are other
	// references we don't know about.
	//
	// TODO: Figure out what to do with an addReference without a
	// preceding Put.

	err := s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
	if err != nil {
		return err
	}

	return s.addJournalEntryLocked("addReference", id, []BlockContext{context})
}

func (s *bserverTlfJournal) removeReferences(
	id BlockID, contexts []BlockContext) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return 0, errBserverTlfJournalShutdown
	}

	refs := s.refs[id]
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
	if count == 0 {
		err := os.RemoveAll(s.buildPath(id))
		if err != nil {
			return 0, err
		}
	}

	// TODO: Figure out what to do with live count.

	err := s.addJournalEntryLocked("removeReferences", id, contexts)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (s *bserverTlfJournal) archiveReferences(id BlockID, contexts []BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfJournalShutdown
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		refEntry, err := s.getRefEntryLocked(id, refNonce)
		switch err.(type) {
		case BServerErrorBlockNonExistent:
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
				"doesn't exist and cannot be archived.", id, refNonce)}
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
		err = s.putRefEntryLocked(id, refEntry)
		if err != nil {
			return err
		}
	}

	// TODO: Coalesce into one entry.
	return s.addJournalEntryLocked("archiveReferences", id, contexts)
}

func (s *bserverTlfJournal) shutdown() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.isShutdown = true

	refs, err := s.getRefEntriesJournalLocked()
	if err != nil {
		panic(err)
	}

	if !reflect.DeepEqual(refs, s.refs) {
		panic(fmt.Sprintf("refs = %v != s.refs = %v", refs, s.refs))
	}
}
