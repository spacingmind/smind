package runs

import "sync"

// subQueue is an unbounded, single-consumer FIFO queue of Events, backing
// one Subscribe call's live feed. It exists so record's broadcast to many
// subscribers never blocks on a slow one: push only ever appends to a
// slice and returns, regardless of whether pop is being called.
//
// closeQueue marks the queue done; pop continues draining whatever's left
// before reporting closed, so events queued before a subscriber detaches
// are never silently lost.
type subQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []Event
	closed bool
}

func newSubQueue() *subQueue {
	q := &subQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *subQueue) push(e Event) {
	q.mu.Lock()
	q.items = append(q.items, e)
	q.cond.Signal()
	q.mu.Unlock()
}

func (q *subQueue) closeQueue() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

// pop blocks until an item is available or the queue is closed and
// drained, in which case ok is false.
func (q *subQueue) pop() (e Event, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return Event{}, false
	}
	e, q.items = q.items[0], q.items[1:]
	return e, true
}

// relay drains q into out, in order, until q closes (and is fully drained)
// or done fires -- the latter only on Subscribe's own unsubscribe, so a
// subscriber that stops reading out without unsubscribing doesn't leak
// this goroutine blocked on out<-e forever.
func relay(q *subQueue, out chan<- Event, done <-chan struct{}) {
	defer close(out)
	for {
		e, ok := q.pop()
		if !ok {
			return
		}
		select {
		case out <- e:
		case <-done:
			return
		}
	}
}
