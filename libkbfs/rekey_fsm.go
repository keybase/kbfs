package libkbfs

import (
	"context"
	"sync"
	"time"

	"github.com/keybase/kbfs/tlf"
)

// CtxRekeyTagKey is the type used for unique context tags within an
// enqueued Rekey.
type CtxRekeyTagKey int

const (
	// CtxRekeyIDKey is the type of the tag for unique operation IDs
	// within an enqueued Rekey.
	CtxRekeyIDKey CtxRekeyTagKey = iota
)

// CtxRekeyOpID is the display name for the unique operation
// enqueued rekey ID tag.
const CtxRekeyOpID = "REKEYID"

type rekeyEventType int

const (
	_ rekeyEventType = iota
	rekeyRequestEvent
	rekeyFinishedEvent
	rekeyTimeupEvent

	rekeyShutdownEvent

	rekeyKickoffEventForTest
	rekeyCancelEventForTest
)

// RekeyTask describes a rekey task.
type RekeyTask struct {
	// Timeout, if non-nil, causes rekey to fail if it takes more than this
	// duration since it enters rekeyStateStarted.
	Timeout     *time.Duration
	ttl         *int
	promptPaper bool

	// injectContextForTest, if non-nil, is used when calling fbo.rekeyLocked.
	injectContextForTest context.Context
}

// RekeyRequest describes a rekey request.
type RekeyRequest struct {
	// Delay is the duration to wait for since the request enters the FSM until
	// starting the rekey.
	Delay time.Duration
	RekeyTask
}

// RekeyFinished describes a rekeyFinishedEvent. It contains results from an
// actual rekey operation.
type RekeyFinished struct {
	RekeyResult
	err error
}

// RekeyEvent describes an event to send into the RekeyFSM. A function, e.g.,
// NewRekeyRequestEvent, should be used to construct one
type RekeyEvent struct {
	eventType     rekeyEventType
	request       *RekeyRequest
	rekeyFinished *RekeyFinished
}

// NewRekeyRequestEvent creates a rekey request Event with req.
func NewRekeyRequestEvent(req RekeyRequest) RekeyEvent {
	return RekeyEvent{
		eventType: rekeyRequestEvent,
		request:   &req,
	}
}

func newRekeyFinishedEvent(res RekeyResult, err error) RekeyEvent {
	return RekeyEvent{
		eventType: rekeyFinishedEvent,
		rekeyFinished: &RekeyFinished{
			RekeyResult: res,
			err:         err,
		},
	}
}

func newRekeyTimeupEvent() RekeyEvent {
	return RekeyEvent{
		eventType: rekeyTimeupEvent,
	}
}

func newRekeyShutdownEvent() RekeyEvent {
	return RekeyEvent{
		eventType: rekeyShutdownEvent,
	}
}

func newRekeyKickoffEventForTest() RekeyEvent {
	return RekeyEvent{
		eventType: rekeyKickoffEventForTest,
	}
}

func newRekeyCancelEventForTest() RekeyEvent {
	return RekeyEvent{
		eventType: rekeyCancelEventForTest,
	}
}

type rekeyState interface {
	reactToEvent(event RekeyEvent) rekeyState
}

type rekeyStateIdle struct {
	fsm *rekeyFSM
}

func newRekeyStateIdle(fsm *rekeyFSM) *rekeyStateIdle {
	return &rekeyStateIdle{fsm: fsm}
}

func (r *rekeyStateIdle) reactToEvent(event RekeyEvent) rekeyState {
	switch event.eventType {
	case rekeyRequestEvent:
		return newRekeyStateScheduled(r.fsm, *event.request)
	default:
		return r
	}
}

type rekeyStateScheduled struct {
	fsm *rekeyFSM

	timer    *time.Timer
	deadline time.Time

	task RekeyTask
}

func newRekeyStateScheduled(fsm *rekeyFSM, req RekeyRequest) *rekeyStateScheduled {
	defaultTTL := rekeyInitialTTL
	if req.ttl == nil {
		req.ttl = &defaultTTL
	}

	return &rekeyStateScheduled{
		fsm: fsm,
		timer: time.AfterFunc(req.Delay, func() {
			fsm.Event(newRekeyTimeupEvent())
		}),
		deadline: time.Now().Add(req.Delay),
		task:     req.RekeyTask,
	}
}

func (r *rekeyStateScheduled) reactToEvent(event RekeyEvent) rekeyState {
	switch event.eventType {
	case rekeyTimeupEvent:
		return newRekeyStateStarted(r.fsm, r.task)
	case rekeyRequestEvent:
		if !r.deadline.After(time.Now().Add(event.request.Delay)) {
			r.task.promptPaper = event.request.promptPaper
			return r
		}
		r.timer.Stop()
		return newRekeyStateScheduled(r.fsm, *event.request)
	case rekeyKickoffEventForTest:
		r.timer.Reset(time.Millisecond)
		return r
	case rekeyCancelEventForTest:
		r.timer.Stop()
		return newRekeyStateIdle(r.fsm)
	case rekeyShutdownEvent:
		r.timer.Stop()
		return r
	default:
		return r
	}
}

type rekeyStateStarted struct {
	fsm  *rekeyFSM
	task RekeyTask
}

func newRekeyStateStarted(fsm *rekeyFSM, task RekeyTask) *rekeyStateStarted {
	ctx := task.injectContextForTest
	if ctx == nil {
		ctx = ctxWithRandomIDReplayable(context.Background(), CtxRekeyIDKey,
			CtxRekeyOpID, nil)
	}
	var cancel context.CancelFunc
	if task.Timeout != nil {
		ctx, cancel = context.WithTimeout(ctx, *task.Timeout)
	}
	go func() {
		fsm.fbo.log.CDebugf(ctx, "Processing rekey for %s", fsm.fbo.folderBranch.Tlf)
		var res RekeyResult
		err := fsm.fbo.doMDWriteWithRetryUnlessCanceled(ctx,
			func(lState *lockState) (err error) {
				res, err = fsm.fbo.rekeyLocked(ctx, lState, false)
				return err
			})
		fsm.fbo.log.CDebugf(ctx, "Rekey finished with res=%#+v, error=%v", res, err)
		fsm.Event(newRekeyFinishedEvent(res, err))
		if cancel != nil {
			cancel()
		}
	}()
	return &rekeyStateStarted{
		fsm:  fsm,
		task: task,
	}
}

func (r *rekeyStateStarted) reactToEvent(event RekeyEvent) rekeyState {
	ttl := *r.task.ttl - 1
	switch event.eventType {
	case rekeyFinishedEvent:
		switch event.rekeyFinished.err {
		case nil:
			// TODO logging
		default:
			// TODO logging
			if ttl > 0 {
				return newRekeyStateScheduled(r.fsm, RekeyRequest{
					Delay: 0,
					RekeyTask: RekeyTask{
						Timeout:     r.task.Timeout,
						promptPaper: r.task.promptPaper,
						ttl:         &ttl,
					},
				})
			}
			return newRekeyStateIdle(r.fsm)
		}

		d := r.fsm.fbo.config.RekeyWithPromptWaitTime()

		if ttl > 0 && event.rekeyFinished.NeedsPaperKey {
			return newRekeyStateScheduled(r.fsm, RekeyRequest{
				Delay: d,
				RekeyTask: RekeyTask{
					Timeout:     &d,
					promptPaper: true,
					ttl:         &ttl,
				},
			})
		}

		if ttl > 0 && event.rekeyFinished.DidRekey {
			// We enqueue the rekey here again, in case we missed a device due to a
			// race condition. This is specifically for the situation where user
			// provisions two devices in a row, and the key update for the 2nd device
			// only comes in after rekey for a TLF is done, which didn't include the
			// second device. At this point, there wouldn't be a new MD with rekey
			// bit set since it's already set. As a result, the TLF won't get rekeyed
			// for the second device until the next 1-hour timer triggers another
			// scan.
			return newRekeyStateScheduled(r.fsm, RekeyRequest{
				Delay: rekeyRecheckInterval,
				RekeyTask: RekeyTask{
					promptPaper: false,
					ttl:         &ttl,
				},
			})
		}

		return newRekeyStateIdle(r.fsm)
	default:
		return r
	}
}

type listenerForTest struct {
	repeatedly bool
	body       func(RekeyEvent)
}

type rekeyFSM struct {
	reqs chan RekeyEvent
	fbo  *folderBranchOps

	current rekeyState

	muListenersForTest sync.Mutex
	listenersForTest   map[rekeyEventType][]listenerForTest
}

func (m *rekeyFSM) loop() {
	for e := range m.reqs {
		if e.eventType == rekeyShutdownEvent {
			close(m.reqs)
		}

		m.current = m.current.reactToEvent(e)

		m.triggerCallbacksForTest(e)
	}
}

// NewRekeyFSM creates a new rekey FSM.
func NewRekeyFSM(fbo *folderBranchOps) RekeyFSM {
	fsm := &rekeyFSM{
		reqs: make(chan RekeyEvent, rekeyQueueSize),
		fbo:  fbo,

		listenersForTest: make(map[rekeyEventType][]listenerForTest),
	}
	fsm.current = newRekeyStateIdle(fsm)
	go fsm.loop()
	return fsm
}

// Event implements RekeyFSM interface for rekeyFSM.
func (m *rekeyFSM) Event(event RekeyEvent) {
	select {
	case m.reqs <- event:
	default:
		go func() { m.reqs <- event }()
	}
}

// Shutdown implements RekeyFSM interface for rekeyFSM.
func (m *rekeyFSM) Shutdown() {
	m.Event(newRekeyShutdownEvent())
}

func (m *rekeyFSM) triggerCallbacksForTest(e RekeyEvent) {
	var cbs []listenerForTest
	func() {
		m.muListenersForTest.Lock()
		defer m.muListenersForTest.Unlock()
		cbs = m.listenersForTest[e.eventType]
		m.listenersForTest[e.eventType] = nil
		for _, cb := range cbs {
			if cb.repeatedly {
				m.listenersForTest[e.eventType] = append(
					m.listenersForTest[e.eventType], cb)
			}
		}
	}()
	for _, cb := range cbs {
		cb.body(e)
	}
}

// listenOnEventForTest implements RekeyFSM interface for rekeyFSM.
func (m *rekeyFSM) listenOnEventForTest(
	event rekeyEventType, callback func(RekeyEvent), repeatedly bool) {
	m.muListenersForTest.Lock()
	defer m.muListenersForTest.Unlock()
	m.listenersForTest[event] = append(m.listenersForTest[event], listenerForTest{
		body:       callback,
		repeatedly: repeatedly,
	})
}

func getRekeyFSMForTest(ops KBFSOps, tlfID tlf.ID) RekeyFSM {
	switch o := ops.(type) {
	case *KBFSOpsStandard:
		return o.getOpsNoAdd(FolderBranch{Tlf: tlfID, Branch: MasterBranch}).rekeyFSM
	case *folderBranchOps:
		return o.rekeyFSM
	}
	return nil
}

// RequestRekeyAndWaitForOneFinishEventForTest sends a rekey request to the FSM
// associated with tlfID, and wait for exact one rekeyFinished event. This can
// be useful for waiting for a rekey result in tests.
func RequestRekeyAndWaitForOneFinishEventForTest(
	ops KBFSOps, tlfID tlf.ID) (res RekeyResult, err error) {
	fsm := getRekeyFSMForTest(ops, tlfID)
	rekeyWaiter := make(chan struct{})
	// now user 1 should rekey
	fsm.listenOnEventForTest(rekeyFinishedEvent, func(e RekeyEvent) {
		res = e.rekeyFinished.RekeyResult
		err = e.rekeyFinished.err
		close(rekeyWaiter)
	}, false)
	ops.RequestRekey(tlfID)
	<-rekeyWaiter
	return res, err
}
