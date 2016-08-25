// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/client/go/logger"
)

type testMDServerLocalConfig struct {
	t                  *testing.T
	clock              Clock
	codec              Codec
	crypto             Crypto
	_currentInfoGetter currentInfoGetter
}

func newTestMDServerLocalConfig(t *testing.T) testMDServerLocalConfig {
	return testMDServerLocalConfig{
		t:     t,
		clock: newTestClockNow(),
		codec: NewCodecMsgpack(),
	}
}

func (c testMDServerLocalConfig) Clock() Clock {
	return c.clock
}

func (c testMDServerLocalConfig) Codec() Codec {
	return c.codec
}

func (c testMDServerLocalConfig) Crypto() Crypto {
	return c.crypto
}

func (c testMDServerLocalConfig) currentInfoGetter() currentInfoGetter {
	return c._currentInfoGetter
}

func (c testMDServerLocalConfig) MetadataVersion() MetadataVer {
	return InitialExtraMetadataVer
}

func (c testMDServerLocalConfig) MakeLogger(module string) logger.Logger {
	return logger.NewTestLogger(c.t)
}
