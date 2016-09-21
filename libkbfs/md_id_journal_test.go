// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "testing"

type mdIDJournalEntryFuture struct {
	mdIDJournalEntry
	extra
}

func (ef mdIDJournalEntryFuture) toCurrent() mdIDJournalEntry {
	return ef.mdIDJournalEntry
}

func (ef mdIDJournalEntryFuture) toCurrentStruct() currentStruct {
	return ef.toCurrent()
}

func makeFakeMDIDJournalEntryFuture(t *testing.T) mdIDJournalEntryFuture {
	ef := mdIDJournalEntryFuture{
		mdIDJournalEntry{
			fakeMdID(1),
		},
		makeExtraOrBust("mdIDJournalEntry", t),
	}
	return ef
}

func TestMDIDJournalEntryUnknownFields(t *testing.T) {
	testStructUnknownFields(t, makeFakeMDIDJournalEntryFuture(t))
}
