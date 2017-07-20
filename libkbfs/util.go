// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// Runs fn (which may block) in a separate goroutine and waits for it
// to finish, unless ctx is cancelled. Returns nil only when fn was
// run to completion and succeeded.  Any closed-over variables updated
// in fn should be considered visible only if nil is returned.
func runUnlessCanceled(ctx context.Context, fn func() error) error {
	c := make(chan error, 1) // buffered, in case the request is canceled
	go func() {
		c <- fn()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c:
		return err
	}
}

// MakeRandomRequestID generates a random ID suitable for tagging a
// request in KBFS, and very likely to be universally unique.
func MakeRandomRequestID() (string, error) {
	// Use a random ID to tag each request.  We want this to be really
	// universally unique, as these request IDs might need to be
	// propagated all the way to the server.  Use a base64-encoded
	// random 128-bit number.
	buf := make([]byte, 128/8)
	err := kbfscrypto.RandRead(buf)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BoolForString returns false if trimmed string is "" (empty), "0", "false", or "no"
func BoolForString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "false" || s == "no" {
		return false
	}
	return true
}

// PrereleaseBuild is set at compile time for prerelease builds
var PrereleaseBuild string

// VersionString returns semantic version string
func VersionString() string {
	if PrereleaseBuild != "" {
		return fmt.Sprintf("%s-%s", Version, PrereleaseBuild)
	}
	return Version
}

// CtxBackgroundSyncKeyType is the type for a context background sync key.
type CtxBackgroundSyncKeyType int

const (
	// CtxBackgroundSyncKey is set in the context for any change
	// notifications that are triggered from a background sync.
	// Observers can ignore these if they want, since they will have
	// already gotten the relevant notifications via LocalChanges.
	CtxBackgroundSyncKey CtxBackgroundSyncKeyType = iota
)

func ctxWithRandomIDReplayable(ctx context.Context, tagKey interface{},
	tagName string, log logger.Logger) context.Context {
	ctx = logger.ConvertRPCTagsToLogTags(ctx)

	id, err := MakeRandomRequestID()
	if err != nil && log != nil {
		log.Warning("Couldn't generate a random request ID: %v", err)
	}
	return NewContextReplayable(ctx, func(ctx context.Context) context.Context {
		logTags := make(logger.CtxLogTags)
		logTags[tagKey] = tagName
		newCtx := logger.NewContextWithLogTags(ctx, logTags)
		if err == nil {
			newCtx = context.WithValue(newCtx, tagKey, id)
		}
		return newCtx
	})
}

// checkDataVersion validates that the data version for a
// block pointer is valid for the given version validator
func checkDataVersion(versioner dataVersioner, p path, ptr BlockPointer) error {
	if ptr.DataVer < FirstValidDataVer {
		return InvalidDataVersionError{ptr.DataVer}
	}
	if versioner != nil && ptr.DataVer > versioner.DataVersion() {
		return NewDataVersionError{p, ptr.DataVer}
	}
	return nil
}

func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return errors.WithStack(ctx.Err())
	default:
		return nil
	}
}

func chargedToForTLF(ctx context.Context, sessionGetter CurrentSessionGetter,
	handle *TlfHandle) (keybase1.UserOrTeamID, error) {
	if handle.Type() == tlf.SingleTeam {
		chargedTo := handle.FirstResolvedWriter()
		if chargedTo.AsTeamOrBust().IsSubTeam() {
			// TODO after CORE-5445: ask the service for the root team
			// ID for this subteam, since that's what should be
			// charged.
			panic(fmt.Sprintf("Trying to charge blocks to subteam %v in TLF %s",
				chargedTo, handle.GetCanonicalName()))
		}
		return chargedTo, nil
	}

	// For private and public folders, use the session user.
	session, err := sessionGetter.GetCurrentSession(ctx)
	if err != nil {
		return keybase1.UserOrTeamID(""), err
	}
	return session.UID.AsUserOrTeam(), nil
}

type useFromCacheOrFetch int

const (
	useFromCache useFromCacheOrFetch = iota
	fetchAndCache
)

// fetchAndCacheDecider is a helper function that helps avoid having
// too frequent calls into a remote server. The caller can provide a
// positive tolerance, to accept stale LimitBytes and UsageBytes
// data. If tolerance is 0 or negative, this always makes a blocking
// RPC to bserver and return latest quota usage.
//
// 1) If the age of cached data is more than blockTolerance, it returns a
// `fetchAndCache` decision, telling the caller to fetch the newest data.
// 2) Otherwise, if the age of cached data is more than bgTolerance,
// a background RPC is spawned to refresh cached data using `backgroundFetch`,
// and the caller is instructed to use the stale data immediately with a
// `useFromCache` decision.
// 3) Otherwise, a `useFromCache` decision is returned.
func fetchAndCacheDecider(
	ctx context.Context, bgTolerance, blockTolerance time.Duration,
	cachedTimestamp time.Time, backgroundFetch func(ctx context.Context),
	backgroundInProcess *int32, log logger.Logger, tagKey interface{},
	tagName string, clock Clock) (decision useFromCacheOrFetch, err error) {
	past := clock.Now().Sub(cachedTimestamp)
	switch {
	case past > blockTolerance || cachedTimestamp.IsZero():
		log.CDebugf(
			ctx, "Blocking on getAndCache. Cached data is %s old.", past)
		// TODO: optimize this to make sure there's only one outstanding RPC. In
		// other words, wait for it to finish if one is already in progress.
		return fetchAndCache, nil
	case past > bgTolerance:
		if atomic.CompareAndSwapInt32(backgroundInProcess, 0, 1) {
			id, err := MakeRandomRequestID()
			if err != nil {
				log.Warning("Couldn't generate a random request ID: %v", err)
			}
			log.CDebugf(ctx, "Cached data is %s old. Spawning getAndCache in "+
				"background with tag:%s=%v.", past, tagName, id)
			go func() {
				// Make a new context so that it doesn't get canceled
				// when returned.
				logTags := make(logger.CtxLogTags)
				logTags[tagKey] = tagName
				bgCtx := logger.NewContextWithLogTags(
					context.Background(), logTags)
				bgCtx = context.WithValue(bgCtx, tagKey, id)
				// Make sure a timeout is on the context, in case the
				// RPC blocks forever somehow, where we'd end up with
				// never resetting backgroundInProcess flag again.
				bgCtx, cancel := context.WithTimeout(bgCtx, 10*time.Second)
				defer cancel()
				backgroundFetch(bgCtx)
				atomic.StoreInt32(backgroundInProcess, 0)
			}()
		} else {
			log.CDebugf(ctx, "Cached data is %s old, but backgroundFetch "+
				"is already running.", past)
		}
	default:
		log.CDebugf(ctx, "Using cached data from %s ago.", past)
	}
	return useFromCache, nil
}
