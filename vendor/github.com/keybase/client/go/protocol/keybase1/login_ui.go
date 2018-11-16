// Auto-generated by avdl-compiler v1.3.26 (https://github.com/keybase/node-avdl-compiler)
//   Input file: avdl/keybase1/login_ui.avdl

package keybase1

import (
	"github.com/keybase/go-framed-msgpack-rpc/rpc"
	context "golang.org/x/net/context"
)

type GetEmailOrUsernameArg struct {
	SessionID int `codec:"sessionID" json:"sessionID"`
}

type PromptRevokePaperKeysArg struct {
	SessionID int    `codec:"sessionID" json:"sessionID"`
	Device    Device `codec:"device" json:"device"`
	Index     int    `codec:"index" json:"index"`
}

type DisplayPaperKeyPhraseArg struct {
	SessionID int    `codec:"sessionID" json:"sessionID"`
	Phrase    string `codec:"phrase" json:"phrase"`
}

type DisplayPrimaryPaperKeyArg struct {
	SessionID int    `codec:"sessionID" json:"sessionID"`
	Phrase    string `codec:"phrase" json:"phrase"`
}

type LoginUiInterface interface {
	GetEmailOrUsername(context.Context, int) (string, error)
	PromptRevokePaperKeys(context.Context, PromptRevokePaperKeysArg) (bool, error)
	DisplayPaperKeyPhrase(context.Context, DisplayPaperKeyPhraseArg) error
	DisplayPrimaryPaperKey(context.Context, DisplayPrimaryPaperKeyArg) error
}

func LoginUiProtocol(i LoginUiInterface) rpc.Protocol {
	return rpc.Protocol{
		Name: "keybase.1.loginUi",
		Methods: map[string]rpc.ServeHandlerDescription{
			"getEmailOrUsername": {
				MakeArg: func() interface{} {
					var ret [1]GetEmailOrUsernameArg
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[1]GetEmailOrUsernameArg)
					if !ok {
						err = rpc.NewTypeError((*[1]GetEmailOrUsernameArg)(nil), args)
						return
					}
					ret, err = i.GetEmailOrUsername(ctx, typedArgs[0].SessionID)
					return
				},
			},
			"promptRevokePaperKeys": {
				MakeArg: func() interface{} {
					var ret [1]PromptRevokePaperKeysArg
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[1]PromptRevokePaperKeysArg)
					if !ok {
						err = rpc.NewTypeError((*[1]PromptRevokePaperKeysArg)(nil), args)
						return
					}
					ret, err = i.PromptRevokePaperKeys(ctx, typedArgs[0])
					return
				},
			},
			"displayPaperKeyPhrase": {
				MakeArg: func() interface{} {
					var ret [1]DisplayPaperKeyPhraseArg
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[1]DisplayPaperKeyPhraseArg)
					if !ok {
						err = rpc.NewTypeError((*[1]DisplayPaperKeyPhraseArg)(nil), args)
						return
					}
					err = i.DisplayPaperKeyPhrase(ctx, typedArgs[0])
					return
				},
			},
			"displayPrimaryPaperKey": {
				MakeArg: func() interface{} {
					var ret [1]DisplayPrimaryPaperKeyArg
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[1]DisplayPrimaryPaperKeyArg)
					if !ok {
						err = rpc.NewTypeError((*[1]DisplayPrimaryPaperKeyArg)(nil), args)
						return
					}
					err = i.DisplayPrimaryPaperKey(ctx, typedArgs[0])
					return
				},
			},
		},
	}
}

type LoginUiClient struct {
	Cli rpc.GenericClient
}

func (c LoginUiClient) GetEmailOrUsername(ctx context.Context, sessionID int) (res string, err error) {
	__arg := GetEmailOrUsernameArg{SessionID: sessionID}
	err = c.Cli.CallCompressed(ctx, "keybase.1.loginUi.getEmailOrUsername", []interface{}{__arg}, &res, rpc.CompressionNone)
	return
}

func (c LoginUiClient) PromptRevokePaperKeys(ctx context.Context, __arg PromptRevokePaperKeysArg) (res bool, err error) {
	err = c.Cli.CallCompressed(ctx, "keybase.1.loginUi.promptRevokePaperKeys", []interface{}{__arg}, &res, rpc.CompressionNone)
	return
}

func (c LoginUiClient) DisplayPaperKeyPhrase(ctx context.Context, __arg DisplayPaperKeyPhraseArg) (err error) {
	err = c.Cli.CallCompressed(ctx, "keybase.1.loginUi.displayPaperKeyPhrase", []interface{}{__arg}, nil, rpc.CompressionNone)
	return
}

func (c LoginUiClient) DisplayPrimaryPaperKey(ctx context.Context, __arg DisplayPrimaryPaperKeyArg) (err error) {
	err = c.Cli.CallCompressed(ctx, "keybase.1.loginUi.displayPrimaryPaperKey", []interface{}{__arg}, nil, rpc.CompressionNone)
	return
}
