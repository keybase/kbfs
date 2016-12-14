// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package ioutil

import (
	"os"

	"github.com/pkg/errors"
)

func MkdirAll(path string, perm os.FileMode) error {
	err := os.MkdirAll(path, perm)
	if err != nil {
		return errors.Wrapf(err, "failed to mkdir (all) %q", path)
	}

	return nil
}
