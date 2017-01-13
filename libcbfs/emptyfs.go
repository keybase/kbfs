// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libcbfs

import (
	"time"

	"github.com/keybase/kbfs/dokan/winacl"
	"github.com/keybase/kbfs/libcbfs/cbfs"
	"golang.org/x/net/context"
)

// Return cbfs.ErrAccessDenied by default as that is a safe default.

type emptyFile struct{}

func (t emptyFile) Cleanup(ctx context.Context) {
}
func (t emptyFile) CloseFile(ctx context.Context) {
}
func (t emptyFile) SetEndOfFile(ctx context.Context, length int64) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) SetAllocationSize(ctx context.Context, length int64) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) ReadFile(ctx context.Context, bs []byte, offset int64) (int, error) {
	return 0, cbfs.ErrAccessDenied
}
func (t emptyFile) WriteFile(ctx context.Context, bs []byte, offset int64) (int, error) {
	return 0, cbfs.ErrAccessDenied
}
func (t emptyFile) FlushFileBuffers(ctx context.Context) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) FindFiles(ctx context.Context, ignored string, cb func(*cbfs.NamedStat) error) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) SetFileTime(context.Context, *cbfs.FileInfo, time.Time, time.Time, time.Time) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) SetFileAttributes(ctx context.Context, fileAttributes *cbfs.Stat) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) LockFile(ctx context.Context, offset int64, length int64) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) UnlockFile(ctx context.Context, offset int64, length int64) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) CanDelete(ctx context.Context) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) Delete(ctx context.Context) error {
	return cbfs.ErrAccessDenied
}
func (t emptyFile) IsDirectoryEmpty(context.Context) (bool, error) {
	return false, nil
}
func (t emptyFile) GetFileSecurity(ctx context.Context, si winacl.SecurityInformation, sd *winacl.SecurityDescriptor) error {
	if si&winacl.OwnerSecurityInformation != 0 && currentUserSID != nil {
		sd.SetOwner(currentUserSID)
	}
	if si&winacl.GroupSecurityInformation != 0 && currentGroupSID != nil {
		sd.SetGroup(currentGroupSID)
	}
	if si&winacl.DACLSecurityInformation != 0 {
		var acl winacl.ACL
		acl.AddAllowAccess(0x001F01FF, currentUserSID)
		sd.SetDacl(&acl)
	}
	return nil
}
func (t emptyFile) SetFileSecurity(ctx context.Context, si winacl.SecurityInformation, sd *winacl.SecurityDescriptor) error {
	return cbfs.ErrAccessDenied
}
