// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"errors"
	"fmt"
	"sync"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/tlf"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"
)

type extendedIdentify struct {
	behavior keybase1.TLFIdentifyBehavior

	// lock guards userBreaks and tlfBreaks
	lock       sync.Mutex
	userBreaks chan keybase1.TLFIdentifyFailure
	tlfBreaks  *keybase1.TLFBreak
}

func (ei *extendedIdentify) userBreak(username libkb.NormalizedUsername, uid keybase1.UID, breaks *keybase1.IdentifyTrackBreaks) {
	if ei.userBreaks == nil {
		return
	}

	ei.userBreaks <- keybase1.TLFIdentifyFailure{
		Breaks: breaks,
		User: keybase1.User{
			Uid:      uid,
			Username: string(username),
		},
	}
}

func (ei *extendedIdentify) makeTlfBreaksIfNeeded(
	ctx context.Context, numUserInTlf int) error {
	if ei.userBreaks == nil {
		return nil
	}

	ei.lock.Lock()
	defer ei.lock.Unlock()

	b := &keybase1.TLFBreak{}
	for i := 0; i < numUserInTlf; i++ {
		select {
		case ub, ok := <-ei.userBreaks:
			if !ok {
				return errors.New("makeTlfBreaksIfNeeded called on extendedIdentify" +
					" with closed userBreaks channel.")
			}
			if ub.Breaks != nil {
				b.Breaks = append(b.Breaks, ub)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ei.tlfBreaks = b

	return nil
}

// getTlfBreakOrBust returns a keybase1.TLFBreak. This should only be called
// for behavior.WarningInsteadOfErrorOnBrokenTracks() == true, and after
// makeTlfBreaksIfNeeded is called, to make sure user proof breaks get
// populated in GUI mode.
//
// If called otherwise, we don't panic here anymore, since we can't panic on
// nil ei.tlfBreaks. The reason is if a previous successful identify has
// already happened recently, it could cause this identify to be skipped, which
// means ei.tlfBreaks is never populated. In this case, it's safe to return an
// empty keybase1.TLFBreak.
func (ei *extendedIdentify) getTlfBreakAndClose() keybase1.TLFBreak {
	ei.lock.Lock()
	defer ei.lock.Unlock()

	if ei.userBreaks != nil {
		close(ei.userBreaks)
		ei.userBreaks = nil
	}
	if ei.tlfBreaks != nil {
		return *ei.tlfBreaks
	}

	return keybase1.TLFBreak{}
}

// ctxExtendedIdentifyKeyType is a type for the context key for using
// extendedIdentify
type ctxExtendedIdentifyKeyType int

const (
	// ctxExtendedIdentifyKeyType is a context key for using extendedIdentify
	ctxExtendedIdentifyKey ctxExtendedIdentifyKeyType = iota
)

// ExtendedIdentifyAlreadyExists is returned when makeExtendedIdentify is
// called on a context already with extendedIdentify.
type ExtendedIdentifyAlreadyExists struct{}

func (e ExtendedIdentifyAlreadyExists) Error() string {
	return "extendedIdentify already exists"
}

func makeExtendedIdentify(ctx context.Context,
	behavior keybase1.TLFIdentifyBehavior) (context.Context, error) {
	if _, ok := ctx.Value(ctxExtendedIdentifyKey).(*extendedIdentify); ok {
		return nil, ExtendedIdentifyAlreadyExists{}
	}

	if !behavior.WarningInsteadOfErrorOnBrokenTracks() {
		return NewContextReplayable(ctx, func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxExtendedIdentifyKey, &extendedIdentify{
				behavior: behavior,
			})
		}), nil
	}

	ch := make(chan keybase1.TLFIdentifyFailure)
	return NewContextReplayable(ctx, func(ctx context.Context) context.Context {
		return context.WithValue(ctx, ctxExtendedIdentifyKey, &extendedIdentify{
			behavior:   behavior,
			userBreaks: ch,
		})
	}), nil
}

func getExtendedIdentify(ctx context.Context) (ei *extendedIdentify) {
	if ei, ok := ctx.Value(ctxExtendedIdentifyKey).(*extendedIdentify); ok {
		return ei
	}
	return &extendedIdentify{
		behavior: keybase1.TLFIdentifyBehavior_DEFAULT_KBFS,
	}
}

// identifyUID performs identify based only on UID. It should be
// used only if the username is not known - as e.g. when rekeying.
func identifyUID(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, id keybase1.UserOrTeamID, t tlf.Type) error {
	name, err := nug.GetNormalizedUsername(ctx, id)
	if err != nil {
		return err
	}
	return identifyUser(ctx, nug, identifier, name, id, t)
}

// identifyUser is the preferred way to run identifies.
func identifyUser(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, name libkb.NormalizedUsername,
	id keybase1.UserOrTeamID, t tlf.Type) error {
	// Check to see if identify should be skipped altogether.
	ei := getExtendedIdentify(ctx)
	if ei.behavior == keybase1.TLFIdentifyBehavior_CHAT_SKIP {
		return nil
	}

	var reason string
	switch t {
	case tlf.Public:
		reason = "You accessed a public folder."
	case tlf.Private:
		reason = fmt.Sprintf("You accessed a private folder with %s.", name.String())
	case tlf.SingleTeam:
		reason = fmt.Sprintf("You accessed a folder for private team %s.", name.String())
	}
	resultName, resultID, err := identifier.Identify(ctx, name.String(), reason)
	if err != nil {
		// Convert libkb.NoSigChainError into one we can report.  (See
		// KBFS-1252).
		if _, ok := err.(libkb.NoSigChainError); ok {
			return NoSigChainError{name}
		}
		return err
	}
	if resultName != name {
		return fmt.Errorf("Identify returned name=%s, expected %s",
			resultName, name)
	}
	if resultID != id {
		return fmt.Errorf("Identify returned uid=%s, expected %s", resultID, id)
	}
	return nil
}

// identifyUserToChan calls identifyUser and plugs the result into the error channnel.
func identifyUserToChan(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, name libkb.NormalizedUsername,
	id keybase1.UserOrTeamID, t tlf.Type, errChan chan error) {
	errChan <- identifyUser(ctx, nug, identifier, name, id, t)
}

// identifyUsers identifies the users in the given maps.
func identifyUsers(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier,
	names map[keybase1.UserOrTeamID]libkb.NormalizedUsername,
	t tlf.Type) error {
	eg, ctx := errgroup.WithContext(ctx)

	// TODO: limit the number of concurrent identifies?
	// TODO: implement a version of errgroup with limited concurrency.
	for id, name := range names {
		// Capture range variables.
		id, name := id, name
		eg.Go(func() error {
			return identifyUser(ctx, nug, identifier, name, id, t)
		})
	}

	return eg.Wait()
}

// identifyUserList identifies the users in the given list.
// Only use this when the usernames are not known - like when rekeying.
func identifyUserList(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier, ids []keybase1.UserOrTeamID, t tlf.Type) error {
	eg, ctx := errgroup.WithContext(ctx)

	// TODO: limit the number of concurrent identifies?
	// TODO: implement concurrency limited version of errgroup.
	for _, id := range ids {
		// Capture range variable.
		id := id
		eg.Go(func() error {
			return identifyUID(ctx, nug, identifier, id, t)
		})
	}

	return eg.Wait()
}

// identifyUsersForTLF is a helper for identifyHandle for easier testing.
func identifyUsersForTLF(ctx context.Context, nug normalizedUsernameGetter,
	identifier identifier,
	names map[keybase1.UserOrTeamID]libkb.NormalizedUsername,
	t tlf.Type) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		ei := getExtendedIdentify(ctx)
		return ei.makeTlfBreaksIfNeeded(ctx, len(names))
	})

	eg.Go(func() error {
		return identifyUsers(ctx, nug, identifier, names, t)
	})

	return eg.Wait()
}

// identifyHandle identifies the canonical names in the given handle.
func identifyHandle(ctx context.Context, nug normalizedUsernameGetter, identifier identifier, h *TlfHandle) error {
	return identifyUsersForTLF(ctx, nug, identifier,
		h.ResolvedUsersMap(), h.Type())
}
