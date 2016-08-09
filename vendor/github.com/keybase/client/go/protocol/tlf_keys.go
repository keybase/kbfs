// Auto-generated by avdl-compiler v1.3.1 (https://github.com/keybase/node-avdl-compiler)
//   Input file: avdl/tlf_keys.avdl

package keybase1

import (
	rpc "github.com/keybase/go-framed-msgpack-rpc"
	context "golang.org/x/net/context"
)

type CanonicalTlfName string
type TLFCryptKeys struct {
	TlfID            TLFID            `codec:"tlfID" json:"tlfID"`
	CanonicalName    CanonicalTlfName `codec:"CanonicalName" json:"CanonicalName"`
	FirstValidKeyGen int              `codec:"FirstValidKeyGen" json:"FirstValidKeyGen"`
	CryptKeys        []Bytes32        `codec:"CryptKeys" json:"CryptKeys"`
}

type GetTLFCryptKeysArg struct {
	TlfName string `codec:"tlfName" json:"tlfName"`
}

type TlfKeysInterface interface {
	// getTLFCryptKeys returns TLF crypt keys from all generations.
	GetTLFCryptKeys(context.Context, string) (TLFCryptKeys, error)
}

func TlfKeysProtocol(i TlfKeysInterface) rpc.Protocol {
	return rpc.Protocol{
		Name: "keybase.1.tlfKeys",
		Methods: map[string]rpc.ServeHandlerDescription{
			"getTLFCryptKeys": {
				MakeArg: func() interface{} {
					ret := make([]GetTLFCryptKeysArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetTLFCryptKeysArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetTLFCryptKeysArg)(nil), args)
						return
					}
					ret, err = i.GetTLFCryptKeys(ctx, (*typedArgs)[0].TlfName)
					return
				},
				MethodType: rpc.MethodCall,
			},
		},
	}
}

type TlfKeysClient struct {
	Cli rpc.GenericClient
}

// getTLFCryptKeys returns TLF crypt keys from all generations.
func (c TlfKeysClient) GetTLFCryptKeys(ctx context.Context, tlfName string) (res TLFCryptKeys, err error) {
	__arg := GetTLFCryptKeysArg{TlfName: tlfName}
	err = c.Cli.Call(ctx, "keybase.1.tlfKeys.getTLFCryptKeys", []interface{}{__arg}, &res)
	return
}
