// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

func (r *Root) platformLookup(ctx context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) fs.Node {
	switch req.Name {
	case VolIconFileName:
		volIcon, err := newExternalBundleResourceFile("KeybaseFolder.icns")
		if err != nil {
			r.log().CErrorf(ctx, "Error getting bundle resource path: %s", err)
		} else if volIcon != nil {
			resp.EntryValid = 0
			return volIcon
		}
	case ExtendedAttributeSelfFileName:
		xattrSelf, err := newExternalBundleResourceFile("ExtendedAttributeFinderInfo.bin")
		if err != nil {
			r.log().CErrorf(ctx, "Error getting bundle resource path: %s", err)
		} else if xattrSelf != nil {
			resp.EntryValid = 0
			return xattrSelf
		}
	}
	return nil
}
