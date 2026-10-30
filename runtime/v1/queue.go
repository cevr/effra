package v1

import (
	"fmt"
	"iter"
	"sync"
)

// Queue is queue.Queue[T], the body of the std package queue
// (docs/spec/runtime.md, Queues): a FIFO of T between tasks with a fixed
// capacity. Its methods take no task, so *Queue[T] is a value of the
// operations interface the lowering emits for queue.Queue[T]:
// Push(v T) fails Closed, Pop() T fails Closed, Values() Seq[T] and
// Close(). The contract:
//
//   - New(capacity) makes an open, empty queue. capacity is how many values
//     it holds that no Pop has taken yet; 0 makes every Push wait for the
//     Pop that takes its value, as Go's unbuffered channel does. A negative
//     capacity is a defect.
//   - Push(v) adds v at the back. While the queue holds capacity values it
//     waits (backpressure); waiting pushes enter in the order they began.
//     Push on a closed queue, and a Push still waiting when the queue
//     closes, fail with Closed, and v never enters the queue.
//   - Pop() takes the value at the front, waiting while the queue is open
//     and empty. On a closed queue it still takes each value that entered
//     before the close; on a closed, empty queue it fails with Closed. Its
//     Go value is then the zero value, which the failure makes unreachable
//     to Ergo code: no value is invented (types.md, Zero values 4).
//   - Values() is the queue as a Seq[T] (Go's iter.Seq): each step is a
//     Pop, so it drains the queue in FIFO order and ends normally when the
//     queue is closed and empty. A loop that stops early (break, return,
//     try) takes no further value: the rest stay in the queue for the next
//     Pop or Values.
//   - Close() ends admission. It is idempotent, never waits and never
//     fails. Values already in the queue stay for Pop and Values.
//
// Every value that a Push delivered (the Push returned no failure) is
// taken exactly once, by one Pop or one step of Values, in the order the
// values entered; a value whose Push failed is never taken. Several tasks
// may Push, Pop and iterate one queue at once; the order in which waiting
// Pops are served is not specified.
//
// Not yet: a task stopped while it waits in Push or Pop keeps waiting until
// the queue changes (a Pop, a Push or Close), because the queue does not
// know the waiter's task; Close wakes every waiter. Interruption of these
// waits (I1, rule 5) is #199.
type Queue[T any] struct {
	mu       sync.Mutex
	capacity int
	buf      []T          // the values that entered, oldest first; len <= capacity
	senders  []*sender[T] // the Pushes waiting for room, oldest first
	closed   bool
	// wake is closed, and cleared, at each change a waiter may wait for;
	// a waiter makes it. It is nil when nobody waits, so an uncontended
	// Push or Pop allocates nothing for it.
	wake chan struct{}
	// parked counts the Pushes and Pops parked in wait, which a change
	// wakes.
	parked int
}

// Closed is queue.Closed, the typed failure of Push on a closed queue and
// of Pop on a closed, empty one. A closed queue is a normal end of the
// protocol, so it is in the operations' error rows, and a catch handles
// it; an interruption stays a stop from outside.
type Closed struct{}

// Error is the failure's text, its Ergo name.
func (Closed) Error() string { return "queue.Closed" }

// sender is a Push waiting for room.
type sender[T any] struct {
	v        T
	accepted bool // v entered the queue: into buf, or straight to a Pop
	rejected bool // the queue closed first: v never entered
}

// New is queue.New[T](capacity).
func New[T any](capacity int) *Queue[T] {
	if capacity < 0 {
		panic(fmt.Sprintf("queue.New: negative capacity %d", capacity))
	}
	return &Queue[T]{capacity: capacity}
}

// Push is q.Push(v): nil when v entered the queue, Closed when it did not.
func (q *Queue[T]) Push(v T) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return Closed{}
	}
	if len(q.senders) == 0 && len(q.buf) < q.capacity {
		q.buf = append(q.buf, v)
		q.signal()
		q.mu.Unlock()
		return nil
	}
	s := &sender[T]{v: v}
	q.senders = append(q.senders, s)
	q.signal() // with capacity 0, a waiting Pop takes v straight away
	for !s.accepted && !s.rejected {
		q.wait()
	}
	rejected := s.rejected
	q.mu.Unlock()
	if rejected {
		return Closed{}
	}
	return nil
}

// Pop is q.Pop(): the front value, or Closed on a closed, empty queue.
func (q *Queue[T]) Pop() (T, error) {
	v, ok := q.take()
	if !ok {
		return v, Closed{}
	}
	return v, nil
}

// Values is q.Values().
func (q *Queue[T]) Values() iter.Seq[T] {
	return func(yield func(T) bool) {
		for {
			v, ok := q.take()
			if !ok || !yield(v) {
				return
			}
		}
	}
}

// Close is q.Close().
func (q *Queue[T]) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		for i, s := range q.senders {
			s.rejected = true
			q.senders[i] = nil
		}
		q.senders = nil
		q.signal()
	}
	q.mu.Unlock()
}

// take removes the front value, waiting while the queue is open and empty;
// ok is false on a closed, empty queue.
func (q *Queue[T]) take() (v T, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if len(q.buf) > 0 {
			v = q.buf[0]
			var zero T
			q.buf[0] = zero // the slot keeps no reference
			q.buf = q.buf[1:]
			if len(q.senders) > 0 {
				// The room goes to the oldest waiting Push.
				s := q.popSender()
				q.buf = append(q.buf, s.v)
				s.accepted = true
				q.signal()
			}
			return v, true
		}
		if len(q.senders) > 0 {
			// An empty buffer with a waiting Push: capacity 0, a rendezvous.
			s := q.popSender()
			s.accepted = true
			q.signal()
			return s.v, true
		}
		if q.closed {
			return v, false
		}
		q.wait()
	}
}

// popSender removes the oldest waiting Push. q.mu is held.
func (q *Queue[T]) popSender() *sender[T] {
	s := q.senders[0]
	q.senders[0] = nil
	q.senders = q.senders[1:]
	return s
}

// wait releases q.mu until the next change, then takes it again.
func (q *Queue[T]) wait() {
	if q.wake == nil {
		q.wake = make(chan struct{})
	}
	ch := q.wake
	q.parked++
	q.mu.Unlock()
	<-ch
	q.mu.Lock()
	q.parked--
}

// signal wakes every waiter, which looks again. q.mu is held.
func (q *Queue[T]) signal() {
	if q.wake != nil {
		close(q.wake)
		q.wake = nil
	}
}
