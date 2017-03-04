package libkbfs

import (
	"context"
	"time"
)

type RekeyReason int

const (
	RekeyReasonGeneric RekeyReason = iota
)

type RekeyRequest struct {
	Delay time.Duration
	RekeyTask
}

type RekeyTask struct {
	Timeout     *time.Duration
	ttl         *int
	promptPaper bool
}

type rekeyEventType int

const (
	_ rekeyEventType = iota
	rekeyRequestEvent
	rekeyFinishedEvent
	rekeyTimeupEvent

	rekeyShutdownEvent
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

type rekeyState interface {
	reactToEvent(event rekeyEvent) rekeyState
}

type rekeyStateIdle struct {
	fsm RekeyFSM
}

func newRekeyStateIdle(fsm RekeyFSM) *rekeyStateIdle {
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
	fsm RekeyFSM

	timer    *time.Timer
	deadline time.Time

	task RekeyTask
}

func newRekeyStateScheduled(fsm RekeyFSM, req RekeyRequest) *rekeyStateScheduled {
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
			return r
		}
		r.timer.Stop()
		return newRekeyStateScheduled(r.fsm, *event.request)
	default:
		r.task.promptPaper = event.request.promptPaper
		return r
	}
}

// TODO: split this state into multiple representing different rekey states,
// e.g. waiting for paper key prompt, etc.
type rekeyStateStarted struct {
	fsm  RekeyFSM
	task RekeyTask
}

func newRekeyStateStarted(fsm RekeyFSM, task RekeyTask) *rekeyStateStarted {
	ctx := ctxWithRandomIDReplayable(context.Background(), CtxRekeyIDKey,
		CtxRekeyOpID, nil)
	var cancel context.CancelFunc
	if task.Timeout != nil {
		ctx, cancel = context.WithTimeout(ctx, *task.Timeout)
	}
	go func() {
		var res RekeyResult
		err := fsm.folderBranchOps().doMDWriteWithRetryUnlessCanceled(ctx,
			func(lState *lockState) (err error) {
				res, err = fsm.folderBranchOps().rekeyLocked(ctx, lState, false)
				return err
			})
		fsm.folderBranchOps().log.CDebugf(ctx, "Rekey finished with res=%v, error=%v", res, err)
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

		d := r.fsm.config().RekeyWithPromptWaitTime()

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

	config() Config
	folderBranchOps() *folderBranchOps
}

type rekeyFSM struct {
	reqs chan rekeyEvent
	fbo  *folderBranchOps

	current rekeyState
}

func (m rekeyFSM) loop() {
	for e := range m.reqs {
		if e.eventType == rekeyShutdownEvent {
			close(m.reqs)
		}

		m.current = m.current.reactToEvent(e)
	}
}

func (m rekeyFSM) config() Config {
	return m.fbo.config
}

func (m rekeyFSM) folderBranchOps() *folderBranchOps {
	return m.fbo
}

func NewRekeyFSM(fbo *folderBranchOps) rekeyFSM {
	fsm := rekeyFSM{
		reqs: make(chan rekeyEvent, rekeyQueueSize),
		fbo:  fbo,
	}
	fsm.current = newRekeyStateIdle(fsm)
	go fsm.loop()
	return fsm
}

func (m rekeyFSM) Event(event rekeyEvent) {
	select {
	case m.reqs <- event:
	default:
		go func() { m.reqs <- event }()
	}
}

func (m rekeyFSM) Shutdown() {
	m.Event(newRekeyShutdownEvent())
}
