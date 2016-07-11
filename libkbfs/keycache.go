// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	lru "github.com/hashicorp/golang-lru"
)

// KeyCacheStandard is an LRU-based implementation of the KeyCache interface.
type KeyCacheStandard struct {
	lru *lru.Cache
}

type keyCacheKey struct {
	tlf    IFCERFTTlfID
	keyGen IFCERFTKeyGen
}

var _ IFCERFTKeyCache = (*KeyCacheStandard)(nil)

// NewKeyCacheStandard constructs a new KeyCacheStandard with the given
// cache capacity.
func NewKeyCacheStandard(capacity int) *KeyCacheStandard {
	head, err := lru.New(capacity)
	if err != nil {
		panic(err.Error())
	}
	return &KeyCacheStandard{head}
}

// GetTLFCryptKey implements the KeyCache interface for KeyCacheStandard.
func (k *KeyCacheStandard) GetTLFCryptKey(tlf IFCERFTTlfID, keyGen IFCERFTKeyGen) (
	IFCERFTTLFCryptKey, error) {
	cacheKey := keyCacheKey{tlf, keyGen}
	if entry, ok := k.lru.Get(cacheKey); ok {
		if key, ok := entry.(IFCERFTTLFCryptKey); ok {
			return key, nil
		}
		// shouldn't really be possible
		return IFCERFTTLFCryptKey{}, IFCERFTKeyCacheHitError{tlf, keyGen}
	}
	return IFCERFTTLFCryptKey{}, IFCERFTKeyCacheMissError{tlf, keyGen}
}

// PutTLFCryptKey implements the KeyCache interface for KeyCacheStandard.
func (k *KeyCacheStandard) PutTLFCryptKey(tlf IFCERFTTlfID, keyGen IFCERFTKeyGen, key IFCERFTTLFCryptKey) error {
	cacheKey := keyCacheKey{tlf, keyGen}
	k.lru.Add(cacheKey, key)
	return nil
}
