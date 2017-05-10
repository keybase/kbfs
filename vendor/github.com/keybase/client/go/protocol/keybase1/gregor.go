// Auto-generated by avdl-compiler v1.3.13 (https://github.com/keybase/node-avdl-compiler)
//   Input file: avdl/keybase1/gregor.avdl

package keybase1

import (
	gregor1 "github.com/keybase/client/go/protocol/gregor1"
	"github.com/keybase/go-framed-msgpack-rpc/rpc"
	context "golang.org/x/net/context"
)

type GetStateArg struct {
}

func (o GetStateArg) DeepCopy() GetStateArg {
	return GetStateArg{}
}

type GregorInterface interface {
	GetState(context.Context) (gregor1.State, error)
}

func GregorProtocol(i GregorInterface) rpc.Protocol {
	return rpc.Protocol{
		Name: "keybase.1.gregor",
		Methods: map[string]rpc.ServeHandlerDescription{
			"getState": {
				MakeArg: func() interface{} {
					ret := make([]GetStateArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					ret, err = i.GetState(ctx)
					return
				},
				MethodType: rpc.MethodCall,
			},
		},
	}
}

type GregorClient struct {
	Cli rpc.GenericClient
}

func (c GregorClient) GetState(ctx context.Context) (res gregor1.State, err error) {
	err = c.Cli.Call(ctx, "keybase.1.gregor.getState", []interface{}{GetStateArg{}}, &res)
	return
}
