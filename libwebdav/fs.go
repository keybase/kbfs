// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libwebdav

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/net/webdav"
)

type fileInfo struct{}

func (*fileInfo) Name() string {
	return "XXX"
}

func (*fileInfo) Size() int64 {
	return 0
}

func (*fileInfo) Mode() os.FileMode {
	return os.ModeDir
}

func (*fileInfo) ModTime() time.Time {
	return time.Now()
}

func (*fileInfo) IsDir() bool {
	return true
}

func (*fileInfo) Sys() interface{} {
	return nil
}

type file struct{}

func (f *file) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (f *file) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (f *file) Close() error {
	return nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (f *file) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}

func (f *file) Stat() (os.FileInfo, error) {
	return &fileInfo{}, nil
}

// FS ...
type FS struct{}

// Mkdir ...
func (fs *FS) Mkdir(name string, perm os.FileMode) error {
	fmt.Println("MKDIR CALLED")
	return nil
}

// OpenFile ...
func (fs *FS) OpenFile(name string, flag int, perm os.FileMode) (
	webdav.File, error) {
	fmt.Printf("OPENFILE %s CALLED\n", name)
	return &file{}, nil
}

// RemoveAll ...
func (fs *FS) RemoveAll(name string) error {
	fmt.Println("REMOVEALL CALLED")
	return nil
}

// Rename ...
func (fs *FS) Rename(oldName, newName string) error {
	fmt.Println("RENAME CALLED")
	return nil
}

// Stat ...
func (fs *FS) Stat(name string) (os.FileInfo, error) {
	fmt.Println("STAT CALLED")
	return &fileInfo{}, nil
}
