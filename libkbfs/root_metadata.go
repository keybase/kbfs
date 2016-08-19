// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	keybase1 "github.com/keybase/client/go/protocol"
	"github.com/keybase/go-codec/codec"
)

// MetadataFlags bitfield.
type MetadataFlags byte

// Possible flags set in the MetadataFlags bitfield.
const (
	MetadataFlagRekey MetadataFlags = 1 << iota
	MetadataFlagWriterMetadataCopied
	MetadataFlagFinal
)

// WriterFlags bitfield.
type WriterFlags byte

// Possible flags set in the WriterFlags bitfield.
const (
	MetadataFlagUnmerged WriterFlags = 1 << iota
)

// MetadataRevision is the type for the revision number.
// This is currently int64 since that's the type of Avro's long.
type MetadataRevision int64

// String converts a MetadataRevision to its string form.
func (mr MetadataRevision) String() string {
	return strconv.FormatInt(mr.Number(), 10)
}

// Number casts a MetadataRevision to it's primitive type.
func (mr MetadataRevision) Number() int64 {
	return int64(mr)
}

const (
	// MetadataRevisionUninitialized indicates that a top-level folder has
	// not yet been initialized.
	MetadataRevisionUninitialized = MetadataRevision(0)
	// MetadataRevisionInitial is always the first revision for an
	// initialized top-level folder.
	MetadataRevisionInitial = MetadataRevision(1)
)

// PrivateMetadata contains the portion of metadata that's secret for private
// directories
type PrivateMetadata struct {
	// directory entry for the root directory block
	Dir DirEntry

	// m_f as described in 4.1.1 of https://keybase.io/blog/kbfs-crypto.
	TLFPrivateKey TLFPrivateKey
	// The block changes done as part of the update that created this MD
	Changes BlockChanges

	codec.UnknownFieldSetHandler

	// When the above Changes field gets unembedded into its own
	// block, we may want to temporarily keep around the old
	// BlockChanges for easy reference.
	cachedChanges BlockChanges
}

func (p PrivateMetadata) checkValid() error {
	for i, op := range p.Changes.Ops {
		err := op.checkValid()
		if err != nil {
			return fmt.Errorf("op[%d]=%v invalid: %v", i, op, err)
		}
	}
	return nil
}

// ChangesBlockInfo returns the block info for any unembedded changes.
func (p PrivateMetadata) ChangesBlockInfo() BlockInfo {
	return p.cachedChanges.Info
}

// A RootMetadata is a BareRootMetadata but with a deserialized
// PrivateMetadata. However, note that it is possible that the
// PrivateMetadata has to be left serialized due to not having the
// right keys.
type RootMetadata struct {
	bareMd MutableBareRootMetadata

	// The plaintext, deserialized PrivateMetadata
	//
	// TODO: This should really be a pointer so that it's more
	// clear when the data has been successfully deserialized.
	data PrivateMetadata

	// The TLF handle for this MD. May be nil if this object was
	// deserialized (more common on the server side).
	tlfHandle *TlfHandle
}

var _ KeyMetadata = (*RootMetadata)(nil)

// NewRootMetadata returns a new RootMetadata object at the latest known version.
func NewRootMetadata() *RootMetadata {
	return &RootMetadata{bareMd: &BareRootMetadataV2{}}
}

// Data returns the private metadata of this RootMetadata.
func (md *RootMetadata) Data() *PrivateMetadata {
	return &md.data
}

// IsReadable returns true if the private metadata can be read.
func (md *RootMetadata) IsReadable() bool {
	return md.TlfID().IsPublic() || md.data.Dir.IsInitialized()
}

func (md *RootMetadata) clearLastRevision() {
	md.ClearBlockChanges()
	// remove the copied flag (if any.)
	md.clearWriterMetadataCopiedBit()
}

func (md *RootMetadata) deepCopy(codec Codec, copyHandle bool) (*RootMetadata, error) {
	var newMd RootMetadata
	if err := md.deepCopyInPlace(codec, copyHandle, &newMd); err != nil {
		return nil, err
	}
	return &newMd, nil
}

func (md *RootMetadata) deepCopyInPlace(codec Codec, copyHandle bool,
	newMd *RootMetadata) error {
	if err := CodecUpdate(codec, newMd, md); err != nil {
		return err
	}
	if err := CodecUpdate(codec, &newMd.data, md.data); err != nil {
		return err
	}

	brmdCopy, err := md.bareMd.DeepCopy(codec)
	if err != nil {
		return err
	}

	mutableBrmdCopy, ok := brmdCopy.(MutableBareRootMetadata)
	if !ok {
		return MutableBareRootMetadataNoImplError{}
	}
	newMd.bareMd = mutableBrmdCopy

	if copyHandle {
		newMd.tlfHandle = md.tlfHandle.deepCopy()
	}

	// No need to copy mdID.

	return nil
}

// MakeSuccessor returns a complete copy of this RootMetadata (but
// with cleared block change lists and cleared serialized metadata),
// with the revision incremented and a correct backpointer.
func (md *RootMetadata) MakeSuccessor(
	config Config, mdID MdID, isWriter bool) (*RootMetadata, error) {
	if mdID == (MdID{}) {
		return nil, errors.New("Empty MdID in MakeSuccessor")
	}
	if md.IsFinal() {
		return nil, MetadataIsFinalError{}
	}
	newMd, err := md.deepCopy(config.Codec(), true)
	if err != nil {
		return nil, err
	}

	if md.IsReadable() && isWriter {
		newMd.clearLastRevision()
		// clear the serialized data.
		newMd.SetSerializedPrivateMetadata(nil)
	} else {
		// if we can't read it it means we're simply setting the rekey bit
		// and copying the previous data.
		newMd.SetRekeyBit()
		newMd.SetWriterMetadataCopiedBit()
	}

	newMd.SetPrevRoot(mdID)
	// bump revision
	if md.Revision() < MetadataRevisionInitial {
		return nil, errors.New("MD with invalid revision")
	}
	newMd.SetRevision(md.Revision() + 1)
	return newMd, nil
}

// AddNewKeys makes a new key generation for this RootMetadata using the
// given TLF key bundles.
func (md *RootMetadata) AddNewKeys(
	wkb TLFWriterKeyBundle, rkb TLFReaderKeyBundle) error {
	if md.TlfID().IsPublic() {
		return InvalidPublicTLFOperation{md.TlfID(), "AddNewKeys"}
	}
	md.bareMd.AddNewKeys(wkb, rkb)
	return nil
}

// GetTlfHandle returns the TlfHandle for this RootMetadata.
func (md *RootMetadata) GetTlfHandle() *TlfHandle {
	if md.tlfHandle == nil {
		panic(fmt.Sprintf("RootMetadata %v with no handle", md))
	}

	return md.tlfHandle
}

// MakeBareTlfHandle makes a BareTlfHandle for this
// RootMetadata. Should be used only by servers and MDOps.
func (md *RootMetadata) MakeBareTlfHandle() (BareTlfHandle, error) {
	if md.tlfHandle != nil {
		panic(errors.New("MakeBareTlfHandle called when md.tlfHandle exists"))
	}

	return md.bareMd.MakeBareTlfHandle()
}

// IsInitialized returns whether or not this RootMetadata has been initialized
func (md *RootMetadata) IsInitialized() bool {
	keyGen := md.LatestKeyGeneration()
	if md.TlfID().IsPublic() {
		return keyGen == PublicKeyGen
	}
	// The data is only initialized once we have at least one set of keys
	return keyGen >= FirstValidKeyGen
}

// AddRefBlock adds the newly-referenced block to the add block change list.
func (md *RootMetadata) AddRefBlock(info BlockInfo) {
	md.AddRefBytes(uint64(info.EncodedSize))
	md.AddDiskUsage(uint64(info.EncodedSize))
	md.data.Changes.AddRefBlock(info.BlockPointer)
}

// AddUnrefBlock adds the newly-unreferenced block to the add block change list.
func (md *RootMetadata) AddUnrefBlock(info BlockInfo) {
	if info.EncodedSize > 0 {
		md.AddUnrefBytes(uint64(info.EncodedSize))
		md.SetDiskUsage(md.DiskUsage() - uint64(info.EncodedSize))
		md.data.Changes.AddUnrefBlock(info.BlockPointer)
	}
}

// AddUpdate adds the newly-updated block to the add block change list.
func (md *RootMetadata) AddUpdate(oldInfo BlockInfo, newInfo BlockInfo) {
	if oldInfo.EncodedSize > 0 {
		md.AddUnrefBytes(uint64(oldInfo.EncodedSize))
		md.AddRefBytes(uint64(newInfo.EncodedSize))
		md.AddDiskUsage(uint64(newInfo.EncodedSize))
		md.SetDiskUsage(md.DiskUsage() - uint64(oldInfo.EncodedSize))
		md.data.Changes.AddUpdate(oldInfo.BlockPointer, newInfo.BlockPointer)
	}
}

// AddOp starts a new operation for this MD update.  Subsequent
// AddRefBlock, AddUnrefBlock, and AddUpdate calls will be applied to
// this operation.
func (md *RootMetadata) AddOp(o op) {
	md.data.Changes.AddOp(o)
}

// ClearBlockChanges resets the block change lists to empty for this
// RootMetadata.
func (md *RootMetadata) ClearBlockChanges() {
	md.SetRefBytes(0)
	md.SetUnrefBytes(0)
	md.data.Changes.sizeEstimate = 0
	md.data.Changes.Info = BlockInfo{}
	md.data.Changes.Ops = nil
}

// updateFromTlfHandle updates the current RootMetadata's fields to
// reflect the given handle, which must be the result of running the
// current handle with ResolveAgain().
func (md *RootMetadata) updateFromTlfHandle(newHandle *TlfHandle) error {
	// TODO: Strengthen check, e.g. make sure every writer/reader
	// in the old handle is also a writer/reader of the new
	// handle.
	if md.TlfID().IsPublic() != newHandle.IsPublic() {
		return fmt.Errorf(
			"Trying to update public=%t rmd with public=%t handle",
			md.TlfID().IsPublic(), newHandle.IsPublic())
	}

	if newHandle.IsPublic() {
		md.SetWriters(newHandle.ResolvedWriters())
	} else {
		md.SetUnresolvedReaders(newHandle.UnresolvedReaders())
	}

	md.SetUnresolvedWriters(newHandle.UnresolvedWriters())
	md.SetConflictInfo(newHandle.ConflictInfo())
	md.SetFinalizedInfo(newHandle.FinalizedInfo())

	bareHandle, err := md.bareMd.MakeBareTlfHandle()
	if err != nil {
		return err
	}

	newBareHandle, err := newHandle.ToBareHandle()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(bareHandle, newBareHandle) {
		return fmt.Errorf(
			"bareHandle=%+v != newBareHandle=%+v",
			bareHandle, newBareHandle)
	}

	md.tlfHandle = newHandle
	return nil
}

// swapCachedBlockChanges swaps any cached block changes so that
// future local accesses to this MD (from the cache) can directly
// access the ops without needing to re-embed the block changes.
func (md *RootMetadata) swapCachedBlockChanges() {
	if md.data.Changes.Ops == nil {
		md.data.Changes, md.data.cachedChanges =
			md.data.cachedChanges, md.data.Changes
		md.data.Changes.Ops[0].
			AddRefBlock(md.data.cachedChanges.Info.BlockPointer)
	}
}

// GetTLFCryptKeyParams wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) GetTLFCryptKeyParams(
	keyGen KeyGen, user keybase1.UID, key CryptPublicKey) (
	TLFEphemeralPublicKey, EncryptedTLFCryptKeyClientHalf,
	TLFCryptKeyServerHalfID, bool, error) {
	return md.bareMd.GetTLFCryptKeyParams(keyGen, user, key)
}

// LatestKeyGeneration wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) LatestKeyGeneration() KeyGen {
	return md.bareMd.LatestKeyGeneration()
}

// TlfID wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) TlfID() TlfID {
	return md.bareMd.TlfID()
}

// LastModifyingWriterKID wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) LastModifyingWriterKID() keybase1.KID {
	return md.bareMd.LastModifyingWriterKID()
}

// LastModifyingWriter wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) LastModifyingWriter() keybase1.UID {
	return md.bareMd.LastModifyingWriter()
}

// LastModifyingUser wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) LastModifyingUser() keybase1.UID {
	return md.bareMd.GetLastModifyingUser()
}

// RefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) RefBytes() uint64 {
	return md.bareMd.RefBytes()
}

// UnrefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) UnrefBytes() uint64 {
	return md.bareMd.UnrefBytes()
}

// DiskUsage wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) DiskUsage() uint64 {
	return md.bareMd.DiskUsage()
}

// SetRefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetRefBytes(refBytes uint64) {
	md.bareMd.SetRefBytes(refBytes)
}

// SetUnrefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetUnrefBytes(unrefBytes uint64) {
	md.bareMd.SetUnrefBytes(unrefBytes)
}

// SetDiskUsage wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetDiskUsage(diskUsage uint64) {
	md.bareMd.SetDiskUsage(diskUsage)
}

// AddRefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) AddRefBytes(refBytes uint64) {
	md.bareMd.AddRefBytes(refBytes)
}

// AddUnrefBytes wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) AddUnrefBytes(unrefBytes uint64) {
	md.bareMd.AddUnrefBytes(unrefBytes)
}

// AddDiskUsage wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) AddDiskUsage(diskUsage uint64) {
	md.bareMd.AddDiskUsage(diskUsage)
}

// IsWriterMetadataCopiedSet wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) IsWriterMetadataCopiedSet() bool {
	return md.bareMd.IsWriterMetadataCopiedSet()
}

// IsRekeySet wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) IsRekeySet() bool {
	return md.bareMd.IsRekeySet()
}

// IsUnmergedSet wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) IsUnmergedSet() bool {
	return md.bareMd.IsUnmergedSet()
}

// Revision wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) Revision() MetadataRevision {
	return md.bareMd.RevisionNumber()
}

// MergedStatus wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) MergedStatus() MergeStatus {
	return md.bareMd.MergedStatus()
}

// BID wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) BID() BranchID {
	return md.bareMd.BID()
}

// PrevRoot wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) PrevRoot() MdID {
	return md.bareMd.GetPrevRoot()
}

func (md *RootMetadata) clearRekeyBit() {
	md.bareMd.ClearRekeyBit()
}

func (md *RootMetadata) clearWriterMetadataCopiedBit() {
	md.bareMd.ClearWriterMetadataCopiedBit()
}

// SetUnmerged wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetUnmerged() {
	md.bareMd.SetUnmerged()
}

// SetBranchID wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetBranchID(bid BranchID) {
	md.bareMd.SetBranchID(bid)
}

// SetPrevRoot wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetPrevRoot(mdID MdID) {
	md.bareMd.SetPrevRoot(mdID)
}

// GetSerializedPrivateMetadata wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) GetSerializedPrivateMetadata() []byte {
	return md.bareMd.GetSerializedPrivateMetadata()
}

// GetSerializedWriterMetadata wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) GetSerializedWriterMetadata(codec Codec) ([]byte, error) {
	return md.bareMd.GetSerializedWriterMetadata(codec)
}

// GetWriterMetadataSigInfo wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) GetWriterMetadataSigInfo() SignatureInfo {
	return md.bareMd.GetWriterMetadataSigInfo()
}

// SetWriterMetadataSigInfo wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetWriterMetadataSigInfo(sigInfo SignatureInfo) {
	md.bareMd.SetWriterMetadataSigInfo(sigInfo)
}

// IsFinal wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) IsFinal() bool {
	return md.bareMd.IsFinal()
}

// SetSerializedPrivateMetadata wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetSerializedPrivateMetadata(spmd []byte) {
	md.bareMd.SetSerializedPrivateMetadata(spmd)
}

// SetRekeyBit wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetRekeyBit() {
	md.bareMd.SetRekeyBit()
}

// SetFinalBit wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetFinalBit() {
	md.bareMd.SetFinalBit()
}

// SetWriterMetadataCopiedBit wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetWriterMetadataCopiedBit() {
	md.bareMd.SetWriterMetadataCopiedBit()
}

// SetRevision wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetRevision(revision MetadataRevision) {
	md.bareMd.SetRevision(revision)
}

// SetWriters wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetWriters(writers []keybase1.UID) {
	md.bareMd.SetWriters(writers)
}

// SetUnresolvedReaders wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetUnresolvedReaders(readers []keybase1.SocialAssertion) {
	md.bareMd.SetUnresolvedReaders(readers)
}

// SetUnresolvedWriters wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetUnresolvedWriters(writers []keybase1.SocialAssertion) {
	md.bareMd.SetUnresolvedWriters(writers)
}

// SetConflictInfo wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetConflictInfo(ci *TlfHandleExtension) {
	md.bareMd.SetConflictInfo(ci)
}

// SetFinalizedInfo wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetFinalizedInfo(fi *TlfHandleExtension) {
	md.bareMd.SetFinalizedInfo(fi)
}

// SetLastModifyingWriter wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetLastModifyingWriter(user keybase1.UID) {
	md.bareMd.SetLastModifyingWriter(user)
}

// SetLastModifyingUser wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetLastModifyingUser(user keybase1.UID) {
	md.bareMd.SetLastModifyingUser(user)
}

// SetTlfID wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) SetTlfID(tlf TlfID) {
	md.bareMd.SetTlfID(tlf)
}

// HasKeyForUser wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) HasKeyForUser(keyGen KeyGen, user keybase1.UID) bool {
	return md.bareMd.HasKeyForUser(keyGen, user)
}

// FakeInitialRekey wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) FakeInitialRekey(h BareTlfHandle) {
	md.bareMd.FakeInitialRekey(h)
}

// Update wraps the respective method of the underlying BareRootMetadata for convenience.
func (md *RootMetadata) Update(id TlfID, h BareTlfHandle) error {
	return md.bareMd.Update(id, h)
}

// GetBareRootMetadata returns an interface to the underlying serializeable metadata.
func (md *RootMetadata) GetBareRootMetadata() BareRootMetadata {
	return md.bareMd
}

// A ReadOnlyRootMetadata is a thin wrapper around a
// *RootMetadata. Functions that take a ReadOnlyRootMetadata parameter
// must not modify it, and therefore code that passes a
// ReadOnlyRootMetadata to a function can assume that it is not
// modified by that function. However, callers that convert a
// *RootMetadata to a ReadOnlyRootMetadata may still modify the
// underlying RootMetadata through the original pointer, so care must
// be taken if a function stores a ReadOnlyRootMetadata object past
// the end of the function, or when a function takes both a
// *RootMetadata and a ReadOnlyRootMetadata (see
// decryptMDPrivateData).
type ReadOnlyRootMetadata struct {
	*RootMetadata
}

// CheckValidSuccessor makes sure the given ReadOnlyRootMetadata is a
// valid successor to the current one, and returns an error otherwise.
func (md ReadOnlyRootMetadata) CheckValidSuccessor(
	currID MdID, nextMd ReadOnlyRootMetadata) error {
	return md.bareMd.CheckValidSuccessor(currID, nextMd.bareMd)
}

// ReadOnly makes a ReadOnlyRootMetadata from the current
// *RootMetadata.
func (md *RootMetadata) ReadOnly() ReadOnlyRootMetadata {
	return ReadOnlyRootMetadata{md}
}

// ImmutableRootMetadata is a thin wrapper around a
// ReadOnlyRootMetadata that takes ownership of it and does not ever
// modify it again. Thus, its MdID can be calculated and
// stored. Unlike ReadOnlyRootMetadata, ImmutableRootMetadata objects
// can be assumed to never alias a (modifiable) *RootMetadata.
type ImmutableRootMetadata struct {
	ReadOnlyRootMetadata
	mdID MdID
	// localTimestamp represents the time at which the MD update was
	// applied at the server, adjusted for the local clock.  So for
	// example, it can be used to show how long ago a particular
	// update happened (e.g., "5 hours ago").  Note that the update
	// time supplied by the server is technically untrusted (i.e., not
	// signed by a writer of the TLF, only provided by the server).
	// If this ImmutableRootMetadata was generated locally and still
	// persists in the journal or in the cache, localTimestamp comes
	// directly from the local clock.
	localTimestamp time.Time
}

// MakeImmutableRootMetadata makes a new ImmutableRootMetadata from
// the given RMD and its corresponding MdID.
func MakeImmutableRootMetadata(
	rmd *RootMetadata, mdID MdID,
	localTimestamp time.Time) ImmutableRootMetadata {
	if mdID == (MdID{}) {
		panic("zero mdID passed to MakeImmutableRootMetadata")
	}
	if localTimestamp == (time.Time{}) {
		panic("zero localTimestamp passed to MakeImmutableRootMetadata")
	}
	return ImmutableRootMetadata{rmd.ReadOnly(), mdID, localTimestamp}
}

// MdID returns the pre-computed MdID of the contained RootMetadata
// object.
func (irmd ImmutableRootMetadata) MdID() MdID {
	return irmd.mdID
}

// RootMetadataSigned is the top-level MD object stored in MD server
type RootMetadataSigned struct {
	// signature over the root metadata by the private signing key
	SigInfo SignatureInfo `codec:",omitempty"`
	// all the metadata
	MD MutableBareRootMetadata
	// When does the server say this MD update was received?  (This is
	// not necessarily trustworthy, just for informational purposes.)
	untrustedServerTimestamp time.Time
}

// NewRootMetadataSigned returns a new RootMetadataSigned object at the latest known version.
func NewRootMetadataSigned() *RootMetadataSigned {
	return &RootMetadataSigned{MD: &BareRootMetadataV2{}}
}

// MerkleHash computes a hash of this RootMetadataSigned object for inclusion
// into the KBFS Merkle tree.
func (rmds *RootMetadataSigned) MerkleHash(config Config) (MerkleHash, error) {
	return config.Crypto().MakeMerkleHash(rmds)
}

// Version returns the metadata version of this MD block, depending on
// which features it uses.
func (rmds *RootMetadataSigned) Version() MetadataVer {
	return rmds.MD.Version()
}

// MakeFinalCopy returns a complete copy of this RootMetadataSigned
// with the revision incremented and the final bit set.
func (rmds *RootMetadataSigned) MakeFinalCopy(config Config) (
	*RootMetadataSigned, error) {
	if rmds.MD.IsFinal() {
		return nil, MetadataIsFinalError{}
	}
	newRmds := RootMetadataSigned{}
	newBareMd, err := rmds.MD.DeepCopy(config.Codec())
	if err != nil {
		return nil, err
	}
	newMutableBareMd, ok := newBareMd.(MutableBareRootMetadata)
	if !ok {
		return nil, MutableBareRootMetadataNoImplError{}
	}
	// Set the bare metadata.
	newRmds.MD = newMutableBareMd
	// Copy the signature.
	newRmds.SigInfo = rmds.SigInfo.deepCopy()
	// Set the final flag.
	newRmds.MD.SetFinalBit()
	// Increment revision but keep the PrevRoot --
	// We want the client to be able to verify the signature by masking out the final
	// bit, decrementing the revision, and nulling out the finalized extension info.
	// This way it can easily tell a server didn't modify anything unexpected when
	// creating the final metadata block. Note that PrevRoot isn't being updated. This
	// is to make verification easier for the client as otherwise it'd need to request
	// the head revision - 1.
	newRmds.MD.SetRevision(rmds.MD.RevisionNumber() + 1)
	return &newRmds, nil
}

// IsValidAndSigned verifies the RootMetadataSigned, checks the root
// signature, and returns an error if a problem was found.
func (rmds *RootMetadataSigned) IsValidAndSigned(
	codec Codec, crypto cryptoPure) error {
	// Optimization -- if the RootMetadata signature is nil, it
	// will fail verification.
	if rmds.SigInfo.IsNil() {
		return errors.New("Missing RootMetadata signature")
	}

	err := rmds.MD.IsValidAndSigned(codec, crypto)
	if err != nil {
		return err
	}

	md := rmds.MD
	if rmds.MD.IsFinal() {
		// Since we're just working with the immediate fields
		// of RootMetadata, we can get away with a shallow
		// copy here.
		mdCopy := rmds.MD

		// Mask out finalized additions.  These are the only
		// things allowed to change in the finalized metadata
		// block.
		mdCopy.SetFinalBit()
		mdCopy.SetRevision(md.RevisionNumber() - 1)
		mdCopy.SetFinalizedInfo(nil)
		md = mdCopy
	}
	// Re-marshal the whole RootMetadata. This is not avoidable
	// without support from ugorji/codec.
	buf, err := codec.Encode(md)
	if err != nil {
		return err
	}

	err = crypto.Verify(buf, rmds.SigInfo)
	if err != nil {
		return fmt.Errorf("Could not verify root metadata: %v", err)
	}

	return nil
}

// IsLastModifiedBy verifies that the RootMetadataSigned is written by
// the given user and device (identified by the KID of the device
// verifying key), and returns an error if not. Should be called only
// after IsValidAndSigned.
func (rmds *RootMetadataSigned) IsLastModifiedBy(
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey) error {
	err := rmds.MD.IsLastModifiedBy(currentUID, currentVerifyingKey)
	if err != nil {
		return err
	}

	if rmds.SigInfo.VerifyingKey != currentVerifyingKey {
		return fmt.Errorf(
			"Last modifier verifying key %v doesn't match current verifying key %v",
			rmds.SigInfo.VerifyingKey, currentVerifyingKey)
	}

	return nil
}

// DecodeRootMetadataSigned deserializes a metaddata block into the specified versioned structure.
func DecodeRootMetadataSigned(codec Codec, tlf TlfID, ver, max MetadataVer, buf []byte) (
	*RootMetadataSigned, error) {
	if ver < FirstValidMetadataVer {
		return nil, InvalidMetadataVersionError{tlf, ver}
	} else if ver > max {
		return nil, NewMetadataVersionError{tlf, ver}
	}
	// For now only v1 & v2 are supported (both by the same BareRootMetadataSignedV2 struct)
	if ver > InitialExtraMetadataVer {
		// Shouldn't be possible at the moment.
		panic("Invalid metadata version")
	}
	var brmds BareRootMetadataSignedV2
	if err := codec.Decode(buf, &brmds); err != nil {
		return nil, err
	}
	return &RootMetadataSigned{
		MD:      &brmds.MD,
		SigInfo: brmds.SigInfo,
	}, nil
}
