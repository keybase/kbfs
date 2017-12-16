// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfshash

import (
	"crypto/sha256"
	"math/rand"
	"testing"

	"github.com/keybase/kbfs/kbfscodec"
	miniosha256 "github.com/minio/sha256-simd"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// Make sure Hash encodes and decodes properly with minimal overhead.
func TestHashEncodeDecode(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	h, err := DefaultHash([]byte{1})
	require.NoError(t, err)

	encodedH, err := codec.Encode(h)
	require.NoError(t, err)

	// See
	// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-bin
	// for why there are two bytes of overhead.
	const overhead = 2
	require.Equal(t, DefaultHashByteLength+overhead, len(encodedH))

	var h2 Hash
	err = codec.Decode(encodedH, &h2)
	require.NoError(t, err)

	require.Equal(t, h, h2)
}

// Make sure the zero Hash value encodes and decodes properly.
func TestHashEncodeDecodeZero(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	encodedH, err := codec.Encode(Hash{})
	require.NoError(t, err)

	expectedEncodedH := []byte{0xc0}
	require.Equal(t, expectedEncodedH, encodedH)

	var h Hash
	err = codec.Decode(encodedH, &h)
	require.NoError(t, err)

	require.Equal(t, Hash{}, h)
}

// Make sure that default hash gives a valid hash that verifies.
func TestDefaultHash(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	h, err := DefaultHash(data)
	require.NoError(t, err)

	require.True(t, h.IsValid())

	err = h.Verify(data)
	require.NoError(t, err)
}

// hashFromRawNoCheck() is like HashFromRaw() except it doesn't check
// validity.
func hashFromRawNoCheck(hashType HashType, rawHash []byte) Hash {
	return Hash{string(append([]byte{byte(hashType)}, rawHash...))}
}

// Make sure Hash.IsValid() fails properly.
func TestHashIsValid(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	validH, err := DefaultHash(data)
	require.NoError(t, err)

	// Zero hash.
	require.False(t, (Hash{}).IsValid())

	var smallH Hash
	smallH.h = validH.h[:MinHashByteLength-1]
	require.False(t, smallH.IsValid())

	var largeH Hash
	padding := make([]byte, MaxHashByteLength-len(validH.h)+1)
	largeH.h = string(append([]byte(validH.h), padding...))
	require.False(t, largeH.IsValid())

	invalidH := hashFromRawNoCheck(InvalidHash, validH.hashData())
	require.False(t, invalidH.IsValid())

	// A hash with an unknown version is still valid.
	unknownH := hashFromRawNoCheck(validH.hashType()+1, validH.hashData())
	require.True(t, unknownH.IsValid())
}

// Make sure Hash.Verify() fails properly.
func TestHashVerify(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}

	// Zero (invalid) hash.
	err := (Hash{}).Verify(data)
	require.Equal(t, InvalidHashError{Hash{}}, errors.Cause(err))

	validH, err := DefaultHash(data)
	require.NoError(t, err)

	corruptData := make([]byte, len(data))
	copy(corruptData, data)
	corruptData[0] ^= 1
	err = validH.Verify(corruptData)
	require.IsType(t, HashMismatchError{}, errors.Cause(err))

	invalidH := hashFromRawNoCheck(InvalidHash, validH.hashData())
	err = invalidH.Verify(data)
	require.Equal(t, InvalidHashError{invalidH}, errors.Cause(err))

	unknownType := validH.hashType() + 1
	unknownH := hashFromRawNoCheck(unknownType, validH.hashData())
	err = unknownH.Verify(data)
	require.Equal(t, UnknownHashTypeError{unknownType}, errors.Cause(err))

	hashData := validH.hashData()
	hashData[0] ^= 1
	corruptH := hashFromRawNoCheck(validH.hashType(), hashData)
	err = corruptH.Verify(data)
	require.IsType(t, HashMismatchError{}, errors.Cause(err))
}

// Make sure HMAC encodes and decodes properly with minimal overhead.
func TestHMACEncodeDecode(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	hmac, err := DefaultHMAC([]byte{1}, []byte{2})
	require.NoError(t, err)

	encodedHMAC, err := codec.Encode(hmac)
	require.NoError(t, err)

	// See
	// https://github.com/msgpack/msgpack/blob/master/spec.md#formats-bin
	// for why there are two bytes of overhead.
	const overhead = 2
	require.Equal(t, DefaultHashByteLength+overhead, len(encodedHMAC))

	var hmac2 HMAC
	err = codec.Decode(encodedHMAC, &hmac2)
	require.NoError(t, err)

	require.Equal(t, hmac, hmac2)
}

// Make sure the zero Hash value encodes and decodes properly.
func TestHMACEncodeDecodeZero(t *testing.T) {
	codec := kbfscodec.NewMsgpack()
	encodedHMAC, err := codec.Encode(HMAC{})
	require.NoError(t, err)

	expectedEncodedHMAC := []byte{0xc0}
	require.Equal(t, expectedEncodedHMAC, encodedHMAC)

	var hmac HMAC
	err = codec.Decode(encodedHMAC, &hmac)
	require.NoError(t, err)

	require.Equal(t, HMAC{}, hmac)
}

// Make sure that default HMAC gives a valid HMAC that verifies.
func TestDefaultHMAC(t *testing.T) {
	key := []byte{1, 2}
	data := []byte{1, 2, 3, 4, 5}
	hmac, err := DefaultHMAC(key, data)
	require.NoError(t, err)

	require.True(t, hmac.IsValid())

	err = hmac.Verify(key, data)
	require.NoError(t, err)
}

// No need to test HMAC.IsValid().

// hmacFromRawNoCheck() is like HmacFromRaw() except it doesn't check
// validity.
func hmacFromRawNoCheck(hashType HashType, rawHash []byte) HMAC {
	h := hashFromRawNoCheck(hashType, rawHash)
	return HMAC{h}
}

// Make sure HMAC.Verify() fails properly.
func TestVerify(t *testing.T) {
	key := []byte{1, 2}
	data := []byte{1, 2, 3, 4, 5}

	// Zero (invalid) HMAC.
	err := (HMAC{}).Verify(key, data)
	require.Equal(t, InvalidHashError{Hash{}}, errors.Cause(err))

	validHMAC, err := DefaultHMAC(key, data)
	require.NoError(t, errors.Cause(err))

	corruptKey := make([]byte, len(key))
	copy(corruptKey, key)
	corruptKey[0] ^= 1
	err = validHMAC.Verify(corruptKey, data)
	require.IsType(t, HashMismatchError{}, errors.Cause(err))

	corruptData := make([]byte, len(data))
	copy(corruptData, data)
	corruptData[0] ^= 1
	err = validHMAC.Verify(key, corruptData)
	require.IsType(t, HashMismatchError{}, errors.Cause(err))

	invalidHMAC := hmacFromRawNoCheck(InvalidHash, validHMAC.hashData())
	err = invalidHMAC.Verify(key, data)
	require.Equal(t, InvalidHashError{invalidHMAC.h}, errors.Cause(err))

	unknownType := validHMAC.hashType() + 1
	unknownHMAC := hmacFromRawNoCheck(unknownType, validHMAC.hashData())
	err = unknownHMAC.Verify(key, data)
	require.Equal(t, UnknownHashTypeError{unknownType}, errors.Cause(err))

	hashData := validHMAC.hashData()
	hashData[0] ^= 1
	corruptHMAC := hmacFromRawNoCheck(validHMAC.hashType(), hashData)
	err = corruptHMAC.Verify(key, data)
	require.IsType(t, HashMismatchError{}, errors.Cause(err))
}

var defaultHashBenchmarkResult [sha256.Size]byte

func benchmarkGenerateRandomBytes(dataSize int, b *testing.B) []byte {
	data := make([]byte, dataSize)
	n, err := rand.Read(data)
	if err != nil {
		b.Errorf("Error reading from random: %+v", err)
	}
	if n != dataSize {
		b.Errorf("Wrong size for random read. Expected: %d, actual: %d",
			dataSize, n)
	}
	b.ResetTimer()
	return data
}

func benchmarkCryptoSha256(dataSize int, b *testing.B) (s [sha256.Size]byte) {
	data := benchmarkGenerateRandomBytes(dataSize, b)

	for i := 0; i < b.N; i++ {
		s1 := sha256.Sum256(data)
		if s == s1 {
			b.Errorf("Hashes are equal but they shouldn't be: %+v, %+v", s, s1)
		}
		s = s1
		// rotate `data` to get a different hash each time
		data = append(data[1:], data[:1]...)
	}
	return s
}

func benchmarkMinioSha256(dataSize int, b *testing.B) (s [sha256.Size]byte) {
	data := benchmarkGenerateRandomBytes(dataSize, b)

	for i := 0; i < b.N; i++ {
		s1 := miniosha256.Sum256(data)
		if s == s1 {
			b.Errorf("Hashes are equal but they shouldn't be: %+v, %+v", s, s1)
		}
		s = s1
		// rotate `data` to get a different hash each time
		data = append(data[1:], data[:1]...)
	}
	return s
}

func BenchmarkCryptoSha1k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkCryptoSha256(1<<10, b)
}

func BenchmarkCryptoSha8k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkCryptoSha256(1<<13, b)
}

func BenchmarkCryptoSha65k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkCryptoSha256(1<<16, b)
}

func BenchmarkCryptoSha768k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkCryptoSha256(1<<19, b)
}

func BenchmarkMinioSha1k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkMinioSha256(1<<10, b)
}

func BenchmarkMinioSha8k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkMinioSha256(1<<13, b)
}

func BenchmarkMinioSha65k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkMinioSha256(1<<16, b)
}

func BenchmarkMinioSha768k(b *testing.B) {
	defaultHashBenchmarkResult = benchmarkMinioSha256(1<<19, b)
}
