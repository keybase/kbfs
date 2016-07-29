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
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/kardianos/osext"
)

func newExternalFile(path string) (*SpecialReadFile, error) {
	if path == "" {
		return nil, fmt.Errorf("No path for external file")
	}

	var once sync.Once
	var data []byte
	var err error
	var fileTime time.Time
	return &SpecialReadFile{
		read: func(context.Context) ([]byte, time.Time, error) {
			once.Do(func() {
				var info os.FileInfo
				info, err = os.Stat(path)
				if err != nil {
					return
				}
				fileTime = info.ModTime()
				data, err = ioutil.ReadFile(path)
			})
			return data, fileTime, err
		},
	}, nil
}

func newExternalBundleResourceFile(path string) (*SpecialReadFile, error) {
	bpath, err := bundleResourcePath(path)
	if err != nil {
		return nil, err
	}
	return newExternalFile(bpath)
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
