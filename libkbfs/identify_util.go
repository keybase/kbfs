// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"golang.org/x/net/context"
)

func identifyUID(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, uid keybase1.UID, isPublic bool) error {
	_, _, err := identifyUIDWithIdentifyBehavior(ctx,
		nug, identifier, uid, isPublic, keybase1.TLFIdentifyBehavior_DEFAULT_KBFS)
	return err
}

func identifyUIDWithIdentifyBehavior(ctx context.Context,
	nug normalizedUsernameGetter, identifier identifier, uid keybase1.UID,
	isPublic bool, identifyBehavior keybase1.TLFIdentifyBehavior) (
	libkb.NormalizedUsername, *keybase1.IdentifyTrackBreaks, error) {
	username, err := nug.GetNormalizedUsername(ctx, uid)
	if err != nil {
		return "", nil, err
	}
	var reason string
	if isPublic {
		reason = "You accessed a public folder."
	} else {
		reason = fmt.Sprintf("You accessed a private folder with %s.",
			username.String())
	}

	var (
		userInfo UserInfo
		breaks   *keybase1.IdentifyTrackBreaks
	)
	switch identifyBehavior {
	case keybase1.TLFIdentifyBehavior_DEFAULT_KBFS:
		userInfo, err = identifier.Identify(ctx, username.String(), reason)
		if err != nil {
			// Convert libkb.NoSigChainError into one we can report.  (See
			// KBFS-1252).
			if _, ok := err.(libkb.NoSigChainError); ok {
				return "", nil, NoSigChainError{username}
			}
			return "", nil, err
		}
	case keybase1.TLFIdentifyBehavior_CHAT:
		userInfo, breaks, err = identifier.IdentifyForChat(
			ctx, username.String(), reason)
		if err != nil {
			// Convert libkb.NoSigChainError into one we can report.  (See
			// KBFS-1252).
			if _, ok := err.(libkb.NoSigChainError); ok {
				return "", nil, NoSigChainError{username}
			}
			return "", nil, err
		}
	default:
		return "", nil, errors.New("unknown identifyBehavior")
	}

	if userInfo.Name != username {
		return "", nil, fmt.Errorf("Identify returned name=%s, expected %s",
			userInfo.Name, username)
	}
	if userInfo.UID != uid {
		return "", nil, fmt.Errorf("Identify returned uid=%s, expected %s",
			userInfo.UID, uid)
	}

	return username, breaks, nil
}

// identifyUserList identifies the users in the given list.
func identifyUserList(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, uids []keybase1.UID, public bool) error {
	_, err := identifyUserListWithIdentifyBehavior(ctx,
		nug, identifier, uids, public, keybase1.TLFIdentifyBehavior_DEFAULT_KBFS)
	return err
}

func identifyUserListWithIdentifyBehavior(ctx context.Context,
	nug normalizedUsernameGetter, identifier identifier, uids []keybase1.UID,
	public bool, identifyBehavior keybase1.TLFIdentifyBehavior) (
	map[libkb.NormalizedUsername]*keybase1.IdentifyTrackBreaks, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type jobRes struct {
		err      error
		breaks   *keybase1.IdentifyTrackBreaks
		username libkb.NormalizedUsername
	}

	errChan := make(chan jobRes, len(uids))
	// TODO: limit the number of concurrent identifies?
	for _, uid := range uids {
		go func(uid keybase1.UID) {
			username, breaks, err := identifyUIDWithIdentifyBehavior(
				ctx, nug, identifier, uid, public, identifyBehavior)
			errChan <- jobRes{
				err:      err,
				breaks:   breaks,
				username: username,
			}
		}(uid)
	}

	breaksMap := make(map[libkb.NormalizedUsername]*keybase1.IdentifyTrackBreaks)
	for i := 0; i < len(uids); i++ {
		res := <-errChan
		if res.err != nil {
			return nil, res.err
		}
		breaksMap[res.username] = res.breaks
	}

	return breaksMap, nil
}

// identifyHandle identifies the canonical names in the given handle.
func identifyHandle(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, h *TlfHandle) error {
	_, err := identifyHandleWithIdentifyBehavior(ctx, nug, identifier, h,
		keybase1.TLFIdentifyBehavior_DEFAULT_KBFS)
	return err
}

func identifyHandleWithIdentifyBehavior(
	ctx context.Context, nug normalizedUsernameGetter, identifier identifier,
	h *TlfHandle, identifyBehavior keybase1.TLFIdentifyBehavior) (
	map[libkb.NormalizedUsername]*keybase1.IdentifyTrackBreaks, error) {
	uids := append(h.ResolvedWriters(), h.ResolvedReaders()...)
	return identifyUserListWithIdentifyBehavior(
		ctx, nug, identifier, uids, h.IsPublic(), identifyBehavior)
}
