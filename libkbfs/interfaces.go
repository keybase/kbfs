// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

type resolver interface {
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
}

type identifier interface {
	// Identify resolves an assertion (which could also be a
	// username) to a UserInfo struct, spawning tracker popups if
	// necessary.  The reason string is displayed on any tracker
	// popups spawned.
	Identify(ctx context.Context, assertion, reason string) (IFCERFTUserInfo, error)
}

type normalizedUsernameGetter interface {
	// GetNormalizedUsername returns the normalized username
	// corresponding to the given UID.
	GetNormalizedUsername(ctx context.Context, uid keybase1.UID) (libkb.NormalizedUsername, error)
}

// cryptoPure contains all methods of Crypto that don't depend on
// implicit state, i.e. they're pure functions of the input.
type cryptoPure interface {
	// MakeRandomTlfID generates a dir ID using a CSPRNG.
	MakeRandomTlfID(isPublic bool) (IFCERFTTlfID, error)

	// MakeRandomBranchID generates a per-device branch ID using a CSPRNG.
	MakeRandomBranchID() (BranchID, error)

	// MakeMdID computes the MD ID of a RootMetadata object.
	MakeMdID(md *IFCERFTRootMetadata) (MdID, error)

	// MakeMerkleHash computes the hash of a RootMetadataSigned object
	// for inclusion into the KBFS Merkle tree.
	MakeMerkleHash(md *RootMetadataSigned) (MerkleHash, error)

	// MakeTemporaryBlockID generates a temporary block ID using a
	// CSPRNG. This is used for indirect blocks before they're
	// committed to the server.
	MakeTemporaryBlockID() (BlockID, error)

	// MakePermanentBlockID computes the permanent ID of a block
	// given its encoded and encrypted contents.
	MakePermanentBlockID(encodedEncryptedData []byte) (BlockID, error)

	// VerifyBlockID verifies that the given block ID is the
	// permanent block ID for the given encoded and encrypted
	// data.
	VerifyBlockID(encodedEncryptedData []byte, id BlockID) error

	// MakeRefNonce generates a block reference nonce using a
	// CSPRNG. This is used for distinguishing different references to
	// the same BlockID.
	MakeBlockRefNonce() (IFCERFTBlockRefNonce, error)

	// MakeRandomTLFKeys generates top-level folder keys using a CSPRNG.
	MakeRandomTLFKeys() (TLFPublicKey, TLFPrivateKey, IFCERFTTLFEphemeralPublicKey, TLFEphemeralPrivateKey, IFCERFTTLFCryptKey, error)
	// MakeRandomTLFCryptKeyServerHalf generates the server-side of a
	// top-level folder crypt key.
	MakeRandomTLFCryptKeyServerHalf() (TLFCryptKeyServerHalf, error)
	// MakeRandomBlockCryptKeyServerHalf generates the server-side of
	// a block crypt key.
	MakeRandomBlockCryptKeyServerHalf() (IFCERFTBlockCryptKeyServerHalf, error)

	// MaskTLFCryptKey returns the client-side of a top-level folder crypt key.
	MaskTLFCryptKey(serverHalf TLFCryptKeyServerHalf, key IFCERFTTLFCryptKey) (
		TLFCryptKeyClientHalf, error)
	// UnmaskTLFCryptKey returns the top-level folder crypt key.
	UnmaskTLFCryptKey(serverHalf TLFCryptKeyServerHalf,
		clientHalf TLFCryptKeyClientHalf) (IFCERFTTLFCryptKey, error)
	// UnmaskBlockCryptKey returns the block crypt key.
	UnmaskBlockCryptKey(serverHalf IFCERFTBlockCryptKeyServerHalf, tlfCryptKey IFCERFTTLFCryptKey) (BlockCryptKey, error)

	// Verify verifies that sig matches msg being signed with the
	// private key that corresponds to verifyingKey.
	Verify(msg []byte, sigInfo IFCERFTSignatureInfo) error

	// EncryptTLFCryptKeyClientHalf encrypts a TLFCryptKeyClientHalf
	// using both a TLF's ephemeral private key and a device pubkey.
	EncryptTLFCryptKeyClientHalf(privateKey TLFEphemeralPrivateKey,
		publicKey IFCERFTCryptPublicKey, clientHalf TLFCryptKeyClientHalf) (
		IFCERFTEncryptedTLFCryptKeyClientHalf, error)

	// EncryptPrivateMetadata encrypts a PrivateMetadata object.
	EncryptPrivateMetadata(pmd *PrivateMetadata, key IFCERFTTLFCryptKey) (IFCERFTEncryptedPrivateMetadata, error)
	// DecryptPrivateMetadata decrypts a PrivateMetadata object.
	DecryptPrivateMetadata(encryptedPMD IFCERFTEncryptedPrivateMetadata, key IFCERFTTLFCryptKey) (*PrivateMetadata, error)

	// EncryptBlocks encrypts a block. plainSize is the size of the encoded
	// block; EncryptBlock() must guarantee that plainSize <=
	// len(encryptedBlock).
	EncryptBlock(block IFCERFTBlock, key BlockCryptKey) (
		plainSize int, encryptedBlock IFCERFTEncryptedBlock, err error)

	// DecryptBlock decrypts a block. Similar to EncryptBlock(),
	// DecryptBlock() must guarantee that (size of the decrypted
	// block) <= len(encryptedBlock).
	DecryptBlock(encryptedBlock IFCERFTEncryptedBlock, key BlockCryptKey, block IFCERFTBlock) error

	// GetTLFCryptKeyServerHalfID creates a unique ID for this particular
	// TLFCryptKeyServerHalf.
	GetTLFCryptKeyServerHalfID(
		user keybase1.UID, deviceKID keybase1.KID,
		serverHalf TLFCryptKeyServerHalf) (TLFCryptKeyServerHalfID, error)

	// VerifyTLFCryptKeyServerHalfID verifies the ID is the proper HMAC result.
	VerifyTLFCryptKeyServerHalfID(serverHalfID TLFCryptKeyServerHalfID, user keybase1.UID,
		deviceKID keybase1.KID, serverHalf TLFCryptKeyServerHalf) error

	// EncryptMerkleLeaf encrypts a Merkle leaf node with the TLFPublicKey.
	EncryptMerkleLeaf(leaf MerkleLeaf, pubKey TLFPublicKey, nonce *[24]byte,
		ePrivKey TLFEphemeralPrivateKey) (IFCERFTEncryptedMerkleLeaf, error)

	// DecryptMerkleLeaf decrypts a Merkle leaf node with the TLFPrivateKey.
	DecryptMerkleLeaf(encryptedLeaf IFCERFTEncryptedMerkleLeaf, privKey TLFPrivateKey,
		nonce *[24]byte, ePubKey IFCERFTTLFEphemeralPublicKey) (*MerkleLeaf, error)
}

type mdServerLocal interface {
	IFCERFTMDServer
	addNewAssertionForTest(
		uid keybase1.UID, newAssertion keybase1.SocialAssertion) error
	getCurrentMergedHeadRevision(ctx context.Context, id IFCERFTTlfID) (
		rev MetadataRevision, err error)
	isShutdown() bool
	copy(config IFCERFTConfig) mdServerLocal
}

type blockRefLocalStatus int

const (
	liveBlockRef     blockRefLocalStatus = 1
	archivedBlockRef                     = 2
)

// blockServerLocal is the interface for BlockServer implementations
// that store data locally.
type blockServerLocal interface {
	IFCERFTBlockServer
	// getAll returns all the known block references, and should only be
	// used during testing.
	getAll(tlfID IFCERFTTlfID) (map[BlockID]map[IFCERFTBlockRefNonce]blockRefLocalStatus, error)
}

// fileBlockDeepCopier fetches a file block, makes a deep copy of it
// (duplicating pointer for any indirect blocks) and generates a new
// random temporary block ID for it.  It returns the new BlockPointer,
// and internally saves the block for future uses.
type fileBlockDeepCopier func(context.Context, string, IFCERFTBlockPointer) (
	IFCERFTBlockPointer, error)

// crAction represents a specific action to take as part of the
// conflict resolution process.
type crAction interface {
	// swapUnmergedBlock should be called before do(), and if it
	// returns true, the caller must use the merged block
	// corresponding to the returned BlockPointer instead of
	// unmergedBlock when calling do().  If BlockPointer{} is zeroPtr
	// (and true is returned), just swap in the regular mergedBlock.
	swapUnmergedBlock(unmergedChains *crChains, mergedChains *crChains,
		unmergedBlock *DirBlock) (bool, IFCERFTBlockPointer, error)
	// do modifies the given merged block in place to resolve the
	// conflict, and potential uses the provided blockCopyFetchers to
	// obtain copies of other blocks (along with new BlockPointers)
	// when requiring a block copy.
	do(ctx context.Context, unmergedCopier fileBlockDeepCopier,
		mergedCopier fileBlockDeepCopier, unmergedBlock *DirBlock,
		mergedBlock *DirBlock) error
	// updateOps potentially modifies, in place, the slices of
	// unmerged and merged operations stored in the corresponding
	// crChains for the given unmerged and merged most recent
	// pointers.  Eventually, the "unmerged" ops will be pushed as
	// part of a MD update, and so should contain any necessarily
	// operations to fully merge the unmerged data, including any
	// conflict resolution.  The "merged" ops will be played through
	// locally, to notify any caches about the newly-obtained merged
	// data (and any changes to local data that were required as part
	// of conflict resolution, such as renames).  A few things to note:
	// * A particular action's updateOps method may be called more than
	//   once for different sets of chains, however it should only add
	//   new directory operations (like create/rm/rename) into directory
	//   chains.
	// * updateOps doesn't necessarily result in correct BlockPointers within
	//   each of those ops; that must happen in a later phase.
	// * mergedBlock can be nil if the chain is for a file.
	updateOps(unmergedMostRecent IFCERFTBlockPointer, mergedMostRecent IFCERFTBlockPointer, unmergedBlock *DirBlock, mergedBlock *DirBlock,
		unmergedChains *crChains, mergedChains *crChains) error
	// String returns a string representation for this crAction, used
	// for debugging.
	String() string
}
