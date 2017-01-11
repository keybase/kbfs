// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build darwin

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s ROOT MOUNTPOINT\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 2 {
		usage()
		os.Exit(2)
	}
	mountpoint := flag.Arg(1)

	c, err := fuse.Mount(
		mountpoint,
		fuse.FSName("helloworld"),
		fuse.Subtype("hellofs"),
		fuse.VolumeName("Hello world!"),
		fuse.AllowRoot(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Fatal(err)
	}

	log.Println("mounted!")

	err = fs.Serve(c, &FS{rootPath: flag.Arg(0)})
	if err != nil {
		log.Fatal(err)
	}

}

const (
	attrValidDuration = time.Second
)

// FS is the filesystem root
type FS struct {
	rootPath string

	lock    sync.Mutex
	handles map[fuse.HandleID]*Handle
}

func (f *FS) newHandle(h *Handle) fuse.HandleID {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.handles == nil {
		f.handles = make(map[fuse.HandleID]*Handle)
	}

	hid := fuse.HandleID(rand.Int63())
	for f.handles[hid] != nil {
		hid = fuse.HandleID(rand.Int63())
	}
	f.handles[hid] = h

	return hid
}

func (f *FS) forgetHandle(hid fuse.HandleID) {
	f.lock.Lock()
	defer f.lock.Unlock()

	delete(f.handles, hid)
}

func (f *FS) getHandle(hid fuse.HandleID) *Handle {
	f.lock.Lock()
	defer f.lock.Unlock()

	return f.handles[hid]
}

// Root implements fs.FS interface for *FS
func (f *FS) Root() (n fs.Node, err error) {
	defer func() { log.Printf("FS.Root(): %#+v error=%v", n, err) }()
	return &Node{realPath: f.rootPath, isDir: true, fs: f}, nil
}

var _ fs.FSStatfser = (*FS)(nil)

// Statfs implements fs.FSStatfser interface for *FS
func (f *FS) Statfs(ctx context.Context,
	req *fuse.StatfsRequest, resp *fuse.StatfsResponse) (err error) {
	defer func() { log.Printf("FS.Statfs(): error=%v", err) }()
	var stat syscall.Statfs_t
	if err := syscall.Statfs(f.rootPath, &stat); err != nil {
		return err
	}
	resp.Blocks = stat.Blocks
	resp.Bfree = stat.Bfree
	resp.Bavail = stat.Bavail
	resp.Files = 0 // TODO
	resp.Ffree = stat.Ffree
	resp.Bsize = stat.Bsize
	resp.Namelen = 255 // TODO
	resp.Frsize = 8    // TODO

	return nil
}

// Node is the node for both directories and files
type Node struct {
	fs *FS

	realPath string
	isDir    bool
}

func fillAttrWithFileInfo(a *fuse.Attr, fi os.FileInfo) {
	s := fi.Sys().(*syscall.Stat_t)
	a.Valid = attrValidDuration
	a.Inode = s.Ino
	a.Size = uint64(s.Size)
	a.Blocks = uint64(s.Blocks)
	a.Atime = time.Unix(s.Atimespec.Unix())
	a.Mtime = time.Unix(s.Mtimespec.Unix())
	a.Ctime = time.Unix(s.Ctimespec.Unix())
	a.Crtime = time.Unix(s.Birthtimespec.Unix())
	a.Mode = fi.Mode()
	a.Nlink = uint32(s.Nlink)
	a.Uid = s.Uid
	a.Gid = s.Gid
	a.Flags = s.Flags
	a.BlockSize = uint32(s.Blksize)
}

var _ fs.NodeAccesser = (*Node)(nil)

// Access implements fs.NodeAccesser interface for *Node
func (n *Node) Access(ctx context.Context, a *fuse.AccessRequest) (err error) {
	defer func() { log.Printf("%s.Access(): error=%v", n.realPath, err) }()
	fi, err := os.Stat(n.realPath)
	if err != nil {
		return err
	}
	if a.Mask&uint32(fi.Mode()>>6) != a.Mask {
		log.Printf("MASK %v %v", a.Mask, fi.Mode()>>6)
		return fuse.EPERM
	}
	return nil
}

// Attr implements fs.Node interface for *Dir
func (n *Node) Attr(ctx context.Context, a *fuse.Attr) (err error) {
	defer func() { log.Printf("%s.Attr(): %#+v error=%v", n.realPath, a, err) }()
	fi, err := os.Stat(n.realPath)
	if err != nil {
		return err
	}

	fillAttrWithFileInfo(a, fi)

	return nil
}

// Lookup implements fs.Node interface for *Node
func (n *Node) Lookup(ctx context.Context,
	name string) (ret fs.Node, err error) {
	defer func() {
		log.Printf("%s.Lookup(%s): %#+v error=%v",
			n.realPath, name, ret, err)
	}()

	if !n.isDir {
		return nil, fuse.ENOTSUP
	}

	p := filepath.Join(n.realPath, name)
	fi, err := os.Stat(p)

	switch {
	case os.IsNotExist(err):
		return nil, fuse.ENOENT
	case err == nil:
		if fi.IsDir() {
			return &Node{realPath: p, isDir: true, fs: n.fs}, nil
		}
		return &Node{realPath: p, isDir: false, fs: n.fs}, nil
	default:
		return nil, err
	}
}

func getDirentsWithFileInfos(fis []os.FileInfo) (dirs []fuse.Dirent) {
	for _, fi := range fis {
		stat := fi.Sys().(*syscall.Stat_t)
		var tp fuse.DirentType

		switch {
		case fi.IsDir():
			tp = fuse.DT_Dir
		case fi.Mode().IsRegular():
			tp = fuse.DT_File
		default:
			panic("unsupported dirent type")
		}

		dirs = append(dirs, fuse.Dirent{
			Inode: stat.Ino,
			Name:  fi.Name(),
			Type:  tp,
		})
	}

	return dirs
}

func fuseOpenFlagsToOSStuff(f fuse.OpenFlags) (flag int, perm os.FileMode) {
	flag = int(f & fuse.OpenAccessModeMask)
	if f&fuse.OpenAppend != 0 {
		perm |= os.ModeAppend
	}
	if f&fuse.OpenCreate != 0 {
		flag |= os.O_CREATE
	}
	if f&fuse.OpenDirectory != 0 {
		perm |= os.ModeDir
	}
	if f&fuse.OpenExclusive != 0 {
		perm |= os.ModeExclusive
	}
	if f&fuse.OpenNonblock != 0 {
	}
	if f&fuse.OpenSync != 0 {
		flag |= os.O_SYNC
	}
	if f&fuse.OpenTruncate != 0 {
		flag |= os.O_TRUNC
	}

	return flag, perm
}

var _ fs.NodeOpener = (*Node)(nil)

// Open implements fs.NodeOpener interface for *Node
func (n *Node) Open(ctx context.Context,
	req *fuse.OpenRequest, resp *fuse.OpenResponse) (h fs.Handle, err error) {
	flags, perm := fuseOpenFlagsToOSStuff(req.Flags)
	defer func() {
		log.Printf("%s.Open(): %o %o error=%v",
			n.realPath, flags, perm, err)
	}()

	opener := func() (*os.File, error) {
		return os.OpenFile(n.realPath, flags, perm)
	}

	f, err := opener()
	if err != nil {
		return nil, err
	}

	handle := &Handle{fs: n.fs, f: f, reopener: opener}
	resp.Handle = n.fs.newHandle(handle)
	return handle, nil
}

var _ fs.NodeCreater = (*Node)(nil)

// Create implements fs.NodeCreater interface for *Node
func (n *Node) Create(
	ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (
	fsn fs.Node, fsh fs.Handle, err error) {
	flags, _ := fuseOpenFlagsToOSStuff(req.Flags)
	name := filepath.Join(n.realPath, req.Name)
	defer func() {
		log.Printf("%s.Create(%s): %o %o error=%v",
			n.realPath, name, flags, req.Mode, err)
	}()

	opener := func() (f *os.File, err error) {
		return os.OpenFile(name, flags, req.Mode)
	}

	f, err := opener()
	if err != nil {
		return nil, nil, err
	}

	h := &Handle{fs: n.fs, f: f, reopener: opener}

	node := &Node{realPath: filepath.Join(n.realPath, req.Name),
		isDir: req.Mode.IsDir(), fs: n.fs}
	resp.Handle = n.fs.newHandle(h)
	return node, h, nil
}

var _ fs.NodeMkdirer = (*Node)(nil)

// Mkdir implements fs.NodeMkdirer interface for *Node
func (n *Node) Mkdir(ctx context.Context,
	req *fuse.MkdirRequest) (fs.Node, error) {
	name := filepath.Join(n.realPath, req.Name)
	err := os.Mkdir(name, req.Mode)
	if err != nil {
		return nil, err
	}
	return &Node{realPath: name, isDir: true, fs: n.fs}, nil
}

var _ fs.NodeRemover = (*Node)(nil)

// Remove implements fs.NodeRemover interface for *Node
func (n *Node) Remove(ctx context.Context, req *fuse.RemoveRequest) (err error) {
	name := filepath.Join(n.realPath, req.Name)
	defer func() { log.Printf("%s.Remove(%s): error=%v", n.realPath, name, err) }()
	return os.Remove(filepath.Join(n.realPath, req.Name))
}

var _ fs.NodeFsyncer = (*Node)(nil)

// Fsync implements fs.NodeFsyncer interface for *Node
func (n *Node) Fsync(ctx context.Context, req *fuse.FsyncRequest) (err error) {
	defer func() { log.Printf("%s.Fsync(): error=%v", n.realPath, err) }()
	return n.fs.getHandle(req.Handle).f.Sync()
}

func tToTv(t time.Time) (tv syscall.Timeval) {
	tv.Sec = int64(t.Unix())
	tv.Usec = int32(t.UnixNano() % time.Second.Nanoseconds() /
		time.Microsecond.Nanoseconds())
	return tv
}

var _ fs.NodeSetattrer = (*Node)(nil)

// Setattr implements fs.NodeSetattrer interface for *Node
func (n *Node) Setattr(ctx context.Context,
	req *fuse.SetattrRequest, resp *fuse.SetattrResponse) (err error) {
	defer func() { log.Printf("%s.Setattr(): error=%v", n.realPath, err) }()
	if req.Valid.Size() {
		if err = syscall.Truncate(n.realPath, int64(req.Size)); err != nil {
			return nil
		}
	}

	if req.Valid.Mtime() {
		var tvs [2]syscall.Timeval
		if !req.Valid.Atime() {
			tvs[0] = tToTv(time.Now())
		} else {
			tvs[0] = tToTv(req.Atime)
		}
		tvs[1] = tToTv(req.Mtime)
	}

	if req.Valid.Handle() {
		// panic("no idea what this is for")
	}

	if req.Valid.Mode() {
		if err = os.Chmod(n.realPath, req.Mode); err != nil {
			return err
		}
	}

	if req.Valid.Uid() || req.Valid.Gid() {
		if req.Valid.Uid() && req.Valid.Gid() {
			if err = os.Chown(n.realPath, int(req.Uid), int(req.Gid)); err != nil {
				return err
			}
		}
		fi, err := os.Stat(n.realPath)
		if err != nil {
			return err
		}
		s := fi.Sys().(*syscall.Stat_t)
		if req.Valid.Uid() {
			if err = os.Chown(n.realPath, int(req.Uid), int(s.Gid)); err != nil {
				return err
			}
		} else {
			if err = os.Chown(n.realPath, int(s.Uid), int(req.Gid)); err != nil {
				return err
			}
		}
	}

	if req.Valid.Flags() {
		if err = syscall.Chflags(n.realPath, int(req.Flags)); err != nil {
			return err
		}
	}

	return nil
}

var _ fs.NodeRenamer = (*Node)(nil)

// Rename implements fs.NodeRenamer interface for *Node
func (n *Node) Rename(ctx context.Context,
	req *fuse.RenameRequest, newDir fs.Node) (err error) {
	np := filepath.Join(newDir.(*Node).realPath, req.NewName)
	op := filepath.Join(n.realPath, req.OldName)
	defer func() {
		log.Printf("%s.Rename(%s->%s): error=%v",
			n.realPath, op, np, err)
	}()
	return os.Rename(op, np)
}

// Handle represent an open file or directory
type Handle struct {
	fs       *FS
	reopener func() (*os.File, error)

	f *os.File
}

var _ fs.HandleFlusher = (*Handle)(nil)

// Flush implements fs.HandleFlusher interface for *Handle
func (h *Handle) Flush(ctx context.Context,
	req *fuse.FlushRequest) (err error) {
	defer func() { log.Printf("Handle(%s).Flush(): error=%v", h.f.Name(), err) }()
	return h.f.Sync()
}

var _ fs.HandleReadAller = (*Handle)(nil)

// ReadAll implements fs.HandleReadAller interface for *Handle
func (h *Handle) ReadAll(ctx context.Context) (d []byte, err error) {
	defer func() {
		log.Printf("Handle(%s).ReadAll(): error=%v",
			h.f.Name(), err)
	}()
	return ioutil.ReadAll(h.f)
}

var _ fs.HandleReadDirAller = (*Handle)(nil)

// ReadDirAll implements fs.HandleReadDirAller interface for *Handle
func (h *Handle) ReadDirAll(ctx context.Context) (
	dirs []fuse.Dirent, err error) {
	defer func() {
		log.Printf("Handle(%s).ReadDirAll(): %#+v error=%v",
			h.f.Name(), dirs, err)
	}()
	fis, err := h.f.Readdir(0)
	if err != nil {
		return nil, err
	}
	if err = h.f.Close(); err != nil {
		return nil, err
	}

	// reopen the fd so next readdirall would work
	if h.f, err = h.reopener(); err != nil {
		return nil, err
	}

	return getDirentsWithFileInfos(fis), nil
}

var _ fs.HandleReader = (*Handle)(nil)

// Read implements fs.HandleReader interface for *Handle
func (h *Handle) Read(ctx context.Context,
	req *fuse.ReadRequest, resp *fuse.ReadResponse) (err error) {
	defer func() {
		log.Printf("Handle(%s).Read(): error=%v",
			h.f.Name(), err)
	}()
	resp.Data = make([]byte, req.Size)
	n, err := h.f.Read(resp.Data)
	resp.Data = resp.Data[:n]
	return err
}

var _ fs.HandleReleaser = (*Handle)(nil)

// Release implements fs.HandleReleaser interface for *Handle
func (h *Handle) Release(ctx context.Context,
	req *fuse.ReleaseRequest) (err error) {
	defer func() {
		log.Printf("Handle(%s).Release(): error=%v",
			h.f.Name(), err)
	}()
	h.fs.forgetHandle(req.Handle)
	return h.f.Close()
}

var _ fs.HandleWriter = (*Handle)(nil)

// Write implements fs.HandleWriter interface for *Handle
func (h *Handle) Write(ctx context.Context,
	req *fuse.WriteRequest, resp *fuse.WriteResponse) (err error) {
	defer func() {
		log.Printf("Handle(%s).Write(): error=%v",
			h.f.Name(), err)
	}()

	if _, err = h.f.Seek(req.Offset, 0); err != nil {
		return err
	}
	n, err := h.f.Write(req.Data)
	resp.Size = n
	return err
}
