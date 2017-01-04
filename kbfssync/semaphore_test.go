// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSerial(t *testing.T) {
	n := 100
	count := 0
	ch := make(chan struct{}, n)

	ctx, cancel := context.WithTimeout(
		context.Background(), 10*time.Second)
	defer cancel()

	s := NewSemaphore()
	for i := 0; i < n; i++ {
		go func() {
			err := s.Acquire(ctx, 1)
			require.NoError(t, err)
			count++
			ch <- struct{}{}
		}()
	}

	for i := 0; i < n; i++ {
		s.Release(1)
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	require.Equal(t, n, count)
}
