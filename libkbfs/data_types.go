// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/go/protocol"
)

const (
	// ReaderSep is the string that separates readers from writers in a
	// TLF name.
	IFCERFTReaderSep = "#"

	// TlfHandleExtensionSep is the string that separates the folder
	// participants from an extension suffix in the TLF name.
	IFCERFTTlfHandleExtensionSep = " "

	// PublicUIDName is the name given to keybase1.PublicUID.  This string
	// should correspond to an illegal or reserved Keybase user name.
	IFCERFTPublicUIDName = "_public"
)

// disallowedPrefixes must not be allowed at the beginning of any
// user-created directory entry name.
var disallowedPrefixes = [...]string{".kbfs"}

// UserInfo contains all the info about a keybase user that kbfs cares
// about.
type IFCERFTUserInfo struct {
	Name            libkb.NormalizedUsername
	UID             keybase1.UID
	VerifyingKeys   []IFCERFTVerifyingKey
	CryptPublicKeys []IFCERFTCryptPublicKey
	KIDNames        map[keybase1.KID]string

	// Revoked keys, and the time at which they were revoked.
	RevokedVerifyingKeys   map[IFCERFTVerifyingKey]keybase1.KeybaseTime
	RevokedCryptPublicKeys map[IFCERFTCryptPublicKey]keybase1.KeybaseTime
}

// SessionInfo contains all the info about the keybase session that
// kbfs cares about.
type IFCERFTSessionInfo struct {
	Name           libkb.NormalizedUsername
	UID            keybase1.UID
	Token          string
	CryptPublicKey IFCERFTCryptPublicKey
	VerifyingKey   IFCERFTVerifyingKey
}

// SigVer denotes a signature version.
type IFCERFTSigVer int

const (
	// SigED25519 is the signature type for ED25519
	IFCERFTSigED25519 IFCERFTSigVer = 1
)

// IsNil returns true if this SigVer is nil.
func (v IFCERFTSigVer) IsNil() bool {
	return int(v) == 0
}

// SignatureInfo contains all the info needed to verify a signature
// for a message.
type IFCERFTSignatureInfo struct {
	// Exported only for serialization purposes.
	Version      IFCERFTSigVer       `codec:"v"`
	Signature    []byte              `codec:"s"`
	VerifyingKey IFCERFTVerifyingKey `codec:"k"`
}

// IsNil returns true if this SignatureInfo is nil.
func (s IFCERFTSignatureInfo) IsNil() bool {
	return s.Version.IsNil() && len(s.Signature) == 0 && s.VerifyingKey.IsNil()
}

// deepCopy makes a complete copy of this SignatureInfo.
func (s IFCERFTSignatureInfo) deepCopy() IFCERFTSignatureInfo {
	signature := make([]byte, len(s.Signature))
	copy(signature[:], s.Signature[:])
	return IFCERFTSignatureInfo{s.Version, signature, s.VerifyingKey}
}

// String implements the fmt.Stringer interface for SignatureInfo.
func (s IFCERFTSignatureInfo) String() string {
	return fmt.Sprintf("SignatureInfo{Version: %d, Signature: %s, "+
		"VerifyingKey: %s}", s.Version, hex.EncodeToString(s.Signature[:]),
		&s.VerifyingKey)
}

// TLFEphemeralPublicKeys stores a list of TLFEphemeralPublicKey
type IFCERFTTLFEphemeralPublicKeys []IFCERFTTLFEphemeralPublicKey

// EncryptionVer denotes a version for the encryption method.
type IFCERFTEncryptionVer int

const (
	// EncryptionSecretbox is the encryption version that uses
	// nacl/secretbox or nacl/box.
	IFCERFTEncryptionSecretbox IFCERFTEncryptionVer = 1
)

// encryptedData is encrypted data with a nonce and a version.
type encryptedData struct {
	// Exported only for serialization purposes. Should only be
	// used by implementations of Crypto.
	Version       IFCERFTEncryptionVer `codec:"v"`
	EncryptedData []byte               `codec:"e"`
	Nonce         []byte               `codec:"n"`
}

// EncryptedTLFCryptKeyClientHalf is an encrypted
// TLFCryptKeyCLientHalf object.
type IFCERFTEncryptedTLFCryptKeyClientHalf encryptedData

// EncryptedPrivateMetadata is an encrypted PrivateMetadata object.
type IFCERFTEncryptedPrivateMetadata encryptedData

// EncryptedBlock is an encrypted Block.
type IFCERFTEncryptedBlock encryptedData

// EncryptedMerkleLeaf is an encrypted Merkle leaf.
type IFCERFTEncryptedMerkleLeaf struct {
	_struct       bool `codec:",toarray"`
	Version       IFCERFTEncryptionVer
	EncryptedData []byte
}

// EncryptedTLFCryptKeyClientAndEphemeral has what's needed to
// request a client half decryption.
type IFCERFTEncryptedTLFCryptKeyClientAndEphemeral struct {
	// PublicKey contains the wrapped Key ID of the public key
	PubKey IFCERFTCryptPublicKey
	// ClientHalf contains the encrypted client half of the TLF key
	ClientHalf IFCERFTEncryptedTLFCryptKeyClientHalf
	// EPubKey contains the ephemeral public key used to encrypt ClientHalf
	EPubKey IFCERFTTLFEphemeralPublicKey
}

// KeyGen is the type of a key generation for a top-level folder.
type IFCERFTKeyGen int

const (
	// PublicKeyGen is the value used for public TLFs. Note that
	// it is not considered a valid key generation.
	IFCERFTPublicKeyGen IFCERFTKeyGen = -1
	// FirstValidKeyGen is the first value that is considered a
	// valid key generation. Note that the nil value is not
	// considered valid.
	IFCERFTFirstValidKeyGen = 1
)

// MetadataVer is the type of a version for marshalled KBFS metadata
// structures.
type IFCERFTMetadataVer int

const (
	// FirstValidMetadataVer is the first value that is considered a
	// valid data version. For historical reasons 0 is considered
	// valid.
	FirstValidMetadataVer = 0
	// PreExtraMetadataVer is the latest metadata version that did not include
	// support for extra MD fields.
	PreExtraMetadataVer = 1
	// InitialExtraMetadataVer is the first metadata version that did
	// include support for extra MD fields.
	InitialExtraMetadataVer = 2
)

// DataVer is the type of a version for marshalled KBFS data
// structures.
type IFCERFTDataVer int

const (
	// FirstValidDataVer is the first value that is considered a
	// valid data version. Note that the nil value is not
	// considered valid.
	FirstValidDataVer = 1
	// FilesWithHolesDataVer is the data version for files
	// with holes.
	FilesWithHolesDataVer = 2
)

// BlockRefNonce is a 64-bit unique sequence of bytes for identifying
// this reference of a block ID from other references to the same
// (duplicated) block.
type IFCERFTBlockRefNonce [8]byte

// zeroBlockRefNonce is a special BlockRefNonce used for the initial
// reference to a block.
var zeroBlockRefNonce = IFCERFTBlockRefNonce([8]byte{0, 0, 0, 0, 0, 0, 0, 0})

func (nonce IFCERFTBlockRefNonce) String() string {
	return hex.EncodeToString(nonce[:])
}

// blockRef is a block ID/ref nonce pair, which defines a unique
// reference to a block.
type blockRef struct {
	id       BlockID
	refNonce IFCERFTBlockRefNonce
}

func (r blockRef) IsValid() bool {
	return r.id.IsValid()
}

func (r blockRef) String() string {
	s := fmt.Sprintf("blockRef{id: %s", r.id)
	if r.refNonce != zeroBlockRefNonce {
		s += fmt.Sprintf(", refNonce: %s", r.refNonce)
	}
	s += "}"
	return s
}

// BlockContext contains all the information used by the server to
// identify blocks (other than the ID).
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them.
type IFCERFTBlockContext struct {
	// Creator is the UID that was first charged for the initial
	// reference to this block.
	Creator keybase1.UID `codec:"c"`
	// Writer is the UID that should be charged for this reference to
	// the block.  If empty, it defaults to Creator.
	Writer keybase1.UID `codec:"w,omitempty"`
	// When RefNonce is all 0s, this is the initial reference to a
	// particular block.  Using a constant refnonce for the initial
	// reference allows the server to identify and optimize for the
	// common case where there is only one reference for a block.  Two
	// initial references cannot happen simultaneously, because the
	// encrypted block contents (and thus the block ID) will be
	// randomized by the server-side block crypt key half.  All
	// subsequent references to the same block must have a random
	// RefNonce (it can't be a monotonically increasing number because
	// that would require coordination among clients).
	RefNonce IFCERFTBlockRefNonce `codec:"r,omitempty"`
}

// GetCreator returns the creator of the associated block.
func (c IFCERFTBlockContext) GetCreator() keybase1.UID {
	return c.Creator
}

// GetWriter returns the writer of the associated block.
func (c IFCERFTBlockContext) GetWriter() keybase1.UID {
	if !c.Writer.IsNil() {
		return c.Writer
	}
	return c.Creator
}

// SetWriter sets the Writer field, if necessary.
func (c *IFCERFTBlockContext) SetWriter(newWriter keybase1.UID) {
	if c.Creator != newWriter {
		c.Writer = newWriter
	} else {
		// save some bytes by not populating the separate Writer
		// field if it matches the creator.
		c.Writer = ""
	}
}

// GetRefNonce returns the ref nonce of the associated block.
func (c IFCERFTBlockContext) GetRefNonce() IFCERFTBlockRefNonce {
	return c.RefNonce
}

// IsFirstRef returns whether or not p represents the first reference
// to the corresponding BlockID.
func (c IFCERFTBlockContext) IsFirstRef() bool {
	return c.RefNonce == zeroBlockRefNonce
}

func (c IFCERFTBlockContext) String() string {
	s := fmt.Sprintf("BlockContext{Creator: %s", c.Creator)
	if len(c.Writer) > 0 {
		s += fmt.Sprintf(", Writer: %s", c.Writer)
	}
	if c.RefNonce != zeroBlockRefNonce {
		s += fmt.Sprintf(", RefNonce: %s", c.RefNonce)
	}
	s += "}"
	return s
}

// BlockPointer contains the identifying information for a block in KBFS.
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them.
type IFCERFTBlockPointer struct {
	ID      BlockID        `codec:"i"`
	KeyGen  IFCERFTKeyGen  `codec:"k"` // if valid, which generation of the TLFKeyBundle to use.
	DataVer IFCERFTDataVer `codec:"d"` // if valid, which version of the KBFS data structures is pointed to
	IFCERFTBlockContext
}

// IsValid returns whether the block pointer is valid. A zero block
// pointer is considered invalid.
func (p IFCERFTBlockPointer) IsValid() bool {
	if !p.ID.IsValid() {
		return false
	}

	// TODO: Should also check KeyGen, DataVer, and Creator. (A
	// bunch of tests use invalid values for one of these.)

	return true
}

func (p IFCERFTBlockPointer) String() string {
	return fmt.Sprintf("BlockPointer{ID: %s, KeyGen: %d, DataVer: %d, Context: %s}", p.ID, p.KeyGen, p.DataVer, p.IFCERFTBlockContext)
}

// IsInitialized returns whether or not this BlockPointer has non-nil data.
func (p IFCERFTBlockPointer) IsInitialized() bool {
	return p.ID != BlockID{}
}

func (p IFCERFTBlockPointer) ref() blockRef {
	return blockRef{
		id:       p.ID,
		refNonce: p.RefNonce,
	}
}

// BlockInfo contains all information about a block in KBFS and its
// contents.
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them.
type IFCERFTBlockInfo struct {
	IFCERFTBlockPointer
	// When non-zero, the size of the encoded (and possibly
	// encrypted) data contained in the block. When non-zero,
	// always at least the size of the plaintext data contained in
	// the block.
	EncodedSize uint32 `codec:"e"`
}

var bpSize = uint64(reflect.TypeOf(IFCERFTBlockPointer{}).Size())

// ReadyBlockData is a block that has been encoded (and encrypted).
type IFCERFTReadyBlockData struct {
	// These fields should not be used outside of BlockOps.Put().
	buf        []byte
	serverHalf IFCERFTBlockCryptKeyServerHalf
}

// GetEncodedSize returns the size of the encoded (and encrypted)
// block data.
func (r IFCERFTReadyBlockData) GetEncodedSize() int {
	return len(r.buf)
}

// Favorite is a top-level favorited folder name.
type IFCERFTFavorite struct {
	Name   string
	Public bool
}

// NewFavoriteFromFolder creates a Favorite from a
// keybase1.Folder.
func IFCERFTNewFavoriteFromFolder(folder keybase1.Folder) *IFCERFTFavorite {
	name := folder.Name
	if !folder.Private {
		// Old versions of the client still use an outdated "#public"
		// suffix for favorited public folders. TODO: remove this once
		// those old versions of the client are retired.
		const oldPublicSuffix = IFCERFTReaderSep + "public"
		name = strings.TrimSuffix(folder.Name, oldPublicSuffix)
	}

	return &IFCERFTFavorite{
		Name:   name,
		Public: !folder.Private,
	}
}

func (f IFCERFTFavorite) toKBFolder(created bool) keybase1.Folder {
	return keybase1.Folder{
		Name:    f.Name,
		Private: !f.Public,
		Created: created,
	}
}

// PathNode is a single node along an KBFS path, pointing to the top
// block for that node of the path.
type pathNode struct {
	IFCERFTBlockPointer
	Name string
}

func (n pathNode) isValid() bool {
	return n.IFCERFTBlockPointer.IsValid()
}

// DebugString returns a string representation of the node with all
// pointer information.
func (n pathNode) DebugString() string {
	return fmt.Sprintf("%s(ptr=%s)", n.Name, n.IFCERFTBlockPointer)
}

// BranchName is the name given to a KBFS branch, for a particular
// top-level folder.  Currently, the notion of a "branch" is
// client-side only, and can be used to specify which root to use for
// a top-level folder.  (For example, viewing a historical archive
// could use a different branch name.)
type IFCERFTBranchName string

const (
	// MasterBranch represents the mainline branch for a top-level
	// folder.  Set to the empty string so that the default will be
	// the master branch.
	IFCERFTMasterBranch IFCERFTBranchName = ""
)

// FolderBranch represents a unique pair of top-level folder and a
// branch of that folder.
type IFCERFTFolderBranch struct {
	Tlf    IFCERFTTlfID
	Branch IFCERFTBranchName // master branch, by default
}

func (fb IFCERFTFolderBranch) String() string {
	s := fb.Tlf.String()
	if len(fb.Branch) > 0 {
		s += fmt.Sprintf("(branch=%s)", fb.Branch)
	}
	return s
}

// path represents the full KBFS path to a particular location, so
// that a flush can traverse backwards and fix up ids along the way.
type path struct {
	IFCERFTFolderBranch
	path []pathNode
}

// isValid() returns true if the path has at least one node (for the
// root).
func (p path) isValid() bool {
	if len(p.path) < 1 {
		return false
	}

	for _, n := range p.path {
		if !n.isValid() {
			return false
		}
	}

	return true
}

// hasValidParent() returns true if this path is valid and
// parentPath() is a valid path.
func (p path) hasValidParent() bool {
	return len(p.path) >= 2 && p.parentPath().isValid()
}

// tailName returns the name of the final node in the Path. Must be
// called with a valid path.
func (p path) tailName() string {
	return p.path[len(p.path)-1].Name
}

// tailPointer returns the BlockPointer of the final node in the Path.
// Must be called with a valid path.
func (p path) tailPointer() IFCERFTBlockPointer {
	return p.path[len(p.path)-1].IFCERFTBlockPointer
}

// DebugString returns a string representation of the path with all
// branch and pointer information.
func (p path) DebugString() string {
	debugNames := make([]string, 0, len(p.path))
	for _, node := range p.path {
		debugNames = append(debugNames, node.DebugString())
	}
	return fmt.Sprintf("%s:%s", p.IFCERFTFolderBranch, strings.Join(debugNames, "/"))
}

// String implements the fmt.Stringer interface for Path.
func (p path) String() string {
	names := make([]string, 0, len(p.path))
	for _, node := range p.path {
		names = append(names, node.Name)
	}
	return strings.Join(names, "/")
}

// parentPath returns a new Path representing the parent subdirectory
// of this Path. Must be called with a valid path. Should not be
// called with a path of only a single node, as that would produce an
// invalid path.
func (p path) parentPath() *path {
	return &path{p.IFCERFTFolderBranch, p.path[:len(p.path)-1]}
}

// ChildPath returns a new Path with the addition of a new entry
// with the given name and BlockPointer.
func (p path) ChildPath(name string, ptr IFCERFTBlockPointer) path {
	child := path{
		IFCERFTFolderBranch: p.IFCERFTFolderBranch,
		path:                make([]pathNode, len(p.path), len(p.path)+1),
	}
	copy(child.path, p.path)
	child.path = append(child.path, pathNode{Name: name, IFCERFTBlockPointer: ptr})
	return child
}

// ChildPathNoPtr returns a new Path with the addition of a new entry
// with the given name.  That final PathNode will have no BlockPointer.
func (p path) ChildPathNoPtr(name string) path {
	return p.ChildPath(name, IFCERFTBlockPointer{})
}

// hasPublic returns whether or not this is a top-level folder that
// should have a "public" subdirectory.
func (p path) hasPublic() bool {
	// This directory has a corresponding public subdirectory if the
	// path has only one node and the top-level directory is not
	// already public TODO: Ideally, we'd also check if there are no
	// explicit readers, but for now we expect the caller to check
	// that.
	return len(p.path) == 1 && !p.Tlf.IsPublic()
}

// BlockChanges tracks the set of blocks that changed in a commit, and
// the operations that made the changes.  It might consist of just a
// BlockPointer if the list is too big to embed in the MD structure
// directly.
//
// If this commit represents a conflict-resolution merge, which may
// comprise multiple individual operations, then there will be an
// ordered list of the changes for individual operations.  This lets
// the notification and conflict resolution strategies figure out the
// difference between a renamed file and a modified file, for example.
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them.
type IFCERFTBlockChanges struct {
	// If this is set, the actual changes are stored in a block (where
	// the block contains a serialized version of BlockChanges)
	Info IFCERFTBlockInfo `codec:"p,omitempty"`
	// An ordered list of operations completed in this update
	Ops opsList `codec:"o,omitempty"`
	// Estimate the number of bytes that this set of changes will take to encode
	sizeEstimate uint64
}

// Equals returns true if the given BlockChanges is equal to this
// BlockChanges.  Currently does not check for equality at the
// operation level.
func (bc IFCERFTBlockChanges) Equals(other IFCERFTBlockChanges) bool {
	if bc.Info != other.Info || len(bc.Ops) != len(other.Ops) ||
		bc.sizeEstimate != other.sizeEstimate {
		return false
	}
	// TODO: check for op equality?
	return true
}

func (bc *IFCERFTBlockChanges) addBPSize() {
	// We want an estimate of the codec-encoded size, but the
	// in-memory size is good enough.
	bc.sizeEstimate += bpSize
}

// AddRefBlock adds the newly-referenced block to this BlockChanges
// and updates the size estimate.
func (bc *IFCERFTBlockChanges) AddRefBlock(ptr IFCERFTBlockPointer) {
	bc.Ops[len(bc.Ops)-1].AddRefBlock(ptr)
	bc.addBPSize()
}

// AddUnrefBlock adds the newly unreferenced block to this BlockChanges
// and updates the size estimate.
func (bc *IFCERFTBlockChanges) AddUnrefBlock(ptr IFCERFTBlockPointer) {
	bc.Ops[len(bc.Ops)-1].AddUnrefBlock(ptr)
	bc.addBPSize()
}

// AddUpdate adds the newly updated block to this BlockChanges
// and updates the size estimate.
func (bc *IFCERFTBlockChanges) AddUpdate(oldPtr IFCERFTBlockPointer, newPtr IFCERFTBlockPointer) {
	bc.Ops[len(bc.Ops)-1].AddUpdate(oldPtr, newPtr)
	// add sizes for both block pointers
	bc.addBPSize()
	bc.addBPSize()
}

// AddOp starts a new operation for this BlockChanges.  Subsequent
// Add* calls will populate this operation.
func (bc *IFCERFTBlockChanges) AddOp(o op) {
	bc.Ops = append(bc.Ops, o)
	bc.sizeEstimate += o.SizeExceptUpdates()
}

// EntryType is the type of a directory entry.
type IFCERFTEntryType int

const (
	// File is a regular file.
	IFCERFTFile IFCERFTEntryType = iota
	// Exec is an executable file.
	IFCERFTExec
	// Dir is a directory.
	IFCERFTDir
	// Sym is a symbolic link.
	IFCERFTSym
)

// String implements the fmt.Stringer interface for EntryType
func (et IFCERFTEntryType) String() string {
	switch et {
	case IFCERFTFile:
		return "FILE"
	case IFCERFTExec:
		return "EXEC"
	case IFCERFTDir:
		return "DIR"
	case IFCERFTSym:
		return "SYM"
	}
	return "<invalid EntryType>"
}

// EXCL indicates whether O_EXCL is set on a fuse call
type IFCERFTEXCL bool

const (
	// NoEXCL indicates O_EXCL is not set
	IFCERFTNoEXCL IFCERFTEXCL = false

	// WithEXCL indicates O_EXCL is set
	IFCERFTWithEXCL IFCERFTEXCL = true
)

func (o IFCERFTEXCL) String() string {
	switch o {
	case IFCERFTNoEXCL:
		return "O_EXCL unset"
	case IFCERFTWithEXCL:
		return "O_EXCL set"
	default:
		return "<invalid EXCL>"
	}
}

// EntryInfo is the (non-block-related) info a directory knows about
// its child.
//
// NOTE: Don't add or modify anything in this struct without
// considering how old clients will handle them (since this is
// embedded in DirEntry).
type IFCERFTEntryInfo struct {
	Type    IFCERFTEntryType
	Size    uint64
	SymPath string `codec:",omitempty"` // must be within the same root dir
	// Mtime is in unix nanoseconds
	Mtime int64
	// Ctime is in unix nanoseconds
	Ctime int64
}

// extCode is used to register codec extensions
type extCode uint64

// these track the start of a range of unique extCodes for various
// types of extensions.
const (
	extCodeOpsRangeStart  = 1
	extCodeListRangeStart = 101
)

// ReportedError represents an error reported by KBFS.
type ReportedError struct {
	Time  time.Time
	Error error
	Stack []uintptr
}

// MergeStatus represents the merge status of a TLF.
type IFCERFTMergeStatus int

const (
	// Merged means that the TLF is merged and no conflict
	// resolution needs to be done.
	IFCERFTMerged IFCERFTMergeStatus = iota
	// Unmerged means that the TLF is unmerged and conflict
	// resolution needs to be done. Metadata blocks which
	// represent unmerged history should have a non-null
	// branch ID defined.
	IFCERFTUnmerged
)

func (m IFCERFTMergeStatus) String() string {
	switch m {
	case IFCERFTMerged:
		return "merged"
	case IFCERFTUnmerged:
		return "unmerged"
	default:
		return "unknown"
	}
}

// UsageType indicates the type of usage that quota manager is keeping stats of
type IFCERFTUsageType int

const (
	// UsageWrite indicates a block is written (written blocks include archived blocks)
	IFCERFTUsageWrite IFCERFTUsageType = iota
	// UsageArchive indicates an existing block is archived
	IFCERFTUsageArchive
	// UsageRead indicates a block is read
	IFCERFTUsageRead
	// NumUsage indicates the number of usage types
	IFCERFTNumUsage
)

// UsageStat tracks the amount of bytes/blocks used, broken down by usage types
type IFCERFTUsageStat struct {
	Bytes  map[IFCERFTUsageType]int64
	Blocks map[IFCERFTUsageType]int64
	// Mtime is in unix nanoseconds
	Mtime int64
}

// NewUsageStat creates a new UsageStat
func IFCERFTNewUsageStat() *IFCERFTUsageStat {
	return &IFCERFTUsageStat{
		Bytes:  make(map[IFCERFTUsageType]int64),
		Blocks: make(map[IFCERFTUsageType]int64),
	}
}

// NonZero checks whether UsageStat has accumulated any usage info
func (u *IFCERFTUsageStat) NonZero() bool {
	for i := IFCERFTUsageType(0); i < IFCERFTNumUsage; i++ {
		if u.Bytes[i] != 0 {
			return true
		}
	}
	return false
}

//AccumOne records the usage of one block, whose size is denoted by change
//A positive change means the block is newly added, negative means the block
//is deleted. If archive is true, it means the block is archived.
func (u *IFCERFTUsageStat) AccumOne(change int, usage IFCERFTUsageType) {
	if change == 0 {
		return
	}
	if usage < IFCERFTUsageWrite || usage > IFCERFTUsageRead {
		return
	}
	u.Bytes[usage] += int64(change)
	if change > 0 {
		u.Blocks[usage]++
	} else {
		u.Blocks[usage]--
	}
}

// Accum combines changes to the existing UserQuotaInfo object using accumulation function accumF.
func (u *IFCERFTUsageStat) Accum(another *IFCERFTUsageStat, accumF func(int64, int64) int64) {
	if another == nil {
		return
	}
	for k, v := range another.Bytes {
		u.Bytes[k] = accumF(u.Bytes[k], v)
	}
	for k, v := range another.Blocks {
		u.Blocks[k] = accumF(u.Blocks[k], v)
	}
}

// UserQuotaInfo contains a user's quota usage information
type IFCERFTUserQuotaInfo struct {
	Folders map[string]*IFCERFTUsageStat
	Total   *IFCERFTUsageStat
	Limit   int64
}

// NewUserQuotaInfo returns a newly constructed UserQuotaInfo.
func IFCERFTNewUserQuotaInfo() *IFCERFTUserQuotaInfo {
	return &IFCERFTUserQuotaInfo{
		Folders: make(map[string]*IFCERFTUsageStat),
		Total:   IFCERFTNewUsageStat(),
	}
}

// AccumOne combines one quota charge to the existing UserQuotaInfo
func (u *IFCERFTUserQuotaInfo) AccumOne(change int, folder string, usage IFCERFTUsageType) {
	if _, ok := u.Folders[folder]; !ok {
		u.Folders[folder] = IFCERFTNewUsageStat()
	}
	u.Folders[folder].AccumOne(change, usage)
	u.Total.AccumOne(change, usage)
}

// Accum combines changes to the existing UserQuotaInfo object using accumulation function accumF.
func (u *IFCERFTUserQuotaInfo) Accum(another *IFCERFTUserQuotaInfo, accumF func(int64, int64) int64) {
	if another == nil {
		return
	}
	if u.Total == nil {
		u.Total = IFCERFTNewUsageStat()
	}
	u.Total.Accum(another.Total, accumF)
	for f, change := range another.Folders {
		if _, ok := u.Folders[f]; !ok {
			u.Folders[f] = IFCERFTNewUsageStat()
		}
		u.Folders[f].Accum(change, accumF)
	}
}

// ToBytes marshals this UserQuotaInfo
func (u *IFCERFTUserQuotaInfo) ToBytes(config IFCERFTConfig) ([]byte, error) {
	return config.Codec().Encode(u)
}

// UserQuotaInfoDecode decodes b into a UserQuotaInfo
func IFCERFTUserQuotaInfoDecode(b []byte, config IFCERFTConfig) (*IFCERFTUserQuotaInfo, error) {
	var info IFCERFTUserQuotaInfo
	err := config.Codec().Decode(b, &info)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

// OpSummary describes the changes performed by a single op, and is
// suitable for encoding directly as JSON.
type IFCERFTOpSummary struct {
	Op      string
	Refs    []string
	Unrefs  []string
	Updates map[string]string
}

// UpdateSummary describes the operations done by a single MD revision.
type IFCERFTUpdateSummary struct {
	Revision  MetadataRevision
	Date      time.Time
	Writer    string
	LiveBytes uint64 // the "DiskUsage" for the TLF as of this revision
	Ops       []IFCERFTOpSummary
}

// TLFUpdateHistory gives all the summaries of all updates in a TLF's
// history.
type IFCERFTTLFUpdateHistory struct {
	ID      string
	Name    string
	Updates []IFCERFTUpdateSummary
}

// writerInfo is the keybase username and device that generated the operation.
type writerInfo struct {
	name       libkb.NormalizedUsername
	kid        keybase1.KID
	deviceName string
}

// ErrorModeType indicates what type of operation was being attempted
// when an error was reported.
type IFCERFTErrorModeType int

const (
	// ReadMode indicates that an error happened while trying to read.
	IFCERFTReadMode IFCERFTErrorModeType = iota
	// WriteMode indicates that an error happened while trying to write.
	IFCERFTWriteMode
)
