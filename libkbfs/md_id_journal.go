// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"reflect"

	"github.com/keybase/go-codec/codec"
	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/pkg/errors"
)

// An mdIDJournal wraps a diskJournal to provide a persistent list of
// MdIDs (with possible other fields in the future) with sequential
// MetadataRevisions for a single branch.
//
// Like diskJournal, this type assumes that the directory passed into
// makeMdIDJournal isn't used by anything else, and that all
// synchronization is done at a higher level.
//
// TODO: Write unit tests for this. For now, we're relying on
// md_journal.go's unit tests.
type mdIDJournal struct {
	j diskJournal
}

// An mdIDJournalEntry is an MdID and a boolean describing whether
// this entry was the result of a local squash. Make sure that new
// fields don't depend on the ID or `isLocalSquash`, as the mdJournal
// may change these when converting to a branch.  Note that
// `isLocalSquash` may only be true for entries in a continuous prefix
// of the id journal; once there is one entry with `isLocalSquash =
// false`, it will be the same in all the remaining entries.
type mdIDJournalEntry struct {
	ID MdID
	// IsLocalSquash is true when this MD is the result of
	// squashing some other local MDs.
	IsLocalSquash bool `codec:",omitempty"`
	// WKBNew is true when the writer key bundle for this MD is
	// new and has to be pushed to the server. This is always
	// false for MDv2.
	WKBNew bool `codec:",omitempty"`
	// RKBNew is true when the reader key bundle for this MD is
	// new and has to be pushed to the server. This is always
	// false for MDv2.
	RKBNew bool `codec:",omitempty"`

	codec.UnknownFieldSetHandler
}

func makeMdIDJournal(codec kbfscodec.Codec, dir string) (mdIDJournal, error) {
	j, err :=
		makeDiskJournal(codec, dir, reflect.TypeOf(mdIDJournalEntry{}))
	if err != nil {
		return mdIDJournal{}, err
	}
	return mdIDJournal{j}, nil
}

func ordinalToRevision(o journalOrdinal) (MetadataRevision, error) {
	r := MetadataRevision(o)
	if r < MetadataRevisionInitial {
		return MetadataRevisionUninitialized, errors.Errorf(
			"Cannot convert ordinal %s to a MetadataRevision", o)
	}
	return r, nil
}

func revisionToOrdinal(r MetadataRevision) (journalOrdinal, error) {
	if r < MetadataRevisionInitial {
		return journalOrdinal(0), errors.Errorf(
			"Cannot convert revision %s to an ordinal", r)
	}
	return journalOrdinal(r), nil
}

// TODO: Consider caching the values returned by the read functions
// below in memory.

func (j mdIDJournal) readEarliestRevision() (MetadataRevision, error) {
	o, err := j.j.readEarliestOrdinal()
	if ioutil.IsNotExist(err) {
		return MetadataRevisionUninitialized, nil
	} else if err != nil {
		return MetadataRevisionUninitialized, err
	}
	return ordinalToRevision(o)
}

func (j mdIDJournal) writeEarliestRevision(r MetadataRevision) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	return j.j.writeEarliestOrdinal(o)
}

func (j mdIDJournal) readLatestRevision() (MetadataRevision, error) {
	o, err := j.j.readLatestOrdinal()
	if ioutil.IsNotExist(err) {
		return MetadataRevisionUninitialized, nil
	} else if err != nil {
		return MetadataRevisionUninitialized, err
	}
	return ordinalToRevision(o)
}

func (j mdIDJournal) writeLatestRevision(r MetadataRevision) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	return j.j.writeLatestOrdinal(o)
}

func (j mdIDJournal) readJournalEntry(r MetadataRevision) (
	mdIDJournalEntry, error) {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return mdIDJournalEntry{}, err
	}
	e, err := j.j.readJournalEntry(o)
	if err != nil {
		return mdIDJournalEntry{}, err
	}

	return e.(mdIDJournalEntry), nil
}

// All functions below are public functions.

func (j mdIDJournal) length() (uint64, error) {
	return j.j.length()
}

func (j mdIDJournal) end() (MetadataRevision, error) {
	last, err := j.readLatestRevision()
	if err != nil {
		return MetadataRevisionUninitialized, err
	}
	if last == MetadataRevisionUninitialized {
		return MetadataRevisionUninitialized, nil
	}

	return last + 1, nil
}

func (j mdIDJournal) getEarliestEntry() (
	entry mdIDJournalEntry, exists bool, err error) {
	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return mdIDJournalEntry{}, false, err
	} else if earliestRevision == MetadataRevisionUninitialized {
		return mdIDJournalEntry{}, false, nil
	}
	entry, err = j.readJournalEntry(earliestRevision)
	if err != nil {
		return mdIDJournalEntry{}, false, err
	}
	return entry, true, err
}

func (j mdIDJournal) getLatestEntry() (
	entry mdIDJournalEntry, exists bool, err error) {
	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return mdIDJournalEntry{}, false, err
	} else if latestRevision == MetadataRevisionUninitialized {
		return mdIDJournalEntry{}, false, nil
	}
	entry, err = j.readJournalEntry(latestRevision)
	if err != nil {
		return mdIDJournalEntry{}, false, err
	}
	return entry, true, err
}

func (j mdIDJournal) getEntryRange(start, stop MetadataRevision) (
	MetadataRevision, []mdIDJournalEntry, error) {
	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return MetadataRevisionUninitialized, nil, err
	} else if earliestRevision == MetadataRevisionUninitialized {
		return MetadataRevisionUninitialized, nil, nil
	}

	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return MetadataRevisionUninitialized, nil, err
	} else if latestRevision == MetadataRevisionUninitialized {
		return MetadataRevisionUninitialized, nil, nil
	}

	if start < earliestRevision {
		start = earliestRevision
	}

	if stop > latestRevision {
		stop = latestRevision
	}

	if stop < start {
		return MetadataRevisionUninitialized, nil, nil
	}

	var entries []mdIDJournalEntry
	for i := start; i <= stop; i++ {
		entry, err := j.readJournalEntry(i)
		if err != nil {
			return MetadataRevisionUninitialized, nil, err
		}
		entries = append(entries, entry)
	}
	return start, entries, nil
}

func (j mdIDJournal) replaceHead(entry mdIDJournalEntry) error {
	o, err := j.j.readLatestOrdinal()
	if err != nil {
		return err
	}
	return j.j.writeJournalEntry(o, entry)
}

func (j mdIDJournal) append(r MetadataRevision, entry mdIDJournalEntry) error {
	o, err := revisionToOrdinal(r)
	if err != nil {
		return err
	}
	_, err = j.j.appendJournalEntry(&o, entry)
	return err
}

func (j mdIDJournal) removeEarliest() (empty bool, err error) {
	return j.j.removeEarliest()
}

func (j mdIDJournal) clear() error {
	return j.j.clear()
}

func (j mdIDJournal) clearFrom(revision MetadataRevision) error {
	earliestRevision, err := j.readEarliestRevision()
	if err != nil {
		return err
	}

	if revision < earliestRevision {
		return errors.Errorf("Cannot call clearFrom with revision %s < %s",
			revision, earliestRevision)
	}

	if revision == earliestRevision {
		return j.clear()
	}

	latestRevision, err := j.readLatestRevision()
	if err != nil {
		return err
	}

	err = j.writeLatestRevision(revision - 1)
	if err != nil {
		return err
	}

	o, err := revisionToOrdinal(revision)
	if err != nil {
		return err
	}

	latestOrdinal, err := revisionToOrdinal(latestRevision)
	if err != nil {
		return err
	}

	for ; o <= latestOrdinal; o++ {
		p := j.j.journalEntryPath(o)
		err = ioutil.Remove(p)
		if err != nil {
			return err
		}
	}

	return nil
}

// Note that since diskJournal.move takes a pointer receiver, so must
// this.
func (j *mdIDJournal) move(newDir string) (oldDir string, err error) {
	return j.j.move(newDir)
}
