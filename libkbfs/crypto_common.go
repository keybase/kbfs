// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
)

// Belt-and-suspenders wrapper around crypto.rand.Read().
func cryptoRandRead(buf []byte) error {
	n, err := rand.Read(buf)
	if err != nil {
		return err
	}
	// This is truly unexpected, as rand.Read() is supposed to
	// return an error on a short read already!
	if n != len(buf) {
		return UnexpectedShortCryptoRandRead{}
	}
	return nil
}

// CryptoCommon contains many of the function implementations need for
// the Crypto interface, which can be reused by other implementations.
type CryptoCommon struct {
	codec    Codec
	log      logger.Logger
	deferLog logger.Logger
}

var _ cryptoPure = (*CryptoCommon)(nil)

// MakeCryptoCommon returns a default CryptoCommon object.
func MakeCryptoCommon(codec Codec, log logger.Logger) CryptoCommon {
	return CryptoCommon{codec, log, log.CloneWithAddedDepth(1)}
}

// MakeRandomTlfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomTlfID(isPublic bool) (TlfID, error) {
	var id TlfID
	err := cryptoRandRead(id.id[:])
	if err != nil {
		return TlfID{}, err
	}
	if isPublic {
		id.id[TlfIDByteLen-1] = PubTlfIDSuffix
	} else {
		id.id[TlfIDByteLen-1] = TlfIDSuffix
	}
	return id, nil
}

// MakeRandomBranchID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomBranchID() (BranchID, error) {
	var id BranchID
	err := cryptoRandRead(id.id[:])
	if err != nil {
		return BranchID{}, err
	}
	return id, nil
}

// MakeMdID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeMdID(md *RootMetadata) (MdID, error) {
	// Make sure that the serialized metadata is set; otherwise we
	// won't get the right MdID.
	if md.SerializedPrivateMetadata == nil {
		return MdID{}, MDMissingDataError{md.ID}
	}

	buf, err := c.codec.Encode(md)
	if err != nil {
		return MdID{}, err
	}

	h, err := DefaultHash(buf)
	if err != nil {
		return MdID{}, err
	}
	return MdID{h}, nil
}

// MakeMerkleHash implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeMerkleHash(md *RootMetadataSigned) (MerkleHash, error) {
	buf, err := c.codec.Encode(md)
	if err != nil {
		return MerkleHash{}, err
	}
	h, err := DefaultHash(buf)
	if err != nil {
		return MerkleHash{}, err
	}
	return MerkleHash{h}, nil
}

// MakeTemporaryBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeTemporaryBlockID() (BlockID, error) {
	var dh RawDefaultHash
	err := cryptoRandRead(dh[:])
	if err != nil {
		return BlockID{}, err
	}
	h, err := HashFromRaw(DefaultHashType, dh[:])
	if err != nil {
		return BlockID{}, err
	}
	return BlockID{h}, nil
}

// MakePermanentBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakePermanentBlockID(encodedEncryptedData []byte) (BlockID, error) {
	h, err := DefaultHash(encodedEncryptedData)
	if err != nil {
		return BlockID{}, nil
	}
	return BlockID{h}, nil
}

// VerifyBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) VerifyBlockID(encodedEncryptedData []byte, id BlockID) error {
	return id.h.Verify(encodedEncryptedData)
}

// MakeBlockRefNonce implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeBlockRefNonce() (nonce BlockRefNonce, err error) {
	err = cryptoRandRead(nonce[:])
	return
}

// MakeRandomTLFKeys implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomTLFKeys() (
	tlfPublicKey TLFPublicKey,
	tlfPrivateKey TLFPrivateKey,
	tlfEphemeralPublicKey TLFEphemeralPublicKey,
	tlfEphemeralPrivateKey TLFEphemeralPrivateKey,
	tlfCryptKey TLFCryptKey,
	err error) {
	defer func() {
		if err != nil {
			tlfPublicKey = TLFPublicKey{}
			tlfPrivateKey = TLFPrivateKey{}
			tlfEphemeralPublicKey = TLFEphemeralPublicKey{}
			tlfEphemeralPrivateKey = TLFEphemeralPrivateKey{}
			tlfCryptKey = TLFCryptKey{}
		}
	}()

	publicKey, privateKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return
	}

	tlfPublicKey = MakeTLFPublicKey(*publicKey)
	tlfPrivateKey = MakeTLFPrivateKey(*privateKey)

	keyPair, err := libkb.GenerateNaclDHKeyPair()
	if err != nil {
		return
	}

	tlfEphemeralPublicKey = MakeTLFEphemeralPublicKey(keyPair.Public)
	tlfEphemeralPrivateKey = MakeTLFEphemeralPrivateKey(*keyPair.Private)

	err = cryptoRandRead(tlfCryptKey.data[:])
	if err != nil {
		return
	}

	return
}

// MakeRandomTLFCryptKeyServerHalf implements the Crypto interface for
// CryptoCommon.
func (c CryptoCommon) MakeRandomTLFCryptKeyServerHalf() (serverHalf TLFCryptKeyServerHalf, err error) {
	err = cryptoRandRead(serverHalf.data[:])
	if err != nil {
		serverHalf = TLFCryptKeyServerHalf{}
		return
	}
	return
}

// MakeRandomBlockCryptKeyServerHalf implements the Crypto interface
// for CryptoCommon.
func (c CryptoCommon) MakeRandomBlockCryptKeyServerHalf() (serverHalf BlockCryptKeyServerHalf, err error) {
	err = cryptoRandRead(serverHalf.data[:])
	if err != nil {
		serverHalf = BlockCryptKeyServerHalf{}
		return
	}
	return
}

func xorKeys(x, y [32]byte) [32]byte {
	var res [32]byte
	for i := 0; i < 32; i++ {
		res[i] = x[i] ^ y[i]
	}
	return res
}

// MaskTLFCryptKey implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MaskTLFCryptKey(serverHalf TLFCryptKeyServerHalf, key TLFCryptKey) (clientHalf TLFCryptKeyClientHalf, err error) {
	clientHalf.data = xorKeys(serverHalf.data, key.data)
	return
}

// UnmaskTLFCryptKey implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) UnmaskTLFCryptKey(serverHalf TLFCryptKeyServerHalf, clientHalf TLFCryptKeyClientHalf) (key TLFCryptKey, err error) {
	key.data = xorKeys(serverHalf.data, clientHalf.data)
	return
}

// UnmaskBlockCryptKey implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) UnmaskBlockCryptKey(serverHalf BlockCryptKeyServerHalf, tlfCryptKey TLFCryptKey) (key BlockCryptKey, error error) {
	key.data = xorKeys(serverHalf.data, tlfCryptKey.data)
	return
}

// Verify implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) Verify(msg []byte, sigInfo SignatureInfo) (err error) {
	defer func() {
		c.deferLog.Debug(
			"Verify result for %d-byte message with %s: %v",
			len(msg), sigInfo, err)
	}()

	if sigInfo.Version != SigED25519 {
		err = UnknownSigVer{sigInfo.Version}
		return
	}

	publicKey := libkb.KIDToNaclSigningKeyPublic(sigInfo.VerifyingKey.kid.ToBytes())
	if publicKey == nil {
		err = libkb.KeyCannotVerifyError{}
		return
	}

	var naclSignature libkb.NaclSignature
	if len(sigInfo.Signature) != len(naclSignature) {
		err = libkb.VerificationError{}
		return
	}
	copy(naclSignature[:], sigInfo.Signature)

	if !publicKey.Verify(msg, &naclSignature) {
		err = libkb.VerificationError{}
		return
	}

	return
}

// EncryptTLFCryptKeyClientHalf implements the Crypto interface for
// CryptoCommon.
func (c CryptoCommon) EncryptTLFCryptKeyClientHalf(privateKey TLFEphemeralPrivateKey, publicKey CryptPublicKey, clientHalf TLFCryptKeyClientHalf) (encryptedClientHalf EncryptedTLFCryptKeyClientHalf, err error) {
	var nonce [24]byte
	err = cryptoRandRead(nonce[:])
	if err != nil {
		return
	}

	keypair, err := libkb.ImportKeypairFromKID(publicKey.kid)
	if err != nil {
		return
	}

	dhKeyPair, ok := keypair.(libkb.NaclDHKeyPair)
	if !ok {
		err = libkb.KeyCannotEncryptError{}
		return
	}

	encryptedData := box.Seal(nil, clientHalf.data[:], &nonce, (*[32]byte)(&dhKeyPair.Public), (*[32]byte)(&privateKey.data))

	encryptedClientHalf = EncryptedTLFCryptKeyClientHalf{
		Version:       EncryptionSecretbox,
		Nonce:         nonce[:],
		EncryptedData: encryptedData,
	}
	return
}

func (c CryptoCommon) encryptData(data []byte, key [32]byte) (encryptedData, error) {
	var nonce [24]byte
	err := cryptoRandRead(nonce[:])
	if err != nil {
		return encryptedData{}, err
	}

	sealedData := secretbox.Seal(nil, data, &nonce, &key)

	return encryptedData{
		Version:       EncryptionSecretbox,
		Nonce:         nonce[:],
		EncryptedData: sealedData,
	}, nil
}

// EncryptPrivateMetadata implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) EncryptPrivateMetadata(pmd *PrivateMetadata, key TLFCryptKey) (encryptedPmd EncryptedPrivateMetadata, err error) {
	encodedPmd, err := c.codec.Encode(pmd)
	if err != nil {
		return
	}

	encryptedData, err := c.encryptData(encodedPmd, key.data)
	if err != nil {
		return
	}

	encryptedPmd = EncryptedPrivateMetadata(encryptedData)
	return
}

func (c CryptoCommon) decryptData(encryptedData encryptedData, key [32]byte) ([]byte, error) {
	if encryptedData.Version != EncryptionSecretbox {
		return nil, UnknownEncryptionVer{encryptedData.Version}
	}

	var nonce [24]byte
	if len(encryptedData.Nonce) != len(nonce) {
		return nil, InvalidNonceError{encryptedData.Nonce}
	}
	copy(nonce[:], encryptedData.Nonce)

	decryptedData, ok := secretbox.Open(nil, encryptedData.EncryptedData, &nonce, &key)
	if !ok {
		return nil, libkb.DecryptionError{}
	}

	return decryptedData, nil
}

// DecryptPrivateMetadata implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) DecryptPrivateMetadata(encryptedPmd EncryptedPrivateMetadata, key TLFCryptKey) (*PrivateMetadata, error) {
	encodedPmd, err := c.decryptData(encryptedData(encryptedPmd), key.data)
	if err != nil {
		return nil, err
	}

	var pmd PrivateMetadata
	err = c.codec.Decode(encodedPmd, &pmd)
	if err != nil {
		return nil, err
	}

	return &pmd, nil
}

const minBlockSize = 256

// nextPowerOfTwo returns next power of 2 greater than the input n.
// https://en.wikipedia.org/wiki/Power_of_two#Algorithm_to_round_up_to_power_of_two
func nextPowerOfTwo(n uint32) uint32 {
	if n < minBlockSize {
		return minBlockSize
	}
	if n&(n-1) == 0 {
		// if n is already power of 2, get the next one
		n++
	}

	n--
	n = n | (n >> 1)
	n = n | (n >> 2)
	n = n | (n >> 4)
	n = n | (n >> 8)
	n = n | (n >> 16)
	n++

	return n
}

const padPrefixSize = 4

// padBlock adds random padding to an encoded block.
func (c CryptoCommon) padBlock(block []byte) ([]byte, error) {
	blockLen := uint32(len(block))
	overallLen := nextPowerOfTwo(blockLen)
	padLen := int64(overallLen - blockLen)

	buf := bytes.NewBuffer(make([]byte, 0, overallLen+padPrefixSize))

	// first 4 bytes contain the length of the block data
	if err := binary.Write(buf, binary.LittleEndian, blockLen); err != nil {
		return nil, err
	}

	// followed by the actual block data
	buf.Write(block)

	// followed by random data
	n, err := io.CopyN(buf, rand.Reader, padLen)
	if err != nil {
		return nil, err
	}
	if n != padLen {
		return nil, UnexpectedShortCryptoRandRead{}
	}

	return buf.Bytes(), nil
}

// depadBlock extracts the actual block data from a padded block.
func (c CryptoCommon) depadBlock(paddedBlock []byte) ([]byte, error) {
	buf := bytes.NewBuffer(paddedBlock)

	var blockLen uint32
	if err := binary.Read(buf, binary.LittleEndian, &blockLen); err != nil {
		return nil, err
	}
	blockEndPos := int(blockLen + padPrefixSize)

	if len(paddedBlock) < blockEndPos {
		return nil, PaddedBlockReadError{ActualLen: len(paddedBlock), ExpectedLen: blockEndPos}
	}
	return buf.Next(int(blockLen)), nil
}

// EncryptBlock implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) EncryptBlock(block Block, key BlockCryptKey) (plainSize int, encryptedBlock EncryptedBlock, err error) {
	encodedBlock, err := c.codec.Encode(block)
	if err != nil {
		return
	}

	paddedBlock, err := c.padBlock(encodedBlock)
	if err != nil {
		return
	}

	encryptedData, err := c.encryptData(paddedBlock, key.data)
	if err != nil {
		return
	}

	plainSize = len(encodedBlock)
	encryptedBlock = EncryptedBlock(encryptedData)
	return
}

// DecryptBlock implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) DecryptBlock(encryptedBlock EncryptedBlock, key BlockCryptKey, block Block) error {
	paddedBlock, err := c.decryptData(encryptedData(encryptedBlock), key.data)
	if err != nil {
		return err
	}

	encodedBlock, err := c.depadBlock(paddedBlock)
	if err != nil {
		return err
	}

	err = c.codec.Decode(encodedBlock, &block)
	if err != nil {
		return BlockDecodeError{err}
	}
	return nil
}

// GetTLFCryptKeyServerHalfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) GetTLFCryptKeyServerHalfID(
	user keybase1.UID, deviceKID keybase1.KID,
	serverHalf TLFCryptKeyServerHalf) (TLFCryptKeyServerHalfID, error) {
	key := serverHalf.data[:]
	data := append(user.ToBytes(), deviceKID.ToBytes()...)
	hmac, err := DefaultHMAC(key, data)
	if err != nil {
		return TLFCryptKeyServerHalfID{}, err
	}
	return TLFCryptKeyServerHalfID{
		ID: hmac,
	}, nil
}

// VerifyTLFCryptKeyServerHalfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) VerifyTLFCryptKeyServerHalfID(serverHalfID TLFCryptKeyServerHalfID,
	user keybase1.UID, deviceKID keybase1.KID, serverHalf TLFCryptKeyServerHalf) error {
	key := serverHalf.data[:]
	data := append(user.ToBytes(), deviceKID.ToBytes()...)
	return serverHalfID.ID.Verify(key, data)
}

// EncryptMerkleLeaf encrypts a Merkle leaf node with the TLFPublicKey.
func (c CryptoCommon) EncryptMerkleLeaf(leaf MerkleLeaf, pubKey TLFPublicKey,
	nonce *[24]byte, ePrivKey TLFEphemeralPrivateKey) (EncryptedMerkleLeaf, error) {
	// encode the clear-text leaf
	leafBytes, err := c.codec.Encode(leaf)
	if err != nil {
		return EncryptedMerkleLeaf{}, err
	}
	// encrypt the encoded leaf
	encryptedData := box.Seal(nil, leafBytes[:], nonce, (*[32]byte)(&pubKey.data), (*[32]byte)(&ePrivKey.data))
	return EncryptedMerkleLeaf{
		Version:       EncryptionSecretbox,
		EncryptedData: encryptedData,
	}, nil
}

// DecryptMerkleLeaf decrypts a Merkle leaf node with the TLFPrivateKey.
func (c CryptoCommon) DecryptMerkleLeaf(encryptedLeaf EncryptedMerkleLeaf, privKey TLFPrivateKey,
	nonce *[24]byte, ePubKey TLFEphemeralPublicKey) (*MerkleLeaf, error) {
	if encryptedLeaf.Version != EncryptionSecretbox {
		return nil, UnknownEncryptionVer{encryptedLeaf.Version}
	}
	leafBytes, ok := box.Open(nil, encryptedLeaf.EncryptedData[:], nonce, (*[32]byte)(&ePubKey.data), (*[32]byte)(&privKey.data))
	if !ok {
		return nil, libkb.DecryptionError{}
	}
	// decode the leaf
	var leaf MerkleLeaf
	if err := c.codec.Decode(leafBytes, &leaf); err != nil {
		return nil, err
	}
	return &leaf, nil
}
