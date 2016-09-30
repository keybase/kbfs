// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfscrypto

import (
	"encoding/hex"
	"fmt"

	"golang.org/x/net/context"
)

// SigVer denotes a signature version.
type SigVer int

const (
	// SigED25519 is the signature type for ED25519
	SigED25519 SigVer = 1
)

// IsNil returns true if this SigVer is nil.
func (v SigVer) IsNil() bool {
	return int(v) == 0
}

// SignatureInfo contains all the info needed to verify a signature
// for a message.
type SignatureInfo struct {
	// Exported only for serialization purposes.
	Version      SigVer       `codec:"v"`
	Signature    []byte       `codec:"s"`
	VerifyingKey VerifyingKey `codec:"k"`
}

// IsNil returns true if this SignatureInfo is nil.
func (s SignatureInfo) IsNil() bool {
	return s.Version.IsNil() && len(s.Signature) == 0 && s.VerifyingKey.IsNil()
}

// DeepCopy makes a complete copy of this SignatureInfo.
func (s SignatureInfo) DeepCopy() SignatureInfo {
	signature := make([]byte, len(s.Signature))
	copy(signature[:], s.Signature[:])
	return SignatureInfo{s.Version, signature, s.VerifyingKey}
}

// String implements the fmt.Stringer interface for SignatureInfo.
func (s SignatureInfo) String() string {
	return fmt.Sprintf("SignatureInfo{Version: %d, Signature: %s, "+
		"VerifyingKey: %s}", s.Version, hex.EncodeToString(s.Signature[:]),
		&s.VerifyingKey)
}

// A Signer is something that can sign using an internal private key.
type Signer interface {
	// Sign signs msg with some internal private key.
	Sign(ctx context.Context, msg []byte) (sigInfo SignatureInfo, err error)
	// Sign signs msg with some internal private key and outputs
	// the full serialized NaclSigInfo.
	SignToString(ctx context.Context, msg []byte) (signature string, err error)
}

// SigningKeySigner is a Signer wrapper around a SigningKey.
type SigningKeySigner struct {
	Key SigningKey
}

func (s SigningKeySigner) Sign(
	ctx context.Context, data []byte) (SignatureInfo, error) {
	return s.Key.Sign(data), nil
}

func (s SigningKeySigner) SignToString(
	ctx context.Context, data []byte) (sig string, err error) {
	return s.Key.SignToString(data)
}
