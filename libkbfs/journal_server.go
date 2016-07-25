// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/keybase/client/go/logger"

	"golang.org/x/net/context"
)

type tlfJournalBundle struct {
	lock sync.RWMutex

	// TODO: Fill in with a block journal.
	mdJournal mdJournal
}

// JournalServer is the server that handles write journals. It
// interposes itself in front of BlockServer and MDOps. It uses MDOps
// instead of MDServer because it has to potentially modify the
// RootMetadata passed in, and by the time it hits MDServer it's
// already too late. However, this assumes that all MD ops go through
// MDOps.
type JournalServer struct {
	config Config

	log      logger.Logger
	deferLog logger.Logger

	dir string

	delegateBlockServer BlockServer
	delegateMDOps       MDOps
	delegateMDServer    MDServer

	lock       sync.RWMutex
	tlfBundles map[TlfID]*tlfJournalBundle
}

func (j *JournalServer) getBundle(tlfID TlfID) (*tlfJournalBundle, bool) {
	j.lock.RLock()
	defer j.lock.RUnlock()
	bundle, ok := j.tlfBundles[tlfID]
	return bundle, ok
}

// Enable turns on the write journal for the given TLF.
func (j *JournalServer) Enable(tlfID TlfID) (err error) {
	j.log.Debug("Enabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Error when enabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	_, ok := j.tlfBundles[tlfID]
	if ok {
		j.log.Debug("Journal already enabled for %s", tlfID)
		return nil
	}

	tlfDir := filepath.Join(j.dir, tlfID.String())
	j.log.Debug("Enabled journal for %s with path %s", tlfID, tlfDir)

	mdJournal := makeMDJournal(j.config.Codec(), j.config.Crypto(), tlfDir)

	j.tlfBundles[tlfID] = &tlfJournalBundle{
		mdJournal: mdJournal,
	}
	return nil
}

// Flush flushes the write journal for the given TLF.
func (j *JournalServer) Flush(ctx context.Context, tlfID TlfID) (err error) {
	j.log.Debug("Flushing journal for %s", tlfID)
	flushedBlockEntries := 0
	flushedMDEntries := 0
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Flushed %d block entries and %d MD entries "+
					"for %s, but got error %v",
				flushedBlockEntries, flushedMDEntries,
				tlfID, err)
		}
	}()
	bundle, ok := j.getBundle(tlfID)
	if !ok {
		j.log.Debug("Journal not enabled for %s", tlfID)
		return nil
	}

	// TODO: Flush block journal.

	_, uid, err := j.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return err
	}

	key, err := j.config.KBPKI().GetCurrentVerifyingKey(ctx)
	if err != nil {
		return err
	}

	for {
		flushed, err := func() (bool, error) {
			bundle.lock.Lock()
			defer bundle.lock.Unlock()
			return bundle.mdJournal.flushOne(
				ctx, j.log, j.config.Crypto(),
				uid, key, j.config.MDServer())
		}()
		if err != nil {
			return err
		}
		if !flushed {
			break
		}
		flushedMDEntries++
	}

	j.log.Debug("Flushed %d block entries and %d MD entries for %s",
		flushedBlockEntries, flushedMDEntries, tlfID)

	return nil
}

// Disable turns off the write journal for the given TLF.
func (j *JournalServer) Disable(tlfID TlfID) (err error) {
	j.log.Debug("Disabling journal for %s", tlfID)
	defer func() {
		if err != nil {
			j.deferLog.Debug(
				"Error when disabling journal for %s: %v",
				tlfID, err)
		}
	}()

	j.lock.Lock()
	defer j.lock.Unlock()
	bundle, ok := j.tlfBundles[tlfID]
	if !ok {
		j.log.Debug("Journal already disabled for %s", tlfID)
		return nil
	}

	bundle.lock.RLock()
	defer bundle.lock.RUnlock()
	length, err := bundle.mdJournal.length()
	if err != nil {
		return err
	}

	if length != 0 {
		return fmt.Errorf("Journal still has %d entries", length)
	}

	j.log.Debug("Disabled journal for %s", tlfID)

	delete(j.tlfBundles, tlfID)
	return nil
}

type journalBlockServer struct {
	jServer *JournalServer
	BlockServer
}

var _ BlockServer = journalBlockServer{}

func (j journalBlockServer) Put(
	ctx context.Context, id BlockID, tlfID TlfID, context BlockContext,
	buf []byte, serverHalf BlockCryptKeyServerHalf) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.Put(ctx, id, tlfID, context, buf, serverHalf)
}

func (j journalBlockServer) AddBlockReference(
	ctx context.Context, id BlockID, tlfID TlfID,
	context BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.AddBlockReference(ctx, id, tlfID, context)
}

func (j journalBlockServer) RemoveBlockReference(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) (
	liveCounts map[BlockID]int, err error) {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.RemoveBlockReference(ctx, tlfID, contexts)
}

func (j journalBlockServer) ArchiveBlockReferences(
	ctx context.Context, tlfID TlfID,
	contexts map[BlockID][]BlockContext) error {
	_, ok := j.jServer.getBundle(tlfID)
	if ok {
		// TODO: Delegate to bundle's block journal.
	}

	return j.BlockServer.ArchiveBlockReferences(ctx, tlfID, contexts)
}

type journalMDOps struct {
	MDOps
	jServer  *JournalServer
	mdServer MDServer
}

var _ MDOps = journalMDOps{}

// TODO: Prefer server updates to journal updates.

// TODO: Figure out locking.
func (j journalMDOps) getFromJournal(
	ctx context.Context, id TlfID, bid BranchID, mStatus MergeStatus,
	handle *TlfHandle) (
	ImmutableRootMetadata, error) {
	bundle, ok := j.jServer.getBundle(id)
	if !ok {
		return ImmutableRootMetadata{}, nil
	}

	_, uid, err := j.jServer.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	bundle.lock.RLock()
	defer bundle.lock.RUnlock()

	head, err := bundle.mdJournal.getHead(uid)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	if head == (ImmutableBareRootMetadata{}) {
		return ImmutableRootMetadata{}, nil
	}

	if head.MergedStatus() != mStatus {
		return ImmutableRootMetadata{}, nil
	}

	if mStatus == Unmerged && bid != NullBranchID && bid != head.BID {
		return ImmutableRootMetadata{}, nil
	}

	if handle == nil {
		bareHandle, err := head.MakeBareTlfHandle()
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
		handle, err = MakeTlfHandle(ctx, bareHandle, j.jServer.config.KBPKI())
		if err != nil {
			return ImmutableRootMetadata{}, err
		}
	}

	rmd := RootMetadata{
		BareRootMetadata: *head.BareRootMetadata,
		tlfHandle:        handle,
	}

	err = decryptMDPrivateData(
		ctx, j.jServer.config, &rmd, rmd.ReadOnly())
	if err != nil {
		return ImmutableRootMetadata{}, err
	}

	return MakeImmutableRootMetadata(&rmd, head.mdID), nil
}

func (j journalMDOps) GetForHandle(
	ctx context.Context, handle *TlfHandle, mStatus MergeStatus) (
	TlfID, ImmutableRootMetadata, error) {
	tlfID, rmd, err := j.MDOps.GetForHandle(ctx, handle, mStatus)
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}

	lookupTlfID := tlfID
	if rmd != (ImmutableRootMetadata{}) {
		lookupTlfID = rmd.ID
	}

	irmd, err := j.getFromJournal(ctx, lookupTlfID, NullBranchID, mStatus, handle)
	if err != nil {
		return TlfID{}, ImmutableRootMetadata{}, err
	}
	if irmd != (ImmutableRootMetadata{}) {
		return TlfID{}, irmd, nil
	}

	return tlfID, rmd, nil
}

func (j journalMDOps) GetForTLF(
	ctx context.Context, id TlfID) (ImmutableRootMetadata, error) {
	irmd, err := j.getFromJournal(ctx, id, NullBranchID, Merged, nil)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	if irmd != (ImmutableRootMetadata{}) {
		return irmd, nil
	}

	return j.MDOps.GetForTLF(ctx, id)
}

func (j journalMDOps) GetUnmergedForTLF(
	ctx context.Context, id TlfID, bid BranchID) (
	ImmutableRootMetadata, error) {
	irmd, err := j.getFromJournal(ctx, id, bid, Unmerged, nil)
	if err != nil {
		return ImmutableRootMetadata{}, err
	}
	if irmd != (ImmutableRootMetadata{}) {
		return irmd, nil
	}

	return j.MDOps.GetUnmergedForTLF(ctx, id, bid)
}

func (j journalMDOps) getRangeFromJournal(
	ctx context.Context, id TlfID, bid BranchID,
	start, stop MetadataRevision, mStatus MergeStatus) (
	[]ImmutableRootMetadata, error) {
	bundle, ok := j.jServer.getBundle(id)
	if !ok {
		return nil, nil
	}

	_, uid, err := j.jServer.config.KBPKI().GetCurrentUserInfo(ctx)
	if err != nil {
		return nil, err
	}

	bundle.lock.RLock()
	defer bundle.lock.RUnlock()

	ibrmds, err := bundle.mdJournal.getRange(uid, start, stop)
	if err != nil {
		return nil, err
	}

	if len(ibrmds) == 0 {
		return nil, nil
	}

	head := ibrmds[len(ibrmds)-1]

	if head.MergedStatus() != mStatus {
		return nil, nil
	}

	if mStatus == Unmerged && bid != NullBranchID && bid != head.BID {
		return nil, nil
	}

	bareHandle, err := head.MakeBareTlfHandle()
	if err != nil {
		return nil, err
	}
	handle, err := MakeTlfHandle(ctx, bareHandle, j.jServer.config.KBPKI())
	if err != nil {
		return nil, err
	}

	irmds := make([]ImmutableRootMetadata, 0, len(ibrmds))

	for _, ibrmd := range ibrmds {
		rmd := RootMetadata{
			BareRootMetadata: *ibrmd.BareRootMetadata,
			tlfHandle:        handle,
		}

		// TODO: Use head?
		err = decryptMDPrivateData(
			ctx, j.jServer.config, &rmd, rmd.ReadOnly())
		if err != nil {
			return nil, err
		}

		irmd := MakeImmutableRootMetadata(&rmd, ibrmd.mdID)
		irmds = append(irmds, irmd)
	}

	return irmds, nil
}

func (j journalMDOps) GetRange(
	ctx context.Context, id TlfID, start, stop MetadataRevision) (
	[]ImmutableRootMetadata, error) {
	jirmds, err := j.getRangeFromJournal(
		ctx, id, NullBranchID, start, stop, Merged)
	if err != nil {
		return nil, err
	}

	if len(jirmds) == 0 {
		return j.MDOps.GetRange(ctx, id, start, stop)
	}

	if jirmds[0].Revision == start {
		return jirmds, nil
	}

	serverStop := jirmds[0].Revision - 1
	irmds, err := j.MDOps.GetRange(ctx, id, start, serverStop)
	if err != nil {
		return nil, err
	}

	if len(irmds) == 0 {
		return jirmds, nil
	}

	if irmds[len(irmds)-1].Revision != serverStop {
		return nil, fmt.Errorf("Expected server rev %d, got %d", serverStop, irmds[len(irmds)-1].Revision)
	}

	return append(irmds, jirmds...), nil
}

func (j journalMDOps) GetUnmergedRange(
	ctx context.Context, id TlfID, bid BranchID,
	start, stop MetadataRevision) ([]ImmutableRootMetadata, error) {
	jirmds, err := j.getRangeFromJournal(
		ctx, id, bid, start, stop, Unmerged)
	if err != nil {
		return nil, err
	}

	if len(jirmds) == 0 {
		return j.MDOps.GetUnmergedRange(ctx, id, bid, start, stop)
	}

	if jirmds[0].Revision == start {
		return jirmds, nil
	}

	serverStop := jirmds[0].Revision - 1
	irmds, err := j.MDOps.GetUnmergedRange(ctx, id, bid, start, serverStop)
	if err != nil {
		return nil, err
	}

	if len(irmds) == 0 {
		return jirmds, nil
	}

	if irmds[len(irmds)-1].Revision != serverStop {
		return nil, fmt.Errorf("Expected server rev %d, got %d", serverStop, irmds[len(irmds)-1].Revision)
	}

	return append(irmds, jirmds...), nil
}

func (j journalMDOps) Put(ctx context.Context, rmd *RootMetadata) (
	MdID, error) {
	bundle, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		_, uid, err := j.jServer.config.KBPKI().GetCurrentUserInfo(ctx)
		if err != nil {
			return MdID{}, err
		}

		key, err := j.jServer.config.KBPKI().GetCurrentVerifyingKey(ctx)
		if err != nil {
			return MdID{}, err
		}

		bundle.lock.Lock()
		defer bundle.lock.Unlock()
		return bundle.mdJournal.put(ctx, j.jServer.config.Crypto(),
			j.jServer.config.KeyManager(), rmd, uid, key)
	}

	return j.MDOps.Put(ctx, rmd)
}

func (j journalMDOps) PutUnmerged(ctx context.Context, rmd *RootMetadata) (
	MdID, error) {
	bundle, ok := j.jServer.getBundle(rmd.ID)
	if ok {
		_, uid, err := j.jServer.config.KBPKI().GetCurrentUserInfo(ctx)
		if err != nil {
			return MdID{}, err
		}

		key, err := j.jServer.config.KBPKI().GetCurrentVerifyingKey(ctx)
		if err != nil {
			return MdID{}, err
		}

		rmd.WFlags |= MetadataFlagUnmerged
		if rmd.BID == NullBranchID {
			// TODO: Figure out race with PruneBranch.
			head, err := j.GetUnmergedForTLF(ctx, rmd.ID, NullBranchID)
			if err != nil {
				return MdID{}, err
			}
			if head == (ImmutableRootMetadata{}) {
				// new branch ID
				bid, err := j.jServer.config.Crypto().MakeRandomBranchID()
				if err != nil {
					return MdID{}, err
				}
				rmd.BID = bid
			} else {
				rmd.BID = head.BID
			}
		}

		bundle.lock.Lock()
		defer bundle.lock.Unlock()
		return bundle.mdJournal.put(ctx, j.jServer.config.Crypto(),
			j.jServer.config.KeyManager(), rmd, uid, key)
	}

	return j.MDOps.PutUnmerged(ctx, rmd)
}

func (j journalMDOps) PruneBranch(
	ctx context.Context, id TlfID, bid BranchID) error {
	bundle, ok := j.jServer.getBundle(id)
	if ok {
		irmd, err := j.getFromJournal(ctx, id, bid, Unmerged, nil)
		if err != nil {
			return err
		}
		if irmd.BID == bid {
			err := bundle.mdJournal.clear()
			if err != nil {
				return err
			}
		}
	}

	return j.MDOps.PruneBranch(ctx, id, bid)
}

func (j *JournalServer) blockServer() journalBlockServer {
	return journalBlockServer{j, j.delegateBlockServer}
}

func (j *JournalServer) mdOps() journalMDOps {
	return journalMDOps{j.delegateMDOps, j, j.delegateMDServer}
}

func makeJournalServer(
	config Config, log logger.Logger,
	dir string, bserver BlockServer,
	mdOps MDOps, mdServer MDServer) *JournalServer {
	jServer := JournalServer{
		config:              config,
		log:                 log,
		deferLog:            log.CloneWithAddedDepth(1),
		dir:                 dir,
		delegateBlockServer: bserver,
		delegateMDOps:       mdOps,
		delegateMDServer:    mdServer,
		tlfBundles:          make(map[TlfID]*tlfJournalBundle),
	}
	return &jServer
}
