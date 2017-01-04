// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var testTimeout = 10 * time.Second

func requireEmpty(t *testing.T, errCh <-chan error) {
	select {
	case err := <-errCh:
		t.Fatalf("Unexpected error: %+v", err)
	default:
	}
}

// TestSimple tests that Adjust, Acquire, and Release work in a simple
// two-goroutine scenario.
func TestSimple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	var n int64 = 10

	s := NewSemaphore()
	require.Equal(t, int64(0), s.Count())

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Acquire(ctx, n)
	}()

	requireEmpty(t, errCh)

	s.Adjust(n - 1)
	require.Equal(t, n-1, s.Count())

	requireEmpty(t, errCh)

	s.Release(1)

	// s.Count() should go to n, then 0.

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	require.Equal(t, int64(0), s.Count())
}

// TestCancel tests that cancelling the context passed into Acquire
// causes it to return an error.
func TestCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()

	var n int64 = 10

	s := NewSemaphore()
	require.Equal(t, int64(0), s.Count())

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Acquire(ctx2, n)
	}()

	requireEmpty(t, errCh)

	s.Adjust(n - 1)
	require.Equal(t, n-1, s.Count())

	requireEmpty(t, errCh)

	cancel2()
	require.Equal(t, n-1, s.Count())

	select {
	case err := <-errCh:
		require.Equal(t, ctx2.Err(), errors.Cause(err))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	require.Equal(t, n-1, s.Count())
}

// TestSerialRelease tests that Release(1) causes exactly one waiting
// Acquire(1) to wake up at a time.
func TestSerialRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	acquirerCount := 100

	s := NewSemaphore()
	n := 0
	errCh := make(chan error, acquirerCount)
	for i := 0; i < acquirerCount; i++ {
		go func() {
			err := s.Acquire(ctx, 1)
			n++
			errCh <- err
		}()
	}

	for i := 0; i < acquirerCount; i++ {
		requireEmpty(t, errCh)

		s.Release(1)
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}

		requireEmpty(t, errCh)

		require.Equal(t, int64(0), s.Count())
	}

	// n should have been incremented race-free.
	require.Equal(t, n, acquirerCount)
}

func TestAcquireDifferentSizes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	acquirerCount := 10

	type acquirer struct {
		n   int64
		err error
	}

	// Shadow the global requireEmpty.
	var requireEmpty = func(t *testing.T, acquirerCh <-chan acquirer) {
		select {
		case a := <-acquirerCh:
			t.Fatalf("Unexpected acquirer: %+v", a)
		default:
		}
	}

	s := NewSemaphore()
	n := 0
	acquirerCh := make(chan acquirer, acquirerCount)
	for i := 0; i < acquirerCount; i++ {
		go func(i int) {
			err := s.Acquire(ctx, int64(i+1))
			n++
			acquirerCh <- acquirer{int64(i + 1), err}
		}(i)
	}

	for i := 0; i < acquirerCount; i++ {
		requireEmpty(t, acquirerCh)

		if i == 0 {
			require.Equal(t, int64(0), s.Count())
		} else {
			s.Release(int64(i))
			require.Equal(t, int64(i), s.Count())
		}

		requireEmpty(t, acquirerCh)

		s.Release(1)

		select {
		case a := <-acquirerCh:
			require.Equal(t, acquirer{int64(i + 1), nil}, a)
		case <-ctx.Done():
			t.Fatalf("err=%+v, i=%d", ctx.Err(), i)
		}

		requireEmpty(t, acquirerCh)

		require.Equal(t, int64(0), s.Count())
	}

	// n should have been incremented race-free.
	require.Equal(t, n, acquirerCount)
}
