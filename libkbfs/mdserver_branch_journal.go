// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"os"
	"reflect"
)

// An mdServerBranchJournal wraps a diskJournal to provide a
// persistent list of MdIDs with sequential MetadataRevisions for a
// single branch.
//
// TODO: Consider future-proofing this in case we want to journal
// other stuff besides metadata puts. But doing so would be difficult,
// since then we would require the ordinals to be something other than
// MetadataRevisions.
type mdServerBranchJournal struct {
	j diskJournal
}

func makeMDServerBranchJournal(codec IFCERFTCodec, dir string) mdServerBranchJournal {
	j := makeDiskJournal(codec, dir, reflect.TypeOf(IFCERFTMdID{}))
	return mdServerBranchJournal{j}
}

func ordinalToRevision(o journalOrdinal) (IFCERFTMetadataRevision, error) {
	r := IFCERFTMetadataRevision(o)
	if r < IFCERFTMetadataRevisionInitial {
		return IFCERFTMetadataRevisionUninitialized,
			fmt.Errorf("Cannot convert ordinal %s to a MetadataRevision", o)
	}
	return r, nil
}

func revisionToOrdinal(r IFCERFTMetadataRevision) (journalOrdinal, error) {
	if r < IFCERFTMetadataRevisionInitial {
		return journalOrdinal(0),
			fmt.Errorf("Cannot convert revision %s to an ordinal", r)
	}
	return journalOrdinal(r), nil
}

// TODO: Consider caching the values returned by the read functions
// below in memory.

func (j mdServerBranchJournal) readEarliestRevision() (
	IFCERFTMetadataRevision, error) {
	o, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return IFCERFTMetadataRevisionUninitialized, nil
	} else if err != nil {
		return IFCERFTMetadataRevisionUninitialized, err
	}
	return ordinalToRevision(o)
}

func (j mdServerBranchJournal) writeEarliestRevision(r IFCERFTMetadataRevision) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	return j.j.writeEarliestOrdinal(o)
}

func (j mdServerBranchJournal) readLatestRevision() (
	IFCERFTMetadataRevision, error) {
	o, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		return IFCERFTMetadataRevisionUninitialized, nil
	} else if err != nil {
		return IFCERFTMetadataRevisionUninitialized, err
	}
	return ordinalToRevision(o)
}

func (j mdServerBranchJournal) writeLatestRevision(r IFCERFTMetadataRevision) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	return j.j.writeLatestOrdinal(o)
}

func (j mdServerBranchJournal) readMdID(r IFCERFTMetadataRevision) (IFCERFTMdID, error) {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return IFCERFTMdID{}, err
	}
	e, err := j.j.readJournalEntry(o)
	if err != nil {
		return IFCERFTMdID{}, err
	}

	// TODO: Validate MdID?
	return e.(IFCERFTMdID), nil
}

// All functions below are public functions.

func (j mdServerBranchJournal) journalLength() (uint64, error) {
	return j.j.journalLength()
}

func (j mdServerBranchJournal) getHead() (IFCERFTMdID, error) {
	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return IFCERFTMdID{}, err
	} else if latestRevision == IFCERFTMetadataRevisionUninitialized {
		return IFCERFTMdID{}, nil
	}
	return j.readMdID(latestRevision)
}

func (j mdServerBranchJournal) getRange(
	start, stop IFCERFTMetadataRevision) (IFCERFTMetadataRevision, []IFCERFTMdID, error) {
	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return IFCERFTMetadataRevisionUninitialized, nil, err
	} else if earliestRevision == IFCERFTMetadataRevisionUninitialized {
		return IFCERFTMetadataRevisionUninitialized, nil, nil
	}

	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return IFCERFTMetadataRevisionUninitialized, nil, err
	} else if latestRevision == IFCERFTMetadataRevisionUninitialized {
		return IFCERFTMetadataRevisionUninitialized, nil, nil
	}

	if start < earliestRevision {
		start = earliestRevision
	}

	if stop > latestRevision {
		stop = latestRevision
	}

	if stop < start {
		return IFCERFTMetadataRevisionUninitialized, nil, nil
	}

	var mdIDs []IFCERFTMdID
	for i := start; i <= stop; i++ {
		mdID, err := j.readMdID(i)
		if err != nil {
			return IFCERFTMetadataRevisionUninitialized, nil, err
		}
		mdIDs = append(mdIDs, mdID)
	}
	return start, mdIDs, nil
}

func (j mdServerBranchJournal) append(r IFCERFTMetadataRevision, mdID IFCERFTMdID) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	return j.j.appendJournalEntry(&o, mdID)
}
