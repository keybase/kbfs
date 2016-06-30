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
	j               diskJournal
	initialRevision MetadataRevision
	mdIDs           []MdID
}

// An mdServerJournalEntry is just the parameters for a Put
// operation. Fields are exported only for serialization.
type mdServerJournalEntry struct {
	MetadataID MdID
}

func makeMDServerBranchJournal(
	codec Codec, dir string) (*mdServerBranchJournal, error) {
	j := makeDiskJournal(codec, dir, reflect.TypeOf(mdServerJournalEntry{}))
	journal := mdServerBranchJournal{
		j: j,
	}

	initialRevision, mdIDs, err := journal.readJournal()
	if err != nil {
		return nil, err
	}

	journal.initialRevision = initialRevision
	journal.mdIDs = mdIDs

	return &journal, nil
}

func (j *mdServerBranchJournal) readJournalEntry(o journalOrdinal) (
	mdServerJournalEntry, error) {
	entry, err := j.j.readJournalEntry(o)
	if err != nil {
		return mdServerJournalEntry{}, err
	}

	return entry.(mdServerJournalEntry), nil
}

// readJournal reads the journal and returns an array of all the MdIDs
// in the journal.
func (j *mdServerBranchJournal) readJournal() (MetadataRevision, []MdID, error) {

	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return MetadataRevisionUninitialized, nil, nil
	} else if err != nil {
		return MetadataRevisionUninitialized, nil, err
	}
	last, err := j.j.readLatestOrdinal()
	if err != nil {
		return MetadataRevisionUninitialized, nil, err
	}

	var mdIDs []MdID
	for i := first; i <= last; i++ {
		e, err := j.readJournalEntry(i)
		if err != nil {
			return MetadataRevisionUninitialized, nil, err
		}
		mdIDs = append(mdIDs, e.MetadataID)
	}
	return MetadataRevision(first), mdIDs, nil
}

func (j *mdServerBranchJournal) writeJournalEntry(
	o journalOrdinal, entry mdServerJournalEntry) error {
	return j.j.writeJournalEntry(o, entry)
}

func (j *mdServerBranchJournal) journalLength() (uint64, error) {
	return j.j.journalLength()
}

func (j *mdServerBranchJournal) put(
	revision MetadataRevision, mdID MdID) error {
	if j.initialRevision != MetadataRevisionUninitialized {
		expectedRevision := j.initialRevision + MetadataRevision(len(j.mdIDs))
		if expectedRevision != revision {
			return fmt.Errorf("expected revision %s, got %s",
				expectedRevision, revision)
		}
	}

	err := j.j.writeJournalEntry(journalOrdinal(revision),
		mdServerJournalEntry{
			MetadataID: mdID,
		})
	if err != nil {
		return err
	}

	if j.initialRevision == MetadataRevisionUninitialized {
		err := j.j.writeEarliestOrdinal(journalOrdinal(revision))
		if err != nil {
			return err
		}
	}
	err = j.j.writeLatestOrdinal(journalOrdinal(revision))
	if err != nil {
		return err
	}

	if j.initialRevision == MetadataRevisionUninitialized {
		j.initialRevision = revision
	}
	j.mdIDs = append(j.mdIDs, mdID)
	return nil
}

func (j *mdServerBranchJournal) checkJournal() error {
	initialRevision, mdIDs, err := j.readJournal()
	if err != nil {
		return err
	}

	if initialRevision != j.initialRevision {
		return fmt.Errorf("initialRevision = %v != s.initialRevision= %v",
			initialRevision, j.initialRevision)
	}
	if !reflect.DeepEqual(mdIDs, j.mdIDs) {
		return fmt.Errorf("mdIDs = %v != s.mdIDs = %v", mdIDs, j.mdIDs)
	}

	return nil
}
