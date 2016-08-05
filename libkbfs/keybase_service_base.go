// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/logger"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

// KeybaseServiceBase implements most of KeybaseService from protocol
// defined clients.
type KeybaseServiceBase struct {
	context        Context
	identifyClient keybase1.IdentifyInterface
	userClient     keybase1.UserInterface
	sessionClient  keybase1.SessionInterface
	favoriteClient keybase1.FavoriteInterface
	kbfsClient     keybase1.KbfsInterface
	log            logger.Logger

	config Config

	sessionCacheLock sync.RWMutex
	// Set to the zero value when invalidated.
	cachedCurrentSession SessionInfo

	userCacheLock sync.RWMutex
	// Map entries are removed when invalidated.
	userCache               map[keybase1.UID]UserInfo
	userCacheUnverifiedKeys map[keybase1.UID][]keybase1.PublicKey

	lastNotificationFilenameLock sync.Mutex
	lastNotificationFilename     string
}

// NewKeybaseServiceBase makes a new KeybaseService.
func NewKeybaseServiceBase(config Config, kbCtx Context, log logger.Logger) *KeybaseServiceBase {
	k := KeybaseServiceBase{
		config:                  config,
		context:                 kbCtx,
		log:                     log,
		userCache:               make(map[keybase1.UID]UserInfo),
		userCacheUnverifiedKeys: make(map[keybase1.UID][]keybase1.PublicKey),
	}
	return &k
}

// SetClients sets the client protocol implementations needed for a KeybaseService.
func (k *KeybaseServiceBase) FillClients(identifyClient keybase1.IdentifyInterface,
	userClient keybase1.UserInterface, sessionClient keybase1.SessionInterface,
	favoriteClient keybase1.FavoriteInterface, kbfsClient keybase1.KbfsInterface) {
	k.identifyClient = identifyClient
	k.userClient = userClient
	k.sessionClient = sessionClient
	k.favoriteClient = favoriteClient
	k.kbfsClient = kbfsClient
}

type addVerifyingKeyFunc func(VerifyingKey)
type addCryptPublicKeyFunc func(CryptPublicKey)

// processKey adds the given public key to the appropriate verifying
// or crypt list (as return values), and also updates the given name
// map in place.
func processKey(publicKey keybase1.PublicKey,
	addVerifyingKey addVerifyingKeyFunc,
	addCryptPublicKey addCryptPublicKeyFunc,
	kidNames map[keybase1.KID]string) error {
	if len(publicKey.PGPFingerprint) > 0 {
		return nil
	}
	// Import the KID to validate it.
	key, err := libkb.ImportKeypairFromKID(publicKey.KID)
	if err != nil {
		return err
	}
	if publicKey.IsSibkey {
		addVerifyingKey(MakeVerifyingKey(key.GetKID()))
	} else {
		addCryptPublicKey(MakeCryptPublicKey(key.GetKID()))
	}
	if publicKey.DeviceDescription != "" {
		kidNames[publicKey.KID] = publicKey.DeviceDescription
	}
	return nil
}

func filterKeys(keys []keybase1.PublicKey) (
	[]VerifyingKey, []CryptPublicKey, map[keybase1.KID]string, error) {
	var verifyingKeys []VerifyingKey
	var cryptPublicKeys []CryptPublicKey
	var kidNames = map[keybase1.KID]string{}

	addVerifyingKey := func(key VerifyingKey) {
		verifyingKeys = append(verifyingKeys, key)
	}
	addCryptPublicKey := func(key CryptPublicKey) {
		cryptPublicKeys = append(cryptPublicKeys, key)
	}

	for _, publicKey := range keys {
		err := processKey(publicKey, addVerifyingKey, addCryptPublicKey,
			kidNames)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return verifyingKeys, cryptPublicKeys, kidNames, nil
}

func filterRevokedKeys(keys []keybase1.RevokedKey) (
	map[VerifyingKey]keybase1.KeybaseTime,
	map[CryptPublicKey]keybase1.KeybaseTime, map[keybase1.KID]string, error) {
	verifyingKeys := make(map[VerifyingKey]keybase1.KeybaseTime)
	cryptPublicKeys := make(map[CryptPublicKey]keybase1.KeybaseTime)
	var kidNames = map[keybase1.KID]string{}

	for _, revokedKey := range keys {
		addVerifyingKey := func(key VerifyingKey) {
			verifyingKeys[key] = revokedKey.Time
		}
		addCryptPublicKey := func(key CryptPublicKey) {
			cryptPublicKeys[key] = revokedKey.Time
		}
		err := processKey(revokedKey.Key, addVerifyingKey, addCryptPublicKey,
			kidNames)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return verifyingKeys, cryptPublicKeys, kidNames, nil

}

func (k *KeybaseServiceBase) getCachedCurrentSession() SessionInfo {
	k.sessionCacheLock.RLock()
	defer k.sessionCacheLock.RUnlock()
	return k.cachedCurrentSession
}

func (k *KeybaseServiceBase) setCachedCurrentSession(s SessionInfo) {
	k.sessionCacheLock.Lock()
	defer k.sessionCacheLock.Unlock()
	k.cachedCurrentSession = s
}

func (k *KeybaseServiceBase) getCachedUserInfo(uid keybase1.UID) UserInfo {
	k.userCacheLock.RLock()
	defer k.userCacheLock.RUnlock()
	return k.userCache[uid]
}

func (k *KeybaseServiceBase) setCachedUserInfo(uid keybase1.UID, info UserInfo) {
	k.userCacheLock.Lock()
	defer k.userCacheLock.Unlock()
	if info.Name == libkb.NormalizedUsername("") {
		delete(k.userCache, uid)
	} else {
		k.userCache[uid] = info
	}
}

func (k *KeybaseServiceBase) getCachedUnverifiedKeys(uid keybase1.UID) (
	[]keybase1.PublicKey, bool) {
	k.userCacheLock.RLock()
	defer k.userCacheLock.RUnlock()
	if unverifiedKeys, ok := k.userCacheUnverifiedKeys[uid]; ok {
		return unverifiedKeys, true
	}
	return nil, false
}

func (k *KeybaseServiceBase) setCachedUnverifiedKeys(uid keybase1.UID, pk []keybase1.PublicKey) {
	k.userCacheLock.Lock()
	defer k.userCacheLock.Unlock()
	k.userCacheUnverifiedKeys[uid] = pk
}

func (k *KeybaseServiceBase) clearCachedUnverifiedKeys(uid keybase1.UID) {
	k.userCacheLock.Lock()
	defer k.userCacheLock.Unlock()
	delete(k.userCacheUnverifiedKeys, uid)
}

func (k *KeybaseServiceBase) clearCaches() {
	k.setCachedCurrentSession(SessionInfo{})
	k.userCacheLock.Lock()
	defer k.userCacheLock.Unlock()
	k.userCache = make(map[keybase1.UID]UserInfo)
	k.userCacheUnverifiedKeys = make(map[keybase1.UID][]keybase1.PublicKey)
}

// LoggedIn implements keybase1.NotifySessionInterface.
func (k *KeybaseServiceBase) LoggedIn(ctx context.Context, name string) error {
	k.log.CDebugf(ctx, "Current session logged in: %s", name)
	// Since we don't have the whole session, just clear the cache.
	k.setCachedCurrentSession(SessionInfo{})
	if k.config != nil {
		k.config.MDServer().RefreshAuthToken(ctx)
		k.config.BlockServer().RefreshAuthToken(ctx)
		k.config.KBFSOps().RefreshCachedFavorites(ctx)
	}
	return nil
}

// LoggedOut implements keybase1.NotifySessionInterface.
func (k *KeybaseServiceBase) LoggedOut(ctx context.Context) error {
	k.log.CDebugf(ctx, "Current session logged out")
	k.setCachedCurrentSession(SessionInfo{})
	if k.config != nil {
		k.config.MDServer().RefreshAuthToken(ctx)
		k.config.BlockServer().RefreshAuthToken(ctx)
		k.config.KBFSOps().RefreshCachedFavorites(ctx)
	}
	return nil
}

// KeyfamilyChanged implements keybase1.NotifyKeyfamilyInterface.
func (k *KeybaseServiceBase) KeyfamilyChanged(ctx context.Context,
	uid keybase1.UID) error {
	k.log.CDebugf(ctx, "Key family for user %s changed", uid)
	k.setCachedUserInfo(uid, UserInfo{})
	k.clearCachedUnverifiedKeys(uid)

	if k.getCachedCurrentSession().UID == uid {
		// Ignore any errors for now, we don't want to block this
		// notification and it's not worth spawning a goroutine for.
		k.config.MDServer().CheckForRekeys(context.Background())
	}

	return nil
}

// PaperKeyCached implements keybase1.NotifyPaperKeyInterface.
func (k *KeybaseServiceBase) PaperKeyCached(ctx context.Context,
	arg keybase1.PaperKeyCachedArg) error {
	k.log.CDebugf(ctx, "Paper key for %s cached", arg.Uid)

	if k.getCachedCurrentSession().UID == arg.Uid {
		// Ignore any errors for now, we don't want to block this
		// notification and it's not worth spawning a goroutine for.
		k.config.MDServer().CheckForRekeys(context.Background())
	}

	return nil
}

// ClientOutOfDate implements keybase1.NotifySessionInterface.
func (k *KeybaseServiceBase) ClientOutOfDate(ctx context.Context,
	arg keybase1.ClientOutOfDateArg) error {
	k.log.CDebugf(ctx, "Client out of date: %v", arg)
	return nil
}

// ConvertIdentifyError converts a errors during identify into KBFS errors
func ConvertIdentifyError(assertion string, err error) error {
	switch err.(type) {
	case libkb.NotFoundError:
		return NoSuchUserError{assertion}
	case libkb.ResolutionError:
		return NoSuchUserError{assertion}
	}
	return err
}

// Resolve implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) Resolve(ctx context.Context, assertion string) (
	libkb.NormalizedUsername, keybase1.UID, error) {
	user, err := k.identifyClient.Resolve2(ctx, assertion)
	if err != nil {
		return libkb.NormalizedUsername(""), keybase1.UID(""),
			ConvertIdentifyError(assertion, err)
	}
	return libkb.NewNormalizedUsername(user.Username), user.Uid, nil
}

// Identify implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) Identify(ctx context.Context, assertion, reason string) (
	UserInfo, error) {
	// setting UseDelegateUI to true here will cause daemon to use
	// registered identify ui providers instead of terminal if any
	// are available.  If not, then it will use the terminal UI.
	arg := keybase1.Identify2Arg{
		UserAssertion: assertion,
		UseDelegateUI: true,
		Reason:        keybase1.IdentifyReason{Reason: reason},
	}
	res, err := k.identifyClient.Identify2(ctx, arg)
	if err != nil {
		return UserInfo{}, ConvertIdentifyError(assertion, err)
	}

	return k.processUserPlusKeys(res.Upk)
}

// LoadUserPlusKeys implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) LoadUserPlusKeys(ctx context.Context, uid keybase1.UID) (
	UserInfo, error) {
	cachedUserInfo := k.getCachedUserInfo(uid)
	if cachedUserInfo.Name != libkb.NormalizedUsername("") {
		return cachedUserInfo, nil
	}

	arg := keybase1.LoadUserPlusKeysArg{Uid: uid}
	res, err := k.userClient.LoadUserPlusKeys(ctx, arg)
	if err != nil {
		return UserInfo{}, err
	}

	return k.processUserPlusKeys(res)
}

func (k *KeybaseServiceBase) processUserPlusKeys(upk keybase1.UserPlusKeys) (
	UserInfo, error) {
	verifyingKeys, cryptPublicKeys, kidNames, err := filterKeys(upk.DeviceKeys)
	if err != nil {
		return UserInfo{}, err
	}

	revokedVerifyingKeys, revokedCryptPublicKeys, revokedKidNames, err :=
		filterRevokedKeys(upk.RevokedDeviceKeys)
	if err != nil {
		return UserInfo{}, err
	}

	if len(revokedKidNames) > 0 {
		for k, v := range revokedKidNames {
			kidNames[k] = v
		}
	}

	u := UserInfo{
		Name:                   libkb.NewNormalizedUsername(upk.Username),
		UID:                    upk.Uid,
		VerifyingKeys:          verifyingKeys,
		CryptPublicKeys:        cryptPublicKeys,
		KIDNames:               kidNames,
		RevokedVerifyingKeys:   revokedVerifyingKeys,
		RevokedCryptPublicKeys: revokedCryptPublicKeys,
	}

	k.setCachedUserInfo(upk.Uid, u)
	return u, nil
}

// LoadUnverifiedKeys implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) LoadUnverifiedKeys(ctx context.Context, uid keybase1.UID) (
	[]keybase1.PublicKey, error) {
	if keys, ok := k.getCachedUnverifiedKeys(uid); ok {
		return keys, nil
	}

	arg := keybase1.LoadAllPublicKeysUnverifiedArg{Uid: uid}
	keys, err := k.userClient.LoadAllPublicKeysUnverified(ctx, arg)
	if err != nil {
		return nil, err
	}

	k.setCachedUnverifiedKeys(uid, keys)
	return keys, nil
}

// CurrentSession implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) CurrentSession(ctx context.Context, sessionID int) (
	SessionInfo, error) {
	cachedCurrentSession := k.getCachedCurrentSession()
	if cachedCurrentSession != (SessionInfo{}) {
		return cachedCurrentSession, nil
	}

	res, err := k.sessionClient.CurrentSession(ctx, sessionID)
	if err != nil {
		if ncs := (NoCurrentSessionError{}); err.Error() ==
			NoCurrentSessionExpectedError {
			// Use an error with a proper OS error code attached to
			// it.  TODO: move ErrNoSession from client/go/service to
			// client/go/libkb, so we can use types for the check
			// above.
			err = ncs
		}
		return SessionInfo{}, err
	}
	s, err := SessionInfoFromProtocol(res)
	if err != nil {
		return s, err
	}

	k.log.CDebugf(
		ctx, "new session with username %s, uid %s, crypt public key %s, and verifying key %s",
		s.Name, s.UID, s.CryptPublicKey, s.VerifyingKey)

	k.setCachedCurrentSession(s)

	return s, nil
}

// FavoriteAdd implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) FavoriteAdd(ctx context.Context, folder keybase1.Folder) error {
	return k.favoriteClient.FavoriteAdd(ctx, keybase1.FavoriteAddArg{Folder: folder})
}

// FavoriteDelete implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) FavoriteDelete(ctx context.Context, folder keybase1.Folder) error {
	return k.favoriteClient.FavoriteIgnore(ctx,
		keybase1.FavoriteIgnoreArg{Folder: folder})
}

// FavoriteList implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) FavoriteList(ctx context.Context, sessionID int) ([]keybase1.Folder, error) {
	results, err := k.favoriteClient.GetFavorites(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return results.FavoriteFolders, nil
}

// Notify implements the KeybaseService interface for KeybaseDaemonRPC.
func (k *KeybaseServiceBase) Notify(ctx context.Context, notification *keybase1.FSNotification) error {
	// Reduce log spam by not repeating log lines for
	// notifications with the same filename.
	//
	// TODO: Only do this in debug mode.
	func() {
		k.lastNotificationFilenameLock.Lock()
		defer k.lastNotificationFilenameLock.Unlock()
		if notification.Filename != k.lastNotificationFilename {
			k.lastNotificationFilename = notification.Filename
			k.log.CDebugf(ctx, "Sending notification for %s", notification.Filename)
		}
	}()
	return k.kbfsClient.FSEvent(ctx, *notification)
}

// FlushUserFromLocalCache implements the KeybaseService interface for
// KeybaseDaemonRPC.
func (k *KeybaseServiceBase) FlushUserFromLocalCache(ctx context.Context,
	uid keybase1.UID) {
	k.log.CDebugf(ctx, "Flushing cache for user %s", uid)
	k.setCachedUserInfo(uid, UserInfo{})
}

// FlushUserUnverifiedKeysFromLocalCache implements the KeybaseService interface for
// KeybaseDaemonRPC.
func (k *KeybaseServiceBase) FlushUserUnverifiedKeysFromLocalCache(ctx context.Context,
	uid keybase1.UID) {
	k.log.CDebugf(ctx, "Flushing cache of unverified keys for user %s", uid)
	k.clearCachedUnverifiedKeys(uid)
}
