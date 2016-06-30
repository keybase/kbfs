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
	j     diskJournal
	mdIDs []MdID
}

// An mdServerJournalEntry is just the parameters for a Put
// operation. Fields are exported only for serialization.
type mdServerJournalEntry struct {
	Revision   MetadataRevision
	MetadataID MdID
}

func makeMDServerBranchJournal(
	codec Codec, dir string) (*mdServerBranchJournal, error) {
	j := makeDiskJournal(codec, dir, reflect.TypeOf(mdServerJournalEntry{}))
	journal := mdServerBranchJournal{
		j: j,
	}

	mdIDs, err := journal.readJournal()
	if err != nil {
		return nil, err
	}

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
func (j *mdServerBranchJournal) readJournal() ([]MdID, error) {
	var mdIDs []MdID

	first, err := j.j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return mdIDs, nil
	} else if err != nil {
		return nil, err
	}
	last, err := j.j.readLatestOrdinal()
	if err != nil {
		return nil, err
	}

	var initialRev MetadataRevision

	for i := first; i <= last; i++ {
		e, err := j.readJournalEntry(i)
		if err != nil {
			return nil, err
		}

		if i == first {
			initialRev = e.Revision
		} else {
			expectedRevision := MetadataRevision(int(initialRev) + len(mdIDs))
			if expectedRevision != e.Revision {
				return nil, fmt.Errorf(
					"Revision mismatch: expected %s, got %s",
					expectedRevision, e.Revision)
			}
		}

		mdIDs = append(mdIDs, e.MetadataID)
	}
	return mdIDs, nil
}

func (j *mdServerBranchJournal) writeJournalEntry(
	o journalOrdinal, entry mdServerJournalEntry) error {
	return j.j.writeJournalEntry(o, entry)
}

func (j *mdServerBranchJournal) appendJournalEntry(
	revision MetadataRevision, mdID MdID) error {
	return j.j.appendJournalEntry(mdServerJournalEntry{
		Revision:   revision,
		MetadataID: mdID,
	})
}

func (j *mdServerBranchJournal) journalLength() (uint64, error) {
	return j.j.journalLength()
}

func (j *mdServerBranchJournal) put(
	revision MetadataRevision, mdID MdID) error {
	err := j.appendJournalEntry(revision, mdID)
	if err != nil {
		return err
	}
	j.mdIDs = append(j.mdIDs, mdID)
	return nil
}

func (j *mdServerBranchJournal) checkJournal() error {
	mdIDs, err := j.readJournal()
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(mdIDs, j.mdIDs) {
		return fmt.Errorf("mdIDs = %v != s.mdIDs = %v", mdIDs, j.mdIDs)
	}

	return nil
}
