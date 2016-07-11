// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

// This file contains test functions related to RootMetadata that need
// to be exported for use by other modules' tests.

// NewRootMetadataSignedForTest returns a new RootMetadataSigned for testing.
func NewRootMetadataSignedForTest(id IFCERFTTlfID, h IFCERFTBareTlfHandle) (*IFCERFTRootMetadataSigned, error) {
	rmds := &IFCERFTRootMetadataSigned{}
	err := IFCERFTUpdateNewRootMetadata(&rmds.MD, id, h)
	if err != nil {
		return nil, err
	}
	return rmds, nil
}

// FakeInitialRekey fakes the initial rekey for the given
// RootMetadata. This is necessary since newly-created RootMetadata
// objects don't have enough data to build a TlfHandle from until the
// first rekey.
func FakeInitialRekey(rmd *IFCERFTRootMetadata, h IFCERFTBareTlfHandle) {
	if rmd.ID.IsPublic() {
		panic("Called FakeInitialRekey on public TLF")
	}
	wkb := IFCERFTTLFWriterKeyBundle{
		WKeys: make(IFCERFTUserDeviceKeyInfoMap),
	}
	for _, w := range h.Writers {
		k := MakeFakeCryptPublicKeyOrBust(string(w))
		wkb.WKeys[w] = IFCERFTFillInDeviceInf{
			k.kid: IFCERFTTLFCryptKeyInfo{},
		}
	}
	rmd.WKeys = IFCERFTTLFWriterKeyGenerations{wkb}

	rkb := IFCERFTTLFReaderKeyBundle{
		RKeys: make(IFCERFTUserDeviceKeyInfoMap),
	}
	for _, r := range h.Readers {
		k := MakeFakeCryptPublicKeyOrBust(string(r))
		rkb.RKeys[r] = IFCERFTFillInDeviceInf{
			k.kid: IFCERFTTLFCryptKeyInfo{},
		}
	}
	rmd.RKeys = IFCERFTTLFReaderKeyGenerations{rkb}
}
