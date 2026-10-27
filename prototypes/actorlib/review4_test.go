package actorlib

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"effra.local/prototype/runtime/rt"
)

// Round-4 review probes (round-4-findings.md), kept as tests. Two of them
// pass; three pin a limitation of the prototype's measured domain at today's
// count, so that the general fix fails them (then each must assert 1).

// M1a: a Send asks for no decision. The handler owns the successful Done
// output it discards and releases it through drop.
func TestR4UnaskedMachineDoneOutputIsReleased(t *testing.T) {
	var closed atomic.Int32
	within(t, func(task *rt.Task, s *rt.Scope) error {
		handled := make(chan struct{})
		m := Machine[int, int, lease]{Step: func(_ *rt.Task, _ int, _ int) (Decision[int, lease], error) {
			defer close(handled)
			return Done[int, lease]{Output: lease{&closed}}, nil
		}}
		b := StartMachine(s, "unasked-output", m, 1)
		if err := b.Send(task, 0); err != nil {
			return err
		}
		<-handled
		return b.Stop()
	})
	if got := closed.Load(); got != 1 {
		t.Errorf("unasked Done output closed %d times; want 1", got)
	}
}

// r4AckEvent's release waits until the queued asker has observed its
// refusal. Released before the reply, it would wait forever, because the
// refusal comes from the reply's release (finding M2).
type r4AckEvent struct {
	ack    <-chan struct{}
	closed *atomic.Int32
	order  *orderLog
}

func (e r4AckEvent) Close() {
	<-e.ack
	if e.order != nil {
		e.order.add("event released")
	}
	e.closed.Add(1)
}

type orderLog struct {
	mu   sync.Mutex
	seen []string
}

func (o *orderLog) add(s string) {
	o.mu.Lock()
	o.seen = append(o.seen, s)
	o.mu.Unlock()
}

// M2: the drain releases a queued Ask's envelope reply first, then its event.
// No escape timer: with the old order the drain and the asker wait on each
// other.
func TestR4QueuedMachineEventReleasesFieldsInReverseOrder(t *testing.T) {
	var closed atomic.Int32
	var order orderLog
	within(t, func(task *rt.Task, s *rt.Scope) error {
		holding := make(chan struct{})
		ready := make(chan struct{})
		close(ready)
		m := Machine[int, r4AckEvent, int]{Step: func(at *rt.Task, n int, e r4AckEvent) (Decision[int, int], error) {
			defer e.Close()
			close(holding)
			<-at.Done()
			return Ignore[int, int]{}, nil
		}}
		b := StartMachine(s, "reverse-fields", m, 1)
		if err := b.Send(task, r4AckEvent{ready, new(atomic.Int32), nil}); err != nil {
			return err
		}
		<-holding
		ack := make(chan struct{})
		asker := rt.Go(s, "queued-event-asker", func(ct *rt.Task) (struct{}, error) {
			_, err := b.Ask(ct, r4AckEvent{ack, &closed, &order})
			order.add("refusal observed")
			close(ack)
			return struct{}{}, err
		})
		waitQueued(t, b.a.addr, 1)
		if err := b.Stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
		if _, err := asker.Wait(); !failureIs[Stopped](err) {
			t.Errorf("queued caller: %v", err)
		}
		return nil
	})
	if got := closed.Load(); got != 1 {
		t.Errorf("queued event released %d times; want 1", got)
	}
	want := []string{"refusal observed", "event released"}
	if len(order.seen) != 2 || order.seen[0] != want[0] || order.seen[1] != want[1] {
		t.Errorf("release order %v; want %v", order.seen, want)
	}
}

// stateAtRelease records whether the envelope's reply was already answered
// when the event's release ran.
type stateAtRelease struct {
	slot     *slot[Decision[int, int], error]
	answered *atomic.Bool
	closed   *atomic.Int32
}

func (e stateAtRelease) Close() {
	e.answered.Store(e.slot.state.Load() == slotAnswered)
	e.closed.Add(1)
}

// M2, deterministic: the generated envelope release runs the reply's release
// (the later field) before the event's, under a drop.
func TestGeneratedEnvelopeReleasesReplyBeforeEvent(t *testing.T) {
	var answered atomic.Bool
	var closed atomic.Int32
	task := rt.Root("envelope-order")
	sl := newSlot[Decision[int, int], error](&control{task: task})
	var msg machineMsg[int, stateAtRelease, int] = event[int, stateAtRelease, int]{
		ev:    stateAtRelease{sl, &answered, &closed},
		reply: Reply[Decision[int, int], error]{sl},
	}
	var err error
	drop(task, msg, &err)
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	if closed.Load() != 1 || !answered.Load() {
		t.Errorf("event released %d times, reply answered first %v; want 1, true", closed.Load(), answered.Load())
	}
}

// M2: every remaining field release runs after a defect. A reply whose
// release panics (a slot with no control) still lets the event be released,
// and drop records the panic as one in-release defect.
func TestGeneratedEnvelopeReleasesEventAfterReplyDefect(t *testing.T) {
	var closed atomic.Int32
	task := rt.Root("envelope-defect")
	var msg machineMsg[int, lease, int] = event[int, lease, int]{
		ev:    lease{&closed},
		reply: Reply[Decision[int, int], error]{newSlot[Decision[int, int], error](nil)},
	}
	var err error
	drop(task, msg, &err)
	var c *rt.Cause
	if !errors.As(err, &c) || len(c.Reasons) != 1 || c.Reasons[0].Kind != rt.KindDefect || !c.Reasons[0].InRelease {
		t.Errorf("drop: %v; want one in-release defect", err)
	}
	if got := closed.Load(); got != 1 {
		t.Errorf("event released %d times after the reply's defect; want 1", got)
	}
}

// The tests below pin the measured domain. The prototype releases a value
// that implements Close itself, a resource union lowered as an interface with
// Close included. It does not release an aggregate that holds a resource in a
// field or an element: that needs compiler-generated drop glue (resources.md
// R1, R34, R37; research note, Gap A8; #197, #190).

// M1b: Done[S, O] is a struct with a resource field O. A detached asker's
// rejected Done reaches drop, which finds no Close on the struct.
func TestR4MachineDetachedDoneOutputIsReleased(t *testing.T) {
	var closed atomic.Int32
	within(t, func(task *rt.Task, s *rt.Scope) error {
		holding, gate := make(chan struct{}), make(chan struct{})
		m := Machine[int, int, lease]{Step: func(_ *rt.Task, _ int, _ int) (Decision[int, lease], error) {
			close(holding)
			<-gate
			return Done[int, lease]{Output: lease{&closed}}, nil
		}}
		b := StartMachine(s, "resource-output", m, 1)
		asker := rt.Go(s, "detached-output-asker", func(ct *rt.Task) (Decision[int, lease], error) {
			return b.Ask(ct, 0)
		})
		<-holding
		asker.Task().Interrupt()
		if _, err := asker.Wait(); !errors.Is(err, rt.Interrupted) {
			t.Errorf("asker: %v", err)
		}
		close(gate)
		return b.Stop()
	})
	if got := closed.Load(); got != 0 {
		t.Errorf("detached Done output closed %d times; general drop glue landed: update Gap A8 and assert 1", got)
	}
}

// M1b: native slice, array and map states that hold a resource.
func TestR4AgentContainerStateIsReleased(t *testing.T) {
	for _, mode := range []string{"slice", "array", "map"} {
		t.Run(mode, func(t *testing.T) {
			var closed atomic.Int32
			within(t, func(task *rt.Task, s *rt.Scope) error {
				switch mode {
				case "slice":
					return StartAgent(s, "resource-slice", []lease{{&closed}}, 1).Stop()
				case "array":
					return StartAgent(s, "resource-array", [1]lease{{&closed}}, 1).Stop()
				default:
					return StartAgent(s, "resource-map", map[string]lease{"one": {&closed}}, 1).Stop()
				}
			})
			if got := closed.Load(); got != 0 {
				t.Errorf("%s state closed %d times; general drop glue landed: update Gap A8 and assert 1", mode, got)
			}
		})
	}
}

// O1: a machine whose state is a resource hands out a snapshot that shares
// the state's handle, so the caller's release and the machine's both close
// it. The general rule for copying a resource-bearing value (#197) owns it.
func TestR4KnownResourceMachineStateSnapshotAliases(t *testing.T) {
	var closed atomic.Int32
	within(t, func(task *rt.Task, s *rt.Scope) error {
		m := Machine[lease, int, int]{Initial: lease{&closed}, Step: func(_ *rt.Task, _ lease, _ int) (Decision[lease, int], error) {
			return Ignore[lease, int]{}, nil
		}}
		b := StartMachine(s, "known-resource-state", m, 1)
		v, err := b.State(task)
		if err != nil {
			return err
		}
		v.Close()
		return b.Stop()
	})
	if got := closed.Load(); got != 2 {
		t.Errorf("resource state snapshot closed %d times; the snapshot rule changed: update the limitation and assert 1", got)
	}
}
