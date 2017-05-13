// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsmd

import (
	"fmt"

	"github.com/keybase/kbfs/tlf"
)

// MDMissingDataError indicates that we are trying to take get the
// metadata ID of a MD object with no serialized data field.
type MDMissingDataError struct {
	tlfID tlf.ID
}

// Error implements the error interface for MDMissingDataError
func (e MDMissingDataError) Error() string {
	return fmt.Sprintf("No serialized private data in the metadata "+
		"for directory %v", e.tlfID)
}
