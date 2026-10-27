package actorlib

import "effra.local/prototype/runtime/rt"

// Decision is a step's result: the prelude Decision[S, O] of runtime.md
// ("Machines and actors", item 2), lowered as a sealed interface. The marker
// method names S and O, so a decision of one machine is not a decision of
// another.
type Decision[S, O any] interface{ decision(S, O) }

// Enter re-enters a state, even the same variant.
type Enter[S, O any] struct{ Next S }

// Stay keeps the entry and replaces the state value.
type Stay[S, O any] struct{ Next S }

// Ignore leaves the state unchanged.
type Ignore[S, O any] struct{}

// Reject leaves the state unchanged and tells the asker so.
type Reject[S, O any] struct{}

// Done ends the machine with an output.
type Done[S, O any] struct{ Output O }

func (Enter[S, O]) decision(S, O)  {}
func (Stay[S, O]) decision(S, O)   {}
func (Ignore[S, O]) decision(S, O) {}
func (Reject[S, O]) decision(S, O) {}
func (Done[S, O]) decision(S, O)   {}

// Machine is the value of a `machine[S, E]` declaration: its initial state
// and its step. The step's whole signature is in the type: its state S, event
// E and output O as type arguments, its failure row F erased to Go's error,
// and its requirement row R captured where the value is built (the sketch's
// Machine[S, E, O, F, R], research note O1). A machine value is a plain
// value.
type Machine[S, E, O any] struct {
	Initial S
	Step    func(t *rt.Task, s S, e E) (Decision[S, O], error)
}

// machineMsg is the machine actor's private message union.
type machineMsg[S, E, O any] interface {
	machineMsg(S, E, O)
	Close()
}

// event carries one event and, for Ask, its reply. A Send leaves the reply
// zero, which answers nothing.
type event[S, E, O any] struct {
	ev    E
	reply Reply[Decision[S, O], error]
}

// stateQuery asks for a snapshot of the current state.
type stateQuery[S, E, O any] struct{ reply Reply[S, Never] }

func (event[S, E, O]) machineMsg(S, E, O)      {}
func (stateQuery[S, E, O]) machineMsg(S, E, O) {}

// Close is the generated release of a struct with resource fields (R3): it
// releases every field that may be a resource, the event by its variant and
// the reply, and runs the reply's release even when the event's panics. It
// runs inside a drop (the drain's or a refused Send's).
func (m event[S, E, O]) Close() {
	defer m.reply.Close()
	closeField(m.ev)
}
func (m stateQuery[S, E, O]) Close() { m.reply.Close() }

// MachineActor is the owner handle of a machine-backed actor.
type MachineActor[S, E, O any] struct {
	a Actor[machineMsg[S, E, O]]
}

// MachineAddress is its copyable half.
type MachineAddress[S, E, O any] struct {
	a Address[machineMsg[S, E, O]]
}

// StartMachine runs m as an actor. It is ordinary library code over Start:
// the machine's events travel in a private envelope with an optional reply,
// and the handler applies the step's decision. A step failure goes to the
// asker and leaves the state unchanged; a failing Send has no asker, so the
// failure ends the actor and Stop returns it. Done answers the asker, then
// ends the actor. The state snapshot and every decision sent to an asker are
// copies (snapshot): the actor keeps the only reference to its state.
func StartMachine[S, E, O any](s *rt.Scope, span string, m Machine[S, E, O], capacity int) MachineActor[S, E, O] {
	handle := func(t *rt.Task, st S, msg machineMsg[S, E, O]) (Next[S], error) {
		switch msg := msg.(type) {
		case event[S, E, O]:
			// The step consumes the event, so the handler releases only the
			// reply, on every path where it is still owned; after Send or
			// Fail its release answers nothing.
			defer msg.reply.Close()
			d, err := m.Step(t, st, msg.ev)
			if err != nil {
				if f, ok := rt.Failure(err); ok && msg.reply.s != nil {
					msg.reply.Fail(t, f)
					return Continue(st), nil
				}
				return Next[S]{}, err
			}
			end := false
			switch d := d.(type) {
			case Enter[S, O]:
				st = d.Next
			case Stay[S, O]:
				st = d.Next
			case Done[S, O]:
				end = true
			}
			if msg.reply.s != nil {
				msg.reply.Send(t, d) // Reply.Send copies (snapshot)
			}
			if end {
				return End(st), nil
			}
			return Continue(st), nil
		case stateQuery[S, E, O]:
			msg.reply.Send(t, st) // a snapshot: Reply.Send copies
			return Continue(st), nil
		}
		panic("unreachable: machineMsg is sealed")
	}
	return MachineActor[S, E, O]{a: Start(s, span, m.Initial, capacity, handle)}
}

// Address returns the copyable half.
func (b MachineActor[S, E, O]) Address() MachineAddress[S, E, O] {
	return MachineAddress[S, E, O]{b.a.Address()}
}

// Send admits e without waiting for its decision.
func (b MachineActor[S, E, O]) Send(t *rt.Task, e E) error { return b.Address().Send(t, e) }

// Ask admits e and returns its decision, or the step's typed failure.
func (b MachineActor[S, E, O]) Ask(t *rt.Task, e E) (Decision[S, O], error) {
	return b.Address().Ask(t, e)
}

// State returns a snapshot of the state after every message admitted before
// it.
func (b MachineActor[S, E, O]) State(t *rt.Task) (S, error) { return b.Address().State(t) }

// Stop is Actor.Stop.
func (b MachineActor[S, E, O]) Stop() error { return b.a.Stop() }

// Send admits e without waiting for its decision.
func (b MachineAddress[S, E, O]) Send(t *rt.Task, e E) error {
	return b.a.Send(t, event[S, E, O]{ev: e})
}

// Ask admits e and returns its decision, or the step's typed failure.
func (b MachineAddress[S, E, O]) Ask(t *rt.Task, e E) (Decision[S, O], error) {
	return Ask(t, b.a, func(r Reply[Decision[S, O], error]) machineMsg[S, E, O] {
		return event[S, E, O]{ev: e, reply: r}
	})
}

// State returns a snapshot of the state.
func (b MachineAddress[S, E, O]) State(t *rt.Task) (S, error) {
	return Ask(t, b.a, func(r Reply[S, Never]) machineMsg[S, E, O] {
		return stateQuery[S, E, O]{reply: r}
	})
}
