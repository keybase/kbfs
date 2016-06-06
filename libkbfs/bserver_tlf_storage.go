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
	codec Codec
	dir   string

	// Protects any IO operations in dir or any of its children,
	// as well as isShutdown.
	lock       sync.RWMutex
	isShutdown bool
}

func makeBserverTlfStorage(codec Codec, dir string) *bserverTlfStorage {
	return &bserverTlfStorage{codec: codec, dir: dir}
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
		return blockRefEntry{}, err
	}

	var refEntry blockRefEntry
	err = s.codec.Decode(buf, &refEntry)
	if err != nil {
		return blockRefEntry{}, err
	}

	return refEntry, nil
}

func (s *bserverTlfStorage) getRefEntryCountLocked(id BlockID) (int, error) {
	refInfos, err := ioutil.ReadDir(s.buildRefsPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	// TODO: Do more checking.
	return len(refInfos), nil
}

func (s *bserverTlfStorage) getRefsLocked(id BlockID) (map[BlockRefNonce]blockRefEntry, error) {
	refsPath := s.buildRefsPath(id)
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
		// TODO: Validate length.
		copy(refNonce[:], buf)

		refEntry, err := s.getRefEntryLocked(id, refNonce)
		if err != nil {
			return nil, err
		}
		refs[refNonce] = refEntry
	}

	return refs, nil
}

var bserverTlfStorageShutdownErr = errors.New("bserverTlfStorage is shutdown")

func (s *bserverTlfStorage) get(id BlockID) ([]byte, BlockCryptKeyServerHalf, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, BlockCryptKeyServerHalf{}, bserverTlfStorageShutdownErr
	}

	data, err := ioutil.ReadFile(s.buildDataPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			err = BServerErrorBlockNonExistent{}
		}
		return nil, BlockCryptKeyServerHalf{}, err
	}

	buf, err := ioutil.ReadFile(s.buildKeyServerHalfPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			err = BServerErrorBlockNonExistent{}
		}
		return nil, BlockCryptKeyServerHalf{}, err
	}

	var kshData [32]byte
	// TODO: Validate length.
	copy(kshData[:], buf)
	serverHalf := MakeBlockCryptKeyServerHalf(kshData)
	return data, serverHalf, nil
}

func (s *bserverTlfStorage) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.isShutdown {
		return nil, bserverTlfStorageShutdownErr
	}

	subdirInfos, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	for _, subdirInfo := range subdirInfos {
		if !subdirInfo.IsDir() {
			continue
		}

		subDir := filepath.Join(s.dir, subdirInfo.Name())
		fileInfos, err := ioutil.ReadDir(subDir)
		if err != nil {
			return nil, err
		}

		for _, fileInfo := range fileInfos {
			idStr := subdirInfo.Name() + fileInfo.Name()
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

			refs, err := s.getRefsLocked(id)
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

func (s *bserverTlfStorage) putRefEntryLocked(id BlockID, refEntry blockRefEntry) error {
	buf, err := s.codec.Encode(refEntry)
	if err != nil {
		return err
	}
	refPath := s.buildRefPath(id, refEntry.Context.GetRefNonce())
	return ioutil.WriteFile(refPath, buf, 0600)
}

func (s *bserverTlfStorage) put(id BlockID, context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return bserverTlfStorageShutdownErr
	}

	err := os.MkdirAll(s.buildRefsPath(id), 0700)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.buildDataPath(id), buf, 0600)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.buildKeyServerHalfPath(id), serverHalf.data[:], 0600)
	if err != nil {
		return err
	}

	return s.putRefEntryLocked(id, blockRefEntry{
		Status:  liveBlockRef,
		Context: context,
	})
}

func (s *bserverTlfStorage) hasNonArchivedReferenceLocked(id BlockID) (bool, error) {
	refsPath := s.buildRefsPath(id)
	refInfos, err := ioutil.ReadDir(refsPath)
	if err != nil {
		return false, err
	}

	for _, refInfo := range refInfos {
		var refNonce BlockRefNonce
		buf, err := hex.DecodeString(refInfo.Name())
		if err != nil {
			return false, err
		}
		// TODO: Validate length.
		copy(refNonce[:], buf)

		refEntry, err := s.getRefEntryLocked(id, refNonce)
		if err != nil {
			return false, err
		}
		if refEntry.Status == liveBlockRef {
			return true, nil
		}
	}
	return false, nil
}

func (s *bserverTlfStorage) addReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return bserverTlfStorageShutdownErr
	}

	// Only add it if there's a non-archived reference.
	hasNonArchivedRef, err := s.hasNonArchivedReferenceLocked(id)
	if err != nil {
		if os.IsNotExist(err) {
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
				"doesn't exist and cannot be referenced.", id)}
		}
		return err
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

func (s *bserverTlfStorage) removeRefEntryLocked(id BlockID, refNonce BlockRefNonce) error {
	refPath := s.buildRefPath(id, refNonce)
	return os.RemoveAll(refPath)
}

func (s *bserverTlfStorage) removeReferences(
	id BlockID, contexts []BlockContext) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.isShutdown {
		return 0, bserverTlfStorageShutdownErr
	}

	for _, context := range contexts {
		refNonce := context.GetRefNonce()
		// TODO: Check context before removing.
		err := s.removeRefEntryLocked(id, refNonce)
		if err != nil {
			return 0, err
		}
	}

	count, err := s.getRefEntryCountLocked(id)
	if err != nil {
		return 0, err
	}

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
	if err != nil {
		if os.IsNotExist(err) {
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
				"doesn't exist and cannot be archived.", id, refNonce)}
		}
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
