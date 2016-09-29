// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libwebdav

import (
	"time"

	"golang.org/x/net/webdav"
)

// Locker ...
type Locker struct {
	lastDetails webdav.LockDetails
}

// Confirm confirms that the caller can claim all of the locks specified by
// the given conditions, and that holding the union of all of those locks
// gives exclusive access to all of the named resources. Up to two resources
// can be named. Empty names are ignored.
//
// Exactly one of release and err will be non-nil. If release is non-nil,
// all of the requested locks are held until release is called. Calling
// release does not unlock the lock, in the WebDAV UNLOCK sense, but once
// Confirm has confirmed that a lock claim is valid, that lock cannot be
// Confirmed again until it has been released.
//
// If Confirm returns ErrConfirmationFailed then the Handler will continue
// to try any other set of locks presented (a WebDAV HTTP request can
// present more than one set of locks). If it returns any other non-nil
// error, the Handler will write a "500 Internal Server Error" HTTP status.
func (l *Locker) Confirm(now time.Time, name0, name1 string, conditions ...webdav.Condition) (release func(), err error) {
	return func() {}, nil
}

// Create creates a lock with the given depth, duration, owner and root
// (name). The depth will either be negative (meaning infinite) or zero.
//
// If Create returns ErrLocked then the Handler will write a "423 Locked"
// HTTP status. If it returns any other non-nil error, the Handler will
// write a "500 Internal Server Error" HTTP status.
//
// See http://www.webdav.org/specs/rfc4918.html#rfc.section.9.10.6 for
// when to use each error.
//
// The token returned identifies the created lock. It should be an absolute
// URI as defined by RFC 3986, Section 4.3. In particular, it should not
// contain whitespace.
func (l *Locker) Create(now time.Time, details webdav.LockDetails) (token string, err error) {
	l.lastDetails = details
	return "lock", nil
}

// Refresh refreshes the lock with the given token.
//
// If Refresh returns ErrLocked then the Handler will write a "423 Locked"
// HTTP Status. If Refresh returns ErrNoSuchLock then the Handler will write
// a "412 Precondition Failed" HTTP Status. If it returns any other non-nil
// error, the Handler will write a "500 Internal Server Error" HTTP status.
//
// See http://www.webdav.org/specs/rfc4918.html#rfc.section.9.10.6 for
// when to use each error.
func (l *Locker) Refresh(now time.Time, token string, duration time.Duration) (webdav.LockDetails, error) {
	return l.lastDetails, nil
}

// Unlock unlocks the lock with the given token.
//
// If Unlock returns ErrForbidden then the Handler will write a "403
// Forbidden" HTTP Status. If Unlock returns ErrLocked then the Handler
// will write a "423 Locked" HTTP status. If Unlock returns ErrNoSuchLock
// then the Handler will write a "409 Conflict" HTTP Status. If it returns
// any other non-nil error, the Handler will write a "500 Internal Server
// Error" HTTP status.
//
// See http://www.webdav.org/specs/rfc4918.html#rfc.section.9.11.1 for
// when to use each error.
func (l *Locker) Unlock(now time.Time, token string) error {
	return nil
}
