package actorlib_test

// The matched baseline's contract (Q8, findings M6 of rounds 1 and 2): each
// test checks one clause that the library's tests check, so the matched
// ratios compare the same work and the same guarantees.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Round-2 review probe: a sender cancelled before it sends is refused.
func TestMatchedSendRejectsCanceledCaller(t *testing.T) {
	a := startMatchedCounter(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.send(ctx, mInc{1})
	if stopErr := a.stop(); stopErr != nil {
		t.Errorf("stop: %v", stopErr)
	}
	if err == nil {
		t.Error("matched send admitted a message from an already-canceled caller")
	}
}

// Round-2 review probe, with the callback written against the baseline's
// contract: it waits on the actor's context, as a library callback waits on
// the agent's task. Stop must not need the gate.
func TestMatchedAgentCallbackObservesStop(t *testing.T) {
	a := startMatchedAgent(4)
	holding, gate, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := matchedCall(context.Background(), a, func(ctx context.Context, n *int64) (int64, error) {
			close(holding)
			select {
			case <-gate:
				return *n, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		})
		completed <- err
	}()
	<-holding
	var forced atomic.Bool
	watchdog := time.AfterFunc(5*time.Second, func() { forced.Store(true); close(gate) })
	if err := a.stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	watchdog.Stop()
	if forced.Load() {
		t.Error("matched stop needed an external gate release")
	}
	if err := <-completed; !errors.Is(err, errStopped) {
		t.Errorf("caller of an interrupted callback: got %v, want stopped", err)
	}
}

// A reply dropped while the actor runs completes "no reply", not "stopped".
func TestMatchedRunningDroppedReplyIsNoReply(t *testing.T) {
	a := startFenced(4, func(_ context.Context, m mGet) error {
		m.release() // the handler drops the reply
		return nil
	})
	r := newMSlot[int64](&a.closing)
	if err := a.send(context.Background(), mGet{r}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.receive(context.Background()); !errors.Is(err, errNoReply) {
		t.Errorf("dropped reply: got %v, want no reply", err)
	}
	if err := a.stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
}

type mPanicky struct {
	value  string
	closed *atomic.Int32
}

func (m mPanicky) release() {
	m.closed.Add(1)
	if m.value != "" {
		panic(m.value)
	}
}

// The drain releases every queued message and keeps each release panic, in
// order.
func TestMatchedDrainKeepsEveryReleasePanic(t *testing.T) {
	var closed atomic.Int32
	holding := make(chan struct{})
	a := startFenced(4, func(ctx context.Context, m mPanicky) error {
		close(holding)
		<-ctx.Done()
		return nil
	})
	if err := a.send(context.Background(), mPanicky{closed: &closed}); err != nil {
		t.Fatal(err)
	}
	<-holding
	for _, v := range []string{"first", "second"} {
		if err := a.send(context.Background(), mPanicky{value: v, closed: &closed}); err != nil {
			t.Fatal(err)
		}
	}
	err := a.stop()
	if err == nil || err.Error() != "panic: first\npanic: second" {
		t.Errorf("stop: got %v, want both release panics in order", err)
	}
	if n := closed.Load(); n != 2 {
		t.Errorf("released %d queued messages; want 2", n)
	}
}

type mLease struct{ closed *atomic.Int32 }

func (l mLease) Close() { l.closed.Add(1) }

// A ready answer taken for a cancelled receiver is released.
func TestMatchedCanceledReadyReplyIsReleased(t *testing.T) {
	var closing atomic.Bool
	for range 1024 {
		var closed atomic.Int32
		s := newMSlot[mLease](&closing)
		s.answer(mOutcome[mLease]{v: mLease{&closed}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.receive(ctx); err == nil {
			t.Fatal("cancelled receiver got the answer")
		}
		if closed.Load() != 1 {
			t.Fatalf("answer released %d times; want 1", closed.Load())
		}
	}
}
