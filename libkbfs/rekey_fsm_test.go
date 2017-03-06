// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"

	"github.com/keybase/kbfs/tlf"
)

func requestRekeyWithContextAndWaitForOneFinishEvent(ctx context.Context, ops KBFSOps, tlfID tlf.ID) (res RekeyResult, err error) {
	fsm := getRekeyFSMForTest(ops, tlfID)
	rekeyWaiter := make(chan struct{})
	// now user 1 should rekey
	fsm.listenOnEventForTest(rekeyFinishedEvent, func(e RekeyEvent) {
		res = e.rekeyFinished.RekeyResult
		err = e.rekeyFinished.err
		close(rekeyWaiter)
	}, false)
	fsm.Event(NewRekeyRequestEvent(RekeyRequest{RekeyTask: RekeyTask{
		injectContextForTest: ctx}}))
	<-rekeyWaiter
	return res, err
}
