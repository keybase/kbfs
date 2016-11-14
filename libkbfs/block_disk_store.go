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
	"golang.org/x/net/context"
)

// blockDiskStore stores block data in flat files on disk.
//
// The directory layout looks like:
//
// /0100/0...01/data
// /0100/0...01/id
// /0100/0...01/key_server_half
// /0100/0...01/refs
// ...
// /01cc/5...55/id
// /01cc/5...55/refs
// ...
// /01dd/6...66/data
// /01dd/6...66/id
// /01dd/6...66/key_server_half
// ...
// /01ff/f...ff/data
// /01ff/f...ff/id
// /01ff/f...ff/key_server_half
// /01ff/f...ff/refs
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
//   - id: The full block ID in binary format. (TODO: make
//         human-readable.) Always present.
//   - data: The raw block data that should hash to the block ID.
//           May be missing.
//   - key_server_half: The raw data for the associated key server half.
//                      May be missing, but should be present when data is.
//   - refs: The list of references to the block. May be missing.
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

func (s *blockDiskStore) dataPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "data")
}

func (s *blockDiskStore) idPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "id")
}

func (s *blockDiskStore) keyServerHalfPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "key_server_half")
}

func (s *blockDiskStore) refsPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "refs")
}

// The functions below are for getting and putting refs/contexts.

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

func (s *blockDiskStore) putRefs(id BlockID, refs blockRefMap) error {
	if len(refs) == 0 {
		err := os.Remove(s.refsPath(id))
		if err != nil {
			return err
		}

		return nil
	}

	err := os.MkdirAll(s.blockPath(id), 0700)
	if err != nil {
		return err
	}

	data, err := id.MarshalBinary()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(s.idPath(id), data, 0600)
	if err != nil {
		return err
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

func (s *blockDiskStore) addContexts(
	id BlockID, contexts []BlockContext,
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

func (s *blockDiskStore) removeContexts(
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

func (s *blockDiskStore) hasRef(id BlockID) (bool, error) {
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

func (s *blockDiskStore) getAllRefs() (map[BlockID]blockRefMap, error) {
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

			idPath := filepath.Join(s.dir, name, subName, "id")
			buf, err := ioutil.ReadFile(idPath)
			if err != nil {
				return nil, err
			}

			var id BlockID
			err = id.UnmarshalBinary(buf)
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

func (s *blockDiskStore) putData(
	ctx context.Context, id BlockID, context BlockContext, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf,
	tag string) (err error) {
	err = validateBlockPut(s.crypto, id, context, buf)
	if err != nil {
		return err
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

	err = os.MkdirAll(s.blockPath(id), 0700)
	if err != nil {
		return err
	}

	data, err := id.MarshalBinary()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(s.idPath(id), data, 0600)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.dataPath(id), buf, 0600)
	if err != nil {
		return err
	}

	// TODO: Add integrity-checking for key server half?

	data, err = serverHalf.MarshalBinary()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(s.keyServerHalfPath(id), data, 0600)
	if err != nil {
		return err
	}

	return s.addContexts(id, []BlockContext{context}, liveBlockRef, tag)
}

func (s *blockDiskStore) addReference(
	ctx context.Context, id BlockID, context BlockContext, tag string) (
	err error) {
	return s.addContexts(id, []BlockContext{context}, liveBlockRef, tag)
}

func (s *blockDiskStore) archiveReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext,
	tag string) (err error) {
	for id, idContexts := range contexts {
		err = s.addContexts(id, idContexts, archivedBlockRef, tag)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *blockDiskStore) removeReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext,
	tag string) (
	liveCounts map[BlockID]int, err error) {
	liveCounts = make(map[BlockID]int)

	for id, idContexts := range contexts {
		liveCount, err := s.removeContexts(id, idContexts, tag)
		if err != nil {
			return nil, err
		}
		liveCounts[id] = liveCount
	}

	return liveCounts, nil
}

// removeBlockData removes any existing block data for the given
// ID. If there is no data, nil is returned; this can happen when we
// have only non-put references to a block in the journal.
func (s *blockDiskStore) removeBlockData(id BlockID) error {
	hasRef, err := s.hasRef(id)
	if err != nil {
		return err
	}
	if hasRef {
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
