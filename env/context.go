// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package env

import (
	"net"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/go-framed-msgpack-rpc"
)

type Context struct {
	g *libkb.GlobalContext
}

func NewContext() *Context {
	// TODO: Remove direct use of libkb.G
	libkb.G.Init()
	libkb.G.ConfigureConfig()
	libkb.G.ConfigureLogging()
	libkb.G.ConfigureCaches()
	libkb.G.ConfigureMerkleClient()
	return &Context{g: libkb.G}
}

func (c Context) GetLogDir() string {
	return c.g.Env.GetLogDir()
}

func (c Context) GetRunMode() libkb.RunMode {
	return c.g.GetRunMode()
}

func (c Context) GetSocket(clearError bool) (net.Conn, rpc.Transporter, bool, error) {
	return c.g.GetSocket(clearError)
}

func (c Context) ConfigureSocketInfo() error {
	return c.g.ConfigureSocketInfo()
}

func (c Context) NewRPCLogFactory() *libkb.RPCLogFactory {
	return &libkb.RPCLogFactory{Contextified: libkb.NewContextified(c.g)}
}
