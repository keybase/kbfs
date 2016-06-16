// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package main

import (
	"net"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/go-framed-msgpack-rpc"
)

type context struct {
	g *libkb.GlobalContext
}

func newContext() *context {
	// TODO: Remove direct use of libkb.G
	libkb.G.Init()
	libkb.G.ConfigureConfig()
	libkb.G.ConfigureLogging()
	libkb.G.ConfigureCaches()
	libkb.G.ConfigureMerkleClient()
	return &context{g: libkb.G}
}

func (c context) GetLogDir() string {
	return c.g.Env.GetLogDir()
}

func (c context) GetRunMode() libkb.RunMode {
	return c.g.GetRunMode()
}

func (c context) GetSocket(clearError bool) (net.Conn, rpc.Transporter, bool, error) {
	return c.g.GetSocket(clearError)
}
