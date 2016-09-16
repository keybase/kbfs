// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"github.com/keybase/client/go/logger"
	"golang.org/x/net/context"
)

// serviceLoggedIn should be called when a new user logs in. It
// shouldn't be called again until after serviceLoggedOut is called.
func serviceLoggedIn(ctx context.Context, log logger.Logger, name string,
	hasSession hasSession, config Config,
	bws TLFJournalBackgroundWorkStatus) {
	const sessionID = 0
	session, err := hasSession.CurrentSession(ctx, sessionID)
	if err != nil {
		log.CDebugf(ctx, "Getting current session failed when %s is logged in, so pretending user has logged out: %v",
			name, err)
		serviceLoggedOut(ctx, config)
		return
	}
	if jServer, err := GetJournalServer(config); err == nil {
		jServer.EnableExistingJournals(
			ctx, session.UID, session.VerifyingKey, bws)
	}
	config.MDServer().RefreshAuthToken(ctx)
	config.BlockServer().RefreshAuthToken(ctx)
	config.KBFSOps().RefreshCachedFavorites(ctx)
}

// serviceLoggedIn should be called when the current user logs out.
func serviceLoggedOut(ctx context.Context, config Config) {
	if jServer, err := GetJournalServer(config); err == nil {
		jServer.shutdownExistingJournals(ctx)
	}
	config.ResetCaches()
	config.MDServer().RefreshAuthToken(ctx)
	config.BlockServer().RefreshAuthToken(ctx)
	config.KBFSOps().RefreshCachedFavorites(ctx)
}
