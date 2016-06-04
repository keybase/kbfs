// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

type blockEntry struct {
	// These fields are only exported for serialization purposes.
	BlockData     []byte
	Refs          map[BlockRefNonce]blockRefEntry
	KeyServerHalf BlockCryptKeyServerHalf
}

// bserverTlfStorage stores block data in flat files on disk.
type bserverTlfStorage struct {
	codec Codec
	lock  sync.RWMutex
	dir   string
}

func makeBserverTlfStorage(codec Codec, dir string) *bserverTlfStorage {
	return &bserverTlfStorage{codec: codec, dir: dir}
}

// Store each block in its own file with name equal to the hex-encoded
// blockID. Splay the filenames over 256^2 subdirectories (one byte
// for the hash type plus the first byte of the hash data) using the
// first four characters of the name to keep the number of directories
// in dir itself to a manageable number, similar to git.
func (s *bserverTlfStorage) buildPath(id BlockID) string {
	idStr := id.String()
	return filepath.Join(s.dir, idStr[:4], idStr[4:])
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

func (s *bserverTlfStorage) getLocked(id BlockID) (blockEntry, error) {
	p := s.buildPath(id)
	dataPath := filepath.Join(p, "data")
	data, err := ioutil.ReadFile(dataPath)
	if err != nil {
		return blockEntry{}, err
	}

	kshPath := filepath.Join(p, "ksh")
	buf, err := ioutil.ReadFile(kshPath)
	if err != nil {
		return blockEntry{}, err
	}

	var kshData [32]byte
	// TODO: Validate length.
	copy(kshData[:], buf)
	ksh := MakeBlockCryptKeyServerHalf(kshData)

	refsPath := s.buildRefsPath(id)
	refInfos, err := ioutil.ReadDir(refsPath)
	if err != nil {
		return blockEntry{}, err
	}

	refs := make(map[BlockRefNonce]blockRefEntry)
	for _, refInfo := range refInfos {
		var refNonce BlockRefNonce
		buf, err := hex.DecodeString(refInfo.Name())
		if err != nil {
			return blockEntry{}, err
		}
		// TODO: Validate length.
		copy(refNonce[:], buf)

		refEntry, err := s.getRefEntryLocked(id, refNonce)
		if err != nil {
			return blockEntry{}, err
		}
		refs[refNonce] = refEntry
	}

	return blockEntry{
		BlockData:     data,
		Refs:          refs,
		KeyServerHalf: ksh,
	}, nil
}

func (s *bserverTlfStorage) get(id BlockID) (blockEntry, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	entry, err := s.getLocked(id)
	if err != nil {
		if os.IsNotExist(err) {
			err = BServerErrorBlockNonExistent{}
		}
		return blockEntry{}, err
	}
	return entry, nil
}

func (s *bserverTlfStorage) getAll() (
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

			entry, err := s.getLocked(id)
			if err != nil {
				return nil, err
			}

			for ref, refEntry := range entry.Refs {
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

func (s *bserverTlfStorage) putLocked(id BlockID, entry blockEntry) error {
	p := s.buildPath(id)
	err := os.RemoveAll(p)
	if err != nil {
		return err
	}

	refsPath := filepath.Join(p, "refs")

	err = os.MkdirAll(refsPath, 0700)
	if err != nil {
		return err
	}

	dataPath := filepath.Join(p, "data")
	err = ioutil.WriteFile(dataPath, entry.BlockData, 0600)
	if err != nil {
		return err
	}

	kshPath := filepath.Join(p, "ksh")
	err = ioutil.WriteFile(kshPath, entry.KeyServerHalf.data[:], 0600)
	if err != nil {
		return err
	}

	for _, refEntry := range entry.Refs {
		err := s.putRefEntryLocked(id, refEntry)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *bserverTlfStorage) put(id BlockID, entry blockEntry) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.putLocked(id, entry)
}

func (s *bserverTlfStorage) addReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	entry, err := s.getLocked(id)
	if err != nil {
		if os.IsNotExist(err) {
			return BServerErrorBlockNonExistent{fmt.Sprintf("Block ID %s "+
				"doesn't exist and cannot be referenced.", id)}
		}
		return err
	}

	// Only add it if there's a non-archived reference.
	hasNonArchivedRef := false
	for _, refEntry := range entry.Refs {
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

func (s *bserverTlfStorage) removeReference(id BlockID, refNonce BlockRefNonce) (
	liveCount int, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	p := s.buildPath(id)
	entry, err := s.getLocked(id)
	if err != nil {
		if os.IsNotExist(err) {
			// This block is already gone; no error.
			return 0, nil
		}
		return -1, err
	}

	delete(entry.Refs, refNonce)
	if len(entry.Refs) == 0 {
		return 0, os.RemoveAll(p)
	}
	return len(entry.Refs), s.putLocked(id, entry)
}

func (s *bserverTlfStorage) archiveReference(id BlockID, context BlockContext) error {
	s.lock.Lock()
	defer s.lock.Unlock()

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

// BlockServerDisk implements the BlockServer interface by just
// storing blocks in a local leveldb instance
type BlockServerDisk struct {
	codec        Codec
	log          logger.Logger
	dirPath      string
	shutdownFunc func()

	tlfStorageLock sync.RWMutex
	tlfStorage     map[TlfID]*bserverTlfStorage
}

var _ BlockServer = (*BlockServerDisk)(nil)

// newBlockServerDisk constructs a new BlockServerDisk that stores
// its data in the given directory.
func newBlockServerDisk(
	config Config, dirPath string, shutdownFunc func()) *BlockServerDisk {
	bserv := &BlockServerDisk{
		config.Codec(),
		config.MakeLogger("BSD"),
		dirPath,
		shutdownFunc,
		sync.RWMutex{},
		make(map[TlfID]*bserverTlfStorage),
	}
	return bserv
}

// NewBlockServerDir constructs a new BlockServerDisk that stores
// its data in the given directory.
func NewBlockServerDir(config Config, dirPath string) *BlockServerDisk {
	return newBlockServerDisk(config, dirPath, nil)
}

// NewBlockServerTempDir constructs a new BlockServerDisk that stores its
// data in a temp directory which is cleaned up on shutdown.
func NewBlockServerTempDir(config Config) (*BlockServerDisk, error) {
	tempdir, err := ioutil.TempDir(os.TempDir(), "kbfs_bserver_tmp")
	if err != nil {
		return nil, err
	}
	return newBlockServerDisk(config, tempdir, func() {
		os.RemoveAll(tempdir)
	}), nil
}

func (b *BlockServerDisk) getStorage(tlfID TlfID) *bserverTlfStorage {
	storage := func() *bserverTlfStorage {
		b.tlfStorageLock.RLock()
		defer b.tlfStorageLock.RUnlock()
		return b.tlfStorage[tlfID]
	}()

	if storage != nil {
		return storage
	}

	b.tlfStorageLock.Lock()
	defer b.tlfStorageLock.Unlock()
	storage = b.tlfStorage[tlfID]
	if storage != nil {
		return storage
	}

	path := filepath.Join(b.dirPath, tlfID.String())
	storage = makeBserverTlfStorage(b.codec, path)
	b.tlfStorage[tlfID] = storage
	return storage
}

// Get implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) Get(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) ([]byte, BlockCryptKeyServerHalf, error) {
	b.log.CDebugf(ctx, "BlockServerDisk.Get id=%s tlfID=%s context=%s",
		id, tlfID, context)
	entry, err := b.getStorage(tlfID).get(id)
	if err != nil {
		return nil, BlockCryptKeyServerHalf{}, err
	}
	return entry.BlockData, entry.KeyServerHalf, nil
}

// Put implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) Put(ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext, buf []byte,
	serverHalf BlockCryptKeyServerHalf) error {
	b.log.CDebugf(ctx, "BlockServerDisk.Put id=%s tlfID=%s context=%s",
		id, tlfID, context)

	if context.GetRefNonce() != zeroBlockRefNonce {
		return fmt.Errorf("Can't Put() a block with a non-zero refnonce.")
	}

	entry := blockEntry{
		BlockData: buf,
		Refs: map[BlockRefNonce]blockRefEntry{
			zeroBlockRefNonce: blockRefEntry{
				Status:  liveBlockRef,
				Context: context,
			},
		},
		KeyServerHalf: serverHalf,
	}
	return b.getStorage(tlfID).put(id, entry)
}

// AddBlockReference implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) AddBlockReference(ctx context.Context, id BlockID,
	tlfID TlfID, context BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerDisk.AddBlockReference id=%s "+
		"tlfID=%s context=%s", id, tlfID, context)
	return b.getStorage(tlfID).addReference(id, context)
}

// RemoveBlockReference implements the BlockServer interface for
// BlockServerDisk
func (b *BlockServerDisk) RemoveBlockReference(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	b.log.CDebugf(ctx, "BlockServerDisk.RemoveBlockReference "+
		"tlfID=%s contexts=%v", tlfID, contexts)
	liveCounts = make(map[BlockID]int)
	for bid, refs := range contexts {
		for _, ref := range refs {
			count, err := b.getStorage(tlfID).removeReference(bid, ref.GetRefNonce())
			if err != nil {
				return liveCounts, err
			}
			existing, ok := liveCounts[bid]
			if !ok || existing > count {
				liveCounts[bid] = count
			}
		}
	}
	return liveCounts, nil
}

// ArchiveBlockReferences implements the BlockServer interface for
// BlockServerDisk
func (b *BlockServerDisk) ArchiveBlockReferences(ctx context.Context,
	tlfID TlfID, contexts map[BlockID][]BlockContext) error {
	b.log.CDebugf(ctx, "BlockServerDisk.ArchiveBlockReferences "+
		"tlfID=%s contexts=%v", tlfID, contexts)

	for id, idContexts := range contexts {
		for _, context := range idContexts {
			err := b.getStorage(tlfID).archiveReference(id, context)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// getAll returns all the known block references, and should only be
// used during testing.
func (b *BlockServerDisk) getAll(tlfID TlfID) (
	map[BlockID]map[BlockRefNonce]blockRefLocalStatus, error) {
	return b.getStorage(tlfID).getAll()
}

// Shutdown implements the BlockServer interface for BlockServerDisk.
func (b *BlockServerDisk) Shutdown() {
	func() {
		b.tlfStorageLock.Lock()
		defer b.tlfStorageLock.Unlock()
		// Make further accesses panic.
		b.tlfStorage = nil
	}()

	if b.shutdownFunc != nil {
		b.shutdownFunc()
	}
}

// RefreshAuthToken implements the BlockServer interface for BlockServerDisk.
func (b *BlockServerDisk) RefreshAuthToken(_ context.Context) {}

// GetUserQuotaInfo implements the BlockServer interface for BlockServerDisk
func (b *BlockServerDisk) GetUserQuotaInfo(ctx context.Context) (info *UserQuotaInfo, err error) {
	// Return a dummy value here.
	return &UserQuotaInfo{Limit: 0x7FFFFFFFFFFFFFFF}, nil
}
