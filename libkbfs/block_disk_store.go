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

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"golang.org/x/net/context"
)

// blockDiskStore stores blocks along with their associated data in
// flat files in a directory on disk.
//
// The directory layout looks like:
//
// dir/0100/0...01/data
// dir/0100/0...01/key_server_half
// dir/0100/0...01/refs
// ...
// dir/01ff/f...ff/data
// dir/01ff/f...ff/key_server_half
// dir/01ff/f...ff/refs
//
// Each block has its own subdirectory with its ID truncated to 17
// bytes (34 characters) as a name. The block subdirectories are
// splayed over (# of possible hash types) * 256 subdirectories -- one
// byte for the hash type (currently only one) plus the first byte of
// the hash data -- using the first four characters of the name to
// keep the number of directories in dir itself to a manageable
// number, similar to git. Each block directory has data, which is the
// raw block data that should hash to the block ID, key_server_half,
// which contains the raw data for the associated key server half, and
// refs, which contains the list of references to the block. Future
// versions of the disk store might add more files to this directory;
// if any code is written to move blocks around, it should be careful
// to preserve any unknown files in a block directory.
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

	log      logger.Logger
	deferLog logger.Logger
}

// makeBlockDiskStore returns a new blockDiskStore for the given
// directory.
func makeBlockDiskStore(codec kbfscodec.Codec, crypto cryptoPure,
	dir string, log logger.Logger) *blockDiskStore {
	deferLog := log.CloneWithAddedDepth(1)
	return &blockDiskStore{
		codec:    codec,
		crypto:   crypto,
		dir:      dir,
		log:      log,
		deferLog: deferLog,
	}
}

// The functions below are for building various paths.

func (s *blockDiskStore) blockPath(id BlockID) string {
	// Truncate to 34 characters, which corresponds to 16 random
	// bytes (since the first byte is a hash type) or 128 random
	// bits, which means that the expected number of blocks
	// generated before getting a path collision is 2^64 (see
	// https://en.wikipedia.org/wiki/Birthday_problem#Cast_as_a_collision_problem
	// ). The full ID can be recovered just by hashing the data
	// again with the same hash type.
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
	status blockRefStatus, tag interface{}) error {
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
	id BlockID, contexts []BlockContext, tag interface{}) (
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
	fileInfos, err := ioutil.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	res := make(map[BlockID]blockRefMap)
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

func (s *blockDiskStore) getAll() (
	map[BlockID]map[BlockRefNonce]blockRefStatus, error) {
	allRefs, err := s.getAllRefs()
	if err != nil {
		return nil, err
	}

	res := make(map[BlockID]map[BlockRefNonce]blockRefStatus)
	for id, refs := range allRefs {
		res[id] = refs.getStatuses()
	}
	return res, nil
}

func (s *blockDiskStore) putData(
	ctx context.Context, id BlockID, context BlockContext, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf,
	tag interface{}) (err error) {
	s.log.CDebugf(ctx, "Putting %d bytes of data for block %s with context %v",
		len(buf), id, context)
	defer func() {
		if err != nil {
			s.deferLog.CDebugf(ctx,
				"Put for block %s with context %v failed with %v",
				id, context, err)
		}
	}()

	err = validateBlockServerPut(s.crypto, id, context, buf)
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

	err = ioutil.WriteFile(s.dataPath(id), buf, 0600)
	if err != nil {
		return err
	}

	// TODO: Add integrity-checking for key server half?

	data, err := serverHalf.MarshalBinary()
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
	ctx context.Context, id BlockID, context BlockContext, tag interface{}) (
	err error) {
	s.log.CDebugf(ctx, "Adding reference for block %s with context %v",
		id, context)
	defer func() {
		if err != nil {
			s.deferLog.CDebugf(ctx,
				"Adding reference for block %s with context %v failed with %v",
				id, context, err)
		}
	}()

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

	return s.addContexts(id, []BlockContext{context}, liveBlockRef, tag)
}

// removeReferences fixes up the in-memory reference map to delete the
// given references.
func (s *blockDiskStore) removeReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext,
	tag interface{}) (
	liveCounts map[BlockID]int, err error) {
	s.log.CDebugf(ctx, "Removing references for %v", contexts)
	defer func() {
		if err != nil {
			s.deferLog.CDebugf(ctx,
				"Removing references for %v", contexts, err)
		}
	}()

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

func (s *blockDiskStore) archiveReferences(
	ctx context.Context, contexts map[BlockID][]BlockContext,
	tag interface{}) (err error) {
	s.log.CDebugf(ctx, "Archiving references for %v", contexts)
	defer func() {
		if err != nil {
			s.deferLog.CDebugf(ctx,
				"Archiving references for %v,", contexts, err)
		}
	}()

	for id, idContexts := range contexts {
		err = s.addContexts(id, idContexts, archivedBlockRef, tag)
		if err != nil {
			return err
		}
	}

	return nil
}
