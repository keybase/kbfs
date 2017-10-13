package libkbfs

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/keybase/client/go/logger"
)

type ctxTimeTracker struct {
	ctx       context.Context
	expiresAt time.Time

	done int32
}

type ctxTimeTrackerListNode struct {
	tracker *ctxTimeTracker
	next    *ctxTimeTrackerListNode
}

type ctxTimeTrackerList struct {
	front *ctxTimeTrackerListNode
	tail  *ctxTimeTrackerListNode
}

func (c *ctxTimeTrackerList) peekFront() (t *ctxTimeTracker) {
	if c.front == nil {
		return nil
	}
	return c.front.tracker
}

func (c *ctxTimeTrackerList) popFront() (t *ctxTimeTracker) {
	if c.front == nil {
		return nil
	}
	t = c.front.tracker
	c.front = c.front.next
	if c.front == nil {
		// last one!
		c.tail = nil
	}
	return t
}

func (c *ctxTimeTrackerList) append(t *ctxTimeTracker) {
	newNode := &ctxTimeTrackerListNode{
		tracker: t,
	}
	if c.tail != nil {
		c.tail.next = newNode
	}
	c.tail = newNode
	if c.front == nil {
		c.front = newNode
	}
}

type ImpatientDebugDumper struct {
	config Config
	logger logger.Logger
	dumpIn time.Duration

	ticker  *time.Ticker
	limiter *rate.Limiter

	lock          sync.Mutex
	chronological *ctxTimeTrackerList
}

const impatientDebugDumperCheckInterval = time.Second
const impatientDebugDumperDumpMinInterval = time.Second // 1 dump per sec max

func NewImpatientDebugDumper(config Config, dumpIn time.Duration) *ImpatientDebugDumper {
	d := &ImpatientDebugDumper{
		config: config,
		logger: config.MakeLogger("IGD"),
		dumpIn: dumpIn,
		ticker: time.NewTicker(impatientDebugDumperCheckInterval),
		limiter: rate.NewLimiter(
			rate.Every(impatientDebugDumperDumpMinInterval), 1),
		chronological: &ctxTimeTrackerList{},
	}
	go d.dumpLoop()
	return d
}

func (d *ImpatientDebugDumper) dump(tracker *ctxTimeTracker) {
	if !d.limiter.Allow() {
		// Use a limiter to avoid dumping too much into log accidently.
		return
	}
	buf := &bytes.Buffer{}
	base64er := base64.NewEncoder(base64.StdEncoding, buf)
	gzipper := gzip.NewWriter(base64er)
	for _, p := range pprof.Profiles() {
		fmt.Fprintf(gzipper,
			"\n======== START Profile: %s ========\n\n", p.Name())
		_ = p.WriteTo(gzipper, 2)
		fmt.Fprintf(gzipper,
			"\n======== END   Profile: %s ========\n\n", p.Name())
	}
	gzipper.Close()
	base64er.Close()
	d.logger.CDebugf(tracker.ctx,
		"operation expired. dump>gzip>base64: %q", buf.String())
}

func (d *ImpatientDebugDumper) dumpTick() {
	d.lock.Lock()
	defer d.lock.Unlock()
	for {
		t := d.chronological.peekFront()
		if t == nil {
			return
		}
		if atomic.LoadInt32(&t.done) == 1 {
			// This operation is done, so just move on.
			d.chronological.popFront()
			continue
		}
		if d.config.Clock().Now().After(t.expiresAt) {
			// This operation isn't done, and it has expired. So dump debug
			// information and move on.
			d.dump(t)
			d.chronological.popFront()
			continue
		}
		// This operation isn't done yet, but it also hasn't expired. So
		// just return and wait for next tick and check again.
		return
	}
}

func (d *ImpatientDebugDumper) dumpLoop() {
	for range d.ticker.C {
		d.dumpTick()
	}
}

func (d *ImpatientDebugDumper) Begin(ctx context.Context) (done func()) {
	tracker := &ctxTimeTracker{
		ctx:       ctx,
		expiresAt: d.config.Clock().Now().Add(d.dumpIn),
	}
	d.lock.Lock()
	defer d.lock.Unlock()
	d.chronological.append(tracker)
	return func() {
		atomic.StoreInt32(&tracker.done, 1)
	}
}
