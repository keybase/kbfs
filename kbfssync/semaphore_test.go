// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfssync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSerial(t *testing.T) {
	n := 100
	i := 0

	var wg sync.WaitGroup
	wg.Add(n)

	ctx, cancel := context.WithTimeout(
		context.Background(), 10*time.Second)
	defer cancel()

	s := NewSemaphore()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			err := s.Acquire(ctx, 1)
			require.NoError(t, err)
			i++
		}()
	}

	for i := 0; i < n; i++ {
		s.Release(1)
	}

	wg.Wait()

	require.Equal(t, n, i)
}
