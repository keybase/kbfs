package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type passthroughOptions struct {
	mountPoint string
	rootDir    string
	logFile    string
}

type passthroughFS struct {
	options passthroughOptions
}

func (fs *passthroughFS) Root() (fs.Node, error) {
	return passthroughDir{
		realPath: options.rootDir,
		fs:       fs,
	}, nil
}

const iNodeRoot = 1 << 10

type passthroughDir struct {
	realPath string
	fs       *passthroughFS
}

func (dir *passthroughDir) Attr(ctx context.Context, a *fuse.Attr) (err error) {
	var fi os.FileInfo
	if fi, err = os.Stat(dir.realPath); err != nil {
		return err
	}
	a.Mode = fi.Mode()
	a.Mtime = fi.ModTime()
	a.Size = fi.Size()
	a.Inode = iNodeRoot
	a.Mode = os.ModeDir | 0555
	return nil
}

func (dir *passthroughDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	p := filepath.Join(dir.realPath, name)
	fi, err := os.Stat(p)
	if err != nil {
	}
	if name == "hello" {
		return File{}, nil
	}
	return nil, fuse.ENOENT
}

var dirDirs = []fuse.Dirent{
	{Inode: 2, Name: "hello", Type: fuse.DT_File},
}

func (passthroughDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	return dirDirs, nil
}

type passthroughFile struct{}

const greeting = "hello, world\n"

func (passthroughFile) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = 2
	a.Mode = 0444
	a.Size = uint64(len(greeting))
	return nil
}

func (passthroughFile) ReadAll(ctx context.Context) ([]byte, error) {
	return []byte(greeting), nil
}

func run(options passthroughOptions) {
	c, err := fuse.Mount(
		options.mountPoint,
		fuse.FSName("passthrough"),
		fuse.Subtype("O_O"),
		fuse.LocalVolume(),
		fuse.VolumeName("PassThrough"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	err = fs.Serve(c, &fs{options: options})
	if err != nil {
		log.Fatal(err)
	}

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Fatal(err)
	}
}
