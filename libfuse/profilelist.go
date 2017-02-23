// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"bytes"
	"io"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/keybase/kbfs/libfs"

	"golang.org/x/net/context"
)

type TimedProfile interface {
	Start(w io.Writer) error
	Stop()
}

type CPUProfile struct{}

func (p CPUProfile) Start(w io.Writer) error {
	return pprof.StartCPUProfile(w)
}

func (p CPUProfile) Stop() {
	pprof.StopCPUProfile()
}

type TraceProfile struct{}

func (p TraceProfile) Start(w io.Writer) error {
	return trace.Start(w)
}

func (p TraceProfile) Stop() {
	trace.Stop()
}

// TimedProfileFile represents a file whose contents are
// determined by a timed profile.
type TimedProfileFile struct {
	duration time.Duration
	profile  TimedProfile
}

var _ fs.Node = TimedProfileFile{}

// Attr implements the fs.Node interface for TimedProfileFile.
func (f TimedProfileFile) Attr(ctx context.Context, a *fuse.Attr) error {
	// Have a low non-zero value for Valid to avoid being swamped
	// with requests.
	a.Valid = 1 * time.Second
	now := time.Now()
	a.Size = 0
	a.Mtime = now
	a.Ctime = now
	a.Mode = 0444
	return nil
}

var _ fs.Handle = TimedProfileFile{}

var _ fs.NodeOpener = TimedProfileFile{}

// Open implements the fs.NodeOpener interface for TimedProfileFile.
func (f TimedProfileFile) Open(ctx context.Context,
	req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	var buf bytes.Buffer
	err := f.profile.Start(&buf)
	if err != nil {
		return nil, err
	}

	select {
	case <-time.After(f.duration):
	case <-ctx.Done():
	}

	f.profile.Stop()

	resp.Flags |= fuse.OpenDirectIO
	return fs.DataHandle(buf.Bytes()), nil
}

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
	if strings.HasPrefix(req.Name, "profile.") {
		dStr := strings.TrimPrefix(req.Name, "profile.")
		d, err := time.ParseDuration(dStr)
		if err != nil {
			return nil, err
		}

		return TimedProfileFile{d, CPUProfile{}}, nil
	} else if strings.HasPrefix(req.Name, "trace.") {
		dStr := strings.TrimPrefix(req.Name, "trace.")
		d, err := time.ParseDuration(dStr)
		if err != nil {
			return nil, err
		}
		return TimedProfileFile{d, TraceProfile{}}, nil
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
		Name: "profile.30s",
	})
	res = append(res, fuse.Dirent{
		Type: fuse.DT_File,
		Name: "trace.1s",
	})
	return res, nil
}

var _ fs.NodeRemover = (*FolderList)(nil)

// Remove implements the fs.NodeRemover interface for ProfileList.
func (ProfileList) Remove(_ context.Context, req *fuse.RemoveRequest) (err error) {
	return fuse.EPERM
}
