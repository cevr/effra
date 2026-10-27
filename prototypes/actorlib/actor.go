package actorlib

import (
	"reflect"
	"sync"
	"sync/atomic"

	"effra.local/prototype/runtime/rt"
)

// Stopped is the typed failure of a request the actor refused or discarded
// because it was stopping or had ended: a Send after the stop was requested, a
// queued request discarded by the stop, and a reply released while the actor
// stopped.
type Stopped struct{}

func (Stopped) Error() string { return "actor stopped" }

// NoReply is the typed failure of a request the actor dequeued and then
// released unanswered while it was running: its handler dropped the reply,
// or failed with a defect while it held the reply.
type NoReply struct{}

func (NoReply) Error() string { return "actor released the reply unanswered" }

// Never is the error type of a reply with an empty error row. Nothing in the
// package constructs it.
type Never struct{ never struct{} }

func (Never) Error() string { return "never" }

// kind says which answer an outcome holds.
type kind uint8

const (
	answered kind = iota
	failed
	refusedStopped
	refusedNoReply
)

// outcome is one answer in a reply slot: a value, a typed failure or a
// refusal. It is the lowering of the Exit[T, E] value the sketch names
// (research note, Gap A7). The failure is held as its own type E, not as a Go
// error, so an outcome of pointer-free T and E is pointer-free and its
// channel is one allocation.
type outcome[T any, E error] struct {
	v    T
	e    E
	kind kind
}

func (o outcome[T, E]) result() (T, error) {
	var zero T
	switch o.kind {
	case answered:
		return o.v, nil
	case failed:
		return zero, o.e
	case refusedStopped:
		return zero, Stopped{}
	default:
		return zero, NoReply{}
	}
}

// release releases what the outcome owns: the value of an answer or the
// failure of a failed answer. A refusal owns nothing.
func (o outcome[T, E]) release() {
	switch o.kind {
	case answered:
		release(o.v)
	case failed:
		release(o.e)
	}
}

const (
	slotOpen uint32 = iota
	slotAnswered
	slotAbandoned
)

// slot is a one-shot handoff from the answering side to one receiver. Its
// state makes the handoff cancellation-safe: the first answer wins and is
// delivered, unless the receiver has abandoned the slot; every other answer,
// and an answer nobody can receive, is released (research note, Gap A3: a
// value nobody can receive is released). Tokio's oneshot has the same two
// halves: a dropped sender completes the receiver, a dropped receiver
// releases the value.
type slot[T any, E error] struct {
	state atomic.Uint32
	ch    chan outcome[T, E] // capacity 1: an answer never blocks
	from  *control
}

func newSlot[T any, E error](from *control) *slot[T, E] {
	return &slot[T, E]{ch: make(chan outcome[T, E], 1), from: from}
}

// answer delivers o, or releases it when the slot is answered or abandoned.
func (s *slot[T, E]) answer(o outcome[T, E]) {
	if s.state.CompareAndSwap(slotOpen, slotAnswered) {
		s.ch <- o
		return
	}
	o.release()
}

// abandon is the release of the receiving end: an answer that arrived first
// is taken and released, and a later one will be released by answer.
func (s *slot[T, E]) abandon() {
	if s.state.CompareAndSwap(slotOpen, slotAbandoned) {
		return
	}
	(<-s.ch).release() // answered: the answer is buffered or about to be
}

// receive waits for the answer. It is an interruption point (I1): when t is
// stopped, the receiver detaches and abandons the slot, and stop wins over an
// answer that raced it. Ownership of a taken answer passes only to a live
// receiver: receive checks the stop before it waits and again after it takes
// an answer, because a select with both arms ready may take the answer for a
// task that is already stopped; such an answer is released here.
//
// The answer can still be lost after receive returns it: when the stop
// arrives between this check and the receiving task's boundary, rt.Run's
// stop-wins discards the task's result without releasing it. That window is
// a runtime rule, not an actor one (research note, Gap A13).
func (s *slot[T, E]) receive(t *rt.Task) (T, error) {
	var zero T
	if t.Stopped() {
		s.abandon()
		return zero, rt.Interrupted
	}
	select {
	case o := <-s.ch:
		if t.Stopped() {
			o.release()
			return zero, rt.Interrupted
		}
		return o.result()
	case <-t.Done():
		s.abandon()
		return zero, rt.Interrupted
	}
}

// Reply is a one-shot reply address carried in a message: actor.Reply[T, E]
// in the sketch. It is a resource (resources.md R1): Send and Fail consume it,
// and its Close, which an owner scope runs when the reply is released
// unanswered, answers NoReply, or Stopped when the actor is stopping. The
// first answer wins; the checker makes a second answer a use after consume
// (R20), and the lowering releases it. A Reply is one pointer, so a message
// variant holding only a Reply is pointer-shaped and boxes for free.
type Reply[T any, E error] struct{ s *slot[T, E] }

// Send answers the request with v.
func (r Reply[T, E]) Send(v T) {
	if r.s == nil {
		release(v)
		return
	}
	r.s.answer(outcome[T, E]{v: snapshot(v)}) // a copy: the state keeps its own value
}

// Fail answers the request with the typed failure e. e leaves the actor's
// task, so it is a copy, as a value is.
func (r Reply[T, E]) Fail(e E) {
	if r.s == nil {
		release(e)
		return
	}
	r.s.answer(outcome[T, E]{e: snapshot(e), kind: failed})
}

// Close answers an unanswered request with NoReply, or Stopped when the actor
// is stopping or has ended. After Send or Fail it has no effect.
func (r Reply[T, E]) Close() {
	if r.s == nil {
		return
	}
	r.s.answer(outcome[T, E]{kind: r.s.from.refusal()})
}

// control is the part of an actor that replies and senders share.
type control struct {
	// mu fences admission. A sender holds it shared while it admits a
	// message; closeAdmission takes it exclusively once. After that no
	// message is admitted, so every admitted message is handled or released
	// by the drain.
	mu     sync.RWMutex
	closed bool // guarded by mu

	closing  atomic.Bool   // admission closed or closing
	ended    atomic.Bool   // the loop has ended or is ending
	stopOnce sync.Once     // closes stopping
	stopping chan struct{} // closed with admission: wakes blocked senders
	task     *rt.Task      // the actor's task, set before Start returns
}

// closeAdmission refuses every later Send and wakes every waiting sender. It
// runs at stop initiation (Actor.Stop) and again, idempotently, when the loop
// ends for any reason.
func (c *control) closeAdmission() {
	c.closing.Store(true)
	c.stopOnce.Do(func() { close(c.stopping) })
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}

// refused reports whether admission is refused: closed, or a stop requested
// on the actor's task by any path (Stop, the owner scope's end, a stop of an
// ancestor task).
func (c *control) refused() bool {
	return c.closed || c.task.StopRequested()
}

func (c *control) refusal() kind {
	if c.ended.Load() || c.closing.Load() || c.task.StopRequested() {
		return refusedStopped
	}
	return refusedNoReply
}

type cell[M any] struct {
	control
	mailbox chan M
}

// Address is the copyable half of an actor: Send and Ask. It is not a
// resource and does not keep the actor running. After the stop is requested,
// Send and Ask fail with Stopped.
type Address[M any] struct{ c *cell[M] }

// Send admits m, waiting while the mailbox is full. It is an interruption
// point (I1). It fails with Stopped when the actor's stop has been requested,
// and with rt.Interrupted when t is stopped first. Send consumes m: on
// failure it releases m, so a reply inside it is answered.
//
// A sender that started waiting on a full mailbox before a stop that came
// from outside Actor.Stop (an ancestor's stop) may complete its send while the
// loop winds down; the drain then releases that message as a queued request.
// Actor.Stop closes admission before it interrupts, so it has no such window.
func (a Address[M]) Send(t *rt.Task, m M) error {
	if t.Stopped() {
		release(m)
		return rt.Interrupted
	}
	return a.admit(t, snapshot(m)) // a copy: the sender keeps its own value; a resource moves
}

// admit is Send without the copy, for a message the library built from
// handles only (an agent call), where the compiler knows statically that
// there is nothing to copy.
func (a Address[M]) admit(t *rt.Task, m M) error {
	c := a.c
	c.mu.RLock()
	if c.refused() {
		c.mu.RUnlock()
		release(m)
		return Stopped{}
	}
	select {
	case c.mailbox <- m:
		c.mu.RUnlock()
		return nil
	default:
	}
	var err error
	select {
	case c.mailbox <- m:
	case <-c.stopping:
		err = Stopped{}
	case <-t.Done():
		err = rt.Interrupted
	}
	c.mu.RUnlock()
	if err != nil {
		release(m)
	}
	return err
}

// Ask sends the message that request builds around a fresh reply address and
// waits for the answer. The reply type T and failure type E come from
// request's parameter alone, so a message that carries a reply of another
// type is a compile error. The exit is the answer's value, its typed failure
// E, Stopped, NoReply, or rt.Interrupted when t is stopped while it waits;
// the caller then detaches, and the actor's late answer is released.
func Ask[M, T any, E error](t *rt.Task, to Address[M], request func(Reply[T, E]) M) (T, error) {
	s := newSlot[T, E](&to.c.control)
	if err := to.Send(t, request(Reply[T, E]{s})); err != nil {
		s.abandon()
		var zero T
		return zero, err
	}
	return s.receive(t)
}

// Next is a handler's decision: continue with a state, or end the actor
// normally (Gleam OTP actor.continue and actor.stop).
type Next[S any] struct {
	state S
	end   bool
}

// Continue keeps the actor running with state s.
func Continue[S any](s S) Next[S] { return Next[S]{state: s} }

// End ends the actor normally after this message; s is released.
func End[S any](s S) Next[S] { return Next[S]{state: s, end: true} }

// Handler handles one message. It owns m: it answers a reply in m or releases
// it. A typed failure ends the actor with that failure; per-request failures
// go through a Reply instead.
type Handler[S, M any] func(t *rt.Task, s S, m M) (Next[S], error)

// Actor is the owner handle: actor.Actor[M, E] in the sketch. It is a
// resource: Stop consumes it, and the owner scope stops it at scope end if
// Stop did not run (resources.md R5). Its methods belong to the owner task's
// goroutine; hand the Address to other tasks.
type Actor[M any] struct {
	addr  Address[M]
	h     *rt.Handle[struct{}]
	owner *rt.Task
}

// Start runs handle over a mailbox of the given capacity, as a child task
// owned by s. The handler's requirements are whatever it captured, so they
// are charged where Start is called. initial crosses into the actor's task,
// so it is a copy (snapshot). capacity must be at least 1.
func Start[S, M any](s *rt.Scope, span string, initial S, capacity int, handle Handler[S, M]) Actor[M] {
	return start(s, span, initial, capacity, handle, false)
}

// start is Start. When lends is true the handler only borrows the state, as
// an agent's handler does: the loop keeps owning it while the handler runs,
// so a handler that unwinds or fails leaves the state to the loop's release.
func start[S, M any](s *rt.Scope, span string, initial S, capacity int, handle Handler[S, M], lends bool) Actor[M] {
	if capacity < 1 {
		panic("actorlib: capacity must be at least 1")
	}
	l := &loop[S, M]{s: snapshot(initial), owned: true, lends: lends}
	l.c.mailbox = make(chan M, capacity)
	l.c.stopping = make(chan struct{})
	h := rt.Go(s, span, func(t *rt.Task) (struct{}, error) {
		return struct{}{}, l.run(t, handle)
	})
	l.c.task = h.Task()
	return Actor[M]{addr: Address[M]{&l.c}, h: h, owner: s.Task()}
}

// Address returns the copyable half of the actor.
func (a Actor[M]) Address() Address[M] { return a.addr }

// Send is a.Address().Send.
func (a Actor[M]) Send(t *rt.Task, m M) error { return a.addr.Send(t, m) }

// Stop closes admission, interrupts the actor and waits until it has ended:
// its in-flight handler has stopped at its next interruption point, queued
// requests are answered Stopped and its state is released. It returns the
// actor's exit without the interruption Stop caused: nil, the typed failure
// that ended the actor before the stop, and every defect, in order. Stop
// consumes the handle.
func (a Actor[M]) Stop() error {
	a.addr.c.closeAdmission()
	a.h.Task().Interrupt()
	_, err := a.h.Wait()
	if a.owner.Stopped() {
		return err
	}
	return dropInterruption(err)
}

// loop is the actor's task body and what it owns. The cell is embedded, so
// one allocation holds both.
type loop[S, M any] struct {
	c       cell[M]
	s       S
	owned   bool // the loop owns s, except while a consuming handler holds it
	lends   bool // the handler borrows s and never consumes it
	held    M    // a message dequeued after the stop was requested
	holding bool
}

// run is the actor's task body. Every exit goes through exit.
func (l *loop[S, M]) run(t *rt.Task, handle Handler[S, M]) (err error) {
	defer l.exit(t, &err)
	return l.serve(t, handle)
}

func (l *loop[S, M]) serve(t *rt.Task, handle Handler[S, M]) error {
	done := t.Done()
	for {
		if t.Stopped() {
			return rt.Interrupted
		}
		select {
		case m := <-l.c.mailbox:
			if t.StopRequested() {
				// Dequeued as the stop arrived: it is a queued request.
				l.held, l.holding = m, true
				return rt.Interrupted
			}
			l.owned = l.lends
			next, err := handle(t, l.s, m)
			if err != nil {
				if f, ok := rt.Failure(err); ok {
					return snapshot(f) // the failure crosses to the owner's task
				}
				return err
			}
			l.s, l.owned = next.state, true
			if next.end {
				return nil
			}
		case <-done:
			return rt.Interrupted
		}
	}
}

// exit runs on every exit of the loop. It closes admission, then releases
// the held message, every queued message in admission order (a queued
// request is answered Stopped) and last the state.
//
// The library classifies no panic itself: rt does, on its canonical path. A
// body panic with nothing to release is raised again for the task boundary
// (rt.Run) to record; otherwise drain records it in an owner scope
// (rt.Scope.End). So the rt.Interrupted sentinel and an exit carried out of a
// nested scope keep their kind and order, and only other panic values become
// defects. A clean exit with nothing to release returns the body's exit as it
// is and builds no scope.
func (l *loop[S, M]) exit(t *rt.Task, err *error) {
	p := recover()
	l.c.ended.Store(true)
	l.c.closeAdmission() // no message is admitted after this, so the queue is final
	if !l.holding && len(l.c.mailbox) == 0 && !(l.owned && isResource(l.s)) {
		if p != nil {
			panic(p) // the task boundary records it (rt.Run)
		}
		return
	}
	l.drain(t, p, err)
}

// drain releases what the loop owns in an owner scope, masked (I2), each as
// its own release: every release runs even after an earlier one panicked
// (R9), and rt records each release cause after the body's exit and the body
// panic p, with its kind, its InRelease mark and its order.
//
//go:noinline
func (l *loop[S, M]) drain(t *rt.Task, p any, err *error) {
	rs := rt.NewScope(t)
	defer rs.End(err)
	if l.owned && isResource(l.s) { // registered first, so released last
		s := l.s
		rs.Defer(func() { release(s) })
	}
	var queued []M
	if l.holding {
		queued = append(queued, l.held)
	}
	for {
		select {
		case m := <-l.c.mailbox:
			queued = append(queued, m)
			continue
		default:
		}
		break
	}
	for i := len(queued) - 1; i >= 0; i-- {
		m := queued[i]
		rs.Defer(func() { release(m) })
	}
	if p != nil {
		panic(p) // rs.End recovers it first, then runs the releases
	}
}

var closer = reflect.TypeFor[interface{ Close() }]()

// isResource reports whether v has a release. It does not box v unless V is
// an interface type, whose dynamic value decides.
func isResource[V any](v V) bool {
	t := reflect.TypeFor[V]()
	if t.Kind() == reflect.Interface {
		_, ok := any(v).(interface{ Close() })
		return ok
	}
	return t.Implements(closer)
}

// release is the release of a value whose type is a type parameter: the
// lowering of R5 for a value that may or may not be a resource (Gap A8).
func release[V any](v V) {
	if r, ok := any(v).(interface{ Close() }); ok {
		r.Close()
	}
}

// dropInterruption removes the interruption a stop caused from an exit and
// keeps every other reason.
func dropInterruption(err error) error {
	if err == nil || err == rt.Interrupted {
		return nil
	}
	c, ok := err.(*rt.Cause)
	if !ok {
		return err
	}
	var kept rt.Cause
	for _, r := range c.Reasons {
		if r.Kind != rt.KindInterruption {
			kept.Reasons = append(kept.Reasons, r)
		}
	}
	return kept.Err()
}
