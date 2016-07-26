// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"

	keybase1 "github.com/keybase/client/go/protocol"
)

// ImmutableBareRootMetadata is a thin wrapper around a
// *BareRootMetadata that takes ownership of it and does not ever
// modify it again. Thus, its MdID can be calculated and
// stored. ImmutableBareRootMetadata objects can be assumed to never
// alias a (modifiable) *BareRootMetadata.
//
// TODO: Move this to bare_root_metadata.go if it's used in more
// places.
type ImmutableBareRootMetadata struct {
	*BareRootMetadata
	mdID MdID
}

// MakeImmutableBareRootMetadata makes a new ImmutableBareRootMetadata
// from the given BareRootMetadata and its corresponding MdID.
func MakeImmutableBareRootMetadata(
	rmd *BareRootMetadata, mdID MdID) ImmutableBareRootMetadata {
	if mdID == (MdID{}) {
		panic("zero mdID passed to MakeImmutableBareRootMetadata")
	}
	return ImmutableBareRootMetadata{rmd, mdID}
}

// mdJournal stores a single ordered list of metadata IDs for
// a single TLF, along with the associated metadata objects, in flat
// files on disk.
//
// The directory layout looks like:
//
// dir/md_journal/EARLIEST
// dir/md_journal/LATEST
// dir/md_journal/0...001
// dir/md_journal/0...002
// dir/md_journal/0...fff
// dir/mds/0100/0...01
// ...
// dir/mds/01ff/f...ff
//
// There's a single journal subdirectory; the journal ordinals are
// just MetadataRevisions, and the journal entries are just MdIDs.
//
// The Metadata objects are stored separately in dir/mds. Each block
// has its own subdirectory with its ID as a name. The MD
// subdirectories are splayed over (# of possible hash types) * 256
// subdirectories -- one byte for the hash type (currently only one)
// plus the first byte of the hash data -- using the first four
// characters of the name to keep the number of directories in dir
// itself to a manageable number, similar to git.
//
// mdJournal is not goroutine-safe, so any code that uses it must
// guarantee that only one goroutine at a time calls its functions.
type mdJournal struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	log logger.Logger

	j mdIDJournal
}

func makeMDJournal(codec Codec, crypto cryptoPure, dir string,
	log logger.Logger) mdJournal {
	journalDir := filepath.Join(dir, "md_journal")

	journal := mdJournal{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
		log:    log,
		j:      makeMdIDJournal(codec, journalDir),
	}
	return journal
}

// The functions below are for building various paths.

func (j mdJournal) mdsPath() string {
	return filepath.Join(j.dir, "mds")
}

func (j mdJournal) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(j.mdsPath(), idStr[:4], idStr[4:])
}

// getMD verifies the MD data and the writer signature (but not the
// key) for the given ID and returns it.
func (j mdJournal) getMD(id MdID) (*BareRootMetadata, error) {
	// Read file.

	path := j.mdPath(id)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rmd BareRootMetadata
	err = j.codec.Decode(data, &rmd)
	if err != nil {
		return nil, err
	}

	// Check integrity.

	mdID, err := j.crypto.MakeMdID(&rmd)
	if err != nil {
		return nil, err
	}

	if id != mdID {
		return nil, fmt.Errorf(
			"Metadata ID mismatch: expected %s, got %s", id, mdID)
	}

	err = rmd.VerifyWriterMetadata(j.codec, j.crypto)
	if err != nil {
		return nil, err
	}

	return &rmd, nil
}

// putMD stores the given metadata under its ID, if it's not already
// stored.
func (j mdJournal) putMD(
	rmd *BareRootMetadata, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey) (MdID, error) {
	err := rmd.IsValidAndSigned(
		j.codec, j.crypto, currentUID, currentVerifyingKey)
	if err != nil {
		return MdID{}, err
	}

	id, err := j.crypto.MakeMdID(rmd)
	if err != nil {
		return MdID{}, err
	}

	_, err = j.getMD(id)
	if os.IsNotExist(err) {
		// Continue on.
	} else if err != nil {
		return MdID{}, err
	} else {
		// Entry exists, so nothing else to do.
		return MdID{}, nil
	}

	path := j.mdPath(id)

	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return MdID{}, err
	}

	buf, err := j.codec.Encode(rmd)
	if err != nil {
		return MdID{}, err
	}

	err = ioutil.WriteFile(path, buf, 0600)
	if err != nil {
		return MdID{}, err
	}

	return id, nil
}

func (j mdJournal) getHeadHelper() (ImmutableBareRootMetadata, error) {
	headID, err := j.j.getLatest()
	if err != nil {
		return ImmutableBareRootMetadata{}, err
	}
	if headID == (MdID{}) {
		return ImmutableBareRootMetadata{}, nil
	}
	head, err := j.getMD(headID)
	if err != nil {
		return ImmutableBareRootMetadata{}, err
	}
	return MakeImmutableBareRootMetadata(head, headID), nil
}

func (j mdJournal) checkGetParams(currentUID keybase1.UID) (
	ImmutableBareRootMetadata, error) {
	head, err := j.getHeadHelper()
	if err != nil {
		return ImmutableBareRootMetadata{}, err
	}

	if head != (ImmutableBareRootMetadata{}) {
		ok, err := isReader(currentUID, head.BareRootMetadata)
		if err != nil {
			return ImmutableBareRootMetadata{}, err
		}
		if !ok {
			// TODO: Use a non-server error.
			return ImmutableBareRootMetadata{},
				MDServerErrorUnauthorized{}
		}
	}

	return head, nil
}

func (j mdJournal) convertToBranch(
	ctx context.Context, signer cryptoSigner,
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey) error {
	head, err := j.getHeadHelper()
	if err != nil {
		return err
	}

	if head.BID != NullBranchID {
		return fmt.Errorf(
			"convertToBranch called with BID=%s", head.BID)
	}

	earliestRevision, err := j.j.readEarliestRevision()
	if err != nil {
		return err
	}

	latestRevision := head.Revision

	j.log.Debug("rewriting MDs %s to %s", earliestRevision, latestRevision)

	_, allMdIDs, err := j.j.getRange(earliestRevision, latestRevision)
	if err != nil {
		return err
	}

	bid, err := j.crypto.MakeRandomBranchID()
	if err != nil {
		return err
	}

	j.log.Debug("New branch ID=%s", bid)

	// TODO: Do the below atomically.

	var prevID MdID

	for i, id := range allMdIDs {
		brmd, err := j.getMD(id)
		if err != nil {
			return err
		}
		brmd.WFlags |= MetadataFlagUnmerged
		brmd.BID = bid

		// Re-sign the writer metadata.
		buf, err := j.codec.Encode(brmd.WriterMetadata)
		if err != nil {
			return err
		}

		sigInfo, err := signer.Sign(ctx, buf)
		if err != nil {
			return err
		}
		brmd.WriterMetadataSigInfo = sigInfo

		j.log.Debug("Old prev root of rev=%s is %s",
			brmd.Revision, brmd.PrevRoot)

		if i > 0 {
			j.log.Debug("Changing prev root of rev=%s to %s",
				brmd.Revision, prevID)
			brmd.PrevRoot = prevID
		}

		newID, err := j.putMD(brmd, currentUID, currentVerifyingKey)
		if err != nil {
			return err
		}

		o, err := revisionToOrdinal(brmd.Revision)
		if err != nil {
			return err
		}

		err = j.j.j.writeJournalEntry(o, newID)
		if err != nil {
			return err
		}

		prevID = newID

		j.log.Debug("Changing ID for rev=%s from %s to %s",
			brmd.Revision, id, newID)
	}

	return err
}

func (j mdJournal) pushEarliestToServer(
	ctx context.Context, signer cryptoSigner, mdserver MDServer) (
	MdID, *BareRootMetadata, error) {
	earliestID, err := j.j.getEarliest()
	if err != nil {
		return MdID{}, nil, err
	}
	if earliestID == (MdID{}) {
		return MdID{}, nil, nil
	}

	rmd, err := j.getMD(earliestID)
	if err != nil {
		return MdID{}, nil, err
	}

	j.log.Debug("Flushing MD put id=%s, rev=%s", earliestID, rmd.Revision)

	var rmds RootMetadataSigned
	rmds.MD = *rmd
	err = signMD(ctx, j.codec, signer, &rmds)
	if err != nil {
		return MdID{}, nil, err
	}
	err = mdserver.Put(ctx, &rmds)
	if err != nil {
		// Still return the ID and RMD so that they can be
		// consulted.
		return earliestID, rmd, err
	}

	return earliestID, rmd, nil
}

// All functions below are public functions.

func (j mdJournal) length() (uint64, error) {
	return j.j.length()
}

func (j mdJournal) getHead(currentUID keybase1.UID) (
	ImmutableBareRootMetadata, error) {
	return j.checkGetParams(currentUID)
}

func (j mdJournal) getRange(
	currentUID keybase1.UID, start, stop MetadataRevision) (
	[]ImmutableBareRootMetadata, error) {
	_, err := j.checkGetParams(currentUID)
	if err != nil {
		return nil, err
	}

	realStart, mdIDs, err := j.j.getRange(start, stop)
	if err != nil {
		return nil, err
	}
	var rmds []ImmutableBareRootMetadata
	for i, mdID := range mdIDs {
		expectedRevision := realStart + MetadataRevision(i)
		rmd, err := j.getMD(mdID)
		if err != nil {
			return nil, err
		}
		if expectedRevision != rmd.Revision {
			panic(fmt.Errorf("expected revision %v, got %v",
				expectedRevision, rmd.Revision))
		}
		irmd := MakeImmutableBareRootMetadata(rmd, mdID)
		rmds = append(rmds, irmd)
	}

	return rmds, nil
}

// MDJournalConflictError is an error that is returned when a put
// detects a rewritten journal.
type MDJournalConflictError struct{}

func (e MDJournalConflictError) Error() string {
	return "MD journal conflict error"
}

// put verifies and stores the given RootMetadata in the journal,
// modifying it as needed. In particular, if this is an unmerged
// RootMetadata but the branch ID isn't set, it will be set to the
// journal's branch ID, which is assumed to be non-zero.
func (j mdJournal) put(
	ctx context.Context, signer cryptoSigner, ekg encryptionKeyGetter,
	rmd *RootMetadata, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey) (MdID, error) {
	head, err := j.getHeadHelper()
	if err != nil {
		return MdID{}, err
	}

	mStatus := rmd.MergedStatus()
	bid := rmd.BID

	if (mStatus == Unmerged) && (bid == NullBranchID) &&
		(head != ImmutableBareRootMetadata{}) {
		rmd.BID = head.BID
		rmd.PrevRoot = head.mdID
		bid = rmd.BID
	}

	if (mStatus == Merged) != (bid == NullBranchID) {
		return MdID{}, errors.New("Invalid branch ID")
	}

	// Check permissions and consistency with head, if it exists.
	if head != (ImmutableBareRootMetadata{}) {
		ok, err := isWriterOrValidRekey(
			j.codec, currentUID, head.BareRootMetadata,
			&rmd.BareRootMetadata)
		if err != nil {
			return MdID{}, err
		}
		if !ok {
			// TODO: Use a non-server error.
			return MdID{}, MDServerErrorUnauthorized{}
		}

		// If we're trying to push a merged MD onto a branch,
		// return a conflict error so the caller can retry
		// with an unmerged MD.
		if mStatus == Merged && head.BID != NullBranchID {
			return MdID{}, MDJournalConflictError{}
		}

		// Consistency checks
		err = head.CheckValidSuccessorForServer(
			head.mdID, &rmd.BareRootMetadata)
		if err != nil {
			return MdID{}, err
		}
	}

	brmd, err := encryptMDPrivateData(
		ctx, j.codec, j.crypto, signer, ekg,
		currentUID, rmd.ReadOnly())
	if err != nil {
		return MdID{}, err
	}

	id, err := j.putMD(brmd, currentUID, currentVerifyingKey)
	if err != nil {
		return MdID{}, err
	}

	err = j.j.append(brmd.Revision, id)
	if err != nil {
		return MdID{}, err
	}

	return id, nil
}

// flushOne sends the earliest MD in the journal to the given MDServer
// if one exists, and then removes it. Returns whether there was an MD
// that was put.
func (j mdJournal) flushOne(
	ctx context.Context, signer cryptoSigner, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey, mdserver MDServer) (
	flushed bool, err error) {
	earliestID, rmd, pushErr := j.pushEarliestToServer(
		ctx, signer, mdserver)
	if isRevisionConflict(pushErr) && rmd.MergedStatus() == Merged {
		j.log.Debug("Conflict detected %v", pushErr)

		err := j.convertToBranch(
			ctx, signer, currentUID, currentVerifyingKey)
		if err != nil {
			return false, err
		}

		earliestID, rmd, pushErr = j.pushEarliestToServer(
			ctx, signer, mdserver)
	}
	if pushErr != nil {
		return false, pushErr
	}
	if earliestID == (MdID{}) {
		return false, nil
	}

	err = j.j.removeEarliest()
	if err != nil {
		return false, err
	}

	return true, nil
}

func (j mdJournal) clear(currentUID keybase1.UID, bid BranchID) error {
	head, err := j.getHead(currentUID)
	if err != nil {
		return err
	}

	if head.BID != bid {
		// Nothing to do.
		return nil
	}

	return j.j.clear()
}
