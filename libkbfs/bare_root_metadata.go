// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
)

// MakeInitialBareRootMetadata creates a new MutableBareRootMetadata
// instance of the given MetadataVer with revision
// MetadataRevisionInitial, and the given TLF ID and handle. Note that
// if the given ID/handle are private, rekeying must be done
// separately.
func MakeInitialBareRootMetadata(
	ver MetadataVer, tlfID tlf.ID, h tlf.Handle) (
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

func addServerHalfIDs(allServerHalfIDs map[keybase1.UID]map[kbfscrypto.CryptPublicKey][]TLFCryptKeyServerHalfID,
	serverHalfIDs map[keybase1.UID]map[kbfscrypto.CryptPublicKey]TLFCryptKeyServerHalfID) {
	for uid, userServerHalfIDs := range serverHalfIDs {
		if allServerHalfIDs[uid] == nil {
			allServerHalfIDs[uid] = make(map[kbfscrypto.CryptPublicKey][]TLFCryptKeyServerHalfID)
		}
		for key, serverHalf := range userServerHalfIDs {
			allServerHalfIDs[uid][key] =
				append(allServerHalfIDs[uid][key], serverHalf)
		}
	}
}
