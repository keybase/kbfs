// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"context"

	"github.com/keybase/kbfs/tlf"
)

func getRekeyFSM(ops KBFSOps, tlfID tlf.ID) RekeyFSM {
	switch o := ops.(type) {
	case *KBFSOpsStandard:
		return o.getOpsNoAdd(FolderBranch{Tlf: tlfID, Branch: MasterBranch}).rekeyFSM
	case *folderBranchOps:
		return o.rekeyFSM
	}
	return nil
}

func requestRekeyWithContextAndWaitForOneFinishEvent(ctx context.Context, ops KBFSOps, tlfID tlf.ID) (res RekeyResult, err error) {
	fsm := getRekeyFSM(ops, tlfID)
	rekeyWaiter := make(chan struct{})
	// now user 1 should rekey
	fsm.listenOnEventForTest(rekeyFinishedEvent, func(e rekeyEvent) {
		res = e.rekeyFinished.RekeyResult
		err = e.rekeyFinished.err
		close(rekeyWaiter)
	}, false)
	fsm.Event(NewRekeyRequestEvent(RekeyRequest{RekeyTask: RekeyTask{
		injectContextForTest: ctx}}))
	<-rekeyWaiter
	return res, err
}

func requestRekeyAndWaitForOneFinishEvent(ops KBFSOps, tlfID tlf.ID) (res RekeyResult, err error) {
	fsm := getRekeyFSM(ops, tlfID)
	rekeyWaiter := make(chan struct{})
	// now user 1 should rekey
	fsm.listenOnEventForTest(rekeyFinishedEvent, func(e rekeyEvent) {
		res = e.rekeyFinished.RekeyResult
		err = e.rekeyFinished.err
		close(rekeyWaiter)
	}, false)
	ops.RequestRekey(tlfID)
	<-rekeyWaiter
	return res, err
}
