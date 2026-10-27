package actorlib

// The round-2 review probes (research note Q2, Q5, Q6 and Q7), kept as
// regression tests.

import (
	"errors"
	"sync/atomic"
	"testing"

	"effra.local/prototype/runtime/rt"
)

// M1: an answer and a stop ready together. The receiving task is stopped
// before it receives, so a select alone may take the answer for a stopped
// task, whose boundary then discards it. Each answer must be released once.
func TestCanceledReadyReplyReleasesResource(t *testing.T) {
	const rounds = 4096
	leaked := 0
	for range rounds {
		var closed atomic.Int32
		task := rt.Root("already-canceled-ready-reply")
		sl := newSlot[lease, Never](&control{task: task})
		sl.answer(outcome[lease, Never]{v: lease{&closed}})
		task.Interrupt()
		_, err := rt.Run(task, func(ct *rt.Task) (lease, error) { return sl.receive(ct) })
		if !errors.Is(err, rt.Interrupted) {
			t.Fatalf("exit: %v", err)
		}
		if closed.Load() != 1 {
			leaked++
		}
	}
	if leaked != 0 {
		t.Errorf("%d/%d simultaneous ready-reply and stop outcomes lost their resource", leaked, rounds)
	}
}

// M2: the initial state crosses into the agent's task, so it is a copy.
func TestAgentInitialValueIsCopied(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		initial := []int64{1}
		ag := StartAgent(s, "initial-copy", initial, 4)
		initial[0] = 99
		got, err := Call(task, ag.Address(), func(_ *rt.Task, st *[]int64) ([]int64, error) { return *st, nil })
		if err != nil {
			return err
		}
		if got[0] != 1 {
			t.Errorf("sender mutation changed initial actor state to %d; want 1", got[0])
		}
		return ag.Stop()
	})
}

// M2: a queued message that holds a Reply is a resource, but its ordinary
// fields are values: they are copied at Send, while the Reply moves.
func TestQueuedReplyBearingMessageIsCopied(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		holding, gate := make(chan struct{}), make(chan struct{})
		first := true
		a := Start(s, "queued-copy", 0, 4, func(_ *rt.Task, n int, m item) (Next[int], error) {
			defer m.Close()
			if first {
				first = false
				close(holding)
				<-gate
			} else {
				m.reply.Send(m.tags)
			}
			return Continue(n), nil
		})
		if err := a.Send(task, item{}); err != nil {
			return err
		}
		<-holding
		sl := newSlot[[]string, Never](&a.addr.c.control)
		tags := []string{"a"}
		if err := a.Send(task, item{tags: tags, reply: Reply[[]string, Never]{sl}}); err != nil {
			return err
		}
		tags[0] = "sender changed"
		close(gate)
		got, err := sl.receive(task)
		if err != nil {
			return err
		}
		if got[0] != "a" {
			t.Errorf("queued message reads %q; want a", got[0])
		}
		return a.Stop()
	})
}

type valuesFailure struct{ values []int64 }

func (valuesFailure) Error() string { return "failure-with-values" }

type valuesFailureRequest struct {
	reply Reply[struct{}, valuesFailure]
}

func (m valuesFailureRequest) Close() { m.reply.Close() }

// M2: a failure payload leaves the actor's task, so it is a copy.
func TestFailurePayloadIsCopied(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		a := Start(s, "failure-copy", []int64{1}, 4, func(_ *rt.Task, st []int64, m valuesFailureRequest) (Next[[]int64], error) {
			defer m.Close()
			m.reply.Fail(valuesFailure{st})
			return Continue(st), nil
		})
		request := func(r Reply[struct{}, valuesFailure]) valuesFailureRequest { return valuesFailureRequest{r} }
		_, err := Ask(task, a.Address(), request)
		first := err.(valuesFailure)
		first.values[0] = 99
		_, err = Ask(task, a.Address(), request)
		if got := err.(valuesFailure).values[0]; got != 1 {
			t.Errorf("failure mutation changed actor state to %d; want 1", got)
		}
		return a.Stop()
	})
}

// M2: an agent call's typed failure is copied too.
func TestAgentFailurePayloadIsCopied(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		ag := StartAgent(s, "agent-failure-copy", []int64{1}, 4)
		fail := func(_ *rt.Task, st *[]int64) (int, error) { return 0, valuesFailure{*st} }
		_, err := Call(task, ag.Address(), fail)
		err.(valuesFailure).values[0] = 99
		_, err = Call(task, ag.Address(), fail)
		if got := err.(valuesFailure).values[0]; got != 1 {
			t.Errorf("failure mutation changed agent state to %d; want 1", got)
		}
		return ag.Stop()
	})
}

// M3: a callback defect ends the agent, and the agent still releases its
// state once.
func TestAgentStateReleasedOnCallbackDefect(t *testing.T) {
	var closed atomic.Int32
	within(t, func(task *rt.Task, s *rt.Scope) error {
		ag := StartAgent(s, "state-on-defect", lease{&closed}, 4)
		_, err := Call(task, ag.Address(), func(_ *rt.Task, st *lease) (int, error) { panic("callback defect") })
		if !failureIs[NoReply](err) {
			t.Errorf("Call: %v", err)
		}
		if err := ag.Stop(); err == nil {
			t.Error("missing callback defect")
		}
		return nil
	})
	if closed.Load() != 1 {
		t.Errorf("state released %d times after callback defect; want 1", closed.Load())
	}
}

// M5: a defect cause that f returns through a managed scope is the agent's
// defect, not the caller's typed failure.
func TestAgentStopsOnManagedCallbackDefect(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		ag := StartAgent(s, "managed-callback-defect", 0, 4)
		_, err := Call(task, ag.Address(), func(at *rt.Task, st *int) (v int, err error) {
			local := rt.NewScope(at)
			defer local.End(&err)
			local.Defer(func() { panic("callback cleanup defect") })
			return 1, nil
		})
		if !failureIs[NoReply](err) {
			t.Errorf("callback defect became caller result %v; want NoReply", err)
		}
		if _, err := Call(task, ag.Address(), get); !failureIs[Stopped](err) {
			t.Errorf("agent continued after callback cleanup defect: %v", err)
		}
		err = ag.Stop()
		var c *rt.Cause
		if !errors.As(err, &c) || len(c.Reasons) != 1 || c.Reasons[0].Kind != rt.KindDefect || c.Reasons[0].Value != "callback cleanup defect" {
			t.Errorf("Stop: got %v, want the callback's cleanup defect alone", err)
		}
		return nil
	})
}

// M4: an interruption carried out of a nested scope stays an interruption.
func TestManagedInterruptionIsNotADefect(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		ag := StartAgent(s, "managed-interruption", 0, 4)
		holding := make(chan struct{})
		asker := rt.Go(s, "asker", func(ct *rt.Task) (int, error) {
			return Call(ct, ag.Address(), func(at *rt.Task, st *int) (int, error) {
				local := rt.NewScope(at)
				defer local.End(nil)
				close(holding)
				<-at.Done()
				at.Check()
				return 0, nil
			})
		})
		<-holding
		if err := ag.Stop(); err != nil {
			t.Errorf("normal managed interruption became a defect: %v", err)
		}
		if _, err := asker.Wait(); !failureIs[Stopped](err) {
			t.Errorf("caller: %v", err)
		}
		return nil
	})
}

type scopedRelease struct{ task *rt.Task }

func (r scopedRelease) Close() {
	s := rt.NewScope(r.task)
	defer s.End(nil)
	s.Defer(func() { panic("release one") })
	s.Defer(func() { panic("release two") })
}

type scopedMessage struct {
	close  scopedRelease
	active bool
}

func (m scopedMessage) Close() {
	if m.active {
		m.close.Close()
	}
}

// M4: a queued message whose Close ends a scope with two release defects
// adds both defects to the actor's exit, each as itself, in order.
func TestDrainPreservesNestedManagedCauses(t *testing.T) {
	within(t, func(task *rt.Task, s *rt.Scope) error {
		holding := make(chan *rt.Task)
		a := Start(s, "managed-drain", 0, 4, func(ct *rt.Task, n int, m scopedMessage) (Next[int], error) {
			holding <- ct
			<-ct.Done()
			return Continue(n), nil
		})
		if err := a.Send(task, scopedMessage{}); err != nil {
			return err
		}
		at := <-holding
		if err := a.Send(task, scopedMessage{close: scopedRelease{at}, active: true}); err != nil {
			return err
		}
		err := a.Stop()
		var cause *rt.Cause
		if !errors.As(err, &cause) {
			t.Fatalf("not a cause: %v", err)
		}
		var values []any
		for _, r := range cause.Reasons {
			if r.Kind != rt.KindDefect || !r.InRelease {
				t.Errorf("reason %+v is not a release defect", r)
			}
			values = append(values, r.Value)
		}
		// The nested scope releases in reverse order: two, then one.
		if len(values) != 2 || values[0] != "release two" || values[1] != "release one" {
			t.Errorf("got reasons %v; want the two original release defects in release order", values)
		}
		return nil
	})
}
