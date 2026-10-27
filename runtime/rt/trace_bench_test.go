package rt_test

import (
	"testing"

	"effra.local/prototype/runtime/rt"
	"effra.local/prototype/runtime/rt/rttrace"
)

// The traced benchmarks run the bodies of BenchmarkPoll, BenchmarkSpawnWait
// and BenchmarkAcquireRelease (bench_test.go) on a tree traced by the real
// bounded recorder. The recorder is reset every resetEvery iterations, while
// no task runs, so it keeps recording instead of measuring the drop path; the
// reset's cost is amortized into ns/op.
const resetEvery = 512

var sinkTraced bool

func BenchmarkPollTraced(b *testing.B) {
	t := rt.TracedRoot("bench", rttrace.New(rttrace.DefaultCapacity))
	stopped := false
	for b.Loop() {
		stopped = stopped || t.Stopped()
	}
	sinkTraced = stopped
}

func emptyTracedBody(*rt.Task) (struct{}, error) { return struct{}{}, nil }

func BenchmarkSpawnWaitTraced(b *testing.B) {
	rec := rttrace.New(rttrace.DefaultCapacity)
	root := rt.TracedRoot("bench", rec)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		s := rt.NewScope(root)
		h := rt.Go(&s, "empty", emptyTracedBody)
		if _, err := h.Wait(); err != nil {
			b.Fatal(err)
		}
		s.End(nil)
		if i++; i%resetEvery == 0 {
			rec.Reset()
		}
	}
	if rec.Dropped() != 0 {
		b.Fatalf("recorder dropped %d events; the benchmark measured the drop path", rec.Dropped())
	}
}

// BenchmarkTraceCallOff is what emitted code pays before each capability call
// when no tracer is installed: Task.Trace, inlined to one load and compare.
func BenchmarkTraceCallOff(b *testing.B) {
	t := rt.Root("bench")
	for b.Loop() {
		t.Trace(rt.TraceCall, "app.Gateway.Authorize", "app.sketch:9:14")
	}
}

// BenchmarkTraceCallOn is one call event recorded by the bounded recorder.
func BenchmarkTraceCallOn(b *testing.B) {
	rec := rttrace.New(rttrace.DefaultCapacity)
	t := rt.TracedRoot("bench", rec)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		t.Trace(rt.TraceCall, "app.Gateway.Authorize", "app.sketch:9:14")
		if i++; i%resetEvery == 0 {
			rec.Reset()
		}
	}
}

type tracedNoop struct{ closed int }

func closeTracedNoop(r *tracedNoop) { r.closed++ }

func BenchmarkAcquireReleaseTraced(b *testing.B) {
	rec := rttrace.New(rttrace.DefaultCapacity)
	root := rt.TracedRoot("bench", rec)
	r := &tracedNoop{}
	open := func() (*tracedNoop, error) { return r, nil }
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		func() {
			s := rt.NewScope(root)
			defer s.End(nil)
			_, _ = rt.Acquire(&s, open, closeTracedNoop)
		}()
		if i++; i%resetEvery == 0 {
			rec.Reset()
		}
	}
	if r.closed == 0 || rec.Dropped() != 0 {
		b.Fatalf("releases %d, dropped %d", r.closed, rec.Dropped())
	}
}
