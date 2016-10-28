// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

import "fmt"

// InvalidID indicates whether the TLF ID string is not parseable
// or invalid.
type InvalidID struct {
	id string
}

func (e InvalidID) Error() string {
	return fmt.Sprintf("Invalid TLF ID %q", e.id)
}
