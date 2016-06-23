// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

type fsRPC struct {
	config Config
	log    logger.Logger
}

func (f fsRPC) favorites(ctx context.Context, path Path) (keybase1.FsListResult, error) {
	favs, err := f.config.KBFSOps().GetFavorites(ctx)
	if err != nil {
		return keybase1.FsListResult{}, err
	}
	files := []keybase1.File{}
	for _, fav := range favs {
		if (fav.Public && path.Public) || (!fav.Public && !path.Public) {
			files = append(files, keybase1.File{Name: fav.Name})
		}
	}
	return keybase1.FsListResult{Files: files}, nil
}

func (f fsRPC) tlf(ctx context.Context, path Path) (keybase1.FsListResult, error) {
	files := []keybase1.File{}

	node, de, err := path.GetNode(ctx, f.config)
	if err != nil {
		return keybase1.FsListResult{}, err
	}

	if node == nil {
		return keybase1.FsListResult{}, fmt.Errorf("Node not found for path: %s", path)
	}

	if de.Type == Dir {
		children, err := f.config.KBFSOps().GetDirChildren(ctx, node)
		if err != nil {
			return keybase1.FsListResult{}, err
		}

		// For entryInfo: for name, entryInfo := range children
		for name := range children {
			files = append(files, keybase1.File{Name: name})
		}
	} else {
		_, name, err := path.DirAndBasename()
		if err != nil {
			return keybase1.FsListResult{}, err
		}
		files = append(files, keybase1.File{Name: name})
	}
	return keybase1.FsListResult{Files: files}, nil
}

func (f fsRPC) keybase(ctx context.Context) (keybase1.FsListResult, error) {
	return keybase1.FsListResult{
		Files: []keybase1.File{
			keybase1.File{Name: "/keybase/public"},
			keybase1.File{Name: "/keybase/private"},
		},
	}, nil
}

func (f fsRPC) root(ctx context.Context) (keybase1.FsListResult, error) {
	return keybase1.FsListResult{
		Files: []keybase1.File{
			keybase1.File{Name: "/keybase"},
		},
	}, nil
}

// FsList implements keyubase1.FsInterface
func (f *fsRPC) FsList(ctx context.Context, arg keybase1.FsListArg) (keybase1.FsListResult, error) {
	f.log.CDebugf(ctx, "fsRpc:List %q", arg.Path)

	kbfsPath, err := NewPath(arg.Path)
	if err != nil {
		return keybase1.FsListResult{}, err
	}

	var result keybase1.FsListResult
	switch kbfsPath.PathType {
	case RootPathType:
		result, err = f.root(ctx)
	case KeybasePathType:
		result, err = f.keybase(ctx)
	case KeybaseChildPathType:
		result, err = f.favorites(ctx, kbfsPath)
	default:
		result, err = f.tlf(ctx, kbfsPath)
	}
	if err != nil {
		f.log.CErrorf(ctx, "Error listing path %q: %s", arg.Path, err)
	}
	return result, err
}
