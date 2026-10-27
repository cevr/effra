package rt

import "sync/atomic"

// EventKind is the kind of one trace event. The names are the trace
// vocabulary of the runtime laws (#124, docs/research/lawful-runtime-contract.md
// RS1), so a law checker and a person read the same stream. Two kinds are
// additions, justified in docs/research/traceability.md: call (the capability
// boundary a checked row names) and interrupt (the interruption point that
// observed a stop).
type EventKind uint8

const (
	// TraceStart: a task's body begins at its boundary (Run).
	TraceStart EventKind = iota + 1
	// TraceBodyExit: a task's boundary settled its exit; Event.Cause is the
	// exit, nil on success.
	TraceBodyExit
	// TraceCancelRequest: a stop was requested of a task, directly or by the
	// cascade from an ancestor.
	TraceCancelRequest
	// TraceObserve: an owner observed a child's completed exit: a Wait that
	// was not interrupted, a Stop, or the join inside After. An interrupted
	// join records none (law R4).
	TraceObserve
	// TraceDeadline: a timeout's deadline passed before its body finished.
	// The event belongs to the child the deadline stops.
	TraceDeadline
	// TraceRelease: a scope ran one release, on every exit of the release.
	TraceRelease
	// TracePanic: a defect was recorded; Event.Defect is the panic value.
	TracePanic
	// TraceHandler: a catch arm handled a typed failure (emitted code).
	TraceHandler
	// TraceConstruct: a wire was built (emitted code).
	TraceConstruct
	// TraceCall: a capability call begins (emitted code). An addition to #124.
	TraceCall
	// TraceInterrupt: a task observed a stop at an interruption point (I1)
	// (emitted code). An addition to #124.
	TraceInterrupt
)

var kindNames = [...]string{
	TraceStart:         "start",
	TraceBodyExit:      "body-exit",
	TraceCancelRequest: "cancel-request",
	TraceObserve:       "observe",
	TraceDeadline:      "deadline",
	TraceRelease:       "release",
	TracePanic:         "panic",
	TraceHandler:       "handler",
	TraceConstruct:     "construct",
	TraceCall:          "call",
	TraceInterrupt:     "interrupt",
}

// String returns the vocabulary name of k.
func (k EventKind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "unknown"
}

// Event is one trace event. It carries identities, never argument or result
// values: the task and its parent, a definition ID, a source site, and for an
// exit or a defect the error and panic value, which a recorder renders by type
// only.
type Event struct {
	Kind EventKind
	// Task is the task the event belongs to.
	Task uint64
	// Parent is the task's parent while it is linked; 0 for a root and for a
	// task that has finished. A recorder reads the parent of a task from its
	// start event.
	Parent uint64
	// Def is a definition ID: the task's span for a task event, or the
	// definition the emitted code named.
	Def string
	// Site is the source site emitted code named, "" for an event the runtime
	// raises itself (the definition's own site applies).
	Site string
	// Cause is the exit of a body-exit event, in canonical form.
	Cause error
	// Defect is the panic value of a panic event.
	Defect any
	// InRelease marks a panic raised by a release.
	InRelease bool
}

// Tracer receives the events of one task tree. The runtime calls Event from
// any goroutine of the tree, concurrently, sometimes while it holds a task's
// lock or runs a masked region. So Event must return promptly, must not block
// on the program's own progress, and must not call back into a task (no
// Interrupt, Wait or Stop). A Tracer that panics is disabled for its tree from
// then on: its panic never becomes a defect of the program and never skips a
// release.
type Tracer interface {
	Event(Event)
}

// tracer is the per-tree hook a task points to. A task with no tracer holds a
// nil pointer, so the off path is one load and compare at each emission point,
// and no emission point exists on the poll.
type tracer struct {
	sink   Tracer
	broken atomic.Bool
}

func (h *tracer) deliver(e Event) {
	if h.broken.Load() {
		return
	}
	defer func() {
		if recover() != nil {
			h.broken.Store(true)
		}
	}()
	h.sink.Event(e)
}

// TracedRoot returns a root task whose tree reports its events to tr: every
// task started below it inherits tr when it starts. A nil tr gives an
// untraced root, the same as Root. The runtime never installs a tracer
// itself; emitted main decides (for example from an environment switch).
func TracedRoot(span string, tr Tracer) *Task {
	t := Root(span)
	if tr != nil {
		t.trace = &tracer{sink: tr}
	}
	return t
}

// Traced reports whether t reports events to a tracer.
func (t *Task) Traced() bool { return t.trace != nil }

// Trace reports an event that emitted code raises at a source site: a call,
// a handler, a construct or an interrupt. def is a definition ID and site a
// source site, both compile-time constants. With no tracer it costs one load
// and compare.
func (t *Task) Trace(k EventKind, def, site string) {
	if t.trace != nil {
		t.traceAt(k, def, site)
	}
}

// The emission helpers below are never inlined. An Event literal built in a
// hot function (Run, Wait, a release) would grow that function's frame even
// when tracing is off: measured, Run's frame went from 0x60 to 0xc0 bytes and
// spawn+wait slowed by about 90 ns per task. So the hot functions pass only a
// kind and a pointer or two, and the Event is built here.

//go:noinline
func (t *Task) traceAt(k EventKind, def, site string) {
	t.emit(Event{Kind: k, Def: def, Site: site})
}

// traceTask raises a task event of t: start, body-exit, cancel-request or
// release.
//
//go:noinline
func (t *Task) traceTask(k EventKind, cause error) {
	t.emit(Event{Kind: k, Def: t.span, Cause: cause})
}

// traceDefect raises the panic event of a defect recorded in t.
//
//go:noinline
func (t *Task) traceDefect(p any, inRelease bool) {
	t.emit(Event{Kind: TracePanic, Def: t.span, Defect: p, InRelease: inRelease})
}

// childEvent delivers an event of child t raised by its owner's goroutine: an
// observation or a deadline. The owner is named explicitly, because a
// finished child has dropped its parent link. Call it only when t.trace is
// set.
//
//go:noinline
func (t *Task) childEvent(k EventKind, owner *Task) {
	e := Event{Kind: k, Task: t.id, Def: t.span}
	if owner != nil {
		e.Parent = owner.id
	}
	t.trace.deliver(e)
}

// emit fills in t's identity and delivers e. Call it only when t.trace is
// set.
func (t *Task) emit(e Event) {
	e.Task = t.id
	if p := t.parent.Load(); p != nil {
		e.Parent = p.id
	}
	t.trace.deliver(e)
}
