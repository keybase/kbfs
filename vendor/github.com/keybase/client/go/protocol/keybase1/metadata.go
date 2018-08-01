// Auto-generated by avdl-compiler v1.3.24 (https://github.com/keybase/node-avdl-compiler)
//   Input file: avdl/keybase1/metadata.avdl

package keybase1

import (
	"github.com/keybase/go-framed-msgpack-rpc/rpc"
	context "golang.org/x/net/context"
)

type KeyHalf struct {
	User      UID    `codec:"user" json:"user"`
	DeviceKID KID    `codec:"deviceKID" json:"deviceKID"`
	Key       []byte `codec:"key" json:"key"`
}

func (o KeyHalf) DeepCopy() KeyHalf {
	return KeyHalf{
		User:      o.User.DeepCopy(),
		DeviceKID: o.DeviceKID.DeepCopy(),
		Key: (func(x []byte) []byte {
			if x == nil {
				return nil
			}
			return append([]byte{}, x...)
		})(o.Key),
	}
}

type MDBlock struct {
	Version   int    `codec:"version" json:"version"`
	Timestamp Time   `codec:"timestamp" json:"timestamp"`
	Block     []byte `codec:"block" json:"block"`
}

func (o MDBlock) DeepCopy() MDBlock {
	return MDBlock{
		Version:   o.Version,
		Timestamp: o.Timestamp.DeepCopy(),
		Block: (func(x []byte) []byte {
			if x == nil {
				return nil
			}
			return append([]byte{}, x...)
		})(o.Block),
	}
}

type KeyBundle struct {
	Version int    `codec:"version" json:"version"`
	Bundle  []byte `codec:"bundle" json:"bundle"`
}

func (o KeyBundle) DeepCopy() KeyBundle {
	return KeyBundle{
		Version: o.Version,
		Bundle: (func(x []byte) []byte {
			if x == nil {
				return nil
			}
			return append([]byte{}, x...)
		})(o.Bundle),
	}
}

type MetadataResponse struct {
	FolderID string    `codec:"folderID" json:"folderID"`
	MdBlocks []MDBlock `codec:"mdBlocks" json:"mdBlocks"`
}

func (o MetadataResponse) DeepCopy() MetadataResponse {
	return MetadataResponse{
		FolderID: o.FolderID,
		MdBlocks: (func(x []MDBlock) []MDBlock {
			if x == nil {
				return nil
			}
			ret := make([]MDBlock, len(x))
			for i, v := range x {
				vCopy := v.DeepCopy()
				ret[i] = vCopy
			}
			return ret
		})(o.MdBlocks),
	}
}

type MerkleRoot struct {
	Version int    `codec:"version" json:"version"`
	Root    []byte `codec:"root" json:"root"`
}

func (o MerkleRoot) DeepCopy() MerkleRoot {
	return MerkleRoot{
		Version: o.Version,
		Root: (func(x []byte) []byte {
			if x == nil {
				return nil
			}
			return append([]byte{}, x...)
		})(o.Root),
	}
}

type PingResponse struct {
	Timestamp Time `codec:"timestamp" json:"timestamp"`
}

func (o PingResponse) DeepCopy() PingResponse {
	return PingResponse{
		Timestamp: o.Timestamp.DeepCopy(),
	}
}

type KeyBundleResponse struct {
	WriterBundle KeyBundle `codec:"WriterBundle" json:"WriterBundle"`
	ReaderBundle KeyBundle `codec:"ReaderBundle" json:"ReaderBundle"`
}

func (o KeyBundleResponse) DeepCopy() KeyBundleResponse {
	return KeyBundleResponse{
		WriterBundle: o.WriterBundle.DeepCopy(),
		ReaderBundle: o.ReaderBundle.DeepCopy(),
	}
}

type LockID int64

func (o LockID) DeepCopy() LockID {
	return o
}

type MDPriority int

func (o MDPriority) DeepCopy() MDPriority {
	return o
}

type LockContext struct {
	RequireLockID       LockID `codec:"requireLockID" json:"requireLockID"`
	ReleaseAfterSuccess bool   `codec:"releaseAfterSuccess" json:"releaseAfterSuccess"`
}

func (o LockContext) DeepCopy() LockContext {
	return LockContext{
		RequireLockID:       o.RequireLockID.DeepCopy(),
		ReleaseAfterSuccess: o.ReleaseAfterSuccess,
	}
}

type FindNextMDResponse struct {
	KbfsRoot    MerkleRoot `codec:"kbfsRoot" json:"kbfsRoot"`
	MerkleNodes [][]byte   `codec:"merkleNodes" json:"merkleNodes"`
	RootSeqno   Seqno      `codec:"rootSeqno" json:"rootSeqno"`
	RootHash    HashMeta   `codec:"rootHash" json:"rootHash"`
}

func (o FindNextMDResponse) DeepCopy() FindNextMDResponse {
	return FindNextMDResponse{
		KbfsRoot: o.KbfsRoot.DeepCopy(),
		MerkleNodes: (func(x [][]byte) [][]byte {
			if x == nil {
				return nil
			}
			ret := make([][]byte, len(x))
			for i, v := range x {
				vCopy := (func(x []byte) []byte {
					if x == nil {
						return nil
					}
					return append([]byte{}, x...)
				})(v)
				ret[i] = vCopy
			}
			return ret
		})(o.MerkleNodes),
		RootSeqno: o.RootSeqno.DeepCopy(),
		RootHash:  o.RootHash.DeepCopy(),
	}
}

type GetChallengeArg struct {
}

type AuthenticateArg struct {
	Signature string `codec:"signature" json:"signature"`
}

type PutMetadataArg struct {
	MdBlock         MDBlock           `codec:"mdBlock" json:"mdBlock"`
	ReaderKeyBundle KeyBundle         `codec:"readerKeyBundle" json:"readerKeyBundle"`
	WriterKeyBundle KeyBundle         `codec:"writerKeyBundle" json:"writerKeyBundle"`
	LogTags         map[string]string `codec:"logTags" json:"logTags"`
	LockContext     *LockContext      `codec:"lockContext,omitempty" json:"lockContext,omitempty"`
	Priority        MDPriority        `codec:"priority" json:"priority"`
}

type GetMetadataArg struct {
	FolderID      string            `codec:"folderID" json:"folderID"`
	FolderHandle  []byte            `codec:"folderHandle" json:"folderHandle"`
	BranchID      string            `codec:"branchID" json:"branchID"`
	Unmerged      bool              `codec:"unmerged" json:"unmerged"`
	StartRevision int64             `codec:"startRevision" json:"startRevision"`
	StopRevision  int64             `codec:"stopRevision" json:"stopRevision"`
	LogTags       map[string]string `codec:"logTags" json:"logTags"`
	LockBeforeGet *LockID           `codec:"lockBeforeGet,omitempty" json:"lockBeforeGet,omitempty"`
}

type GetMetadataByTimestampArg struct {
	FolderID   string `codec:"folderID" json:"folderID"`
	ServerTime Time   `codec:"serverTime" json:"serverTime"`
}

type RegisterForUpdatesArg struct {
	FolderID     string            `codec:"folderID" json:"folderID"`
	CurrRevision int64             `codec:"currRevision" json:"currRevision"`
	LogTags      map[string]string `codec:"logTags" json:"logTags"`
}

type PruneBranchArg struct {
	FolderID string            `codec:"folderID" json:"folderID"`
	BranchID string            `codec:"branchID" json:"branchID"`
	LogTags  map[string]string `codec:"logTags" json:"logTags"`
}

type PutKeysArg struct {
	KeyHalves []KeyHalf         `codec:"keyHalves" json:"keyHalves"`
	LogTags   map[string]string `codec:"logTags" json:"logTags"`
}

type GetKeyArg struct {
	KeyHalfID []byte            `codec:"keyHalfID" json:"keyHalfID"`
	DeviceKID string            `codec:"deviceKID" json:"deviceKID"`
	LogTags   map[string]string `codec:"logTags" json:"logTags"`
}

type DeleteKeyArg struct {
	Uid       UID               `codec:"uid" json:"uid"`
	DeviceKID KID               `codec:"deviceKID" json:"deviceKID"`
	KeyHalfID []byte            `codec:"keyHalfID" json:"keyHalfID"`
	LogTags   map[string]string `codec:"logTags" json:"logTags"`
}

type TruncateLockArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
}

type TruncateUnlockArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
}

type GetFolderHandleArg struct {
	FolderID  string `codec:"folderID" json:"folderID"`
	Signature string `codec:"signature" json:"signature"`
	Challenge string `codec:"challenge" json:"challenge"`
}

type GetFoldersForRekeyArg struct {
	DeviceKID KID `codec:"deviceKID" json:"deviceKID"`
}

type PingArg struct {
}

type Ping2Arg struct {
}

type GetLatestFolderHandleArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
}

type GetKeyBundlesArg struct {
	FolderID       string `codec:"folderID" json:"folderID"`
	WriterBundleID string `codec:"writerBundleID" json:"writerBundleID"`
	ReaderBundleID string `codec:"readerBundleID" json:"readerBundleID"`
}

type LockArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
	LockID   LockID `codec:"lockID" json:"lockID"`
}

type ReleaseLockArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
	LockID   LockID `codec:"lockID" json:"lockID"`
}

type StartImplicitTeamMigrationArg struct {
	FolderID string `codec:"folderID" json:"folderID"`
}

type GetMerkleRootArg struct {
	TreeID MerkleTreeID `codec:"treeID" json:"treeID"`
	SeqNo  int64        `codec:"seqNo" json:"seqNo"`
}

type GetMerkleRootLatestArg struct {
	TreeID MerkleTreeID `codec:"treeID" json:"treeID"`
}

type GetMerkleRootSinceArg struct {
	TreeID MerkleTreeID `codec:"treeID" json:"treeID"`
	When   Time         `codec:"when" json:"when"`
}

type GetMerkleNodeArg struct {
	Hash string `codec:"hash" json:"hash"`
}

type FindNextMDArg struct {
	Seqno    Seqno  `codec:"seqno" json:"seqno"`
	FolderID string `codec:"folderID" json:"folderID"`
}

type SetImplicitTeamModeForTestArg struct {
	ImplicitTeamMode string `codec:"implicitTeamMode" json:"implicitTeamMode"`
}

type ForceMerkleBuildForTestArg struct {
}

type MetadataInterface interface {
	GetChallenge(context.Context) (ChallengeInfo, error)
	Authenticate(context.Context, string) (int, error)
	PutMetadata(context.Context, PutMetadataArg) error
	GetMetadata(context.Context, GetMetadataArg) (MetadataResponse, error)
	GetMetadataByTimestamp(context.Context, GetMetadataByTimestampArg) (MDBlock, error)
	RegisterForUpdates(context.Context, RegisterForUpdatesArg) error
	PruneBranch(context.Context, PruneBranchArg) error
	PutKeys(context.Context, PutKeysArg) error
	GetKey(context.Context, GetKeyArg) ([]byte, error)
	DeleteKey(context.Context, DeleteKeyArg) error
	TruncateLock(context.Context, string) (bool, error)
	TruncateUnlock(context.Context, string) (bool, error)
	GetFolderHandle(context.Context, GetFolderHandleArg) ([]byte, error)
	GetFoldersForRekey(context.Context, KID) error
	Ping(context.Context) error
	Ping2(context.Context) (PingResponse, error)
	GetLatestFolderHandle(context.Context, string) ([]byte, error)
	GetKeyBundles(context.Context, GetKeyBundlesArg) (KeyBundleResponse, error)
	Lock(context.Context, LockArg) error
	ReleaseLock(context.Context, ReleaseLockArg) error
	StartImplicitTeamMigration(context.Context, string) error
	GetMerkleRoot(context.Context, GetMerkleRootArg) (MerkleRoot, error)
	GetMerkleRootLatest(context.Context, MerkleTreeID) (MerkleRoot, error)
	GetMerkleRootSince(context.Context, GetMerkleRootSinceArg) (MerkleRoot, error)
	GetMerkleNode(context.Context, string) ([]byte, error)
	FindNextMD(context.Context, FindNextMDArg) (FindNextMDResponse, error)
	SetImplicitTeamModeForTest(context.Context, string) error
	ForceMerkleBuildForTest(context.Context) error
}

func MetadataProtocol(i MetadataInterface) rpc.Protocol {
	return rpc.Protocol{
		Name: "keybase.1.metadata",
		Methods: map[string]rpc.ServeHandlerDescription{
			"getChallenge": {
				MakeArg: func() interface{} {
					ret := make([]GetChallengeArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					ret, err = i.GetChallenge(ctx)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"authenticate": {
				MakeArg: func() interface{} {
					ret := make([]AuthenticateArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]AuthenticateArg)
					if !ok {
						err = rpc.NewTypeError((*[]AuthenticateArg)(nil), args)
						return
					}
					ret, err = i.Authenticate(ctx, (*typedArgs)[0].Signature)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"putMetadata": {
				MakeArg: func() interface{} {
					ret := make([]PutMetadataArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]PutMetadataArg)
					if !ok {
						err = rpc.NewTypeError((*[]PutMetadataArg)(nil), args)
						return
					}
					err = i.PutMetadata(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMetadata": {
				MakeArg: func() interface{} {
					ret := make([]GetMetadataArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMetadataArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMetadataArg)(nil), args)
						return
					}
					ret, err = i.GetMetadata(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMetadataByTimestamp": {
				MakeArg: func() interface{} {
					ret := make([]GetMetadataByTimestampArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMetadataByTimestampArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMetadataByTimestampArg)(nil), args)
						return
					}
					ret, err = i.GetMetadataByTimestamp(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"registerForUpdates": {
				MakeArg: func() interface{} {
					ret := make([]RegisterForUpdatesArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]RegisterForUpdatesArg)
					if !ok {
						err = rpc.NewTypeError((*[]RegisterForUpdatesArg)(nil), args)
						return
					}
					err = i.RegisterForUpdates(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"pruneBranch": {
				MakeArg: func() interface{} {
					ret := make([]PruneBranchArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]PruneBranchArg)
					if !ok {
						err = rpc.NewTypeError((*[]PruneBranchArg)(nil), args)
						return
					}
					err = i.PruneBranch(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"putKeys": {
				MakeArg: func() interface{} {
					ret := make([]PutKeysArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]PutKeysArg)
					if !ok {
						err = rpc.NewTypeError((*[]PutKeysArg)(nil), args)
						return
					}
					err = i.PutKeys(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getKey": {
				MakeArg: func() interface{} {
					ret := make([]GetKeyArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetKeyArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetKeyArg)(nil), args)
						return
					}
					ret, err = i.GetKey(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"deleteKey": {
				MakeArg: func() interface{} {
					ret := make([]DeleteKeyArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]DeleteKeyArg)
					if !ok {
						err = rpc.NewTypeError((*[]DeleteKeyArg)(nil), args)
						return
					}
					err = i.DeleteKey(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"truncateLock": {
				MakeArg: func() interface{} {
					ret := make([]TruncateLockArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]TruncateLockArg)
					if !ok {
						err = rpc.NewTypeError((*[]TruncateLockArg)(nil), args)
						return
					}
					ret, err = i.TruncateLock(ctx, (*typedArgs)[0].FolderID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"truncateUnlock": {
				MakeArg: func() interface{} {
					ret := make([]TruncateUnlockArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]TruncateUnlockArg)
					if !ok {
						err = rpc.NewTypeError((*[]TruncateUnlockArg)(nil), args)
						return
					}
					ret, err = i.TruncateUnlock(ctx, (*typedArgs)[0].FolderID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getFolderHandle": {
				MakeArg: func() interface{} {
					ret := make([]GetFolderHandleArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetFolderHandleArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetFolderHandleArg)(nil), args)
						return
					}
					ret, err = i.GetFolderHandle(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getFoldersForRekey": {
				MakeArg: func() interface{} {
					ret := make([]GetFoldersForRekeyArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetFoldersForRekeyArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetFoldersForRekeyArg)(nil), args)
						return
					}
					err = i.GetFoldersForRekey(ctx, (*typedArgs)[0].DeviceKID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"ping": {
				MakeArg: func() interface{} {
					ret := make([]PingArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					err = i.Ping(ctx)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"ping2": {
				MakeArg: func() interface{} {
					ret := make([]Ping2Arg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					ret, err = i.Ping2(ctx)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getLatestFolderHandle": {
				MakeArg: func() interface{} {
					ret := make([]GetLatestFolderHandleArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetLatestFolderHandleArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetLatestFolderHandleArg)(nil), args)
						return
					}
					ret, err = i.GetLatestFolderHandle(ctx, (*typedArgs)[0].FolderID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getKeyBundles": {
				MakeArg: func() interface{} {
					ret := make([]GetKeyBundlesArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetKeyBundlesArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetKeyBundlesArg)(nil), args)
						return
					}
					ret, err = i.GetKeyBundles(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"lock": {
				MakeArg: func() interface{} {
					ret := make([]LockArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]LockArg)
					if !ok {
						err = rpc.NewTypeError((*[]LockArg)(nil), args)
						return
					}
					err = i.Lock(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"releaseLock": {
				MakeArg: func() interface{} {
					ret := make([]ReleaseLockArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]ReleaseLockArg)
					if !ok {
						err = rpc.NewTypeError((*[]ReleaseLockArg)(nil), args)
						return
					}
					err = i.ReleaseLock(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"startImplicitTeamMigration": {
				MakeArg: func() interface{} {
					ret := make([]StartImplicitTeamMigrationArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]StartImplicitTeamMigrationArg)
					if !ok {
						err = rpc.NewTypeError((*[]StartImplicitTeamMigrationArg)(nil), args)
						return
					}
					err = i.StartImplicitTeamMigration(ctx, (*typedArgs)[0].FolderID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMerkleRoot": {
				MakeArg: func() interface{} {
					ret := make([]GetMerkleRootArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMerkleRootArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMerkleRootArg)(nil), args)
						return
					}
					ret, err = i.GetMerkleRoot(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMerkleRootLatest": {
				MakeArg: func() interface{} {
					ret := make([]GetMerkleRootLatestArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMerkleRootLatestArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMerkleRootLatestArg)(nil), args)
						return
					}
					ret, err = i.GetMerkleRootLatest(ctx, (*typedArgs)[0].TreeID)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMerkleRootSince": {
				MakeArg: func() interface{} {
					ret := make([]GetMerkleRootSinceArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMerkleRootSinceArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMerkleRootSinceArg)(nil), args)
						return
					}
					ret, err = i.GetMerkleRootSince(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"getMerkleNode": {
				MakeArg: func() interface{} {
					ret := make([]GetMerkleNodeArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]GetMerkleNodeArg)
					if !ok {
						err = rpc.NewTypeError((*[]GetMerkleNodeArg)(nil), args)
						return
					}
					ret, err = i.GetMerkleNode(ctx, (*typedArgs)[0].Hash)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"findNextMD": {
				MakeArg: func() interface{} {
					ret := make([]FindNextMDArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]FindNextMDArg)
					if !ok {
						err = rpc.NewTypeError((*[]FindNextMDArg)(nil), args)
						return
					}
					ret, err = i.FindNextMD(ctx, (*typedArgs)[0])
					return
				},
				MethodType: rpc.MethodCall,
			},
			"setImplicitTeamModeForTest": {
				MakeArg: func() interface{} {
					ret := make([]SetImplicitTeamModeForTestArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]SetImplicitTeamModeForTestArg)
					if !ok {
						err = rpc.NewTypeError((*[]SetImplicitTeamModeForTestArg)(nil), args)
						return
					}
					err = i.SetImplicitTeamModeForTest(ctx, (*typedArgs)[0].ImplicitTeamMode)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"forceMerkleBuildForTest": {
				MakeArg: func() interface{} {
					ret := make([]ForceMerkleBuildForTestArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					err = i.ForceMerkleBuildForTest(ctx)
					return
				},
				MethodType: rpc.MethodCall,
			},
		},
	}
}

type MetadataClient struct {
	Cli rpc.GenericClient
}

func (c MetadataClient) GetChallenge(ctx context.Context) (res ChallengeInfo, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getChallenge", []interface{}{GetChallengeArg{}}, &res)
	return
}

func (c MetadataClient) Authenticate(ctx context.Context, signature string) (res int, err error) {
	__arg := AuthenticateArg{Signature: signature}
	err = c.Cli.Call(ctx, "keybase.1.metadata.authenticate", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) PutMetadata(ctx context.Context, __arg PutMetadataArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.putMetadata", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) GetMetadata(ctx context.Context, __arg GetMetadataArg) (res MetadataResponse, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMetadata", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetMetadataByTimestamp(ctx context.Context, __arg GetMetadataByTimestampArg) (res MDBlock, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMetadataByTimestamp", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) RegisterForUpdates(ctx context.Context, __arg RegisterForUpdatesArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.registerForUpdates", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) PruneBranch(ctx context.Context, __arg PruneBranchArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.pruneBranch", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) PutKeys(ctx context.Context, __arg PutKeysArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.putKeys", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) GetKey(ctx context.Context, __arg GetKeyArg) (res []byte, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getKey", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) DeleteKey(ctx context.Context, __arg DeleteKeyArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.deleteKey", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) TruncateLock(ctx context.Context, folderID string) (res bool, err error) {
	__arg := TruncateLockArg{FolderID: folderID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.truncateLock", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) TruncateUnlock(ctx context.Context, folderID string) (res bool, err error) {
	__arg := TruncateUnlockArg{FolderID: folderID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.truncateUnlock", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetFolderHandle(ctx context.Context, __arg GetFolderHandleArg) (res []byte, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getFolderHandle", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetFoldersForRekey(ctx context.Context, deviceKID KID) (err error) {
	__arg := GetFoldersForRekeyArg{DeviceKID: deviceKID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.getFoldersForRekey", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) Ping(ctx context.Context) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.ping", []interface{}{PingArg{}}, nil)
	return
}

func (c MetadataClient) Ping2(ctx context.Context) (res PingResponse, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.ping2", []interface{}{Ping2Arg{}}, &res)
	return
}

func (c MetadataClient) GetLatestFolderHandle(ctx context.Context, folderID string) (res []byte, err error) {
	__arg := GetLatestFolderHandleArg{FolderID: folderID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.getLatestFolderHandle", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetKeyBundles(ctx context.Context, __arg GetKeyBundlesArg) (res KeyBundleResponse, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getKeyBundles", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) Lock(ctx context.Context, __arg LockArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.lock", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) ReleaseLock(ctx context.Context, __arg ReleaseLockArg) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.releaseLock", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) StartImplicitTeamMigration(ctx context.Context, folderID string) (err error) {
	__arg := StartImplicitTeamMigrationArg{FolderID: folderID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.startImplicitTeamMigration", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) GetMerkleRoot(ctx context.Context, __arg GetMerkleRootArg) (res MerkleRoot, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMerkleRoot", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetMerkleRootLatest(ctx context.Context, treeID MerkleTreeID) (res MerkleRoot, err error) {
	__arg := GetMerkleRootLatestArg{TreeID: treeID}
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMerkleRootLatest", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetMerkleRootSince(ctx context.Context, __arg GetMerkleRootSinceArg) (res MerkleRoot, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMerkleRootSince", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) GetMerkleNode(ctx context.Context, hash string) (res []byte, err error) {
	__arg := GetMerkleNodeArg{Hash: hash}
	err = c.Cli.Call(ctx, "keybase.1.metadata.getMerkleNode", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) FindNextMD(ctx context.Context, __arg FindNextMDArg) (res FindNextMDResponse, err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.findNextMD", []interface{}{__arg}, &res)
	return
}

func (c MetadataClient) SetImplicitTeamModeForTest(ctx context.Context, implicitTeamMode string) (err error) {
	__arg := SetImplicitTeamModeForTestArg{ImplicitTeamMode: implicitTeamMode}
	err = c.Cli.Call(ctx, "keybase.1.metadata.setImplicitTeamModeForTest", []interface{}{__arg}, nil)
	return
}

func (c MetadataClient) ForceMerkleBuildForTest(ctx context.Context) (err error) {
	err = c.Cli.Call(ctx, "keybase.1.metadata.forceMerkleBuildForTest", []interface{}{ForceMerkleBuildForTestArg{}}, nil)
	return
}
