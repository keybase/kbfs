// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfs

import (
	"fmt"

	"github.com/keybase/kbfs/libkbfs"
)

type JournalAction int

const (
	JournalEnable JournalAction = iota
	JournalFlush
	JournalDisable
)

func (a JournalAction) String() string {
	switch a {
	case JournalEnable:
		return "Enable journal"
	case JournalFlush:
		return "Flush journal"
	case JournalDisable:
		return "Disable journal"
	}
	return fmt.Sprintf("JournalAction(%d)", int(a))
}

func (a JournalAction) Execute(
	jServer *libkbfs.JournalServer, tlf libkbfs.TlfID) error {
	switch a {
	case JournalEnable:
		err := jServer.Enable(tlf)
		if err != nil {
			return err
		}

	case JournalFlush:
		err := jServer.Flush(tlf)
		if err != nil {
			return err
		}

	case JournalDisable:
		err := jServer.Disable(tlf)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unknown action %s", a)
	}

	return nil
}
