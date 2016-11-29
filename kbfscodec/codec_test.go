// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfscodec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodecEqualNil(t *testing.T) {
	codec := NewMsgpack()
	eq, err := Equal(codec, nil, nil)
	require.NoError(t, err)
	require.True(t, eq)
}
