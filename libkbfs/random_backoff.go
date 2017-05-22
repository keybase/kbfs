// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/keybase/kbfs/kbfscrypto"
)

// fireOnce is a construct to synchronize different go routines. Specifically,
// one routine can use wait() to wait on a signal, and another routine can call
// fire() to wake up the first routine. Only the first call to fire() is
// effective, and multiple calls to fire() doesn't panic.
//
// A zero value fireOnce is valid, and no-op for both fire() and wait(). Use
// newFireOnce() to make one that's fire-able.
//
// This is to replace the use case where a channel may be used to sychronize
// different go routines, and one routine waits on a channel read while another
// closes the channel to signal the first routine. fireOnce addresses the issue
// where second call to close the channel can panic.
type fireOnce struct {
	ch   chan struct{}
	once *sync.Once
}

func newFireOnce() fireOnce {
	return fireOnce{
		ch:   make(chan struct{}),
		once: &sync.Once{},
	}
}

func (o fireOnce) fire() {
	if o.once == nil || o.ch == nil {
		return
	}
	o.once.Do(func() { close(o.ch) })
}

func (o fireOnce) wait() {
	if o.ch == nil {
		return
	}
	<-o.ch
}

// CancellableRandomTimer can be used to wait on a random backoff timer. A
// pointer to a zero value of CancellableRandomTimer if usable.
type CancellableRandomTimer struct {
	mu sync.Mutex
	// A *time.Timer is not enough here since we need to be able to cancel the
	// timer and fire the signal (as opposed to the Stop() method on time.Timer
	// which stops the timer and prevents the signal from being fired) when
	// switching out timers.
	fo fireOnce
}

func (b *CancellableRandomTimer) swap(newFo fireOnce) (oldFo fireOnce) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fo, oldFo = newFo, b.fo
	return oldFo
}

func (b *CancellableRandomTimer) get() fireOnce {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fo
}

// Start starts a random backoff timer. The timer is fast-forward-able with
// b.FireNow(). Use b.Wait() to wait for the timer.
//
// It's OK to call b.Start() multiple times. It essentially resets the timer to
// a new value, i.e., any pending b.Wait() waits until the last effective timer
// completes.
func (b *CancellableRandomTimer) Start(maxWait time.Duration) {
	f := newFireOnce()
	b.swap(f).fire()

	var buf [8]byte
	if err := kbfscrypto.RandRead(buf[:]); err != nil {
		panic(err)
	}
	waitDur := time.Duration(
		int64(binary.LittleEndian.Uint64(buf[:])) % int64(maxWait))

	time.AfterFunc(waitDur, f.fire)
}

// Wait waits on any existing random timer. If there isn't a timer
// started, Wait() returns immediately. If b.Start() is called in the middle of
// the wait, it waits until the new timer completes (no matter it's sonner or
// later than the old timer). If FireNow() is called, Wait() returns
// immediately.
func (b *CancellableRandomTimer) Wait() {
	var oldF fireOnce
	f := b.get()
	for f != oldF {
		f.wait()
		f, oldF = b.get(), f
	}
}

// FireNow fast-forwards any existing timer so that any Wait() calls on b wakes
// up immediately. If no timer exists, this is a no-op.
func (b *CancellableRandomTimer) FireNow() {
	b.swap(fireOnce{}).fire()
}
