package libkbfs

import "sync"

// OngoingWorkLimiter limits maximum number of routines that can work on a same
// type of thing at the same time. For example, it can be used for limiting
// number of ongoing rekeys.
type OngoingWorkLimiter struct {
	cond     sync.Cond
	capacity int64
}

// NewOngoingWorkLimiter creates a new *OngoingWorkLimiter with capacity of
// maxNumOngoingWorks.
func NewOngoingWorkLimiter(maxNumOngoingWorks int) *OngoingWorkLimiter {
	return &OngoingWorkLimiter{
		capacity: int64(maxNumOngoingWorks),
		cond:     sync.Cond{L: new(sync.Mutex)},
	}
}

// WaitToStart blocks until the limiter would allow one more routine to start
// working on the thing.
func (owl *OngoingWorkLimiter) WaitToStart() {
	owl.cond.L.Lock()
	for owl.capacity <= 0 {
		owl.cond.Wait()
	}
	owl.capacity--
	owl.cond.L.Unlock()
}

// Done tells the limiter that the caller is done working on the thing, and
// somebody else is free to start work.
func (owl *OngoingWorkLimiter) Done() {
	owl.cond.L.Lock()
	defer owl.cond.L.Unlock()
	owl.capacity++
	owl.cond.Signal()
}
