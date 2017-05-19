// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"math/rand"
	"sync"
	"time"
)

var randomBackoffRand *rand.Rand

func init() {
	randomBackoffRand = rand.New(rand.NewSource(time.Now().Unix()))
}

type fireOnce struct {
	ch   chan struct{}
	once sync.Once
}

func newFireOnce() *fireOnce {
	return &fireOnce{ch: make(chan struct{})}
}

func (o *fireOnce) fire() {
	if o == nil {
		return
	}
	o.once.Do(func() { close(o.ch) })
}

func (o *fireOnce) wait() {
	if o == nil {
		return
	}
	<-o.ch
}

// CancelableRandomBackoff can be used to wait on a random backoff timer. A
// pointer to a zero value of CancelableRandomBackoff if usable.
type CancelableRandomBackoff struct {
	mu sync.Mutex
	ch *fireOnce
}

func (b *CancelableRandomBackoff) swap(newCh *fireOnce) (oldCh *fireOnce) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ch, oldCh = newCh, b.ch
	return oldCh
}

func (b *CancelableRandomBackoff) get() *fireOnce {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

// Start starts a random backoff timer. The timer is cancelable with
// b.Cancel(). Use b.Wait() to wait for the timer.
//
// It's OK to call b.Start() multiple times. Any pending b.Wait() honors only
// the last call to b.Start() until it fires.
func (b *CancelableRandomBackoff) Start(maxWait time.Duration) {
	f := newFireOnce()
	b.swap(f).fire()
	time.AfterFunc(time.Duration(randomBackoffRand.Int63()%int64(maxWait)),
		f.fire)
}

// Wait waits on any existing random backoff timer. If there isn't a timer
// started, Wait() returns immediately. If b.Start() is called in the middle of
// the wait, the new timer is honored.
func (b *CancelableRandomBackoff) Wait() {
	var oldF *fireOnce
	f := b.get()
	for f != oldF {
		f.wait()
		f, oldF = b.get(), f
	}
}

// Cancel cancels any existing timer. If no timer exists, this is a no-op.
func (b *CancelableRandomBackoff) Cancel() {
	b.swap(nil).fire()
}
