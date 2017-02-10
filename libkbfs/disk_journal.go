// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/keybase/kbfs/ioutil"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/pkg/errors"
)

// diskJournal stores an ordered list of entries.
//
// The directory layout looks like:
//
// dir/EARLIEST
// dir/LATEST
// dir/0...000
// dir/0...001
// dir/0...fff
//
// Each file in dir is named with an ordinal and contains a generic
// serializable entry object. The files EARLIEST and LATEST point to
// the earliest and latest valid ordinal, respectively.
//
// This class is not goroutine-safe; it assumes that all
// synchronization is done at a higher level.
//
// TODO: Do all high-level operations atomically on the file-system
// level.
//
// TODO: Make IO ops cancellable.
type diskJournal struct {
	codec     kbfscodec.Codec
	dir       string
	entryType reflect.Type
}

// makeDiskJournal returns a new diskJournal for the given directory.
func makeDiskJournal(
	codec kbfscodec.Codec, dir string, entryType reflect.Type) diskJournal {
	return diskJournal{
		codec:     codec,
		dir:       dir,
		entryType: entryType,
	}
}

// journalOrdinal is the ordinal used for naming journal entries.
type journalOrdinal uint64

func makeJournalOrdinal(s string) (journalOrdinal, error) {
	if len(s) != 16 {
		return 0, errors.Errorf("invalid journal ordinal %q", s)
	}
	u, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to parse %q", s)
	}
	return journalOrdinal(u), nil
}

func (o journalOrdinal) String() string {
	return fmt.Sprintf("%016x", uint64(o))
}

// The functions below are for building various paths for the journal.

func (j diskJournal) earliestPath() string {
	return filepath.Join(j.dir, "EARLIEST")
}

func (j diskJournal) latestPath() string {
	return filepath.Join(j.dir, "LATEST")
}

func (j diskJournal) journalEntryPath(o journalOrdinal) string {
	return filepath.Join(j.dir, o.String())
}

// The functions below are for reading and writing the earliest and
// latest ordinals. The read functions may return an error for which
// ioutil.IsNotExist() returns true.

func (j diskJournal) readOrdinal(path string) (journalOrdinal, error) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return makeJournalOrdinal(string(buf))
}

func (j diskJournal) writeOrdinal(
	path string, o journalOrdinal) error {
	// Don't use ioutil.WriteFile because it truncates the file first,
	// and if there's a crash it will leave the journal in an unknown
	// state.  TODO: it's technically possible a partial write could
	// lead to corrupted ordinal.  If we ever get rid of the block
	// journal (which would greatly reduce the number of times this is
	// called) it might make sense to do an atomic rename here
	// instead.
	f, err := ioutil.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = errors.WithStack(closeErr)
		}
	}()
	// Overwrite whatever data is there.  This works as long as the
	// ordinals are a constant size.
	s := o.String()
	n, err := f.WriteString(s)
	if err != nil {
		return errors.WithStack(err)
	} else if n < len(s) {
		return errors.WithStack(io.ErrShortWrite)
	}
	return nil
}

func (j diskJournal) readEarliestOrdinal() (
	journalOrdinal, error) {
	return j.readOrdinal(j.earliestPath())
}

func (j diskJournal) writeEarliestOrdinal(o journalOrdinal) error {
	return j.writeOrdinal(j.earliestPath(), o)
}

func (j diskJournal) readLatestOrdinal() (journalOrdinal, error) {
	return j.readOrdinal(j.latestPath())
}

func (j diskJournal) writeLatestOrdinal(o journalOrdinal) error {
	return j.writeOrdinal(j.latestPath(), o)
}

func (j diskJournal) clearOrdinals() error {
	earliestOrdinal, err := j.readEarliestOrdinal()
	if err != nil {
		return err
	}
	latestOrdinal, err := j.readLatestOrdinal()
	if err != nil {
		return err
	}

	err = ioutil.Remove(j.earliestPath())
	if err != nil {
		return err
	}
	err = ioutil.Remove(j.latestPath())
	if err != nil {
		return err
	}

	// Garbage-collect the old entries.  TODO: we'll eventually need a
	// sweeper to clean up entries left behind if we crash right here.
	for ordinal := earliestOrdinal; ordinal <= latestOrdinal; ordinal++ {
		p := j.journalEntryPath(ordinal)
		err = ioutil.Remove(p)
		if err != nil {
			return err
		}
	}
	return nil
}

func (j diskJournal) removeEarliest() (empty bool, err error) {
	earliestOrdinal, err := j.readEarliestOrdinal()
	if err != nil {
		return false, err
	}

	latestOrdinal, err := j.readLatestOrdinal()
	if err != nil {
		return false, err
	}

	if earliestOrdinal == latestOrdinal {
		err := j.clearOrdinals()
		if err != nil {
			return false, err
		}
		return true, nil
	}

	err = j.writeEarliestOrdinal(earliestOrdinal + 1)
	if err != nil {
		return false, err
	}

	// Garbage-collect the old entry.  TODO: we'll eventually need a
	// sweeper to clean up entries left behind if we crash right here.
	p := j.journalEntryPath(earliestOrdinal)
	err = ioutil.Remove(p)
	if err != nil {
		return false, err
	}

	return false, nil
}

// The functions below are for reading and writing journal entries.

func (j diskJournal) readJournalEntry(o journalOrdinal) (interface{}, error) {
	p := j.journalEntryPath(o)
	entry := reflect.New(j.entryType)
	err := kbfscodec.DeserializeFromFile(j.codec, p, entry)
	if err != nil {
		return nil, err
	}

	return entry.Elem().Interface(), nil
}

func (j diskJournal) writeJournalEntry(
	o journalOrdinal, entry interface{}) error {
	entryType := reflect.TypeOf(entry)
	if entryType != j.entryType {
		panic(errors.Errorf("Expected entry type %v, got %v",
			j.entryType, entryType))
	}

	return kbfscodec.SerializeToFile(j.codec, entry, j.journalEntryPath(o))
}

// appendJournalEntry appends the given entry to the journal. If o is
// nil, then if the journal is empty, the new entry will have ordinal
// 0, and otherwise it will have ordinal equal to the successor of the
// latest ordinal. Otherwise, if o is non-nil, then if the journal is
// empty, the new entry will have ordinal *o, and otherwise it returns
// an error if *o is not the successor of the latest ordinal. If
// successful, appendJournalEntry returns the ordinal of the
// just-appended entry.
func (j diskJournal) appendJournalEntry(
	o *journalOrdinal, entry interface{}) (journalOrdinal, error) {
	// TODO: Consider caching the latest ordinal in memory instead
	// of reading it from disk every time.
	var next journalOrdinal
	lo, err := j.readLatestOrdinal()
	if ioutil.IsNotExist(err) {
		if o != nil {
			next = *o
		} else {
			next = 0
		}
	} else if err != nil {
		return 0, err
	} else {
		next = lo + 1
		if next == 0 {
			// Rollover is almost certainly a bug.
			return 0, errors.Errorf(
				"Ordinal rollover for %+v", entry)
		}
		if o != nil && next != *o {
			return 0, errors.Errorf(
				"%v unexpectedly does not follow %v for %+v",
				*o, lo, entry)
		}
	}

	err = j.writeJournalEntry(next, entry)
	if err != nil {
		return 0, err
	}

	_, err = j.readEarliestOrdinal()
	if ioutil.IsNotExist(err) {
		err := j.writeEarliestOrdinal(next)
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	err = j.writeLatestOrdinal(next)
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (j *diskJournal) move(newDir string) (oldDir string, err error) {
	err = ioutil.Rename(j.dir, newDir)
	if err != nil {
		return "", err
	}
	oldDir = j.dir
	j.dir = newDir
	return oldDir, nil
}

func (j diskJournal) length() (uint64, error) {
	first, err := j.readEarliestOrdinal()
	if ioutil.IsNotExist(err) {
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
