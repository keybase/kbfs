// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"testing"
)

// runTestCopiesInParallel runs numCopies of the given test function
// in parallel. This is used to induce failures in flaky tests.
func runTestCopiesInParallel(
	t *testing.T, numCopies int, f func(t *testing.T)) {
	for i := 0; i < numCopies; i++ {
		i := i // capture range variable.
		name := fmt.Sprintf("%d", i)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f(t)
		})
	}
}
