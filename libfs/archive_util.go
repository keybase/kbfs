// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfs

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/keybase/kbfs/kbfsmd"
	"github.com/keybase/kbfs/libkbfs"
)

// BranchNameFromArchiveRefDir returns a branch name and true if the
// given directory name is specifying an archived revision with a
// revision number.
func BranchNameFromArchiveRefDir(dir string) (libkbfs.BranchName, bool) {
	if !strings.HasPrefix(dir, ArchivedRevDirPrefix) {
		return "", false
	}

	rev, err := strconv.ParseInt(dir[len(ArchivedRevDirPrefix):], 10, 64)
	if err != nil {
		return "", false
	}

	return libkbfs.MakeRevBranchName(kbfsmd.Revision(rev)), true
}

// RevFromTimeString converts a time string (in any supported golang
// date format) to the earliest revision number with a server
// timestamp greater or equal to that time.  Ambiguous dates are
// parsed in MM/DD format.
func RevFromTimeString(
	ctx context.Context, config libkbfs.Config, h *libkbfs.TlfHandle,
	timeString string) (kbfsmd.Revision, error) {
	// BUG: it looks like `dateparse` has trouble understanding some
	// time zones, it seems to ignore things like "PDT" and "EST".  I
	// filed https://github.com/araddon/dateparse/issues/68 about it.
	// TODO: revert this back to `ParseAny` once the above issue is
	// fixed, so that links are universally-shareable again when they
	// contain time zones.
	t, err := dateparse.ParseLocal(timeString)
	if err != nil {
		return kbfsmd.RevisionUninitialized, err
	}

	return libkbfs.GetMDRevisionByTime(ctx, config, h, t)
}

// LinkTargetFromTimeString returns the name of a by-revision archive
// directory, and true, if the given link specifies a valid by-time
// link name.  Ambiguous dates are parsed in MM/DD format.
func LinkTargetFromTimeString(
	ctx context.Context, config libkbfs.Config, h *libkbfs.TlfHandle,
	link string) (string, bool, error) {
	if !strings.HasPrefix(link, ArchivedTimeLinkPrefix) {
		return "", false, nil
	}

	rev, err := RevFromTimeString(
		ctx, config, h, link[len(ArchivedTimeLinkPrefix):])
	if err != nil {
		return "", false, err
	}

	return ArchivedRevDirPrefix + strconv.FormatInt(int64(rev), 10), true, nil
}

// RevFromRelativeTimeString turns a string describing a time in the
// past relative to now (e.g., "5m", "2h55s"), and turns it into a
// revision number for the given TLF.
func RevFromRelativeTimeString(
	ctx context.Context, config libkbfs.Config, h *libkbfs.TlfHandle,
	relTime string) (kbfsmd.Revision, error) {
	d, err := time.ParseDuration(relTime)
	if err != nil {
		return 0, err
	}

	absTime := config.Clock().Now().Add(-d)
	return libkbfs.GetMDRevisionByTime(ctx, config, h, absTime)
}

// FileDataFromRelativeTimeString returns a byte string containing the
// name of a revision-based archive directory, and true, if the given
// file name specifies a valid by-relative-time file name.  The time
// is relative to the local clock.
func FileDataFromRelativeTimeString(
	ctx context.Context, config libkbfs.Config, h *libkbfs.TlfHandle,
	filename string) ([]byte, bool, error) {
	if !strings.HasPrefix(filename, ArchivedRelTimeFilePrefix) {
		return nil, false, nil
	}

	rev, err := RevFromRelativeTimeString(
		ctx, config, h, filename[len(ArchivedRelTimeFilePrefix):])
	if err != nil {
		return nil, false, err
	}

	return []byte(ArchivedRevDirPrefix + strconv.FormatInt(int64(rev), 10)),
		true, nil
}
