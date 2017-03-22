// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import (
	"crypto/hmac"
	"crypto/sha256"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/pkg/errors"
)

// TODO: Move keyGen out of libkbfs.
type keyGen int

type pseudonymInput struct {
	_struct bool `codec:",toarray"`
	Version int
	Name    string
	ID      ID
	KeyGen  keyGen
}

// MakePseudonym makes a TLF pseudonym from the given input.
func MakePseudonym(version int, name string, id ID,
	keyGen keyGen, key [32]byte) ([32]byte, error) {
	input := pseudonymInput{
		Version: version,
		Name:    name,
		ID:      id,
		KeyGen:  keyGen,
	}
	codec := kbfscodec.NewMsgpack()
	buf, err := codec.Encode(input)
	if err != nil {
		return [32]byte{}, err
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write(buf)
	hmacArr := mac.Sum(nil)
	var hmac [32]byte
	if len(hmacArr) != len(hmac) {
		return [32]byte{}, errors.Errorf(
			"Expected array of size %d, got %d",
			len(hmac), len(hmacArr))
	}
	copy(hmac[:], hmacArr)
	return hmac, nil
}
