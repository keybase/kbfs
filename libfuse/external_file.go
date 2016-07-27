// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/osext"

	"golang.org/x/net/context"
)

// newByteFile constructs a special file from bytes
func newByteFile(data []byte, t time.Time) *SpecialReadFile {
	return &SpecialReadFile{
		read: func(context.Context) ([]byte, time.Time, error) {
			return data, t, nil
		},
	}
}

func newExternalFile(path string) (*SpecialReadFile, error) {
	if path == "" {
		return nil, fmt.Errorf("No path for external file")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	fileTime := info.ModTime()
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return newByteFile(data, fileTime), nil
}

func bundleResourcePath(path string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("Bundle resource path only available on macOS/darwin")
	}
	execPath, err := osext.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(execPath, "..", "..", "..", "Resources", path), nil
}
