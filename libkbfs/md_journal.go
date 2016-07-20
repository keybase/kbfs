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

	// Set only when the journal is empty.
	headMdID MdID
	head     *BareRootMetadata
}

func makeMDJournal(codec Codec, crypto cryptoPure, dir string) *mdJournal {
	journal := &mdJournal{
		codec:  codec,
		crypto: crypto,
		dir:    dir,
		j:      makeMdIDJournal(codec, dir),
	}
	return journal
}

// The functions below are for building various paths.

func (s *mdJournal) journalPath() string {
	return filepath.Join(s.dir, "md_journal")
}

func (s *mdJournal) mdsPath() string {
	return filepath.Join(s.dir, "mds")
}

func (s *mdJournal) mdPath(id MdID) string {
	idStr := id.String()
	return filepath.Join(s.mdsPath(), idStr[:4], idStr[4:])
}

// getDataLocked verifies the MD data (but not the signature) for the
// given ID and returns it.
//
// TODO: Verify signature?
func (s *mdJournal) getMD(id MdID) (*BareRootMetadata, error) {
	// Read file.

	path := s.mdPath(id)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rmd BareRootMetadata
	err = s.codec.Decode(data, &rmd)
	if err != nil {
		return nil, err
	}

	// Check integrity.

	mdID, err := s.crypto.MakeMdID(&rmd)
	if err != nil {
		return nil, err
	}

	if id != mdID {
		return nil, fmt.Errorf(
			"Metadata ID mismatch: expected %s, got %s", id, mdID)
	}

	return &rmd, nil
}

func (s *mdJournal) putMD(rmd *BareRootMetadata) error {
	id, err := s.crypto.MakeMdID(rmd)
	if err != nil {
		return err
	}

	_, err = s.getMD(id)
	if os.IsNotExist(err) {
		// Continue on.
	} else if err != nil {
		return err
	} else {
		// Entry exists, so nothing else to do.
		return nil
	}

	path := s.mdPath(id)

	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return err
	}

	buf, err := s.codec.Encode(rmd)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, buf, 0600)
}

func (s *mdJournal) getHead() (mdID MdID, rmd *BareRootMetadata, err error) {
	headID, err := s.j.getLatest()
	if err != nil {
		return MdID{}, nil, err
	}
	if headID == (MdID{}) {
		return s.headMdID, s.head, nil
	}
	rmd, err = s.getMD(headID)
	if err != nil {
		return MdID{}, nil, err
	}
	return headID, rmd, nil
}

func (s *mdJournal) checkGetParams(currentUID keybase1.UID) error {
	_, head, err := s.getHead()
	if err != nil {
		return err
	}

	if head != nil {
		ok, err := isReader(currentUID, head)
		if err != nil {
			return err
		}
		if !ok {
			// TODO: Better error?
			return MDServerErrorUnauthorized{}
		}
	}

	return nil
}

func (s *mdJournal) convertToBranch(log logger.Logger) error {
	_, head, err := s.getHead()
	if err != nil {
		return err
	}

	if head.BID != NullBranchID {
		return fmt.Errorf("convertToBranch called with BID=%s", head.BID)
	}

	earliestRevision, err := s.j.readEarliestRevision()
	if err != nil {
		return err
	}

	latestRevision, err := s.j.readLatestRevision()
	if err != nil {
		return err
	}

	log.Debug("rewriting MDs %s to %s", earliestRevision, latestRevision)

	_, allMdIDs, err := s.j.getRange(earliestRevision, latestRevision)
	if err != nil {
		return err
	}

	bid, err := s.crypto.MakeRandomBranchID()
	if err != nil {
		return err
	}

	log.Debug("New branch ID=%s", bid)

	// TODO: Do the below atomically.

	var prevID MdID

	for i, id := range allMdIDs {
		brmd, err := s.getMD(id)
		if err != nil {
			return err
		}
		brmd.WFlags |= MetadataFlagUnmerged
		brmd.BID = bid

		log.Debug("Old prev root of rev=%s is %s", brmd.Revision, brmd.PrevRoot)

		if i > 0 {
			log.Debug("Changing prev root of rev=%s to %s", brmd.Revision, prevID)
			brmd.PrevRoot = prevID
		}

		err = s.putMD(brmd)
		if err != nil {
			return err
		}

		o, err := revisionToOrdinal(brmd.Revision)
		if err != nil {
			return err
		}

		newID, err := s.crypto.MakeMdID(brmd)
		if err != nil {
			return err
		}

		err = s.j.j.writeJournalEntry(o, newID)
		if err != nil {
			return err
		}

		prevID = newID

		log.Debug("Changing ID for rev=%s from %s to %s",
			brmd.Revision, id, newID)
	}

	return err
}

func (s *mdJournal) pushEarliestToServer(
	ctx context.Context, signer cryptoSigner, mdserver MDServer,
	log logger.Logger) (MdID, *BareRootMetadata, error) {
	earliestID, err := s.j.getEarliest()
	if err != nil {
		return MdID{}, nil, err
	}
	if earliestID == (MdID{}) {
		return MdID{}, nil, nil
	}

	rmd, err := s.getMD(earliestID)
	if err != nil {
		return MdID{}, nil, err
	}

	log.Debug("Flushing MD put id=%s, rev=%s", earliestID, rmd.Revision)

	var rmds RootMetadataSigned
	rmds.MD = *rmd
	err = signMD(ctx, s.codec, signer, &rmds)
	if err != nil {
		return MdID{}, nil, err
	}
	err = mdserver.Put(ctx, &rmds)
	if err != nil {
		return earliestID, rmd, err
	}

	return earliestID, rmd, nil
}

// All functions below are public functions.

func (s *mdJournal) length() (uint64, error) {
	return s.j.length()
}

func (s *mdJournal) get(currentUID keybase1.UID) (*BareRootMetadata, error) {
	err := s.checkGetParams(currentUID)
	if err != nil {
		return nil, err
	}

	_, rmd, err := s.getHead()
	if err != nil {
		return nil, err
	}
	return rmd, nil
}

func (s *mdJournal) getRange(
	currentUID keybase1.UID, start, stop MetadataRevision) (
	[]*BareRootMetadata, error) {
	err := s.checkGetParams(currentUID)
	if err != nil {
		return nil, err
	}

	realStart, mdIDs, err := s.j.getRange(start, stop)
	if err != nil {
		return nil, err
	}
	var rmds []*BareRootMetadata
	for i, mdID := range mdIDs {
		expectedRevision := realStart + MetadataRevision(i)
		rmd, err := s.getMD(mdID)
		if err != nil {
			return nil, err
		}
		if expectedRevision != rmd.Revision {
			panic(fmt.Errorf("expected revision %v, got %v",
				expectedRevision, rmd.Revision))
		}
		rmds = append(rmds, rmd)
	}

	return rmds, nil
}

func (s *mdJournal) setHead(mdID MdID, rmd *BareRootMetadata) error {
	length, err := s.length()
	if err != nil {
		return err
	}
	if length != 0 {
		return errors.New("setHead called when not empty")
	}

	s.headMdID = mdID
	s.head = rmd
	return nil
}

// MDJournalConflictError is an error that is returned when a put
// detects a rewritten journal.
type MDJournalConflictError struct{}

func (e MDJournalConflictError) Error() string {
	return "MD journal conflict error"
}

func (s *mdJournal) put(
	ctx context.Context, signer cryptoSigner, ekg encryptionKeyGetter,
	currentUID keybase1.UID, rmd *RootMetadata) (MdID, error) {
	headID, head, err := s.getHead()
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

	// Check permissions

	// TODO: Figure out nil case.
	if head != nil {
		ok, err := isWriterOrValidRekey(s.codec, currentUID, head, &rmd.BareRootMetadata)
		if err != nil {
			return MdID{}, err
		}
		if !ok {
			// TODO: Better error?
			return MdID{}, MDServerErrorUnauthorized{}
		}
	}

	if mStatus == Merged && head != nil && head.BID != NullBranchID {
		return MdID{}, MDJournalConflictError{}
	}

	// Consistency checks
	if head == nil {
		if rmd.Revision != MetadataRevisionInitial {
			return MdID{}, fmt.Errorf("Expected initial revision")
		}

		if rmd.PrevRoot != (MdID{}) {
			return MdID{}, fmt.Errorf("Expected no PrevRoot for initial revision")
		}

		if rmd.BID != NullBranchID {
			return MdID{}, fmt.Errorf("Expected no branch ID for initial revision")
		}
	} else {
		err = head.CheckValidSuccessorForServer(
			headID, &rmd.BareRootMetadata)
		if err != nil {
			return MdID{}, err
		}
	}

	var brmd BareRootMetadata
	err = encryptMDPrivateData(
		ctx, s.codec, s.crypto, signer, ekg,
		currentUID, rmd.ReadOnly(), &brmd)
	if err != nil {
		return MdID{}, err
	}

	err = s.putMD(&brmd)
	if err != nil {
		return MdID{}, err
	}

	id, err := s.crypto.MakeMdID(&brmd)
	if err != nil {
		return MdID{}, err
	}

	err = s.j.append(brmd.Revision, id)
	if err != nil {
		return MdID{}, err
	}

	return id, nil
}

func (s *mdJournal) flushOne(
	ctx context.Context, signer cryptoSigner, mdserver MDServer,
	log logger.Logger) (bool, error) {
	earliestID, rmd, pushErr := s.pushEarliestToServer(
		ctx, signer, mdserver, log)
	if isRevisionConflict(pushErr) && rmd.MergedStatus() == Merged {
		log.Debug("Conflict detected %v", pushErr)

		err := s.convertToBranch(log)
		if err != nil {
			return false, err
		}

		earliestID, rmd, pushErr = s.pushEarliestToServer(
			ctx, signer, mdserver, log)
	}
	if pushErr != nil {
		return false, pushErr
	}
	if earliestID == (MdID{}) {
		return false, nil
	}

	cleared, err := s.j.removeEarliest()
	if err != nil {
		return false, err
	}

	if cleared {
		s.headMdID = earliestID
		s.head = rmd
	}

	return true, nil
}

func (s *mdJournal) clear() error {
	err := s.j.clear()
	if err != nil {
		return err
	}
	s.headMdID = MdID{}
	s.head = nil
	return nil
}
