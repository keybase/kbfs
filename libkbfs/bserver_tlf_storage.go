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
	"sync"
)

// bserverTlfStorage stores block data for a single TLF in flat files
// on disk. In particular, it stores data for each block in a
// directory with name equal to the hex-encoded blockID.
//
// The block ID name is splayed over 256^2 subdirectories (one byte
// for the hash type plus the first byte of the hash data) using the
// first four characters of the name to keep the number of directories
// in dir itself to a manageable number, similar to git.
//
// An example directory structure would be:
//
// dir/0100/0...01/data
// dir/0100/0...01/key_server_half
// dir/0100/0...01/refs/0000000000000000
// dir/0100/0...01/refs/0000000000000001
// ...
// dir/01ff/f...ff/data
// dir/01ff/f...ff/key_server_half
// dir/01ff/f...ff/refs/0000000000000000
// dir/01ff/f...ff/refs/ffffffffffffffff
type bserverTlfStorage struct {
	codec  Codec
	crypto Crypto
	dir    string

	// Protects any IO operations in dir or any of its children,
	// as well as isShutdown.
	lock       sync.RWMutex
	isShutdown bool
}

func makeBserverTlfStorage(
	codec Codec, crypto Crypto, dir string) *bserverTlfStorage {
	return &bserverTlfStorage{codec: codec, crypto: crypto, dir: dir}
}

func (s *bserverTlfStorage) buildPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(s.dir, idStr[:4], idStr[4:])
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

var bserverTlfStorageShutdownErr = errors.New("bserverTlfStorage is shutdown")

func (s *bserverTlfStorage) getData(id BlockID, context BlockContext) (
	[]byte, BlockCryptKeyServerHalf, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, BlockCryptKeyServerHalf{},
			bserverTlfStorageShutdownErr
	}

	refEntry, err := s.getRefEntryLocked(id, context.GetRefNonce())
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	if refEntry.Context != context {
		return nil, BlockCryptKeyServerHalf{},
			fmt.Errorf("Context mismatch: expected %s, got %s",
				refEntry.Context, context)
	}

	data, err := ioutil.ReadFile(s.buildDataPath(id))
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	keyServerHalfPath := s.buildKeyServerHalfPath(id)
	buf, err := ioutil.ReadFile(keyServerHalfPath)
	if os.IsNotExist(err) {
		return nil, BlockCryptKeyServerHalf{},
			BServerErrorBlockNonExistent{}
	} else if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}

	var serverHalfData [32]byte
	if len(buf) != len(serverHalfData) {
		return nil, BlockCryptKeyServerHalf{},
			fmt.Errorf("Corrupt server half data file %s",
				keyServerHalfPath)
	}
	copy(serverHalfData[:], buf)
	serverHalf := MakeBlockCryptKeyServerHalf(serverHalfData)
	return data, serverHalf, nil
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
		return nil, bserverTlfStorageShutdownErr
	}

	levelOneInfos, err := ioutil.ReadDir(s.dir)
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

		levelOneDir := filepath.Join(s.dir, levelOneInfo.Name())
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

			refs, err := s.getRefEntriesLocked(id)
			if err != nil {
				return nil, err
			}

			for ref, refEntry := range refs {
				res[id][ref] = refEntry.Status
			}
		}
	}

	return res, nil
}

func (s *bserverTlfStorage) putRefEntryLocked(
	id BlockID, refEntry blockRefEntry) error {
	buf, err := s.codec.Encode(refEntry)
	if err != nil {
		return err
	}
	// TODO: Add integrity-checking?
	refPath := s.buildRefPath(id, refEntry.Context.GetRefNonce())
	return ioutil.WriteFile(refPath, buf, 0600)
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
		return bserverTlfStorageShutdownErr
	}

	// TODO: Do the below atomically.

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

	return s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
}

func (s *bserverTlfStorage) addReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return bserverTlfStorageShutdownErr
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

	return s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
}

func (s *bserverTlfStorage) removeReferences(
	id BlockID, contexts []BlockContext) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return 0, bserverTlfStorageShutdownErr
	}

	refEntries, err := s.getRefEntriesLocked(id)
	if os.IsNotExist(err) {
		// This block is already gone; no error.
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	if len(refEntries) == 0 {
		// This block is already gone; no error.
		return 0, nil
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		// If this check fails, this ref is already gone,
		// which is not an error.
		if refEntry, ok := refEntries[refNonce]; ok {
			if refEntry.Context != context {
				return 0, fmt.Errorf(
					"Context mismatch: expected %s, got %s",
					refEntry.Context, context)
			}
			refPath := s.buildRefPath(id, refNonce)
			err := os.RemoveAll(refPath)
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
	return count, nil
}

func (s *bserverTlfStorage) archiveReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return bserverTlfStorageShutdownErr
	}

	refNonce := context.GetRefNonce()
	refEntry, err := s.getRefEntryLocked(id, refNonce)
	if os.IsNotExist(err) {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
			"doesn't exist and cannot be archived.", id, refNonce)}
	} else if err != nil {
		return err
	}

	if refEntry.Context != context {
		return fmt.Errorf("Context mismatch: expected %s, got %s",
			refEntry.Context, context)
	}

	refEntry.Status = archivedBlockRef
	return s.putRefEntryLocked(id, refEntry)
}

func (s *bserverTlfStorage) shutdown() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.isShutdown = true
}
