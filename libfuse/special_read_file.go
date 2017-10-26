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
// while still keeping the size up to date. Docker tests poll on special files
// with a 1s interval, so we make the valid duration 0.5s here to make docker
// tests refresh on every read.
const specialReadFileValidDuration = time.Second / 2

var _ fs.Node = (*SpecialReadFile)(nil)

// refreshCachedDataIfNeeded reads and caches the data, which expires in
// specialReadFileValidDuration. This exists because Attr needs to call read()
// to get the actual size for the file. If application calls Attr before Open,
// and use the size information from the former, we need to make sure the data
// returned from latter has a size consistent with the former. This mechanism
// is a best effort solution for that.
func (f *SpecialReadFile) refreshCachedDataIfNeeded(ctx context.Context) (
	data []byte, ts time.Time, err error) {
	f.dataLock.Lock()
	defer f.dataLock.Unlock()
	t := time.Now()
	if t.Before(f.dataExpire) {
		return f.data, f.ts, nil
	}
	f.data, f.ts, err = f.read(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	f.dataExpire = t.Add(specialReadFileValidDuration)
	return f.data, f.ts, nil
}

// Attr implements the fs.Node interface for SpecialReadFile.
func (f *SpecialReadFile) Attr(ctx context.Context, a *fuse.Attr) error {
	data, ts, err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return err
	}

	a.Valid = specialReadFileValidDuration
	// Some apps (e.g., Chrome) get confused if we use a 0 size
	// here, as is usual for pseudofiles. So return the actual
	// size, even though it may be racy.
	a.Size = uint64(len(data))
	a.Mtime = ts
	a.Ctime = ts
	a.Mode = 0444
	return nil
}

var _ fs.NodeOpener = (*SpecialReadFile)(nil)

// Open implements the fs.NodeOpener interface for SpecialReadFile.
func (f *SpecialReadFile) Open(ctx context.Context, req *fuse.OpenRequest,
	resp *fuse.OpenResponse) (fs.Handle, error) {
	data, _, err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return nil, err
	}

	resp.Flags |= fuse.OpenDirectIO
	return fs.DataHandle(data), nil
}
