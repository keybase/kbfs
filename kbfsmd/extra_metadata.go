// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsmd

import "github.com/keybase/kbfs/kbfscodec"

// ExtraMetadata is a per-version blob of extra metadata which may
// exist outside of the given metadata block, e.g. key bundles for
// post-v2 metadata.
type ExtraMetadata interface {
	MetadataVersion() MetadataVer
	DeepCopy(kbfscodec.Codec) (ExtraMetadata, error)
	MakeSuccessorCopy(kbfscodec.Codec) (ExtraMetadata, error)
}
