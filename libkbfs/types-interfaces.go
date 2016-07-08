// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"reflect"
	"time"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol"
	"github.com/rcrowley/go-metrics"
	"golang.org/x/net/context"
)

// AuthTokenRefreshHandler defines a callback to be called when an auth token refresh
// is needed.
type IFCERFTAuthTokenRefreshHandler interface {
	RefreshAuthToken(context.Context)
}

// Block just needs to be (de)serialized using msgpack
type IFCERFTBlock interface {
	// GetEncodedSize returns the encoded size of this block, but only
	// if it has been previously set; otherwise it returns 0.
	GetEncodedSize() uint32
	// SetEncodedSize sets the encoded size of this block, locally
	// caching it.  The encoded size is not serialized.
	SetEncodedSize(size uint32)
	// DataVersion returns the data version for this block
	DataVersion() IFCERFTDataVer
}

// NodeID is a unique but transient ID for a Node. That is, two Node
// objects in memory at the same time represent the same file or
// directory if and only if their NodeIDs are equal (by pointer).
type IFCERFTNodeID interface {
	// ParentID returns the NodeID of the directory containing the
	// pointed-to file or directory, or nil if none exists.
	ParentID() IFCERFTNodeID
}

// Node represents a direct pointer to a file or directory in KBFS.
// It is somewhat like an inode in a regular file system.  Users of
// KBFS can use Node as a handle when accessing files or directories
// they have previously looked up.
type IFCERFTNode interface {
	// GetID returns the ID of this Node. This should be used as a
	// map key instead of the Node itself.
	GetID() IFCERFTNodeID
	// GetFolderBranch returns the folder ID and branch for this Node.
	GetFolderBranch() IFCERFTFolderBranch
	// GetBasename returns the current basename of the node, or ""
	// if the node has been unlinked.
	GetBasename() string
}

// KBFSOps handles all file system operations.  Expands all indirect
// pointers.  Operations that modify the server data change all the
// block IDs along the path, and so must return a path with the new
// BlockIds so the caller can update their references.
//
// KBFSOps implementations must guarantee goroutine-safety of calls on
// a per-top-level-folder basis.
//
// There are two types of operations that could block:
//   * remote-sync operations, that need to synchronously update the
//     MD for the corresponding top-level folder.  When these
//     operations return successfully, they will have guaranteed to
//     have successfully written the modification to the KBFS servers.
//   * remote-access operations, that don't sync any modifications to KBFS
//     servers, but may block on reading data from the servers.
//
// KBFSOps implementations are supposed to give git-like consistency
// semantics for modification operations; they will be visible to
// other clients immediately after the remote-sync operations succeed,
// if and only if there was no other intervening modification to the
// same folder.  If not, the change will be sync'd to the server in a
// special per-device "unmerged" area before the operation succeeds.
// In this case, the modification will not be visible to other clients
// until the KBFS code on this device performs automatic conflict
// resolution in the background.
//
// All methods take a Context (see https://blog.golang.org/context),
// and if that context is cancelled during the operation, KBFSOps will
// abort any blocking calls and return ctx.Err(). Any notifications
// resulting from an operation will also include this ctx (or a
// Context derived from it), allowing the caller to determine whether
// the notification is a result of their own action or an external
// action.
type IFCERFTKBFSOps interface {
	// GetFavorites returns the logged-in user's list of favorite
	// top-level folders.  This is a remote-access operation.
	GetFavorites(ctx context.Context) ([]IFCERFTFavorite, error)
	// RefreshCachedFavorites tells the instances to forget any cached
	// favorites list and fetch a new list from the server.  The
	// effects are asychronous; if there's an error refreshing the
	// favorites, the cached favorites will become empty.
	RefreshCachedFavorites(ctx context.Context)
	// AddFavorite adds the favorite to both the server and
	// the local cache.
	AddFavorite(ctx context.Context, fav IFCERFTFavorite) error
	// DeleteFavorite deletes the favorite from both the server and
	// the local cache.  Idempotent, so it succeeds even if the folder
	// isn't favorited.
	DeleteFavorite(ctx context.Context, fav IFCERFTFavorite) error

	// GetOrCreateRootNode returns the root node and root entry
	// info associated with the given TLF handle and branch, if
	// the logged-in user has read permissions to the top-level
	// folder. It creates the folder if one doesn't exist yet (and
	// branch == MasterBranch), and the logged-in user has write
	// permissions to the top-level folder.  This is a
	// remote-access operation.
	GetOrCreateRootNode(
		ctx context.Context, h *IFCERFTTlfHandle, branch IFCERFTBranchName) (
		node IFCERFTNode, ei IFCERFTEntryInfo, err error)
	// GetDirChildren returns a map of children in the directory,
	// mapped to their EntryInfo, if the logged-in user has read
	// permission for the top-level folder.  This is a remote-access
	// operation.
	GetDirChildren(ctx context.Context, dir IFCERFTNode) (map[string]IFCERFTEntryInfo, error)
	// Lookup returns the Node and entry info associated with a
	// given name in a directory, if the logged-in user has read
	// permissions to the top-level folder.  The returned Node is nil
	// if the name is a symlink.  This is a remote-access operation.
	Lookup(ctx context.Context, dir IFCERFTNode, name string) (IFCERFTNode, IFCERFTEntryInfo, error)
	// Stat returns the entry info associated with a
	// given Node, if the logged-in user has read permissions to the
	// top-level folder.  This is a remote-access operation.
	Stat(ctx context.Context, node IFCERFTNode) (IFCERFTEntryInfo, error)
	// CreateDir creates a new subdirectory under the given node, if
	// the logged-in user has write permission to the top-level
	// folder.  Returns the new Node for the created subdirectory, and
	// its new entry info.  This is a remote-sync operation.
	CreateDir(ctx context.Context, dir IFCERFTNode, name string) (
		IFCERFTNode, IFCERFTEntryInfo, error)
	// CreateFile creates a new file under the given node, if the
	// logged-in user has write permission to the top-level folder.
	// Returns the new Node for the created file, and its new
	// entry info. excl (when implemented) specifies whether this is an exclusive
	// create.  Semantically setting excl to WithEXCL is like O_CREAT|O_EXCL in a
	// Unix open() call.
	//
	// This is a remote-sync operation.
	CreateFile(ctx context.Context, dir IFCERFTNode, name string, isExec bool, excl IFCERFTEXCL) (
		IFCERFTNode, IFCERFTEntryInfo, error)
	// CreateLink creates a new symlink under the given node, if the
	// logged-in user has write permission to the top-level folder.
	// Returns the new entry info for the created symlink.  This
	// is a remote-sync operation.
	CreateLink(ctx context.Context, dir IFCERFTNode, fromName string, toPath string) (
		IFCERFTEntryInfo, error)
	// RemoveDir removes the subdirectory represented by the given
	// node, if the logged-in user has write permission to the
	// top-level folder.  Will return an error if the subdirectory is
	// not empty.  This is a remote-sync operation.
	RemoveDir(ctx context.Context, dir IFCERFTNode, dirName string) error
	// RemoveEntry removes the directory entry represented by the
	// given node, if the logged-in user has write permission to the
	// top-level folder.  This is a remote-sync operation.
	RemoveEntry(ctx context.Context, dir IFCERFTNode, name string) error
	// Rename performs an atomic rename operation with a given
	// top-level folder if the logged-in user has write permission to
	// that folder, and will return an error if nodes from different
	// folders are passed in.  Also returns an error if the new name
	// already has an entry corresponding to an existing directory
	// (only non-dir types may be renamed over).  This is a
	// remote-sync operation.
	Rename(ctx context.Context, oldParent IFCERFTNode, oldName string, newParent IFCERFTNode, newName string) error
	// Read fills in the given buffer with data from the file at the
	// given node starting at the given offset, if the logged-in user
	// has read permission to the top-level folder.  The read data
	// reflects any outstanding writes and truncates to that file that
	// have been written through this KBFSOps object, even if those
	// writes have not yet been sync'd.  There is no guarantee that
	// Read returns all of the requested data; it will return the
	// number of bytes that it wrote to the dest buffer.  Reads on an
	// unlinked file may or may not succeed, depending on whether or
	// not the data has been cached locally.  If (0, nil) is returned,
	// that means EOF has been reached. This is a remote-access
	// operation.
	Read(ctx context.Context, file IFCERFTNode, dest []byte, off int64) (int64, error)
	// Write modifies the file at the given node, by writing the given
	// buffer at the given offset within the file, if the logged-in
	// user has write permission to the top-level folder.  It
	// overwrites any data already there, and extends the file size as
	// necessary to accomodate the new data.  It guarantees to write
	// the entire buffer in one operation.  Writes on an unlinked file
	// may or may not succeed as no-ops, depending on whether or not
	// the necessary blocks have been locally cached.  This is a
	// remote-access operation.
	Write(ctx context.Context, file IFCERFTNode, data []byte, off int64) error
	// Truncate modifies the file at the given node, by either
	// shrinking or extending its size to match the given size, if the
	// logged-in user has write permission to the top-level folder.
	// If extending the file, it pads the new data with 0s.  Truncates
	// on an unlinked file may or may not succeed as no-ops, depending
	// on whether or not the necessary blocks have been locally
	// cached.  This is a remote-access operation.
	Truncate(ctx context.Context, file IFCERFTNode, size uint64) error
	// SetEx turns on or off the executable bit on the file
	// represented by a given node, if the logged-in user has write
	// permissions to the top-level folder.  This is a remote-sync
	// operation.
	SetEx(ctx context.Context, file IFCERFTNode, ex bool) error
	// SetMtime sets the modification time on the file represented by
	// a given node, if the logged-in user has write permissions to
	// the top-level folder.  If mtime is nil, it is a noop.  This is
	// a remote-sync operation.
	SetMtime(ctx context.Context, file IFCERFTNode, mtime *time.Time) error
	// Sync flushes all outstanding writes and truncates for the given
	// file to the KBFS servers, if the logged-in user has write
	// permissions to the top-level folder.  If done through a file
	// system interface, this may include modifications done via
	// multiple file handles.  This is a remote-sync operation.
	Sync(ctx context.Context, file IFCERFTNode) error
	// FolderStatus returns the status of a particular folder/branch, along
	// with a channel that will be closed when the status has been
	// updated (to eliminate the need for polling this method).
	FolderStatus(ctx context.Context, folderBranch IFCERFTFolderBranch) (
		IFCERFTFolderBranchStatus, <-chan IFCERFTStatusUpdate, error)
	// Status returns the status of KBFS, along with a channel that will be
	// closed when the status has been updated (to eliminate the need for
	// polling this method). KBFSStatus can be non-empty even if there is an
	// error.
	Status(ctx context.Context) (
		KBFSStatus, <-chan IFCERFTStatusUpdate, error)
	// UnstageForTesting clears out this device's staged state, if
	// any, and fast-forwards to the current head of this
	// folder-branch.
	UnstageForTesting(ctx context.Context, folderBranch IFCERFTFolderBranch) error
	// Rekey rekeys this folder.
	Rekey(ctx context.Context, id IFCERFTTlfID) error
	// SyncFromServerForTesting blocks until the local client has
	// contacted the server and guaranteed that all known updates
	// for the given top-level folder have been applied locally
	// (and notifications sent out to any observers).  It returns
	// an error if this folder-branch is currently unmerged or
	// dirty locally.
	SyncFromServerForTesting(ctx context.Context, folderBranch IFCERFTFolderBranch) error
	// GetUpdateHistory returns a complete history of all the merged
	// updates of the given folder, in a data structure that's
	// suitable for encoding directly into JSON.  This is an expensive
	// operation, and should only be used for ocassional debugging.
	// Note that the history does not include any unmerged changes or
	// outstanding writes from the local device.
	GetUpdateHistory(ctx context.Context, folderBranch IFCERFTFolderBranch) (
		history IFCERFTTLFUpdateHistory, err error)
	// Shutdown is called to clean up any resources associated with
	// this KBFSOps instance.
	Shutdown() error
	// PushConnectionStatusChange updates the status of a service for
	// human readable connection status tracking.
	PushConnectionStatusChange(service string, newStatus error)
}

// KeybaseDaemon is an interface for communicating with the local
// Keybase daemon.
type IFCERFTKeybaseDaemon interface {
	// Resolve, given an assertion, resolves it to a username/UID
	// pair. The username <-> UID mapping is trusted and
	// immutable, so it can be cached. If the assertion is just
	// the username or a UID assertion, then the resolution can
	// also be trusted. If the returned pair is equal to that of
	// the current session, then it can also be
	// trusted. Otherwise, Identify() needs to be called on the
	// assertion before the assertion -> (username, UID) mapping
	// can be trusted.
	Resolve(ctx context.Context, assertion string) (
		libkb.NormalizedUsername, keybase1.UID, error)

	// Identify, given an assertion, returns a UserInfo struct
	// with the user that matches that assertion, or an error
	// otherwise. The reason string is displayed on any tracker
	// popups spawned.
	Identify(ctx context.Context, assertion, reason string) (IFCERFTUserInfo, error)

	// LoadUserPlusKeys returns a UserInfo struct for a
	// user with the specified UID.
	// If you have the UID for a user and don't require Identify to
	// validate an assertion or the identity of a user, use this to
	// get UserInfo structs as it is much cheaper than Identify.
	LoadUserPlusKeys(ctx context.Context, uid keybase1.UID) (IFCERFTUserInfo, error)

	// LoadUnverifiedKeys returns a list of unverified public keys.  They are the union
	// of all known public keys associated with the account and the currently verified
	// keys currently part of the user's sigchain.
	LoadUnverifiedKeys(ctx context.Context, uid keybase1.UID) (
		[]IFCERFTVerifyingKey, []IFCERFTCryptPublicKey, error)

	// CurrentSession returns a SessionInfo struct with all the
	// information for the current session, or an error otherwise.
	CurrentSession(ctx context.Context, sessionID int) (IFCERFTSessionInfo, error)

	// FavoriteAdd adds the given folder to the list of favorites.
	FavoriteAdd(ctx context.Context, folder keybase1.Folder) error

	// FavoriteAdd removes the given folder from the list of
	// favorites.
	FavoriteDelete(ctx context.Context, folder keybase1.Folder) error

	// FavoriteList returns the current list of favorites.
	FavoriteList(ctx context.Context, sessionID int) ([]keybase1.Folder, error)

	// Notify sends a filesystem notification.
	Notify(ctx context.Context, notification *keybase1.FSNotification) error

	// FlushUserFromLocalCache instructs this layer to clear any
	// KBFS-side, locally-cached information about the given user.
	// This does NOT involve communication with the daemon, this is
	// just to force future calls loading this user to fall through to
	// the daemon itself, rather than being served from the cache.
	FlushUserFromLocalCache(ctx context.Context, uid keybase1.UID)

	// FlushUserUnverifiedKeysFromLocalCache instructs this layer to clear any
	// KBFS-side, locally-cached unverified keys for the given user.
	FlushUserUnverifiedKeysFromLocalCache(ctx context.Context, uid keybase1.UID)

	// TODO: Add CryptoClient methods, too.

	// Shutdown frees any resources associated with this
	// instance. No other methods may be called after this is
	// called.
	Shutdown()
}

// KBPKI interacts with the Keybase daemon to fetch user info.
type IFCERFTKBPKI interface {
	// GetCurrentToken gets the current keybase session token.
	GetCurrentToken(ctx context.Context) (string, error)
	// GetCurrentUserInfo gets the name and UID of the current
	// logged-in user.
	GetCurrentUserInfo(ctx context.Context) (
		libkb.NormalizedUsername, keybase1.UID, error)
	// GetCurrentCryptPublicKey gets the crypt public key for the
	// currently-active device.
	GetCurrentCryptPublicKey(ctx context.Context) (IFCERFTCryptPublicKey, error)
	// GetCurrentVerifyingKey gets the public key used for signing for the
	// currently-active device.
	GetCurrentVerifyingKey(ctx context.Context) (IFCERFTVerifyingKey, error)

	resolver
	identifier
	normalizedUsernameGetter

	// HasVerifyingKey returns nil if the given user has the given
	// VerifyingKey, and an error otherwise.
	HasVerifyingKey(ctx context.Context, uid keybase1.UID,
		verifyingKey IFCERFTVerifyingKey, atServerTime time.Time) error

	// HasUnverifiedVerifyingKey returns nil if the given user has the given
	// unverified VerifyingKey, and an error otherwise.  Note that any match
	// is with a key not verified to be currently connected to the user via
	// their sigchain.  This is currently only used to verify finalized or
	// reset TLFs.  Further note that unverified keys is a super set of
	// verified keys.
	HasUnverifiedVerifyingKey(ctx context.Context, uid keybase1.UID,
		verifyingKey IFCERFTVerifyingKey) error

	// GetCryptPublicKeys gets all of a user's crypt public keys (including
	// paper keys).
	GetCryptPublicKeys(ctx context.Context, uid keybase1.UID) (
		[]IFCERFTCryptPublicKey, error)

	// TODO: Split the methods below off into a separate
	// FavoriteOps interface.

	// FavoriteAdd adds folder to the list of the logged in user's
	// favorite folders.  It is idempotent.
	FavoriteAdd(ctx context.Context, folder keybase1.Folder) error

	// FavoriteDelete deletes folder from the list of the logged in user's
	// favorite folders.  It is idempotent.
	FavoriteDelete(ctx context.Context, folder keybase1.Folder) error

	// FavoriteList returns the list of all favorite folders for
	// the logged in user.
	FavoriteList(ctx context.Context) ([]keybase1.Folder, error)

	// Notify sends a filesystem notification.
	Notify(ctx context.Context, notification *keybase1.FSNotification) error
}

// KeyManager fetches and constructs the keys needed for KBFS file
// operations.
type IFCERFTKeyManager interface {
	// GetTLFCryptKeyForEncryption gets the crypt key to use for
	// encryption (i.e., with the latest key generation) for the
	// TLF with the given metadata.
	GetTLFCryptKeyForEncryption(ctx context.Context, md *IFCERFTRootMetadata) (
		IFCERFTTLFCryptKey, error)

	// GetTLFCryptKeyForMDDecryption gets the crypt key to use for the
	// TLF with the given metadata to decrypt the private portion of
	// the metadata.  It finds the appropriate key from mdWithKeys
	// (which in most cases is the same as mdToDecrypt) if it's not
	// already cached.
	GetTLFCryptKeyForMDDecryption(ctx context.Context,
		mdToDecrypt, mdWithKeys *IFCERFTRootMetadata) (IFCERFTTLFCryptKey, error)

	// GetTLFCryptKeyForBlockDecryption gets the crypt key to use
	// for the TLF with the given metadata to decrypt the block
	// pointed to by the given pointer.
	GetTLFCryptKeyForBlockDecryption(ctx context.Context, md *IFCERFTRootMetadata, blockPtr IFCERFTBlockPointer) (IFCERFTTLFCryptKey, error)

	// Rekey checks the given MD object, if it is a private TLF,
	// against the current set of device keys for all valid
	// readers and writers.  If there are any new devices, it
	// updates all existing key generations to include the new
	// devices.  If there are devices that have been removed, it
	// creates a new epoch of keys for the TLF.  If no devices
	// have changed, or if there was an error, it returns false.
	// Otherwise, it returns true. If a new key generation is
	// added the second return value points to this new key. This
	// is to allow for caching of the TLF crypt key only after a
	// successful merged write of the metadata. Otherwise we could
	// prematurely pollute the key cache.
	//
	// If the given MD object is a public TLF, it simply updates
	// the TLF's handle with any newly-resolved writers.
	//
	// If promptPaper is set, prompts for any unlocked paper keys.
	// promptPaper shouldn't be set if md is for a public TLF.
	Rekey(ctx context.Context, md *IFCERFTRootMetadata, promptPaper bool) (bool, *IFCERFTTLFCryptKey, error)
}

// Reporter exports events (asynchronously) to any number of sinks
type IFCERFTReporter interface {
	// ReportErr records that a given error happened.
	ReportErr(ctx context.Context, tlfName IFCERFTCanonicalTlfName, public bool,
		mode IFCERFTErrorModeType, err error)
	// AllKnownErrors returns all errors known to this Reporter.
	AllKnownErrors() []ReportedError
	// Notify sends the given notification to any sink.
	Notify(ctx context.Context, notification *keybase1.FSNotification)
	// Shutdown frees any resources allocated by a Reporter.
	Shutdown()
}

// MDCache gets and puts plaintext top-level metadata into the cache.
type IFCERFTMDCache interface {
	// Get gets the metadata object associated with the given TlfID,
	// revision number, and branch ID (NullBranchID for merged MD).
	Get(tlf IFCERFTTlfID, rev MetadataRevision, bid BranchID) (*IFCERFTRootMetadata, error)
	// Put stores the metadata object.
	Put(md *IFCERFTRootMetadata) error
}

// KeyCache handles caching for both TLFCryptKeys and BlockCryptKeys.
type IFCERFTKeyCache interface {
	// GetTLFCryptKey gets the crypt key for the given TLF.
	GetTLFCryptKey(IFCERFTTlfID, IFCERFTKeyGen) (IFCERFTTLFCryptKey, error)
	// PutTLFCryptKey stores the crypt key for the given TLF.
	PutTLFCryptKey(IFCERFTTlfID, IFCERFTKeyGen, IFCERFTTLFCryptKey) error
}

// BlockCacheLifetime denotes the lifetime of an entry in BlockCache.
type IFCERFTBlockCacheLifetime int

const (
	// TransientEntry means that the cache entry may be evicted at
	// any time.
	IFCERFTTransientEntry IFCERFTBlockCacheLifetime = iota
	// PermanentEntry means that the cache entry must remain until
	// explicitly removed from the cache.
	IFCERFTPermanentEntry
)

// BlockCache gets and puts plaintext dir blocks and file blocks into
// a cache.  These blocks are immutable and identified by their
// content hash.
type IFCERFTBlockCache interface {
	// Get gets the block associated with the given block ID.
	Get(ptr IFCERFTBlockPointer) (IFCERFTBlock, error)
	// CheckForKnownPtr sees whether this cache has a transient
	// entry for the given file block, which must be a direct file
	// block containing data).  Returns the full BlockPointer
	// associated with that ID, including key and data versions.
	// If no ID is known, return an uninitialized BlockPointer and
	// a nil error.
	CheckForKnownPtr(tlf IFCERFTTlfID, block *FileBlock) (IFCERFTBlockPointer, error)
	// Put stores the final (content-addressable) block associated
	// with the given block ID. If lifetime is TransientEntry,
	// then it is assumed that the block exists on the server and
	// the entry may be evicted from the cache at any time. If
	// lifetime is PermanentEntry, then it is assumed that the
	// block doesn't exist on the server and must remain in the
	// cache until explicitly removed. As an intermediary state,
	// as when a block is being sent to the server, the block may
	// be put into the cache both with TransientEntry and
	// PermanentEntry -- these are two separate entries. This is
	// fine, since the block should be the same.
	Put(ptr IFCERFTBlockPointer, tlf IFCERFTTlfID, block IFCERFTBlock, lifetime IFCERFTBlockCacheLifetime) error
	// DeleteTransient removes the transient entry for the given
	// pointer from the cache, as well as any cached IDs so the block
	// won't be reused.
	DeleteTransient(ptr IFCERFTBlockPointer, tlf IFCERFTTlfID) error
	// Delete removes the permanent entry for the non-dirty block
	// associated with the given block ID from the cache.  No
	// error is returned if no block exists for the given ID.
	DeletePermanent(id BlockID) error
	// DeleteKnownPtr removes the cached ID for the given file
	// block. It does not remove the block itself.
	DeleteKnownPtr(tlf IFCERFTTlfID, block *FileBlock) error
}

// DirtyPermChan is a channel that gets closed when the holder has
// permission to write.  We are forced to define it as a type due to a
// bug in mockgen that can't handle return values with a chan
// struct{}.
type IFCERFTDirtyPermChan <-chan struct{}

// DirtyBlockCache gets and puts plaintext dir blocks and file blocks
// into a cache, which have been modified by the application and not
// yet committed on the KBFS servers.  They are identified by a
// (potentially random) ID that may not have any relationship with
// their context, along with a Branch in case the same TLF is being
// modified via multiple branches.  Dirty blocks are never evicted,
// they must be deleted explicitly.
type IFCERFTDirtyBlockCache interface {
	// Get gets the block associated with the given block ID.  Returns
	// the dirty block for the given ID, if one exists.
	Get(ptr IFCERFTBlockPointer, branch IFCERFTBranchName) (IFCERFTBlock, error)
	// Put stores a dirty block currently identified by the
	// given block pointer and branch name.
	Put(ptr IFCERFTBlockPointer, branch IFCERFTBranchName, block IFCERFTBlock) error
	// Delete removes the dirty block associated with the given block
	// pointer and branch from the cache.  No error is returned if no
	// block exists for the given ID.
	Delete(ptr IFCERFTBlockPointer, branch IFCERFTBranchName) error
	// IsDirty states whether or not the block associated with the
	// given block pointer and branch name is dirty in this cache.
	IsDirty(ptr IFCERFTBlockPointer, branch IFCERFTBranchName) bool
	// RequestPermissionToDirty is called whenever a user wants to
	// write data to a file.  The caller provides an estimated number
	// of bytes that will become dirty -- this is difficult to know
	// exactly without pre-fetching all the blocks involved, but in
	// practice we can just use the number of bytes sent in via the
	// Write. It returns a channel that blocks until the cache is
	// ready to receive more dirty data, at which point the channel is
	// closed.  The user must call
	// `UpdateUnsyncedBytes(-estimatedDirtyBytes)` once it has
	// completed its write and called `UpdateUnsyncedBytes` for all
	// the exact dirty block sizes.
	RequestPermissionToDirty(ctx context.Context,
		estimatedDirtyBytes int64) (IFCERFTDirtyPermChan, error)
	// UpdateUnsyncedBytes is called by a user, who has already been
	// granted permission to write, with the delta in block sizes that
	// were dirtied as part of the write.  So for example, if a
	// newly-dirtied block of 20 bytes was extended by 5 bytes, they
	// should send 25.  If on the next write (before any syncs), bytes
	// 10-15 of that same block were overwritten, they should send 0
	// over the channel because there were no new bytes.  If an
	// already-dirtied block is truncated, or if previously requested
	// bytes have now been updated more accurately in previous
	// requests, newUnsyncedBytes may be negative.  wasSyncing should
	// be true if `BlockSyncStarted` has already been called for this
	// block.
	UpdateUnsyncedBytes(newUnsyncedBytes int64, wasSyncing bool)
	// UpdateSyncingBytes is called when a particular block has
	// started syncing, or with a negative number when a block is no
	// longer syncing due to an error (and BlockSyncFinished will
	// never be called).
	UpdateSyncingBytes(size int64)
	// BlockSyncFinished is called when a particular block has
	// finished syncing, though the overall sync might not yet be
	// complete.  This lets the cache know it might be able to grant
	// more permission to writers.
	BlockSyncFinished(size int64)
	// SyncFinished is called when a complete sync has completed and
	// its dirty blocks have been removed from the cache.  This lets
	// the cache know it might be able to grant more permission to
	// writers.
	SyncFinished(size int64)
	// ShouldForceSync returns true if the sync buffer is full enough
	// to force all callers to sync their data immediately.
	ShouldForceSync() bool

	// Shutdown frees any resources associated with this instance.  It
	// returns an error if there are any unsynced blocks.
	Shutdown() error
}

// Crypto signs, verifies, encrypts, and decrypts stuff.
type IFCERFTCrypto interface {
	cryptoPure

	// Sign signs the msg with the current device's private key.
	Sign(ctx context.Context, msg []byte) (sigInfo IFCERFTSignatureInfo, err error)
	// Sign signs the msg with the current device's private key and output
	// the full serialized NaclSigInfo.
	SignToString(ctx context.Context, msg []byte) (signature string, err error)
	// DecryptTLFCryptKeyClientHalf decrypts a TLFCryptKeyClientHalf
	// using the current device's private key and the TLF's ephemeral
	// public key.
	DecryptTLFCryptKeyClientHalf(ctx context.Context,
		publicKey IFCERFTTLFEphemeralPublicKey, encryptedClientHalf IFCERFTEncryptedTLFCryptKeyClientHalf) (
		TLFCryptKeyClientHalf, error)

	// DecryptTLFCryptKeyClientHalfAny decrypts one of the
	// TLFCryptKeyClientHalf using the available private keys and the
	// ephemeral public key.  If promptPaper is true, the service will
	// prompt the user for any unlocked paper keys.
	DecryptTLFCryptKeyClientHalfAny(ctx context.Context,
		keys []IFCERFTEncryptedTLFCryptKeyClientAndEphemeral, promptPaper bool) (
		TLFCryptKeyClientHalf, int, error)

	// Shutdown frees any resources associated with this instance.
	Shutdown()
}

// Codec encodes and decodes arbitrary data
type IFCERFTCodec interface {
	// Decode unmarshals the given buffer into the given object, if possible.
	Decode(buf []byte, obj interface{}) error
	// Encode marshals the given object into a returned buffer.
	Encode(obj interface{}) ([]byte, error)
	// RegisterType should be called for all types that are stored
	// under ambiguous types (like interface{} or nil interface) in a
	// struct that will be encoded/decoded by the codec.  Each must
	// have a unique extCode.  Types that include other extension
	// types are not supported.
	RegisterType(rt reflect.Type, code extCode)
	// RegisterIfaceSliceType should be called for all encoded slices
	// that contain ambiguous interface types.  Each must have a
	// unique extCode.  Slice element types that include other
	// extension types are not supported.
	//
	// If non-nil, typer is used to do a type assertion during
	// decoding, to convert the encoded value into the value expected
	// by the rest of the code.  This is needed, for example, when the
	// codec cannot decode interface types to their desired pointer
	// form.
	RegisterIfaceSliceType(rt reflect.Type, code extCode,
		typer func(interface{}) reflect.Value)
}

// MDOps gets and puts root metadata to an MDServer.  On a get, it
// verifies the metadata is signed by the metadata's signing key.
type IFCERFTMDOps interface {
	// GetForHandle returns the current metadata
	// object corresponding to the given top-level folder's handle, if
	// the logged-in user has read permission on the folder.  It
	// creates the folder if one doesn't exist yet, and the logged-in
	// user has permission to do so.
	GetForHandle(ctx context.Context, handle *IFCERFTTlfHandle) (
		*IFCERFTRootMetadata, error)

	// GetUnmergedForHandle is the same as the above but for unmerged
	// metadata history.
	GetUnmergedForHandle(ctx context.Context, handle *IFCERFTTlfHandle) (
		*IFCERFTRootMetadata, error)

	// GetForTLF returns the current metadata object
	// corresponding to the given top-level folder, if the logged-in
	// user has read permission on the folder.
	GetForTLF(ctx context.Context, id IFCERFTTlfID) (*IFCERFTRootMetadata, error)

	// GetUnmergedForTLF is the same as the above but for unmerged
	// metadata.
	GetUnmergedForTLF(ctx context.Context, id IFCERFTTlfID, bid BranchID) (
		*IFCERFTRootMetadata, error)

	// GetRange returns a range of metadata objects corresponding to
	// the passed revision numbers (inclusive).
	GetRange(ctx context.Context, id IFCERFTTlfID, start, stop MetadataRevision) (
		[]*IFCERFTRootMetadata, error)

	// GetUnmergedRange is the same as the above but for unmerged
	// metadata history (inclusive).
	GetUnmergedRange(ctx context.Context, id IFCERFTTlfID, bid BranchID,
		start, stop MetadataRevision) ([]*IFCERFTRootMetadata, error)

	// Put stores the metadata object for the given
	// top-level folder.
	Put(ctx context.Context, rmd *IFCERFTRootMetadata) error

	// PutUnmerged is the same as the above but for unmerged
	// metadata history.
	PutUnmerged(ctx context.Context, rmd *IFCERFTRootMetadata, bid BranchID) error

	// GetLatestHandleForTLF returns the server's idea of the latest handle for the TLF,
	// which may not yet be reflected in the MD if the TLF hasn't been rekeyed since it
	// entered into a conflicting state.
	GetLatestHandleForTLF(ctx context.Context, id IFCERFTTlfID) (
		BareTlfHandle, error)
}

// KeyOps fetches server-side key halves from the key server.
type IFCERFTKeyOps interface {
	// GetTLFCryptKeyServerHalf gets a server-side key half for a
	// device given the key half ID.
	GetTLFCryptKeyServerHalf(ctx context.Context,
		serverHalfID TLFCryptKeyServerHalfID,
		cryptPublicKey IFCERFTCryptPublicKey) (TLFCryptKeyServerHalf, error)

	// PutTLFCryptKeyServerHalves stores a server-side key halves for a
	// set of users and devices.
	PutTLFCryptKeyServerHalves(ctx context.Context,
		serverKeyHalves map[keybase1.UID]map[keybase1.KID]TLFCryptKeyServerHalf) error

	// DeleteTLFCryptKeyServerHalf deletes a server-side key half for a
	// device given the key half ID.
	DeleteTLFCryptKeyServerHalf(ctx context.Context,
		uid keybase1.UID, kid keybase1.KID,
		serverHalfID TLFCryptKeyServerHalfID) error
}

// BlockOps gets and puts data blocks to a BlockServer. It performs
// the necessary crypto operations on each block.
type IFCERFTBlockOps interface {
	// Get gets the block associated with the given block pointer
	// (which belongs to the TLF with the given metadata),
	// decrypts it if necessary, and fills in the provided block
	// object with its contents, if the logged-in user has read
	// permission for that block.
	Get(ctx context.Context, md *IFCERFTRootMetadata, blockPtr IFCERFTBlockPointer, block IFCERFTBlock) error

	// Ready turns the given block (which belongs to the TLF with
	// the given metadata) into encoded (and encrypted) data, and
	// calculates its ID and size, so that we can do a bunch of
	// block puts in parallel for every write. Ready() must
	// guarantee that plainSize <= readyBlockData.QuotaSize().
	Ready(ctx context.Context, md *IFCERFTRootMetadata, block IFCERFTBlock) (
		id BlockID, plainSize int, readyBlockData IFCERFTReadyBlockData, err error)

	// Put stores the readied block data under the given block
	// pointer (which belongs to the TLF with the given metadata)
	// on the server.
	Put(ctx context.Context, md *IFCERFTRootMetadata, blockPtr IFCERFTBlockPointer, readyBlockData IFCERFTReadyBlockData) error

	// Delete instructs the server to delete the given block references.
	// It returns the number of not-yet deleted references to
	// each block reference
	Delete(ctx context.Context, md *IFCERFTRootMetadata, ptrs []IFCERFTBlockPointer) (
		liveCounts map[BlockID]int, err error)

	// Archive instructs the server to mark the given block references
	// as "archived"; that is, they are not being used in the current
	// view of the folder, and shouldn't be served to anyone other
	// than folder writers.
	Archive(ctx context.Context, md *IFCERFTRootMetadata, ptrs []IFCERFTBlockPointer) error
}

// MDServer gets and puts metadata for each top-level directory.  The
// instantiation should be able to fetch session/user details via KBPKI.  On a
// put, the server is responsible for 1) ensuring the user has appropriate
// permissions for whatever modifications were made; 2) ensuring that
// LastModifyingWriter and LastModifyingUser are updated appropriately; and 3)
// detecting conflicting writes based on the previous root block ID (i.e., when
// it supports strict consistency).  On a get, it verifies the logged-in user
// has read permissions.
//
// TODO: Add interface for searching by time
type IFCERFTMDServer interface {
	IFCERFTAuthTokenRefreshHandler

	// GetForHandle returns the current (signed/encrypted) metadata
	// object corresponding to the given top-level folder's handle, if
	// the logged-in user has read permission on the folder.  It
	// creates the folder if one doesn't exist yet, and the logged-in
	// user has permission to do so.
	GetForHandle(ctx context.Context, handle BareTlfHandle,
		mStatus IFCERFTMergeStatus) (IFCERFTTlfID, *RootMetadataSigned, error)

	// GetForTLF returns the current (signed/encrypted) metadata object
	// corresponding to the given top-level folder, if the logged-in
	// user has read permission on the folder.
	GetForTLF(ctx context.Context, id IFCERFTTlfID, bid BranchID, mStatus IFCERFTMergeStatus) (
		*RootMetadataSigned, error)

	// GetRange returns a range of (signed/encrypted) metadata objects
	// corresponding to the passed revision numbers (inclusive).
	GetRange(ctx context.Context, id IFCERFTTlfID, bid BranchID, mStatus IFCERFTMergeStatus, start, stop MetadataRevision) ([]*RootMetadataSigned, error)

	// Put stores the (signed/encrypted) metadata object for the given
	// top-level folder. Note: If the unmerged bit is set in the metadata
	// block's flags bitmask it will be appended to the unmerged per-device
	// history.
	Put(ctx context.Context, rmds *RootMetadataSigned) error

	// PruneBranch prunes all unmerged history for the given TLF branch.
	PruneBranch(ctx context.Context, id IFCERFTTlfID, bid BranchID) error

	// RegisterForUpdate tells the MD server to inform the caller when
	// there is a merged update with a revision number greater than
	// currHead, which did NOT originate from this same MD server
	// session.  This method returns a chan which can receive only a
	// single error before it's closed.  If the received err is nil,
	// then there is updated MD ready to fetch which didn't originate
	// locally; if it is non-nil, then the previous registration
	// cannot send the next notification (e.g., the connection to the
	// MD server may have failed). In either case, the caller must
	// re-register to get a new chan that can receive future update
	// notifications.
	RegisterForUpdate(ctx context.Context, id IFCERFTTlfID, currHead MetadataRevision) (<-chan error, error)

	// CheckForRekeys initiates the rekey checking process on the
	// server.  The server is allowed to delay this request, and so it
	// returns a channel for returning the error. Actual rekey
	// requests are expected to come in asynchronously.
	CheckForRekeys(ctx context.Context) <-chan error

	// TruncateLock attempts to take the history truncation lock for
	// this folder, for a TTL defined by the server.  Returns true if
	// the lock was successfully taken.
	TruncateLock(ctx context.Context, id IFCERFTTlfID) (bool, error)
	// TruncateUnlock attempts to release the history truncation lock
	// for this folder.  Returns true if the lock was successfully
	// released.
	TruncateUnlock(ctx context.Context, id IFCERFTTlfID) (bool, error)

	// DisableRekeyUpdatesForTesting disables processing rekey updates
	// received from the mdserver while testing.
	DisableRekeyUpdatesForTesting()

	// Shutdown is called to shutdown an MDServer connection.
	Shutdown()

	// IsConnected returns whether the MDServer is connected.
	IsConnected() bool

	// GetLatestHandleForTLF returns the server's idea of the latest handle for the TLF,
	// which may not yet be reflected in the MD if the TLF hasn't been rekeyed since it
	// entered into a conflicting state.  For the highest level of confidence, the caller
	// should verify the mapping with a Merkle tree lookup.
	GetLatestHandleForTLF(ctx context.Context, id IFCERFTTlfID) (
		BareTlfHandle, error)
}

// BlockServer gets and puts opaque data blocks.  The instantiation
// should be able to fetch session/user details via KBPKI.  On a
// put/delete, the server is reponsible for: 1) checking that the ID
// matches the hash of the buffer; and 2) enforcing writer quotas.
type IFCERFTBlockServer interface {
	IFCERFTAuthTokenRefreshHandler

	// Get gets the (encrypted) block data associated with the given
	// block ID and context, uses the provided block key to decrypt
	// the block, and fills in the provided block object with its
	// contents, if the logged-in user has read permission for that
	// block.
	Get(ctx context.Context, id BlockID, tlfID IFCERFTTlfID, context IFCERFTBlockContext) (
		[]byte, IFCERFTBlockCryptKeyServerHalf, error)
	// Put stores the (encrypted) block data under the given ID and
	// context on the server, along with the server half of the block
	// key.  context should contain a BlockRefNonce of zero.  There
	// will be an initial reference for this block for the given
	// context.
	//
	// Put should be idempotent, although it should also return an
	// error if, for a given ID, any of the other arguments differ
	// from previous Put calls with the same ID.
	//
	// If this returns a BServerErrorOverQuota, with Throttled=false,
	// the caller can treat it as informational and otherwise ignore
	// the error.
	Put(ctx context.Context, id BlockID, tlfID IFCERFTTlfID, context IFCERFTBlockContext, buf []byte, serverHalf IFCERFTBlockCryptKeyServerHalf) error

	// AddBlockReference adds a new reference to the given block,
	// defined by the given context (which should contain a non-zero
	// BlockRefNonce).  (Contexts with a BlockRefNonce of zero should
	// be used when putting the block for the first time via Put().)
	// Returns a BServerErrorBlockNonExistent if id is unknown within
	// this folder.
	//
	// AddBlockReference should be idempotent, although it should
	// also return an error if, for a given ID and refnonce, any
	// of the other fields of context differ from previous
	// AddBlockReference calls with the same ID and refnonce.
	//
	// If this returns a BServerErrorOverQuota, with Throttled=false,
	// the caller can treat it as informational and otherwise ignore
	// the error.
	AddBlockReference(ctx context.Context, id BlockID, tlfID IFCERFTTlfID, context IFCERFTBlockContext) error
	// RemoveBlockReference removes the reference to the given block
	// ID defined by the given context.  If no references to the block
	// remain after this call, the server is allowed to delete the
	// corresponding block permanently.  If the reference defined by
	// the count has already been removed, the call is a no-op.
	// It returns the number of remaining not-yet-deleted references after this
	// reference has been removed
	RemoveBlockReference(ctx context.Context, tlfID IFCERFTTlfID, contexts map[BlockID][]IFCERFTBlockContext) (liveCounts map[BlockID]int, err error)

	// ArchiveBlockReferences marks the given block references as
	// "archived"; that is, they are not being used in the current
	// view of the folder, and shouldn't be served to anyone other
	// than folder writers.
	//
	// For a given ID/refnonce pair, ArchiveBlockReferences should
	// be idempotent, although it should also return an error if
	// any of the other fields of the context differ from previous
	// calls with the same ID/refnonce pair.
	ArchiveBlockReferences(ctx context.Context, tlfID IFCERFTTlfID, contexts map[BlockID][]IFCERFTBlockContext) error

	// Shutdown is called to shutdown a BlockServer connection.
	Shutdown()

	// GetUserQuotaInfo returns the quota for the user.
	GetUserQuotaInfo(ctx context.Context) (info *IFCERFTUserQuotaInfo, err error)
}

// BlockSplitter decides when a file or directory block needs to be split
type IFCERFTBlockSplitter interface {
	// CopyUntilSplit copies data into the block until we reach the
	// point where we should split, but only if writing to the end of
	// the last block.  If this is writing into the middle of a file,
	// just copy everything that will fit into the block, and assume
	// that block boundaries will be fixed later. Return how much was
	// copied.
	CopyUntilSplit(
		block *FileBlock, lastBlock bool, data []byte, off int64) int64

	// CheckSplit, given a block, figures out whether it ends at the
	// right place.  If so, return 0.  If not, return either the
	// offset in the block where it should be split, or -1 if more
	// bytes from the next block should be appended.
	CheckSplit(block *FileBlock) int64

	// ShouldEmbedBlockChanges decides whether we should keep the
	// block changes embedded in the MD or not.
	ShouldEmbedBlockChanges(bc *IFCERFTBlockChanges) bool
}

// KeyServer fetches/writes server-side key halves from/to the key server.
type IFCERFTKeyServer interface {
	// GetTLFCryptKeyServerHalf gets a server-side key half for a
	// device given the key half ID.
	GetTLFCryptKeyServerHalf(ctx context.Context,
		serverHalfID TLFCryptKeyServerHalfID,
		cryptPublicKey IFCERFTCryptPublicKey) (TLFCryptKeyServerHalf, error)

	// PutTLFCryptKeyServerHalves stores a server-side key halves for a
	// set of users and devices.
	PutTLFCryptKeyServerHalves(ctx context.Context,
		serverKeyHalves map[keybase1.UID]map[keybase1.KID]TLFCryptKeyServerHalf) error

	// DeleteTLFCryptKeyServerHalf deletes a server-side key half for a
	// device given the key half ID.
	DeleteTLFCryptKeyServerHalf(ctx context.Context,
		uid keybase1.UID, kid keybase1.KID,
		serverHalfID TLFCryptKeyServerHalfID) error

	// Shutdown is called to free any KeyServer resources.
	Shutdown()
}

// NodeChange represents a change made to a node as part of an atomic
// file system operation.
type IFCERFTNodeChange struct {
	Node IFCERFTNode
	// Basenames of entries added/removed.
	DirUpdated  []string
	FileUpdated []WriteRange
}

// Observer can be notified that there is an available update for a
// given directory.  The notification callbacks should not block, or
// make any calls to the Notifier interface.  Nodes passed to the
// observer should not be held past the end of the notification
// callback.
type IFCERFTObserver interface {
	// LocalChange announces that the file at this Node has been
	// updated locally, but not yet saved at the server.
	LocalChange(ctx context.Context, node IFCERFTNode, write WriteRange)
	// BatchChanges announces that the nodes have all been updated
	// together atomically.  Each NodeChange in changes affects the
	// same top-level folder and branch.
	BatchChanges(ctx context.Context, changes []IFCERFTNodeChange)
	// TlfHandleChange announces that the handle of the corresponding
	// folder branch has changed, likely due to previously-unresolved
	// assertions becoming resolved.  This indicates that the listener
	// should switch over any cached paths for this folder-branch to
	// the new name.  Nodes that were acquired under the old name will
	// still continue to work, but new lookups on the old name may
	// either encounter alias errors or entirely new TLFs (in the case
	// of conflicts).
	TlfHandleChange(ctx context.Context, newHandle *IFCERFTTlfHandle)
}

// Notifier notifies registrants of directory changes
type IFCERFTNotifier interface {
	// RegisterForChanges declares that the given Observer wants to
	// subscribe to updates for the given top-level folders.
	RegisterForChanges(folderBranches []IFCERFTFolderBranch, obs IFCERFTObserver) error
	// UnregisterFromChanges declares that the given Observer no
	// longer wants to subscribe to updates for the given top-level
	// folders.
	UnregisterFromChanges(folderBranches []IFCERFTFolderBranch, obs IFCERFTObserver) error
}

// Clock is an interface for getting the current time
type IFCERFTClock interface {
	// Now returns the current time.
	Now() time.Time
}

// ConflictRenamer deals with names for conflicting directory entries.
type IFCERFTConflictRenamer interface {
	// ConflictRename returns the appropriately modified filename.
	ConflictRename(op op, original string) string
}

// Config collects all the singleton instance instantiations needed to
// run KBFS in one place.  The methods below are self-explanatory and
// do not require comments.
type IFCERFTConfig interface {
	KBFSOps() IFCERFTKBFSOps
	SetKBFSOps(IFCERFTKBFSOps)
	KBPKI() IFCERFTKBPKI
	SetKBPKI(IFCERFTKBPKI)
	KeyManager() IFCERFTKeyManager
	SetKeyManager(IFCERFTKeyManager)
	Reporter() IFCERFTReporter
	SetReporter(IFCERFTReporter)
	MDCache() IFCERFTMDCache
	SetMDCache(IFCERFTMDCache)
	KeyCache() IFCERFTKeyCache
	SetKeyCache(IFCERFTKeyCache)
	BlockCache() IFCERFTBlockCache
	SetBlockCache(IFCERFTBlockCache)
	DirtyBlockCache() IFCERFTDirtyBlockCache
	SetDirtyBlockCache(IFCERFTDirtyBlockCache)
	Crypto() IFCERFTCrypto
	SetCrypto(IFCERFTCrypto)
	Codec() IFCERFTCodec
	SetCodec(IFCERFTCodec)
	MDOps() IFCERFTMDOps
	SetMDOps(IFCERFTMDOps)
	KeyOps() IFCERFTKeyOps
	SetKeyOps(IFCERFTKeyOps)
	BlockOps() IFCERFTBlockOps
	SetBlockOps(IFCERFTBlockOps)
	MDServer() IFCERFTMDServer
	SetMDServer(IFCERFTMDServer)
	BlockServer() IFCERFTBlockServer
	SetBlockServer(IFCERFTBlockServer)
	KeyServer() IFCERFTKeyServer
	SetKeyServer(IFCERFTKeyServer)
	KeybaseDaemon() IFCERFTKeybaseDaemon
	SetKeybaseDaemon(IFCERFTKeybaseDaemon)
	BlockSplitter() IFCERFTBlockSplitter
	SetBlockSplitter(IFCERFTBlockSplitter)
	Notifier() IFCERFTNotifier
	SetNotifier(IFCERFTNotifier)
	Clock() IFCERFTClock
	SetClock(IFCERFTClock)
	ConflictRenamer() IFCERFTConflictRenamer
	SetConflictRenamer(IFCERFTConflictRenamer)
	MetadataVersion() IFCERFTMetadataVer
	DataVersion() IFCERFTDataVer
	RekeyQueue() IFCERFTRekeyQueue
	SetRekeyQueue(IFCERFTRekeyQueue)
	// ReqsBufSize indicates the number of read or write operations
	// that can be buffered per folder
	ReqsBufSize() int
	// MaxFileBytes indicates the maximum supported plaintext size of
	// a file in bytes.
	MaxFileBytes() uint64
	// MaxNameBytes indicates the maximum supported size of a
	// directory entry name in bytes.
	MaxNameBytes() uint32
	// MaxDirBytes indicates the maximum supported plaintext size of a
	// directory in bytes.
	MaxDirBytes() uint64
	// DoBackgroundFlushes says whether we should periodically try to
	// flush dirty files, even without a sync from the user.  Should
	// be true except for during some testing.
	DoBackgroundFlushes() bool
	SetDoBackgroundFlushes(bool)
	// RekeyWithPromptWaitTime indicates how long to wait, after
	// setting the rekey bit, before prompting for a paper key.
	RekeyWithPromptWaitTime() time.Duration

	// QuotaReclamationPeriod indicates how often should each TLF
	// should check for quota to reclaim.  If the Duration.Seconds()
	// == 0, quota reclamation should not run automatically.
	QuotaReclamationPeriod() time.Duration
	// QuotaReclamationMinUnrefAge indicates the minimum time a block
	// must have been unreferenced before it can be reclaimed.
	QuotaReclamationMinUnrefAge() time.Duration

	// ResetCaches clears and re-initializes all data and key caches.
	ResetCaches()

	MakeLogger(module string) logger.Logger
	SetLoggerMaker(func(module string) logger.Logger)
	// MetricsRegistry may be nil, which should be interpreted as
	// not using metrics at all. (i.e., as if UseNilMetrics were
	// set). This differs from how go-metrics treats nil Registry
	// objects, which is to use the default registry.
	MetricsRegistry() metrics.Registry
	SetMetricsRegistry(metrics.Registry)
	// TLFValidDuration is the time TLFs are valid before identification needs to be redone.
	TLFValidDuration() time.Duration
	// SetTLFValidDuration sets TLFValidDuration.
	SetTLFValidDuration(time.Duration)
	// Shutdown is called to free config resources.
	Shutdown() error
	// CheckStateOnShutdown tells the caller whether or not it is safe
	// to check the state of the system on shutdown.
	CheckStateOnShutdown() bool
}

// NodeCache holds Nodes, and allows libkbfs to update them when
// things change about the underlying KBFS blocks.  It is probably
// most useful to instantiate this on a per-folder-branch basis, so
// that it can create a Path with the correct DirId and Branch name.
type IFCERFTNodeCache interface {
	// GetOrCreate either makes a new Node for the given
	// BlockPointer, or returns an existing one. TODO: If we ever
	// support hard links, we will have to revisit the "name" and
	// "parent" parameters here.  name must not be empty. Returns
	// an error if parent cannot be found.
	GetOrCreate(ptr IFCERFTBlockPointer, name string, parent IFCERFTNode) (IFCERFTNode, error)
	// Get returns the Node associated with the given ptr if one
	// already exists.  Otherwise, it returns nil.
	Get(ref blockRef) IFCERFTNode
	// UpdatePointer updates the BlockPointer for the corresponding
	// Node.  NodeCache ignores this call when oldRef is not cached in
	// any Node.
	UpdatePointer(oldRef blockRef, newPtr IFCERFTBlockPointer)
	// Move swaps the parent node for the corresponding Node, and
	// updates the node's name.  NodeCache ignores the call when ptr
	// is not cached.  Returns an error if newParent cannot be found.
	// If newParent is nil, it treats the ptr's corresponding node as
	// being unlinked from the old parent completely.
	Move(ref blockRef, newParent IFCERFTNode, newName string) error
	// Unlink set the corresponding node's parent to nil and caches
	// the provided path in case the node is still open. NodeCache
	// ignores the call when ptr is not cached.  The path is required
	// because the caller may have made changes to the parent nodes
	// already that shouldn't be reflected in the cached path.
	Unlink(ref blockRef, oldPath path)
	// PathFromNode creates the path up to a given Node.
	PathFromNode(node IFCERFTNode) path
}

// RekeyQueue is a managed queue of folders needing some rekey action taken upon them
// by the current client.
type IFCERFTRekeyQueue interface {
	// Enqueue enqueues a folder for rekey action.
	Enqueue(IFCERFTTlfID) <-chan error
	// IsRekeyPending returns true if the given folder is in the rekey queue.
	IsRekeyPending(IFCERFTTlfID) bool
	// GetRekeyChannel will return any rekey completion channel (if pending.)
	GetRekeyChannel(id IFCERFTTlfID) <-chan error
	// Clear cancels all pending rekey actions and clears the queue.
	Clear()
	// Waits for all queued rekeys to finish
	Wait(ctx context.Context) error
}
