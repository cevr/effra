package actorlib

import "effra.local/prototype/runtime/rt"

// call is an agent's message: a state transition that also answers its
// caller. apply(s, true) runs the transition; apply(s, false) answers a
// refusal without running it. A call is a resource: its Close, which the
// drain runs on a queued call and the handler runs on a call that panicked,
// answers the refusal, as Reply.Close does.
type call[S any] struct{ apply func(s S, run bool) S }

func (c call[S]) Close() {
	var zero S
	c.apply(zero, false)
}

// Agent is the owner handle of an agent: an actor whose messages are calls
// on its state (Elixir Agent). A protocol that is a set of methods on the
// state is served with no message union and no projection.
type Agent[S any] = Actor[call[S]]

// AgentAddress is its copyable half.
type AgentAddress[S any] = Address[call[S]]

// StartAgent runs an agent over initial with a mailbox of the given capacity.
func StartAgent[S any](s *rt.Scope, span string, initial S, capacity int) Agent[S] {
	return Start(s, span, initial, capacity, func(_ *rt.Task, st S, f call[S]) (Next[S], error) {
		defer f.Close() // answers nothing after apply answered; NoReply if it panicked
		return Continue(f.apply(st, true)), nil
	})
}

// Call runs f on the agent's state, in mailbox order, and returns f's value or
// its typed failure. On failure the state is unchanged. The reply is f's
// return value, so every admitted call is answered once by construction. f
// takes no task and no requirement: it is a pure transition, because a
// detached caller's call may run after the caller's providers have been
// released (research note, Q5). The exit is Stopped when the agent ends
// before f runs, NoReply when f panics, and rt.Interrupted when t is stopped
// while it waits.
func Call[S, T any](t *rt.Task, to AgentAddress[S], f func(s S) (S, T, error)) (T, error) {
	slot := make(chan outcome[T, error], 1)
	from := &to.c.control
	answer := func(o outcome[T, error]) {
		select {
		case slot <- o:
		default:
		}
	}
	err := to.Send(t, call[S]{apply: func(s S, run bool) S {
		if !run {
			answer(outcome[T, error]{kind: from.refusal()})
			return s
		}
		next, v, err := f(s)
		if err != nil {
			answer(outcome[T, error]{e: err, kind: failed})
			return s
		}
		answer(outcome[T, error]{v: v})
		return next
	}})
	if err != nil {
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
