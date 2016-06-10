// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/keybase/client/go/libkb"
	"github.com/stretchr/testify/require"
)

func TestBserverTlfStorageBasic(t *testing.T) {
	codec := NewCodecMsgpack()
	localUsers := MakeLocalUsers([]libkb.NormalizedUsername{"user1", "user2"})
	currentUID := localUsers[0].UID
	crypto := &CryptoLocal{CryptoCommon: MakeCryptoCommonNoConfig()}
	config := &ConfigLocal{codec: codec, crypto: crypto}
	setTestLogger(config, t)

	tempdir, err := ioutil.TempDir(os.TempDir(), "bserver_tlf_storage")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempdir)
		if err != nil {
			t.Log("error removing %s: %s", tempdir, err)
		}
	}()

	s := makeBserverTlfStorage(codec, crypto, tempdir)

	bCtx := BlockContext{currentUID, "", zeroBlockRefNonce}
	data := []byte{1, 2, 3, 4}
	bID, err := crypto.MakePermanentBlockID(data)
	if err != nil {
		t.Fatal(err)
	}

	serverHalf, err := config.Crypto().MakeRandomBlockCryptKeyServerHalf()
	if err != nil {
		t.Errorf("Couldn't make block server key half: %v", err)
	}
	err = s.putData(bID, bCtx, data, serverHalf)
	if err != nil {
		t.Fatalf("Put got error: %v", err)
	}

	// Now get the same block back
	buf, key, err := s.getData(bID, bCtx)
	if err != nil {
		t.Fatalf("Get returned an error: %v", err)
	}
	if !bytes.Equal(buf, data) {
		t.Errorf("Got bad data -- got %v, expected %v", buf, data)
	}
	if key != serverHalf {
		t.Errorf("Got bad key -- got %v, expected %v", key, serverHalf)
	}

	// Add a reference.
	nonce, err := crypto.MakeBlockRefNonce()
	if err != nil {
		t.Fatal(err)
	}
	bCtx2 := BlockContext{currentUID, localUsers[1].UID, nonce}
	s.addReference(bID, bCtx2)

	// Now get the same block back
	buf, key, err = s.getData(bID, bCtx2)
	if err != nil {
		t.Fatalf("Get returned an error: %v", err)
	}
	if !bytes.Equal(buf, data) {
		t.Errorf("Got bad data -- got %v, expected %v", buf, data)
	}
	if key != serverHalf {
		t.Errorf("Got bad key -- got %v, expected %v", key, serverHalf)
	}
}
