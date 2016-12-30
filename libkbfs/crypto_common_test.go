// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"testing"
	"testing/quick"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test (very superficially) that MakeRandomTLFEphemeralKeys() returns
// non-zero values that aren't equal.
func TestCryptoCommonRandomTLFEphemeralKeys(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	a1, a2, err := c.MakeRandomTLFEphemeralKeys()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFEphemeralPublicKey{}, a1)
	require.NotEqual(t, kbfscrypto.TLFEphemeralPrivateKey{}, a2)

	b1, b2, err := c.MakeRandomTLFEphemeralKeys()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFEphemeralPublicKey{}, b1)
	require.NotEqual(t, kbfscrypto.TLFEphemeralPrivateKey{}, b2)

	require.NotEqual(t, a1, b1)
	require.NotEqual(t, a2, b2)
}

// Test (very superficially) that MakeRandomTLFKeys() returns non-zero
// values that aren't equal.
func TestCryptoCommonRandomTLFKeys(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	a1, a2, a3, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFPublicKey{}, a1)
	require.NotEqual(t, kbfscrypto.TLFPrivateKey{}, a2)
	require.NotEqual(t, kbfscrypto.TLFCryptKey{}, a3)

	b1, b2, b3, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFPublicKey{}, b1)
	require.NotEqual(t, kbfscrypto.TLFPrivateKey{}, b2)
	require.NotEqual(t, kbfscrypto.TLFCryptKey{}, b3)

	require.NotEqual(t, a1, b1)
	require.NotEqual(t, a2, b2)
	require.NotEqual(t, a3, b3)
}

// Test (very superficially) that MakeRandomTLFCryptKeyServerHalf()
// returns non-zero values that aren't equal.
func TestCryptoCommonRandomTLFCryptKeyServerHalf(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	k1, err := c.MakeRandomTLFCryptKeyServerHalf()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFCryptKeyServerHalf{}, k1)

	k2, err := c.MakeRandomTLFCryptKeyServerHalf()
	require.NoError(t, err)
	require.NotEqual(t, kbfscrypto.TLFCryptKeyServerHalf{}, k2)

	require.NotEqual(t, k1, k2)
}

// Test that MaskTLFCryptKey() returns bytes that are different from
// the server half and the key, and that UnmaskTLFCryptKey() undoes
// the masking properly.
func TestCryptoCommonMaskUnmaskTLFCryptKey(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	serverHalf, err := c.MakeRandomTLFCryptKeyServerHalf()
	require.NoError(t, err)

	_, _, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	clientHalf := kbfscrypto.MaskTLFCryptKey(serverHalf, cryptKey)

	if clientHalf.Data() == serverHalf.Data() {
		t.Error("client half == server half")
	}

	if clientHalf.Data() == cryptKey.Data() {
		t.Error("client half == key")
	}

	cryptKey2 := kbfscrypto.UnmaskTLFCryptKey(serverHalf, clientHalf)

	if cryptKey2 != cryptKey {
		t.Error("cryptKey != cryptKey2")
	}
}

// Test that UnmaskBlockCryptKey() returns bytes that are different from
// the server half and the key.
func TestCryptoCommonUnmaskTLFCryptKey(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	serverHalf, err := kbfscrypto.MakeRandomBlockCryptKeyServerHalf()
	require.NoError(t, err)

	_, _, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	key := kbfscrypto.UnmaskBlockCryptKey(serverHalf, cryptKey)

	if key.Data() == serverHalf.Data() {
		t.Error("key == server half")
	}

	if key.Data() == cryptKey.Data() {
		t.Error("key == crypt key")
	}
}

type TestBlock struct {
	A int
}

func (TestBlock) DataVersion() DataVer {
	return FirstValidDataVer
}

func (tb TestBlock) GetEncodedSize() uint32 {
	return 0
}

func (tb TestBlock) SetEncodedSize(size uint32) {
}

func (tb TestBlock) NewEmpty() Block {
	return &TestBlock{}
}

func (tb *TestBlock) Set(other Block, _ kbfscodec.Codec) {
	otherTb := other.(*TestBlock)
	tb.A = otherTb.A
}

func TestCryptoCommonEncryptDecryptBlock(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	block := TestBlock{42}
	key := kbfscrypto.BlockCryptKey{}

	_, encryptedBlock, err := c.EncryptBlock(&block, key)
	require.NoError(t, err)

	var decryptedBlock TestBlock
	err = c.DecryptBlock(encryptedBlock, key, &decryptedBlock)
	require.NoError(t, err)

	if block != decryptedBlock {
		t.Errorf("Expected block %v got %v", block, decryptedBlock)
	}
}

// Test that crypto.EncryptTLFCryptKeyClientHalf() encrypts its
// passed-in client half properly.
func TestCryptoCommonEncryptTLFCryptKeyClientHalf(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	ephPublicKey, ephPrivateKey, err := c.MakeRandomTLFEphemeralKeys()
	require.NoError(t, err)

	_, _, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	privateKey := kbfscrypto.MakeFakeCryptPrivateKeyOrBust("fake key")
	publicKey := privateKey.GetPublicKey()

	serverHalf, err := c.MakeRandomTLFCryptKeyServerHalf()
	require.NoError(t, err)

	clientHalf := kbfscrypto.MaskTLFCryptKey(serverHalf, cryptKey)

	encryptedClientHalf, err := c.EncryptTLFCryptKeyClientHalf(ephPrivateKey, publicKey, clientHalf)
	require.NoError(t, err)

	if encryptedClientHalf.Version != EncryptionSecretbox {
		t.Errorf("Expected version %v, got %v", EncryptionSecretbox, encryptedClientHalf.Version)
	}

	expectedEncryptedLength := len(clientHalf.Data()) + box.Overhead
	if len(encryptedClientHalf.EncryptedData) != expectedEncryptedLength {
		t.Errorf("Expected encrypted length %d, got %d", expectedEncryptedLength, len(encryptedClientHalf.EncryptedData))
	}

	if len(encryptedClientHalf.Nonce) != 24 {
		t.Fatalf("Expected nonce length 24, got %d", len(encryptedClientHalf.Nonce))
	}

	var nonce [24]byte
	copy(nonce[:], encryptedClientHalf.Nonce)
	if nonce == ([24]byte{}) {
		t.Error("Empty nonce")
	}

	ephPublicKeyData := ephPublicKey.Data()
	privateKeyData := privateKey.Data()
	decryptedData, ok := box.Open(
		nil, encryptedClientHalf.EncryptedData, &nonce,
		&ephPublicKeyData, &privateKeyData)
	if !ok {
		t.Fatal("Decryption failed")
	}

	if len(decryptedData) != len(clientHalf.Data()) {
		t.Fatalf("Expected decrypted data length %d, got %d", len(clientHalf.Data()), len(decryptedData))
	}

	var clientHalf2Data [32]byte
	copy(clientHalf2Data[:], decryptedData)
	clientHalf2 := kbfscrypto.MakeTLFCryptKeyClientHalf(clientHalf2Data)
	if clientHalf != clientHalf2 {
		t.Fatal("client half != decrypted client half")
	}
}

func checkSecretboxOpen(t *testing.T, encryptedData encryptedData, key [32]byte) (encodedData []byte) {
	if encryptedData.Version != EncryptionSecretbox {
		t.Errorf("Expected version %v, got %v", EncryptionSecretbox, encryptedData.Version)
	}

	if len(encryptedData.Nonce) != 24 {
		t.Fatalf("Expected nonce length 24, got %d", len(encryptedData.Nonce))
	}

	var nonce [24]byte
	copy(nonce[:], encryptedData.Nonce)
	if nonce == ([24]byte{}) {
		t.Error("Empty nonce")
	}

	encodedData, ok := secretbox.Open(nil, encryptedData.EncryptedData, &nonce, &key)
	if !ok {
		t.Fatal("Decryption failed")
	}

	return encodedData
}

// Test that crypto.EncryptPrivateMetadata() encrypts its passed-in
// PrivateMetadata object properly.
func TestEncryptPrivateMetadata(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	_, tlfPrivateKey, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	privateMetadata := PrivateMetadata{
		TLFPrivateKey: tlfPrivateKey,
	}
	expectedEncodedPrivateMetadata, err := c.codec.Encode(privateMetadata)
	require.NoError(t, err)

	encryptedPrivateMetadata, err := c.EncryptPrivateMetadata(privateMetadata, cryptKey)
	require.NoError(t, err)

	encodedPrivateMetadata := checkSecretboxOpen(t, encryptedPrivateMetadata.encryptedData, cryptKey.Data())

	if string(encodedPrivateMetadata) != string(expectedEncodedPrivateMetadata) {
		t.Fatalf("Expected encoded data %v, got %v", expectedEncodedPrivateMetadata, encodedPrivateMetadata)
	}
}

func secretboxSeal(t *testing.T, c *CryptoCommon, data interface{}, key [32]byte) encryptedData {
	encodedData, err := c.codec.Encode(data)
	require.NoError(t, err)

	return secretboxSealEncoded(t, c, encodedData, key)
}

func secretboxSealEncoded(t *testing.T, c *CryptoCommon, encodedData []byte, key [32]byte) encryptedData {
	var nonce [24]byte
	err := kbfscrypto.RandRead(nonce[:])
	require.NoError(t, err)

	sealedPmd := secretbox.Seal(nil, encodedData, &nonce, &key)

	return encryptedData{
		Version:       EncryptionSecretbox,
		Nonce:         nonce[:],
		EncryptedData: sealedPmd,
	}
}

// Test that crypto.DecryptPrivateMetadata() decrypts a
// PrivateMetadata object encrypted with the default method (current
// nacl/secretbox).
func TestDecryptPrivateMetadataSecretboxSeal(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	_, tlfPrivateKey, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	privateMetadata := PrivateMetadata{
		TLFPrivateKey: tlfPrivateKey,
	}

	encryptedPrivateMetadata := EncryptedPrivateMetadata{secretboxSeal(t, &c, privateMetadata, cryptKey.Data())}

	decryptedPrivateMetadata, err := c.DecryptPrivateMetadata(encryptedPrivateMetadata, cryptKey)
	require.NoError(t, err)

	pmEquals, err := kbfscodec.Equal(
		c.codec, decryptedPrivateMetadata, privateMetadata)
	require.NoError(t, err)
	if !pmEquals {
		t.Errorf("Decrypted private metadata %v doesn't match %v", decryptedPrivateMetadata, privateMetadata)
	}
}

// Test that crypto.DecryptPrivateMetadata() decrypts a
// PrivateMetadata object encrypted with the default method (current
// nacl/secretbox).
func TestDecryptEncryptedPrivateMetadata(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	_, tlfPrivateKey, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	privateMetadata := PrivateMetadata{
		TLFPrivateKey: tlfPrivateKey,
	}

	encryptedPrivateMetadata, err := c.EncryptPrivateMetadata(privateMetadata, cryptKey)
	require.NoError(t, err)

	decryptedPrivateMetadata, err := c.DecryptPrivateMetadata(encryptedPrivateMetadata, cryptKey)
	require.NoError(t, err)

	pmEquals, err := kbfscodec.Equal(
		c.codec, decryptedPrivateMetadata, privateMetadata)
	require.NoError(t, err)
	if !pmEquals {
		t.Errorf("Decrypted private metadata %v doesn't match %v", decryptedPrivateMetadata, privateMetadata)
	}
}

func checkDecryptionFailures(
	t *testing.T, encryptedData encryptedData, key interface{},
	decryptFn func(encryptedData encryptedData, key interface{}) error,
	corruptKeyFn func(interface{}) interface{}) {

	// Wrong version.

	encryptedDataWrongVersion := encryptedData
	encryptedDataWrongVersion.Version++
	err := decryptFn(encryptedDataWrongVersion, key)
	assert.Equal(t,
		UnknownEncryptionVer{encryptedDataWrongVersion.Version},
		errors.Cause(err))

	// Wrong nonce size.

	encryptedDataWrongNonceSize := encryptedData
	encryptedDataWrongNonceSize.Nonce = encryptedDataWrongNonceSize.Nonce[:len(encryptedDataWrongNonceSize.Nonce)-1]
	err = decryptFn(encryptedDataWrongNonceSize, key)
	assert.Equal(t,
		InvalidNonceError{encryptedDataWrongNonceSize.Nonce},
		errors.Cause(err))

	// Corrupt key.

	keyCorrupt := corruptKeyFn(key)
	err = decryptFn(encryptedData, keyCorrupt)
	assert.Equal(t, libkb.DecryptionError{}, errors.Cause(err))

	// Corrupt data.

	encryptedDataCorruptData := encryptedData
	encryptedDataCorruptData.EncryptedData[0] = ^encryptedDataCorruptData.EncryptedData[0]
	err = decryptFn(encryptedDataCorruptData, key)
	assert.Equal(t, libkb.DecryptionError{}, errors.Cause(err))
}

// Test various failure cases for crypto.DecryptPrivateMetadata().
func TestDecryptPrivateMetadataFailures(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	_, tlfPrivateKey, cryptKey, err := c.MakeRandomTLFKeys()
	require.NoError(t, err)

	privateMetadata := PrivateMetadata{
		TLFPrivateKey: tlfPrivateKey,
	}

	encryptedPrivateMetadata, err := c.EncryptPrivateMetadata(privateMetadata, cryptKey)
	require.NoError(t, err)

	checkDecryptionFailures(t, encryptedPrivateMetadata.encryptedData, cryptKey,
		func(encryptedData encryptedData, key interface{}) error {
			_, err = c.DecryptPrivateMetadata(
				EncryptedPrivateMetadata{encryptedData},
				key.(kbfscrypto.TLFCryptKey))
			return err
		},
		func(key interface{}) interface{} {
			cryptKey := key.(kbfscrypto.TLFCryptKey)
			cryptKeyCorruptData := cryptKey.Data()
			cryptKeyCorruptData[0] = ^cryptKeyCorruptData[0]
			cryptKeyCorrupt := kbfscrypto.MakeTLFCryptKey(
				cryptKeyCorruptData)
			return cryptKeyCorrupt
		})
}

func makeFakeBlockCryptKey(t *testing.T) kbfscrypto.BlockCryptKey {
	var blockCryptKeyData [32]byte
	err := kbfscrypto.RandRead(blockCryptKeyData[:])
	blockCryptKey := kbfscrypto.MakeBlockCryptKey(blockCryptKeyData)
	require.NoError(t, err)
	return blockCryptKey
}

// Test that crypto.EncryptBlock() encrypts its passed-in Block object
// properly.
func TestEncryptBlock(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	cryptKey := makeFakeBlockCryptKey(t)

	block := TestBlock{50}
	expectedEncodedBlock, err := c.codec.Encode(block)
	require.NoError(t, err)

	plainSize, encryptedBlock, err := c.EncryptBlock(&block, cryptKey)
	require.NoError(t, err)

	if plainSize != len(expectedEncodedBlock) {
		t.Errorf("Expected plain size %d, got %d", len(expectedEncodedBlock), plainSize)
	}

	paddedBlock := checkSecretboxOpen(t, encryptedBlock.encryptedData, cryptKey.Data())
	encodedBlock, err := c.depadBlock(paddedBlock)
	require.NoError(t, err)

	if string(encodedBlock) != string(expectedEncodedBlock) {
		t.Fatalf("Expected encoded data %v, got %v", expectedEncodedBlock, encodedBlock)
	}
}

// Test that crypto.DecryptBlock() decrypts a Block object encrypted
// with the default method (current nacl/secretbox).
func TestDecryptBlockSecretboxSeal(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	cryptKey := makeFakeBlockCryptKey(t)

	block := TestBlock{50}

	encodedBlock, err := c.codec.Encode(block)
	require.NoError(t, err)

	paddedBlock, err := c.padBlock(encodedBlock)
	require.NoError(t, err)

	encryptedBlock := EncryptedBlock{secretboxSealEncoded(t, &c, paddedBlock, cryptKey.Data())}

	var decryptedBlock TestBlock
	err = c.DecryptBlock(encryptedBlock, cryptKey, &decryptedBlock)
	require.NoError(t, err)

	if decryptedBlock != block {
		t.Errorf("Decrypted block %d doesn't match %d", decryptedBlock, block)
	}
}

// Test that crypto.DecryptBlock() decrypts a Block object encrypted
// with the default method (current nacl/secretbox).
func TestDecryptEncryptedBlock(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	cryptKey := makeFakeBlockCryptKey(t)

	block := TestBlock{50}

	_, encryptedBlock, err := c.EncryptBlock(&block, cryptKey)
	require.NoError(t, err)

	var decryptedBlock TestBlock
	err = c.DecryptBlock(encryptedBlock, cryptKey, &decryptedBlock)
	require.NoError(t, err)

	if decryptedBlock != block {
		t.Errorf("Decrypted block %d doesn't match %d", decryptedBlock, block)
	}
}

// Test various failure cases for crypto.DecryptBlock().
func TestDecryptBlockFailures(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	cryptKey := makeFakeBlockCryptKey(t)

	block := TestBlock{50}

	_, encryptedBlock, err := c.EncryptBlock(&block, cryptKey)
	require.NoError(t, err)

	checkDecryptionFailures(t, encryptedBlock.encryptedData, cryptKey,
		func(encryptedData encryptedData, key interface{}) error {
			var dummy TestBlock
			return c.DecryptBlock(
				EncryptedBlock{encryptedData},
				key.(kbfscrypto.BlockCryptKey), &dummy)
		},
		func(key interface{}) interface{} {
			cryptKey := key.(kbfscrypto.BlockCryptKey)
			cryptKeyCorruptData := cryptKey.Data()
			cryptKeyCorruptData[0] = ^cryptKeyCorruptData[0]
			cryptKeyCorrupt := kbfscrypto.MakeBlockCryptKey(
				cryptKeyCorruptData)
			return cryptKeyCorrupt
		})
}

// Test padding of blocks results in a larger block, with length
// equal to power of 2 + 4.
func TestBlockPadding(t *testing.T) {
	var c CryptoCommon
	f := func(b []byte) bool {
		padded, err := c.padBlock(b)
		if err != nil {
			t.Logf("padBlock err: %s", err)
			return false
		}
		n := len(padded)
		if n <= len(b) {
			t.Logf("padBlock padded block len %d <= input block len %d", n, len(b))
			return false
		}
		// len of slice without uint32 prefix:
		h := n - 4
		if h&(h-1) != 0 {
			t.Logf("padBlock padded block len %d not a power of 2", h)
			return false
		}
		return true
	}

	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Tests padding -> depadding results in same block data.
func TestBlockDepadding(t *testing.T) {
	var c CryptoCommon
	f := func(b []byte) bool {
		padded, err := c.padBlock(b)
		if err != nil {
			t.Logf("padBlock err: %s", err)
			return false
		}
		depadded, err := c.depadBlock(padded)
		if err != nil {
			t.Logf("depadBlock err: %s", err)
			return false
		}
		if !bytes.Equal(b, depadded) {
			return false
		}
		return true
	}

	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Test padding of blocks results in blocks at least 2^8.
func TestBlockPadMinimum(t *testing.T) {
	var c CryptoCommon
	for i := 0; i < 256; i++ {
		b := make([]byte, i)
		if err := kbfscrypto.RandRead(b); err != nil {
			t.Fatal(err)
		}
		padded, err := c.padBlock(b)
		if err != nil {
			t.Errorf("padBlock error: %s", err)
		}
		if len(padded) != 260 {
			t.Errorf("padded block len: %d, expected 260", len(padded))
		}
	}
}

// Test that secretbox encrypted data length is a deterministic
// function of the input data length.
func TestSecretboxEncryptedLen(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())

	const startSize = 100
	const endSize = 100000
	const iterations = 5

	// Generating random data is slow, so do it all up-front and
	// index into it. Note that we're intentionally re-using most
	// of the data between iterations intentionally.
	randomData := make([]byte, endSize+iterations)
	if err := kbfscrypto.RandRead(randomData); err != nil {
		t.Fatal(err)
	}

	cryptKeys := make([]kbfscrypto.BlockCryptKey, iterations)
	for j := 0; j < iterations; j++ {
		cryptKeys[j] = makeFakeBlockCryptKey(t)
	}

	for i := startSize; i < endSize; i += 1000 {
		var enclen int
		for j := 0; j < iterations; j++ {
			data := randomData[j : j+i]
			enc := secretboxSealEncoded(t, &c, data, cryptKeys[j].Data())
			if j == 0 {
				enclen = len(enc.EncryptedData)
			} else if len(enc.EncryptedData) != enclen {
				t.Errorf("encrypted data len: %d, expected %d", len(enc.EncryptedData), enclen)
			}
		}
	}
}

type testBlockArray []byte

func (tba testBlockArray) GetEncodedSize() uint32 {
	return 0
}

func (tba testBlockArray) SetEncodedSize(size uint32) {
}

func (testBlockArray) DataVersion() DataVer { return FirstValidDataVer }

func (tba testBlockArray) NewEmpty() Block {
	return &testBlockArray{}
}

func (tba *testBlockArray) Set(other Block, _ kbfscodec.Codec) {
	otherTba := other.(*testBlockArray)
	*tba = *otherTba
}

// Test that block encrypted data length is the same for data
// length within same power of 2.
func TestBlockEncryptedLen(t *testing.T) {
	c := MakeCryptoCommon(kbfscodec.NewMsgpack())
	cryptKey := makeFakeBlockCryptKey(t)

	const startSize = 1025
	const endSize = 2000

	// Generating random data is slow, so do it all up-front and
	// index into it. Note that we're intentionally re-using most
	// of the data between iterations intentionally.
	randomData := make(testBlockArray, endSize)
	err := kbfscrypto.RandRead(randomData)
	require.NoError(t, err)

	var expectedLen int
	for i := 1025; i < 2000; i++ {
		data := randomData[:i]
		_, encBlock, err := c.EncryptBlock(&data, cryptKey)
		require.NoError(t, err)

		if expectedLen == 0 {
			expectedLen = len(encBlock.EncryptedData)
			continue
		}

		if len(encBlock.EncryptedData) != expectedLen {
			t.Errorf("len encrypted data: %d, expected %d (input len: %d)",
				len(encBlock.EncryptedData), expectedLen, i)
		}
	}
}
