// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libdokan

import (
	"sync"
	"time"

	"github.com/keybase/kbfs/dokan"
	"golang.org/x/net/context"
)

// SpecialReadFile represents a file whose contents are determined by
// a function.
type SpecialReadFile struct {
	read func(context.Context) ([]byte, time.Time, error)
	fs   *FS
	emptyFile

	dataLock   sync.Mutex
	data       []byte
	ts         time.Time
	dataExpire time.Time
}

// Docker tests poll on special files with a 1s interval, so we make the valid
// duration 0.5s here to make docker tests refresh on every read.
const specialReadFileValidDuration = time.Second / 2

// refreshCachedDataIfNeeded reads and caches the data, which expires in
// specialReadFileValidDuration. This exists because GetFileInformation needs
// to call read() to get the actual size for the file. If application calls
// GetFileInformation before ReadFile, and use the size information from the
// former, we need to make sure the data returned from latter has a size
// consistent with the former. This mechanism is a best effort solution for
// that.
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

// GetFileInformation does stats for dokan.
func (f *SpecialReadFile) GetFileInformation(ctx context.Context, fi *dokan.FileInfo) (*dokan.Stat, error) {
	f.fs.logEnter(ctx, "SpecialReadFile GetFileInformation")
	data, ts, err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return nil, err
	}

	// Some apps (e.g., Chrome) get confused if we use a 0 size
	// here, as is usual for pseudofiles. So return the actual
	// size, even though it may be racy.
	a, err := defaultFileInformation()
	if err != nil {
		return nil, err
	}
	a.FileAttributes |= dokan.FileAttributeReadonly
	a.FileSize = int64(len(data))
	a.LastWrite = ts
	a.LastAccess = ts
	a.Creation = ts
	return a, nil
}

// ReadFile does reads for dokan.
func (f *SpecialReadFile) ReadFile(ctx context.Context, fi *dokan.FileInfo, bs []byte, offset int64) (int, error) {
	f.fs.logEnter(ctx, "SpecialReadFile ReadFile")
	data, _, err := f.refreshCachedDataIfNeeded(ctx)
	if err != nil {
		return 0, err
	}

	if offset >= int64(len(data)) {
		return 0, nil
	}

	data = data[int(offset):]

	return copy(bs, data), nil
}
