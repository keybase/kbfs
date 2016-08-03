// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"time"

	"golang.org/x/net/context"
)

const (
	// CtxReplayKey is a context key for CtxReplayFunc
	CtxReplayKey = "libkbfs-replay"

	// CtxCriticalAwarenessKey is a context key for using criticalAwareness
	CtxCriticalAwarenessKey = "libkbfs-critical"
)

// CtxReplayFunc is a function for replaying a series of changes done on a
// context.
type CtxReplayFunc func(ctx context.Context) context.Context

// CtxNotReplayableError is returned when NewContextWithReplayFrom is called on
// a ctx with no replay func.
type CtxNotReplayableError struct{}

func (e CtxNotReplayableError) Error() string {
	return "Unable to replay on ctx"
}

// NoCriticalAwarenessError is returned when EnterCriticalWithTimeout or
// ExitCritical are called on a ctx without Critical Awareness
type NoCriticalAwarenessError struct{}

func (e NoCriticalAwarenessError) Error() string {
	return "Context doesn't have critical awareness or CtxCriticalAwarenessKey " +
		"already exists in ctx but is not of type *criticalAwareness"
}

// ContextAlreadyHasCriticalAwarenessError is returned when
// NewContextWithCriticalAwareness is called second time on the same ctx, which
// is not supported yet.
type ContextAlreadyHasCriticalAwarenessError struct{}

func (e ContextAlreadyHasCriticalAwarenessError) Error() string {
	return "Context already has critical awareness; only one layer is supported."
}

// NewContextReplayable creates a new context from ctx, with change applied. It
// also makes this change replayable by NewContextWithReplayFrom. When
// replayed, the resulting context is replayable as well.
//
// It is important that all WithValue-isu mutations on ctx is done "replayably"
// (with NewContextReplayable) if any delayed cancellation is used, e.g.
// through EnterCriticalWithTimeout,
func NewContextReplayable(
	ctx context.Context, change CtxReplayFunc) context.Context {
	var replay CtxReplayFunc
	replay = func(ctx context.Context) context.Context {
		ctx = change(ctx)
		// _ is necessary to avoid panic if nil
		replays, _ := ctx.Value(CtxReplayKey).([]CtxReplayFunc)
		replays = append(replays, replay)
		return context.WithValue(ctx, CtxReplayKey, replays)
	}
	return replay(ctx)
}

// NewContextWithReplayFrom constructs a new context out of ctx by calling all
// attached replay functions. This disconnects any existing context.CancelFunc.
func NewContextWithReplayFrom(ctx context.Context) (context.Context, error) {
	if replays, ok := ctx.Value(CtxReplayKey).([]CtxReplayFunc); ok {
		newCtx := context.Background()
		for _, replay := range replays {
			newCtx = replay(newCtx)
		}
		return newCtx, nil
	}
	return nil, CtxNotReplayableError{}
}

type criticalAwareness struct {
	*sync.Cond
	isCritical bool
	timer      *time.Timer
	canceled   bool
	done       chan struct{}
}

func newCriticalAwareness() *criticalAwareness {
	return &criticalAwareness{
		Cond:       sync.NewCond(&sync.Mutex{}),
		isCritical: false,
		timer:      nil,
		canceled:   false,
		done:       make(chan struct{}),
	}
}

// NewContextWithCriticalAwareness creates a new context out of ctx. All replay
// functions attached to ctx are run on the new new context. In addition, the
// new context is made "critical aware". That is, it disconnects the cancelFunc
// from ctx, and watch for the cancellation. When cancellation happens, it
// checks if the associated context is in critical state, and wait until it
// exits the critical state before cancelling the new context. This provides a
// hacky way to allow finer control over cancellation.
//
// Note that, it's important to call context.WithCancel (or its friends) before
// this function if those cancellations need to be controllable (critical
// aware). Otherwise, the new cancelFunc is inheriently NOT "critical aware".
func NewContextWithCriticalAwareness(
	ctx context.Context) (newCtx context.Context, err error) {
	v := ctx.Value(CtxCriticalAwarenessKey)
	if v != nil {
		if _, ok := v.(*criticalAwareness); ok {
			return nil, ContextAlreadyHasCriticalAwarenessError{}
		}
		return nil, NoCriticalAwarenessError{}
	}

	if newCtx, err = NewContextWithReplayFrom(ctx); err != nil {
		return nil, err
	}
	c := newCriticalAwareness()
	newCtx = NewContextReplayable(newCtx,
		func(ctx context.Context) context.Context {
			return context.WithValue(ctx, CtxCriticalAwarenessKey, c)
		})
	newCtx, cancel := context.WithCancel(newCtx)
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		c.L.Lock()
		for c.isCritical {
			c.Wait()
		}
		c.canceled = true
		c.L.Unlock()
		cancel()
	}()
	return newCtx, nil
}

// EnterCriticalWithTimeout can be called on a "critical aware" context
// produced by NewContextWithCriticalAwareness, to indicate that the
// operation(s) associated with the context has entered a critical state, and
// it should not be canceled until ExitCritical is called either explicitly or
// automatically after timeout.
func EnterCriticalWithTimeout(ctx context.Context, timeout time.Duration) error {
	if c, ok := ctx.Value(CtxCriticalAwarenessKey).(*criticalAwareness); ok {
		c.L.Lock()
		defer c.L.Unlock()
		if c.canceled || (c.timer != nil && !c.timer.Stop()) {
			// Too late! The context is already canceled or the timer for
			// ExitCritical is already fired.
			return context.Canceled
		}
		if !c.isCritical {
			c.isCritical = true
			c.Broadcast()
		}
		c.timer = time.AfterFunc(timeout, c.exitCritical)
		return nil
	}
	return NoCriticalAwarenessError{}
}

func (c *criticalAwareness) exitCritical() {
	c.L.Lock()
	defer c.L.Unlock()
	c.exitCriticalLocked()
}

func (c *criticalAwareness) exitCriticalLocked() {
	if c.isCritical {
		c.isCritical = false
		c.Broadcast()
	}
}

// ExitCritical can be called on a "critical aware" context produced by
// NewContextWithCriticalAwareness, to indicate that the operation(s)
// associated with the context has exited a critical state and it is safe to be
// canceled.
func ExitCritical(ctx context.Context) error {
	if c, ok := ctx.Value(CtxCriticalAwarenessKey).(*criticalAwareness); ok {
		c.L.Lock()
		defer c.L.Unlock()
		if c.timer == nil {
			// already exited
			return nil
		}
		c.timer.Stop()
		c.timer = nil
		c.exitCriticalLocked()
		return nil
	}
	return NoCriticalAwarenessError{}
}

// CleanupCriticalAwareContext cleans up a context with critical awareness
// (ctx) and makes the go routine spawned in NewContextWithCriticalAwareness
// exit. As part of the cleanup, this also causes the critical aware context to
// be canceled, since otherwise the cancelFunc would never be called.
//
// Ideally, the parent ctx's cancelFunc is always called upon completion of
// handling a request, in which case this wouldn't be necessary.
func CleanupCriticalAwareContext(ctx context.Context) error {
	if c, ok := ctx.Value(CtxCriticalAwarenessKey).(*criticalAwareness); ok {
		select {
		case c.done <- struct{}{}:
		default:
		}
		return nil
	}
	return NoCriticalAwarenessError{}
}

// DummyBackgroundContextWithCriticalAwarenessForTest generate a "Background"
// context that is critical aware.
func DummyBackgroundContextWithCriticalAwarenessForTest() context.Context {
	if ctx, err := NewContextWithCriticalAwareness(NewContextReplayable(
		context.Background(), func(c context.Context) context.Context {
			return c
		})); err != nil {
		panic(err)
	} else {
		return ctx
	}
}
