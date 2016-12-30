// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/pkg/errors"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/net/context"
)

// CryptoLocal implements the Crypto interface by using a local
// signing key and a local crypt private key.
type CryptoLocal struct {
	CryptoCommon
	kbfscrypto.SigningKeySigner
	cryptPrivateKey kbfscrypto.CryptPrivateKey
}

var _ Crypto = CryptoLocal{}

// NewCryptoLocal constructs a new CryptoLocal instance with the given
// signing key.
func NewCryptoLocal(codec kbfscodec.Codec,
	signingKey kbfscrypto.SigningKey,
	cryptPrivateKey kbfscrypto.CryptPrivateKey) CryptoLocal {
	return CryptoLocal{
		MakeCryptoCommon(codec),
		kbfscrypto.SigningKeySigner{Key: signingKey},
		cryptPrivateKey,
	}
}

func (c CryptoLocal) prepareTLFCryptKeyClientHalf(
	encryptedClientHalf EncryptedTLFCryptKeyClientHalf,
	clientHalf kbfscrypto.TLFCryptKeyClientHalf) (
	nonce [24]byte, err error) {
	if encryptedClientHalf.Version != EncryptionSecretbox {
		return [24]byte{}, errors.WithStack(
			UnknownEncryptionVer{encryptedClientHalf.Version})
	}

	// This check isn't strictly needed, but parallels the
	// implementation in CryptoClient.
	if len(encryptedClientHalf.EncryptedData) != len(clientHalf.Data())+box.Overhead {
		return [24]byte{}, errors.WithStack(libkb.DecryptionError{})
	}

	if len(encryptedClientHalf.Nonce) != len(nonce) {
		return [24]byte{}, errors.WithStack(
			InvalidNonceError{encryptedClientHalf.Nonce})
	}
	copy(nonce[:], encryptedClientHalf.Nonce)
	return nonce, nil
}

// DecryptTLFCryptKeyClientHalf implements the Crypto interface for
// CryptoLocal.
func (c CryptoLocal) DecryptTLFCryptKeyClientHalf(ctx context.Context,
	publicKey kbfscrypto.TLFEphemeralPublicKey,
	encryptedClientHalf EncryptedTLFCryptKeyClientHalf) (
	clientHalf kbfscrypto.TLFCryptKeyClientHalf, err error) {
	nonce, err := c.prepareTLFCryptKeyClientHalf(encryptedClientHalf, clientHalf)
	if err != nil {
		return kbfscrypto.TLFCryptKeyClientHalf{}, err
	}

	publicKeyData := publicKey.Data()
	privateKeyData := c.cryptPrivateKey.Data()
	decryptedData, ok := box.Open(nil, encryptedClientHalf.EncryptedData,
		&nonce, &publicKeyData, &privateKeyData)
	if !ok {
		return kbfscrypto.TLFCryptKeyClientHalf{},
			errors.WithStack(libkb.DecryptionError{})
	}

	if len(decryptedData) != len(clientHalf.Data()) {
		err = errors.WithStack(libkb.DecryptionError{})
		return kbfscrypto.TLFCryptKeyClientHalf{},
			errors.WithStack(libkb.DecryptionError{})
	}

	var clientHalfData [32]byte
	copy(clientHalfData[:], decryptedData)
	return kbfscrypto.MakeTLFCryptKeyClientHalf(clientHalfData), nil
}

// DecryptTLFCryptKeyClientHalfAny implements the Crypto interface for
// CryptoLocal.
func (c CryptoLocal) DecryptTLFCryptKeyClientHalfAny(ctx context.Context,
	keys []EncryptedTLFCryptKeyClientAndEphemeral, _ bool) (
	clientHalf kbfscrypto.TLFCryptKeyClientHalf, index int, err error) {
	if len(keys) == 0 {
		return kbfscrypto.TLFCryptKeyClientHalf{}, -1,
			errors.WithStack(NoKeysError{})
	}
	var firstErr error
	for i, k := range keys {
		nonce, err := c.prepareTLFCryptKeyClientHalf(k.ClientHalf, clientHalf)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ePubKeyData := k.EPubKey.Data()
		privateKeyData := c.cryptPrivateKey.Data()
		decryptedData, ok := box.Open(
			nil, k.ClientHalf.EncryptedData, &nonce,
			&ePubKeyData, &privateKeyData)
		if ok {
			var clientHalfData [32]byte
			copy(clientHalfData[:], decryptedData)
			return kbfscrypto.MakeTLFCryptKeyClientHalf(
				clientHalfData), i, nil
		}
	}
	return kbfscrypto.TLFCryptKeyClientHalf{}, -1, firstErr
}

// Shutdown implements the Crypto interface for CryptoLocal.
func (c CryptoLocal) Shutdown() {}
