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

type RekeyRequest struct {
	Delay time.Duration
	RekeyTask
}

type RekeyTask struct {
	Timeout     *time.Duration
	ttl         *int
	promptPaper bool

	injectContextForTest context.Context
}

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

type RekeyFinished struct {
	RekeyResult
	err error
}

type rekeyEvent struct {
	eventType     rekeyEventType
	request       *RekeyRequest
	rekeyFinished *RekeyFinished
	// rekeyTimeupEvent has no argument
}

func NewRekeyRequestEvent(req RekeyRequest) rekeyEvent {
	return rekeyEvent{
		eventType: rekeyRequestEvent,
		request:   &req,
	}
}

func newRekeyFinishedEvent(res RekeyResult, err error) rekeyEvent {
	return rekeyEvent{
		eventType: rekeyFinishedEvent,
		rekeyFinished: &RekeyFinished{
			RekeyResult: res,
			err:         err,
		},
	}
}

func newRekeyTimeupEvent() rekeyEvent {
	return rekeyEvent{
		eventType: rekeyTimeupEvent,
	}
}

func newRekeyShutdownEvent() rekeyEvent {
	return rekeyEvent{
		eventType: rekeyShutdownEvent,
	}
}

func newRekeyKickoffEventForTest() rekeyEvent {
	return rekeyEvent{
		eventType: rekeyKickoffEventForTest,
	}
}

func newRekeyCancelEventForTest() rekeyEvent {
	return rekeyEvent{
		eventType: rekeyCancelEventForTest,
	}
}

type rekeyState interface {
	reactToEvent(event rekeyEvent) rekeyState
}

type rekeyStateIdle struct {
	fsm *rekeyFSM
}

func newRekeyStateIdle(fsm *rekeyFSM) *rekeyStateIdle {
	return &rekeyStateIdle{fsm: fsm}
}

func (r *rekeyStateIdle) reactToEvent(event rekeyEvent) rekeyState {
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

func (r *rekeyStateScheduled) reactToEvent(event rekeyEvent) rekeyState {
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

func (r *rekeyStateStarted) reactToEvent(event rekeyEvent) rekeyState {
	// TODO: make sure fbo doesn't assumed request during "Started" state will be
	// processed
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

// TODO: move this into interfaces.go
type RekeyFSM interface {
	Event(event rekeyEvent)
	Shutdown()

	listenOnEventForTest(event rekeyEventType, callback func(rekeyEvent), repeatedly bool)
}

type listenerForTest struct {
	once *sync.Once
	body func(rekeyEvent)
}

func (l *listenerForTest) do(e rekeyEvent) {
	if l.once != nil {
		l.once.Do(func() {
			l.body(e)
		})
	} else {
		l.body(e)
	}
}

// TODO: report status in FolderBranchStatus?
type rekeyFSM struct {
	reqs chan rekeyEvent
	fbo  *folderBranchOps

	current rekeyState

	muListenersForTest sync.RWMutex
	listenersForTest   map[rekeyEventType][]*listenerForTest
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

func NewRekeyFSM(fbo *folderBranchOps) RekeyFSM {
	fsm := &rekeyFSM{
		reqs: make(chan rekeyEvent, rekeyQueueSize),
		fbo:  fbo,

		listenersForTest: make(map[rekeyEventType][]*listenerForTest),
	}
	fsm.current = newRekeyStateIdle(fsm)
	go fsm.loop()
	return fsm
}

func (m *rekeyFSM) Event(event rekeyEvent) {
	select {
	case m.reqs <- event:
	default:
		go func() { m.reqs <- event }()
	}
}

func (m *rekeyFSM) Shutdown() {
	m.Event(newRekeyShutdownEvent())
}

func (m *rekeyFSM) triggerCallbacksForTest(e rekeyEvent) {
	m.muListenersForTest.RLock()
	cbs := m.listenersForTest[e.eventType]
	m.muListenersForTest.RUnlock()
	for _, cb := range cbs {
		cb.do(e)
	}
}

func (m *rekeyFSM) listenOnEventForTest(
	event rekeyEventType, callback func(rekeyEvent), repeatedly bool) {
	m.muListenersForTest.Lock()
	defer m.muListenersForTest.Unlock()
	var once *sync.Once
	if !repeatedly {
		once = &sync.Once{}
	}
	m.listenersForTest[event] = append(m.listenersForTest[event], &listenerForTest{
		body: callback,
		once: once,
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

func RequestRekeyAndWaitForOneFinishEventForTest(
	ops KBFSOps, tlfID tlf.ID) (res RekeyResult, err error) {
	fsm := getRekeyFSMForTest(ops, tlfID)
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
