// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/kbfsblock"
)

//ECQUCtxTagKey is the type for unique ECQU background opertaion IDs.
type ECQUCtxTagKey struct{}

// ECQUID == "ECQU"
const ECQUID = "ECQU"

type cachedQuotaUsage struct {
	timestamp  time.Time
	usageBytes int64
	limitBytes int64
}

// EventuallyConsistentQuotaUsage keeps tracks of quota usage, in a way user of
// which can choose to accept stale data to reduce calls into block servers.
type EventuallyConsistentQuotaUsage struct {
	config Config
	log    logger.Logger

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
		log:    config.MakeLogger(ECQUID),
	}
}

func (q *EventuallyConsistentQuotaUsage) getAndCache(
	ctx context.Context) (usage cachedQuotaUsage, err error) {
	defer func() {
		q.log.CDebugf(ctx, "getAndCache: error=%v", err)
	}()
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
// 1) If the cached (stale) data is older for less than half of tolerance, the
// stale data is returned immediately.
// 2) If the cached (stale) data is older for more than half of tolerance, but
// not more than tolerance, a background RPC is spawned to refresh cached data,
// and the stale data is returned immediately.
// 3) If the cached (stale) data is older for more than tolerance, a blocking
// RPC is issued and the function only returns after RPC finishes, with the
// newest data from RPC.
func (q *EventuallyConsistentQuotaUsage) Get(ctx context.Context,
	tolerance time.Duration) (usageBytes, limitBytes int64, err error) {
	c := func() cachedQuotaUsage {
		q.mu.RLock()
		defer q.mu.RUnlock()
		return q.cached
	}()
	past := time.Since(c.timestamp)
	switch {
	case past > tolerance:
		q.log.CDebugf(ctx, "Blocking on getAndCache. Cached data is %s old.", past)
		c, err = q.getAndCache(ctx)
		if err != nil {
			return -1, -1, err
		}
	case past > tolerance/2:
		if atomic.CompareAndSwapInt32(&q.backgroundInProcess, 0, 1) {
			id, err := MakeRandomRequestID()
			if err != nil {
				q.log.Warning("Couldn't generate a random request ID: %v", err)
			}
			q.log.CDebugf(ctx, "Cached data is %s old. Spawning getAndCache in "+
				"background with tag:%s=%v.", past, ECQUID, id)
			go func() {
				// Make a new context so that it doesn't get canceled when returned.
				logTags := make(logger.CtxLogTags)
				logTags[ECQUCtxTagKey{}] = ECQUID
				bgCtx := logger.NewContextWithLogTags(context.Background(), logTags)
				bgCtx = context.WithValue(bgCtx, ECQUCtxTagKey{}, id)
				// Make sure a timeout is on the context, in case the RPC blocks
				// forever somehow, where we'd end up with never resetting
				// backgroundInProcess flag again.
				bgCtx, cancel := context.WithTimeout(bgCtx, 10*time.Second)
				defer cancel()
				q.getAndCache(bgCtx)
				atomic.StoreInt32(&q.backgroundInProcess, 0)
			}()
		} else {
			q.log.CDebugf(ctx,
				"Cached data is %s old, but background getAndCache is already running.", past)
		}
	default:
		q.log.CDebugf(ctx, "Returning cached data from %s ago.", past)
	}
	return c.usageBytes, c.limitBytes, nil
}
