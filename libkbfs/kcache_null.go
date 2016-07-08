// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "errors"

// KeyCacheNull is a placeholder, noop implementation of the KeyCache interface.
type KeyCacheNull struct{}

var _ IFCERFTKeyCache = (*KeyCacheNull)(nil)

// GetTLFCryptKey implements the KeyCache interface for KeyCacheNull.
func (k *KeyCacheNull) GetTLFCryptKey(IFCERFTTlfID, KeyGen) (IFCERFTTLFCryptKey, error) {
	return IFCERFTTLFCryptKey{}, errors.New("NULL")
}

// PutTLFCryptKey implements the KeyCache interface for KeyCacheNull.
func (k *KeyCacheNull) PutTLFCryptKey(IFCERFTTlfID, KeyGen, IFCERFTTLFCryptKey) error {
	return nil
}
