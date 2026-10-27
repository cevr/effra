package actorlib

import "effra.local/prototype/runtime/rt"

// call is an agent's message: a function on the state that also answers its
// caller. A call is a resource: its Close, which the drain runs on a queued
// call and the handler runs after every call, answers an unanswered caller
// with the refusal (Stopped while the agent stops, NoReply while it runs), as
// Reply.Close does. After the call answered, Close has no effect.
type call[S any] struct {
	run  func(t *rt.Task, s *S)
	slot refuser
}

func (c call[S]) Close() { c.slot.refuse() }

// refuser is the part of a slot a call needs to answer a refusal; a *slot
// converts to it without allocating.
type refuser interface{ refuse() }

func (s *slot[T, E]) refuse() { s.answer(outcome[T, E]{kind: s.from.refusal()}) }

// Agent is the owner handle of an agent: an actor whose messages are calls
// on its state (Elixir Agent). A protocol that is a set of methods on the
// state is served with no message union and no projection.
type Agent[S any] = Actor[call[S]]

// AgentAddress is its copyable half.
type AgentAddress[S any] = Address[call[S]]

// StartAgent runs an agent over initial with a mailbox of the given capacity.
// The agent owns its state; each call borrows it.
func StartAgent[S any](s *rt.Scope, span string, initial S, capacity int) Agent[S] {
	state := &initial
	return Start(s, span, state, capacity, func(t *rt.Task, st *S, c call[S]) (Next[*S], error) {
		defer c.Close() // answers nothing after run answered; NoReply if run panicked
		c.run(t, st)
		return Continue(st), nil
	})
}

// Call runs f on the agent's state, in mailbox order, and returns f's value or
// its typed failure: actor.Call(a, func(r *S) (T, error[E])) in the sketch. f
// borrows the state (a `*S` parameter, types.md Borrows), so it can change
// the state in place but cannot keep it: the borrow ends when f returns. A
// failure does not roll back changes f made before it failed.
//
// f runs on the agent's task and receives it: its waits are interruption
// points of the agent (I1), so Stop interrupts a waiting f. f has an empty
// requirement row: a detached caller's call may run after the caller's `with`
// has released its providers. The reply is f's return value, so every
// admitted call is answered once by construction; the value is copied out
// (snapshot) so it never aliases the state. The exit is Stopped when the
// agent stops before f runs or while f waits, NoReply when f panics (the
// panic is a defect of the agent, which Stop returns), and rt.Interrupted
// when t is stopped while it waits; the caller then detaches and a late
// result is released.
func Call[S, T any](t *rt.Task, to AgentAddress[S], f func(t *rt.Task, s *S) (T, error)) (T, error) {
	sl := newSlot[T, error](&to.c.control)
	err := to.Send(t, call[S]{
		run: func(at *rt.Task, s *S) {
			v, err := f(at, s)
			switch {
			case err == nil:
				sl.answer(outcome[T, error]{v: snapshot(v)})
			case err == rt.Interrupted:
				release(v) // the agent is stopping: Close answers Stopped
			default:
				sl.answer(outcome[T, error]{e: err, kind: failed})
			}
		},
		slot: sl,
	})
	if err != nil {
		sl.abandon()
		var zero T
		return zero, err
	}
	return sl.receive(t)
}
