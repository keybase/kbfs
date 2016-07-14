// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "errors"

func GetJournalServer(config Config) (*JournalServer, error) {
	bserver := config.BlockServer()
	jbserver, ok := bserver.(journalBlockServer)
	if !ok {
		return nil, errors.New("Write journaling not enabled")
	}
	return jbserver.jServer, nil
}
