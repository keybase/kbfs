// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
)

// blockDiskStore stores block data in flat files on disk.
//
// The directory layout looks like:
//
// dir/0100/0...01/data
// dir/0100/0...01/id
// dir/0100/0...01/key_server_half
// dir/0100/0...01/refs
// ...
// dir/01cc/5...55/id
// dir/01cc/5...55/refs
// ...
// dir/01dd/6...66/data
// dir/01dd/6...66/id
// dir/01dd/6...66/key_server_half
// ...
// dir/01ff/f...ff/data
// dir/01ff/f...ff/id
// dir/01ff/f...ff/key_server_half
// dir/01ff/f...ff/refs
//
// Each block has its own subdirectory with its ID truncated to 17
// bytes (34 characters) as a name. The block subdirectories are
// splayed over (# of possible hash types) * 256 subdirectories -- one
// byte for the hash type (currently only one) plus the first byte of
// the hash data -- using the first four characters of the name to
// keep the number of directories in dir itself to a manageable
// number, similar to git.
//
// Each block directory has the following files:
//
//   - id: The full block ID in binary format. Always present.
//   - data: The raw block data that should hash to the block ID.
//           May be missing.
//   - key_server_half: The raw data for the associated key server half.
//                      May be missing, but should be present when data is.
//   - refs: The list of references to the block, encoded as a serialized
//           blockRefMap. May be missing.
//
// Future versions of the disk store might add more files to this
// directory; if any code is written to move blocks around, it should
// be careful to preserve any unknown files in a block directory.
//
// The maximum number of characters added to the root dir by a block
// disk store is 52:
//
//   /01ff/f...(30 characters total)...ff/key_server_half
//
// blockDiskStore is not goroutine-safe, so any code that uses it must
// guarantee that only one goroutine at a time calls its functions.
type blockDiskStore struct {
	codec  kbfscodec.Codec
	crypto cryptoPure
	dir    string
}

// makeBlockDiskStore returns a new blockDiskStore for the given
// directory.
func makeBlockDiskStore(codec kbfscodec.Codec, crypto cryptoPure,
	dir string) *blockDiskStore {
	return &blockDiskStore{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
	}
}

// The functions below are for building various paths.

func (s *blockDiskStore) blockPath(id BlockID) string {
	// Truncate to 34 characters, which corresponds to 16 random
	// bytes (since the first byte is a hash type) or 128 random
	// bits, which means that the expected number of blocks
	// generated before getting a path collision is 2^64 (see
	// https://en.wikipedia.org/wiki/Birthday_problem#Cast_as_a_collision_problem
	// ).
	idStr := id.String()
	return filepath.Join(s.dir, idStr[:4], idStr[4:34])
}

const dataFilename = "data"

func (s *blockDiskStore) dataPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), dataFilename)
}

const idFilename = "id"

func (s *blockDiskStore) idPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), idFilename)
}

func (s *blockDiskStore) keyServerHalfPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "key_server_half")
}

func (s *blockDiskStore) refsPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "refs")
}

// makeDir makes the directory for the given block ID and writes the
// ID file, if necessary.
func (s *blockDiskStore) makeDir(id BlockID) error {
	err := os.MkdirAll(s.blockPath(id), 0700)
	if err != nil {
		return err
	}

	// TODO: Only write if the file doesn't exist.

	err = ioutil.WriteFile(s.idPath(id), []byte(id.String()), 0600)
	if err != nil {
		return err
	}

	return nil
}

// TODO: Add caching for refs.

// getRefs returns the references for the given ID.
func (s *blockDiskStore) getRefs(id BlockID) (blockRefMap, error) {
	data, err := ioutil.ReadFile(s.refsPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var refs blockRefMap
	err = s.codec.Decode(data, &refs)
	if err != nil {
		return nil, err
	}

	return refs, nil
}

// putRefs stores the given references for the given ID. If the given
// map is empty, it removes the reference file.
func (s *blockDiskStore) putRefs(id BlockID, refs blockRefMap) error {
	if len(refs) == 0 {
		err := os.Remove(s.refsPath(id))
		if err != nil {
			return err
		}

		return nil
	}

	buf, err := s.codec.Encode(refs)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.refsPath(id), buf, 0600)
	if err != nil {
		return err
	}

	return nil
}

// addRefs adds references for the given contexts to the given ID, all
// with the same status and tag.
func (s *blockDiskStore) addRefs(id BlockID, contexts []BlockContext,
	status blockRefStatus, tag string) error {
	refs, err := s.getRefs(id)
	if err != nil {
		return err
	}

	if refs == nil {
		refs = make(blockRefMap)
	} else {
		// Check existing contexts, if any.
		for _, context := range contexts {
			_, err := refs.checkExists(context)
			if err != nil {
				return err
			}
		}
	}

	for _, context := range contexts {
		err = refs.put(context, status, tag)
		if err != nil {
			return err
		}
	}

	return s.putRefs(id, refs)
}

// getData returns the data and server half for the given ID, if
// present.
func (s *blockDiskStore) getData(id BlockID) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	data, err := ioutil.ReadFile(s.dataPath(id))
	if os.IsNotExist(err) {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	} else if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	keyServerHalfPath := s.keyServerHalfPath(id)
	buf, err := ioutil.ReadFile(keyServerHalfPath)
	if os.IsNotExist(err) {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	} else if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}

	// Check integrity.

	dataID, err := s.crypto.MakePermanentBlockID(data)
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

// getTotalDataSize returns the sum of the size of the data for each
// stored block.
func (s *blockDiskStore) getTotalDataSize() (int64, error) {
	var totalSize int64
	err := filepath.Walk(s.dir,
		func(path string, info os.FileInfo, err error) error {
			// The root directory doesn't exist, so just
			// exit early and return 0.
			if path == s.dir && os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if filepath.Base(path) == dataFilename {
				totalSize += info.Size()
			}
			return nil
		})
	if err != nil {
		return 0, err
	}
	return totalSize, nil
}

func (s *blockDiskStore) hasAnyRef(id BlockID) (bool, error) {
	refs, err := s.getRefs(id)
	if err != nil {
		return false, err
	}

	return len(refs) > 0, nil
}

func (s *blockDiskStore) hasNonArchivedRef(id BlockID) (bool, error) {
	refs, err := s.getRefs(id)
	if err != nil {
		return false, err
	}

	return (refs != nil) && refs.hasNonArchivedRef(), nil
}

func (s *blockDiskStore) hasContext(id BlockID, context BlockContext) (
	bool, error) {
	refs, err := s.getRefs(id)
	if err != nil {
		return false, err
	}

	if len(refs) == 0 {
		return false, nil
	}

	return refs.checkExists(context)
}

func (s *blockDiskStore) hasData(id BlockID) error {
	_, err := os.Stat(s.dataPath(id))
	return err
}

func (s *blockDiskStore) getDataSize(id BlockID) (int64, error) {
	fi, err := os.Stat(s.dataPath(id))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (s *blockDiskStore) getDataWithContext(id BlockID, context BlockContext) (
	[]byte, kbfscrypto.BlockCryptKeyServerHalf, error) {
	hasContext, err := s.hasContext(id, context)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}
	if !hasContext {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{},
			blockNonExistentError{id}
	}

	return s.getData(id)
}

func (s *blockDiskStore) getAllRefsForTest() (map[BlockID]blockRefMap, error) {
	res := make(map[BlockID]blockRefMap)

	fileInfos, err := ioutil.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return res, nil
	} else if err != nil {
		return nil, err
	}

	for _, fi := range fileInfos {
		name := fi.Name()
		if !fi.IsDir() {
			return nil, fmt.Errorf("Unexpected non-dir %q", name)
		}

		subFileInfos, err := ioutil.ReadDir(filepath.Join(s.dir, name))
		if err != nil {
			return nil, err
		}

		for _, sfi := range subFileInfos {
			subName := sfi.Name()
			if !sfi.IsDir() {
				return nil, fmt.Errorf("Unexpected non-dir %q",
					subName)
			}

			idPath := filepath.Join(
				s.dir, name, subName, idFilename)
			idBytes, err := ioutil.ReadFile(idPath)
			if err != nil {
				return nil, err
			}

			id, err := BlockIDFromString(string(idBytes))
			if err != nil {
				return nil, err
			}

			if !strings.HasPrefix(id.String(), name+subName) {
				return nil, fmt.Errorf(
					"%q unexpectedly not a prefix of %q", name+subName, id.String())
			}

			refs, err := s.getRefs(id)
			if err != nil {
				return nil, err
			}

			if len(refs) > 0 {
				res[id] = refs
			}
		}
	}
	return res, nil
}

// put puts the given data for the block, which may already exist, and
// adds a reference for the given context.
func (s *blockDiskStore) put(id BlockID, context BlockContext, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf,
	tag string) (didPut bool, err error) {
	err = validateBlockPut(s.crypto, id, context, buf)
	if err != nil {
		return false, err
	}

	// Check the data and retrieve the server half, if they exist.
	_, existingServerHalf, err := s.getDataWithContext(id, context)
	var exists bool
	switch err.(type) {
	case blockNonExistentError:
		exists = false
	case nil:
		exists = true
	default:
		return false, err
	}

	if exists {
		// If the entry already exists, everything should be
		// the same, except for possibly additional
		// references.

		// We checked that both buf and the existing data hash
		// to id, so no need to check that they're both equal.

		if existingServerHalf != serverHalf {
			return false, fmt.Errorf(
				"key server half mismatch: expected %s, got %s",
				existingServerHalf, serverHalf)
		}
	}

	err = s.makeDir(id)
	if err != nil {
		return false, err
	}

	err = ioutil.WriteFile(s.dataPath(id), buf, 0600)
	if err != nil {
		return false, err
	}

	// TODO: Add integrity-checking for key server half?

	data, err := serverHalf.MarshalBinary()
	if err != nil {
		return false, err
	}
	err = ioutil.WriteFile(s.keyServerHalfPath(id), data, 0600)
	if err != nil {
		return false, err
	}

	err = s.addRefs(id, []BlockContext{context}, liveBlockRef, tag)
	if err != nil {
		return false, err
	}

	return !exists, nil
}

func (s *blockDiskStore) addReference(
	id BlockID, context BlockContext, tag string) error {
	err := s.makeDir(id)
	if err != nil {
		return err
	}

	return s.addRefs(id, []BlockContext{context}, liveBlockRef, tag)
}

func (s *blockDiskStore) archiveReferences(
	contexts map[BlockID][]BlockContext, tag string) error {
	for id, idContexts := range contexts {
		err := s.makeDir(id)
		if err != nil {
			return err
		}

		err = s.addRefs(id, idContexts, archivedBlockRef, tag)
		if err != nil {
			return err
		}
	}

	return nil
}

// removeReferences removes references for the given contexts from
// their respective IDs. If tag is non-empty, then a reference will be
// removed only if its most recent tag (passed in to addRefs) matches
// the given one.
func (s *blockDiskStore) removeReferences(
	id BlockID, contexts []BlockContext, tag string) (
	liveCount int, err error) {
	refs, err := s.getRefs(id)
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, nil
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

	err = s.putRefs(id, refs)
	if err != nil {
		return 0, err
	}

	return len(refs), nil
}

// remove removes any existing data for the given ID, which must not
// have any references left.
func (s *blockDiskStore) remove(id BlockID) error {
	hasAnyRef, err := s.hasAnyRef(id)
	if err != nil {
		return err
	}
	if hasAnyRef {
		return fmt.Errorf(
			"Trying to remove data for referenced block %s", id)
	}
	path := s.blockPath(id)

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
