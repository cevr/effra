package rt_test

import (
	"errors"
	"testing"
	"time"

	"effra.local/prototype/runtime/rt"
	"effra.local/prototype/runtime/rt/rttrace"
)

// records returns every event a finished traced tree recorded, failing the
// test on an event still in flight or a dropped one.
func records(t *testing.T, rec *rttrace.Recorder) []rttrace.Record {
	t.Helper()
	recs, inFlight := rec.Records()
	if inFlight != 0 || rec.Dropped() != 0 {
		t.Fatalf("in flight %d, dropped %d; want a complete trace", inFlight, rec.Dropped())
	}
	return recs
}

// find returns the first record of kind k for task id, and whether it exists.
func find(recs []rttrace.Record, k rt.EventKind, id uint64) (rttrace.Record, bool) {
	for _, r := range recs {
		if r.Kind == k && r.Task == id {
			return r, true
		}
	}
	return rttrace.Record{}, false
}

func TestTraceEventsCarryIDsAndParents(t *testing.T) {
	rec := rttrace.New(64)
	root := rt.TracedRoot("app.main", rec)
	var child, grandchild uint64
	_, err := rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
		s := rt.NewScope(t0)
		defer s.End(nil)
		h := rt.Go(&s, "app.child", func(t1 *rt.Task) (struct{}, error) {
			child = t1.ID()
			s1 := rt.NewScope(t1)
			defer s1.End(nil)
			g := rt.Go(&s1, "app.grandchild", func(t2 *rt.Task) (struct{}, error) {
				grandchild = t2.ID()
				return struct{}{}, nil
			})
			_, err := g.Wait()
			return struct{}{}, err
		})
		_, err := h.Wait()
		return struct{}{}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	recs := records(t, rec)
	tree := []struct {
		id, parent uint64
		def        string
	}{
		{root.ID(), 0, "app.main"},
		{child, root.ID(), "app.child"},
		{grandchild, child, "app.grandchild"},
	}
	var exitSeq []uint64
	for _, want := range tree {
		for _, k := range []rt.EventKind{rt.TraceStart, rt.TraceBodyExit} {
			r, ok := find(recs, k, want.id)
			if !ok {
				t.Fatalf("no %v event for task %d (%s): %+v", k, want.id, want.def, recs)
			}
			if r.Parent != want.parent || r.Def != want.def || r.Cause != nil {
				t.Errorf("%v of task %d: parent %d def %q cause %v; want parent %d def %q, success", k, want.id, r.Parent, r.Def, r.Cause, want.parent, want.def)
			}
			if k == rt.TraceBodyExit {
				exitSeq = append(exitSeq, r.Seq)
			}
		}
	}
	// A child's exit happens before its owner's: the owner waits for it.
	if !(exitSeq[2] < exitSeq[1] && exitSeq[1] < exitSeq[0]) {
		t.Errorf("body-exit order (root, child, grandchild) = %v; want grandchild, child, root", exitSeq)
	}
	// Each completed Wait records the owner's observation of the child.
	for _, id := range []uint64{child, grandchild} {
		if _, ok := find(recs, rt.TraceObserve, id); !ok {
			t.Errorf("no observe event for task %d", id)
		}
	}
}

func TestTraceTimeoutRecordsDeadline(t *testing.T) {
	v := rt.NewVirtual(time.Unix(0, 0))
	rec := rttrace.New(64)
	root := rt.TracedRoot("app.main", rec)
	var child uint64
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
			return rt.After(t0, v, time.Second, func(c *rt.Task) (struct{}, error) {
				child = c.ID()
				return struct{}{}, rt.Sleep(c, v, time.Hour)
			})
		})
		done <- err
	}()
	v.AwaitTimers(2) // the deadline and the child's sleep
	v.Advance(time.Second)
	err := <-done
	if _, ok := rt.Failure(err); !ok || !errors.As(err, new(rt.Expired)) {
		t.Fatalf("exit %v; want timeout.Expired", err)
	}
	recs := records(t, rec)
	deadline, okD := find(recs, rt.TraceDeadline, child)
	cancel, okC := find(recs, rt.TraceCancelRequest, child)
	childExit, okE := find(recs, rt.TraceBodyExit, child)
	rootExit, okR := find(recs, rt.TraceBodyExit, root.ID())
	if !okD || !okC || !okE || !okR {
		t.Fatalf("deadline %v, cancel-request %v, child exit %v, root exit %v; want all: %+v", okD, okC, okE, okR, recs)
	}
	if deadline.Parent != root.ID() || deadline.Def != "timeout.After" {
		t.Errorf("deadline: parent %d def %q; want parent %d def timeout.After", deadline.Parent, deadline.Def, root.ID())
	}
	// The deadline and the request come from After's goroutine, in that order;
	// the child's exit happens before the owner's.
	if !(deadline.Seq < cancel.Seq && childExit.Seq < rootExit.Seq) {
		t.Errorf("seq deadline %d, cancel-request %d, child exit %d, root exit %d; want deadline < cancel-request and child exit < root exit",
			deadline.Seq, cancel.Seq, childExit.Seq, rootExit.Seq)
	}
	if childExit.Cause != rt.Interrupted {
		t.Errorf("child exit %v; want interrupted", childExit.Cause)
	}
	if _, ok := rt.Failure(rootExit.Cause); !ok {
		t.Errorf("root exit %v; want the typed failure timeout.Expired", rootExit.Cause)
	}
}

// panicky panics on its first event and counts every call.
type panicky struct{ calls int }

func (p *panicky) Event(rt.Event) {
	p.calls++
	panic("tracer bug")
}

// A tracer that panics is disabled for its tree. Its panic is not a defect of
// the program, and no release is skipped.
func TestTracePanickingTracerIsDisabled(t *testing.T) {
	p := &panicky{}
	root := rt.TracedRoot("app.main", p)
	released := 0
	_, err := rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
		return rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
			s.Defer(func() { released++ })
			t0.Trace(rt.TraceCall, "app.Cap.Op", "app.sketch:3:2")
			return struct{}{}, nil
		})
	})
	if err != nil || released != 1 || p.calls != 1 {
		t.Fatalf("exit %v, releases %d, tracer calls %d; want success, 1 release, 1 call", err, released, p.calls)
	}
}

// Emitted code names its own sites; the runtime passes them through.
func TestTraceCarriesEmittedSites(t *testing.T) {
	rec := rttrace.New(8)
	root := rt.TracedRoot("app.main", rec)
	root.Trace(rt.TraceCall, "app.Gateway.Authorize", "app.sketch:9:14")
	recs := records(t, rec)
	if len(recs) != 1 || recs[0].Kind.String() != "call" || recs[0].Task != root.ID() ||
		recs[0].Def != "app.Gateway.Authorize" || recs[0].Site != "app.sketch:9:14" {
		t.Fatalf("records %+v; want one call event with its definition and site", recs)
	}
	untraced := rt.Root("app.main")
	untraced.Trace(rt.TraceCall, "app.Gateway.Authorize", "app.sketch:9:14") // no tracer: nothing to do
}

// A cancel-request is recorded before the stop flag is set, so the child's
// observation of the stop is always recorded after it. A child started under
// a stopped owner records its request too.
func TestTraceCancelRequestPrecedesObservation(t *testing.T) {
	for i := 0; i < 200; i++ {
		rec := rttrace.New(64)
		root := rt.TracedRoot("app.main", rec)
		var child uint64
		_, _ = rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
			s := rt.NewScope(t0)
			defer s.End(nil)
			started := make(chan struct{})
			h := rt.Go(&s, "app.child", func(c *rt.Task) (struct{}, error) {
				child = c.ID()
				close(started)
				for !c.Stopped() {
				}
				c.Trace(rt.TraceInterrupt, "app.child", "app.sketch:4:2")
				return struct{}{}, rt.Interrupted
			})
			<-started
			return struct{}{}, h.Stop()
		})
		recs := records(t, rec)
		cancel, okC := find(recs, rt.TraceCancelRequest, child)
		seen, okS := find(recs, rt.TraceInterrupt, child)
		if !okC || !okS || cancel.Seq > seen.Seq {
			t.Fatalf("run %d: cancel-request %+v (%v), interrupt %+v (%v); want the request first", i, cancel, okC, seen, okS)
		}
	}

	rec := rttrace.New(64)
	root := rt.TracedRoot("app.main", rec)
	var late uint64
	_, _ = rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
		t0.Interrupt()
		s := rt.NewScope(t0)
		defer s.End(nil)
		h := rt.Go(&s, "app.late", func(c *rt.Task) (struct{}, error) {
			late = c.ID()
			return struct{}{}, nil
		})
		return h.Wait()
	})
	if _, ok := find(records(t, rec), rt.TraceCancelRequest, late); !ok {
		t.Fatalf("a child started under a stopped owner recorded no cancel-request")
	}
}
