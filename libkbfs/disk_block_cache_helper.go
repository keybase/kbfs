package libkbfs

import (
	"time"

	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
)

// diskBlockCacheEntry packages an encoded block and serverHalf into one data
// structure, allowing us to encode it as one set of bytes.
type diskBlockCacheEntry struct {
	Buf        []byte
	ServerHalf kbfscrypto.BlockCryptKeyServerHalf
}

// diskBlockCacheDeleteKey specifies a blockID and TLF pair to delete.
type diskBlockCacheDeleteKey struct {
	TlfID   tlf.ID
	BlockID kbfsblock.ID
}

// diskBlockCacheMetadata packages the metadata needed to make decisions on
// cache eviction.
type diskBlockCacheMetadata struct {
	// the TLF ID for the block
	TlfID tlf.ID
	// the last time the block was used
	LRUTime time.Time
	// the size of the block
	BlockSize uint32
}

// lruEntry is an entry for sorting LRU times
type lruEntry struct {
	TlfID   tlf.ID
	BlockID kbfsblock.ID
	Time    time.Time
}

type blockIDsByTime []lruEntry

func (b blockIDsByTime) Len() int           { return len(b) }
func (b blockIDsByTime) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b blockIDsByTime) Less(i, j int) bool { return b[i].Time.Before(b[j].Time) }

func (b blockIDsByTime) ToBlockIDSlice(numBlocks int) []diskBlockCacheDeleteKey {
	ids := make([]diskBlockCacheDeleteKey, 0, numBlocks)
	for _, entry := range b {
		if len(ids) == numBlocks {
			return ids
		}
		ids = append(ids, diskBlockCacheDeleteKey{entry.TlfID, entry.BlockID})
	}
	return ids
}
