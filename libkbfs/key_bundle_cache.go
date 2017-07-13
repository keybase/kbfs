// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"strconv"

	"github.com/keybase/kbfs/libkbfs/cache"
	"github.com/keybase/kbfs/tlf"
)

// KeyBundleCacheStandard is an LRU-based implementation of the KeyBundleCache interface.
type KeyBundleCacheStandard struct {
	cache cache.Cache
}

var _ KeyBundleCache = (*KeyBundleCacheStandard)(nil)

type keyBundleCacheKey struct {
	tlf         tlf.ID
	bundleIDStr string
	isWriter    bool
}

func (k keyBundleCacheKey) str() string {
	return k.tlf.String() + k.bundleIDStr + strconv.FormatBool(k.isWriter)
}

// NewKeyBundleCacheLRU constructs a new KeyBundleCacheStandard with LRU
// eviction strategy. The capacity of the cache is set to capacityBytes bytes.
func NewKeyBundleCacheLRU(capacityBytes int) *KeyBundleCacheStandard {
	return &KeyBundleCacheStandard{cache.NewLRUEvictedCache(capacityBytes)}
}

// NewKeyBundleCacheRandom constructs a new KeyBundleCacheStandard with random
// eviction strategy. The capacity of the cache is set to capacityBytes bytes.
func NewKeyBundleCacheRandom(capacityBytes int) *KeyBundleCacheStandard {
	return &KeyBundleCacheStandard{cache.NewRandomEvictedCache(capacityBytes)}
}

// GetTLFReaderKeyBundle implements the KeyBundleCache interface for KeyBundleCacheStandard.
func (k *KeyBundleCacheStandard) GetTLFReaderKeyBundle(
	tlf tlf.ID, bundleID TLFReaderKeyBundleID) (*TLFReaderKeyBundleV3, error) {
	cacheKey := keyBundleCacheKey{tlf, bundleID.String(), false}.str()
	if entry, ok := k.cache.Get(cacheKey); ok {
		if rkb, ok := entry.(TLFReaderKeyBundleV3); ok {
			return &rkb, nil
		}
		// Shouldn't be possible.
		return nil, errors.New("Invalid key bundle type")
	}
	return nil, nil
}

// GetTLFWriterKeyBundle implements the KeyBundleCache interface for KeyBundleCacheStandard.
func (k *KeyBundleCacheStandard) GetTLFWriterKeyBundle(
	tlf tlf.ID, bundleID TLFWriterKeyBundleID) (*TLFWriterKeyBundleV3, error) {
	cacheKey := keyBundleCacheKey{tlf, bundleID.String(), true}.str()
	if entry, ok := k.cache.Get(cacheKey); ok {
		if wkb, ok := entry.(TLFWriterKeyBundleV3); ok {
			return &wkb, nil
		}
		// Shouldn't be possible.
		return nil, errors.New("Invalid key bundle type")
	}
	return nil, nil
}

// PutTLFReaderKeyBundle implements the KeyBundleCache interface for KeyBundleCacheStandard.
func (k *KeyBundleCacheStandard) PutTLFReaderKeyBundle(
	tlf tlf.ID, bundleID TLFReaderKeyBundleID, rkb TLFReaderKeyBundleV3) {
	cacheKey := keyBundleCacheKey{tlf, bundleID.String(), false}.str()
	k.cache.Add(cacheKey, rkb)
}

// PutTLFWriterKeyBundle implements the KeyBundleCache interface for KeyBundleCacheStandard.
func (k *KeyBundleCacheStandard) PutTLFWriterKeyBundle(
	tlf tlf.ID, bundleID TLFWriterKeyBundleID, wkb TLFWriterKeyBundleV3) {
	cacheKey := keyBundleCacheKey{tlf, bundleID.String(), true}.str()
	k.cache.Add(cacheKey, wkb)
}
