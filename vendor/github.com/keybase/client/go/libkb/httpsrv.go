// Copyright 2018 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package libkb

// TODO: Remove this file.

import "github.com/keybase/client/go/kbhttp"

type HTTPSrvListenerSource = kbhttp.ListenerSource

func NewHTTPSrv(g *GlobalContext, listenerSource HTTPSrvListenerSource) *HTTPSrv {
	return kbhttp.NewHTTPSrv(g.GetLog(), listenerSource)
}
