package actorlib

import (
	"sync"
	"sync/atomic"

	"effra.local/prototype/runtime/rt"
)

// Stopped is the typed failure of a request the actor refused or discarded
// because it was stopping or had ended: a Send to a stopped actor, a queued
// request discarded by Stop, and a reply released while the actor stopped.
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
// error, so an outcome of pointer-free T and E is pointer-free and its slot is
// one allocation.
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

// Reply is a one-shot reply address carried in a message: actor.Reply[T, E]
// in the sketch. It is a resource (resources.md R1): Send and Fail consume it,
// and its Close, which an owner scope runs when the reply is released
// unanswered, answers NoReply, or Stopped when the actor is stopping. The
// first answer wins; the checker makes a second answer a use after consume
// (R20), and the lowering drops it without blocking.
type Reply[T any, E error] struct {
	slot chan outcome[T, E]
	from *control
}

// Send answers the request with v.
func (r Reply[T, E]) Send(v T) { r.answer(outcome[T, E]{v: v}) }

// Fail answers the request with the typed failure e.
func (r Reply[T, E]) Fail(e E) { r.answer(outcome[T, E]{e: e, kind: failed}) }

// Close answers an unanswered request with NoReply, or Stopped when the actor
// is stopping or has ended. After Send or Fail it has no effect on the caller.
func (r Reply[T, E]) Close() {
	if r.slot == nil {
		return
	}
	r.answer(outcome[T, E]{kind: r.from.refusal()})
}

// answer never blocks: the slot has capacity one, so the first answer is
// buffered for the caller, or the caller has already taken an answer and a
// late one is garbage.
func (r Reply[T, E]) answer(o outcome[T, E]) {
	if r.slot == nil {
		return
	}
	select {
	case r.slot <- o:
	default:
	}
}

// control is the part of an actor that a reply needs to choose its refusal,
// and that senders need to stop waiting.
type control struct {
	// mu fences admission: a sender holds it shared while it admits a
	// message, and shutdown takes it exclusively once, so that no message is
	// admitted after the drain and every admitted message is released.
	mu     sync.RWMutex
	closed bool // guarded by mu

	ended    atomic.Bool
	stopping chan struct{} // closed when the actor ends: wakes blocked senders
	task     *rt.Task      // the actor's task, set before Start returns
}

func (c *control) refusal() kind {
	if c.ended.Load() || c.task.StopRequested() {
		return refusedStopped
	}
	return refusedNoReply
}

type cell[M any] struct {
	control
	mailbox chan M
}

// Address is the copyable half of an actor: Send and Ask. It is not a
// resource and does not keep the actor running. After the actor ends, Send
// and Ask fail with Stopped.
type Address[M any] struct{ c *cell[M] }

// Send admits m, waiting while the mailbox is full. It is an interruption
// point (I1). It fails with Stopped when the actor is stopping or has ended,
// and with rt.Interrupted when t is stopped first. Send consumes m: on
// failure it releases m, so a reply inside it is answered.
func (a Address[M]) Send(t *rt.Task, m M) error {
	if t.Stopped() {
		release(m)
		return rt.Interrupted
	}
	c := a.c
	c.mu.RLock()
	if c.closed {
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
// the caller then detaches and the actor's late answer is dropped.
func Ask[M, T any, E error](t *rt.Task, to Address[M], request func(Reply[T, E]) M) (T, error) {
	slot := make(chan outcome[T, E], 1)
	if err := to.Send(t, request(Reply[T, E]{slot: slot, from: &to.c.control})); err != nil {
		var zero T
		return zero, err
	}
	select {
	case o := <-slot:
		return o.result()
	case <-t.Done():
		var zero T
		return zero, rt.Interrupted
	}
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
// are charged where Start is called. capacity must be at least 1.
func Start[S, M any](s *rt.Scope, span string, initial S, capacity int, handle Handler[S, M]) Actor[M] {
	if capacity < 1 {
		panic("actorlib: capacity must be at least 1")
	}
	c := &cell[M]{mailbox: make(chan M, capacity)}
	c.stopping = make(chan struct{})
	h := rt.Go(s, span, func(t *rt.Task) (struct{}, error) {
		return struct{}{}, serve(t, c, initial, handle)
	})
	c.task = h.Task()
	return Actor[M]{addr: Address[M]{c}, h: h, owner: s.Task()}
}

// Address returns the copyable half of the actor.
func (a Actor[M]) Address() Address[M] { return a.addr }

// Send is a.Address().Send.
func (a Actor[M]) Send(t *rt.Task, m M) error { return a.addr.Send(t, m) }

// Stop interrupts the actor and waits until it has ended: its in-flight
// handler has stopped at its next interruption point, queued requests are
// answered Stopped and its state is released. It returns the actor's exit
// without the interruption Stop caused: nil, the typed failure that ended the
// actor before the stop, and any defect. Stop consumes the handle.
func (a Actor[M]) Stop() error {
	a.h.Task().Interrupt()
	_, err := a.h.Wait()
	if a.owner.Stopped() {
		return err
	}
	return dropInterruption(err)
}

func serve[S, M any](t *rt.Task, c *cell[M], s S, handle Handler[S, M]) (err error) {
	owned := true // the loop owns s except while a handler holds it
	defer func() {
		c.shutdown()
		if owned {
			release(s)
		}
	}()
	done := t.Done()
	for {
		if t.Stopped() {
			return rt.Interrupted
		}
		select {
		case m := <-c.mailbox:
			owned = false
			next, err := handle(t, s, m)
			if err != nil {
				return err
			}
			s, owned = next.state, true
			if next.end {
				return nil
			}
		case <-done:
			return rt.Interrupted
		}
	}
}

// shutdown refuses admission and releases every queued message, so a queued
// request is answered Stopped. It runs on every exit of the loop.
func (c *cell[M]) shutdown() {
	c.ended.Store(true)
	close(c.stopping)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	var first any
	for {
		select {
		case m := <-c.mailbox:
			if p := releaseRecovering(m); p != nil && first == nil {
				first = p
			}
		default:
			if first != nil {
				panic(first) // the task boundary records it as a defect
			}
			return
		}
	}
}

// release is the release of a value whose type is a type parameter: the
// lowering of R5 for a value that may or may not be a resource (Gap A8).
func release[V any](v V) {
	if r, ok := any(v).(interface{ Close() }); ok {
		r.Close()
	}
}

func releaseRecovering[V any](v V) (p any) {
	defer func() { p = recover() }()
	release(v)
	return nil
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
