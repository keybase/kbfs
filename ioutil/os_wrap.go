// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package ioutil

import (
	"os"

	"github.com/pkg/errors"
)

// Mkdir wraps MkdirAll from "os".
func Mkdir(path string, perm os.FileMode) error {
	err := os.MkdirAll(path, perm)
	if err != nil {
		return errors.Wrapf(err, "failed to mkdir %q", path)
	}

	return nil
}

// MkdirAll wraps MkdirAll from "os".
func MkdirAll(path string, perm os.FileMode) error {
	err := os.MkdirAll(path, perm)
	if err != nil {
		return errors.Wrapf(err, "failed to mkdir (all) %q", path)
	}

	return nil
}

// Stat wraps Stat from "os".
func Stat(name string) (os.FileInfo, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat %q", name)
	}

	return info, nil
}

// Remove wraps Remove from "os".
func Remove(name string) error {
	err := os.Remove(name)
	if err != nil {
		return errors.Wrapf(err, "failed to remove %q", name)
	}

	return nil
}

// RemoveAll wraps RemoveAll from "os".
func RemoveAll(name string) error {
	err := os.RemoveAll(name)
	if err != nil {
		return errors.Wrapf(err, "failed to remove (all) %q", name)
	}

	return nil
}

// Rename wraps Rename from "os".
func Rename(oldpath, newpath string) error {
	err := os.Rename(oldpath, newpath)
	if err != nil {
		return errors.Wrapf(
			err, "failed to rename %q to %q", oldpath, newpath)
	}

	return nil
}
