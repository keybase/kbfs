// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfsblock

import (
	"context"
	"errors"

	"github.com/keybase/backoff"
	"github.com/keybase/client/go/logger"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/keybase/kbfs/kbfscodec"
	"github.com/keybase/kbfs/kbfscrypto"
	"github.com/keybase/kbfs/tlf"
)

func makeIDCombo(id ID, context Context) keybase1.BlockIdCombo {
	// ChargedTo is somewhat confusing when this BlockIdCombo is
	// used in a BlockReference -- it just refers to the original
	// creator of the block, i.e. the original user charged for
	// the block.
	//
	// This may all change once we implement groups.
	return keybase1.BlockIdCombo{
		BlockHash: id.String(),
		ChargedTo: context.GetCreator(),
		BlockType: context.GetBlockType(),
	}
}

func makeReference(id ID, context Context) keybase1.BlockReference {
	// Block references to MD blocks are allowed, because they can be
	// deleted in the case of an MD put failing.
	return keybase1.BlockReference{
		Bid: makeIDCombo(id, context),
		// The actual writer to modify quota for.
		ChargedTo: context.GetWriter(),
		Nonce:     keybase1.BlockRefNonce(context.GetRefNonce()),
	}
}

func MakeGetBlockArg(tlfID tlf.ID, id ID, context Context) keybase1.GetBlockArg {
	return keybase1.GetBlockArg{
		Bid:    makeIDCombo(id, context),
		Folder: tlfID.String(),
	}
}

func ParseGetBlockRes(res keybase1.GetBlockRes, resErr error) (
	buf []byte, serverHalf kbfscrypto.BlockCryptKeyServerHalf, err error) {
	if resErr != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, resErr
	}
	serverHalf, err = kbfscrypto.ParseBlockCryptKeyServerHalf(res.BlockKey)
	if err != nil {
		return nil, kbfscrypto.BlockCryptKeyServerHalf{}, err
	}
	return res.Buf, serverHalf, nil
}

func MakePutBlockArg(tlfID tlf.ID, id ID,
	bContext Context, buf []byte,
	serverHalf kbfscrypto.BlockCryptKeyServerHalf) keybase1.PutBlockArg {
	return keybase1.PutBlockArg{
		Bid: makeIDCombo(id, bContext),
		// BlockKey is misnamed -- it contains just the server
		// half.
		BlockKey: serverHalf.String(),
		Folder:   tlfID.String(),
		Buf:      buf,
	}
}

func MakePutBlockAgainArg(tlfID tlf.ID, id ID,
	bContext Context, buf []byte, serverHalf kbfscrypto.BlockCryptKeyServerHalf) keybase1.PutBlockAgainArg {
	return keybase1.PutBlockAgainArg{
		Ref: makeReference(id, bContext),
		// BlockKey is misnamed -- it contains just the server
		// half.
		BlockKey: serverHalf.String(),
		Folder:   tlfID.String(),
		Buf:      buf,
	}
}

func MakeAddReferenceArg(tlfID tlf.ID, id ID, context Context) keybase1.AddReferenceArg {
	return keybase1.AddReferenceArg{
		Ref:    makeReference(id, context),
		Folder: tlfID.String(),
	}
}

func ParseGetQuotaInfoRes(codec kbfscodec.Codec, res []byte, resErr error) (
	info *QuotaInfo, err error) {
	if resErr != nil {
		return nil, resErr
	}
	return QuotaInfoDecode(res, codec)
}

// GetNotDone returns the set of block references in "all" that do not yet appear in "results"
func GetNotDone(all ContextMap, doneRefs map[ID]map[RefNonce]int) (
	notDone []keybase1.BlockReference) {
	for id, idContexts := range all {
		for _, context := range idContexts {
			if _, ok := doneRefs[id]; ok {
				if _, ok1 := doneRefs[id][context.GetRefNonce()]; ok1 {
					continue
				}
			}
			ref := makeReference(id, context)
			notDone = append(notDone, ref)
		}
	}
	return notDone
}

func GetLiveCounts(doneRefs map[ID]map[RefNonce]int) map[ID]int {
	liveCounts := make(map[ID]int)
	for id, nonces := range doneRefs {
		for _, count := range nonces {
			if existing, ok := liveCounts[id]; !ok || existing > count {
				liveCounts[id] = count
			}
		}
	}
	return liveCounts
}

// BatchDowngradeReferences archives or deletes a batch of references
func BatchDowngradeReferences(ctx context.Context, log logger.Logger,
	tlfID tlf.ID, contexts ContextMap, archive bool,
	server keybase1.BlockInterface) (
	doneRefs map[ID]map[RefNonce]int, finalError error) {
	doneRefs = make(map[ID]map[RefNonce]int)
	notDone := GetNotDone(contexts, doneRefs)

	throttleErr := backoff.Retry(func() error {
		var res keybase1.DowngradeReferenceRes
		var err error
		if archive {
			res, err = server.ArchiveReferenceWithCount(ctx,
				keybase1.ArchiveReferenceWithCountArg{
					Refs:   notDone,
					Folder: tlfID.String(),
				})
		} else {
			res, err = server.DelReferenceWithCount(ctx,
				keybase1.DelReferenceWithCountArg{
					Refs:   notDone,
					Folder: tlfID.String(),
				})
		}

		// log errors
		if err != nil {
			log.CWarningf(ctx, "batchDowngradeReferences archive=%t sent=%v done=%v failedRef=%v err=%v",
				archive, notDone, res.Completed, res.Failed, err)
		} else {
			log.CDebugf(ctx, "batchDowngradeReferences archive=%t notdone=%v all succeeded",
				archive, notDone)
		}

		// update the set of completed reference
		for _, ref := range res.Completed {
			bid, err := IDFromString(ref.Ref.Bid.BlockHash)
			if err != nil {
				continue
			}
			nonces, ok := doneRefs[bid]
			if !ok {
				nonces = make(map[RefNonce]int)
				doneRefs[bid] = nonces
			}
			nonces[RefNonce(ref.Ref.Nonce)] = ref.LiveCount
		}
		// update the list of references to downgrade
		notDone = GetNotDone(contexts, doneRefs)

		//if context is cancelled, return immediately
		select {
		case <-ctx.Done():
			finalError = ctx.Err()
			return nil
		default:
		}

		// check whether to backoff and retry
		if err != nil {
			// if error is of type throttle, retry
			if IsThrottleError(err) {
				return err
			}
			// non-throttle error, do not retry here
			finalError = err
		}
		return nil
	}, backoff.NewExponentialBackOff())

	// if backoff has given up retrying, return error
	if throttleErr != nil {
		return doneRefs, throttleErr
	}

	if finalError == nil {
		if len(notDone) != 0 {
			log.CErrorf(ctx, "batchDowngradeReferences finished successfully with outstanding refs? all=%v done=%v notDone=%v\n", contexts, doneRefs, notDone)
			return doneRefs,
				errors.New("batchDowngradeReferences inconsistent result")
		}
	}
	return doneRefs, finalError
}
