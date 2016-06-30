// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"os"
	"reflect"
)

type mdServerBranchJournal struct {
	j diskJournal
}

// An mdServerJournalEntry is just the parameters for a Put
// operation. Fields are exported only for serialization.
type mdServerJournalEntry struct {
	MetadataID MdID
}

func makeMDServerBranchJournal(codec Codec, dir string) mdServerBranchJournal {
	j := makeDiskJournal(codec, dir, reflect.TypeOf(mdServerJournalEntry{}))
	return mdServerBranchJournal{j}
}

func (j mdServerBranchJournal) readEarliestRevision() (
	MetadataRevision, error) {
	o, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return MetadataRevisionUninitialized, nil
	} else if err != nil {
		return MetadataRevisionUninitialized, err
	}
	return MetadataRevision(o), nil
}

func (j mdServerBranchJournal) writeEarliestRevision(r MetadataRevision) error {
	return j.j.writeEarliestOrdinal(journalOrdinal(r))
}

func (j mdServerBranchJournal) readLatestRevision() (
	MetadataRevision, error) {
	o, err := j.j.readLatestOrdinal()
	if os.IsNotExist(err) {
		return MetadataRevisionUninitialized, nil
	} else if err != nil {
		return MetadataRevisionUninitialized, err
	}
	return MetadataRevision(o), nil
}

func (j mdServerBranchJournal) writeLatestRevision(r MetadataRevision) error {
	return j.j.writeLatestOrdinal(journalOrdinal(r))
}

func (j mdServerBranchJournal) readJournalEntry(r MetadataRevision) (
	MdID, error) {
	entry, err := j.j.readJournalEntry(journalOrdinal(r))
	if err != nil {
		return MdID{}, err
	}

	return entry.(mdServerJournalEntry).MetadataID, nil
}

func (j mdServerBranchJournal) writeJournalEntry(
	r MetadataRevision, mdID MdID) error {
	return j.j.writeJournalEntry(
		journalOrdinal(r), mdServerJournalEntry{mdID})
}

func (j mdServerBranchJournal) journalLength() (uint64, error) {
	return j.j.journalLength()
}

func (j mdServerBranchJournal) getHead() (MdID, error) {
	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return MdID{}, err
	} else if latestRevision == MetadataRevisionUninitialized {
		return MdID{}, nil
	}
	mdID, err := j.readJournalEntry(latestRevision)
	if err != nil {
		return MdID{}, err
	}
	return mdID, nil
}

func (j mdServerBranchJournal) getRange(
	start, stop MetadataRevision) ([]MdID, error) {
	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return nil, err
	} else if earliestRevision == MetadataRevisionUninitialized {
		return nil, nil
	}

	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return nil, err
	} else if latestRevision == MetadataRevisionUninitialized {
		return nil, nil
	}

	if start < earliestRevision {
		start = earliestRevision
	}

	if stop > latestRevision {
		stop = latestRevision
	}

	var mdIDs []MdID
	for i := start; i <= stop; i++ {
		mdID, err := j.readJournalEntry(i)
		if err != nil {
			return nil, err
		}
		mdIDs = append(mdIDs, mdID)
	}
	return mdIDs, nil
}

func (j mdServerBranchJournal) put(
	revision MetadataRevision, mdID MdID) error {
	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return err
	} else if latestRevision != MetadataRevisionUninitialized &&
		revision != latestRevision+1 {
		return fmt.Errorf("expected revision %s, got %s",
			latestRevision+1, revision)
	}

	err = j.writeJournalEntry(revision, mdID)
	if err != nil {
		return err
	}

	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return err
	} else if earliestRevision == MetadataRevisionUninitialized {
		err := j.writeEarliestRevision(revision)
		if err != nil {
			return err
		}
	}
	return j.writeLatestRevision(revision)
}
