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
	"time"
)

// Trace query: read a recorded run against the checked model of the file it
// came from. This is the prototype of `ef trace` and MCP `code.trace`
// (docs/research/traceability.md, Query surface).
//
// The query states a causal link only where the runtime recorded it: an
// event's link names the occurrence it refers to (a handler the failure it
// caught, a call-exit its call, a panic the call it left), and each reason
// of a body-exit names its own origin (a typed failure the failed call-exit,
// deadline or child exit it came from, a defect its panic, an interruption
// its cancel-request). A reason with no recorded origin is answered "origin
// not recorded", with candidates by type, labelled as candidates; another
// reason's origin never stands in for it. Before it explains anything the
// query validates the trace: every definition and site against the model,
// every task against its start, the parent graph, every link and origin and
// every defect frame. A trace that fails is refused with the reason, never
// explained.

const (
	// TraceSchema is the JSON Lines format a kernel program records
	// (runtime/rt/rttrace).
	TraceSchema = "effra-trace-3"
	// TraceEvidenceSchema is the envelope a query answers with. Its envelope
	// fields are those of the review evidence schema (#156): schema,
	// producer, revision, budgets, omissions and fingerprint.
	TraceEvidenceSchema = "trace-evidence-1"
	// TraceProducer names this query.
	TraceProducer = "sketchc trace (effra.local/sketch9)"
	// TraceByteBudget is the largest trace a query reads.
	TraceByteBudget = 8 << 20
	// traceMaxChain is the most frames the runtime keeps in a defect chain
	// (runtime/rt maxChain).
	traceMaxChain = 32
)

// taskKinds are the events the runtime raises for a task, keyed by the task's
// definition; pointKinds are raised by emitted code at a point of the model.
// preStartKinds may be raised before the task's start: a stop requested of a
// task, or its deadline, before its goroutine runs.
var (
	taskKinds     = []string{"start", "body-exit", "cancel-request", "observe", "deadline", "release", "panic"}
	pointKinds    = []string{"call", "call-exit", "interrupt", "construct", "handler"}
	preStartKinds = []string{"cancel-request", "deadline"}
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

// TraceReason is one reason of an exit or a call outcome, by type name.
// Origin, on a body-exit reason only, is the occurrence the runtime recorded
// as that reason's origin, 0 when none was recorded.
type TraceReason struct {
	Reason    string       `json:"reason"`
	Name      string       `json:"name,omitempty"`
	Def       string       `json:"def,omitempty"` // the failure's definition ID, resolved by the query
	InRelease bool         `json:"inRelease,omitempty"`
	Chain     []TraceFrame `json:"chain,omitempty"`
	Origin    uint64       `json:"origin,omitempty"`
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
	ID      uint64        `json:"id"`
	Link    uint64        `json:"link,omitempty"`
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

// TraceCallSpan is one capability call: its call event, its call-exit and the
// time between them, read from the recorder's clock as each event arrived.
type TraceCallSpan struct {
	Call       uint64 `json:"call"`           // occurrence ID of the call event
	Exit       uint64 `json:"exit,omitempty"` // occurrence ID of its call-exit, 0 when absent
	Task       uint64 `json:"task"`
	Def        string `json:"def"`
	Site       string `json:"site"`
	In         string `json:"in"`
	Outcome    string `json:"outcome"` // ok, failure <def>, defect <type>, interrupted, or open
	DurationNs int64  `json:"durationNs,omitempty"`
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
	Calls       []TraceCallSpan `json:"calls"`
	Events      []TraceEvent    `json:"events"`
	CauseTrace  []string        `json:"causeTrace"`
}

// traceIndex is a validated trace: every lookup the explanation makes.
type traceIndex struct {
	m        *Model
	events   []TraceEvent
	dropped  bool
	byID     map[uint64]*TraceEvent
	start    map[uint64]*TraceEvent // by task
	exit     map[uint64]*TraceEvent // by task
	callExit map[uint64]*TraceEvent // by the call's ID
	caught   map[uint64]*TraceEvent // by the caught occurrence: its handler
	order    []uint64               // tasks in start order
	root     uint64
	omit     []TraceOmission
	omitted  map[string]bool
}

// parentOf returns the parent its start recorded for task, and whether the
// start is in the trace.
func (x *traceIndex) parentOf(task uint64) (uint64, bool) {
	s := x.start[task]
	if s == nil {
		return 0, false
	}
	return s.Parent, true
}

// unavailable records that the ancestry of task is not in the trace, once.
// It is a refusal when the recorder dropped nothing: then the trace was
// edited, not cut short.
func (x *traceIndex) unavailable(task uint64, what string) error {
	if !x.dropped {
		return fmt.Errorf("%s: task #%d never started in this trace", what, task)
	}
	key := fmt.Sprintf("ancestry %d", task)
	if !x.omitted[key] {
		x.omitted[key] = true
		x.omit = append(x.omit, TraceOmission{Kind: "ancestryUnavailable", Task: task, Detail: "the start of this task is not in the trace (the recorder dropped events), so its parent and definition are unavailable"})
	}
	return nil
}

// QueryTrace reads data, a recorded trace, against m, the model of the
// source whose revision is revision. It refuses a trace whose revision,
// definition IDs, sites, task relationships or links do not match the model
// and the runtime's rules, rather than guess.
func QueryTrace(m *Model, revision string, data []byte) (*TraceEvidence, error) {
	if len(data) > TraceByteBudget {
		return nil, fmt.Errorf("trace is %d bytes, over the %d-byte budget", len(data), TraceByteBudget)
	}
	h, events, err := parseTrace(data)
	if err != nil {
		return nil, err
	}
	switch {
	case h.Schema != TraceSchema:
		return nil, fmt.Errorf("trace schema %q; this query reads %s", h.Schema, TraceSchema)
	case h.Revision != revision:
		return nil, fmt.Errorf("trace revision %s does not match %s revision %s: the trace was recorded from another program or another version of this one", short(h.Revision), m.File, short(revision))
	case h.Events != len(events):
		return nil, fmt.Errorf("trace header counts %d events but the trace holds %d: it is truncated or edited", h.Events, len(events))
	case h.Dropped < 0 || h.InFlight < 0:
		return nil, fmt.Errorf("trace header counts %d dropped and %d in-flight events", h.Dropped, h.InFlight)
	}
	x, err := validateTrace(m, h, events)
	if err != nil {
		return nil, err
	}

	ev := &TraceEvidence{
		Schema:   TraceEvidenceSchema,
		Producer: TraceProducer,
		Revision: revision,
		Source:   m.File,
		Recorder: h.Producer,
		Order:    "partial: seq is the recorder's arrival order; id is the runtime's emission order, in which an event that happens before another has the smaller id, though a smaller id alone does not mean happens-before",
		Budgets:  TraceBudgets{Events: h.Capacity, Bytes: TraceByteBudget, KeptEvents: len(events), TraceBytes: len(data)},
		Omissions: []TraceOmission{
			{Kind: "values", Detail: "argument, result and field values are not serialized (privacy default); failures and defects are named by type"},
			{Kind: "functionCalls", Detail: "direct calls between functions are not events; each site resolves to its enclosing definition"},
		},
	}
	if h.Dropped > 0 {
		ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "dropped", Count: h.Dropped, Detail: "the recorder was full; every event after the last kept one is missing"})
	}
	if h.InFlight > 0 {
		ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "inFlight", Count: h.InFlight, Detail: "events still being written when the trace was saved"})
	}
	ev.Omissions = append(ev.Omissions, x.omit...)
	for _, id := range x.order {
		s := x.start[id]
		t := TraceTask{Task: id, Parent: s.Parent, Def: s.Def, DefKind: s.DefKind, Site: s.Site}
		if e := x.exit[id]; e != nil {
			t.Exited, t.Exit = true, e.Cause
		} else {
			ev.Omissions = append(ev.Omissions, TraceOmission{Kind: "missingExit", Task: id, Detail: "the task started but its body-exit is not in the trace"})
		}
		ev.Tasks = append(ev.Tasks, t)
		if id == x.root {
			root := t
			ev.Root = &root
		}
	}
	ev.Calls = x.calls()
	ev.Events = events
	ev.CauseTrace = x.causeTrace(ev.Root)
	canon, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canon)
	ev.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
	return ev, nil
}

// parseTrace reads the header and the event lines.
func parseTrace(data []byte) (TraceHeader, []TraceEvent, error) {
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
				return h, nil, fmt.Errorf("line %d: want a header with schema %s first", line, TraceSchema)
			}
			continue
		}
		var ev TraceEvent
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Kind == "" {
			return h, nil, fmt.Errorf("line %d: not a trace event", line)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return h, nil, err
	}
	if h.Schema == "" {
		return h, nil, fmt.Errorf("trace has no header")
	}
	return h, events, nil
}

// validateTrace checks every event against the model and the runtime's rules
// and indexes the trace. Any violation is a refusal.
func validateTrace(m *Model, h TraceHeader, events []TraceEvent) (*traceIndex, error) {
	x := &traceIndex{
		m: m, events: events, dropped: h.Dropped > 0,
		byID: map[uint64]*TraceEvent{}, start: map[uint64]*TraceEvent{}, exit: map[uint64]*TraceEvent{},
		callExit: map[uint64]*TraceEvent{}, caught: map[uint64]*TraceEvent{}, omitted: map[string]bool{},
	}
	// Each event alone: identity, definition, site and reasons.
	for i := range events {
		e := &events[i]
		if err := x.resolve(e); err != nil {
			return nil, fmt.Errorf("event seq %d: %w", e.Seq, err)
		}
		if e.ID == 0 {
			return nil, fmt.Errorf("event seq %d: no occurrence id", e.Seq)
		}
		if x.byID[e.ID] != nil {
			return nil, fmt.Errorf("event seq %d: occurrence id %d is used twice", e.Seq, e.ID)
		}
		x.byID[e.ID] = e
		if e.Task == 0 {
			return nil, fmt.Errorf("event seq %d: no task", e.Seq)
		}
		switch e.Kind {
		case "start":
			if x.start[e.Task] != nil {
				return nil, fmt.Errorf("event seq %d: task #%d starts twice", e.Seq, e.Task)
			}
			x.start[e.Task] = e
			x.order = append(x.order, e.Task)
		case "body-exit":
			if x.exit[e.Task] != nil {
				return nil, fmt.Errorf("event seq %d: task #%d exits twice", e.Seq, e.Task)
			}
			x.exit[e.Task] = e
		}
	}
	// Each event against its task's start.
	for i := range events {
		e := &events[i]
		s := x.start[e.Task]
		if s == nil {
			if err := x.unavailable(e.Task, fmt.Sprintf("event seq %d", e.Seq)); err != nil {
				return nil, err
			}
			continue
		}
		if e.Parent != s.Parent {
			return nil, fmt.Errorf("event seq %d: task #%d has parent #%d here but #%d at its start", e.Seq, e.Task, e.Parent, s.Parent)
		}
		if slices.Contains(taskKinds, e.Kind) && e.Def != s.Def {
			return nil, fmt.Errorf("event seq %d: %s of task #%d names %s, but the task started as %s", e.Seq, e.Kind, e.Task, e.Def, s.Def)
		}
		if e != s && e.ID < s.ID && !slices.Contains(preStartKinds, e.Kind) {
			return nil, fmt.Errorf("event seq %d: %s of task #%d (id %d) precedes the task's start (id %d)", e.Seq, e.Kind, e.Task, e.ID, s.ID)
		}
		if e.Kind == "observe" {
			switch exit := x.exit[e.Task]; {
			case exit == nil && !x.dropped:
				return nil, fmt.Errorf("event seq %d: observe of task #%d, which has no body-exit", e.Seq, e.Task)
			case exit != nil && exit.ID > e.ID:
				return nil, fmt.Errorf("event seq %d: observe of task #%d (id %d) precedes its body-exit (id %d)", e.Seq, e.Task, e.ID, exit.ID)
			}
		}
	}
	if err := x.checkTasks(); err != nil {
		return nil, err
	}
	for i := range events {
		if err := x.checkLink(&events[i]); err != nil {
			return nil, fmt.Errorf("event seq %d: %w", events[i].Seq, err)
		}
		if err := x.checkOrigins(&events[i]); err != nil {
			return nil, fmt.Errorf("event seq %d: %w", events[i].Seq, err)
		}
	}
	for i := range events {
		if err := x.checkFrames(&events[i]); err != nil {
			return nil, fmt.Errorf("event seq %d: %w", events[i].Seq, err)
		}
	}
	return x, nil
}

// resolve checks e's definition, site and reasons against the model and
// fills in its resolved fields.
func (x *traceIndex) resolve(e *TraceEvent) error {
	m := x.m
	d, ok := m.Def(e.Def)
	if !ok {
		return fmt.Errorf("definition %q is not in %s at this revision", e.Def, m.File)
	}
	e.DefKind = d.Kind
	switch {
	case slices.Contains(taskKinds, e.Kind):
		if !m.isTaskDef(e.Def) {
			return fmt.Errorf("%s of %s, which is not a task definition", e.Kind, e.Def)
		}
		if e.Site != d.Site {
			return fmt.Errorf("site %s, but %s is declared at %s", e.Site, e.Def, d.Site)
		}
		e.In = d.In
	case slices.Contains(pointKinds, e.Kind):
		kind := e.Kind
		if kind == "call-exit" {
			kind = "call"
		}
		p, ok := m.PointAt(kind, e.Def, e.Site)
		if !ok {
			return fmt.Errorf("the model has no %s of %s at %s", kind, e.Def, e.Site)
		}
		e.In = p.In
	default:
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	if len(e.Cause) > 0 && !slices.Contains([]string{"body-exit", "call-exit", "panic"}, e.Kind) {
		return fmt.Errorf("a %s event carries no cause", e.Kind)
	}
	for j := range e.Cause {
		r := &e.Cause[j]
		switch r.Reason {
		case "failure":
			r.Def = m.failureDef(*r)
			if r.Def == "" {
				return fmt.Errorf("failure %q is not an error type of %s", r.Name, m.File)
			}
		case "defect", "interrupted":
		default:
			return fmt.Errorf("unknown reason %q", r.Reason)
		}
		if len(r.Chain) > 0 && r.Reason != "defect" {
			return fmt.Errorf("a %s reason carries a defect chain", r.Reason)
		}
		if r.Origin != 0 && e.Kind != "body-exit" {
			return fmt.Errorf("a %s reason carries an origin; only a body-exit's reasons do", e.Kind)
		}
	}
	return nil
}

// isTaskDef reports whether id names a task: main or a timeout block.
func (m *Model) isTaskDef(id string) bool {
	d, ok := m.Def(id)
	return ok && (d.Kind == DefTimeout || id == m.id("main"))
}

// checkTasks validates the parent graph: one root, every parent started
// before its child, and no cycle.
func (x *traceIndex) checkTasks() error {
	var roots []uint64
	for _, id := range x.order {
		s := x.start[id]
		switch {
		case s.Parent == 0:
			roots = append(roots, id)
			continue
		case s.Parent == id:
			return fmt.Errorf("task #%d names itself as its parent", id)
		}
		p := x.start[s.Parent]
		if p == nil {
			if err := x.unavailable(s.Parent, fmt.Sprintf("task #%d names parent #%d, but the parent", id, s.Parent)); err != nil {
				return err
			}
			continue
		}
		if p.ID > s.ID {
			return fmt.Errorf("task #%d (start id %d) starts before its parent #%d (start id %d)", id, s.ID, s.Parent, p.ID)
		}
	}
	// The start-order rule above already excludes a cycle; walk anyway, so a
	// later change to that rule cannot make the explanation loop.
	for _, id := range x.order {
		seen := map[uint64]bool{}
		for t := id; t != 0; {
			if seen[t] {
				return fmt.Errorf("the parent links of task #%d form a cycle", id)
			}
			seen[t] = true
			p, ok := x.parentOf(t)
			if !ok {
				break
			}
			t = p
		}
	}
	switch {
	case len(roots) == 0:
		return fmt.Errorf("trace has no root task start")
	case len(roots) > 1:
		return fmt.Errorf("trace has %d root tasks (#%d and #%d); one recorder holds one tree", len(roots), roots[0], roots[1])
	}
	x.root = roots[0]
	return nil
}

// isChild reports whether task's recorded parent is owner.
func (x *traceIndex) isChild(task, owner uint64) bool {
	p, ok := x.parentOf(task)
	return ok && p == owner
}

// checkLink validates the link of e against the kinds' rules.
func (x *traceIndex) checkLink(e *TraceEvent) error {
	if e.Link == 0 {
		if e.Kind == "call-exit" {
			return fmt.Errorf("call-exit links no call")
		}
		return nil
	}
	if e.Kind == "body-exit" {
		return fmt.Errorf("a body-exit carries no link; each of its reasons carries its own origin")
	}
	if !slices.Contains([]string{"call-exit", "handler", "observe", "panic"}, e.Kind) {
		return fmt.Errorf("a %s event carries no link", e.Kind)
	}
	if e.Link >= e.ID {
		return fmt.Errorf("%s (id %d) links occurrence %d, which was raised after it", e.Kind, e.ID, e.Link)
	}
	t := x.byID[e.Link]
	if t == nil {
		if !x.dropped {
			return fmt.Errorf("%s links occurrence %d, which is not in the trace", e.Kind, e.Link)
		}
		x.omit = append(x.omit, TraceOmission{Kind: "unresolvedLink", Task: e.Task, Detail: fmt.Sprintf("the %s at seq %d links occurrence %d, which the recorder dropped", e.Kind, e.Seq, e.Link)})
		return nil
	}
	ok := false
	switch e.Kind {
	case "call-exit":
		ok = t.Kind == "call" && t.Task == e.Task && t.Def == e.Def && t.Site == e.Site
		if ok && x.callExit[t.ID] != nil {
			return fmt.Errorf("call %d (seq %d) exits twice", t.ID, t.Seq)
		}
		if ok {
			x.callExit[t.ID] = e
		}
	case "handler":
		switch t.Kind {
		case "call-exit":
			ok = t.Task == e.Task && hasFailure(t.Cause, e.Def)
		case "deadline":
			ok = x.isChild(t.Task, e.Task) && e.Def == expiredType
		case "body-exit":
			ok = x.isChild(t.Task, e.Task) && hasFailure(t.Cause, e.Def)
		}
		if ok {
			if o := x.caught[t.ID]; o != nil {
				return fmt.Errorf("occurrence %d is caught by two handlers (seq %d and %d)", t.ID, o.Seq, e.Seq)
			}
			x.caught[t.ID] = e
		}
	case "observe":
		ok = t.Kind == "body-exit" && t.Task == e.Task
	case "panic":
		ok = t.Kind == "call-exit" && t.Task == e.Task && hasReason(t.Cause, "defect")
	}
	if !ok {
		return fmt.Errorf("%s of task #%d links the %s of task #%d (seq %d), which it cannot refer to", e.Kind, e.Task, t.Kind, t.Task, t.Seq)
	}
	return nil
}

// checkOrigins validates the origin of each reason of e against the reason
// it explains: a typed failure's origin is a failed call-exit of the same
// task with that failure, the deadline of a child (for timeout.Expired) or a
// child's exit with that failure; a defect's origin is the panic of the
// task that heads its chain, with the same panic type; an interruption's
// origin is a cancel-request of the same task. Two reasons never share an
// origin.
func (x *traceIndex) checkOrigins(e *TraceEvent) error {
	used := map[uint64]int{}
	for j, r := range e.Cause {
		if r.Origin == 0 {
			continue
		}
		if k, dup := used[r.Origin]; dup {
			return fmt.Errorf("reasons %d and %d of the body-exit name the same origin %d", k, j, r.Origin)
		}
		used[r.Origin] = j
		if r.Origin >= e.ID {
			return fmt.Errorf("reason %d of the body-exit (id %d) names origin %d, which was raised after it", j, e.ID, r.Origin)
		}
		t := x.byID[r.Origin]
		if t == nil {
			if !x.dropped {
				return fmt.Errorf("reason %d of the body-exit names origin %d, which is not in the trace", j, r.Origin)
			}
			x.omit = append(x.omit, TraceOmission{Kind: "unresolvedLink", Task: e.Task, Detail: fmt.Sprintf("reason %d of the body-exit at seq %d names origin %d, which the recorder dropped", j, e.Seq, r.Origin)})
			continue
		}
		ok := false
		switch r.Reason {
		case "failure":
			switch {
			case t.Task == e.Task:
				ok = t.Kind == "call-exit" && hasFailure(t.Cause, r.Def)
			case x.isChild(t.Task, e.Task):
				ok = (t.Kind == "deadline" && r.Def == expiredType) || (t.Kind == "body-exit" && hasFailure(t.Cause, r.Def))
			}
		case "defect":
			ok = t.Kind == "panic" && len(r.Chain) > 0 && t.Task == r.Chain[0].Task &&
				len(t.Cause) == 1 && t.Cause[0].Name == r.Name && t.Cause[0].InRelease == r.InRelease
		case "interrupted":
			ok = t.Kind == "cancel-request" && t.Task == e.Task
		}
		if !ok {
			return fmt.Errorf("the %s reason of task #%d's body-exit names the %s of task #%d (seq %d) as its origin, which cannot raise it", r.Reason, e.Task, t.Kind, t.Task, t.Seq)
		}
	}
	return nil
}

func hasFailure(rs []TraceReason, def string) bool {
	return slices.ContainsFunc(rs, func(r TraceReason) bool { return r.Reason == "failure" && r.Def == def })
}

func hasReason(rs []TraceReason, reason string) bool {
	return slices.ContainsFunc(rs, func(r TraceReason) bool { return r.Reason == reason })
}

// checkFrames validates the defect chains e carries: each frame a task
// definition of the model, the task that started under it, each frame the
// parent of the one before, and the chain passing through e's own task.
func (x *traceIndex) checkFrames(e *TraceEvent) error {
	for _, r := range e.Cause {
		if len(r.Chain) == 0 {
			continue
		}
		if len(r.Chain) > traceMaxChain {
			return fmt.Errorf("defect chain of %d frames; the runtime keeps at most %d", len(r.Chain), traceMaxChain)
		}
		through := false
		for i, f := range r.Chain {
			if _, ok := x.m.Def(f.Def); !ok {
				return fmt.Errorf("defect chain frame %d names definition %q, which is not in %s at this revision", i, f.Def, x.m.File)
			}
			if !x.m.isTaskDef(f.Def) {
				return fmt.Errorf("defect chain frame %d names %s, which is not a task definition", i, f.Def)
			}
			through = through || f.Task == e.Task
			s := x.start[f.Task]
			if s == nil {
				if err := x.unavailable(f.Task, fmt.Sprintf("defect chain frame %d", i)); err != nil {
					return err
				}
				continue
			}
			if s.Def != f.Def {
				return fmt.Errorf("defect chain frame %d names task #%d as %s, but it started as %s", i, f.Task, f.Def, s.Def)
			}
			if i > 0 {
				prev := r.Chain[i-1].Task
				if p, ok := x.parentOf(prev); ok && p != f.Task {
					return fmt.Errorf("defect chain frame %d: task #%d is not the parent of task #%d (its parent is #%d)", i, f.Task, prev, p)
				}
			}
		}
		last := r.Chain[len(r.Chain)-1].Task
		p, known := x.parentOf(last)
		switch {
		case len(r.Chain) < traceMaxChain && known && p != 0:
			return fmt.Errorf("defect chain stops at task #%d, whose parent #%d it leaves out", last, p)
		case len(r.Chain) == traceMaxChain && known && p != 0:
			key := fmt.Sprintf("chain %d", e.ID)
			if !x.omitted[key] {
				x.omitted[key] = true
				x.omit = append(x.omit, TraceOmission{Kind: "ancestryUnavailable", Task: last, Detail: fmt.Sprintf("the defect chain at seq %d keeps %d frames; the ancestors above this task are not in it", e.Seq, traceMaxChain)})
			}
		}
		if !through && (len(r.Chain) < traceMaxChain) && x.start[e.Task] != nil {
			return fmt.Errorf("defect chain of the %s of task #%d does not pass through that task", e.Kind, e.Task)
		}
	}
	return nil
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

// exitText renders an exit or a call outcome.
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

// outcome renders a call-exit's outcome.
func outcome(e *TraceEvent) string {
	if e == nil {
		return "open"
	}
	if len(e.Cause) == 0 {
		return "ok"
	}
	return exitText(e.Cause, true)
}

// calls pairs each call with its call-exit.
func (x *traceIndex) calls() []TraceCallSpan {
	var out []TraceCallSpan
	for i := range x.events {
		c := &x.events[i]
		if c.Kind != "call" {
			continue
		}
		s := TraceCallSpan{Call: c.ID, Task: c.Task, Def: c.Def, Site: c.Site, In: c.In}
		e := x.callExit[c.ID]
		s.Outcome = outcome(e)
		if e != nil {
			s.Exit, s.DurationNs = e.ID, e.Time-c.Time
		}
		out = append(out, s)
	}
	return out
}

// causeTrace explains the root exit: each reason it carries, then each
// failure a catch handled on the way, back along the recorded links.
func (x *traceIndex) causeTrace(root *TraceTask) []string {
	c := &causeWriter{x: x, seen: map[uint64]bool{}}
	c.say(0, "task #%d %s (%s, %s) exited with %s", root.Task, root.Def, root.DefKind, root.Site, exitText(root.Exit, root.Exited))
	if exit := x.exit[root.Task]; exit != nil {
		c.reasons(exit, 1)
	}
	for i := range x.events {
		e := &x.events[i]
		if e.Kind != "handler" {
			continue
		}
		c.say(1, "handled %s at %s in %s (task #%d, seq %d)", e.Def, e.Site, e.In, e.Task, e.Seq)
		if e.Link != 0 {
			c.origin(e.Link, e.Def, 2, "caught ")
			continue
		}
		c.unrecorded(e.Task, e.Def, e.ID, 2)
	}
	return c.out
}

// reasons explains each reason of body-exit e from that reason's own
// recorded origin, and says "origin not recorded" where it has none.
func (c *causeWriter) reasons(e *TraceEvent, depth int) {
	for _, r := range e.Cause {
		switch r.Reason {
		case "failure":
			c.say(depth, "failure %s", r.Def)
			c.reasonOrigin(e, r, depth+1)
		case "defect":
			in := ""
			if r.InRelease {
				in = " in a release"
			}
			c.say(depth, "defect %s%s, chain:", r.Name, in)
			for _, f := range r.Chain {
				d, _ := c.x.m.Def(f.Def)
				c.say(depth+1, "task #%d %s at %s", f.Task, f.Def, d.Site)
			}
			c.reasonOrigin(e, r, depth+1)
		case "interrupted":
			c.say(depth, "interrupted")
			c.reasonOrigin(e, r, depth+1)
			c.stopFacts(e.Task, depth+1)
		}
	}
}

// reasonOrigin explains the recorded origin of reason r of body-exit e.
func (c *causeWriter) reasonOrigin(e *TraceEvent, r TraceReason, depth int) {
	if r.Origin != 0 {
		verb := map[string]string{"failure": "carried ", "defect": "raised by ", "interrupted": "requested by "}[r.Reason]
		c.origin(r.Origin, r.Def, depth, verb)
		return
	}
	switch r.Reason {
	case "failure":
		c.unrecorded(e.Task, r.Def, e.ID, depth)
	case "defect":
		c.say(depth, "origin not recorded: no event links this defect")
	case "interrupted":
		c.say(depth, "origin not recorded: no event links the stop request")
	}
}

// causeWriter writes cause-trace lines. seen guards every traversal: an
// occurrence is explained once.
type causeWriter struct {
	x    *traceIndex
	out  []string
	seen map[uint64]bool
}

func (c *causeWriter) say(depth int, format string, args ...any) {
	c.out = append(c.out, strings.Repeat("  ", depth)+fmt.Sprintf(format, args...))
}

// origin explains occurrence id, which a recorded link or origin named for
// failure def ("" for a reason that is not a typed failure).
func (c *causeWriter) origin(id uint64, def string, depth int, verb string) {
	x := c.x
	e := x.byID[id]
	if e == nil {
		c.say(depth, "%soccurrence %d, which the recorder dropped (see omissions)", verb, id)
		return
	}
	if c.seen[id] {
		c.say(depth, "%s%s seq %d, explained above", verb, e.Kind, e.Seq)
		return
	}
	c.seen[id] = true
	switch e.Kind {
	case "call-exit":
		d, _ := x.m.Def(e.Def)
		callSeq := "dropped"
		if call := x.byID[e.Link]; call != nil {
			callSeq = fmt.Sprint(call.Seq)
		}
		c.say(depth, "%sthe %s of call %s at %s in %s, whose row is error[%s] (call seq %s, call-exit seq %d)", verb, outcome(e), e.Def, e.Site, e.In, strings.Join(d.Errors, " | "), callSeq, e.Seq)
	case "deadline":
		d, _ := x.m.Def(e.Def)
		c.say(depth, "%sthe deadline of task #%d %s (seq %d): the %s %s at %s in %s, row error[%s]", verb, e.Task, e.Def, e.Seq, d.Kind, d.Spelling, d.Site, d.In, strings.Join(d.Errors, " | "))
		c.deadlineFacts(e, depth+1)
	case "body-exit":
		d, _ := x.m.Def(e.Def)
		c.say(depth, "%sthe exit of task #%d %s (%s at %s) with %s (seq %d)", verb, e.Task, e.Def, d.Kind, d.Site, exitText(e.Cause, true), e.Seq)
		// The child's failure def reached the owner: explain that reason of
		// the child's exit from its own origin.
		for _, r := range e.Cause {
			if r.Reason == "failure" && r.Def == def {
				c.reasonOrigin(e, r, depth+1)
				break
			}
		}
	case "cancel-request":
		c.say(depth, "%sthe cancel-request of task #%d (seq %d)", verb, e.Task, e.Seq)
	case "panic":
		in := ""
		if len(e.Cause) == 1 && e.Cause[0].InRelease {
			in = " in a release"
		}
		c.say(depth, "%sthe panic %s%s in task #%d (seq %d)", verb, exitText(e.Cause, true), in, e.Task, e.Seq)
		if e.Link != 0 {
			c.origin(e.Link, "", depth+1, "left ")
		}
	}
}

// deadlineFacts states what the trace records of the task a deadline
// stopped: the calls open when it passed, by occurrence order, the stop
// request, its observation and the task's exit.
func (c *causeWriter) deadlineFacts(dl *TraceEvent, depth int) {
	x := c.x
	for i := range x.events {
		f := &x.events[i]
		if f.Task != dl.Task || f.Kind != "call" || f.ID > dl.ID {
			continue
		}
		exit := x.callExit[f.ID]
		switch {
		case exit == nil:
			c.say(depth, "call %s at %s was open when the deadline passed (call seq %d); its call-exit is not in the trace", f.Def, f.Site, f.Seq)
		case exit.ID > dl.ID:
			c.say(depth, "call %s at %s was open when the deadline passed (call seq %d); it ended %s (call-exit seq %d)", f.Def, f.Site, f.Seq, outcome(exit), exit.Seq)
		}
	}
	c.stopFacts(dl.Task, depth)
}

// stopFacts states the stop requests, interruption points and exit the trace
// records for task.
func (c *causeWriter) stopFacts(task uint64, depth int) {
	x := c.x
	for i := range x.events {
		f := &x.events[i]
		if f.Task != task {
			continue
		}
		switch f.Kind {
		case "cancel-request":
			c.say(depth, "cancel-request of task #%d (seq %d)", f.Task, f.Seq)
		case "interrupt":
			c.say(depth, "task #%d observed the stop in %s at %s (seq %d)", f.Task, f.In, f.Site, f.Seq)
		case "body-exit":
			c.say(depth, "task #%d exited with %s (seq %d)", f.Task, exitText(f.Cause, true), f.Seq)
		}
	}
}

// unrecorded says that no link names the origin of failure def, which task
// handled or exited with at occurrence before, and lists the candidates by
// type, labelled as candidates.
func (c *causeWriter) unrecorded(task uint64, def string, before uint64, depth int) {
	x := c.x
	c.say(depth, "origin not recorded: no event links the occurrence of %s", def)
	n := 0
	for i := range x.events {
		f := &x.events[i]
		if f.ID >= before {
			continue
		}
		switch {
		case f.Kind == "call-exit" && f.Task == task && hasFailure(f.Cause, def):
			c.say(depth+1, "candidate by type only: call %s at %s ended %s (call-exit seq %d)", f.Def, f.Site, outcome(f), f.Seq)
		case f.Kind == "deadline" && def == expiredType && x.isChild(f.Task, task):
			c.say(depth+1, "candidate by type only: the deadline of task #%d %s (seq %d)", f.Task, f.Def, f.Seq)
		case f.Kind == "body-exit" && x.isChild(f.Task, task) && hasFailure(f.Cause, def):
			c.say(depth+1, "candidate by type only: the exit of task #%d %s with %s (seq %d)", f.Task, f.Def, exitText(f.Cause, true), f.Seq)
		default:
			continue
		}
		n++
	}
	if n == 0 {
		c.say(depth+1, "no candidate in the trace: source code that returns the failure itself records no event")
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
	if len(ev.Calls) > 0 {
		b.WriteString("\ncapability calls (span: call to call-exit, on the recorder's clock)\n")
		for _, c := range ev.Calls {
			span := "no call-exit"
			if c.Exit != 0 {
				span = time.Duration(c.DurationNs).String()
			}
			fmt.Fprintf(&b, "  id %-4d task #%-3d %s at %s in %s: %s, %s\n", c.Call, c.Task, c.Def, c.Site, c.In, c.Outcome, span)
		}
	}
	b.WriteString("\nevents\n")
	fmt.Fprintf(&b, "  %-4s %-4s %-5s %-15s %-5s %-38s %-22s %s\n", "seq", "id", "link", "kind", "task", "def", "site", "in")
	for _, e := range ev.Events {
		extra := e.In
		if len(e.Cause) > 0 {
			extra = strings.TrimSpace(extra + " -> " + exitText(e.Cause, true))
		}
		var origins []string
		for _, r := range e.Cause {
			if r.Origin != 0 {
				origins = append(origins, fmt.Sprint(r.Origin))
			} else if e.Kind == "body-exit" {
				origins = append(origins, "-")
			}
		}
		if len(origins) > 0 {
			extra += " (origins " + strings.Join(origins, ", ") + ")"
		}
		link := ""
		if e.Link != 0 {
			link = fmt.Sprint(e.Link)
		}
		fmt.Fprintf(&b, "  %-4d %-4d %-5s %-15s #%-4d %-38s %-22s %s\n", e.Seq, e.ID, link, e.Kind, e.Task, e.Def, e.Site, extra)
	}
	b.WriteString("\ncause trace of the root exit (links recorded by the runtime; candidates are labelled)\n")
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
