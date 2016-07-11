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
		return IFCERFTUnexpectedShortCryptoRandRead{}
	}
	return nil
}

// CryptoCommon contains many of the function implementations need for
// the Crypto interface, which can be reused by other implementations.
type CryptoCommon struct {
	codec    IFCERFTCodec
	log      logger.Logger
	deferLog logger.Logger
}

var _ cryptoPure = (*CryptoCommon)(nil)

// MakeCryptoCommon returns a default CryptoCommon object.
func MakeCryptoCommon(codec IFCERFTCodec, log logger.Logger) CryptoCommon {
	return CryptoCommon{codec, log, log.CloneWithAddedDepth(1)}
}

// MakeRandomTlfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomTlfID(isPublic bool) (IFCERFTTlfID, error) {
	var id IFCERFTTlfID
	err := cryptoRandRead(id.id[:])
	if err != nil {
		return IFCERFTTlfID{}, err
	}
	if isPublic {
		id.id[IFCERFTTlfIDByteLen-1] = IFCERFTPubTlfIDSuffix
	} else {
		id.id[IFCERFTTlfIDByteLen-1] = IFCERFTTlfIDSuffix
	}
	return id, nil
}

// MakeRandomBranchID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomBranchID() (IFCERFTBranchID, error) {
	var id IFCERFTBranchID
	err := cryptoRandRead(id.id[:])
	if err != nil {
		return IFCERFTBranchID{}, err
	}
	return id, nil
}

// MakeMdID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeMdID(md *IFCERFTRootMetadata) (IFCERFTMdID, error) {
	// Make sure that the serialized metadata is set; otherwise we
	// won't get the right MdID.
	if md.SerializedPrivateMetadata == nil {
		return IFCERFTMdID{}, IFCERFTMDMissingDataError{md.ID}
	}

	buf, err := c.codec.Encode(md)
	if err != nil {
		return IFCERFTMdID{}, err
	}

	h, err := IFCERFTDefaultHash(buf)
	if err != nil {
		return IFCERFTMdID{}, err
	}
	return IFCERFTMdID{h}, nil
}

// MakeMerkleHash implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeMerkleHash(md *IFCERFTRootMetadataSigned) (MerkleHash, error) {
	buf, err := c.codec.Encode(md)
	if err != nil {
		return MerkleHash{}, err
	}
	h, err := IFCERFTDefaultHash(buf)
	if err != nil {
		return MerkleHash{}, err
	}
	return MerkleHash{h}, nil
}

// MakeTemporaryBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeTemporaryBlockID() (IFCERFTBlockID, error) {
	var dh IFCERFTRawDefaultHash
	err := cryptoRandRead(dh[:])
	if err != nil {
		return IFCERFTBlockID{}, err
	}
	h, err := IFCERFTHashFromRaw(IFCERFTDefaultHashType, dh[:])
	if err != nil {
		return IFCERFTBlockID{}, err
	}
	return IFCERFTBlockID{h}, nil
}

// MakePermanentBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakePermanentBlockID(encodedEncryptedData []byte) (IFCERFTBlockID, error) {
	h, err := IFCERFTDefaultHash(encodedEncryptedData)
	if err != nil {
		return IFCERFTBlockID{}, nil
	}
	return IFCERFTBlockID{h}, nil
}

// VerifyBlockID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) VerifyBlockID(encodedEncryptedData []byte, id IFCERFTBlockID) error {
	return id.h.Verify(encodedEncryptedData)
}

// MakeBlockRefNonce implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeBlockRefNonce() (nonce IFCERFTBlockRefNonce, err error) {
	err = cryptoRandRead(nonce[:])
	return
}

// MakeRandomTLFKeys implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) MakeRandomTLFKeys() (
	tlfPublicKey IFCERFTTLFPublicKey, tlfPrivateKey IFCERFTTLFPrivateKey, tlfEphemeralPublicKey IFCERFTTLFEphemeralPublicKey, tlfEphemeralPrivateKey IFCERFTTLFEphemeralPrivateKey, tlfCryptKey IFCERFTTLFCryptKey, err error) {
	defer func() {
		if err != nil {
			tlfPublicKey = IFCERFTTLFPublicKey{}
			tlfPrivateKey = IFCERFTTLFPrivateKey{}
			tlfEphemeralPublicKey = IFCERFTTLFEphemeralPublicKey{}
			tlfEphemeralPrivateKey = IFCERFTTLFEphemeralPrivateKey{}
			tlfCryptKey = IFCERFTTLFCryptKey{}
		}
	}()

	publicKey, privateKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return
	}

	tlfPublicKey = IFCERFTMakeTLFPublicKey(*publicKey)
	tlfPrivateKey = IFCERFTMakeTLFPrivateKey(*privateKey)

	keyPair, err := libkb.GenerateNaclDHKeyPair()
	if err != nil {
		return
	}

	tlfEphemeralPublicKey = IFCERFTMakeTLFEphemeralPublicKey(keyPair.Public)
	tlfEphemeralPrivateKey = IFCERFTMakeTLFEphemeralPrivateKey(*keyPair.Private)

	err = cryptoRandRead(tlfCryptKey.data[:])
	if err != nil {
		return
	}

	return
}

// MakeRandomTLFCryptKeyServerHalf implements the Crypto interface for
// CryptoCommon.
func (c CryptoCommon) MakeRandomTLFCryptKeyServerHalf() (serverHalf IFCERFTTLFCryptKeyServerHalf, err error) {
	err = cryptoRandRead(serverHalf.data[:])
	if err != nil {
		serverHalf = IFCERFTTLFCryptKeyServerHalf{}
		return
	}
	return
}

// MakeRandomBlockCryptKeyServerHalf implements the Crypto interface
// for CryptoCommon.
func (c CryptoCommon) MakeRandomBlockCryptKeyServerHalf() (serverHalf IFCERFTBlockCryptKeyServerHalf, err error) {
	err = cryptoRandRead(serverHalf.data[:])
	if err != nil {
		serverHalf = IFCERFTBlockCryptKeyServerHalf{}
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
func (c CryptoCommon) MaskTLFCryptKey(serverHalf IFCERFTTLFCryptKeyServerHalf, key IFCERFTTLFCryptKey) (clientHalf IFCERFTTLFCryptKeyClientHalf, err error) {
	clientHalf.data = xorKeys(serverHalf.data, key.data)
	return
}

// UnmaskTLFCryptKey implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) UnmaskTLFCryptKey(serverHalf IFCERFTTLFCryptKeyServerHalf, clientHalf IFCERFTTLFCryptKeyClientHalf) (key IFCERFTTLFCryptKey, err error) {
	key.data = xorKeys(serverHalf.data, clientHalf.data)
	return
}

// UnmaskBlockCryptKey implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) UnmaskBlockCryptKey(serverHalf IFCERFTBlockCryptKeyServerHalf, tlfCryptKey IFCERFTTLFCryptKey) (key IFCERFTBlockCryptKey, error error) {
	key.data = xorKeys(serverHalf.data, tlfCryptKey.data)
	return
}

// Verify implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) Verify(msg []byte, sigInfo IFCERFTSignatureInfo) (err error) {
	defer func() {
		c.deferLog.Debug(
			"Verify result for %d-byte message with %s: %v",
			len(msg), sigInfo, err)
	}()

	if sigInfo.Version != IFCERFTSigED25519 {
		err = IFCERFTUnknownSigVer{sigInfo.Version}
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
func (c CryptoCommon) EncryptTLFCryptKeyClientHalf(privateKey IFCERFTTLFEphemeralPrivateKey, publicKey IFCERFTCryptPublicKey, clientHalf IFCERFTTLFCryptKeyClientHalf) (encryptedClientHalf IFCERFTEncryptedTLFCryptKeyClientHalf, err error) {
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

	encryptedClientHalf = IFCERFTEncryptedTLFCryptKeyClientHalf{
		Version:       IFCERFTEncryptionSecretbox,
		Nonce:         nonce[:],
		EncryptedData: encryptedData,
	}
	return
}

func (c CryptoCommon) encryptData(data []byte, key [32]byte) (IFCERFTEncryptedDat, error) {
	var nonce [24]byte
	err := cryptoRandRead(nonce[:])
	if err != nil {
		return IFCERFTEncryptedDat{}, err
	}

	sealedData := secretbox.Seal(nil, data, &nonce, &key)

	return IFCERFTEncryptedDat{
		Version:       IFCERFTEncryptionSecretbox,
		Nonce:         nonce[:],
		EncryptedData: sealedData,
	}, nil
}

// EncryptPrivateMetadata implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) EncryptPrivateMetadata(pmd *IFCERFTPrivateMetadata, key IFCERFTTLFCryptKey) (encryptedPmd IFCERFTEncryptedPrivateMetadata, err error) {
	encodedPmd, err := c.codec.Encode(pmd)
	if err != nil {
		return
	}

	encryptedData, err := c.encryptData(encodedPmd, key.data)
	if err != nil {
		return
	}

	encryptedPmd = IFCERFTEncryptedPrivateMetadata(encryptedData)
	return
}

func (c CryptoCommon) decryptData(encryptedData IFCERFTEncryptedDat, key [32]byte) ([]byte, error) {
	if encryptedData.Version != IFCERFTEncryptionSecretbox {
		return nil, IFCERFTUnknownEncryptionVer{encryptedData.Version}
	}

	var nonce [24]byte
	if len(encryptedData.Nonce) != len(nonce) {
		return nil, IFCERFTInvalidNonceError{encryptedData.Nonce}
	}
	copy(nonce[:], encryptedData.Nonce)

	decryptedData, ok := secretbox.Open(nil, encryptedData.EncryptedData, &nonce, &key)
	if !ok {
		return nil, libkb.DecryptionError{}
	}

	return decryptedData, nil
}

// DecryptPrivateMetadata implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) DecryptPrivateMetadata(encryptedPmd IFCERFTEncryptedPrivateMetadata, key IFCERFTTLFCryptKey) (*IFCERFTPrivateMetadata, error) {
	encodedPmd, err := c.decryptData(IFCERFTEncryptedDat(encryptedPmd), key.data)
	if err != nil {
		return nil, err
	}

	var pmd IFCERFTPrivateMetadata
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
		return nil, IFCERFTUnexpectedShortCryptoRandRead{}
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
		return nil, IFCERFTPaddedBlockReadError{ActualLen: len(paddedBlock), ExpectedLen: blockEndPos}
	}
	return buf.Next(int(blockLen)), nil
}

// EncryptBlock implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) EncryptBlock(block IFCERFTBlock, key IFCERFTBlockCryptKey) (plainSize int, encryptedBlock IFCERFTEncryptedBlock, err error) {
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
	encryptedBlock = IFCERFTEncryptedBlock(encryptedData)
	return
}

// DecryptBlock implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) DecryptBlock(encryptedBlock IFCERFTEncryptedBlock, key IFCERFTBlockCryptKey, block IFCERFTBlock) error {
	paddedBlock, err := c.decryptData(IFCERFTEncryptedDat(encryptedBlock), key.data)
	if err != nil {
		return err
	}

	encodedBlock, err := c.depadBlock(paddedBlock)
	if err != nil {
		return err
	}

	err = c.codec.Decode(encodedBlock, &block)
	if err != nil {
		return IFCERFTBlockDecodeError{err}
	}
	return nil
}

// GetTLFCryptKeyServerHalfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) GetTLFCryptKeyServerHalfID(
	user keybase1.UID, deviceKID keybase1.KID,
	serverHalf IFCERFTTLFCryptKeyServerHalf) (IFCERFTTLFCryptKeyServerHalfID, error) {
	key := serverHalf.data[:]
	data := append(user.ToBytes(), deviceKID.ToBytes()...)
	hmac, err := IFCERFTDefaultHMAC(key, data)
	if err != nil {
		return IFCERFTTLFCryptKeyServerHalfID{}, err
	}
	return IFCERFTTLFCryptKeyServerHalfID{
		ID: hmac,
	}, nil
}

// VerifyTLFCryptKeyServerHalfID implements the Crypto interface for CryptoCommon.
func (c CryptoCommon) VerifyTLFCryptKeyServerHalfID(serverHalfID IFCERFTTLFCryptKeyServerHalfID, user keybase1.UID, deviceKID keybase1.KID, serverHalf IFCERFTTLFCryptKeyServerHalf) error {
	key := serverHalf.data[:]
	data := append(user.ToBytes(), deviceKID.ToBytes()...)
	return serverHalfID.ID.Verify(key, data)
}

// EncryptMerkleLeaf encrypts a Merkle leaf node with the TLFPublicKey.
func (c CryptoCommon) EncryptMerkleLeaf(leaf MerkleLeaf, pubKey IFCERFTTLFPublicKey, nonce *[24]byte, ePrivKey IFCERFTTLFEphemeralPrivateKey) (IFCERFTEncryptedMerkleLeaf, error) {
	// encode the clear-text leaf
	leafBytes, err := c.codec.Encode(leaf)
	if err != nil {
		return IFCERFTEncryptedMerkleLeaf{}, err
	}
	// encrypt the encoded leaf
	encryptedData := box.Seal(nil, leafBytes[:], nonce, (*[32]byte)(&pubKey.data), (*[32]byte)(&ePrivKey.data))
	return IFCERFTEncryptedMerkleLeaf{
		Version:       IFCERFTEncryptionSecretbox,
		EncryptedData: encryptedData,
	}, nil
}

// DecryptMerkleLeaf decrypts a Merkle leaf node with the TLFPrivateKey.
func (c CryptoCommon) DecryptMerkleLeaf(encryptedLeaf IFCERFTEncryptedMerkleLeaf, privKey IFCERFTTLFPrivateKey, nonce *[24]byte, ePubKey IFCERFTTLFEphemeralPublicKey) (*MerkleLeaf, error) {
	if encryptedLeaf.Version != IFCERFTEncryptionSecretbox {
		return nil, IFCERFTUnknownEncryptionVer{encryptedLeaf.Version}
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
