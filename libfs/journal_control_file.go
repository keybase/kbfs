// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfs

import (
	"fmt"

	"golang.org/x/net/context"

	"github.com/keybase/kbfs/libkbfs"
)

// JournalAction enumerates all the possible actions to take on a
// TLF's journal.
type JournalAction int

const (
	// JournalEnable is to turn the journal on.
	JournalEnable JournalAction = iota
	// JournalFlush is to flush the journal.
	JournalFlush
	// JournalPauseBackgroundWork is to pause journal background
	// work.
	JournalPauseBackgroundWork
	// JournalResumeBackgroundWork is to resume journal background
	// work.
	JournalResumeBackgroundWork
	// JournalDisable is to disable the journal.
	JournalDisable
)

func (a JournalAction) String() string {
	switch a {
	case JournalEnable:
		return "Enable journal"
	case JournalFlush:
		return "Flush journal"
	case JournalPauseBackgroundWork:
		return "Pause journal background work"
	case JournalResumeBackgroundWork:
		return "Resume journal background work"
	case JournalDisable:
		return "Disable journal"
	}
	return fmt.Sprintf("JournalAction(%d)", int(a))
}

// Execute performs the action on the given JournalServer for the
// given TLF.
func (a JournalAction) Execute(
	ctx context.Context, jServer *libkbfs.JournalServer,
	tlf libkbfs.TlfID) error {
	if tlf == (libkbfs.TlfID{}) {
		panic("zero TlfID in JournalAction.Execute")
	}

	switch a {
	case JournalEnable:
		err := jServer.Enable(
			ctx, tlf, libkbfs.TLFJournalBackgroundWorkEnabled)
		if err != nil {
			return err
		}

	case JournalFlush:
		err := jServer.Flush(ctx, tlf)
		if err != nil {
			return err
		}

	case JournalPauseBackgroundWork:
		jServer.PauseBackgroundWork(ctx, tlf)

	case JournalResumeBackgroundWork:
		jServer.ResumeBackgroundWork(ctx, tlf)

	case JournalDisable:
		_, err := jServer.Disable(ctx, tlf)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unknown action %s", a)
	}

	return nil
}
