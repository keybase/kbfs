// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"time"

	"github.com/keybase/kbfs/kbfscrypto"
)

// This file contains test functions related to RootMetadata that need
// to be exported for use by other modules' tests.

// NewRootMetadataSignedForTest returns a new RootMetadataSigned
// object at the latest known version for testing.
func NewRootMetadataSignedForTest(id TlfID, h BareTlfHandle) (*RootMetadataSigned, error) {
	md := &BareRootMetadataV2{}
	// MDv3 TODO: uncomment the below when we're ready for MDv3
	// md := &BareRootMetadataV#{}
	err := md.Update(id, h)
	if err != nil {
		return nil, err
	}
	return MakeRootMetadataSigned(
		kbfscrypto.SignatureInfo{}, kbfscrypto.SignatureInfo{},
		md, time.Time{})
}
