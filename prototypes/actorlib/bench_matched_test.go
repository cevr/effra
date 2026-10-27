package actorlib_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"ergo.local/prototype/prototypes/actorlib"
	"ergo.local/prototype/prototypes/actorlib/examples"
)

// Q8 (findings M6 of rounds 1 and 2). The raw baselines in bench_test.go give
// less than the library. The matched baselines below are hand-written Go with
// the library's contract, as a careful Go programmer would write it without
// the library. bench_matched_contract_test.go checks each clause:
//
//   - admission: a sender whose context is already cancelled is refused
//     before it admits (I1); a send after stop fails at once; a send blocked
//     on a full mailbox wakes on stop (an RWMutex fence, a closing flag and a
//     stopping channel);
//   - dropped-reply completion: an unanswered reply is completed with a
//     refusal, "no reply" while the actor runs and "stopped" once it stops,
//     and a reply nobody can receive is released, including one taken by a
//     receiver whose context was cancelled (a one-shot slot with an atomic
//     state and checks before and after the wait);
//   - waits inside the actor observe its stop: an agent callback receives the
//     actor's context;
//   - queued release: on stop, every queued request is answered "stopped",
//     and each release's panic is recovered and kept, in order;
//   - structured ownership: stop waits for the goroutine, and a panic in the
//     loop is recovered and kept, so the drain still runs.
//
// The machine baseline runs the same Breaker step logic and returns the same
// decision values. Neither baseline copies values at the boundary: int64 and
// the breaker's states need no copy, which the compiler knows statically; the
// library's run-time stand-in for that decision is measured on its own
// (BenchmarkSnapshot*).

var (
	errStopped = errors.New("stopped")
	errNoReply = errors.New("no reply")
)

const (
	mOpen uint32 = iota
	mAnswered
	mAbandoned
)

// mOutcome is one answer. Its kind decides what it owns, as the library's
// outcome does: a success owns v, a failure (err set, not refused) owns err,
// and a refusal owns nothing, so its zero v is never closed.
type mOutcome[T any] struct {
	v       T
	err     error
	refused bool
}

// release releases what o owns, by kind.
func (o mOutcome[T]) release() {
	switch {
	case o.refused:
	case o.err != nil:
		releaseAny(o.err)
	default:
		releaseAny(o.v)
	}
}

// releaseKeeping is release on the receiving side: a panic in it comes back
// as an error, as the library's receiver gets an in-release defect.
func (o mOutcome[T]) releaseKeeping() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("release panic: %v", p)
		}
	}()
	o.release()
	return nil
}

// mSlot is the hand-written one-shot reply with the library's contract.
type mSlot[T any] struct {
	state   atomic.Uint32
	ch      chan mOutcome[T]
	closing *atomic.Bool // the actor's: set once its stop is requested
}

func newMSlot[T any](closing *atomic.Bool) *mSlot[T] {
	return &mSlot[T]{ch: make(chan mOutcome[T], 1), closing: closing}
}

func (s *mSlot[T]) answer(o mOutcome[T]) {
	if s.state.CompareAndSwap(mOpen, mAnswered) {
		s.ch <- o
		return
	}
	o.release() // on the answering goroutine: a panic ends the actor, as in the library
}

// abandon releases an answer that arrived first and returns its release
// panic; a later answer is released by answer.
func (s *mSlot[T]) abandon() error {
	if s.state.CompareAndSwap(mOpen, mAbandoned) {
		return nil
	}
	return (<-s.ch).releaseKeeping()
}

// refuse is the completion of a dropped or discarded reply.
func (s *mSlot[T]) refuse() {
	err := errNoReply
	if s.closing.Load() {
		err = errStopped
	}
	s.answer(mOutcome[T]{refused: true, err: err})
}

func (s *mSlot[T]) receive(ctx context.Context) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, joinExit(s.abandon(), err)
	}
	select {
	case o := <-s.ch:
		if err := ctx.Err(); err != nil {
			return zero, joinExit(o.releaseKeeping(), err)
		}
		return o.v, o.err
	case <-ctx.Done():
		return zero, joinExit(s.abandon(), ctx.Err())
	}
}

func releaseAny[T any](v T) {
	if r, ok := any(v).(interface{ Close() }); ok {
		r.Close()
	}
}

// joinExit joins two exits in order, keeping a lone one as it is.
func joinExit(a, b error) error {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return errors.Join(a, b)
}

// mMsg is a message of the matched actor: it releases what it owns.
type mMsg interface{ release() }

// releaseMsg releases a message the sender still owns and returns its panic,
// as the library's sender gets an in-release defect after its refusal.
func releaseMsg[M mMsg](m M) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("release panic: %v", p)
		}
	}()
	m.release()
	return nil
}

// fenced is the matched actor's shell: a fenced mailbox, a stoppable
// goroutine and a drain. handle owns each message it receives; a non-nil
// result ends the actor with that exit.
type fenced[M mMsg] struct {
	mu       sync.RWMutex
	closed   bool
	closing  atomic.Bool
	stopping chan struct{}
	once     sync.Once
	mailbox  chan M
	cancel   context.CancelFunc
	done     chan struct{}
	exit     error
	panics   []any // the loop's panic, then each release panic, in order
}

func startFenced[M mMsg](capacity int, handle func(ctx context.Context, m M) error) *fenced[M] {
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
				if err := handle(ctx, m); err != nil {
					a.exit = err
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return a
}

func (a *fenced[M]) closeAdmission() {
	a.closing.Store(true)
	a.once.Do(func() { close(a.stopping) })
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
}

func (a *fenced[M]) finish() {
	if p := recover(); p != nil {
		a.panics = append(a.panics, p)
	}
	a.closeAdmission()
	for {
		select {
		case m := <-a.mailbox:
			a.releaseRecovering(m)
			continue
		default:
		}
		return
	}
}

func (a *fenced[M]) releaseRecovering(m M) {
	defer func() {
		if p := recover(); p != nil {
			a.panics = append(a.panics, p)
		}
	}()
	m.release()
}

func (a *fenced[M]) send(ctx context.Context, m M) error {
	if err := ctx.Err(); err != nil {
		return joinExit(err, releaseMsg(m))
	}
	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		return joinExit(errStopped, releaseMsg(m))
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
		return joinExit(err, releaseMsg(m))
	}
	return nil
}

// stop closes admission, cancels the actor and waits. It returns the exit
// that ended the actor, other than the stop's own cancellation, and every
// panic, in order.
func (a *fenced[M]) stop() error {
	a.closeAdmission()
	a.cancel()
	<-a.done
	var errs []error
	if a.exit != nil && !errors.Is(a.exit, context.Canceled) {
		errs = append(errs, a.exit)
	}
	for _, p := range a.panics {
		errs = append(errs, fmt.Errorf("panic: %v", p))
	}
	return errors.Join(errs...)
}

// The counter.

type mInc struct{ by int64 }

type mGet struct{ reply *mSlot[int64] }

func (mInc) release()   {}
func (m mGet) release() { m.reply.refuse() }

type mCounterMsg interface{ release() }

func startMatchedCounter(capacity int) *fenced[mCounterMsg] {
	var n int64
	return startFenced(capacity, func(_ context.Context, m mCounterMsg) error {
		switch m := m.(type) {
		case mInc:
			n += m.by
		case mGet:
			m.reply.answer(mOutcome[int64]{v: n})
		}
		return nil
	})
}

func matchedCount(ctx context.Context, a *fenced[mCounterMsg]) (int64, error) {
	r := newMSlot[int64](&a.closing)
	if err := a.send(ctx, mGet{r}); err != nil {
		return 0, joinExit(err, r.abandon())
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
	if err := a.stop(); err != nil {
		b.Fatal(err)
	}
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
	if err := a.stop(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkStartStopMatched(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if err := startMatchedCounter(benchCapacity).stop(); err != nil {
			b.Fatal(err)
		}
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
	return startFenced(capacity, func(_ context.Context, m mSignal) error {
		defer m.release() // answers nothing after the answer; "no reply" if the step panics
		d, err := matchedStep(st, m.sig)
		if err != nil {
			m.reply.answer(mOutcome[examples.BreakerDecision]{err: err})
			return nil
		}
		switch d := d.(type) {
		case actorlib.Enter[examples.BreakerState, string]:
			st = d.Next
		case actorlib.Stay[examples.BreakerState, string]:
			st = d.Next
		}
		m.reply.answer(mOutcome[examples.BreakerDecision]{v: d})
		return nil
	})
}

func BenchmarkMachineAskMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedBreaker(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		r := newMSlot[examples.BreakerDecision](&a.closing)
		if err := a.send(ctx, mSignal{examples.Success{}, r}); err != nil {
			b.Fatal(err)
		}
		if _, err := r.receive(ctx); err != nil {
			b.Fatal(err)
		}
	}
	if err := a.stop(); err != nil {
		b.Fatal(err)
	}
}

// The agent: a closure message on the state, completed if it panics. The
// callback receives the actor's context, so its waits observe the stop.

type mCall struct {
	run   func(ctx context.Context, n *int64) error
	reply interface{ refuse() }
}

func (m mCall) release() { m.reply.refuse() }

func startMatchedAgent(capacity int) *fenced[mCall] {
	var n int64
	return startFenced(capacity, func(ctx context.Context, c mCall) error {
		defer c.release() // answers nothing after run answered
		return c.run(ctx, &n)
	})
}

// matchedCall answers the caller with f's value or error. An error that is
// the actor's own cancellation ends the actor; the caller then gets
// "stopped" from the release.
func matchedCall[T any](ctx context.Context, a *fenced[mCall], f func(ctx context.Context, n *int64) (T, error)) (T, error) {
	r := newMSlot[T](&a.closing)
	err := a.send(ctx, mCall{run: func(actx context.Context, n *int64) error {
		v, err := f(actx, n)
		if err != nil && actx.Err() != nil && errors.Is(err, actx.Err()) {
			return err
		}
		r.answer(mOutcome[T]{v: v, err: err})
		return nil
	}, reply: r})
	if err != nil {
		var zero T
		return zero, joinExit(err, r.abandon())
	}
	return r.receive(ctx)
}

func BenchmarkAgentCallMatched(b *testing.B) {
	ctx := context.Background()
	a := startMatchedAgent(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		n, err := matchedCall(ctx, a, func(_ context.Context, n *int64) (int64, error) { return *n, nil })
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	if err := a.stop(); err != nil {
		b.Fatal(err)
	}
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
