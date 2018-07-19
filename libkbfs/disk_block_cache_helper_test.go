package libkbfs

import (
	"testing"
	"time"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
)

func TestDiskBlockCacheMetadataEncode(t *testing.T) {
	codec := kbfscodec.NewMsgpack()

	metaDb := DiskBlockCacheMetadata{
		TlfID:            tlf.FakeID(1, tlf.Public),
		LRUTime:          time.Unix(0xdead, 0xbeef),
		BlockSize:        0xface,
		FinishedPrefetch: true,
	}

	bytes, err := codec.Encode(metaDb)
	require.NoError(t, err)
	require.Equal(t, []byte{}, bytes)
}
