// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// TODO: Have bserverTlfStorage store a journal of put operations.

// TODO: Do all high-level operations atomically on the file-system
// level.

// bserverTlfStorage stores block data for a single TLF in flat files
// in a directory on disk. More specifically, for each block in a TLF,
// it stores data for that block in its own subdirectory.
//
// The block ID name is splayed over (# of possible hash types) * 256
// subdirectories -- one byte for the hash type (currently only one)
// plus the first byte of the hash data -- using the first four
// characters of the name to keep the number of directories in dir
// itself to a manageable number, similar to git.
//
// An example directory structure would be:
//
// dir/blocks/0100/0...01/data
// dir/blocks/0100/0...01/key_server_half
// dir/blocks/0100/0...01/refs/0000000000000000
// dir/blocks/0100/0...01/refs/0000000000000001
// ...
// dir/blocks/01ff/f...ff/data
// dir/blocks/01ff/f...ff/key_server_half
// dir/blocks/01ff/f...ff/refs/0000000000000000
// dir/blocks/01ff/f...ff/refs/ffffffffffffffff
type bserverTlfStorage struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well as refs and isShutdown.
	lock       sync.RWMutex
	refs       map[BlockID]blockRefMap
	isShutdown bool
}

func makeBserverTlfStorage(
	codec Codec, crypto CryptoPure, dir string) *bserverTlfStorage {
	return &bserverTlfStorage{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
		refs:   make(map[BlockID]blockRefMap),
	}
}

func (s *bserverTlfStorage) buildBlocksPath() string {
	return filepath.Join(s.dir, "blocks")
}

func (s *bserverTlfStorage) buildPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(s.buildBlocksPath(), idStr[:4], idStr[4:])
}

func (s *bserverTlfStorage) buildDataPath(id BlockID) string {
	return filepath.Join(s.buildPath(id), "data")
}

func (s *bserverTlfStorage) buildKeyServerHalfPath(id BlockID) string {
	return filepath.Join(s.buildPath(id), "key_server_half")
}

func (s *bserverTlfStorage) buildRefsPath(id BlockID) string {
	return filepath.Join(s.buildPath(id), "refs")
}

func (s *bserverTlfStorage) buildRefPath(id BlockID, refNonce BlockRefNonce) string {
	refNonceStr := refNonce.String()
	return filepath.Join(s.buildRefsPath(id), refNonceStr)
}

func (s *bserverTlfStorage) getJournalPath() string {
	return filepath.Join(s.dir, "journal")
}

func (s *bserverTlfStorage) getEarliestPath() string {
	return filepath.Join(s.getJournalPath(), "EARLIEST")
}

func (s *bserverTlfStorage) getLatestPath() string {
	return filepath.Join(s.getJournalPath(), "LATEST")
}

func (s *bserverTlfStorage) getEntryPath(o uint64) string {
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

func (s *bserverTlfStorage) getOrdinalLocked(path string) (uint64, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strToOrdinal(string(buf))
}

func (s *bserverTlfStorage) putOrdinalLocked(path string, o uint64) error {
	str := ordinalToStr(o)
	return ioutil.WriteFile(path, []byte(str), 0600)
}

func (s *bserverTlfStorage) getEarliestOrdinalLocked() (uint64, error) {
	return s.getOrdinalLocked(s.getEarliestPath())
}

func (s *bserverTlfStorage) putEarliestOrdinalLocked(currOrdinal uint64) error {
	return s.putOrdinalLocked(s.getEarliestPath(), currOrdinal)
}

func (s *bserverTlfStorage) getLatestOrdinalLocked() (uint64, error) {
	return s.getOrdinalLocked(s.getLatestPath())
}

func (s *bserverTlfStorage) putLatestOrdinalLocked(currOrdinal uint64) error {
	return s.putOrdinalLocked(s.getLatestPath(), currOrdinal)
}

type bserverJournalEntry struct {
	Op      string
	ID      BlockID
	Context BlockContext
}

func (s *bserverTlfStorage) getJournalEntryLocked(o uint64) (bserverJournalEntry, error) {
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

func (s *bserverTlfStorage) putJournalEntryLocked(o uint64, e bserverJournalEntry) error {
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

func (s *bserverTlfStorage) addJournalEntryLocked(
	op string, id BlockID, context BlockContext) error {
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
		Op:      op,
		ID:      id,
		Context: context,
	})
	if err != nil {
		return err
	}

	_, err = s.getEarliestOrdinalLocked()
	if os.IsNotExist(err) {
		s.putEarliestOrdinalLocked(next)
	}
	s.putLatestOrdinalLocked(next)
	return nil
}

func (s *bserverTlfStorage) getRefEntryLocked(
	id BlockID, refNonce BlockRefNonce) (blockRefEntry, error) {
	buf, err := ioutil.ReadFile(s.buildRefPath(id, refNonce))
	if err != nil {
		// Let caller handle os.IsNotExist(err) case.
		return blockRefEntry{}, err
	}

	var refEntry blockRefEntry
	err = s.codec.Decode(buf, &refEntry)
	if err != nil {
		return blockRefEntry{}, err
	}

	return refEntry, nil
}

var errBserverTlfStorageShutdown = errors.New("bserverTlfStorage is shutdown")

func (s *bserverTlfStorage) getDataLocked(id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	if s.isShutdown {
		return nil, BlockCryptKeyServerHalf{},
			errBserverTlfStorageShutdown
	}

	refEntry, err := s.getRefEntryLocked(id, context.GetRefNonce())
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
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

func (s *bserverTlfStorage) getData(id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.getDataLocked(id, context)
}

func (s *bserverTlfStorage) getRefEntriesLocked(id BlockID) (
	map[BlockRefNonce]blockRefEntry, error) {
	refsPath := s.buildRefsPath(id)
	// Let caller handle os.IsNotExist(err) case.
	refInfos, err := ioutil.ReadDir(refsPath)
	if err != nil {
		return nil, err
	}

	refs := make(map[BlockRefNonce]blockRefEntry)
	for _, refInfo := range refInfos {
		var refNonce BlockRefNonce
		buf, err := hex.DecodeString(refInfo.Name())
		if err != nil {
			return nil, err
		}
		if len(buf) != len(refNonce) {
			return nil, fmt.Errorf(
				"Invalid ref nonce file %s", refInfo.Name())
		}
		copy(refNonce[:], buf)

		refEntry, err := s.getRefEntryLocked(id, refNonce)
		if err != nil {
			return nil, err
		}
		refs[refNonce] = refEntry
	}

	return refs, nil
}

func (s *bserverTlfStorage) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, errBserverTlfStorageShutdown
	}

	blocksPath := s.buildBlocksPath()
	levelOneInfos, err := ioutil.ReadDir(blocksPath)
	if err != nil {
		return nil, err
	}

	// Ignore non-dirs while traversing below.

	// TODO: Tighten up checks below if we ever use this for
	// anything other than tests.

	for _, levelOneInfo := range levelOneInfos {
		if !levelOneInfo.IsDir() {
			continue
		}

		levelOneDir := filepath.Join(blocksPath, levelOneInfo.Name())
		levelTwoInfos, err := ioutil.ReadDir(levelOneDir)
		if err != nil {
			return nil, err
		}

		for _, levelTwoInfo := range levelTwoInfos {
			if !levelTwoInfo.IsDir() {
				continue
			}

			idStr := levelOneInfo.Name() + levelTwoInfo.Name()
			id, err := BlockIDFromString(idStr)
			if err != nil {
				return nil, err
			}

			_, ok := res[id]
			if ok {
				return nil, fmt.Errorf(
					"Multiple dir entries for block %s", id)
			}

			res[id] = make(map[BlockRefNonce]blockRefLocalStatus)

			refEntries, err := s.getRefEntriesLocked(id)
			if err != nil {
				return nil, err
			}

			for ref, refEntry := range refEntries {
				res[id][ref] = refEntry.Status
			}
		}
	}

	return res, nil
}

func (s *bserverTlfStorage) putRefEntryLocked(
	id BlockID, refEntry blockRefEntry) error {
	existingRefEntry, err := s.getRefEntryLocked(
		id, refEntry.Context.GetRefNonce())
	var exists bool
	switch {
	case os.IsNotExist(err):
		exists = false
	case err == nil:
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

	buf, err := s.codec.Encode(refEntry)
	if err != nil {
		return err
	}

	refPath := s.buildRefPath(id, refEntry.Context.GetRefNonce())
	err = ioutil.WriteFile(refPath, buf, 0600)
	if err != nil {
		return err
	}

	if s.refs[id] == nil {
		s.refs[id] = make(blockRefMap)
	}

	s.refs[id].put(refEntry.Context, refEntry.Status)
	return nil
}

func (s *bserverTlfStorage) putData(
	id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	err := validateBlockServerPut(s.crypto, id, context, buf)
	if err != nil {
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfStorageShutdown
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

	// Do this first, so that it makes the dirs for the data and
	// key server half files.
	err = os.MkdirAll(s.buildRefsPath(id), 0700)
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

	return s.addJournalEntryLocked("put", id, context)
}

func (s *bserverTlfStorage) addReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfStorageShutdown
	}

	refEntries, err := s.getRefEntriesLocked(id)
	if os.IsNotExist(err) {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
			"doesn't exist and cannot be referenced.", id)}
	} else if err != nil {
		return err
	}

	// Only add it if there's a non-archived reference.
	hasNonArchivedRef := false
	for _, refEntry := range refEntries {
		if refEntry.Status == liveBlockRef {
			hasNonArchivedRef = true
			break
		}
	}
	if !hasNonArchivedRef {
		return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
			"been archived and cannot be referenced.", id)}
	}

	err = s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
	if err != nil {
		return err
	}

	return s.addJournalEntryLocked("addReference", id, context)
}

func (s *bserverTlfStorage) removeReferences(
	id BlockID, contexts []BlockContext) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return 0, errBserverTlfStorageShutdown
	}

	refEntries, err := s.getRefEntriesLocked(id)
	if os.IsNotExist(err) {
		// This block is already gone; no error.
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		// If this check fails, this ref is already gone,
		// which is not an error.
		if refEntry, ok := refEntries[refNonce]; ok {
			err := refEntry.checkContext(context)
			if err != nil {
				return 0, err
			}

			refPath := s.buildRefPath(id, refNonce)
			err = os.RemoveAll(refPath)
			if err != nil {
				return 0, err
			}
			delete(refEntries, refNonce)
		}
	}

	count := len(refEntries)
	if count == 0 {
		err := os.RemoveAll(s.buildPath(id))
		if err != nil {
			return 0, err
		}
	}

	// TODO: Coalesce into one entry.
	for _, context := range contexts {
		err := s.addJournalEntryLocked("removeReference", id, context)
		if err != nil {
			return 0, err
		}
	}

	return count, nil
}

func (s *bserverTlfStorage) archiveReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return errBserverTlfStorageShutdown
	}

	refNonce := context.GetRefNonce()
	refEntry, err := s.getRefEntryLocked(id, refNonce)
	if os.IsNotExist(err) {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
			"doesn't exist and cannot be archived.", id, refNonce)}
	} else if err != nil {
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

	// TODO: Coalesce into one entry.
	return s.addJournalEntryLocked("archiveReference", id, context)
}

func (s *bserverTlfStorage) shutdown() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.isShutdown = true

	first, err := s.getEarliestOrdinalLocked()
	if os.IsNotExist(err) {
		return
	} else if err != nil {
		panic(err)
	}
	last, err := s.getLatestOrdinalLocked()
	if err != nil {
		panic(err)
	}

	for i := first; i <= last; i++ {
		_, err := s.getJournalEntryLocked(i)
		if err != nil {
			panic(err)
		}
	}
}
