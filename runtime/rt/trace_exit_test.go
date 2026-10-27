package rt_test

import (
	"testing"

	"effra.local/prototype/runtime/rt"
	"effra.local/prototype/runtime/rt/rttrace"
)

// callExits returns the call-exit records of call.
func callExits(recs []rttrace.Record, call uint64) []rttrace.Record {
	var out []rttrace.Record
	for _, r := range recs {
		if r.Kind == rt.TraceCallExit && r.Link == call {
			out = append(out, r)
		}
	}
	return out
}

// failedCall records an ok call, then a call that fails with missing, and
// returns the failure and its call-exit.
func failedCall(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64) {
	ok := t0.TraceCall("app.Store.Get", "app.sketch:3:2")
	t0.TraceCallExit(ok, "app.Store.Get", "app.sketch:3:2", nil)
	bad := t0.TraceCall("app.Store.Get", "app.sketch:4:2")
	t0.TraceCallExit(bad, "app.Store.Get", "app.sketch:4:2", missing{})
	return missing{}, func() uint64 { return callExits(recs(), bad)[0].ID }
}

// Each reason of a compound exit links its own origin (round-2 M1): a typed
// failure its failed call-exit, each cleanup defect its own panic, an
// interruption its cancel-request. A cleanup defect is never the origin of
// the failure.
func TestTraceCompoundExitLinksEachReason(t *testing.T) {
	type want struct {
		kind  rt.Kind
		event rt.EventKind // the kind the origin names
	}
	cases := []struct {
		name string
		body func(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64)
		want []want
	}{
		{
			// The reviewer's native compound probe, through Scope.
			name: "failure-and-cleanup-defect",
			body: func(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64) {
				var bad func() uint64
				_, err := rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
					s.Defer(func() { panic("cleanup defect") })
					var e error
					e, bad = failedCall(t0, recs)
					return struct{}{}, e
				})
				return err, bad
			},
			want: []want{{rt.KindFailure, rt.TraceCallExit}, {rt.KindDefect, rt.TracePanic}},
		},
		{
			name: "two-cleanup-defects",
			body: func(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64) {
				_, err := rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
					s.Defer(func() { panic("first registered") })
					s.Defer(func() { panic(42) })
					return struct{}{}, nil
				})
				return err, nil
			},
			want: []want{{rt.KindDefect, rt.TracePanic}, {rt.KindDefect, rt.TracePanic}},
		},
		{
			name: "failure-two-cleanup-defects",
			body: func(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64) {
				var bad func() uint64
				_, err := rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
					s.Defer(func() { panic("first registered") })
					s.Defer(func() { panic("second registered") })
					var e error
					e, bad = failedCall(t0, recs)
					return struct{}{}, e
				})
				return err, bad
			},
			want: []want{{rt.KindFailure, rt.TraceCallExit}, {rt.KindDefect, rt.TracePanic}, {rt.KindDefect, rt.TracePanic}},
		},
		{
			name: "failure-and-interruption",
			body: func(t0 *rt.Task, recs func() []rttrace.Record) (error, func() uint64) {
				var bad func() uint64
				_, err := rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
					t0.Interrupt()
					var e error
					e, bad = failedCall(t0, recs)
					return struct{}{}, e
				})
				return err, bad
			},
			want: []want{{rt.KindFailure, rt.TraceCallExit}, {rt.KindInterruption, rt.TraceCancelRequest}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := rttrace.New(64)
			root := rt.TracedRoot("app.main", rec)
			var bad func() uint64
			_, err := rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
				e, b := tc.body(t0, func() []rttrace.Record { rs, _ := rec.Records(); return rs })
				bad = b
				return struct{}{}, e
			})
			c, ok := err.(*rt.Cause)
			if !ok || len(c.Reasons) != len(tc.want) {
				t.Fatalf("exit %v; want %d reasons", err, len(tc.want))
			}
			recs := records(t, rec)
			ids := byID(t, recs)
			exit, _ := find(recs, rt.TraceBodyExit, root.ID())
			if len(exit.Origins) != len(c.Reasons) || exit.Link != 0 {
				t.Fatalf("body-exit origins %v, link %d; want one origin per reason of %v", exit.Origins, exit.Link, err)
			}
			seen := map[uint64]bool{}
			for i, w := range tc.want {
				r, o := c.Reasons[i], ids[exit.Origins[i]]
				if r.Kind != w.kind || o.Kind != w.event || seen[o.ID] {
					t.Fatalf("reason %d (%v) has origin %+v; want a distinct %v", i, r.Kind, o, w.event)
				}
				seen[o.ID] = true
				switch w.event {
				case rt.TraceCallExit:
					if o.ID != bad() {
						t.Fatalf("failure origin %d; want the failed call-exit %d, not the ok call", o.ID, bad())
					}
				case rt.TracePanic:
					if o.Defect != r.Value || !o.InRelease {
						t.Fatalf("defect %v has origin %+v; want its own release panic", r.Value, o)
					}
				}
			}
		})
	}
}
