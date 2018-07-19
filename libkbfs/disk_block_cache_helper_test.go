package libkbfs

import (
	"testing"
	"time"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/tlf"
	"github.com/stretchr/testify/require"
)

func makeTime(data []byte) time.Time {
	var t time.Time
	err := t.UnmarshalBinary(data)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDiskBlockCacheMetadataHardcoded(t *testing.T) {
	codec := kbfscodec.NewMsgpack()

	lruTime := makeTime([]byte{
		// Version.
		0x1,
		// Seconds.
		0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		// Nanoseconds.
		0xa, 0xb, 0xc, 0xd,
		// Zone offset.
		0xe, 0xf,
	})

	metaDb := DiskBlockCacheMetadata{
		TlfID:            tlf.FakeID(1, tlf.Public),
		LRUTime:          lruTime,
		BlockSize:        0xface,
		FinishedPrefetch: true,
	}

	// This was generated by starting with []byte{} and copying
	// the data in the error messaged for the require.Equal below.
	expectedBuf := []byte{
		0x85, 0xa9, 0x42, 0x6c, 0x6f, 0x63,
		0x6b, 0x53, 0x69, 0x7a, 0x65, 0xcd, 0xfa, 0xce, 0xb0, 0x46,
		0x69, 0x6e, 0x69, 0x73, 0x68, 0x65, 0x64, 0x50, 0x72, 0x65,
		0x66, 0x65, 0x74, 0x63, 0x68, 0xc3, 0xad, 0x48, 0x61, 0x73,
		0x50, 0x72, 0x65, 0x66, 0x65, 0x74, 0x63, 0x68, 0x65, 0x64,
		0xc2, 0xa7, 0x4c, 0x52, 0x55, 0x54, 0x69, 0x6d, 0x65, 0xc4,
		0xf, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb,
		0xc, 0xd, 0xe, 0xf, 0xa5, 0x54, 0x6c, 0x66, 0x49, 0x44, 0xc4,
		0x10, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x17,
	}

	bytes, err := codec.Encode(metaDb)
	require.NoError(t, err)
	require.Equal(t, expectedBuf, bytes)
}
