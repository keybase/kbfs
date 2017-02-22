// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"bytes"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/keybase/kbfs/libfs"

	"golang.org/x/net/context"
)

// ProfileList is a node that can list all of the available profiles.
type ProfileList struct{}

var _ fs.Node = ProfileList{}

// Attr implements the fs.Node interface.
func (ProfileList) Attr(_ context.Context, a *fuse.Attr) error {
	a.Mode = os.ModeDir | 0755
	return nil
}

var _ fs.NodeRequestLookuper = ProfileList{}

// Lookup implements the fs.NodeRequestLookuper interface.
func (pl ProfileList) Lookup(_ context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) (node fs.Node, err error) {
	if req.Name == "profile" {
		blockAndReadFn := func(ctx context.Context) ([]byte, error) {
			var buf bytes.Buffer
			err := pprof.StartCPUProfile(&buf)
			if err != nil {
				return nil, err
			}
			defer pprof.StopCPUProfile()

			d := 30 * time.Second
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}

			return buf.Bytes(), nil
		}
		return &SpecialReadBlockingFile{blockAndReadFn}, nil
	} else if req.Name == "trace" {
		blockAndReadFn := func(ctx context.Context) ([]byte, error) {
			var buf bytes.Buffer
			err := trace.Start(&buf)
			if err != nil {
				return nil, err
			}
			defer trace.Stop()

			d := 1 * time.Second
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}

			return buf.Bytes(), nil
		}
		return &SpecialReadBlockingFile{blockAndReadFn}, nil
	}

	f := libfs.ProfileGet(req.Name)
	if f == nil {
		return nil, fuse.ENOENT
	}
	resp.EntryValid = 0
	return &SpecialReadFile{read: f}, nil
}

var _ fs.Handle = ProfileList{}

var _ fs.HandleReadDirAller = ProfileList{}

// ReadDirAll implements the ReadDirAll interface.
func (pl ProfileList) ReadDirAll(_ context.Context) (res []fuse.Dirent, err error) {
	profiles := pprof.Profiles()
	res = make([]fuse.Dirent, 0, len(profiles))
	for _, p := range profiles {
		name := p.Name()
		if !libfs.IsSupportedProfileName(name) {
			continue
		}
		res = append(res, fuse.Dirent{
			Type: fuse.DT_File,
			Name: name,
		})
	}
	res = append(res, fuse.Dirent{
		Type: fuse.DT_File,
		Name: "profile",
	})
	res = append(res, fuse.Dirent{
		Type: fuse.DT_File,
		Name: "trace",
	})
	return res, nil
}

var _ fs.NodeRemover = (*FolderList)(nil)

// Remove implements the fs.NodeRemover interface for ProfileList.
func (ProfileList) Remove(_ context.Context, req *fuse.RemoveRequest) (err error) {
	return fuse.EPERM
}
