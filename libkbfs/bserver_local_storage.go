// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
)

type blockRefLocalStatus int

const (
	noBlockRef blockRefLocalStatus = iota
	liveBlockRef
	archivedBlockRef
)

type blockEntry struct {
	// These fields are only exported for serialization purposes.
	BlockData     []byte
	Refs          map[BlockRefNonce]blockRefLocalStatus
	KeyServerHalf BlockCryptKeyServerHalf
}

// bserverLocalStorage abstracts the various methods of storing blocks
// for bserverLocal.
type bserverLocalStorage interface {
	get(id BlockID) (blockEntry, error)
	getAll() (map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error)
	put(id BlockID, entry blockEntry) error
	addReference(id BlockID, refNonce BlockRefNonce) error
	removeReference(id BlockID, refNonce BlockRefNonce) (int, error)
	archiveReference(id BlockID, refNonce BlockRefNonce) error
}

// bserverMemStorage stores block data in an in-memory map.
type bserverMemStorage struct {
	lock sync.RWMutex
	m    map[BlockID]blockEntry
}

var _ bserverLocalStorage = (*bserverMemStorage)(nil)

func makeBserverMemStorage() *bserverMemStorage {
	return &bserverMemStorage{m: make(map[BlockID]blockEntry)}
}

func (s *bserverMemStorage) get(id BlockID) (blockEntry, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	entry, ok := s.m[id]
	if !ok {
		return blockEntry{}, BServerErrorBlockNonExistent{}
	}
	return entry, nil
}

func (s *bserverMemStorage) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)
	s.lock.RLock()
	defer s.lock.RUnlock()

	for id, entry := range s.m {
		res[id] = make(map[BlockRefNonce]blockRefLocalStatus)
		for ref, status := range entry.Refs {
			res[id][ref] = status
		}
	}
	return res, nil
}

func (s *bserverMemStorage) put(id BlockID, entry blockEntry) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.m[id] = entry
	return nil
}

func (s *bserverMemStorage) addReference(id BlockID, refNonce BlockRefNonce) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	entry, ok := s.m[id]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
			"exist and cannot be referenced.", id)}
	}

	// only add it if there's a non-archived reference
	for _, status := range entry.Refs {
		if status == liveBlockRef {
			entry.Refs[refNonce] = liveBlockRef
			s.m[id] = entry
			return nil
		}
	}
	return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
		"been archived and cannot be referenced.", id)}
}

func (s *bserverMemStorage) removeReference(id BlockID, refNonce BlockRefNonce) (
	liveCount int, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	entry, ok := s.m[id]
	if !ok {
		// This block is already gone; no error.
		return 0, nil
	}

	delete(entry.Refs, refNonce)
	if len(entry.Refs) == 0 {
		delete(s.m, id)
	} else {
		s.m[id] = entry
	}
	liveCount = len(entry.Refs)
	return liveCount, nil
}

func (s *bserverMemStorage) archiveReference(id BlockID, refNonce BlockRefNonce) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	entry, ok := s.m[id]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s doesn't "+
			"exist and cannot be archived.", id)}
	}

	_, ok = entry.Refs[refNonce]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
			"doesn't exist and cannot be archived.", id, refNonce)}
	}

	entry.Refs[refNonce] = archivedBlockRef
	s.m[id] = entry
	return nil
}

// bserverFileStorage stores block data in flat files on disk.
type bserverFileStorage struct {
	codec Codec
	lock  sync.RWMutex
	dir   string
}

var _ bserverLocalStorage = (*bserverFileStorage)(nil)

func makeBserverFileStorage(codec Codec, dir string) *bserverFileStorage {
	return &bserverFileStorage{codec: codec, dir: dir}
}

// Store each block in its own file with name equal to the hex-encoded
// blockID. Splay the filenames over 256^2 subdirectories (one byte
// for the hash type plus the first byte of the hash data) using the
// first four characters of the name to keep the number of directories
// in dir itself to a manageable number, similar to git.
func (s *bserverFileStorage) buildPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(s.dir, idStr[:4], idStr[4:])
}

func (s *bserverFileStorage) getLocked(p string) (blockEntry, error) {
	buf, err := ioutil.ReadFile(p)
	if err != nil {
		return blockEntry{}, err
	}

	var entry blockEntry
	err = s.codec.Decode(buf, &entry)
	if err != nil {
		return blockEntry{}, err
	}

	return entry, nil
}

func (s *bserverFileStorage) get(id BlockID) (blockEntry, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	entry, err := s.getLocked(s.buildPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			err = BServerErrorBlockNonExistent{}
		}
		return blockEntry{}, err
	}
	return entry, nil
}

func (s *bserverFileStorage) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	res := make(map[BlockID]map[BlockRefNonce]blockRefLocalStatus)
	s.lock.RLock()
	defer s.lock.RUnlock()
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

			filePath := filepath.Join(subDir, fileInfo.Name())
			entry, err := s.getLocked(filePath)
			if err != nil {
				return nil, err
			}
			res[id] = entry.Refs
		}
	}

	return res, nil
}

func (s *bserverFileStorage) putLocked(p string, entry blockEntry) error {
	entryBuf, err := s.codec.Encode(entry)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(p), 0700)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(p, entryBuf, 0600)
}

func (s *bserverFileStorage) put(id BlockID, entry blockEntry) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.putLocked(s.buildPath(id), entry)
}

func (s *bserverFileStorage) addReference(id BlockID, refNonce BlockRefNonce) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	p := s.buildPath(id)
	entry, err := s.getLocked(p)
	if err != nil {
		if os.IsNotExist(err) {
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
				"doesn't exist and cannot be referenced.", id)}
		}
		return err
	}

	// only add it if there's a non-archived reference
	for _, status := range entry.Refs {
		if status == liveBlockRef {
			entry.Refs[refNonce] = liveBlockRef
			return s.putLocked(p, entry)
		}
	}

	return BServerErrorBlockArchived{fmt.Sprintf("Block ID %s has "+
		"been archived and cannot be referenced.", id)}
}

func (s *bserverFileStorage) removeReference(id BlockID, refNonce BlockRefNonce) (
	liveCount int, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	p := s.buildPath(id)
	entry, err := s.getLocked(p)
	if err != nil {
		if os.IsNotExist(err) {
			// This block is already gone; no error.
			return 0, nil
		}
		return -1, err
	}

	delete(entry.Refs, refNonce)
	if len(entry.Refs) == 0 {
		return 0, os.Remove(p)
	}
	return len(entry.Refs), s.putLocked(p, entry)
}

func (s *bserverFileStorage) archiveReference(id BlockID, refNonce BlockRefNonce) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	p := s.buildPath(id)
	entry, err := s.getLocked(p)
	if err != nil {
		if os.IsNotExist(err) {
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
				"doesn't exist and cannot be archived.", id)}
		}
		return err
	}

	_, ok := entry.Refs[refNonce]
	if !ok {
		return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s (ref %s) "+
			"doesn't exist and cannot be archived.", id, refNonce)}
	}

	entry.Refs[refNonce] = archivedBlockRef
	return s.putLocked(p, entry)
}
