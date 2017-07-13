// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package cache

// Measurable is an interface for types whose size is measurable.
type Measurable interface {
	// Size returns the size of the object, in bytes, including both statically
	// and dynamically sized parts.
	Size() int
}

// memorizedMeasurable is a wrapper around a Measurable that memorizes the size
// to avoid frequent size calculations.
type memorizedMeasurable struct {
	m    Measurable
	size int
}

// Size implements the Measurable interface.
func (m memorizedMeasurable) Size() int {
	if m.size <= 0 {
		m.size = m.m.Size()
	}
	return m.size
}
