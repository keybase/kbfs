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

func (f fsRPC) favorites(ctx context.Context, path Path) (keybase1.ListResult, error) {
	favs, err := f.config.KBFSOps().GetFavorites(ctx)
	if err != nil {
		return keybase1.ListResult{}, err
	}
	paths := []keybase1.Path{}
	for _, fav := range favs {
		if (fav.Public && path.Public) || (!fav.Public && !path.Public) {
			paths = append(paths, keybase1.Path{Name: fav.Name})
		}
	}
	return keybase1.ListResult{Paths: paths}, nil
}

func (f fsRPC) tlf(ctx context.Context, path Path) (keybase1.ListResult, error) {
	paths := []keybase1.Path{}

	node, de, err := path.GetNode(ctx, f.config)
	if err != nil {
		return keybase1.ListResult{}, err
	}

	if node == nil {
		return keybase1.ListResult{}, fmt.Errorf("Node not found for path: %s", path)
	}

	if de.Type == Dir {
		children, err := f.config.KBFSOps().GetDirChildren(ctx, node)
		if err != nil {
			return keybase1.ListResult{}, err
		}

		// For entryInfo: for name, entryInfo := range children
		for name := range children {
			paths = append(paths, keybase1.Path{Name: name})
		}
	} else {
		_, name, err := path.DirAndBasename()
		if err != nil {
			return keybase1.ListResult{}, err
		}
		paths = append(paths, keybase1.Path{Name: name})
	}
	return keybase1.ListResult{Paths: paths}, nil
}

func (f fsRPC) keybase(ctx context.Context) (keybase1.ListResult, error) {
	return keybase1.ListResult{
		Paths: []keybase1.Path{
			keybase1.Path{Name: "/keybase/public"},
			keybase1.Path{Name: "/keybase/private"},
		},
	}, nil
}

func (f fsRPC) root(ctx context.Context) (keybase1.ListResult, error) {
	return keybase1.ListResult{
		Paths: []keybase1.Path{
			keybase1.Path{Name: "/keybase"},
		},
	}, nil
}

// List implements keyubase1.FsInterface
func (f *fsRPC) List(ctx context.Context, arg keybase1.ListArg) (keybase1.ListResult, error) {
	f.log.CDebugf(ctx, "fsRpc:List %q", arg.Path)

	kbfsPath, err := NewPath(arg.Path)
	if err != nil {
		return keybase1.ListResult{}, err
	}

	var result keybase1.ListResult
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
