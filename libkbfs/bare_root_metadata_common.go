// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// TODO: Rename this to bare_root_metadata.go.

package libkbfs

import (
	"context"
	"time"

	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
)

// MakeInitialBareRootMetadata creates a new MutableBareRootMetadata
// instance of the given MetadataVer with revision
// MetadataRevisionInitial, and the given TlfID and
// BareTlfHandle. Note that if the given ID/handle are private,
// rekeying must be done separately.
func MakeInitialBareRootMetadata(
	ver MetadataVer, tlfID TlfID, h BareTlfHandle) (
	MutableBareRootMetadata, error) {
	if ver < FirstValidMetadataVer {
		return nil, InvalidMetadataVersionError{tlfID, ver}
	}
	if ver > SegregatedKeyBundlesVer {
		// Shouldn't be possible at the moment.
		panic("Invalid metadata version")
	}
	if ver < SegregatedKeyBundlesVer {
		return MakeInitialBareRootMetadataV2(tlfID, h)
	}

	return MakeInitialBareRootMetadataV3(tlfID, h)
}

// SignBareRootMetadata signs the given BareRootMetadata and returns a
// *RootMetadataSigned object.
func SignBareRootMetadata(
	ctx context.Context, codec kbfscodec.Codec, signer kbfscrypto.Signer,
	brmd BareRootMetadata, untrustedServerTimestamp time.Time) (
	*RootMetadataSigned, error) {
	// encode the root metadata and sign it
	buf, err := codec.Encode(brmd)
	if err != nil {
		return nil, err
	}

	// Sign normally using the local device private key
	sigInfo, err := signer.Sign(ctx, buf)
	if err != nil {
		return nil, err
	}
	var writerSigInfo kbfscrypto.SignatureInfo
	if mdv2, ok := brmd.(*BareRootMetadataV2); ok {
		writerSigInfo = mdv2.WriterMetadataSigInfo
	} else {
		// TODO: Implement this!
		panic("not implemented")
	}
	return MakeRootMetadataSigned(
		sigInfo, writerSigInfo, brmd, untrustedServerTimestamp)
}
