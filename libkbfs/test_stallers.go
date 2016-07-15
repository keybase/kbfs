// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"

	"golang.org/x/net/context"
)

type stallableOp string

// StallableBlockOp defines an Op that is stallable using StallBlockOp
type StallableBlockOp stallableOp

// StallableMDOp defines an Op that is stallable using StallMDOp
type StallableMDOp stallableOp

// stallable Block Ops and MD Ops
const (
	StallableBlockGet     StallableBlockOp = "Get"
	StallableBlockReady   StallableBlockOp = "Ready"
	StallableBlockPut     StallableBlockOp = "Put"
	StallableBlockDelete  StallableBlockOp = "Delete"
	StallableBlockArchive StallableBlockOp = "Archive"

	StallableMDGetForHandle          StallableMDOp = "GetForHandle"
	StallableMDGetUnmergedForHandle  StallableMDOp = "GetUnmergedForHandle"
	StallableMDGetForTLF             StallableMDOp = "GetForTLF"
	StallableMDGetLatestHandleForTLF StallableMDOp = "GetLatestHandleForTLF"
	StallableMDGetUnmergedForTLF     StallableMDOp = "GetUnmergedForTLF"
	StallableMDGetRange              StallableMDOp = "GetRange"
	StallableMDGetUnmergedRange      StallableMDOp = "GetUnmergedRange"
	StallableMDPut                   StallableMDOp = "Put"
	StallableMDAfterPut              StallableMDOp = "AfterPut"
	StallableMDPutUnmerged           StallableMDOp = "PutUnmerged"
)

const stallKeyStallEverything = ""

type naïveStallInfo struct {
	onStalled <-chan struct{}
	unstall   chan<- struct{}

	// A blockOps or mdOps stores the stalling Ops associates with this
	// naïveStallInfo.
	blockOps *stallingBlockOps
	mdOps    *stallingMDOps

	// outter is associated with the stalling Ops that uses this stalling Ops as
	// internal delegate to perform actual ops.
	outter *naïveStallInfo
}

// NaïveStaller is used to stall certain ops in BlockOps or MDOps. Unlike
// StallBlockOp and StallMDOp which provides a way to precisely control which
// particular op is stalled by passing in ctx with corresponding stallKey,
// NaïveStaller simply stalls all instances of specified op. Internally
// NaïveStaller uses a delegate chain in order to be able to remove a specific
// stall.
type NaïveStaller struct {
	// protects the entire delegate chain
	mu             sync.RWMutex
	config         Config
	blockOpsStalls map[StallableBlockOp]*naïveStallInfo
	mdOpsStalls    map[StallableMDOp]*naïveStallInfo
	head           *naïveStallInfo
}

// NewNaïveStaller returns a new NaïveStaller
func NewNaïveStaller(config Config) *NaïveStaller {
	return &NaïveStaller{
		config:         config,
		blockOpsStalls: make(map[StallableBlockOp]*naïveStallInfo),
		mdOpsStalls:    make(map[StallableMDOp]*naïveStallInfo),
	}
}

func (s *NaïveStaller) getNaïveStallInfoForBlockOpOrBustLocked(
	stalledOp StallableBlockOp) *naïveStallInfo {
	info, ok := s.blockOpsStalls[stalledOp]
	if !ok {
		panic("naïveStallInfo is not found." +
			"This indicates incorrect use of NaïveStaller")
	}
	return info
}

func (s *NaïveStaller) getNaïveStallInfoForMDOpOrBustLocked(
	stalledOp StallableMDOp) *naïveStallInfo {
	info, ok := s.mdOpsStalls[stalledOp]
	if !ok {
		panic("naïveStallInfo is not found." +
			"This indicates incorrect use of NaïveStaller")
	}
	return info
}

func (s *NaïveStaller) getNaïveStallInfoForBlockOpOrBust(
	stalledOp StallableBlockOp) *naïveStallInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getNaïveStallInfoForBlockOpOrBustLocked(stalledOp)
}

func (s *NaïveStaller) getNaïveStallInfoForMDOpOrBust(
	stalledOp StallableMDOp) *naïveStallInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getNaïveStallInfoForMDOpOrBustLocked(stalledOp)
}

// StallBlockOp wraps the internal BlockOps so that all subsequent stalledOp
// will be stalled. This can be undone by calling UndoStallBlockOp.
func (s *NaïveStaller) StallBlockOp(stalledOp StallableBlockOp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	onStalledCh := make(chan struct{}, 1)
	unstallCh := make(chan struct{})
	oldBlockOps := s.config.BlockOps()
	stallingOps := &stallingBlockOps{
		stallOpName: stalledOp,
		stallKey:    stallKeyStallEverything,
		staller: staller{
			stalled: onStalledCh,
			unstall: unstallCh,
		},
		delegate: oldBlockOps,
	}
	s.config.SetBlockOps(stallingOps)
	stallInfo := &naïveStallInfo{
		onStalled: onStalledCh,
		unstall:   unstallCh,
		blockOps:  stallingOps,
	}
	if _, ok := s.blockOpsStalls[stalledOp]; ok {
		panic("incorrect use of NaïveStaller; requested Op is already stalled")
	}
	s.blockOpsStalls[stalledOp] = stallInfo
	if s.head != nil {
		s.head.outter = stallInfo
	}
	s.head = stallInfo
}

// StallMDOp wraps the internal MDOps so that all subsequent stalledOp
// will be stalled. This can be undone by calling UndoStallMDOp.
func (s *NaïveStaller) StallMDOp(stalledOp StallableMDOp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	onStalledCh := make(chan struct{}, 1)
	unstallCh := make(chan struct{})
	oldMDOps := s.config.MDOps()
	stallingOps := &stallingMDOps{
		stallOpName: stalledOp,
		stallKey:    stallKeyStallEverything,
		staller: staller{
			stalled: onStalledCh,
			unstall: unstallCh,
		},
		delegate: oldMDOps,
	}
	s.config.SetMDOps(stallingOps)
	stallInfo := &naïveStallInfo{
		onStalled: onStalledCh,
		unstall:   unstallCh,
		mdOps:     stallingOps,
	}
	if _, ok := s.mdOpsStalls[stalledOp]; ok {
		panic("incorrect use of NaïveStaller; requested Op is already stalled")
	}
	s.mdOpsStalls[stalledOp] = stallInfo
	if s.head != nil {
		s.head.outter = stallInfo
	}
	s.head = stallInfo
}

// WaitForStallBlockOp blocks until stalledOp is stalled. StallBlockOp should
// have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) WaitForStallBlockOp(stalledOp StallableBlockOp) {
	<-s.getNaïveStallInfoForBlockOpOrBust(stalledOp).onStalled
}

// WaitForStallMDOp blocks until stalledOp is stalled. StallMDOp should
// have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) WaitForStallMDOp(stalledOp StallableMDOp) {
	<-s.getNaïveStallInfoForMDOpOrBust(stalledOp).onStalled
}

// UnstallOneBlockOp unstalls exactly one stalled stalledOp. StallBlockOp
// should have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) UnstallOneBlockOp(stalledOp StallableBlockOp) {
	s.getNaïveStallInfoForBlockOpOrBust(stalledOp).unstall <- struct{}{}
}

// UnstallOneMDOp unstalls exactly one stalled stalledOp. StallMDOp
// should have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) UnstallOneMDOp(stalledOp StallableMDOp) {
	s.getNaïveStallInfoForMDOpOrBust(stalledOp).unstall <- struct{}{}
}

// UndoStallBlockOp reverts StallBlockOp so that future stalledOp are not
// stalled anymore. It also unstalls any stalled stalledOp. StallBlockOp
// should have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) UndoStallBlockOp(stalledOp StallableBlockOp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.getNaïveStallInfoForBlockOpOrBustLocked(stalledOp)
	if ns.outter != nil {
		ns.outter.blockOps.setDelegate(ns.blockOps.getDelegate())
	} else {
		s.config.SetBlockOps(ns.blockOps.getDelegate())
	}
	close(ns.unstall)
	delete(s.blockOpsStalls, stalledOp)
}

// UndoStallMDOp reverts StallMDOp so that future stalledOp are not
// stalled anymore. It also unstalls any stalled stalledOp. StallMDOp
// should have been called upon stalledOp, otherwise this would panic.
func (s *NaïveStaller) UndoStallMDOp(stalledOp StallableMDOp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.getNaïveStallInfoForMDOpOrBustLocked(stalledOp)
	if ns.outter != nil {
		ns.outter.mdOps.setDelegate(ns.mdOps.getDelegate())
	} else {
		s.config.SetMDOps(ns.mdOps.getDelegate())
	}
	close(ns.unstall)
	delete(s.mdOpsStalls, stalledOp)
}

// StallBlockOp sets a wrapped BlockOps in config so that the specified Op, stalledOp,
// is stalled. Caller should use the returned newCtx for subsequent operations
// for the stall to be effective. onStalled is a channel to notify the caller
// when the stall has happened. unstall is a channel for caller to unstall an
// Op.
func StallBlockOp(ctx context.Context, config Config, stalledOp StallableBlockOp) (
	onStalled <-chan struct{}, unstall chan<- struct{}, newCtx context.Context) {
	onStalledCh := make(chan struct{}, 1)
	unstallCh := make(chan struct{})
	stallKey := newStallKey()
	config.SetBlockOps(&stallingBlockOps{
		stallOpName: stalledOp,
		stallKey:    stallKey,
		staller: staller{
			stalled: onStalledCh,
			unstall: unstallCh,
		},
		delegate: config.BlockOps(),
	})
	newCtx = context.WithValue(ctx, stallKey, true)
	return onStalledCh, unstallCh, newCtx
}

// StallMDOp sets a wrapped MDOps in config so that the specified Op,
// stalledOp, is stalled. Caller should use the returned newCtx for subsequent
// operations for the stall to be effective. onStalled is a channel to notify
// the caller when the stall has happened. unstall is a channel for caller to
// unstall an Op.
func StallMDOp(ctx context.Context, config Config, stalledOp StallableMDOp) (
	onStalled <-chan struct{}, unstall chan<- struct{}, newCtx context.Context) {
	onStalledCh := make(chan struct{}, 1)
	unstallCh := make(chan struct{})
	stallKey := newStallKey()
	config.SetMDOps(&stallingMDOps{
		stallOpName: stalledOp,
		stallKey:    stallKey,
		staller: staller{
			stalled: onStalledCh,
			unstall: unstallCh,
		},
		delegate: config.MDOps(),
	})
	newCtx = context.WithValue(ctx, stallKey, true)
	return onStalledCh, unstallCh, newCtx
}

var stallerCounter uint64

func newStallKey() string {
	stallerCounter++
	return fmt.Sprintf("stallKey-%d", stallerCounter)
}

// staller is a pair of channels. Whenever something is to be
// stalled, a value is sent on stalled (if not blocked), and then
// unstall is waited on.
type staller struct {
	stalled chan<- struct{}
	unstall <-chan struct{}
}

func maybeStall(ctx context.Context, opName stallableOp,
	stallOpName stallableOp, stallKey string,
	staller staller) {
	if opName != stallOpName {
		return
	}

	if stallKey != stallKeyStallEverything {
		if v, ok := ctx.Value(stallKey).(bool); !ok || !v {
			return
		}
	}

	select {
	case staller.stalled <- struct{}{}:
	default:
	}
	<-staller.unstall
}

// runWithContextCheck checks ctx.Done() before and after running action. If
// either ctx.Done() check has error, ctx's error is returned. Otherwise,
// action's returned value is returned.
func runWithContextCheck(ctx context.Context, action func(ctx context.Context) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	err := action(ctx)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return err
}

// stallingBlockOps is an implementation of BlockOps whose operations
// sometimes stall. In particular, if the operation name matches
// stallOpName, and ctx.Value(stallKey) is a key in the corresponding
// staller is used to stall the operation.
type stallingBlockOps struct {
	stallOpName StallableBlockOp
	// stallKey is a key for switching on/off stalling. If it's present in ctx,
	// and equal to `true`, the operation is stalled. This allows us to use the
	// ctx to control stallings
	stallKey string
	staller  staller

	mu       sync.RWMutex
	delegate BlockOps
}

var _ BlockOps = (*stallingBlockOps)(nil)

func (f *stallingBlockOps) getDelegate() BlockOps {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.delegate
}

func (f *stallingBlockOps) setDelegate(delegate BlockOps) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delegate = delegate
}

func (f *stallingBlockOps) maybeStall(ctx context.Context, opName StallableBlockOp) {
	maybeStall(ctx, stallableOp(opName), stallableOp(f.stallOpName),
		f.stallKey, f.staller)
}

func (f *stallingBlockOps) Get(
	ctx context.Context, md *RootMetadata, blockPtr BlockPointer,
	block Block) error {
	f.maybeStall(ctx, StallableBlockGet)
	return runWithContextCheck(ctx, func(ctx context.Context) error {
		return f.getDelegate().Get(ctx, md, blockPtr, block)
	})
}

func (f *stallingBlockOps) Ready(
	ctx context.Context, md *RootMetadata, block Block) (
	id BlockID, plainSize int, readyBlockData ReadyBlockData, err error) {
	f.maybeStall(ctx, StallableBlockReady)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errReady error
		id, plainSize, readyBlockData, errReady = f.getDelegate().Ready(ctx, md, block)
		return errReady
	})
	return id, plainSize, readyBlockData, err
}

func (f *stallingBlockOps) Put(
	ctx context.Context, md *RootMetadata, blockPtr BlockPointer,
	readyBlockData ReadyBlockData) error {
	f.maybeStall(ctx, StallableBlockPut)
	return runWithContextCheck(ctx, func(ctx context.Context) error {
		return f.getDelegate().Put(ctx, md, blockPtr, readyBlockData)
	})
}

func (f *stallingBlockOps) Delete(
	ctx context.Context, md *RootMetadata,
	ptrs []BlockPointer) (notDeleted map[BlockID]int, err error) {
	f.maybeStall(ctx, StallableBlockDelete)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errDelete error
		notDeleted, errDelete = f.getDelegate().Delete(ctx, md, ptrs)
		return errDelete
	})
	return notDeleted, err
}

func (f *stallingBlockOps) Archive(
	ctx context.Context, md *RootMetadata, ptrs []BlockPointer) error {
	f.maybeStall(ctx, StallableBlockArchive)
	return runWithContextCheck(ctx, func(ctx context.Context) error {
		return f.getDelegate().Archive(ctx, md, ptrs)
	})
}

// stallingMDOps is an implementation of MDOps whose operations
// sometimes stall. In particular, if the operation name matches
// stallOpName, and ctx.Value(stallKey) is a key in the corresponding
// staller is used to stall the operation.
type stallingMDOps struct {
	stallOpName StallableMDOp
	// stallKey is a key for switching on/off stalling. If it's present in ctx,
	// and equal to `true`, the operation is stalled. This allows us to use the
	// ctx to control stallings
	stallKey string
	staller  staller

	mu       sync.RWMutex
	delegate MDOps
}

var _ MDOps = (*stallingMDOps)(nil)

func (m *stallingMDOps) getDelegate() MDOps {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.delegate
}

func (m *stallingMDOps) setDelegate(delegate MDOps) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delegate = delegate
}

func (m *stallingMDOps) maybeStall(ctx context.Context, opName StallableMDOp) {
	maybeStall(ctx, stallableOp(opName), stallableOp(m.stallOpName),
		m.stallKey, m.staller)
}

func (m *stallingMDOps) GetForHandle(ctx context.Context, handle *TlfHandle) (
	md *RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetForHandle)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetForHandle error
		md, errGetForHandle = m.getDelegate().GetForHandle(ctx, handle)
		return errGetForHandle
	})
	return md, err
}

func (m *stallingMDOps) GetUnmergedForHandle(ctx context.Context,
	handle *TlfHandle) (md *RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetUnmergedForHandle)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetUnmergedForHandle error
		md, errGetUnmergedForHandle = m.getDelegate().GetUnmergedForHandle(ctx, handle)
		return errGetUnmergedForHandle
	})
	return md, err
}

func (m *stallingMDOps) GetForTLF(ctx context.Context, id TlfID) (
	md *RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetForTLF)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetForTLF error
		md, errGetForTLF = m.getDelegate().GetForTLF(ctx, id)
		return errGetForTLF
	})
	return md, err
}

func (m *stallingMDOps) GetLatestHandleForTLF(ctx context.Context, id TlfID) (
	h BareTlfHandle, err error) {
	m.maybeStall(ctx, StallableMDGetLatestHandleForTLF)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetLatestHandleForTLF error
		h, errGetLatestHandleForTLF = m.getDelegate().GetLatestHandleForTLF(ctx, id)
		return errGetLatestHandleForTLF
	})
	return h, err
}

func (m *stallingMDOps) GetUnmergedForTLF(ctx context.Context, id TlfID,
	bid BranchID) (md *RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetUnmergedForTLF)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetUnmergedForTLF error
		md, errGetUnmergedForTLF = m.getDelegate().GetUnmergedForTLF(ctx, id, bid)
		return errGetUnmergedForTLF
	})
	return md, err
}

func (m *stallingMDOps) GetRange(ctx context.Context, id TlfID,
	start, stop MetadataRevision) (
	mds []*RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetRange)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetRange error
		mds, errGetRange = m.getDelegate().GetRange(ctx, id, start, stop)
		return errGetRange
	})
	return mds, err
}

func (m *stallingMDOps) GetUnmergedRange(ctx context.Context, id TlfID,
	bid BranchID, start, stop MetadataRevision) (mds []*RootMetadata, err error) {
	m.maybeStall(ctx, StallableMDGetUnmergedRange)
	err = runWithContextCheck(ctx, func(ctx context.Context) error {
		var errGetUnmergedRange error
		mds, errGetUnmergedRange = m.getDelegate().GetUnmergedRange(ctx, id, bid, start, stop)
		return errGetUnmergedRange
	})
	return mds, err
}

func (m *stallingMDOps) Put(ctx context.Context, md *RootMetadata) error {
	m.maybeStall(ctx, StallableMDPut)
	// If the Put was canceled, return the cancel error.  This
	// emulates the Put being canceled while the RPC is outstanding.
	return runWithContextCheck(ctx, func(ctx context.Context) error {
		err := m.getDelegate().Put(ctx, md)
		m.maybeStall(ctx, StallableMDAfterPut)
		return err
	})
}

func (m *stallingMDOps) PutUnmerged(ctx context.Context, md *RootMetadata,
	bid BranchID) error {
	m.maybeStall(ctx, StallableMDPutUnmerged)
	// If the PutUnmerged was canceled, return the cancel error.  This
	// emulates the PutUnmerged being canceled while the RPC is
	// outstanding.
	return runWithContextCheck(ctx, func(ctx context.Context) error {
		return m.getDelegate().PutUnmerged(ctx, md, bid)
	})
}
