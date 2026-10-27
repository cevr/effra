package sketch

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Trace query: read a recorded run against the checked model of the file it
// came from. This is the prototype of `ef trace` and MCP `code.trace`
// (docs/research/traceability.md, Query surface).

const (
	// TraceSchema is the JSON Lines format a kernel program records
	// (runtime/rt/rttrace).
	TraceSchema = "effra-trace-1"
	// TraceEvidenceSchema is the envelope a query answers with. Its envelope
	// fields are those of the review evidence schema (#156): schema,
	// producer, revision, budgets, omissions and fingerprint.
	TraceEvidenceSchema = "trace-evidence-1"
	// TraceProducer names this query.
	TraceProducer = "sketchc trace (effra.local/sketch9)"
	// TraceByteBudget is the largest trace a query reads.
	TraceByteBudget = 8 << 20
)

// taskKinds are the events the runtime raises for a task, keyed by the task's
// definition; pointKinds are raised by emitted code at a point of the model.
var (
	taskKinds  = []string{"start", "body-exit", "cancel-request", "observe", "deadline", "release", "panic"}
	pointKinds = []string{"call", "interrupt", "construct", "handler"}
)

// TraceHeader is the first line of a recorded trace.
type TraceHeader struct {
	Schema   string `json:"schema"`
	Revision string `json:"revision"`
	Producer string `json:"producer"`
	Capacity int    `json:"capacity"`
	Events   int    `json:"events"`
	Dropped  int    `json:"dropped"`
	InFlight int    `json:"inFlight,omitempty"`
}

// TraceReason is one reason of an exit, by type name.
type TraceReason struct {
	Reason    string       `json:"reason"`
	Name      string       `json:"name,omitempty"`
	Def       string       `json:"def,omitempty"` // the failure's definition ID, resolved by the query
	InRelease bool         `json:"inRelease,omitempty"`
	Chain     []TraceFrame `json:"chain,omitempty"`
}

// TraceFrame is one task of a defect chain.
type TraceFrame struct {
	Task uint64 `json:"task"`
	Def  string `json:"def"`
}

// TraceEvent is one recorded event, resolved against the model.
type TraceEvent struct {
	Seq     uint64        `json:"seq"`
	Time    int64         `json:"time"`
	Kind    string        `json:"kind"`
	Task    uint64        `json:"task"`
	Parent  uint64        `json:"parent"`
	Def     string        `json:"def"`
	Site    string        `json:"site"`
	Cause   []TraceReason `json:"cause,omitempty"`
	DefKind string        `json:"defKind"`      // resolved: the kind of Def
	In      string        `json:"in,omitempty"` // resolved: the definition enclosing Site
}

// TraceTask is one task of the run.
type TraceTask struct {
	Task    uint64        `json:"task"`
	Parent  uint64        `json:"parent"`
	Def     string        `json:"def"`
	DefKind string        `json:"defKind"`
	Site    string        `json:"site"`
	Exited  bool          `json:"exited"`
	Exit    []TraceReason `json:"exit"`
}

// TraceBudgets are the bounds the recording and the query ran under.
type TraceBudgets struct {
	Events     int `json:"events"`     // the recorder's capacity
	Bytes      int `json:"bytes"`      // the query's byte budget
	KeptEvents int `json:"keptEvents"` // events in the trace
	TraceBytes int `json:"traceBytes"` // size of the trace read
}

// TraceOmission names a gap the answer does not fill.
type TraceOmission struct {
	Kind   string `json:"kind"`
	Count  int    `json:"count,omitempty"`
	Task   uint64 `json:"task,omitempty"`
	Detail string `json:"detail"`
}

// TraceEvidence is the answer of a trace query.
type TraceEvidence struct {
	Schema      string          `json:"schema"`
	Producer    string          `json:"producer"`
	Revision    string          `json:"revision"`
	Source      string          `json:"source"`
	Recorder    string          `json:"recorder"`
	Order       string          `json:"order"`
	Budgets     TraceBudgets    `json:"budgets"`
	Omissions   []TraceOmission `json:"omissions"`
	Fingerprint string          `json:"fingerprint"`
	Root        *TraceTask      `json:"root"`
	Tasks       []TraceTask     `json:"tasks"`
	Events      []TraceEvent    `json:"events"`
	CauseTrace  []string        `json:"causeTrace"`
}

// QueryTrace reads data, a recorded trace, against m, the model of the
// source whose revision is revision. It refuses a trace whose revision,
// definition IDs or sites do not match the model, rather than guess.
func QueryTrace(m *Model, revision string, data []byte) (*TraceEvidence, error) {
	if len(data) > TraceByteBudget {
		return nil, fmt.Errorf("trace is %d bytes, over the %d-byte budget", len(data), TraceByteBudget)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), TraceByteBudget)
	var h TraceHeader
	var events []TraceEvent
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		if h.Schema == "" {
			if err := json.Unmarshal(raw, &h); err != nil || h.Schema == "" {
				return nil, fmt.Errorf("line %d: want a header with schema %s first", line, TraceSchema)
			}
			continue
		}
		var ev TraceEvent
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Kind == "" {
			return nil, fmt.Errorf("line %d: not a trace event", line)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	switch {
	case h.Schema == "":
		return nil, fmt.Errorf("trace has no header")
	case h.Schema != TraceSchema:
		return nil, fmt.Errorf("trace schema %q; this query reads %s", h.Schema, TraceSchema)
	case h.Revision != revision:
		return nil, fmt.Errorf("trace revision %s does not match %s revision %s: the trace was recorded from another program or another version of this one", short(h.Revision), m.File, short(revision))
	case h.Events != len(events):
		return nil, fmt.Errorf("trace header counts %d events but the trace holds %d: it is truncated or edited", h.Events, len(events))
	}

	ev := &TraceEvidence{
		Schema:   TraceEvidenceSchema,
		Producer: TraceProducer,
		Revision: revision,
		Source:   m.File,
		Recorder: h.Producer,
		Order:    "partial: seq is the recorder's arrival order, not a happens-before order",
		Budgets:  TraceBudgets{Events: h.Capacity, Bytes: TraceByteBudget, KeptEvents: len(events), TraceBytes: len(data)},
		Omissions: []TraceOmission{
			{Kind: "values", Detail: "argument, result and field values are not recorded (privacy default); failures and defects are named by type"},
			{Kind: "functionCalls", Detail: "direct calls between functions are not events; each site resolves to its enclosing definition"},
		},
	}
	if h.Dropped > 0 {
		ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "dropped", Count: h.Dropped, Detail: "the recorder was full; every event after the last kept one is missing"})
	}
	if h.InFlight > 0 {
		ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "inFlight", Count: h.InFlight, Detail: "events still being written when the trace was saved"})
	}

	tasks := map[uint64]*TraceTask{}
	var order []uint64
	for i := range events {
		e := &events[i]
		d, ok := m.Def(e.Def)
		if !ok {
			return nil, fmt.Errorf("event seq %d: definition %q is not in %s at this revision", e.Seq, e.Def, m.File)
		}
		e.DefKind = d.Kind
		switch {
		case slices.Contains(taskKinds, e.Kind):
			if d.Kind != DefTimeout && e.Def != m.id("main") {
				return nil, fmt.Errorf("event seq %d: %s of %s, which is not a task definition", e.Seq, e.Kind, e.Def)
			}
			if e.Site != d.Site {
				return nil, fmt.Errorf("event seq %d: site %s, but %s is declared at %s", e.Seq, e.Site, e.Def, d.Site)
			}
			e.In = d.In
		case slices.Contains(pointKinds, e.Kind):
			p, ok := m.PointAt(e.Kind, e.Def, e.Site)
			if !ok {
				return nil, fmt.Errorf("event seq %d: the model has no %s of %s at %s", e.Seq, e.Kind, e.Def, e.Site)
			}
			e.In = p.In
		default:
			return nil, fmt.Errorf("event seq %d: unknown kind %q", e.Seq, e.Kind)
		}
		for j := range e.Cause {
			e.Cause[j].Def = m.failureDef(e.Cause[j])
		}
		switch e.Kind {
		case "start":
			if _, dup := tasks[e.Task]; dup {
				return nil, fmt.Errorf("event seq %d: task #%d starts twice", e.Seq, e.Task)
			}
			tasks[e.Task] = &TraceTask{Task: e.Task, Parent: e.Parent, Def: e.Def, DefKind: d.Kind, Site: d.Site}
			order = append(order, e.Task)
		case "body-exit":
			if t := tasks[e.Task]; t != nil {
				t.Exited, t.Exit = true, e.Cause
			}
		}
	}
	for _, id := range order {
		t := tasks[id]
		if t.Parent != 0 && tasks[t.Parent] == nil && h.Dropped == 0 {
			return nil, fmt.Errorf("task #%d names parent #%d, which never started in this trace", t.Task, t.Parent)
		}
		if t.Parent == 0 && ev.Root == nil {
			ev.Root = t
		}
		if !t.Exited {
			ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "missingExit", Task: t.Task, Detail: "the task started but its body-exit is not in the trace"})
		}
		ev.Tasks = append(ev.Tasks, *t)
	}
	if ev.Root == nil {
		return nil, fmt.Errorf("trace has no root task start")
	}
	root := *ev.Root
	ev.Root = &root
	ev.Events = events
	ev.CauseTrace = causeTrace(m, ev)
	ev.Fingerprint = ""
	canon, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canon)
	ev.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
	return ev, nil
}

// failureDef resolves the type name of a failure to its definition ID.
func (m *Model) failureDef(r TraceReason) string {
	if r.Reason != "failure" {
		return ""
	}
	if id := m.ErrorID(r.Name); id != "" {
		if _, ok := m.Def(id); ok {
			return id
		}
	}
	return ""
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// exitText renders an exit.
func exitText(rs []TraceReason, exited bool) string {
	if !exited {
		return "no exit recorded"
	}
	if len(rs) == 0 {
		return "success"
	}
	parts := make([]string, len(rs))
	for i, r := range rs {
		switch {
		case r.Reason == "interrupted":
			parts[i] = "interrupted"
		case r.Def != "":
			parts[i] = r.Reason + " " + r.Def
		default:
			parts[i] = r.Reason + " " + r.Name
		}
	}
	return strings.Join(parts, ", ")
}

// causeTrace explains the root exit: each reason it carries, or, for a
// successful root, each failure that a catch handled on the way, back to the
// events and the model facts that raised it.
func causeTrace(m *Model, ev *TraceEvidence) []string {
	var out []string
	say := func(depth int, format string, args ...any) {
		out = append(out, strings.Repeat("  ", depth)+fmt.Sprintf(format, args...))
	}
	root := ev.Root
	say(0, "task #%d %s (%s, %s) exited with %s", root.Task, root.Def, root.DefKind, root.Site, exitText(root.Exit, root.Exited))
	for _, r := range root.Exit {
		switch r.Reason {
		case "failure":
			say(1, "failure %s", nameOf(r))
			explain(m, ev, say, root.Task, r.Def, 2)
		case "defect":
			say(1, "defect %s, chain:", r.Name)
			for _, f := range r.Chain {
				d, _ := m.Def(f.Def)
				say(2, "task #%d %s at %s", f.Task, f.Def, d.Site)
			}
		case "interrupted":
			say(1, "interrupted")
			for _, e := range ev.Events {
				if e.Kind == "cancel-request" && e.Task == root.Task {
					say(2, "cancel-request of task #%d (seq %d)", e.Task, e.Seq)
				}
			}
		}
	}
	for _, e := range ev.Events {
		if e.Kind == "handler" {
			say(1, "handled %s at %s in %s (task #%d, seq %d)", e.Def, e.Site, e.In, e.Task, e.Seq)
			explain(m, ev, say, e.Task, e.Def, 2)
		}
	}
	return out
}

func nameOf(r TraceReason) string {
	if r.Def != "" {
		return r.Def
	}
	return r.Name
}

// explain shows why task could fail with errDef: a deadline of a timeout
// block it owns, a child that exited with the failure, or a call whose error
// row in the model names it.
func explain(m *Model, ev *TraceEvidence, say func(int, string, ...any), task uint64, errDef string, depth int) {
	if errDef == expiredType {
		for _, e := range ev.Events {
			if e.Kind != "deadline" || e.Parent != task {
				continue
			}
			d, _ := m.Def(e.Def)
			say(depth, "deadline of task #%d %s passed first (seq %d): the %s %s at %s in %s, row error[%s]", e.Task, e.Def, e.Seq, d.Kind, d.Spelling, d.Site, d.In, strings.Join(d.Errors, " | "))
			for _, f := range ev.Events {
				switch {
				case f.Task != e.Task:
				case f.Kind == "cancel-request":
					say(depth+1, "cancel-request of task #%d (seq %d)", f.Task, f.Seq)
				case f.Kind == "interrupt":
					say(depth+1, "task #%d observed the stop in %s at %s (seq %d)", f.Task, f.In, f.Site, f.Seq)
				case f.Kind == "call":
					say(depth+1, "it was in call %s at %s (seq %d)", f.Def, f.Site, f.Seq)
				case f.Kind == "body-exit":
					say(depth+1, "task #%d exited with %s (seq %d)", f.Task, exitText(f.Cause, true), f.Seq)
				}
			}
		}
		return
	}
	for _, e := range ev.Events {
		if e.Kind != "body-exit" || e.Parent != task {
			continue
		}
		for _, r := range e.Cause {
			if r.Def == errDef {
				d, _ := m.Def(e.Def)
				say(depth, "task #%d %s (%s at %s) exited with %s (seq %d)", e.Task, e.Def, d.Kind, d.Site, exitText(e.Cause, true), e.Seq)
				explain(m, ev, say, e.Task, errDef, depth+1)
			}
		}
	}
	for _, e := range ev.Events {
		if e.Kind != "call" || e.Task != task {
			continue
		}
		d, _ := m.Def(e.Def)
		if slices.Contains(d.Errors, errDef) {
			say(depth, "raised by call %s at %s in %s, whose row is error[%s] (seq %d)", e.Def, e.Site, e.In, strings.Join(d.Errors, " | "), e.Seq)
		}
	}
}

// Text renders the answer for a person or an agent reading a terminal.
func (ev *TraceEvidence) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "trace of %s at revision %s (recorder %s; schema %s)\n", ev.Source, short(ev.Revision), ev.Recorder, TraceSchema)
	dropped := 0
	for _, o := range ev.Omissions {
		if o.Kind == "dropped" {
			dropped = o.Count
		}
	}
	fmt.Fprintf(&b, "events: %d kept of capacity %d, %d dropped. Order is %s.\n", ev.Budgets.KeptEvents, ev.Budgets.Events, dropped, ev.Order)
	b.WriteString("\ntasks\n")
	depth := map[uint64]int{}
	for _, t := range ev.Tasks {
		depth[t.Task] = depth[t.Parent] + 1
		fmt.Fprintf(&b, "%s#%d %s (%s) at %s, exit: %s\n", strings.Repeat("  ", depth[t.Task]), t.Task, t.Def, t.DefKind, t.Site, exitText(t.Exit, t.Exited))
	}
	b.WriteString("\nevents\n")
	fmt.Fprintf(&b, "  %-4s %-15s %-5s %-38s %-22s %s\n", "seq", "kind", "task", "def", "site", "in")
	for _, e := range ev.Events {
		extra := e.In
		if len(e.Cause) > 0 {
			extra = strings.TrimSpace(extra + " -> " + exitText(e.Cause, true))
		}
		fmt.Fprintf(&b, "  %-4d %-15s #%-4d %-38s %-22s %s\n", e.Seq, e.Kind, e.Task, e.Def, e.Site, extra)
	}
	b.WriteString("\ncause trace of the root exit\n")
	for _, l := range ev.CauseTrace {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\nomissions\n")
	for _, o := range ev.Omissions {
		fmt.Fprintf(&b, "  - %s", o.Kind)
		if o.Count > 0 {
			fmt.Fprintf(&b, " (%d)", o.Count)
		}
		if o.Task > 0 {
			fmt.Fprintf(&b, " (task #%d)", o.Task)
		}
		fmt.Fprintf(&b, ": %s\n", o.Detail)
	}
	return b.String()
}
