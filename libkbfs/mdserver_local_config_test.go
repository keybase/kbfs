// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"golang.org/x/net/context"
)

type singleCurrentInfoGetter struct {
	name           libkb.NormalizedUsername
	uid            keybase1.UID
	cryptPublicKey kbfscrypto.CryptPublicKey
	verifyingKey   kbfscrypto.VerifyingKey
}

func (cig singleCurrentInfoGetter) GetCurrentUserInfo(ctx context.Context) (
	libkb.NormalizedUsername, keybase1.UID, error) {
	return cig.name, cig.uid, nil
}

func (cig singleCurrentInfoGetter) GetCurrentCryptPublicKey(
	ctx context.Context) (kbfscrypto.CryptPublicKey, error) {
	return cig.cryptPublicKey, nil
}

func (cig singleCurrentInfoGetter) GetCurrentVerifyingKey(
	ctx context.Context) (kbfscrypto.VerifyingKey, error) {
	return cig.verifyingKey, nil
}

type testMDServerLocalConfig struct {
	log    logger.Logger
	clock  Clock
	codec  kbfscodec.Codec
	crypto cryptoPure
	cig    currentInfoGetter
}

func newTestMDServerLocalConfig(
	log logger.Logger, cig currentInfoGetter) testMDServerLocalConfig {
	codec := kbfscodec.NewMsgpack()
	return testMDServerLocalConfig{
		log:    log,
		clock:  newTestClockNow(),
		codec:  codec,
		crypto: MakeCryptoCommon(codec),
		cig:    cig,
	}
}

func (c testMDServerLocalConfig) Clock() Clock {
	return c.clock
}

func (c testMDServerLocalConfig) Codec() kbfscodec.Codec {
	return c.codec
}

func (c testMDServerLocalConfig) cryptoPure() cryptoPure {
	return c.crypto
}

func (c testMDServerLocalConfig) currentInfoGetter() currentInfoGetter {
	return c.cig
}

func (c testMDServerLocalConfig) MetadataVersion() MetadataVer {
	return defaultClientMetadataVer
}

func (c testMDServerLocalConfig) MakeLogger(module string) logger.Logger {
	return c.log
}
