// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"

	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/go/protocol"
)

// ErrorFile is the name of the virtual file in KBFS that should
// contain the last reported error(s).
var IFCERFTErrorFile = ".kbfs_error"

// WrapError simply wraps an error in a fmt.Stringer interface, so
// that it can be reported.
type IFCERFTWrapError struct {
	Err error
}

// String implements the fmt.Stringer interface for WrapError
func (e IFCERFTWrapError) String() string {
	return e.Err.Error()
}

// NameExistsError indicates that the user tried to create an entry
// for a name that already existed in a subdirectory.
type IFCERFTNameExistsError struct {
	Name string
}

// Error implements the error interface for NameExistsError
func (e IFCERFTNameExistsError) Error() string {
	return fmt.Sprintf("%s already exists", e.Name)
}

// NoSuchNameError indicates that the user tried to access a
// subdirectory entry that doesn't exist.
type IFCERFTNoSuchNameError struct {
	Name string
}

// Error implements the error interface for NoSuchNameError
func (e IFCERFTNoSuchNameError) Error() string {
	return fmt.Sprintf("%s doesn't exist", e.Name)
}

// NoSuchUserError indicates that the given user couldn't be resolved.
type IFCERFTNoSuchUserError struct {
	Input string
}

// Error implements the error interface for NoSuchUserError
func (e IFCERFTNoSuchUserError) Error() string {
	return fmt.Sprintf("%s is not a Keybase user", e.Input)
}

// BadTLFNameError indicates a top-level folder name that has an
// incorrect format.
type IFCERFTBadTLFNameError struct {
	Name string
}

// Error implements the error interface for BadTLFNameError.
func (e IFCERFTBadTLFNameError) Error() string {
	return fmt.Sprintf("TLF name %s is in an incorrect format", e.Name)
}

// InvalidBlockRefError indicates an invalid block reference was
// encountered.
type IFCERFTInvalidBlockRefError struct {
	ref IFCERFTBlockRef
}

func (e IFCERFTInvalidBlockRefError) Error() string {
	return fmt.Sprintf("Invalid block ref %s", e.ref)
}

// InvalidPathError indicates an invalid path was encountered.
type IFCERFTInvalidPathError struct {
	p IFCERFTPath
}

// Error implements the error interface for InvalidPathError.
func (e IFCERFTInvalidPathError) Error() string {
	return fmt.Sprintf("Invalid path %s", e.p.DebugString())
}

// InvalidParentPathError indicates a path without a valid parent was
// encountered.
type IFCERFTInvalidParentPathError struct {
	p IFCERFTPath
}

// Error implements the error interface for InvalidParentPathError.
func (e IFCERFTInvalidParentPathError) Error() string {
	return fmt.Sprintf("Path with invalid parent %s", e.p.DebugString())
}

// DirNotEmptyError indicates that the user tried to unlink a
// subdirectory that was not empty.
type IFCERFTDirNotEmptyError struct {
	Name string
}

// Error implements the error interface for DirNotEmptyError
func (e IFCERFTDirNotEmptyError) Error() string {
	return fmt.Sprintf("Directory %s is not empty and can't be removed", e.Name)
}

// TlfAccessError that the user tried to perform an unpermitted
// operation on a top-level folder.
type IFCERFTTlfAccessError struct {
	ID IFCERFTTlfID
}

// Error implements the error interface for TlfAccessError
func (e IFCERFTTlfAccessError) Error() string {
	return fmt.Sprintf("Operation not permitted on folder %s", e.ID)
}

// RenameAcrossDirsError indicates that the user tried to do an atomic
// rename across directories.
type IFCERFTRenameAcrossDirsError struct {
}

// Error implements the error interface for RenameAcrossDirsError
func (e IFCERFTRenameAcrossDirsError) Error() string {
	return fmt.Sprintf("Cannot rename across directories")
}

// ErrorFileAccessError indicates that the user tried to perform an
// operation on the ErrorFile that is not allowed.
type IFCERFTErrorFileAccessError struct {
}

// Error implements the error interface for ErrorFileAccessError
func (e IFCERFTErrorFileAccessError) Error() string {
	return fmt.Sprintf("Operation not allowed on file %s", IFCERFTErrorFile)
}

// ReadAccessError indicates that the user tried to read from a
// top-level folder without read permission.
type IFCERFTReadAccessError struct {
	User   libkb.NormalizedUsername
	Tlf    IFCERFTCanonicalTlfName
	Public bool
}

// Error implements the error interface for ReadAccessError
func (e IFCERFTReadAccessError) Error() string {
	return fmt.Sprintf("%s does not have read access to directory %s",
		e.User, buildCanonicalPath(e.Public, e.Tlf))
}

// WriteAccessError indicates that the user tried to read from a
// top-level folder without read permission.
type IFCERFTWriteAccessError struct {
	User   libkb.NormalizedUsername
	Tlf    IFCERFTCanonicalTlfName
	Public bool
}

// Error implements the error interface for WriteAccessError
func (e IFCERFTWriteAccessError) Error() string {
	return fmt.Sprintf("%s does not have write access to directory %s",
		e.User, buildCanonicalPath(e.Public, e.Tlf))
}

// NewReadAccessError constructs a ReadAccessError for the given
// directory and user.
func IFCERFTNewReadAccessError(h *IFCERFTTlfHandle, username libkb.NormalizedUsername) error {
	tlfname := h.GetCanonicalName()
	return IFCERFTReadAccessError{username, tlfname, h.IsPublic()}
}

// NewWriteAccessError constructs a WriteAccessError for the given
// directory and user.
func IFCERFTNewWriteAccessError(h *IFCERFTTlfHandle, username libkb.NormalizedUsername) error {
	tlfname := h.GetCanonicalName()
	return IFCERFTWriteAccessError{username, tlfname, h.IsPublic()}
}

// NeedSelfRekeyError indicates that the folder in question needs to
// be rekeyed for the local device, and can be done so by one of the
// other user's devices.
type IFCERFTNeedSelfRekeyError struct {
	Tlf IFCERFTCanonicalTlfName
}

// Error implements the error interface for NeedSelfRekeyError
func (e IFCERFTNeedSelfRekeyError) Error() string {
	return fmt.Sprintf("This device does not yet have read access to "+
		"directory %s, log into Keybase from one of your other "+
		"devices to grant access", buildCanonicalPath(false, e.Tlf))
}

// NeedOtherRekeyError indicates that the folder in question needs to
// be rekeyed for the local device, and can only done so by one of the
// other users.
type IFCERFTNeedOtherRekeyError struct {
	Tlf IFCERFTCanonicalTlfName
}

// Error implements the error interface for NeedOtherRekeyError
func (e IFCERFTNeedOtherRekeyError) Error() string {
	return fmt.Sprintf("This device does not yet have read access to "+
		"directory %s, ask one of the other directory participants to "+
		"log into Keybase to grant you access automatically",
		buildCanonicalPath(false, e.Tlf))
}

// NotFileBlockError indicates that a file block was expected but a
// block of a different type was found.
//
// ptr and branch should be filled in, but p may be empty.
type IFCERFTNotFileBlockError struct {
	ptr    IFCERFTBlockPointer
	branch IFCERFTBranchName
	p      IFCERFTPath
}

func (e IFCERFTNotFileBlockError) Error() string {
	return fmt.Sprintf("The block at %s is not a file block (branch=%s, path=%s)", e.ptr, e.branch, e.p)
}

// NotDirBlockError indicates that a file block was expected but a
// block of a different type was found.
//
// ptr and branch should be filled in, but p may be empty.
type IFCERFTNotDirBlockError struct {
	ptr    IFCERFTBlockPointer
	branch IFCERFTBranchName
	p      IFCERFTPath
}

func (e IFCERFTNotDirBlockError) Error() string {
	return fmt.Sprintf("The block at %s is not a dir block (branch=%s, path=%s)", e.ptr, e.branch, e.p)
}

// NotFileError indicates that the user tried to perform a
// file-specific operation on something that isn't a file.
type IFCERFTNotFileError struct {
	path IFCERFTPath
}

// Error implements the error interface for NotFileError
func (e IFCERFTNotFileError) Error() string {
	return fmt.Sprintf("%s is not a file (folder %s)", e.path, e.path.Tlf)
}

// NotDirError indicates that the user tried to perform a
// dir-specific operation on something that isn't a directory.
type IFCERFTNotDirError struct {
	path IFCERFTPath
}

// Error implements the error interface for NotDirError
func (e IFCERFTNotDirError) Error() string {
	return fmt.Sprintf("%s is not a directory (folder %s)", e.path, e.path.Tlf)
}

// BlockDecodeError indicates that a block couldn't be decoded as
// expected; probably it is the wrong type.
type IFCERFTBlockDecodeError struct {
	decodeErr error
}

// Error implements the error interface for BlockDecodeError
func (e IFCERFTBlockDecodeError) Error() string {
	return fmt.Sprintf("Decode error for a block: %v", e.decodeErr)
}

// BadDataError indicates that KBFS is storing corrupt data for a block.
type IFCERFTBadDataError struct {
	ID BlockID
}

// Error implements the error interface for BadDataError
func (e IFCERFTBadDataError) Error() string {
	return fmt.Sprintf("Bad data for block %v", e.ID)
}

// NoSuchBlockError indicates that a block for the associated ID doesn't exist.
type IFCERFTNoSuchBlockError struct {
	ID BlockID
}

// Error implements the error interface for NoSuchBlockError
func (e IFCERFTNoSuchBlockError) Error() string {
	return fmt.Sprintf("Couldn't get block %v", e.ID)
}

// BadCryptoError indicates that KBFS performed a bad crypto operation.
type IFCERFTBadCryptoError struct {
	ID BlockID
}

// Error implements the error interface for BadCryptoError
func (e IFCERFTBadCryptoError) Error() string {
	return fmt.Sprintf("Bad crypto for block %v", e.ID)
}

// BadCryptoMDError indicates that KBFS performed a bad crypto
// operation, specifically on a MD object.
type IFCERFTBadCryptoMDError struct {
	ID IFCERFTTlfID
}

// Error implements the error interface for BadCryptoMDError
func (e IFCERFTBadCryptoMDError) Error() string {
	return fmt.Sprintf("Bad crypto for the metadata of directory %v", e.ID)
}

// BadMDError indicates that the system is storing corrupt MD object
// for the given TLF ID.
type IFCERFTBadMDError struct {
	ID IFCERFTTlfID
}

// Error implements the error interface for BadMDError
func (e IFCERFTBadMDError) Error() string {
	return fmt.Sprintf("Wrong format for metadata for directory %v", e.ID)
}

// MDMissingDataError indicates that we are trying to take get the
// metadata ID of a MD object with no serialized data field.
type IFCERFTMDMissingDataError struct {
	ID IFCERFTTlfID
}

// Error implements the error interface for MDMissingDataError
func (e IFCERFTMDMissingDataError) Error() string {
	return fmt.Sprintf("No serialized private data in the metadata "+
		"for directory %v", e.ID)
}

// MDMismatchError indicates an inconsistent or unverifiable MD object
// for the given top-level folder.
type IFCERFTMDMismatchError struct {
	Dir string
	Err error
}

// Error implements the error interface for MDMismatchError
func (e IFCERFTMDMismatchError) Error() string {
	return fmt.Sprintf("Could not verify metadata for directory %s: %s",
		e.Dir, e.Err)
}

// NoSuchMDError indicates that there is no MD object for the given
// folder, revision, and merged status.
type IFCERFTNoSuchMDError struct {
	Tlf IFCERFTTlfID
	Rev IFCERFTMetadataRevision
	BID IFCERFTBranchID
}

// Error implements the error interface for NoSuchMDError
func (e IFCERFTNoSuchMDError) Error() string {
	return fmt.Sprintf("Couldn't get metadata for folder %v, revision %d, "+
		"%s", e.Tlf, e.Rev, e.BID)
}

// InvalidMetadataVersionError indicates that an invalid metadata version was
// used.
type IFCERFTInvalidMetadataVersionError struct {
	Tlf         IFCERFTTlfID
	MetadataVer IFCERFTMetadataVer
}

// Error implements the error interface for InvalidMetadataVersionError.
func (e IFCERFTInvalidMetadataVersionError) Error() string {
	return fmt.Sprintf("Invalid metadata version %d for folder %s",
		int(e.MetadataVer), e.Tlf)
}

// NewMetadataVersionError indicates that the metadata for the given
// folder has been written using a new metadata version that our
// client doesn't understand.
type IFCERFTNewMetadataVersionError struct {
	Tlf         IFCERFTTlfID
	MetadataVer IFCERFTMetadataVer
}

// Error implements the error interface for NewMetadataVersionError.
func (e IFCERFTNewMetadataVersionError) Error() string {
	return fmt.Sprintf(
		"The metadata for folder %s is of a version (%d) that we can't read",
		e.Tlf, e.MetadataVer)
}

// InvalidDataVersionError indicates that an invalid data version was
// used.
type IFCERFTInvalidDataVersionError struct {
	DataVer IFCERFTDataVer
}

// Error implements the error interface for InvalidDataVersionError.
func (e IFCERFTInvalidDataVersionError) Error() string {
	return fmt.Sprintf("Invalid data version %d", int(e.DataVer))
}

// NewDataVersionError indicates that the data at the given path has
// been written using a new data version that our client doesn't
// understand.
type IFCERFTNewDataVersionError struct {
	path    IFCERFTPath
	DataVer IFCERFTDataVer
}

// Error implements the error interface for NewDataVersionError.
func (e IFCERFTNewDataVersionError) Error() string {
	return fmt.Sprintf(
		"The data at path %s is of a version (%d) that we can't read "+
			"(in folder %s)",
		e.path, e.DataVer, e.path.Tlf)
}

// OutdatedVersionError indicates that we have encountered some new
// data version we don't understand, and the user should be prompted
// to upgrade.
type IFCERFTOutdatedVersionError struct {
}

// Error implements the error interface for OutdatedVersionError.
func (e IFCERFTOutdatedVersionError) Error() string {
	return "Your software is out of date, and cannot read this data.  " +
		"Please use `keybase update check` to upgrade your software."
}

// InvalidKeyGenerationError indicates that an invalid key generation
// was used.
type IFCERFTInvalidKeyGenerationError struct {
	TlfHandle *IFCERFTTlfHandle
	KeyGen    IFCERFTKeyGen
}

// Error implements the error interface for InvalidKeyGenerationError.
func (e IFCERFTInvalidKeyGenerationError) Error() string {
	return fmt.Sprintf("Invalid key generation %d for %v", int(e.KeyGen), e.TlfHandle)
}

// NewKeyGenerationError indicates that the data at the given path has
// been written using keys that our client doesn't have.
type IFCERFTNewKeyGenerationError struct {
	TlfHandle *IFCERFTTlfHandle
	KeyGen    IFCERFTKeyGen
}

// Error implements the error interface for NewKeyGenerationError.
func (e IFCERFTNewKeyGenerationError) Error() string {
	return fmt.Sprintf(
		"The data for %v is keyed with a key generation (%d) that "+
			"we don't know", e.TlfHandle, e.KeyGen)
}

// BadSplitError indicates that the BlockSplitter has an error.
type IFCERFTBadSplitError struct {
}

// Error implements the error interface for BadSplitError
func (e IFCERFTBadSplitError) Error() string {
	return "Unexpected bad block split"
}

// TooLowByteCountError indicates that size of a block is smaller than
// the expected size.
type IFCERFTTooLowByteCountError struct {
	ExpectedMinByteCount int
	ByteCount            int
}

// Error implements the error interface for TooLowByteCountError
func (e IFCERFTTooLowByteCountError) Error() string {
	return fmt.Sprintf("Expected at least %d bytes, got %d bytes",
		e.ExpectedMinByteCount, e.ByteCount)
}

// InconsistentEncodedSizeError is raised when a dirty block has a
// non-zero encoded size.
type IFCERFTInconsistentEncodedSizeError struct {
	info IFCERFTBlockInfo
}

// Error implements the error interface for InconsistentEncodedSizeError
func (e IFCERFTInconsistentEncodedSizeError) Error() string {
	return fmt.Sprintf("Block pointer to dirty block %v with non-zero "+
		"encoded size = %d bytes", e.info.ID, e.info.EncodedSize)
}

// MDWriteNeededInRequest indicates that the system needs MD write
// permissions to successfully complete an operation, so it should
// retry in mdWrite mode.
type IFCERFTMDWriteNeededInRequest struct {
}

// Error implements the error interface for MDWriteNeededInRequest
func (e IFCERFTMDWriteNeededInRequest) Error() string {
	return "This request needs MD write access, but doesn't have it."
}

// UnknownSigVer indicates that we can't process a signature because
// it has an unknown version.
type IFCERFTUnknownSigVer struct {
	sigVer IFCERFTSigVer
}

// Error implements the error interface for UnknownSigVer
func (e IFCERFTUnknownSigVer) Error() string {
	return fmt.Sprintf("Unknown signature version %d", int(e.sigVer))
}

// TLFEphemeralPublicKeyNotFoundError indicates that an ephemeral
// public key matching the user and device KID couldn't be found.
type IFCERFTTLFEphemeralPublicKeyNotFoundError struct {
	uid keybase1.UID
	kid keybase1.KID
}

// Error implements the error interface for TLFEphemeralPublicKeyNotFoundError.
func (e IFCERFTTLFEphemeralPublicKeyNotFoundError) Error() string {
	return fmt.Sprintf("Could not find ephemeral public key for "+
		"user %s, device KID %v", e.uid, e.kid)
}

// KeyNotFoundError indicates that a key matching the given KID
// couldn't be found.
type IFCERFTKeyNotFoundError struct {
	kid keybase1.KID
}

// Error implements the error interface for KeyNotFoundError.
func (e IFCERFTKeyNotFoundError) Error() string {
	return fmt.Sprintf("Could not find key with kid=%s", e.kid)
}

// UnverifiableTlfUpdateError indicates that a MD update could not be
// verified.
type IFCERFTUnverifiableTlfUpdateError struct {
	Tlf  string
	User libkb.NormalizedUsername
	Err  error
}

// Error implements the error interface for UnverifiableTlfUpdateError.
func (e IFCERFTUnverifiableTlfUpdateError) Error() string {
	return fmt.Sprintf("%s was last written by an unknown device claiming "+
		"to belong to user %s.  The device has possibly been revoked by the "+
		"user.  Use `keybase log send` to file an issue with the Keybase "+
		"admins.", e.Tlf, e.User)
}

// KeyCacheMissError indicates that a key matching the given TlfID
// and key generation wasn't found in cache.
type IFCERFTKeyCacheMissError struct {
	tlf    IFCERFTTlfID
	keyGen IFCERFTKeyGen
}

// Error implements the error interface for KeyCacheMissError.
func (e IFCERFTKeyCacheMissError) Error() string {
	return fmt.Sprintf("Could not find key with tlf=%s, keyGen=%d", e.tlf, e.keyGen)
}

// KeyCacheHitError indicates that a key matching the given TlfID
// and key generation was found in cache but the object type was unknown.
type IFCERFTKeyCacheHitError struct {
	tlf    IFCERFTTlfID
	keyGen IFCERFTKeyGen
}

// Error implements the error interface for KeyCacheHitError.
func (e IFCERFTKeyCacheHitError) Error() string {
	return fmt.Sprintf("Invalid key with tlf=%s, keyGen=%d", e.tlf, e.keyGen)
}

// UnexpectedShortCryptoRandRead indicates that fewer bytes were read
// from crypto.rand.Read() than expected.
type IFCERFTUnexpectedShortCryptoRandRead struct {
}

// Error implements the error interface for UnexpectedShortRandRead.
func (e IFCERFTUnexpectedShortCryptoRandRead) Error() string {
	return "Unexpected short read from crypto.rand.Read()"
}

// UnknownEncryptionVer indicates that we can't decrypt an
// encryptedData object because it has an unknown version.
type IFCERFTUnknownEncryptionVer struct {
	ver IFCERFTEncryptionVer
}

// Error implements the error interface for UnknownEncryptionVer.
func (e IFCERFTUnknownEncryptionVer) Error() string {
	return fmt.Sprintf("Unknown encryption version %d", int(e.ver))
}

// InvalidNonceError indicates that an invalid cryptographic nonce was
// detected.
type IFCERFTInvalidNonceError struct {
	nonce []byte
}

// Error implements the error interface for InvalidNonceError.
func (e IFCERFTInvalidNonceError) Error() string {
	return fmt.Sprintf("Invalid nonce %v", e.nonce)
}

// NoKeysError indicates that no keys were provided for a decryption allowing
// multiple device keys
type IFCERFTNoKeysError struct{}

func (e IFCERFTNoKeysError) Error() string {
	return "No keys provided"
}

// InvalidPublicTLFOperation indicates that an invalid operation was
// attempted on a public TLF.
type IFCERFTInvalidPublicTLFOperation struct {
	id     IFCERFTTlfID
	opName string
}

// Error implements the error interface for InvalidPublicTLFOperation.
func (e IFCERFTInvalidPublicTLFOperation) Error() string {
	return fmt.Sprintf("Tried to do invalid operation %s on public TLF %v",
		e.opName, e.id)
}

// WrongOpsError indicates that an unexpected path got passed into a
// FolderBranchOps instance
type IFCERFTWrongOpsError struct {
	nodeFB IFCERFTFolderBranch
	opsFB  IFCERFTFolderBranch
}

// Error implements the error interface for WrongOpsError.
func (e IFCERFTWrongOpsError) Error() string {
	return fmt.Sprintf("Ops for folder %v, branch %s, was given path %s, "+
		"branch %s", e.opsFB.Tlf, e.opsFB.Branch, e.nodeFB.Tlf, e.nodeFB.Branch)
}

// NodeNotFoundError indicates that we tried to find a node for the
// given BlockPointer and failed.
type IFCERFTNodeNotFoundError struct {
	ptr IFCERFTBlockPointer
}

// Error implements the error interface for NodeNotFoundError.
func (e IFCERFTNodeNotFoundError) Error() string {
	return fmt.Sprintf("No node found for pointer %v", e.ptr)
}

// ParentNodeNotFoundError indicates that we tried to update a Node's
// parent with a BlockPointer that we don't yet know about.
type IFCERFTParentNodeNotFoundError struct {
	parent IFCERFTBlockRef
}

// Error implements the error interface for ParentNodeNotFoundError.
func (e IFCERFTParentNodeNotFoundError) Error() string {
	return fmt.Sprintf("No such parent node found for %v", e.parent)
}

// EmptyNameError indicates that the user tried to use an empty name
// for the given blockRef.
type IFCERFTEmptyNameError struct {
	ref IFCERFTBlockRef
}

// Error implements the error interface for EmptyNameError.
func (e IFCERFTEmptyNameError) Error() string {
	return fmt.Sprintf("Cannot use empty name for %v", e.ref)
}

// PaddedBlockReadError occurs if the number of bytes read do not
// equal the number of bytes specified.
type IFCERFTPaddedBlockReadError struct {
	ActualLen   int
	ExpectedLen int
}

// Error implements the error interface of PaddedBlockReadError.
func (e IFCERFTPaddedBlockReadError) Error() string {
	return fmt.Sprintf("Reading block data out of padded block resulted in %d bytes, expected %d",
		e.ActualLen, e.ExpectedLen)
}

// NotDirectFileBlockError indicates that a direct file block was
// expected, but something else (e.g., an indirect file block) was
// given instead.
type IFCERFTNotDirectFileBlockError struct {
}

func (e IFCERFTNotDirectFileBlockError) Error() string {
	return fmt.Sprintf("Unexpected block type; expected a direct file block")
}

// KeyHalfMismatchError is returned when the key server doesn't return the expected key half.
type IFCERFTKeyHalfMismatchError struct {
	Expected IFCERFTTLFCryptKeyServerHalfID
	Actual   IFCERFTTLFCryptKeyServerHalfID
}

// Error implements the error interface for KeyHalfMismatchError.
func (e IFCERFTKeyHalfMismatchError) Error() string {
	return fmt.Sprintf("Key mismatch, expected ID: %s, actual ID: %s",
		e.Expected, e.Actual)
}

// InvalidHashError is returned whenever an invalid hash is
// detected.
type IFCERFTInvalidHashError struct {
	H IFCERFTHash
}

func (e IFCERFTInvalidHashError) Error() string {
	return fmt.Sprintf("Invalid hash %s", e.H)
}

// InvalidTlfID indicates whether the TLF ID string is not parseable
// or invalid.
type IFCERFTInvalidTlfID struct {
	id string
}

func (e IFCERFTInvalidTlfID) Error() string {
	return fmt.Sprintf("Invalid TLF ID %q", e.id)
}

// UnknownHashTypeError is returned whenever a hash with an unknown
// hash type is attempted to be used for verification.
type IFCERFTUnknownHashTypeError struct {
	T IFCERFTHashType
}

func (e IFCERFTUnknownHashTypeError) Error() string {
	return fmt.Sprintf("Unknown hash type %s", e.T)
}

// HashMismatchError is returned whenever a hash mismatch is detected.
type IFCERFTHashMismatchError struct {
	ExpectedH IFCERFTHash
	ActualH   IFCERFTHash
}

func (e IFCERFTHashMismatchError) Error() string {
	return fmt.Sprintf("Hash mismatch: expected %s, got %s",
		e.ExpectedH, e.ActualH)
}

// MDServerDisconnected indicates the MDServer has been disconnected for clients waiting
// on an update channel.
type IFCERFTMDServerDisconnected struct {
}

// Error implements the error interface for MDServerDisconnected.
func (e IFCERFTMDServerDisconnected) Error() string {
	return "MDServer is disconnected"
}

// MDRevisionMismatch indicates that we tried to apply a revision that
// was not the next in line.
type IFCERFTMDRevisionMismatch struct {
	rev  IFCERFTMetadataRevision
	curr IFCERFTMetadataRevision
}

// Error implements the error interface for MDRevisionMismatch.
func (e IFCERFTMDRevisionMismatch) Error() string {
	return fmt.Sprintf("MD revision %d isn't next in line for our "+
		"current revision %d", e.rev, e.curr)
}

// MDTlfIDMismatch indicates that the ID field of a successor MD
// doesn't match the ID field of its predecessor.
type IFCERFTMDTlfIDMismatch struct {
	currID IFCERFTTlfID
	nextID IFCERFTTlfID
}

func (e IFCERFTMDTlfIDMismatch) Error() string {
	return fmt.Sprintf("TLF ID %s doesn't match successor TLF ID %s",
		e.currID, e.nextID)
}

// MDPrevRootMismatch indicates that the PrevRoot field of a successor
// MD doesn't match the metadata ID of its predecessor.
type IFCERFTMDPrevRootMismatch struct {
	prevRoot IFCERFTMdID
	currRoot IFCERFTMdID
}

func (e IFCERFTMDPrevRootMismatch) Error() string {
	return fmt.Sprintf("PrevRoot %s doesn't match current root %s",
		e.prevRoot, e.currRoot)
}

// MDDiskUsageMismatch indicates an inconsistency in the DiskUsage
// field of a RootMetadata object.
type IFCERFTMDDiskUsageMismatch struct {
	expectedDiskUsage uint64
	actualDiskUsage   uint64
}

func (e IFCERFTMDDiskUsageMismatch) Error() string {
	return fmt.Sprintf("Disk usage %d doesn't match expected %d",
		e.actualDiskUsage, e.expectedDiskUsage)
}

// MDUpdateInvertError indicates that we tried to apply a revision that
// was not the next in line.
type IFCERFTMDUpdateInvertError struct {
	rev  IFCERFTMetadataRevision
	curr IFCERFTMetadataRevision
}

// Error implements the error interface for MDUpdateInvertError.
func (e IFCERFTMDUpdateInvertError) Error() string {
	return fmt.Sprintf("MD revision %d isn't next in line for our "+
		"current revision %d while inverting", e.rev, e.curr)
}

// NotPermittedWhileDirtyError indicates that some operation failed
// because of outstanding dirty files, and may be retried later.
type IFCERFTNotPermittedWhileDirtyError struct {
}

// Error implements the error interface for NotPermittedWhileDirtyError.
func (e IFCERFTNotPermittedWhileDirtyError) Error() string {
	return "Not permitted while writes are dirty"
}

// NoChainFoundError indicates that a conflict resolution chain
// corresponding to the given pointer could not be found.
type IFCERFTNoChainFoundError struct {
	ptr IFCERFTBlockPointer
}

// Error implements the error interface for NoChainFoundError.
func (e IFCERFTNoChainFoundError) Error() string {
	return fmt.Sprintf("No chain found for %v", e.ptr)
}

// DisallowedPrefixError indicates that the user attempted to create
// an entry using a name with a disallowed prefix.
type IFCERFTDisallowedPrefixError struct {
	name   string
	prefix string
}

// Error implements the error interface for NoChainFoundError.
func (e IFCERFTDisallowedPrefixError) Error() string {
	return fmt.Sprintf("Cannot create %s because it has the prefix %s",
		e.name, e.prefix)
}

// FileTooBigError indicates that the user tried to write a file that
// would be bigger than KBFS's supported size.
type IFCERFTFileTooBigError struct {
	p               IFCERFTPath
	size            int64
	maxAllowedBytes uint64
}

// Error implements the error interface for FileTooBigError.
func (e IFCERFTFileTooBigError) Error() string {
	return fmt.Sprintf("File %s would have increased to %d bytes, which is "+
		"over the supported limit of %d bytes", e.p, e.size, e.maxAllowedBytes)
}

// NameTooLongError indicates that the user tried to write a directory
// entry name that would be bigger than KBFS's supported size.
type IFCERFTNameTooLongError struct {
	name            string
	maxAllowedBytes uint32
}

// Error implements the error interface for NameTooLongError.
func (e IFCERFTNameTooLongError) Error() string {
	return fmt.Sprintf("New directory entry name %s has more than the maximum "+
		"allowed number of bytes (%d)", e.name, e.maxAllowedBytes)
}

// DirTooBigError indicates that the user tried to write a directory
// that would be bigger than KBFS's supported size.
type IFCERFTDirTooBigError struct {
	p               IFCERFTPath
	size            uint64
	maxAllowedBytes uint64
}

// Error implements the error interface for DirTooBigError.
func (e IFCERFTDirTooBigError) Error() string {
	return fmt.Sprintf("Directory %s would have increased to at least %d "+
		"bytes, which is over the supported limit of %d bytes", e.p,
		e.size, e.maxAllowedBytes)
}

// TlfNameNotCanonical indicates that a name isn't a canonical, and
// that another (not necessarily canonical) name should be tried.
type IFCERFTTlfNameNotCanonical struct {
	Name, NameToTry string
}

func (e IFCERFTTlfNameNotCanonical) Error() string {
	return fmt.Sprintf("TLF name %s isn't canonical: try %s instead",
		e.Name, e.NameToTry)
}

// NoCurrentSessionError indicates that the daemon has no current
// session.  This is basically a wrapper for session.ErrNoSession,
// needed to give the correct return error code to the OS.
type IFCERFTNoCurrentSessionError struct {
}

// Error implements the error interface for NoCurrentSessionError.
func (e IFCERFTNoCurrentSessionError) Error() string {
	return "You are not logged into Keybase.  Try `keybase login`."
}

// NoCurrentSessionExpectedError is the error text that will get
// converted into a NoCurrentSessionError.
var IFCERFTNoCurrentSessionExpectedError = "no current session"

// RekeyPermissionError indicates that the user tried to rekey a
// top-level folder in a manner inconsistent with their permissions.
type IFCERFTRekeyPermissionError struct {
	User libkb.NormalizedUsername
	Dir  string
}

// Error implements the error interface for RekeyPermissionError
func (e IFCERFTRekeyPermissionError) Error() string {
	return fmt.Sprintf("%s is trying to rekey directory %s in a manner "+
		"inconsistent with their role", e.User, e.Dir)
}

// NewRekeyPermissionError constructs a RekeyPermissionError for the given
// directory and user.
func IFCERFTNewRekeyPermissionError(
	dir *IFCERFTTlfHandle, username libkb.NormalizedUsername) error {
	dirname := dir.GetCanonicalPath()
	return IFCERFTRekeyPermissionError{username, dirname}
}

// RekeyIncompleteError is returned when a rekey is partially done but
// needs a writer to finish it.
type IFCERFTRekeyIncompleteError struct{}

func (e IFCERFTRekeyIncompleteError) Error() string {
	return fmt.Sprintf("Rekey did not complete due to insufficient user permissions")
}

// InvalidKIDError is returned whenever an invalid KID is detected.
type IFCERFTInvalidKIDError struct {
	kid keybase1.KID
}

func (e IFCERFTInvalidKIDError) Error() string {
	return fmt.Sprintf("Invalid KID %s", e.kid)
}

// InvalidByte32DataError is returned whenever invalid data for a
// 32-byte type is detected.
type IFCERFTInvalidByte32DataError struct {
	data []byte
}

func (e IFCERFTInvalidByte32DataError) Error() string {
	return fmt.Sprintf("Invalid byte32 data %v", e.data)
}

// TimeoutError is just a replacement for context.DeadlineExceeded
// with a more friendly error string.
type IFCERFTTimeoutError struct {
}

func (e IFCERFTTimeoutError) Error() string {
	return "Operation timed out"
}

// InvalidOpError is returned when an operation is called that isn't supported
// by the current implementation.
type IFCERFTInvalidOpError struct {
	op string
}

func (e IFCERFTInvalidOpError) Error() string {
	return fmt.Sprintf("Invalid operation: %s", e.op)
}

// CRAbandonStagedBranchError indicates that conflict resolution had to
// abandon a staged branch due to an unresolvable error.
type IFCERFTCRAbandonStagedBranchError struct {
	Err error
	Bid IFCERFTBranchID
}

func (e IFCERFTCRAbandonStagedBranchError) Error() string {
	return fmt.Sprintf("Abandoning staged branch %s due to an error: %v",
		e.Bid, e.Err)
}

// NoSuchFolderListError indicates that the user tried to access a
// subdirectory of /keybase that doesn't exist.
type IFCERFTNoSuchFolderListError struct {
	Name     string
	PrivName string
	PubName  string
}

// Error implements the error interface for NoSuchFolderListError
func (e IFCERFTNoSuchFolderListError) Error() string {
	return fmt.Sprintf("/keybase/%s is not a Keybase folder.  "+
		"All folders begin with /keybase/%s or /keybase/%s.",
		e.Name, e.PrivName, e.PubName)
}

// UnexpectedUnmergedPutError indicates that we tried to do an
// unmerged put when that was disallowed.
type IFCERFTUnexpectedUnmergedPutError struct {
}

// Error implements the error interface for UnexpectedUnmergedPutError
func (e IFCERFTUnexpectedUnmergedPutError) Error() string {
	return "Unmerged puts are not allowed"
}

// NoSuchTlfHandleError indicates we were unable to resolve a folder
// ID to a folder handle.
type IFCERFTNoSuchTlfHandleError struct {
	ID IFCERFTTlfID
}

// Error implements the error interface for NoSuchTlfHandleError
func (e IFCERFTNoSuchTlfHandleError) Error() string {
	return fmt.Sprintf("Folder handle for %s not found", e.ID)
}

// TlfHandleExtensionMismatchError indicates the expected extension
// doesn't match the server's extension for the given handle.
type IFCERFTTlfHandleExtensionMismatchError struct {
	Expected IFCERFTTlfHandleExtension
	// Actual may be nil.
	Actual *IFCERFTTlfHandleExtension
}

// Error implements the error interface for TlfHandleExtensionMismatchError
func (e IFCERFTTlfHandleExtensionMismatchError) Error() string {
	return fmt.Sprintf("Folder handle extension mismatch, "+
		"expected: %s, actual: %s", e.Expected, e.Actual)
}

// MetadataIsFinalError indicates that we tried to make or set a
// successor to a finalized folder.
type IFCERFTMetadataIsFinalError struct {
}

// Error implements the error interface for MetadataIsFinalError.
func (e IFCERFTMetadataIsFinalError) Error() string {
	return "Metadata is final"
}

// IncompatibleHandleError indicates that somethine tried to update
// the head of a TLF with a RootMetadata with an incompatible handle.
type IFCERFTIncompatibleHandleError struct {
	oldName                  IFCERFTCanonicalTlfName
	partiallyResolvedOldName IFCERFTCanonicalTlfName
	newName                  IFCERFTCanonicalTlfName
}

func (e IFCERFTIncompatibleHandleError) Error() string {
	return fmt.Sprintf(
		"old head %q resolves to %q instead of new head %q",
		e.oldName, e.partiallyResolvedOldName, e.newName)
}

// ShutdownHappenedError indicates that shutdown has happened.
type IFCERFTShutdownHappenedError struct {
}

// Error implements the error interface for ShutdownHappenedError.
func (e IFCERFTShutdownHappenedError) Error() string {
	return "Shutdown happened"
}

// UnmergedError indicates that fbo is on an unmerged local revision
type IFCERFTUnmergedError struct {
}

// Error implements the error interface for UnmergedError.
func (e IFCERFTUnmergedError) Error() string {
	return "fbo is on an unmerged local revision"
}

// EXCLOnUnmergedError happens when an operation with O_EXCL set when fbo is on
// an unmerged local revision
type IFCERFTEXCLOnUnmergedError struct {
}

// Error implements the error interface for EXCLOnUnmergedError.
func (e IFCERFTEXCLOnUnmergedError) Error() string {
	return "an operation with O_EXCL set is called but fbo is on an unmerged local version"
}

// OverQuotaWarning indicates that the user is over their quota, and
// is being slowed down by the server.
type IFCERFTOverQuotaWarning struct {
	UsageBytes int64
	LimitBytes int64
}

// Error implements the error interface for OverQuotaWarning.
func (w IFCERFTOverQuotaWarning) Error() string {
	return fmt.Sprintf("You are using %d bytes, and your plan limits you "+
		"to %d bytes.  Please delete some data.", w.UsageBytes, w.LimitBytes)
}

// OpsCantHandleFavorite means that folderBranchOps wasn't able to
// deal with a favorites request.
type IFCERFTOpsCantHandleFavorite struct {
	Msg string
}

// Error implements the error interface for OpsCantHandleFavorite.
func (e IFCERFTOpsCantHandleFavorite) Error() string {
	return fmt.Sprintf("Couldn't handle the favorite operation: %s", e.Msg)
}
