// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"testing"

	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfshash"
	"github.com/stretchr/testify/require"
)

type tlfCryptKeyInfoFuture struct {
	TLFCryptKeyInfo
	kbfscodec.Extra
}

func (cki tlfCryptKeyInfoFuture) toCurrent() TLFCryptKeyInfo {
	return cki.TLFCryptKeyInfo
}

func (cki tlfCryptKeyInfoFuture) ToCurrentStruct() kbfscodec.CurrentStruct {
	return cki.toCurrent()
}

func makeFakeTLFCryptKeyInfoFuture(t *testing.T) tlfCryptKeyInfoFuture {
	hmac, err := kbfshash.DefaultHMAC(
		[]byte("fake key"), []byte("fake buf"))
	require.NoError(t, err)
	cki := TLFCryptKeyInfo{
		EncryptedTLFCryptKeyClientHalf{
			EncryptionSecretbox,
			[]byte("fake encrypted data"),
			[]byte("fake nonce"),
		},
		TLFCryptKeyServerHalfID{hmac},
		5,
		codec.UnknownFieldSetHandler{},
	}
	return tlfCryptKeyInfoFuture{
		cki,
		kbfscodec.MakeExtraOrBust("TLFCryptKeyInfo", t),
	}
}

func TestTLFCryptKeyInfoUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeTLFCryptKeyInfoFuture(t))
}
