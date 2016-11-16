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
// dir/0100/0...01/ksh
// dir/0100/0...01/refs
// ...
// dir/01cc/5...55/id
// dir/01cc/5...55/refs
// ...
// dir/01dd/6...66/data
// dir/01dd/6...66/id
// dir/01dd/6...66/ksh
// ...
// dir/01ff/f...ff/data
// dir/01ff/f...ff/id
// dir/01ff/f...ff/ksh
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
//   - id:   The full block ID in binary format. Always present.
//   - data: The raw block data that should hash to the block ID.
//           May be missing.
//   - ksh:  The raw data for the associated key server half.
//           May be missing, but should be present when data is.
//   - refs: The list of references to the block, encoded as a serialized
//           blockRefMap. May be missing.
//
// Future versions of the disk store might add more files to this
// directory; if any code is written to move blocks around, it should
// be careful to preserve any unknown files in a block directory.
//
// The maximum number of characters added to the root dir by a block
// disk store is 44:
//
//   /01ff/f...(30 characters total)...ff/data
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

func (s *blockDiskStore) aggregateInfoPath() string {
	return filepath.Join(s.dir, "aggregate_info")
}

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

const flushInfoFilename = "flushInfo"

func (s *blockDiskStore) flushInfoPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), flushInfoFilename)
}

const idFilename = "id"

func (s *blockDiskStore) idPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), idFilename)
}

func (s *blockDiskStore) keyServerHalfPath(id BlockID) string {
	return filepath.Join(s.blockPath(id), "ksh")
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

// TODO: Support unknown fields.
type aggregateInfo struct {
	UnflushedBytes int64
}

func (s *blockDiskStore) getAggregateInfo() (aggregateInfo, error) {
	data, err := ioutil.ReadFile(s.aggregateInfoPath())
	if os.IsNotExist(err) {
		return aggregateInfo{}, nil
	} else if err != nil {
		return aggregateInfo{}, err
	}

	var info aggregateInfo
	err = s.codec.Decode(data, &info)
	if err != nil {
		return aggregateInfo{}, err
	}

	return info, nil
}

func (s *blockDiskStore) setAggregateInfo(info aggregateInfo) error {
	buf, err := s.codec.Encode(info)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.aggregateInfoPath(), buf, 0600)
	if err != nil {
		return err
	}

	return nil
}

func (s *blockDiskStore) adjustUnflushedBytes(n int64) error {
	info, err := s.getAggregateInfo()
	if err != nil {
		return err
	}

	info.UnflushedBytes += n

	return s.setAggregateInfo(info)
}

// TODO: Support unknown fields.
type flushInfo struct {
	UnflushedPutCount int
}

func (s *blockDiskStore) getFlushInfoHelper(path string) (flushInfo, error) {
	data, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		return flushInfo{}, nil
	} else if err != nil {
		return flushInfo{}, err
	}

	var info flushInfo
	err = s.codec.Decode(data, &info)
	if err != nil {
		return flushInfo{}, err
	}

	return info, nil
}

func (s *blockDiskStore) getFlushInfo(id BlockID) (flushInfo, error) {
	return s.getFlushInfoHelper(s.flushInfoPath(id))
}

func (s *blockDiskStore) setFlushInfo(id BlockID, info flushInfo) error {
	if info == (flushInfo{}) {
		err := os.Remove(s.flushInfoPath(id))
		if os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}

		return nil
	}

	buf, err := s.codec.Encode(info)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(s.flushInfoPath(id), buf, 0600)
	if err != nil {
		return err
	}

	return nil
}

func (s *blockDiskStore) incPutCount(id BlockID) error {
	info, err := s.getFlushInfo(id)
	if err != nil {
		return err
	}

	info.UnflushedPutCount++
	return s.setFlushInfo(id, info)
}

func (s *blockDiskStore) decPutCount(id BlockID) error {
	info, err := s.getFlushInfo(id)
	if err != nil {
		return err
	}

	if info.UnflushedPutCount == 0 {
		return fmt.Errorf(
			"Trying to decrement put count for %s below 0", id)
	}

	info.UnflushedPutCount--
	return s.setFlushInfo(id, info)
}

func (s *blockDiskStore) clearPutCount(id BlockID) error {
	info, err := s.getFlushInfo(id)
	if err != nil {
		return err
	}

	info.UnflushedPutCount = 0
	return s.setFlushInfo(id, info)
}

// TODO: Add caching for refs

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

func (s *blockDiskStore) walkBlockDirs(
	walkFn func(name, subName string) error) error {
	fileInfos, err := ioutil.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	for _, fi := range fileInfos {
		name := fi.Name()
		if !fi.IsDir() {
			// Ignore non-dirs.
			continue
		}

		subFileInfos, err := ioutil.ReadDir(filepath.Join(s.dir, name))
		if err != nil {
			return err
		}

		for _, sfi := range subFileInfos {
			subName := sfi.Name()
			if !sfi.IsDir() {
				// Ignore non-dirs.
				return err
			}

			err := walkFn(name, subName)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// All functions below are public functions.

// getUnflushedBytes returns the sum of the size of the data for each
// stored block that needs to be flushed.
func (s *blockDiskStore) getUnflushedBytes() (int64, error) {
	var totalSize int64
	err := s.walkBlockDirs(func(name, subName string) error {
		dir := filepath.Join(s.dir, name, subName)
		flushInfo, err := s.getFlushInfoHelper(
			filepath.Join(dir, flushInfoFilename))
		if err != nil {
			return err
		}

		if flushInfo.UnflushedPutCount > 0 {
			info, err := os.Stat(filepath.Join(dir, dataFilename))
			if os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}

			totalSize += info.Size() * int64(flushInfo.UnflushedPutCount)
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
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
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

	err := s.walkBlockDirs(func(name, subName string) error {
		idPath := filepath.Join(s.dir, name, subName, idFilename)
		idBytes, err := ioutil.ReadFile(idPath)
		if err != nil {
			return err
		}

		id, err := BlockIDFromString(string(idBytes))
		if err != nil {
			return err
		}

		if !strings.HasPrefix(id.String(), name+subName) {
			return fmt.Errorf(
				"%q unexpectedly not a prefix of %q", name+subName, id.String())
		}

		refs, err := s.getRefs(id)
		if err != nil {
			return err
		}

		if len(refs) > 0 {
			res[id] = refs
		}

		return nil
	})
	if err != nil {
		return nil, err
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
	} else {
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
	}

	err = s.incPutCount(id)
	if err != nil {
		return false, err
	}

	err = s.addRefs(id, []BlockContext{context}, liveBlockRef, tag)
	if err != nil {
		return false, err
	}

	err = s.adjustUnflushedBytes(int64(len(buf)))
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

func (s *blockDiskStore) flushPut(id BlockID) error {
	size, err := s.getDataSize(id)
	if err != nil {
		return err
	}

	err = s.decPutCount(id)
	if err != nil {
		return err
	}

	return s.adjustUnflushedBytes(-size)
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
