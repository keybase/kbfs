// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libpages

import (
	"net"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	keybasePagesPrefix = "kbp="
)

// ErrKeybasePagesRecordNotFound is returned when a domain requested doesn't
// have a kbp= record configured.
type ErrKeybasePagesRecordNotFound struct{}

// Error implements the error interface.
func (ErrKeybasePagesRecordNotFound) Error() string {
	return "no TXT record is found for " + keybasePagesPrefix
}

// ErrKeybasePagesRecordTooMany is returned when a domain requested has more
// than one kbp= record configured.
type ErrKeybasePagesRecordTooMany struct{}

// Error implements the error interface.
func (ErrKeybasePagesRecordTooMany) Error() string {
	return "more than 1 TXT record are found for " + keybasePagesPrefix
}

const kbpRecordPrefix = "_keybase_pages."

// LoadRootFromDNS loads the root path configured for domain, with following
// steps:
//   1. Construct a domain name by prefixing the `domain` parameter with
//      "_keybase_pages.". So for example, "static.keybase.io" turns into
//      "_keybase_pages.static.keybase.io".
//   2. Load TXT record(s) from the domain constructed in step 1, and look for
//      one starting with "kbp=". If exactly one exists, parse it into a `Root`
//      and return it.
//
// There must be exactly one "kbp=" TXT record configured for domain. If more
// than one exists, an ErrKeybasePagesRecordTooMany{} is returned. If none is
// found, an ErrKeybasePagesRecordNotFound{} is returned. In case user has some
// configuration that requires other records that we can't foresee for now,
// other records (TXT or not) can co-exist with the "kbp=" record (as long as
// no CNAME record exists on the "_keybase_pages." prefixed domain of course).
//
// If the given domain is invalid, it would cause the domain name constructed
// in step will be invalid too, which causes Go's DNS resolver to return a
// net.DNSError typed "no such host" error.
//
// Examples for "static.keybase.io", "meatball.gao.io", "song.gao.io",
// "blah.strib.io", and "kbp.jzila.com" respectively:
//
// _keybase_pages.static.keybase.io TXT "kbp=/keybase/team/keybase.bots/static.keybase.io"
// _keybase_pages.meatball.gao.io   TXT "kbp=/keybase/public/songgao/meatball/"
// _keybase_pages.song.gao.io       TXT "kbp=/keybase/private/songgao,kb_bot/blah"
// _keybase_pages.blah.strib.io     TXT "kbp=/keybase/private/strib#kb_bot/blahblahb" "lah/blah/"
// _keybase_pages.kbp.jzila.com     TXT "kbp=git-keybase://private/jzila,kb_bot/kbp.git"
func LoadRootFromDNS(log *zap.Logger, domain string) (root Root, err error) {
	var rootPath string

	defer func() {
		zapFields := []zapcore.Field{
			zap.String("domain", domain),
			zap.String("root_path", rootPath),
		}
		if err == nil {
			log.Info("LoadRootFromDNS", zapFields...)
		} else {
			log.Warn("LoadRootFromDNS", append(zapFields, zap.Error(err))...)
		}
	}()

	txtRecords, err := net.LookupTXT(kbpRecordPrefix + domain)
	if err != nil {
		return Root{}, err
	}

	for _, r := range txtRecords {
		r = strings.TrimSpace(r)

		if strings.HasPrefix(r, keybasePagesPrefix) {
			if len(rootPath) != 0 {
				return Root{}, ErrKeybasePagesRecordTooMany{}
			}
			rootPath = r[len(keybasePagesPrefix):]
		}
	}

	if len(rootPath) == 0 {
		return Root{}, ErrKeybasePagesRecordNotFound{}
	}

	return ParseRoot(rootPath)
}
