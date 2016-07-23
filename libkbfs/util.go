// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/keybase/client/go/logger"
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
	err := cryptoRandRead(buf)
	if err != nil {
		return "", err
	}
	// TODO: go1.5 has RawURLEncoding which leaves off the padding entirely
	return strings.TrimSuffix(base64.URLEncoding.EncodeToString(buf), "=="), nil
}

// LogTagsFromContextToMap parses log tags from the context into a map of strings.
func LogTagsFromContextToMap(ctx context.Context) (tags map[string]string) {
	if ctx == nil {
		return tags
	}
	logTags, ok := logger.LogTagsFromContext(ctx)
	if !ok || len(logTags) == 0 {
		return tags
	}
	tags = make(map[string]string)
	for key, tag := range logTags {
		if v := ctx.Value(key); v != nil {
			if value, ok := v.(fmt.Stringer); ok {
				tags[tag] = value.String()
			} else if value, ok := v.(string); ok {
				tags[tag] = value
			}
		}
	}
	return tags
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

const (
	// CtxBackgroundSyncKey is set in the context for any change
	// notifications that are triggered from a background sync.
	// Observers can ignore these if they want, since they will have
	// already gotten the relevant notifications via LocalChanges.
	CtxBackgroundSyncKey = "kbfs-background"
)

func ctxWithRandomID(ctx context.Context, tagKey interface{},
	tagName string, log logger.Logger) context.Context {
	// Tag each request with a unique ID
	logTags := make(logger.CtxLogTags)
	logTags[tagKey] = tagName
	newCtx := logger.NewContextWithLogTags(ctx, logTags)
	id, err := MakeRandomRequestID()
	if err != nil {
		if log != nil {
			log.Warning("Couldn't generate a random request ID: %v", err)
		}
	} else {
		newCtx = context.WithValue(newCtx, tagKey, id)
	}
	return newCtx
}

// LogTagsFromContext is a wrapper around logger.LogTagsFromContext
// that simply casts the result to the type expected by
// rpc.Connection.
func LogTagsFromContext(ctx context.Context) (map[interface{}]string, bool) {
	tags, ok := logger.LogTagsFromContext(ctx)
	return map[interface{}]string(tags), ok
}

const (
	// CtxReplayKey is a context key for CtxReplayFunc used by
	// NewContextWithReplayFrom
	CtxReplayKey = "libkbfs-replay"
)

// CtxReplayFunc is a function for replaying a series of changes done on a
// context.
type CtxReplayFunc func(ctx context.Context) context.Context

// NewContextReplayable creates a new context from ctx, with change applied. It
// also makes this change replayable by NewContextWithReplayFrom. When
// replayed, the resulting context is replayable as well.
func NewContextReplayable(ctx context.Context, change CtxReplayFunc) context.Context {
	var replay CtxReplayFunc
	replay = func(ctx context.Context) context.Context {
		return context.WithValue(change(ctx), CtxReplayKey, replay)
	}
	return replay(ctx)
}

// NewContextWithReplayFrom creates a new ctx and replays previous context
// changes by calling a function stored in ctx. If CtxReplayKey is not found in
// ctx, nothing is replayed and a context.Background() is returned.
//
// This is an awful hack for reverting a context.WithCancel. It's useful for
// avoiding some "fast" fuse handlers, such as Mkdir, Attr, from being
// canceled by interrupt and returning EINTR.
func NewContextWithReplayFrom(ctx context.Context) (newCtx context.Context) {
	ret := context.Background()
	if replay, ok := ctx.Value(CtxReplayKey).(CtxReplayFunc); ok {
		return replay(ret)
	}
	return ret
}
