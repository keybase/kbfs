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

	// CtxCancellationDelayerKey is a context key for using cancellationDelayer
	CtxCancellationDelayerKey = "libkbfs-cancellation-delayer"
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

// NoCancellationDelayerError is returned when EnableDelayedCancellationWithGracePeriod or
// ExitCritical are called on a ctx without Critical Awareness
type NoCancellationDelayerError struct{}

func (e NoCancellationDelayerError) Error() string {
	return "Context doesn't have critical awareness or CtxCancellationDelayerKey " +
		"already exists in ctx but is not of type *cancellationDelayer"
}

// ContextAlreadyHasCancellationDelayerError is returned when
// NewContextWithCancellationDelayer is called second time on the same ctx, which
// is not supported yet.
type ContextAlreadyHasCancellationDelayerError struct{}

func (e ContextAlreadyHasCancellationDelayerError) Error() string {
	return "Context already has critical awareness; only one layer is supported."
}

// NewContextReplayable creates a new context from ctx, with change applied. It
// also makes this change replayable by NewContextWithReplayFrom. When
// replayed, the resulting context is replayable as well.
//
// It is important that all WithValue-isu mutations on ctx is done "replayably"
// (with NewContextReplayable) if any delayed cancellation is used, e.g.
// through EnableDelayedCancellationWithGracePeriod,
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

type cancellationDelayer struct {
	*sync.Cond
	delayEnabled bool
	timer        *time.Timer
	canceled     bool
	done         chan struct{}
}

func newCancellationDelayer() *cancellationDelayer {
	return &cancellationDelayer{
		Cond:         sync.NewCond(&sync.Mutex{}),
		delayEnabled: false,
		timer:        nil,
		canceled:     false,
		done:         make(chan struct{}),
	}
}

// NewContextWithCancellationDelayer creates a new context out of ctx. All replay
// functions attached to ctx are run on the new new context. In addition, the
// new context is made "cancellation delayable". That is, it disconnects the cancelFunc
// from ctx, and watch for the cancellation. When cancellation happens, it
// checks if delayed cancellation is enabled for the associated context. If so,
// it waits until it's disabled before cancelling the new context. This
// provides a hacky way to allow finer control over cancellation.
//
// Note that, it's important to call context.WithCancel (or its friends) before
// this function if those cancellations need to be controllable ("cancellation
// delayable"). Otherwise, the new cancelFunc is inheriently NOT ("cancellation
// delayable").
//
// If this function is called, it is caller's responsibility to either 1)
// cancel ctx (the context passed in); or 2) call CleanupCancellationDelayer;
// when operations associated with the context is done. Otherwise it leaks go
// routines!
func NewContextWithCancellationDelayer(
	ctx context.Context) (newCtx context.Context, err error) {
	v := ctx.Value(CtxCancellationDelayerKey)
	if v != nil {
		if _, ok := v.(*cancellationDelayer); ok {
			return nil, ContextAlreadyHasCancellationDelayerError{}
		}
		return nil, NoCancellationDelayerError{}
	}

	if newCtx, err = NewContextWithReplayFrom(ctx); err != nil {
		return nil, err
	}
	c := newCancellationDelayer()
	newCtx = NewContextReplayable(newCtx,
		func(ctx context.Context) context.Context {
			return context.WithValue(ctx, CtxCancellationDelayerKey, c)
		})
	newCtx, cancel := context.WithCancel(newCtx)
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		c.L.Lock()
		for c.delayEnabled {
			c.Wait()
		}
		c.canceled = true
		c.L.Unlock()
		cancel()
	}()
	return newCtx, nil
}

// EnableDelayedCancellationWithGracePeriod can be called on a "cancellation
// delayable" context produced by NewContextWithCancellationDelayer, to enable
// delayed cancellation for ctx. This is useful to indicate that the
// operation(s) associated with the context has entered a critical state, and
// it should not be canceled until after timeout or CleanupCancellationDelayer
// is called.
func EnableDelayedCancellationWithGracePeriod(ctx context.Context, timeout time.Duration) error {
	if c, ok := ctx.Value(CtxCancellationDelayerKey).(*cancellationDelayer); ok {
		c.L.Lock()
		defer c.L.Unlock()
		if c.canceled || (c.timer != nil && !c.timer.Stop()) {
			// Too late! The context is already canceled or the timer for
			// disableDelay is already fired.
			return context.Canceled
		}
		if !c.delayEnabled {
			c.delayEnabled = true
			c.Broadcast()
		}
		c.timer = time.AfterFunc(timeout, c.disableDelay)
		return nil
	}
	return NoCancellationDelayerError{}
}

func (c *cancellationDelayer) disableDelay() {
	c.L.Lock()
	defer c.L.Unlock()
	c.disableDelayLocked()
}

func (c *cancellationDelayer) disableDelayLocked() {
	if c.delayEnabled {
		c.delayEnabled = false
		c.Broadcast()
	}
}

// CleanupCancellationDelayer cleans up a context (ctx) that is cancellation
// delayable and makes the go routine spawned in
// NewContextWithCancellationDelayer exit. As part of the cleanup, this also
// causes the cancellation delayable context to be canceled, no matter the
// timeout passed into the EnableDelayedCancellationWithGracePeriod has passed
// or not.
//
// Ideally, the parent ctx's cancelFunc is always called upon completion of
// handling a request, in which case this wouldn't be necessary.
func CleanupCancellationDelayer(ctx context.Context) error {
	if c, ok := ctx.Value(CtxCancellationDelayerKey).(*cancellationDelayer); ok {
		select {
		case c.done <- struct{}{}:
		default:
		}
		return nil
	}
	return NoCancellationDelayerError{}
}

// BackgroundContextWithCancellationDelayer generate a "Background"
// context that is cancellation delayable
func BackgroundContextWithCancellationDelayer() context.Context {
	if ctx, err := NewContextWithCancellationDelayer(NewContextReplayable(
		context.Background(), func(c context.Context) context.Context {
			return c
		})); err != nil {
		panic(err)
	} else {
		return ctx
	}
}
