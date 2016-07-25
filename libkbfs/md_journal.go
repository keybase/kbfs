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
type mdJournal struct {
	codec  Codec
	crypto cryptoPure
	dir    string

	j mdIDJournal
}

func makeMDJournal(codec Codec, crypto cryptoPure, dir string) mdJournal {
	journalDir := filepath.Join(dir, "md_journal")

	journal := mdJournal{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
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

func (j mdJournal) getHead() (mdID MdID, rmd *BareRootMetadata, err error) {
	headID, err := j.j.getLatest()
	if err != nil {
		return MdID{}, nil, err
	}
	if headID == (MdID{}) {
		return MdID{}, nil, nil
	}
	rmd, err = j.getMD(headID)
	if err != nil {
		return MdID{}, nil, err
	}
	return headID, rmd, nil
}

func (j mdJournal) checkGetParams(currentUID keybase1.UID) error {
	_, head, err := j.getHead()
	if err != nil {
		return err
	}

	if head != nil {
		ok, err := isReader(currentUID, head)
		if err != nil {
			return err
		}
		if !ok {
			// TODO: Use a non-server error.
			return MDServerErrorUnauthorized{}
		}
	}

	return nil
}

func (j mdJournal) convertToBranch(
	ctx context.Context, log logger.Logger, signer cryptoSigner,
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey) error {
	_, head, err := j.getHead()
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

	latestRevision, err := j.j.readLatestRevision()
	if err != nil {
		return err
	}

	log.Debug("rewriting MDs %s to %s", earliestRevision, latestRevision)

	_, allMdIDs, err := j.j.getRange(earliestRevision, latestRevision)
	if err != nil {
		return err
	}

	bid, err := j.crypto.MakeRandomBranchID()
	if err != nil {
		return err
	}

	log.Debug("New branch ID=%s", bid)

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

		log.Debug("Old prev root of rev=%s is %s",
			brmd.Revision, brmd.PrevRoot)

		if i > 0 {
			log.Debug("Changing prev root of rev=%s to %s",
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

		log.Debug("Changing ID for rev=%s from %s to %s",
			brmd.Revision, id, newID)
	}

	return err
}

func (j mdJournal) pushEarliestToServer(
	ctx context.Context, log logger.Logger, signer cryptoSigner,
	mdserver MDServer) (MdID, *BareRootMetadata, error) {
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

	log.Debug("Flushing MD put id=%s, rev=%s", earliestID, rmd.Revision)

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

func (j mdJournal) get(currentUID keybase1.UID) (*BareRootMetadata, error) {
	err := j.checkGetParams(currentUID)
	if err != nil {
		return nil, err
	}

	_, rmd, err := j.getHead()
	if err != nil {
		return nil, err
	}
	return rmd, nil
}

// ImmutableBareRootMetadata is a BRMD with an MdID.
type ImmutableBareRootMetadata struct {
	*BareRootMetadata
	mdID MdID
}

// MakeImmutableBareRootMetadata makes a new ImmutableBareRootMetadata
// from the given args.
func MakeImmutableBareRootMetadata(
	rmd *BareRootMetadata, mdID MdID) ImmutableBareRootMetadata {
	if mdID == (MdID{}) {
		panic("zero mdID passed to MakeImmutableBareRootMetadata")
	}
	return ImmutableBareRootMetadata{rmd, mdID}
}

func (j mdJournal) getRange(
	currentUID keybase1.UID, start, stop MetadataRevision) (
	[]ImmutableBareRootMetadata, error) {
	err := j.checkGetParams(currentUID)
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

func (j mdJournal) put(
	ctx context.Context, signer cryptoSigner, ekg encryptionKeyGetter,
	rmd *RootMetadata, currentUID keybase1.UID,
	currentVerifyingKey VerifyingKey) (MdID, error) {
	headID, head, err := j.getHead()
	if err != nil {
		return MdID{}, err
	}

	mStatus := rmd.MergedStatus()
	bid := rmd.BID

	if (mStatus == Unmerged) && (bid == NullBranchID) && (head != nil) {
		rmd.BID = head.BID
		rmd.PrevRoot = headID
		bid = rmd.BID
	}

	if (mStatus == Merged) != (bid == NullBranchID) {
		return MdID{}, errors.New("Invalid branch ID")
	}

	// Check permissions and consistency with head, if it exists.
	if head != nil {
		ok, err := isWriterOrValidRekey(
			j.codec, currentUID, head, &rmd.BareRootMetadata)
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
			headID, &rmd.BareRootMetadata)
		if err != nil {
			return MdID{}, err
		}
	}

	var brmd BareRootMetadata
	err = encryptMDPrivateData(
		ctx, j.codec, j.crypto, signer, ekg,
		currentUID, rmd.ReadOnly(), &brmd)
	if err != nil {
		return MdID{}, err
	}

	id, err := j.putMD(&brmd, currentUID, currentVerifyingKey)
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
	ctx context.Context, log logger.Logger, signer cryptoSigner,
	currentUID keybase1.UID, currentVerifyingKey VerifyingKey,
	mdserver MDServer) (flushed bool, err error) {
	earliestID, rmd, pushErr := j.pushEarliestToServer(
		ctx, log, signer, mdserver)
	if isRevisionConflict(pushErr) && rmd.MergedStatus() == Merged {
		log.Debug("Conflict detected %v", pushErr)

		err := j.convertToBranch(
			ctx, log, signer, currentUID, currentVerifyingKey)
		if err != nil {
			return false, err
		}

		earliestID, rmd, pushErr = j.pushEarliestToServer(
			ctx, log, signer, mdserver)
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

func (j mdJournal) clear() error {
	return j.j.clear()
}
