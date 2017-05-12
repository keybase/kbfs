// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import (
	"testing"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfshash"
	"github.com/stretchr/testify/require"
)

// Make sure MdID encodes and decodes properly with minimal overhead.
func TestMdIDEncodeDecode(t *testing.T) {
	id := FakeMdID(1)
	encodedMdID, err := codec.Encode(id)
	require.NoError(t, err)

	// See
	// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-bin
	// for why there are two bytes of overhead.
	const overhead = 2
	require.Equal(t, kbfshash.DefaultHashByteLength+overhead, len(encodedMdID))

	var id2 MdID
	err = codec.Decode(encodedMdID, &id2)
	require.NoError(t, err)

	require.Equal(t, id, id2)
}

// Make sure the zero MdID value encodes and decodes properly.
func TestMdIDEncodeDecodeZero(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	encodedMdID, err := codec.Encode(MdID{})
	require.NoError(t, err)

	require.Equal(t, []byte{0xc0}, encodedMdID)

	var id MdID
	err = codec.Decode(encodedMdID, &id)
	require.NoError(t, err)

	require.Equal(t, MdID{}, id)
}
