// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/keybase/client/go/logger"

// KeybaseServiceCn defines methods needed to construct KeybaseService
// and Crypto implementations.
type KeybaseServiceCn interface {
	NewKeybaseService(config Config, params InitParams, ctx Context, log logger.Logger) (KeybaseService, error)
	NewCrypto(config Config, params InitParams, ctx Context, log logger.Logger) (Crypto, error)
}
