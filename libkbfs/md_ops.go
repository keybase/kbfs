// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"
	"time"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfsblock"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/kbfsmd"
	"github.com/keybase/kbfs/tlf"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"
)

// MDOpsStandard provides plaintext RootMetadata objects to upper
// layers, and processes RootMetadataSigned objects (encrypted and
// signed) suitable for passing to/from the MDServer backend.
type MDOpsStandard struct {
	config Config
	log    logger.Logger
}

// NewMDOpsStandard returns a new MDOpsStandard
func NewMDOpsStandard(config Config) *MDOpsStandard {
	return &MDOpsStandard{config, config.MakeLogger("")}
}

// convertVerifyingKeyError gives a better error when the TLF was
// signed by a key that is no longer associated with the last writer.
func (md *MDOpsStandard) convertVerifyingKeyError(ctx context.Context,
	rmds *RootMetadataSigned, handle *TlfHandle, err error) error {
	if _, ok := err.(VerifyingKeyNotFoundError); !ok {
		return err
	}

	tlf := handle.GetCanonicalPath()
	writer, nameErr := md.config.KBPKI().GetNormalizedUsername(ctx,
		rmds.MD.LastModifyingWriter().AsUserOrTeam())
	if nameErr != nil {
		writer = libkb.NormalizedUsername("uid: " +
			rmds.MD.LastModifyingWriter().String())
	}
	md.log.CDebugf(ctx, "Unverifiable update for TLF %s: %+v",
		rmds.MD.TlfID(), err)
	return UnverifiableTlfUpdateError{tlf, writer, err}
}

type ctxMDOpsSkipKeyVerificationType int

// This context key indicates that we should skip verification of
// revoked keys, to avoid recursion issues.  Any resulting MD
// that skips verification shouldn't be trusted or cached.
const ctxMDOpsSkipKeyVerification ctxMDOpsSkipKeyVerificationType = 1

func (md *MDOpsStandard) decryptMerkleLeaf(
	ctx context.Context, rmd ReadOnlyRootMetadata,
	kbfsRoot *kbfsmd.MerkleRoot, leafBytes []byte) (
	leaf *kbfsmd.MerkleLeaf, err error) {
	var eLeaf kbfsmd.EncryptedMerkleLeaf
	err = md.config.Codec().Decode(leafBytes, &eLeaf)
	if err != nil {
		return nil, err
	}

	if rmd.TypeForKeying() == tlf.TeamKeying {
		// For teams, only the Keybase service has access to the
		// private key that can decrypt the data, so send the request
		// over to the crypto client.
		cryptoLeaf := kbfscrypto.MakeEncryptedMerkleLeaf(
			eLeaf.Version, eLeaf.EncryptedData, kbfsRoot.Nonce)
		teamID := rmd.GetTlfHandle().FirstResolvedWriter().AsTeamOrBust()
		minKeyGen := keybase1.PerTeamKeyGeneration(rmd.LatestKeyGeneration())
		md.log.CDebugf(ctx, "Decrypting Merkle leaf for team %s with min key "+
			"generation %d", teamID, minKeyGen)
		leafBytes, err := md.config.Crypto().DecryptTeamMerkleLeaf(
			ctx, teamID, *kbfsRoot.EPubKey, cryptoLeaf, minKeyGen)
		if err != nil {
			return nil, err
		}
		var leaf kbfsmd.MerkleLeaf
		if err := md.config.Codec().Decode(leafBytes, &leaf); err != nil {
			return nil, err
		}
		return &leaf, nil
	}

	// The private key we need to decrypt the leaf does not live in
	// key bundles; it lives only in the MDs that were part of a
	// specific keygen.  But we don't yet know what the keygen was, or
	// what MDs were part of it, except that they have to have a
	// larger revision number than the given `rmd`.  So all we can do
	// is iterate up from `rmd`, looking for a key that will unlock
	// the leaf.
	//
	// Luckily, in the common case we'll be trying to verify the head
	// of the folder, so we should be able to use
	// rmd.data.TLFPrivateKey.

	// Fetch the latest MD so we have all possible TLF crypt keys
	// available to this device.
	head, err := md.getForTLF(
		ctx, rmd.TlfID(), rmd.BID(), rmd.MergedStatus(), nil)
	if err != nil {
		return nil, err
	}

	var uid keybase1.UID
	if rmd.TlfID().Type() != tlf.Public {
		session, err := md.config.KBPKI().GetCurrentSession(ctx)
		if err != nil {
			return nil, err
		}
		uid = session.UID
	}

	currRmd := rmd
	for {
		// If currRmd isn't readable, keep fetching MDs until it can
		// be read.  Then try currRmd.data.TLFPrivateKey to decrypt
		// the leaf.  If it doesn't work, fetch MDs until we find the
		// next keygen, and continue the loop.

		privKey := currRmd.data.TLFPrivateKey
		if !currRmd.IsReadable() {
			pmd, err := decryptMDPrivateData(
				ctx, md.config.Codec(), md.config.Crypto(),
				md.config.BlockCache(), md.config.BlockOps(),
				md.config.KeyManager(), md.config.Mode(), uid,
				currRmd.GetSerializedPrivateMetadata(), currRmd,
				head.ReadOnlyRootMetadata, md.log)
			if err != nil {
				return nil, err
			}
			privKey = pmd.TLFPrivateKey
		}
		currKeyGen := currRmd.LatestKeyGeneration()
		if privKey == (kbfscrypto.TLFPrivateKey{}) {
			return nil, errors.Errorf(
				"Can't get TLF private key for key generation %d", currKeyGen)
		}

		mLeaf, err := eLeaf.Decrypt(
			md.config.Codec(), privKey, kbfsRoot.Nonce, *kbfsRoot.EPubKey)
		switch errors.Cause(err).(type) {
		case nil:
			return &mLeaf, nil
		case libkb.DecryptionError:
			// Fall-through to try another key generation.
		default:
			return nil, err
		}

		md.log.CDebugf(ctx, "Key generation %d didn't work; searching for "+
			"the next one", currKeyGen)

	fetchLoop:
		for {
			start := currRmd.Revision() + 1
			end := start + maxMDsAtATime - 1 // range is inclusive
			nextRMDs, err := getMergedMDUpdatesWithEnd(
				ctx, md.config, currRmd.TlfID(), start, end, nil)
			if err != nil {
				return nil, err
			}

			for _, nextRmd := range nextRMDs {
				if nextRmd.LatestKeyGeneration() > currKeyGen {
					md.log.CDebugf(ctx, "Revision %d has key gen %d",
						nextRmd.Revision(), nextRmd.LatestKeyGeneration())
					currRmd = nextRmd.ReadOnlyRootMetadata
					break fetchLoop
				}
			}

			if len(nextRMDs) < maxMDsAtATime {
				md.log.CDebugf(ctx,
					"We tried all revisions and couldn't find a working keygen")
				return nil, errors.Errorf("Can't decrypt merkle leaf")
			}
		}
	}
}

func (md *MDOpsStandard) makeMerkleLeaf(
	ctx context.Context, rmd ReadOnlyRootMetadata,
	kbfsRoot *kbfsmd.MerkleRoot, leafBytes []byte) (
	leaf *kbfsmd.MerkleLeaf, err error) {
	if rmd.TlfID().Type() != tlf.Public {
		return md.decryptMerkleLeaf(ctx, rmd, kbfsRoot, leafBytes)
	}

	var mLeaf kbfsmd.MerkleLeaf
	err = md.config.Codec().Decode(leafBytes, &mLeaf)
	if err != nil {
		return nil, err
	}
	return &mLeaf, nil
}

func (md *MDOpsStandard) verifyKey(
	ctx context.Context, rmds *RootMetadataSigned,
	uid keybase1.UID, verifyingKey kbfscrypto.VerifyingKey,
	irmd ImmutableRootMetadata) (cacheable bool, err error) {
	err = md.config.KBPKI().HasVerifyingKey(ctx, uid, verifyingKey,
		rmds.untrustedServerTimestamp)
	var info revokedKeyInfo
	switch e := errors.Cause(err).(type) {
	case nil:
		return true, nil
	case RevokedDeviceVerificationError:
		if ctx.Value(ctxMDOpsSkipKeyVerification) != nil {
			md.log.CDebugf(ctx,
				"Skipping revoked key verification due to recursion")
			return false, nil
		}
		if e.info.MerkleRoot.Seqno <= 0 {
			md.log.CDebugf(ctx, "Can't verify an MD written by a revoked "+
				"device if there's no valid root seqno to check: %+v", e)
			return true, nil
		}

		info = e.info
		// Fall through to check via the merkle tree.
	default:
		return false, err
	}

	md.log.CDebugf(ctx, "Revision %d for %s was signed by a device that was "+
		"revoked at time=%d,root=%d; checking via Merkle",
		irmd.Revision(), irmd.TlfID(), info.Time, info.MerkleRoot.Seqno)
	ctx = context.WithValue(ctx, ctxMDOpsSkipKeyVerification, struct{}{})

	kbfsRoot, merkleNodes, rootSeqno, err :=
		md.config.MDServer().FindNextMD(ctx, rmds.MD.TlfID(),
			info.MerkleRoot.Seqno)
	if err != nil {
		return false, err
	}
	if len(merkleNodes) == 0 {
		// This can happen legitimately if we are still inside the
		// error window and no new merkle trees have been made yet, or
		// the server could be lying to us.
		md.log.CDebugf(ctx, "The server claims there haven't been any "+
			"KBFS merkle trees published since the revocation")
		// Verify the chain up to the current head.  By using `ctx`,
		// we'll avoid infinite loops in the writer-key-checking code
		// by skipping revoked key verification.  This is ok, because
		// we only care about the hash chain for the purposes of
		// verifying `irmd`.
		chain, err := getMergedMDUpdates(
			ctx, md.config, irmd.TlfID(), irmd.Revision()+1, nil)
		if err != nil {
			return false, err
		}
		if len(chain) > 0 {
			err = irmd.CheckValidSuccessor(
				irmd.mdID, chain[0].ReadOnlyRootMetadata)
			if err != nil {
				return false, err
			}
		}

		// TODO(KBFS-2956): check the most recent global merkle root
		// and KBFS merkle root ctimes and make sure they fall within
		// the expected error window with respect to the revocation.
		// Also eventually check the blockchain-published merkles to
		// make sure the server isn't lying (though that will have a
		// much larger error window).
		return true, nil
	}

	md.log.CDebugf(ctx,
		"Next KBFS merkle root is %d, included in global merkle root seqno=%d",
		kbfsRoot.SeqNo, rootSeqno)

	// Decode (and possibly decrypt) the leaf node, so we can see what
	// the given MD revision number was for the MD that followed the
	// revoke.
	leaf, err := md.makeMerkleLeaf(
		ctx, irmd.ReadOnlyRootMetadata, kbfsRoot,
		merkleNodes[len(merkleNodes)-1])
	if err != nil {
		return false, err
	}

	// If the given revision comes after the merkle leaf revision,
	// then don't verify it.
	if irmd.Revision() > leaf.Revision {
		return false, MDWrittenAfterRevokeError{
			irmd.TlfID(), irmd.Revision(), leaf.Revision, verifyingKey}
	} else if irmd.Revision() == leaf.Revision {
		return true, nil
	}

	// Otherwise it's valid, as long as there's a valid chain of MD
	// revisions between the two, and the global root info checks out
	// as well.  By using `ctx`, we'll avoid infinite loops in the
	// writer-key-checking code by skipping revoked key verification.
	// This is ok, because we only care about the hash chain for the
	// purposes of verifying `irmd`.
	chain, err := getMergedMDUpdatesWithEnd(
		ctx, md.config, irmd.TlfID(), irmd.Revision()+1, leaf.Revision, nil)
	if err != nil {
		return false, err
	}
	if len(chain) == 0 {
		return false, errors.Errorf("Unexpectedly found no revisions "+
			"after %d, even though the merkle tree includes revision %d",
			irmd.Revision(), leaf.Revision)
	}

	err = irmd.CheckValidSuccessor(irmd.mdID, chain[0].ReadOnlyRootMetadata)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (md *MDOpsStandard) verifyWriterKey(ctx context.Context,
	rmds *RootMetadataSigned, irmd ImmutableRootMetadata, handle *TlfHandle,
	getRangeLock *sync.Mutex) error {
	if !rmds.MD.IsWriterMetadataCopiedSet() {
		// Skip verifying the writer key if it's the same as the
		// overall signer's key (which must be verified elsewhere).
		if rmds.GetWriterMetadataSigInfo().VerifyingKey ==
			rmds.SigInfo.VerifyingKey {
			return nil
		}

		_, err := md.verifyKey(
			ctx, rmds, rmds.MD.LastModifyingWriter(),
			rmds.GetWriterMetadataSigInfo().VerifyingKey, irmd)
		if err != nil {
			return md.convertVerifyingKeyError(ctx, rmds, handle, err)
		}
		return nil
	}

	// The writer metadata can be copied only for rekeys or
	// finalizations, neither of which should happen while
	// unmerged.
	if rmds.MD.MergedStatus() != kbfsmd.Merged {
		return errors.Errorf("Revision %d for %s has a copied writer "+
			"metadata, but is unexpectedly not merged",
			rmds.MD.RevisionNumber(), rmds.MD.TlfID())
	}

	if getRangeLock != nil {
		// If there are multiple goroutines, we don't want to risk
		// several concurrent requests to the MD server, just in case
		// there are several revisions with copied writer MD in this
		// range.
		//
		// TODO: bugs could result in thousands (or more) copied MD
		// updates in a row (i.e., too many to fit in the cache).  We
		// could do something more sophisticated here where once one
		// goroutine finds the copied MD, it stores it somewhere so
		// the other goroutines don't have to also search through all
		// the same MD updates (which may have been evicted from the
		// cache in the meantime).  Also, maybe copied writer MDs
		// should include the original revision number so we don't
		// have to search like this.
		getRangeLock.Lock()
		defer getRangeLock.Unlock()
	}

	// The server timestamp on rmds does not reflect when the
	// writer MD was actually signed, since it was copied from a
	// previous revision.  Search backwards for the most recent
	// uncopied writer MD to get the right timestamp.
	prevHead := rmds.MD.RevisionNumber() - 1
	for {
		startRev := prevHead - maxMDsAtATime + 1
		if startRev < kbfsmd.RevisionInitial {
			startRev = kbfsmd.RevisionInitial
		}

		// Recursively call into MDOps.  Note that in the case where
		// we were already fetching a range of MDs, this could do
		// extra work by downloading the same MDs twice (for those
		// that aren't yet in the cache).  That should be so rare that
		// it's not worth optimizing.
		prevMDs, err := getMDRange(ctx, md.config, rmds.MD.TlfID(),
			rmds.MD.BID(), startRev, prevHead, rmds.MD.MergedStatus(), nil)
		if err != nil {
			return err
		}

		for i := len(prevMDs) - 1; i >= 0; i-- {
			if !prevMDs[i].IsWriterMetadataCopiedSet() {
				// We want to compare the writer signature of
				// rmds with that of prevMDs[i]. However, we've
				// already dropped prevMDs[i]'s writer
				// signature. We can just verify prevMDs[i]'s
				// writer metadata with rmds's signature,
				// though.
				buf, err := prevMDs[i].GetSerializedWriterMetadata(md.config.Codec())
				if err != nil {
					return err
				}

				err = kbfscrypto.Verify(
					buf, rmds.GetWriterMetadataSigInfo())
				if err != nil {
					return errors.Errorf("Could not verify "+
						"uncopied writer metadata "+
						"from revision %d of folder "+
						"%s with signature from "+
						"revision %d: %v",
						prevMDs[i].Revision(),
						rmds.MD.TlfID(),
						rmds.MD.RevisionNumber(), err)
				}

				// The fact the fact that we were able to process this
				// MD correctly means that we already verified its key
				// at the correct timestamp, so we're good.
				return nil
			}
		}

		// No more MDs left to process.
		if len(prevMDs) < maxMDsAtATime {
			return errors.Errorf("Couldn't find uncopied MD previous to "+
				"revision %d of folder %s for checking the writer "+
				"timestamp", rmds.MD.RevisionNumber(), rmds.MD.TlfID())
		}
		prevHead = prevMDs[0].Revision() - 1
	}
}

type everyoneOnEveryTeamChecker struct{}

func (e everyoneOnEveryTeamChecker) IsTeamWriter(
	_ context.Context, _ keybase1.TeamID, _ keybase1.UID,
	_ kbfscrypto.VerifyingKey) (bool, error) {
	return true, nil
}

func (e everyoneOnEveryTeamChecker) IsTeamReader(
	_ context.Context, _ keybase1.TeamID, _ keybase1.UID) (bool, error) {
	return true, nil
}

// processMetadata converts the given rmds to an
// ImmutableRootMetadata. After this function is called, rmds
// shouldn't be used.
func (md *MDOpsStandard) processMetadata(ctx context.Context,
	handle *TlfHandle, rmds *RootMetadataSigned, extra kbfsmd.ExtraMetadata,
	getRangeLock *sync.Mutex) (ImmutableRootMetadata, error) {
	// First, verify validity and signatures. Until KBFS-2955 is
	// complete, KBFS doesn't check for team membership on MDs that
	// have been fetched from the server, because if the writer has
	// been removed from the team since the MD was written, we have no
	// easy way of verifying that they used to be in the team.  We
	// rely on the fact that the updates are decryptable with the
	// secret key as a way to prove that only an authorized team
	// member wrote the update, along with trusting that the server
	// would have rejected an update from a former team member that is
	// still using an old key.  TODO(KBFS-2955): remove this.
	err := rmds.IsValidAndSigned(
		ctx, md.config.Codec(),
		everyoneOnEveryTeamChecker{}, extra)
	if err != nil {
		return ImmutableRootMetadata{}, MDMismatchError{
			rmds.MD.RevisionNumber(), handle.GetCanonicalPath(),
			rmds.MD.TlfID(), err,
		}
	}

	// Get the UID unless this is a public tlf - then proceed with empty uid.
	var uid keybase1.UID
	if handle.Type() != tlf.Public {
		session, err := md.config.KBPKI().GetCurrentSession(ctx)
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
		uid = session.UID
	}

	// TODO: Avoid having to do this type assertion.
	brmd, ok := rmds.MD.(kbfsmd.MutableRootMetadata)
	if !ok {
		return ImmutableRootMetadata{}, kbfsmd.MutableRootMetadataNoImplError{}
	}

	rmd := makeRootMetadata(brmd, extra, handle)
	// Try to decrypt using the keys available in this md.  If that
	// doesn't work, a future MD may contain more keys and will be
	// tried later.
	pmd, err := decryptMDPrivateData(
		ctx, md.config.Codec(), md.config.Crypto(),
		md.config.BlockCache(), md.config.BlockOps(),
		md.config.KeyManager(), md.config.Mode(), uid,
		rmd.GetSerializedPrivateMetadata(), rmd, rmd, md.log)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	rmd.data = pmd

	mdID, err := kbfsmd.MakeID(md.config.Codec(), rmd.bareMd)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	localTimestamp := rmds.untrustedServerTimestamp
	if offset, ok := md.config.MDServer().OffsetFromServerTime(); ok {
		localTimestamp = localTimestamp.Add(offset)
	}

	key := rmds.GetWriterMetadataSigInfo().VerifyingKey
	irmd := MakeImmutableRootMetadata(rmd, key, mdID, localTimestamp, true)

	// Then, verify the verifying keys.  We do this after decrypting
	// the MD and making the ImmutableRootMetadata, since we may need
	// access to the private metadata when checking the merkle roots,
	// and we also need access to the `mdID`.
	if err := md.verifyWriterKey(
		ctx, rmds, irmd, handle, getRangeLock); err != nil {
		return ImmutableRootMetadata{}, err
	}

	cacheable, err := md.verifyKey(
		ctx, rmds, rmds.MD.GetLastModifyingUser(), rmds.SigInfo.VerifyingKey,
		irmd)
	if err != nil {
		return ImmutableRootMetadata{}, md.convertVerifyingKeyError(
			ctx, rmds, handle, err)
	}

	// Make sure the caller doesn't use rmds anymore.
	*rmds = RootMetadataSigned{}

	if cacheable {
		err = md.config.MDCache().Put(irmd)
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
	}
	return irmd, nil
}

func (md *MDOpsStandard) getForHandle(ctx context.Context, handle *TlfHandle,
	mStatus kbfsmd.MergeStatus, lockBeforeGet *keybase1.LockID) (
	id tlf.ID, rmd ImmutableRootMetadata, err error) {
	// If we already know the tlf ID, we shouldn't be calling this
	// function.
	if handle.tlfID != tlf.NullID {
		return tlf.ID{}, ImmutableRootMetadata{}, errors.Errorf(
			"GetForHandle called for %s with non-nil TLF ID %s",
			handle.GetCanonicalPath(), handle.tlfID)
	}

	// Check for handle readership, to give a nice error early.
	if handle.Type() == tlf.Private {
		session, err := md.config.KBPKI().GetCurrentSession(ctx)
		if err != nil {
			return tlf.ID{}, ImmutableRootMetadata{}, err
		}

		if !handle.IsReader(session.UID) {
			return tlf.ID{}, ImmutableRootMetadata{}, NewReadAccessError(
				handle, session.Name, handle.GetCanonicalPath())
		}
	}

	md.log.CDebugf(
		ctx, "GetForHandle: %s %s", handle.GetCanonicalPath(), mStatus)
	defer func() {
		// Temporary debugging for KBFS-1921.  TODO: remove.
		if err != nil {
			md.log.CDebugf(ctx, "GetForHandle done with err=%+v", err)
		} else if rmd != (ImmutableRootMetadata{}) {
			md.log.CDebugf(ctx, "GetForHandle done, id=%s, revision=%d, "+
				"mStatus=%s", id, rmd.Revision(), rmd.MergedStatus())
		} else {
			md.log.CDebugf(
				ctx, "GetForHandle done, id=%s, no %s MD revisions yet", id,
				mStatus)
		}
	}()

	mdserv := md.config.MDServer()
	bh, err := handle.ToBareHandle()
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	id, rmds, err := mdserv.GetForHandle(ctx, bh, mStatus, lockBeforeGet)
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	if rmds == nil {
		if mStatus == kbfsmd.Unmerged {
			// The caller ignores the id argument for
			// mStatus == kbfsmd.Unmerged.
			return tlf.ID{}, ImmutableRootMetadata{}, nil
		}
		return id, ImmutableRootMetadata{}, nil
	}

	extra, err := md.getExtraMD(ctx, rmds.MD)
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	bareMdHandle, err := rmds.MD.MakeBareTlfHandle(extra)
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	mdHandle, err := MakeTlfHandle(
		ctx, bareMdHandle, id.Type(), md.config.KBPKI(), md.config.KBPKI(), nil)
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	// Check for mutual handle resolution.
	if err := mdHandle.MutuallyResolvesTo(ctx, md.config.Codec(),
		md.config.KBPKI(), nil, *handle, rmds.MD.RevisionNumber(),
		rmds.MD.TlfID(), md.log); err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}
	// Set the ID after checking the resolve, because `handle` doesn't
	// have the TLF ID set yet.
	mdHandle.tlfID = id

	// TODO: For now, use the mdHandle that came with rmds for
	// consistency. In the future, we'd want to eventually notify
	// the upper layers of the new name, either directly, or
	// through a rekey.
	rmd, err = md.processMetadata(ctx, mdHandle, rmds, extra, nil)
	if err != nil {
		return tlf.ID{}, ImmutableRootMetadata{}, err
	}

	return id, rmd, nil
}

// GetIDForHandle implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetIDForHandle(
	ctx context.Context, handle *TlfHandle) (id tlf.ID, err error) {
	mdcache := md.config.MDCache()
	id, err = mdcache.GetIDForHandle(handle)
	switch errors.Cause(err).(type) {
	case NoSuchTlfIDError:
		// Do the server-based lookup below.
	case nil:
		return id, nil
	default:
		return tlf.NullID, err
	}
	id, _, err = md.getForHandle(ctx, handle, kbfsmd.Merged, nil)
	switch errors.Cause(err).(type) {
	case kbfsmd.ServerErrorClassicTLFDoesNotExist:
		// The server thinks we should create an implicit team for this TLF.
		return tlf.NullID, nil
	case nil:
	default:
		return tlf.NullID, err
	}
	err = mdcache.PutIDForHandle(handle, id)
	if err != nil {
		return tlf.NullID, err
	}
	return id, nil
}

func (md *MDOpsStandard) processMetadataWithID(ctx context.Context,
	id tlf.ID, bid kbfsmd.BranchID, handle *TlfHandle, rmds *RootMetadataSigned,
	extra kbfsmd.ExtraMetadata, getRangeLock *sync.Mutex) (ImmutableRootMetadata, error) {
	// Make sure the signed-over ID matches
	if id != rmds.MD.TlfID() {
		return ImmutableRootMetadata{}, MDMismatchError{
			rmds.MD.RevisionNumber(), id.String(), rmds.MD.TlfID(),
			errors.Errorf("MD contained unexpected folder id %s, expected %s",
				rmds.MD.TlfID().String(), id.String()),
		}
	}
	// Make sure the signed-over branch ID matches
	if bid != kbfsmd.NullBranchID && bid != rmds.MD.BID() {
		return ImmutableRootMetadata{}, MDMismatchError{
			rmds.MD.RevisionNumber(), id.String(), rmds.MD.TlfID(),
			errors.Errorf("MD contained unexpected branch id %s, expected %s, "+
				"folder id %s", rmds.MD.BID().String(), bid.String(), id.String()),
		}
	}

	return md.processMetadata(ctx, handle, rmds, extra, getRangeLock)
}

type constIDGetter struct {
	id tlf.ID
}

func (c constIDGetter) GetIDForHandle(_ context.Context, _ *TlfHandle) (
	tlf.ID, error) {
	return c.id, nil
}

func (md *MDOpsStandard) getForTLF(ctx context.Context, id tlf.ID,
	bid kbfsmd.BranchID, mStatus kbfsmd.MergeStatus, lockBeforeGet *keybase1.LockID) (
	ImmutableRootMetadata, error) {
	rmds, err := md.config.MDServer().GetForTLF(
		ctx, id, bid, mStatus, lockBeforeGet)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	if rmds == nil {
		// Possible if mStatus is kbfsmd.Unmerged
		return ImmutableRootMetadata{}, nil
	}
	extra, err := md.getExtraMD(ctx, rmds.MD)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	bareHandle, err := rmds.MD.MakeBareTlfHandle(extra)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	handle, err := MakeTlfHandle(
		ctx, bareHandle, rmds.MD.TlfID().Type(), md.config.KBPKI(),
		md.config.KBPKI(), constIDGetter{id})
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	rmd, err := md.processMetadataWithID(ctx, id, bid, handle, rmds, extra, nil)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	return rmd, nil
}

// GetForTLF implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetForTLF(
	ctx context.Context, id tlf.ID, lockBeforeGet *keybase1.LockID) (
	ImmutableRootMetadata, error) {
	return md.getForTLF(ctx, id, kbfsmd.NullBranchID, kbfsmd.Merged, lockBeforeGet)
}

// GetUnmergedForTLF implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetUnmergedForTLF(
	ctx context.Context, id tlf.ID, bid kbfsmd.BranchID) (
	ImmutableRootMetadata, error) {
	return md.getForTLF(ctx, id, bid, kbfsmd.Unmerged, nil)
}

func (md *MDOpsStandard) processRange(ctx context.Context, id tlf.ID,
	bid kbfsmd.BranchID, rmdses []*RootMetadataSigned) (
	[]ImmutableRootMetadata, error) {
	if len(rmdses) == 0 {
		return nil, nil
	}

	eg, groupCtx := errgroup.WithContext(ctx)

	// Parallelize the MD decryption, because it could involve
	// fetching blocks to get unembedded block changes.
	rmdsChan := make(chan *RootMetadataSigned, len(rmdses))
	irmdChan := make(chan ImmutableRootMetadata, len(rmdses))
	var getRangeLock sync.Mutex
	worker := func() error {
		for rmds := range rmdsChan {
			extra, err := md.getExtraMD(groupCtx, rmds.MD)
			if err != nil {
				return err
			}
			bareHandle, err := rmds.MD.MakeBareTlfHandle(extra)
			if err != nil {
				return err
			}
			handle, err := MakeTlfHandle(
				groupCtx, bareHandle, rmds.MD.TlfID().Type(), md.config.KBPKI(),
				md.config.KBPKI(), constIDGetter{id})
			if err != nil {
				return err
			}
			irmd, err := md.processMetadataWithID(groupCtx, id, bid,
				handle, rmds, extra, &getRangeLock)
			if err != nil {
				return err
			}
			irmdChan <- irmd
		}
		return nil
	}

	numWorkers := len(rmdses)
	if numWorkers > maxMDsAtATime {
		numWorkers = maxMDsAtATime
	}
	for i := 0; i < numWorkers; i++ {
		eg.Go(worker)
	}

	// Do this first, since processMetadataWithID consumes its
	// rmds argument.
	startRev := rmdses[0].MD.RevisionNumber()
	rmdsCount := len(rmdses)

	for _, rmds := range rmdses {
		rmdsChan <- rmds
	}
	close(rmdsChan)
	rmdses = nil
	err := eg.Wait()
	if err != nil {
		return nil, err
	}
	close(irmdChan)

	// Sort into slice based on revision.
	irmds := make([]ImmutableRootMetadata, rmdsCount)
	numExpected := kbfsmd.Revision(len(irmds))
	for irmd := range irmdChan {
		i := irmd.Revision() - startRev
		if i < 0 || i >= numExpected {
			return nil, errors.Errorf("Unexpected revision %d; expected "+
				"something between %d and %d inclusive", irmd.Revision(),
				startRev, startRev+numExpected-1)
		} else if irmds[i] != (ImmutableRootMetadata{}) {
			return nil, errors.Errorf("Got revision %d twice", irmd.Revision())
		}
		irmds[i] = irmd
	}

	// Now that we have all the immutable RootMetadatas, verify that
	// the given MD objects form a valid sequence.
	var prevIRMD ImmutableRootMetadata
	for _, irmd := range irmds {
		if prevIRMD != (ImmutableRootMetadata{}) {
			err = prevIRMD.CheckValidSuccessor(
				prevIRMD.mdID, irmd.ReadOnlyRootMetadata)
			if err != nil {
				return nil, MDMismatchError{
					prevIRMD.Revision(),
					irmd.GetTlfHandle().GetCanonicalPath(),
					prevIRMD.TlfID(), err,
				}
			}
		}
		prevIRMD = irmd
	}

	// TODO: in the case where lastRoot == MdID{}, should we verify
	// that the starting PrevRoot points back to something that's
	// actually a valid part of this history?  If the MD signature is
	// indeed valid, this probably isn't a huge deal, but it may let
	// the server rollback or truncate unmerged history...

	return irmds, nil
}

func (md *MDOpsStandard) getRange(ctx context.Context, id tlf.ID,
	bid kbfsmd.BranchID, mStatus kbfsmd.MergeStatus, start, stop kbfsmd.Revision,
	lockBeforeGet *keybase1.LockID) ([]ImmutableRootMetadata, error) {
	rmds, err := md.config.MDServer().GetRange(
		ctx, id, bid, mStatus, start, stop, lockBeforeGet)
	if err != nil {
		return nil, err
	}
	rmd, err := md.processRange(ctx, id, bid, rmds)
	if err != nil {
		return nil, err
	}
	return rmd, nil
}

// GetRange implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetRange(ctx context.Context, id tlf.ID,
	start, stop kbfsmd.Revision, lockBeforeGet *keybase1.LockID) (
	[]ImmutableRootMetadata, error) {
	return md.getRange(
		ctx, id, kbfsmd.NullBranchID, kbfsmd.Merged, start, stop, lockBeforeGet)
}

// GetUnmergedRange implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetUnmergedRange(ctx context.Context, id tlf.ID,
	bid kbfsmd.BranchID, start, stop kbfsmd.Revision) (
	[]ImmutableRootMetadata, error) {
	return md.getRange(ctx, id, bid, kbfsmd.Unmerged, start, stop, nil)
}

func (md *MDOpsStandard) put(ctx context.Context, rmd *RootMetadata,
	verifyingKey kbfscrypto.VerifyingKey, lockContext *keybase1.LockContext,
	priority keybase1.MDPriority) (ImmutableRootMetadata, error) {
	session, err := md.config.KBPKI().GetCurrentSession(ctx)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	// Ensure that the block changes are properly unembedded.
	if !rmd.IsWriterMetadataCopiedSet() &&
		rmd.data.Changes.Info.BlockPointer == zeroPtr &&
		!md.config.BlockSplitter().ShouldEmbedBlockChanges(&rmd.data.Changes) {
		return ImmutableRootMetadata{},
			errors.New("MD has embedded block changes, but shouldn't")
	}

	err = encryptMDPrivateData(
		ctx, md.config.Codec(), md.config.Crypto(),
		md.config.Crypto(), md.config.KeyManager(), session.UID, rmd)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	rmds, err := SignBareRootMetadata(
		ctx, md.config.Codec(), md.config.Crypto(), md.config.Crypto(),
		rmd.bareMd, time.Time{})
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	err = md.config.MDServer().Put(ctx, rmds, rmd.extra, lockContext, priority)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	mdID, err := kbfsmd.MakeID(md.config.Codec(), rmds.MD)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	irmd := MakeImmutableRootMetadata(
		rmd, verifyingKey, mdID, md.config.Clock().Now(), true)
	// Revisions created locally should always override anything else
	// in the cache.
	err = md.config.MDCache().Replace(irmd, irmd.BID())
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	md.log.CDebugf(ctx, "Put MD rev=%d id=%s", rmd.Revision(), mdID)

	return irmd, nil
}

// Put implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) Put(ctx context.Context, rmd *RootMetadata,
	verifyingKey kbfscrypto.VerifyingKey, lockContext *keybase1.LockContext,
	priority keybase1.MDPriority) (ImmutableRootMetadata, error) {
	if rmd.MergedStatus() == kbfsmd.Unmerged {
		return ImmutableRootMetadata{}, UnexpectedUnmergedPutError{}
	}
	return md.put(ctx, rmd, verifyingKey, lockContext, priority)
}

// PutUnmerged implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) PutUnmerged(
	ctx context.Context, rmd *RootMetadata,
	verifyingKey kbfscrypto.VerifyingKey) (ImmutableRootMetadata, error) {
	rmd.SetUnmerged()
	if rmd.BID() == kbfsmd.NullBranchID {
		// new branch ID
		bid, err := md.config.Crypto().MakeRandomBranchID()
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
		rmd.SetBranchID(bid)
	}
	return md.put(ctx, rmd, verifyingKey, nil, keybase1.MDPriorityNormal)
}

// PruneBranch implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) PruneBranch(
	ctx context.Context, id tlf.ID, bid kbfsmd.BranchID) error {
	return md.config.MDServer().PruneBranch(ctx, id, bid)
}

// ResolveBranch implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) ResolveBranch(
	ctx context.Context, id tlf.ID, bid kbfsmd.BranchID, _ []kbfsblock.ID,
	rmd *RootMetadata, verifyingKey kbfscrypto.VerifyingKey) (
	ImmutableRootMetadata, error) {
	// Put the MD first.
	irmd, err := md.Put(ctx, rmd, verifyingKey, nil, keybase1.MDPriorityNormal)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	// Prune the branch ia the journal, if there is one.  If the
	// client fails before this is completed, we'll need to check for
	// resolutions on the next restart (see KBFS-798).
	err = md.PruneBranch(ctx, id, bid)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	return irmd, nil
}

// GetLatestHandleForTLF implements the MDOps interface for MDOpsStandard.
func (md *MDOpsStandard) GetLatestHandleForTLF(ctx context.Context, id tlf.ID) (
	tlf.Handle, error) {
	// TODO: Verify this mapping using a Merkle tree.
	return md.config.MDServer().GetLatestHandleForTLF(ctx, id)
}

func (md *MDOpsStandard) getExtraMD(ctx context.Context, brmd kbfsmd.RootMetadata) (
	extra kbfsmd.ExtraMetadata, err error) {
	wkbID, rkbID := brmd.GetTLFWriterKeyBundleID(), brmd.GetTLFReaderKeyBundleID()
	if (wkbID == kbfsmd.TLFWriterKeyBundleID{}) || (rkbID == kbfsmd.TLFReaderKeyBundleID{}) {
		// Pre-v3 metadata embed key bundles and as such won't set any IDs.
		return nil, nil
	}
	mdserv := md.config.MDServer()
	kbcache := md.config.KeyBundleCache()
	tlf := brmd.TlfID()
	// Check the cache.
	wkb, err2 := kbcache.GetTLFWriterKeyBundle(wkbID)
	if err2 != nil {
		md.log.CDebugf(ctx, "Error fetching writer key bundle %s from cache for TLF %s: %s",
			wkbID, tlf, err2)
	}
	rkb, err2 := kbcache.GetTLFReaderKeyBundle(rkbID)
	if err2 != nil {
		md.log.CDebugf(ctx, "Error fetching reader key bundle %s from cache for TLF %s: %s",
			rkbID, tlf, err2)
	}
	if wkb != nil && rkb != nil {
		return kbfsmd.NewExtraMetadataV3(*wkb, *rkb, false, false), nil
	}
	if wkb != nil {
		// Don't need the writer bundle.
		_, rkb, err = mdserv.GetKeyBundles(ctx, tlf, kbfsmd.TLFWriterKeyBundleID{}, rkbID)
	} else if rkb != nil {
		// Don't need the reader bundle.
		wkb, _, err = mdserv.GetKeyBundles(ctx, tlf, wkbID, kbfsmd.TLFReaderKeyBundleID{})
	} else {
		// Need them both.
		wkb, rkb, err = mdserv.GetKeyBundles(ctx, tlf, wkbID, rkbID)
	}
	if err != nil {
		return nil, err
	}
	// Cache the results.
	kbcache.PutTLFWriterKeyBundle(wkbID, *wkb)
	kbcache.PutTLFReaderKeyBundle(rkbID, *rkb)
	return kbfsmd.NewExtraMetadataV3(*wkb, *rkb, false, false), nil
}
