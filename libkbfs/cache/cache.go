// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package cache

import (
	"sync"

	"github.com/golang/groupcache/lru"
)

// Cache defines an interface for a cache that stores Measurable content.
type Cache interface {
	// Get tries to find and return data assiciated with key.
	Get(key string) (data Measurable, ok bool)
	// Add adds data into the cache, associating it with key. Entries are
	// evicted when necessary.
	Add(key string, data Measurable)
}

type randomEvictedCache struct {
	maxBytes int

	mu          sync.RWMutex
	cachedBytes int
	data        map[string]memorizedMeasurable
}

// NewRandomEvictedCache returns a Cache that uses random eviction strategy.
// The cache will have a capacity of maxBytes bytes. A zero-byte capacity cache
// is valid.
//
// Internally we store a memorizing wrapper for the raw Mesurable to avoid
// unnecessarily frequent size calculations.
func NewRandomEvictedCache(maxBytes int) Cache {
	return &randomEvictedCache{
		maxBytes: maxBytes,
		data:     make(map[string]memorizedMeasurable),
	}
}

// Get impelments the Cache interface.
func (c *randomEvictedCache) Get(key string) (data Measurable, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	memorized, ok := c.data[key]
	if !ok {
		return nil, false
	}
	return memorized.m, ok
}

// Add implements the Cache interface.
func (c *randomEvictedCache) Add(key string, data Measurable) {
	memorized := memorizedMeasurable{m: data}
	if len(key)+memorized.Size() > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.cachedBytes += len(key) + memorized.Size()
	}
	if c.cachedBytes > c.maxBytes {
		for k, v := range c.data {
			delete(c.data, key)
			c.cachedBytes -= len(k) + v.Size()
			if c.cachedBytes <= c.maxBytes {
				break
			}
		}
	}
	c.data[key] = memorized
}

// lruEvictedCache is a thin layer wrapped around
// github.com/golang/groupcache/lru.Cache that 1) makes it routine-safe; 2)
// caps on bytes; and 2) returns Measurable instead of interface{}
type lruEvictedCache struct {
	maxBytes int

	mu          sync.Mutex
	cachedBytes int
	data        *lru.Cache // not routine-safe; protected by mu
}

// NewLRUEvictedCache returns a Cache that uses LRU eviction strategy.
// The cache will have a capacity of maxBytes bytes. A zero-byte capacity cache
// is valid.
//
// Internally we store a memorizing wrapper for the raw Mesurable to avoid
// unnecessarily frequent size calculations.
func NewLRUEvictedCache(maxBytes int) Cache {
	c := &lruEvictedCache{
		maxBytes: maxBytes,
	}
	c.data = &lru.Cache{
		OnEvicted: func(key lru.Key, value interface{}) {
			if memorized, ok := value.(memorizedMeasurable); ok {
				if k, ok := key.(string); ok {
					// no locking here as we do them in public methods Get/Add.
					c.cachedBytes -= len(k) + memorized.Size()
				}
			}
		},
	}
	return c
}

// Get impelments the Cache interface.
func (c *lruEvictedCache) Get(key string) (data Measurable, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.data.Get(lru.Key(key))
	if !ok {
		return nil, false
	}
	memorized, ok := d.(memorizedMeasurable)
	if !ok {
		return nil, false
	}
	return memorized.m, ok
}

// Add implements the Cache interface.
func (c *lruEvictedCache) Add(key string, data Measurable) {
	memorized := memorizedMeasurable{m: data}
	if len(key)+memorized.Size() > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.data.Get(lru.Key(key)); ok {
		if m, ok := v.(memorizedMeasurable); ok {
			c.cachedBytes -= len(key) + m.Size()
		}
	}
	c.cachedBytes += len(key) + memorized.Size()
	for c.cachedBytes > c.maxBytes {
		c.data.RemoveOldest()
	}
	c.data.Add(lru.Key(key), memorized)
}

// notReallyACache is not really a cache but satisfies the cache interface.
type notReallyACache struct{}

// Get implements the Cache interface.
func (notReallyACache) Get(string) (Measurable, bool) { return nil, false }

// Add implements the Cache interface.
func (notReallyACache) Add(string, Measurable) { return }

// NewNotReallyACache returns a Cache that doesn't cache anything and always
// returns <nil, false> on Get.
func NewNotReallyACache() Cache { return notReallyACache{} }
