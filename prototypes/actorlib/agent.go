package actorlib

import "effra.local/prototype/runtime/rt"

// call is an agent's message: a function on the state that also answers its
// caller. A call is a resource: its Close, which the drain runs on a queued
// call and the handler runs after every call, answers an unanswered caller
// with the refusal (Stopped once the agent's stop is requested, NoReply while
// it runs), as Reply.Close does. After the call answered, Close has no effect.
type call[S any] struct {
	run  func(t *rt.Task, s *S) error
	slot refuser
}

func (c call[S]) Close() { c.slot.refuse() }

// refuser is the part of a slot a call needs to answer a refusal; a *slot
// converts to it without allocating.
type refuser interface{ refuse() }

// Agent is the owner handle of an agent: an actor whose messages are calls
// on its state (Elixir Agent). A protocol that is a set of methods on the
// state is served with no message union and no projection.
type Agent[S any] = Actor[call[S]]

// AgentAddress is its copyable half.
type AgentAddress[S any] = Address[call[S]]

// StartAgent runs an agent over a copy of initial with a mailbox of the given
// capacity. The agent owns its state on every exit: each call only borrows
// it, so a call that panics or ends the agent leaves the state to the agent's
// release, which runs once after the queue is released. The state lives in
// storage (*S) that calls borrow; the release dispatches on the S value the
// storage holds, so an S that is a resource union or an interface with a
// Close is released as its variant.
func StartAgent[S any](s *rt.Scope, span string, initial S, capacity int) Agent[S] {
	state := new(S)
	*state = snapshot(initial)
	return start(s, span, state, capacity, func(t *rt.Task, st *S, c call[S]) (Next[*S], error) {
		defer c.Close() // answers nothing after run answered; NoReply or Stopped otherwise
		if err := c.run(t, st); err != nil {
			return Next[*S]{}, err
		}
		return Continue(st), nil
	}, held[S])
}

// held is the value an agent's storage holds: what its release releases.
func held[S any](p *S) any { return *p }

// Call runs f on the agent's state, in mailbox order, and returns f's value or
// its typed failure: actor.Call(a, func(r *S) (T, error[E])) in the sketch. f
// borrows the state (a `*S` parameter, types.md Borrows), so it can change
// the state in place but cannot keep it: the borrow ends when f returns. A
// failure does not roll back changes f made before it failed.
//
// f runs on the agent's task and receives it: its waits are interruption
// points of the agent (I1), so Stop interrupts a waiting f. f has an empty
// requirement row: a detached caller's call may run after the caller's `with`
// has released its providers. The value and the typed failure are copied out
// (snapshot) so they never alias the state.
//
// Call decides by the kind of f's exit, as a catch does (rt.Failure): a lone
// typed failure is the caller's failure, and the agent goes on. Any other
// exit, an interruption or a cause with a defect, is the agent's: it ends the
// agent with that exit, which Stop returns (without a stop's interruption);
// the caller gets NoReply, or Stopped when the agent's stop was requested,
// and queued callers get Stopped. A panic in f ends the agent the same way.
// The exit is rt.Interrupted when t is stopped while it waits; the caller
// then detaches and a late result is released.
//
// Ownership of f's value passes to Call only with a successful exit. On any
// other exit the value is the zero value of a failed function in Go's
// convention, and Call does not close it; a resource f built before it
// failed is f's to release (research note, Gap A13).
func Call[S, T any](t *rt.Task, to AgentAddress[S], f func(t *rt.Task, s *S) (T, error)) (T, error) {
	sl := newSlot[T, error](&to.c.control)
	if t.Stopped() {
		var zero T
		return zero, rt.Interrupted
	}
	err := to.admit(t, call[S]{ // a call holds only handles: nothing to copy
		run: func(at *rt.Task, s *S) error {
			v, err := f(at, s)
			if err == nil {
				sl.answer(at, outcome[T, error]{v: snapshot(v)})
				return nil
			}
			if fe, ok := rt.Failure(err); ok {
				sl.answer(at, outcome[T, error]{e: snapshot(fe), kind: failed})
				return nil
			}
			return err // the agent's exit; the handler's Close answers the caller
		},
		slot: sl,
	})
	if err != nil {
		sl.abandon(t, &err)
		var zero T
		return zero, err
	}
	return sl.receive(t)
}
