// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/rcrowley/go-metrics"

type CryptoMeasured struct {
	Crypto
	// Add timers for other Crypto functions as needed.
	makeMdIDTimer metrics.Timer
}

// NewCryptoMeasured creates and returns a new CryptoMeasured instance
// with the given delegate and registry.
func NewCryptoMeasured(delegate Crypto, r metrics.Registry) CryptoMeasured {
	makeMdIDTimer := metrics.GetOrRegisterTimer("Crypto.MakeMdID", r)
	return CryptoMeasured{
		Crypto:        delegate,
		makeMdIDTimer: makeMdIDTimer,
	}
}

// MakeMdID implements the Crypto interface for CryptoMeasured.
func (c CryptoMeasured) MakeMdID(md *RootMetadata) (mdID MdID, err error) {
	c.makeMdIDTimer.Time(func() {
		mdID, err = c.Crypto.MakeMdID(md)
	})
	return mdID, err
}
