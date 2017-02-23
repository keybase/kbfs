// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/keybase/kbfs/kbfsblock"
)

type cachedQuotaUsage struct {
	timestamp  time.Time
	usageBytes int64
	limitBytes int64
}

// EventuallyConsistentQuotaUsage keeps tracks of quota usage, in a way user of
// which can choose to accept stale data to reduce calls into block servers.
type EventuallyConsistentQuotaUsage struct {
	config Config

	backgroundInProcess int32

	mu     sync.RWMutex
	cached cachedQuotaUsage
}

// NewEventuallyConsistentQuotaUsage creates a new
// EventuallyConsistentQuotaUsage object.
func NewEventuallyConsistentQuotaUsage(
	config Config) *EventuallyConsistentQuotaUsage {
	return &EventuallyConsistentQuotaUsage{
		config: config,
	}
}

func (q *EventuallyConsistentQuotaUsage) getAndCache(
	ctx context.Context) (usage cachedQuotaUsage, err error) {
	quotaInfo, err := q.config.BlockServer().GetUserQuotaInfo(ctx)
	if err != nil {
		return usage, err
	}

	usage.limitBytes = quotaInfo.Limit
	if quotaInfo.Total != nil {
		usage.usageBytes = quotaInfo.Total.Bytes[kbfsblock.UsageWrite]
	} else {
		usage.usageBytes = 0
	}
	usage.timestamp = time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()
	q.cached = usage

	return usage, nil
}

// Get returns KBFS bytes used and limit for user. To help avoid having too
// frequent calls into bserver, caller can provide a positive tolerance, to
// accept stale LimitBytes and UsageByts data.
//
// 1) If the cached (stale) data is older for less than tolerance, the stale
// data is returned immediately.
// 2) If the cached (stale) data is older for more than tolerance, but not
// more than twice of tolerance, a background RPC is spawned to refresh
// cached data, and the stale data is returned immediately.
// 3) If the cached (stale) data is older for more than twice of d, a
// blocking RPC is issued and the function only returns after RPC finishes,
// with the newest data from RPC.
func (q *EventuallyConsistentQuotaUsage) Get(ctx context.Context,
	tolerance time.Duration) (usageBytes, limitBytes int64, err error) {
	c := func() cachedQuotaUsage {
		q.mu.RLock()
		defer q.mu.RUnlock()
		return q.cached
	}()
	past := time.Since(c.timestamp)
	switch {
	case past > 2*tolerance:
		c, err = q.getAndCache(ctx)
		if err != nil {
			return -1, -1, err
		}
	case past > tolerance:
		if atomic.CompareAndSwapInt32(&q.backgroundInProcess, 0, 1) {
			go func() {
				// Make sure a timeout is on the context, in case the RPC blocks
				// forever somehow, where we'd end up with never resetting
				// backgroundInProcess flag again.
				withTimeout, _ := context.WithTimeout(ctx, 10*time.Second)
				q.getAndCache(withTimeout)
				atomic.StoreInt32(&q.backgroundInProcess, 0)
			}()
		}
	default:
	}
	return c.usageBytes, c.limitBytes, nil
}
