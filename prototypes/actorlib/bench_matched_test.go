package actorlib_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/prototypes/actorlib/examples"
)

// Q8, round 2 (finding M6). The raw baselines in bench_test.go give less than
// the library. The matched baselines below are hand-written Go with the
// library's contract, as a careful Go programmer would write it without the
// library:
//
//   - admission fencing: a send after stop fails at once; a send blocked on a
//     full mailbox wakes on stop (an RWMutex fence and a stopping channel);
//   - dropped-reply completion: an unanswered reply is completed with a
//     refusal, and a reply nobody can receive is released (a one-shot slot
//     with an atomic state);
//   - queued release: on stop, every queued request is answered "stopped";
//   - structured ownership: stop waits for the goroutine, and a panic in the
//     loop is recovered and kept, so the drain still runs.
//
// The machine baseline runs the same Breaker step logic and returns the same
// decision values. Neither baseline copies values at the boundary: int64 and
// the breaker's states need no copy, which the compiler knows statically; the
// library's run-time stand-in for that decision is measured on its own
// (BenchmarkSnapshot*).

var errStopped = errors.New("stopped")

const (
	mOpen uint32 = iota
	mAnswered
	mAbandoned
)

type mOutcome[T any] struct {
	v       T
	err     error
	refused bool
}

// mSlot is the hand-written one-shot reply with the library's contract.
type mSlot[T any] struct {
	state atomic.Uint32
	ch    chan mOutcome[T]
}

func newMSlot[T any]() *mSlot[T] { return &mSlot[T]{ch: make(chan mOutcome[T], 1)} }

func (s *mSlot[T]) answer(o mOutcome[T]) {
	if s.state.CompareAndSwap(mOpen, mAnswered) {
		s.ch <- o
		return
	}
	releaseAny(o.v)
}

func (s *mSlot[T]) abandon() {
	if s.state.CompareAndSwap(mOpen, mAbandoned) {
		return
	}
	releaseAny((<-s.ch).v)
}

// refuse is the completion of a dropped or discarded reply.
func (s *mSlot[T]) refuse() { s.answer(mOutcome[T]{refused: true, err: errStopped}) }

func (s *mSlot[T]) receive(ctx context.Context) (T, error) {
	select {
	case o := <-s.ch:
		return o.v, o.err
	case <-ctx.Done():
		s.abandon()
		var zero T
		return zero, ctx.Err()
	}
}

func releaseAny[T any](v T) {
	if r, ok := any(v).(interface{ Close() }); ok {
		r.Close()
	}
}

// mMsg is a message of the matched actor: it releases what it owns.
type mMsg interface{ release() }

// fenced is the matched actor's shell: a fenced mailbox, a stoppable
// goroutine and a drain. handle owns each message it receives.
type fenced[M mMsg] struct {
	mu       sync.RWMutex
	closed   bool
	stopping chan struct{}
	once     sync.Once
	mailbox  chan M
	cancel   context.CancelFunc
	done     chan struct{}
	panicked any
}

func startFenced[M mMsg](capacity int, handle func(ctx context.Context, m M)) *fenced[M] {
	ctx, cancel := context.WithCancel(context.Background())
	a := &fenced[M]{stopping: make(chan struct{}), mailbox: make(chan M, capacity), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		defer a.finish()
		for {
			select {
			case m := <-a.mailbox:
				if ctx.Err() != nil {
					m.release()
					return
				}
				handle(ctx, m)
			case <-ctx.Done():
				return
			}
		}
	}()
	return a
}

func (a *fenced[M]) closeAdmission() {
	a.once.Do(func() { close(a.stopping) })
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
}

func (a *fenced[M]) finish() {
	a.panicked = recover()
	a.closeAdmission()
	for {
		select {
		case m := <-a.mailbox:
			m.release()
			continue
		default:
		}
		return
	}
}

func (a *fenced[M]) send(ctx context.Context, m M) error {
	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		m.release()
		return errStopped
	}
	select {
	case a.mailbox <- m:
		a.mu.RUnlock()
		return nil
	default:
	}
	var err error
	select {
	case a.mailbox <- m:
	case <-a.stopping:
		err = errStopped
	case <-ctx.Done():
		err = ctx.Err()
	}
	a.mu.RUnlock()
	if err != nil {
		m.release()
	}
	return err
}

func (a *fenced[M]) stop() {
	a.closeAdmission()
	a.cancel()
	<-a.done
}

// The counter.

type mInc struct{ by int64 }

type mGet struct{ reply *mSlot[int64] }

func (mInc) release()   {}
func (m mGet) release() { m.reply.refuse() }

type mCounterMsg interface{ release() }

func startMatchedCounter(capacity int) *fenced[mCounterMsg] {
	var n int64
	return startFenced(capacity, func(_ context.Context, m mCounterMsg) {
		switch m := m.(type) {
		case mInc:
			n += m.by
		case mGet:
			m.reply.answer(mOutcome[int64]{v: n})
		}
	})
}

func matchedCount(ctx context.Context, a *fenced[mCounterMsg]) (int64, error) {
	r := newMSlot[int64]()
	if err := a.send(ctx, mGet{r}); err != nil {
		r.abandon()
		return 0, err
	}
	return r.receive(ctx)
}

func BenchmarkSendMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedCounter(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		if err := a.send(ctx, mInc{1}); err != nil {
			b.Fatal(err)
		}
	}
	n, err := matchedCount(ctx, a)
	if err != nil {
		b.Fatal(err)
	}
	sink = n
	a.stop()
}

func BenchmarkAskMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedCounter(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		n, err := matchedCount(ctx, a)
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	a.stop()
}

func BenchmarkStartStopMatched(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		startMatchedCounter(benchCapacity).stop()
	}
}

// The machine: the Breaker step on Closed and HalfOpen, with the same state
// and decision values as examples.step.

type mSignal struct {
	sig   examples.Signal
	reply *mSlot[examples.BreakerDecision]
}

func (m mSignal) release() {
	if m.reply != nil {
		m.reply.refuse()
	}
}

func matchedStep(s examples.BreakerState, sig examples.Signal) (examples.BreakerDecision, error) {
	type enter = actorlib.Enter[examples.BreakerState, string]
	type stay = actorlib.Stay[examples.BreakerState, string]
	switch s := s.(type) {
	case examples.Closed:
		switch sig := sig.(type) {
		case examples.Failure:
			if s.Failures+1 >= examples.Trip {
				return enter{Next: examples.Open{Endpoint: sig.Endpoint}}, nil
			}
			return stay{Next: examples.Closed{Failures: s.Failures + 1}}, nil
		case examples.Success:
			return stay{Next: examples.Closed{Failures: 0}}, nil
		}
	case examples.HalfOpen:
		switch sig := sig.(type) {
		case examples.Success:
			return enter{Next: examples.Closed{Failures: 0}}, nil
		case examples.Failure:
			return enter{Next: examples.Open{Endpoint: sig.Endpoint}}, nil
		}
	}
	return actorlib.Ignore[examples.BreakerState, string]{}, nil
}

func startMatchedBreaker(capacity int) *fenced[mSignal] {
	var st examples.BreakerState = examples.Closed{}
	return startFenced(capacity, func(_ context.Context, m mSignal) {
		d, err := matchedStep(st, m.sig)
		if err != nil {
			m.reply.answer(mOutcome[examples.BreakerDecision]{err: err})
			return
		}
		switch d := d.(type) {
		case actorlib.Enter[examples.BreakerState, string]:
			st = d.Next
		case actorlib.Stay[examples.BreakerState, string]:
			st = d.Next
		}
		m.reply.answer(mOutcome[examples.BreakerDecision]{v: d})
	})
}

func BenchmarkMachineAskMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedBreaker(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		r := newMSlot[examples.BreakerDecision]()
		if err := a.send(ctx, mSignal{examples.Success{}, r}); err != nil {
			b.Fatal(err)
		}
		if _, err := r.receive(ctx); err != nil {
			b.Fatal(err)
		}
	}
	a.stop()
}

// The agent: a closure message on the state, completed if it panics.

type mCall struct {
	run   func(n *int64)
	reply interface{ refuse() }
}

func (m mCall) release() { m.reply.refuse() }

func startMatchedAgent(capacity int) *fenced[mCall] {
	var n int64
	return startFenced(capacity, func(_ context.Context, c mCall) {
		defer c.release() // answers nothing after run answered
		c.run(&n)
	})
}

func matchedCall[T any](ctx context.Context, a *fenced[mCall], f func(n *int64) (T, error)) (T, error) {
	r := newMSlot[T]()
	err := a.send(ctx, mCall{run: func(n *int64) {
		v, err := f(n)
		r.answer(mOutcome[T]{v: v, err: err})
	}, reply: r})
	if err != nil {
		r.abandon()
		var zero T
		return zero, err
	}
	return r.receive(ctx)
}

func BenchmarkAgentCallMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedAgent(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		n, err := matchedCall(ctx, a, func(n *int64) (int64, error) { return *n, nil })
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	a.stop()
}

// The copy rule's cost (finding M3): the library's run-time snapshot of a
// value with no slice or map (one type-cache lookup), of an 8-name slice and
// of a struct holding one, against the copy a compiler emits for the same
// static type (none, slices.Clone).

var names8 = []string{"ada", "grace", "edsger", "barbara", "alan", "donald", "leslie", "tony"}

type withNames struct {
	limit int64
	names []string
}

var sinkNames []string

func BenchmarkSnapshotInt64(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sink = actorlib.Snapshot(int64(1))
	}
}

// BenchmarkSnapshotUnion is a message union holding a variant with no slice
// or map: the counter's Inc, as Send sees it.
func BenchmarkSnapshotUnion(b *testing.B) {
	var m examples.CounterMsg = examples.Inc{By: 1}
	b.ReportAllocs()
	for b.Loop() {
		m = actorlib.Snapshot(m)
	}
}

func BenchmarkSnapshotSlice8(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkNames = actorlib.Snapshot(names8)
	}
}

func BenchmarkSnapshotSlice8Static(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkNames = slices.Clone(names8)
	}
}

func BenchmarkSnapshotStruct(b *testing.B) {
	v := withNames{limit: 8, names: names8}
	b.ReportAllocs()
	for b.Loop() {
		sinkNames = actorlib.Snapshot(v).names
	}
}

func BenchmarkSnapshotStructStatic(b *testing.B) {
	v := withNames{limit: 8, names: names8}
	b.ReportAllocs()
	for b.Loop() {
		c := v
		c.names = slices.Clone(v.names)
		sinkNames = c.names
	}
}
