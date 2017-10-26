// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"sync"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

// SpecialReadFile represents a file whose contents are determined by
// a function.
type SpecialReadFile struct {
	read func(context.Context) ([]byte, time.Time, error)

	dataLock   sync.Mutex
	data       []byte
	ts         time.Time
	dataExpire time.Time
}

// Have a low non-zero value for Valid to avoid being swamped with requests,
// while still keeping the size up to date.
const specialReadFileValidDuration = time.Second / 2

var _ fs.Node = (*SpecialReadFile)(nil)

func (f *SpecialReadFile) cachedDataExpiredLocked() bool {
	return time.Now().After(f.dataExpire)
}

func (f *SpecialReadFile) refreshCachedDataIfNeeded(ctx context.Context) (err error) {
	f.dataLock.Lock()
	defer f.dataLock.Unlock()
	if time.Now().Before(f.dataExpire) {
		return nil
	}
	f.data, f.ts, err = f.read(ctx)
	if err != nil {
		return err
	}
	f.dataExpire = time.Now().Add(specialReadFileValidDuration)
	return nil
}

// Attr implements the fs.Node interface for SpecialReadFile.
func (f *SpecialReadFile) Attr(ctx context.Context, a *fuse.Attr) error {
	err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return err
	}

	a.Valid = specialReadFileValidDuration
	// Some apps (e.g., Chrome) get confused if we use a 0 size
	// here, as is usual for pseudofiles. So return the actual
	// size, even though it may be racy.
	a.Size = uint64(len(f.data))
	a.Mtime = f.ts
	a.Ctime = f.ts
	a.Mode = 0444
	return nil
}

var _ fs.NodeOpener = (*SpecialReadFile)(nil)

// Open implements the fs.NodeOpener interface for SpecialReadFile.
func (f *SpecialReadFile) Open(ctx context.Context, req *fuse.OpenRequest,
	resp *fuse.OpenResponse) (fs.Handle, error) {
	err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return nil, err
	}

	resp.Flags |= fuse.OpenDirectIO
	return fs.DataHandle(f.data), nil
}
