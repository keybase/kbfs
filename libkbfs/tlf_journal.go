// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
)

type tlfJournal struct {
	codec     Codec
	dir       string
	entryType reflect.Type
}

// makeTlfJournal returns a new tlfJournal for the given directory.
func makeTlfJournal(
	codec Codec, dir string, entryType reflect.Type) tlfJournal {
	return tlfJournal{
		codec:     codec,
		dir:       dir,
		entryType: entryType,
	}
}

// journalOrdinal is the ordinal used for naming journal entries.
//
// TODO: Incorporate metadata revision numbers.
type journalOrdinal uint64

func makeJournalOrdinal(s string) (journalOrdinal, error) {
	if len(s) != 16 {
		return 0, fmt.Errorf("invalid journal ordinal %q", s)
	}
	u, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, err
	}
	return journalOrdinal(u), nil
}

func (o journalOrdinal) String() string {
	return fmt.Sprintf("%016x", uint64(o))
}

// The functions below are for building various paths for the journal.

func (j tlfJournal) earliestPath() string {
	return filepath.Join(j.dir, "EARLIEST")
}

func (j tlfJournal) latestPath() string {
	return filepath.Join(j.dir, "LATEST")
}

func (j tlfJournal) journalEntryPath(o journalOrdinal) string {
	return filepath.Join(j.dir, o.String())
}

// The functions below are for getting and setting the earliest and
// latest ordinals.

func (j tlfJournal) readOrdinal(path string) (
	journalOrdinal, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return makeJournalOrdinal(string(buf))
}

func (j tlfJournal) writeOrdinal(
	path string, o journalOrdinal) error {
	return ioutil.WriteFile(path, []byte(o.String()), 0600)
}

func (j tlfJournal) readEarliestOrdinal() (
	journalOrdinal, error) {
	return j.readOrdinal(j.earliestPath())
}

func (j tlfJournal) writeEarliestOrdinal(o journalOrdinal) error {
	return j.writeOrdinal(j.earliestPath(), o)
}

func (j tlfJournal) readLatestOrdinal() (journalOrdinal, error) {
	return j.readOrdinal(j.latestPath())
}

func (j tlfJournal) writeLatestOrdinal(o journalOrdinal) error {
	return j.writeOrdinal(j.latestPath(), o)
}

// The functions below are for reading and writing journal entries.

func (j tlfJournal) readJournalEntry(o journalOrdinal) (
	interface{}, error) {
	p := j.journalEntryPath(o)
	buf, err := ioutil.ReadFile(p)
	if err != nil {
		return bserverJournalEntry{}, err
	}

	entry := reflect.New(j.entryType)
	err = j.codec.Decode(buf, entry)
	if err != nil {
		return nil, err
	}

	return entry.Elem().Interface(), nil
}

func (j tlfJournal) writeJournalEntry(
	o journalOrdinal, entry interface{}) error {
	entryType := reflect.TypeOf(entry)
	if entryType != j.entryType {
		panic(fmt.Errorf("Expected entry type %v, got %v",
			j.entryType, entryType))
	}

	err := os.MkdirAll(j.dir, 0700)
	if err != nil {
		return err
	}

	p := j.journalEntryPath(o)

	buf, err := j.codec.Encode(entry)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(p, buf, 0600)
}

func (j tlfJournal) appendJournalEntry(entry interface{}) error {
	// TODO: Consider caching the latest ordinal in memory instead
	// of reading it from disk every time.
	var next journalOrdinal
	o, err := j.readLatestOrdinal()
	if os.IsNotExist(err) {
		next = 0
	} else if err != nil {
		return err
	} else {
		next = o + 1
		if next == 0 {
			// Rollover is almost certainly a bug.
			return fmt.Errorf("Ordinal rollover for %+v", entry)
		}
	}

	err = j.writeJournalEntry(next, entry)
	if err != nil {
		return err
	}

	_, err = j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		j.writeEarliestOrdinal(next)
	} else if err != nil {
		return err
	}
	return j.writeLatestOrdinal(next)
}

func (j tlfJournal) journalLength() (uint64, error) {
	first, err := j.readEarliestOrdinal()
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	last, err := j.readLatestOrdinal()
	if err != nil {
		return 0, err
	}
	return uint64(last - first + 1), nil
}
