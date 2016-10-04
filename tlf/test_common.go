// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package tlf

// FakeTlfID creates a fake public or private TLF ID from the given
// byte.
func FakeTlfID(b byte, public bool) TlfID {
	bytes := [TlfIDByteLen]byte{b}
	if public {
		bytes[TlfIDByteLen-1] = PubTlfIDSuffix
	} else {
		bytes[TlfIDByteLen-1] = TlfIDSuffix
	}
	return TlfID{bytes}
}

func FakeTlfIDByte(id TlfID) byte {
	return id.id[0]
}
