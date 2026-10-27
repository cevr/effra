package sketch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// loadFile parses and checks a testdata file and returns it with its source.
func loadFile(t *testing.T, path string) (*Checked, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(path, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	c, diags := Check(f)
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	return c, string(raw)
}

// Definition IDs come from the checked declarations: the same source gives
// the same table, and the emitted program carries them as constants, never
// computed at run time.
func TestModelIDsAreDerivedAndStable(t *testing.T) {
	c1, src := loadFile(t, "../testdata/trace/timeout.sketch")
	c2, _ := loadFile(t, "../testdata/trace/timeout.sketch")
	m1, m2 := NewModel(c1), NewModel(c2)
	if len(m1.Defs) != len(m2.Defs) || len(m1.Points) != len(m2.Points) {
		t.Fatalf("two checks of one source gave %d/%d defs and %d/%d points", len(m1.Defs), len(m2.Defs), len(m1.Points), len(m2.Points))
	}
	for i := range m1.Defs {
		if m1.Defs[i].ID != m2.Defs[i].ID || m1.Defs[i].Site != m2.Defs[i].Site {
			t.Fatalf("def %d: %+v then %+v", i, m1.Defs[i], m2.Defs[i])
		}
	}
	want := map[string]string{
		"latency.main":                     "timeout.sketch:81:1",
		"latency.roundTrip":                "timeout.sketch:31:1",
		"latency.checkout/timeout.After#1": "timeout.sketch:39:17",
		"latency.Gateway.Authorize":        "timeout.sketch:26:2",
		"latency.FakeGateway.Authorize":    "timeout.sketch:59:1",
		"latency.Live":                     "timeout.sketch:63:1",
		"latency.OrderNotFound":            "timeout.sketch:8:1",
		"timeout.Expired":                  "",
	}
	for id, site := range want {
		d, ok := m1.Def(id)
		if !ok || d.Site != site {
			t.Errorf("def %s: %+v (found %v); want site %q", id, d, ok, site)
		}
	}
	if !m1.HasPoint("call", "host.Scheduler.Sleep", "timeout.sketch:32:2") || !m1.HasPoint("handler", "timeout.Expired", "timeout.sketch:75:2") {
		t.Errorf("points %+v lack the sleep call or the Expired handler", m1.Points)
	}
	code, err := EmitWith(c1, Options{Kernel: true, Revision: Revision(src)})
	if err != nil {
		t.Fatalf("%v\n%s", err, code)
	}
	for _, s := range []string{
		`"latency.checkout/timeout.After#1"`, `rt.AfterSpan(t__,`, `rt.TracedRoot(`, `os.Getenv("ERGO_TRACE")`,
		`const revision__ = "` + Revision(src) + `"`, `t__.TraceCall(`, `t__.TraceCallExit(`, `"timeout.sketch:32:2"`,
	} {
		if !strings.Contains(code, s) {
			t.Errorf("kernel emission lacks %s", s)
		}
	}
	if strings.Contains(code, "runtime.Caller") || strings.Contains(code, "sketchgen/rt") {
		t.Errorf("kernel emission computes sites at run time or links the embedded runtime")
	}
	again, _ := EmitWith(c2, Options{Kernel: true, Revision: Revision(src)})
	if again != code {
		t.Errorf("two emissions of one source differ")
	}
}

// recorded builds a trace of src from events, numbering their seq in order.
func recorded(src string, dropped int, evs ...TraceEvent) []byte {
	h, _ := json.Marshal(TraceHeader{Schema: TraceSchema, Revision: Revision(src), Producer: "test", Capacity: 16, Events: len(evs), Dropped: dropped})
	var b strings.Builder
	b.Write(h)
	b.WriteByte('\n')
	for i, e := range evs {
		e.Seq = uint64(i)
		l, _ := json.Marshal(e)
		b.Write(l)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// The query reads the schema the kernel records now and its name before the
// #218 rename, which the recorded traces carry, names the one it read, and
// refuses any other.
func TestQueryTraceSchemaNames(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	data := string(recorded(src, 0, task(1, 0, "start", 1, 0), task(2, 0, "body-exit", 1, 0)))
	for _, schema := range []string{TraceSchema, traceSchemaEffra, "other-trace-3"} {
		ev, err := QueryTrace(m, Revision(src), []byte(strings.Replace(data, `"schema":"`+TraceSchema+`"`, `"schema":"`+schema+`"`, 1)))
		if schema == "other-trace-3" {
			if err == nil || !strings.Contains(err.Error(), TraceSchema) {
				t.Errorf("%s: got %v; want a refusal naming %s", schema, err, TraceSchema)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: refused: %v", schema, err)
			continue
		}
		if !strings.Contains(ev.Text(), "schema "+schema+")") {
			t.Errorf("%s: text does not name the schema read:\n%s", schema, ev.Text())
		}
	}
}

// Events of timeout.sketch for hand-built traces.
const (
	mainSite  = "timeout.sketch:81:1"
	blockID   = "latency.checkout/timeout.After#1"
	blockSite = "timeout.sketch:39:17"
	findID    = "latency.Orders.Find"
	findSite  = "timeout.sketch:38:15"
)

func task(id, link uint64, kind string, tk, parent uint64, cause ...TraceReason) TraceEvent {
	def, site := "latency.main", mainSite
	if tk != 1 {
		def, site = blockID, blockSite
	}
	return TraceEvent{ID: id, Link: link, Kind: kind, Task: tk, Parent: parent, Def: def, Site: site, Cause: cause}
}

func point(id, link uint64, kind string, tk, parent uint64, def, site string, cause ...TraceReason) TraceEvent {
	return TraceEvent{ID: id, Link: link, Kind: kind, Task: tk, Parent: parent, Def: def, Site: site, Cause: cause}
}

var (
	notFound    = TraceReason{Reason: "failure", Name: "OrderNotFound"}
	interrupted = TraceReason{Reason: "interrupted"}
)

// origin returns r with its recorded origin set to id.
func origin(r TraceReason, id uint64) TraceReason {
	r.Origin = id
	return r
}

// mainDefect is a string defect raised in the root task.
func mainDefect(inRelease bool) TraceReason {
	return TraceReason{Reason: "defect", Name: "string", InRelease: inRelease, Chain: []TraceFrame{{Task: 1, Def: "latency.main"}}}
}

// cleanup is the panic event of a string defect raised by a release of the
// root task.
func cleanup(id uint64) TraceEvent {
	return task(id, 0, "panic", 1, 0, TraceReason{Reason: "defect", Name: "string", InRelease: true})
}

// The query accepts the valid partial orders the runtime produces, including
// a stop requested before a task starts, and reads only recorded links.
func TestQueryTraceAcceptsValidOrders(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	cases := map[string][]TraceEvent{
		"minimal": {task(1, 0, "start", 1, 0), task(2, 0, "body-exit", 1, 0)},
		"handled call failure": {
			task(1, 0, "start", 1, 0),
			point(2, 0, "call", 1, 0, findID, findSite),
			point(3, 2, "call-exit", 1, 0, findID, findSite, notFound),
			point(4, 3, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"),
			task(5, 0, "body-exit", 1, 0),
		},
		// TracedRoot, Interrupt, Run: the request precedes the root's start.
		"root cancelled before start": {
			task(1, 0, "cancel-request", 1, 0), task(2, 0, "start", 1, 0), task(3, 0, "body-exit", 1, 0, interrupted),
		},
		// A deadline and its stop can reach a child before its goroutine runs.
		"child stopped before start": {
			task(1, 0, "start", 1, 0),
			task(2, 0, "deadline", 2, 1),
			task(3, 0, "cancel-request", 2, 1),
			task(4, 0, "start", 2, 1),
			task(5, 0, "body-exit", 2, 1, interrupted),
			task(6, 5, "observe", 2, 1),
			point(7, 2, "handler", 1, 0, "timeout.Expired", "timeout.sketch:75:2"),
			task(8, 0, "body-exit", 1, 0),
		},
	}
	for name, evs := range cases {
		if _, err := QueryTrace(m, Revision(src), recorded(src, 0, evs...)); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	// A cut-short trace names the ancestry it cannot show instead of refusing.
	ev, err := QueryTrace(m, Revision(src), recorded(src, 3, task(1, 0, "start", 1, 0), task(2, 0, "cancel-request", 2, 1)))
	if err != nil {
		t.Fatalf("a trace with dropped events was refused: %v", err)
	}
	if !slices.ContainsFunc(ev.Omissions, func(o TraceOmission) bool { return o.Kind == "ancestryUnavailable" && o.Task == 2 }) {
		t.Errorf("omissions %+v; want ancestryUnavailable for task #2", ev.Omissions)
	}
	// A link to a dropped occurrence is named, not followed and not guessed.
	ev, err = QueryTrace(m, Revision(src), recorded(src, 2,
		task(1, 0, "start", 1, 0),
		point(5, 3, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"),
		task(6, 0, "body-exit", 1, 0),
	))
	if err != nil {
		t.Fatalf("a trace with a dropped link target was refused: %v", err)
	}
	if want := "    caught occurrence 3, which the recorder dropped (see omissions)"; !slices.Contains(ev.CauseTrace, want) {
		t.Errorf("cause trace %q lacks %q", ev.CauseTrace, want)
	}
	if !slices.ContainsFunc(ev.Omissions, func(o TraceOmission) bool { return o.Kind == "unresolvedLink" }) {
		t.Errorf("omissions %+v; want unresolvedLink", ev.Omissions)
	}
}

// The query refuses what it cannot check rather than guess, and says why.
func TestQueryTraceRefuses(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	start, exit := task(1, 0, "start", 1, 0), task(2, 0, "body-exit", 1, 0)
	call := point(2, 0, "call", 1, 0, findID, findSite)
	failed := point(3, 2, "call-exit", 1, 0, findID, findSite, notFound)
	handler := point(4, 3, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2")
	defect := func(frames ...TraceFrame) TraceEvent {
		return task(2, 0, "body-exit", 1, 0, TraceReason{Reason: "defect", Name: "string", Chain: frames})
	}
	child := task(2, 0, "start", 2, 1)
	cases := []struct {
		name, want string
		trace      []byte
	}{
		{"no header", "header", []byte(strings.SplitN(string(recorded(src, 0, start, exit)), "\n", 2)[1])},
		{"other revision", "does not match", []byte(strings.Replace(string(recorded(src, 0, start, exit)), Revision(src), Revision("other"), 1))},
		{"truncated", "truncated", []byte(strings.Replace(string(recorded(src, 0, start, exit)), `"events":2`, `"events":3`, 1))},
		{"unknown def", "latency.ghost", recorded(src, 0, TraceEvent{ID: 1, Kind: "start", Task: 1, Def: "latency.ghost", Site: mainSite})},
		{"wrong task site", "declared at", recorded(src, 0, TraceEvent{ID: 1, Kind: "start", Task: 1, Def: "latency.main", Site: "timeout.sketch:80:1"})},
		{"point not in model", "the model has no call", recorded(src, 0, start, point(2, 0, "call", 1, 0, "latency.Gateway.Authorize", "timeout.sketch:1:1"))},
		{"task kind on a non-task", "not a task definition", recorded(src, 0, TraceEvent{ID: 1, Kind: "start", Task: 1, Def: "latency.roundTrip", Site: "timeout.sketch:31:1"})},
		{"orphan parent", "never started", recorded(src, 0, start, task(2, 0, "start", 2, 9))},
		{"no occurrence id", "no occurrence id", recorded(src, 0, task(0, 0, "start", 1, 0))},
		{"repeated id", "used twice", recorded(src, 0, start, task(1, 0, "body-exit", 1, 0))},
		{"two roots", "root tasks", recorded(src, 0, start, task(2, 0, "start", 2, 0))},
		{"exits twice", "exits twice", recorded(src, 0, start, exit, task(3, 0, "body-exit", 1, 0))},
		{"event before start", "precedes the task's start", recorded(src, 0, point(1, 0, "construct", 1, 0, "latency.Live", "timeout.sketch:70:13"), task(2, 0, "start", 1, 0))},
		{"child before parent", "starts before its parent", recorded(src, 0, task(1, 0, "start", 2, 1), task(2, 0, "start", 1, 0))},
		{"observe before exit", "precedes its body-exit", recorded(src, 0, start, child, task(4, 0, "body-exit", 2, 1), task(3, 4, "observe", 2, 1))},
		{"observe without exit", "no body-exit", recorded(src, 0, start, child, task(3, 0, "observe", 2, 1))},
		// The reviewer's cycle probe (M2): a body-exit naming its own task as
		// parent, against a start with none. It once overflowed the stack.
		{"exit parent differs from start", "has parent #1 here but #0 at its start", recorded(src, 0, start, task(2, 0, "body-exit", 1, 1, notFound))},
		{"task event under another definition", "started as", recorded(src, 0, start, TraceEvent{ID: 2, Kind: "body-exit", Task: 1, Def: blockID, Site: blockSite})},
		{"call-exit without its call", "links no call", recorded(src, 0, start, point(2, 0, "call-exit", 1, 0, findID, findSite))},
		{"link to a missing occurrence", "not in the trace", recorded(src, 0, start, call, failed, point(50, 30, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"))},
		{"link to a later occurrence", "raised after it", recorded(src, 0, start, call, point(3, 4, "call-exit", 1, 0, findID, findSite, notFound), point(4, 0, "call", 1, 0, findID, findSite))},
		{"handler of another type", "cannot refer to", recorded(src, 0, start, call, failed, point(4, 3, "handler", 1, 0, "latency.GatewayDown", "timeout.sketch:73:2"))},
		{"handler of an ok call", "cannot refer to", recorded(src, 0, start, call, point(3, 2, "call-exit", 1, 0, findID, findSite), point(4, 3, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"))},
		{"two handlers, one failure", "caught by two handlers", recorded(src, 0, start, call, failed, handler, point(5, 3, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"))},
		{"body-exit with a link", "a body-exit carries no link", recorded(src, 0, start, call, failed, handler, task(5, 3, "body-exit", 1, 0))},
		// Round-2 M1: each reason's origin must be able to raise that reason.
		{"failure origin is a cleanup panic", "cannot raise it", recorded(src, 0, start, call, failed, cleanup(4), task(5, 0, "body-exit", 1, 0, origin(notFound, 4), origin(mainDefect(true), 0)))},
		{"failure origin is an ok call", "cannot raise it", recorded(src, 0, start, call, point(3, 2, "call-exit", 1, 0, findID, findSite), task(4, 0, "body-exit", 1, 0, origin(notFound, 3)))},
		{"defect origin of another type", "cannot raise it", recorded(src, 0, start, task(2, 0, "panic", 1, 0, TraceReason{Reason: "defect", Name: "int"}), task(3, 0, "body-exit", 1, 0, origin(mainDefect(false), 2)))},
		{"defect origin is a call-exit", "cannot raise it", recorded(src, 0, start, call, failed, task(4, 0, "body-exit", 1, 0, origin(mainDefect(false), 3)))},
		{"interruption origin of another task", "cannot raise it", recorded(src, 0, start, child, task(3, 0, "cancel-request", 2, 1), task(4, 0, "body-exit", 1, 0, origin(interrupted, 3)))},
		{"two reasons, one origin", "the same origin", recorded(src, 0, start, call, failed, task(4, 0, "body-exit", 1, 0, origin(notFound, 3), origin(notFound, 3)))},
		{"origin on a call-exit reason", "only a body-exit's reasons", recorded(src, 0, start, call, point(3, 2, "call-exit", 1, 0, findID, findSite, origin(notFound, 2)))},
		{"origin raised after the exit", "raised after it", recorded(src, 0, start, call, task(3, 0, "body-exit", 1, 0, origin(notFound, 4)), point(4, 2, "call-exit", 1, 0, findID, findSite, notFound))},
		{"origin not in the trace", "not in the trace", recorded(src, 0, start, task(3, 0, "body-exit", 1, 0, origin(notFound, 2)))},
		// Round-3 M1: an origin is an occurrence. One a handler consumed, or a
		// child exit the owner never observed, is not the origin of an exit reason.
		{"origin a handler consumed", "consumed", recorded(src, 0, start, call, failed, handler, task(5, 0, "body-exit", 1, 0, origin(notFound, 3)))},
		{"origin an unobserved child exit", "cannot raise it", recorded(src, 0, start, child, task(3, 0, "body-exit", 2, 1, notFound), task(4, 0, "body-exit", 1, 0, origin(notFound, 3)))},
		{"link on a kind without links", "carries no link", recorded(src, 0, start, point(2, 1, "construct", 1, 0, "latency.Live", "timeout.sketch:70:13"))},
		{"unknown failure type", "not an error type", recorded(src, 0, start, call, point(3, 2, "call-exit", 1, 0, findID, findSite, TraceReason{Reason: "failure", Name: "Ghost"}))},
		// M3: a valid outer event with an invalid nested frame.
		{"unknown frame definition", "unknown.made.up", recorded(src, 0, start, defect(TraceFrame{Task: 1, Def: "unknown.made.up"}))},
		{"frame of a non-task", "not a task definition", recorded(src, 0, start, defect(TraceFrame{Task: 1, Def: "latency.roundTrip"}))},
		{"frame under another definition", "started as", recorded(src, 0, start, defect(TraceFrame{Task: 1, Def: blockID}))},
		{"frame of an unknown task", "never started", recorded(src, 0, start, defect(TraceFrame{Task: 7, Def: blockID}, TraceFrame{Task: 1, Def: "latency.main"}))},
		{"frames out of order", "is not the parent", recorded(src, 0, start, child, task(3, 0, "body-exit", 2, 1), task(4, 0, "body-exit", 1, 0, TraceReason{Reason: "defect", Name: "string", Chain: []TraceFrame{{Task: 1, Def: "latency.main"}, {Task: 2, Def: blockID}}}))},
		{"chain short of the root", "leaves out", recorded(src, 0, start, child, task(3, 0, "body-exit", 2, 1, TraceReason{Reason: "defect", Name: "string", Chain: []TraceFrame{{Task: 2, Def: blockID}}}))},
		{"chain not through the task", "does not pass through", recorded(src, 0, start, child, task(3, 0, "body-exit", 2, 1, TraceReason{Reason: "defect", Name: "string", Chain: []TraceFrame{{Task: 1, Def: "latency.main"}}}))},
	}
	for _, tc := range cases {
		_, err := QueryTrace(m, Revision(src), tc.trace)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v; want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}

// The reviewer's probes (M1) against their recorded traces: the handler of
// the second of two calls links that call's exit and nothing about the
// first, and each of two timeouts' handlers links its own deadline.
func TestQueryTraceStatesOnlyRecordedLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"two-calls", []string{
			"task #1 calls.main (func, two-calls.sketch:33:1) exited with success",
			"  handled calls.Missing at two-calls.sketch:35:2 in calls.main (task #1, seq 6)",
			"    caught the failure calls.Missing of call calls.Store.Get at two-calls.sketch:29:16 in calls.load, whose row is error[calls.Missing] (call seq 4, call-exit seq 5)",
		}},
		{"two-timeouts", []string{
			"task #1 deadlines.main (func, two-timeouts.sketch:23:1) exited with success",
			"  handled timeout.Expired at two-timeouts.sketch:25:2 in deadlines.main (task #1, seq 10)",
			"    caught the deadline of task #2 deadlines.stall/timeout.After#1 (seq 4): the timeout block timeout.After(1ms) at two-timeouts.sketch:10:15 in deadlines.stall, row error[timeout.Expired]",
			"      call host.Scheduler.Sleep at two-timeouts.sketch:15:2 was open when the deadline passed (call seq 3); it ended interrupted (call-exit seq 6)",
			"      cancel-request of task #2 (seq 5)",
			"      task #2 observed the stop in deadlines.block at two-timeouts.sketch:15:2 (seq 7)",
			"      task #2 exited with interrupted (seq 8)",
			"  handled timeout.Expired at two-timeouts.sketch:30:2 in deadlines.main (task #1, seq 22)",
			"    caught the deadline of task #3 deadlines.stall/timeout.After#1 (seq 16): the timeout block timeout.After(1ms) at two-timeouts.sketch:10:15 in deadlines.stall, row error[timeout.Expired]",
			"      call host.Scheduler.Sleep at two-timeouts.sketch:15:2 was open when the deadline passed (call seq 15); it ended interrupted (call-exit seq 18)",
			"      cancel-request of task #3 (seq 17)",
			"      task #3 observed the stop in deadlines.block at two-timeouts.sketch:15:2 (seq 19)",
			"      task #3 exited with interrupted (seq 20)",
		}},
	} {
		c, src := loadFile(t, "../testdata/trace/"+tc.name+".sketch")
		data, err := os.ReadFile("../testdata/trace/" + tc.name + ".jsonl")
		if err != nil {
			t.Fatal(err)
		}
		ev, err := QueryTrace(NewModel(c), Revision(src), data)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(ev.CauseTrace, tc.want) {
			t.Errorf("%s: cause trace\n%s\nwant\n%s", tc.name, strings.Join(ev.CauseTrace, "\n"), strings.Join(tc.want, "\n"))
		}
		for _, s := range ev.Calls {
			if s.Exit == 0 || s.DurationNs <= 0 {
				t.Errorf("%s: call %+v has no recorded span", tc.name, s)
			}
		}
	}
}

// Each reason of a compound exit is explained from its own origin (round-2
// M1): the failure from its failed call-exit, each cleanup defect from its own
// panic, the interruption from its cancel-request. A reason without an origin
// is "origin not recorded", even when another reason of the exit has one.
func TestQueryTraceExplainsEachReason(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	start := task(1, 0, "start", 1, 0)
	call := point(2, 0, "call", 1, 0, findID, findSite)
	failed := point(3, 2, "call-exit", 1, 0, findID, findSite, notFound)
	failedLine := "    carried the failure latency.OrderNotFound of call latency.Orders.Find at timeout.sketch:38:15 in latency.checkout, whose row is error[latency.OrderNotFound] (call seq 1, call-exit seq 2)"
	for _, tc := range []struct {
		name string
		evs  []TraceEvent
		want []string
	}{
		{"failure and two cleanup defects", []TraceEvent{start, call, failed, cleanup(4), cleanup(5),
			task(6, 0, "body-exit", 1, 0, origin(notFound, 3), origin(mainDefect(true), 4), origin(mainDefect(true), 5))}, []string{
			"task #1 latency.main (func, timeout.sketch:81:1) exited with failure latency.OrderNotFound, defect string, defect string",
			"  failure latency.OrderNotFound",
			failedLine,
			"  defect string in a release, chain:",
			"    task #1 latency.main at timeout.sketch:81:1",
			"    raised by the panic defect string in a release in task #1 (seq 3)",
			"  defect string in a release, chain:",
			"    task #1 latency.main at timeout.sketch:81:1",
			"    raised by the panic defect string in a release in task #1 (seq 4)",
		}},
		// The reviewer's compound case with the failure's origin unrecorded:
		// the cleanup defect's panic is never offered as its origin.
		{"failure without origin beside a cleanup defect", []TraceEvent{start, call, failed, cleanup(4),
			task(5, 0, "body-exit", 1, 0, notFound, origin(mainDefect(true), 4))}, []string{
			"task #1 latency.main (func, timeout.sketch:81:1) exited with failure latency.OrderNotFound, defect string",
			"  failure latency.OrderNotFound",
			"    origin not recorded: no event links the occurrence of latency.OrderNotFound",
			"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 2)",
			"  defect string in a release, chain:",
			"    task #1 latency.main at timeout.sketch:81:1",
			"    raised by the panic defect string in a release in task #1 (seq 3)",
		}},
		// Round-3 M1: two failures of one type without origins (the runtime
		// could not tell them apart). Each reason states the shared type and
		// lists both occurrences as candidates by type; neither gets the
		// other's origin, and the answer does not call the values equal,
		// because the recording holds only their type (round-4 m1).
		{"two failures of one type without origins", []TraceEvent{start, call, failed, point(4, 0, "call", 1, 0, findID, findSite), point(5, 4, "call-exit", 1, 0, findID, findSite, notFound),
			task(6, 0, "body-exit", 1, 0, notFound, notFound)}, []string{
			"task #1 latency.main (func, timeout.sketch:81:1) exited with failure latency.OrderNotFound, failure latency.OrderNotFound",
			"  failure latency.OrderNotFound",
			"    origin not recorded: 2 reasons of this exit have the same error type latency.OrderNotFound, and no event links this one",
			"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 2)",
			"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 4)",
			"  failure latency.OrderNotFound",
			"    origin not recorded: 2 reasons of this exit have the same error type latency.OrderNotFound, and no event links this one",
			"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 2)",
			"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 4)",
		}},
		// Round-4 m1: two failures of one type that no traced point raised (in
		// the reviewer's probe, Missing{"first"} and Missing{"second"}). The
		// recording cannot tell equal values from distinct ones, so the
		// answer is the shared type, no origin and no candidate.
		{"two failures of one type that no event raised", []TraceEvent{start,
			task(2, 0, "body-exit", 1, 0, notFound, notFound)}, []string{
			"task #1 latency.main (func, timeout.sketch:81:1) exited with failure latency.OrderNotFound, failure latency.OrderNotFound",
			"  failure latency.OrderNotFound",
			"    origin not recorded: 2 reasons of this exit have the same error type latency.OrderNotFound, and no event links this one",
			"      no candidate in the trace: source code that returns the failure itself records no event",
			"  failure latency.OrderNotFound",
			"    origin not recorded: 2 reasons of this exit have the same error type latency.OrderNotFound, and no event links this one",
			"      no candidate in the trace: source code that returns the failure itself records no event",
		}},
		{"failure and interruption", []TraceEvent{start, task(2, 0, "cancel-request", 1, 0), point(3, 0, "call", 1, 0, findID, findSite), point(4, 3, "call-exit", 1, 0, findID, findSite, notFound),
			task(5, 0, "body-exit", 1, 0, origin(notFound, 4), origin(interrupted, 2))}, []string{
			"task #1 latency.main (func, timeout.sketch:81:1) exited with failure latency.OrderNotFound, interrupted",
			"  failure latency.OrderNotFound",
			"    carried the failure latency.OrderNotFound of call latency.Orders.Find at timeout.sketch:38:15 in latency.checkout, whose row is error[latency.OrderNotFound] (call seq 2, call-exit seq 3)",
			"  interrupted",
			"    requested by the cancel-request of task #1 (seq 1)",
			"    cancel-request of task #1 (seq 1)",
			"    task #1 exited with failure latency.OrderNotFound, interrupted (seq 4)",
		}},
	} {
		ev, err := QueryTrace(m, Revision(src), recorded(src, 0, tc.evs...))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(ev.CauseTrace, tc.want) {
			t.Errorf("%s: cause trace\n%s\nwant\n%s", tc.name, strings.Join(ev.CauseTrace, "\n"), strings.Join(tc.want, "\n"))
		}
	}
}

// Without a recorded link the query says so and labels each candidate; it
// never states an origin.
func TestQueryTraceLabelsCandidates(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	ev, err := QueryTrace(m, Revision(src), recorded(src, 0,
		task(1, 0, "start", 1, 0),
		point(2, 0, "call", 1, 0, findID, findSite),
		point(3, 2, "call-exit", 1, 0, findID, findSite, notFound),
		point(4, 0, "handler", 1, 0, "latency.OrderNotFound", "timeout.sketch:71:2"),
		task(5, 0, "body-exit", 1, 0),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"task #1 latency.main (func, timeout.sketch:81:1) exited with success",
		"  handled latency.OrderNotFound at timeout.sketch:71:2 in latency.report (task #1, seq 3)",
		"    origin not recorded: no event links the occurrence of latency.OrderNotFound",
		"      candidate by type only: call latency.Orders.Find at timeout.sketch:38:15 ended failure latency.OrderNotFound (call-exit seq 2)",
	}
	if !slices.Equal(ev.CauseTrace, want) {
		t.Errorf("cause trace\n%s\nwant\n%s", strings.Join(ev.CauseTrace, "\n"), strings.Join(want, "\n"))
	}
}

// kernelRoot finds the checkout that holds runtime/rt above the test. It
// skips only when there is no checkout at all (sketch9 copied elsewhere).
// Inside a checkout whose module is not KernelModule it fails: a skip there
// hid the #218 rename, when every kernel test skipped silently.
func kernelRoot(t *testing.T) string {
	t.Helper()
	root, mod, ok := FindCheckout(".")
	if !ok {
		t.Skip("no checkout of " + KernelModule + " above the test")
	}
	if mod != KernelModule {
		t.Fatalf("the checkout at %s declares module %s, but kernel mode links %s (sketch.KernelModule); update KernelModule", root, mod, KernelModule)
	}
	return root
}

// The lookup finds the nearest checkout by its module's shape or its
// runtime/rt, passes over sketch9's own go.mod, and reports the module the
// checkout declares, so that a renamed root module is a mismatch and not a
// missing checkout.
func TestKernelCheckoutLookup(t *testing.T) {
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		files map[string]string
		dirs  []string
		want  string // module found; "" for none
	}{
		{"kernel module", map[string]string{"go.mod": "module " + KernelModule + "\n"}, nil, KernelModule},
		{"renamed module", map[string]string{"go.mod": "// root\nmodule other.local/prototype\n"}, nil, "other.local/prototype"},
		{"runtime/rt beside go.mod", map[string]string{"go.mod": "module example.com/x\n"}, []string{"runtime/rt"}, "example.com/x"},
		{"unrelated module", map[string]string{"go.mod": "module example.com/x\n"}, nil, ""},
		{"no go.mod", nil, []string{"runtime/rt"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top := t.TempDir()
			for name, body := range tc.files {
				write(filepath.Join(top, name), body)
			}
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(top, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			start := filepath.Join(top, "prototypes", "sketch9", "sketch")
			write(filepath.Join(top, "prototypes", "sketch9", "go.mod"), "module effra.local/sketch9\n")
			if err := os.MkdirAll(start, 0o755); err != nil {
				t.Fatal(err)
			}
			root, mod, ok := FindCheckout(start)
			if tc.want == "" {
				// t.TempDir can sit under a checkout, so only a find at or
				// below top is wrong.
				if ok && strings.HasPrefix(root, top) {
					t.Fatalf("found %s (%s); want none at or below %s", root, mod, top)
				}
				return
			}
			if !ok || root != top || mod != tc.want {
				t.Fatalf("FindCheckout = %q, %q, %v; want %q, %q, true", root, mod, ok, top, tc.want)
			}
		})
	}
}

// The timeout example built in kernel mode records nothing by default, and a
// recorded run answers "why did this time out" with the deadline and sites.
func TestKernelTimeoutTrace(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	root := kernelRoot(t)
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	code, err := EmitWith(c, Options{Kernel: true, Revision: Revision(src)})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": KernelGoMod(root), "main.go": code} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "timeout.bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = t.TempDir()
	run.Env = append(os.Environ(), "ERGO_TRACE=")
	out, err := run.Output()
	if err != nil || string(out) != "gateway timed out\n" {
		t.Fatalf("untraced run: %q, %v", out, err)
	}
	if entries, _ := os.ReadDir(run.Dir); len(entries) != 0 {
		t.Fatalf("untraced run wrote %d files", len(entries))
	}
	// The variable is named literally, not through TraceEnv: it is the
	// documented contract (README), and a program that reads another name
	// would turn tracing off without a word.
	tracePath := filepath.Join(dir, "timeout.jsonl")
	traced := exec.Command(bin)
	traced.Env = append(os.Environ(), "ERGO_TRACE="+tracePath)
	if out, err := traced.Output(); err != nil || string(out) != "gateway timed out\n" {
		t.Fatalf("traced run: %q, %v", out, err)
	}
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("a run with ERGO_TRACE set recorded no trace: %v", err)
	}
	var header TraceHeader
	if line, _, _ := strings.Cut(string(data), "\n"); json.Unmarshal([]byte(line), &header) != nil || header.Schema != TraceSchema || header.Events == 0 {
		t.Fatalf("a run with ERGO_TRACE set recorded header %+v; want schema %s and events", header, TraceSchema)
	}
	ev, err := QueryTrace(NewModel(c), Revision(src), data)
	if err != nil {
		t.Fatal(err)
	}
	text := ev.Text()
	for _, s := range []string{"deadline of task #2 latency.checkout/timeout.After#1", "timeout.After(100ms)", "observed the stop in latency.roundTrip at timeout.sketch:32:2", "handled timeout.Expired at timeout.sketch:75:2"} {
		if !strings.Contains(text, s) {
			t.Errorf("answer lacks %q:\n%s", s, text)
		}
	}
	if ev.Schema != TraceEvidenceSchema || ev.Fingerprint == "" || len(ev.Omissions) == 0 {
		t.Errorf("envelope %s %s %v", ev.Schema, ev.Fingerprint, ev.Omissions)
	}
}

// The reviewer's probes (M1), built and run live: each handler links the
// occurrence it caught. Arrival numbers vary from run to run, so they are
// masked, and only the handler lines and what they caught are compared: the
// facts under a deadline depend on whether the child reached its sleep.
func TestKernelProbeTraces(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	root := kernelRoot(t)
	seq := regexp.MustCompile(`seq \d+`)
	for _, tc := range []struct {
		name, out string
		want      []string
	}{
		{"two-calls", "missing\n", []string{
			"task #1 calls.main (func, two-calls.sketch:33:1) exited with success",
			"  handled calls.Missing at two-calls.sketch:35:2 in calls.main (task #1, seq N)",
			"    caught the failure calls.Missing of call calls.Store.Get at two-calls.sketch:29:16 in calls.load, whose row is error[calls.Missing] (call seq N, call-exit seq N)",
		}},
		{"two-timeouts", "first timed out\nsecond timed out\n", []string{
			"task #1 deadlines.main (func, two-timeouts.sketch:23:1) exited with success",
			"  handled timeout.Expired at two-timeouts.sketch:25:2 in deadlines.main (task #1, seq N)",
			"    caught the deadline of task #2 deadlines.stall/timeout.After#1 (seq N): the timeout block timeout.After(1ms) at two-timeouts.sketch:10:15 in deadlines.stall, row error[timeout.Expired]",
			"  handled timeout.Expired at two-timeouts.sketch:30:2 in deadlines.main (task #1, seq N)",
			"    caught the deadline of task #3 deadlines.stall/timeout.After#1 (seq N): the timeout block timeout.After(1ms) at two-timeouts.sketch:10:15 in deadlines.stall, row error[timeout.Expired]",
		}},
	} {
		c, src := loadFile(t, "../testdata/trace/"+tc.name+".sketch")
		code, err := EmitWith(c, Options{Kernel: true, Revision: Revision(src)})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for name, body := range map[string]string{"go.mod": KernelGoMod(root), "main.go": code} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		bin := filepath.Join(dir, tc.name+".bin")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("%s: go build: %v\n%s", tc.name, err, out)
		}
		tracePath := filepath.Join(dir, tc.name+".jsonl")
		run := exec.Command(bin)
		run.Env = append(os.Environ(), "ERGO_TRACE="+tracePath)
		if out, err := run.Output(); err != nil || string(out) != tc.out {
			t.Fatalf("%s: traced run: %q, %v", tc.name, out, err)
		}
		data, err := os.ReadFile(tracePath)
		if err != nil {
			t.Fatal(err)
		}
		ev, err := QueryTrace(NewModel(c), Revision(src), data)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got []string
		for _, l := range ev.CauseTrace {
			if len(l)-len(strings.TrimLeft(l, " ")) <= 4 {
				got = append(got, seq.ReplaceAllString(l, "seq N"))
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: cause trace\n%s\nwant\n%s", tc.name, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
		}
	}
}
