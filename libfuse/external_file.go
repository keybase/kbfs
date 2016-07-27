// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"io/ioutil"
	"time"

	"bazil.org/fuse"
	"golang.org/x/net/context"
)

func NewExternalFile(path string, resp *fuse.LookupResponse) *SpecialReadFile {
	resp.EntryValid = 0
	return &SpecialReadFile{
		read: func(context.Context) ([]byte, time.Time, error) {
			data, err := ioutil.ReadFile(path)
			return data, time.Time{}, err
		},
	}
}

func NewByteFile(data []byte, resp *fuse.LookupResponse) *SpecialReadFile {
	resp.EntryValid = 0
	return &SpecialReadFile{
		read: func(context.Context) ([]byte, time.Time, error) {
			return data, time.Time{}, nil
		},
	}
}
