package rt

import (
	"context"
	"sync"
	"testing"
)

// Each rt benchmark has a hand-written Go baseline: context.WithCancel for
// the stop, sync.WaitGroup for the join and defer for the release.

var sinkBool bool

func BenchmarkPoll(b *testing.B) {
	t := Root("bench")
	stopped := false
	for b.Loop() {
		stopped = stopped || t.Stopped()
	}
	sinkBool = stopped
}

func BenchmarkPollBaselineContextDone(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := ctx.Done()
	stopped := false
	for b.Loop() {
		select {
		case <-done:
			stopped = true
		default:
		}
	}
	sinkBool = stopped
}

func BenchmarkPollBaselineContextErr(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := false
	for b.Loop() {
		stopped = stopped || ctx.Err() != nil
	}
	sinkBool = stopped
}

func emptyBody(*Task) (struct{}, error) { return struct{}{}, nil }

func BenchmarkSpawnWait(b *testing.B) {
	root := Root("bench")
	b.ReportAllocs()
	for b.Loop() {
		s := NewScope(root)
		h := Go(&s, "empty", emptyBody)
		if _, err := h.Wait(); err != nil {
			b.Fatal(err)
		}
		s.End(nil)
	}
}

func BenchmarkSpawnWaitBaseline(b *testing.B) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	b.ReportAllocs()
	for b.Loop() {
		ctx, cancel := context.WithCancel(parent)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ctx
		}()
		wg.Wait()
		cancel()
	}
}

type noop struct{ closed int }

func openNoop(r *noop) func() (*noop, error) { return func() (*noop, error) { return r, nil } }

func closeNoop(r *noop) { r.closed++ }

func BenchmarkAcquireRelease(b *testing.B) {
	root := Root("bench")
	r := &noop{}
	open := openNoop(r)
	b.ReportAllocs()
	for b.Loop() {
		func() {
			s := NewScope(root)
			defer s.End(nil)
			_, _ = Acquire(&s, open, closeNoop)
		}()
	}
	if r.closed == 0 {
		b.Fatal("no release ran")
	}
}

func BenchmarkAcquireReleaseBaseline(b *testing.B) {
	r := &noop{}
	open := openNoop(r)
	b.ReportAllocs()
	for b.Loop() {
		func() {
			res, err := open()
			if err != nil {
				return
			}
			defer closeNoop(res)
		}()
	}
	if r.closed == 0 {
		b.Fatal("no release ran")
	}
}
