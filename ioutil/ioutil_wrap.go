// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package ioutil

import (
	"os"

	ioutil_base "io/ioutil"

	"github.com/pkg/errors"
)

// ReadFile wraps ReadFile from "io/ioutil".
func ReadFile(filename string) ([]byte, error) {
	buf, err := ioutil_base.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file %q", filename)
	}
	return buf, nil
}

// WriteFile wraps WriteFile from "io/ioutil".
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	err := ioutil_base.WriteFile(filename, data, perm)
	if err != nil {
		return errors.Wrapf(err, "failed to write file %q", filename)
	}
	return nil
}
