// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "golang.org/x/net/context"

func serviceLoggedIn(
	ctx context.Context, hasSession hasSession, config Config) {
	const sessionID = 0
	session, err := hasSession.CurrentSession(ctx, sessionID)
	if err != nil {
		// TODO: Log something.
		serviceLoggedOut(ctx, config)
		return
	}
	if jServer, err := GetJournalServer(config); err == nil {
		jServer.EnableExistingJournals(
			ctx, session.UID, session.VerifyingKey,
			TLFJournalBackgroundWorkEnabled)
	}
	config.MDServer().RefreshAuthToken(ctx)
	config.BlockServer().RefreshAuthToken(ctx)
	config.KBFSOps().RefreshCachedFavorites(ctx)
}

func serviceLoggedOut(ctx context.Context, config Config) {
	if config != nil {
		if jServer, err := GetJournalServer(config); err == nil {
			jServer.shutdownExistingJournals(ctx)
		}
		config.ResetCaches()
		config.MDServer().RefreshAuthToken(ctx)
		config.BlockServer().RefreshAuthToken(ctx)
		config.KBFSOps().RefreshCachedFavorites(ctx)
	}
}
