package actorlib_test

import (
	"context"
	"sync"
	"testing"

	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/prototypes/actorlib/examples"
	"effra.local/prototype/runtime/rt"
)

// Q8. Each library benchmark has a hand-written Go baseline: the actor a Go
// programmer writes with a goroutine, a bounded channel, a reply channel per
// request and context cancellation (the Tokio "handle + mpsc + oneshot"
// pattern). The baseline gives less: a send after stop blocks until the
// sender's own context ends instead of failing Stopped, queued requests are
// not answered on stop, and a dropped reply hangs the caller. The matched
// baselines with the library's contract are in bench_matched_test.go.

const benchCapacity = 128

type baseMsg interface{ isBaseMsg() }

type baseInc struct{ by int64 }

type baseGet struct{ reply chan int64 }

// baseResult is a reply that can carry a failure, as a request that can fail
// needs. Its error field is a pointer, so Go allocates the reply channel's
// buffer apart from the channel.
type baseResult struct {
	n   int64
	err error
}

type baseGetResult struct{ reply chan baseResult }

func (baseInc) isBaseMsg()       {}
func (baseGet) isBaseMsg()       {}
func (baseGetResult) isBaseMsg() {}

type baseActor struct {
	mailbox chan baseMsg
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func startBase(capacity int) *baseActor {
	ctx, cancel := context.WithCancel(context.Background())
	a := &baseActor{mailbox: make(chan baseMsg, capacity), cancel: cancel}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		var n int64
		for {
			select {
			case m := <-a.mailbox:
				switch m := m.(type) {
				case baseInc:
					n += m.by
				case baseGet:
					m.reply <- n
				case baseGetResult:
					m.reply <- baseResult{n: n}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return a
}

func (a *baseActor) send(ctx context.Context, m baseMsg) error {
	select {
	case a.mailbox <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *baseActor) get(ctx context.Context) (int64, error) {
	reply := make(chan int64, 1)
	if err := a.send(ctx, baseGet{reply}); err != nil {
		return 0, err
	}
	select {
	case n := <-reply:
		return n, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (a *baseActor) getResult(ctx context.Context) (int64, error) {
	reply := make(chan baseResult, 1)
	if err := a.send(ctx, baseGetResult{reply}); err != nil {
		return 0, err
	}
	select {
	case r := <-reply:
		return r.n, r.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (a *baseActor) stop() {
	a.cancel()
	a.wg.Wait()
}

var sink int64

func BenchmarkSend(b *testing.B) {
	root := rt.Root("bench")
	s := rt.NewScope(root)
	defer s.End(nil)
	c := examples.StartCounter(&s, benchCapacity)
	addr := c.Address()
	b.ReportAllocs()
	for b.Loop() {
		if err := addr.Send(root, examples.Inc{By: 1}); err != nil {
			b.Fatal(err)
		}
	}
	n, err := examples.Count(root, addr) // every send handled
	if err != nil {
		b.Fatal(err)
	}
	sink = n
	if err := c.Stop(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkSendBaseline(b *testing.B) {
	ctx := context.Background()
	a := startBase(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		if err := a.send(ctx, baseInc{1}); err != nil {
			b.Fatal(err)
		}
	}
	n, err := a.get(ctx)
	if err != nil {
		b.Fatal(err)
	}
	sink = n
	a.stop()
}

func BenchmarkAsk(b *testing.B) {
	root := rt.Root("bench")
	s := rt.NewScope(root)
	defer s.End(nil)
	c := examples.StartCounter(&s, benchCapacity)
	addr := c.Address()
	b.ReportAllocs()
	for b.Loop() {
		n, err := examples.Count(root, addr)
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	if err := c.Stop(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkAskBaseline(b *testing.B) {
	ctx := context.Background()
	a := startBase(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		n, err := a.get(ctx)
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	a.stop()
}

// BenchmarkAskBaselineResult is the baseline with a reply that can carry a
// failure.
func BenchmarkAskBaselineResult(b *testing.B) {
	ctx := context.Background()
	a := startBase(benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		n, err := a.getResult(ctx)
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	a.stop()
}

func BenchmarkStartStop(b *testing.B) {
	root := rt.Root("bench")
	b.ReportAllocs()
	for b.Loop() {
		s := rt.NewScope(root)
		c := examples.StartCounter(&s, benchCapacity)
		if err := c.Stop(); err != nil {
			b.Fatal(err)
		}
		s.End(nil)
	}
}

func BenchmarkStartStopBaseline(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		startBase(benchCapacity).stop()
	}
}

// The machine-backed actor and the agent against the same Ask baseline.

func BenchmarkMachineAsk(b *testing.B) {
	root := rt.Root("bench")
	s := rt.NewScope(root)
	defer s.End(nil)
	m := examples.StartBreaker(&s, nil, benchCapacity)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Ask(root, examples.Success{}); err != nil {
			b.Fatal(err)
		}
	}
	if err := m.Stop(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkAgentCall(b *testing.B) {
	root := rt.Root("bench")
	s := rt.NewScope(root)
	defer s.End(nil)
	ag := actorlib.StartAgent(&s, "agent", int64(0), benchCapacity)
	addr := ag.Address()
	b.ReportAllocs()
	for b.Loop() {
		n, err := actorlib.Call(root, addr, func(_ *rt.Task, n *int64) (int64, error) { return *n, nil })
		if err != nil {
			b.Fatal(err)
		}
		sink = n
	}
	if err := ag.Stop(); err != nil {
		b.Fatal(err)
	}
}
