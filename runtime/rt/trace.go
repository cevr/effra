package rt

import (
	"sync"
	"sync/atomic"
)

// EventKind is the kind of one trace event. The names are the trace
// vocabulary of the runtime laws (#124, docs/research/lawful-runtime-contract.md
// RS1), so a law checker and a person read the same stream. Three kinds are
// additions, justified in docs/research/traceability.md: call and call-exit
// (the capability boundary a checked row names, and its outcome) and
// interrupt (the interruption point that observed a stop).
type EventKind uint8

const (
	// TraceStart: a task's body begins at its boundary (Run).
	TraceStart EventKind = iota + 1
	// TraceBodyExit: a task's boundary settled its exit; Event.Cause is the
	// exit, nil on success. Event.Link is the occurrence the exit carries:
	// the task's latest failure, interruption or defect no handler consumed,
	// 0 on success or when no occurrence was recorded.
	TraceBodyExit
	// TraceCancelRequest: a stop was requested of a task, directly or by the
	// cascade from an ancestor. It may precede the task's start.
	TraceCancelRequest
	// TraceObserve: an owner observed a child's completed exit: a Wait that
	// was not interrupted, a Stop, or the join inside After. An interrupted
	// join records none (law R4). Event.Link is the child's body-exit.
	TraceObserve
	// TraceDeadline: a timeout's deadline passed before its body finished.
	// The event belongs to the child the deadline stops; it may precede the
	// child's start.
	TraceDeadline
	// TraceRelease: a scope ran one release, on every exit of the release.
	TraceRelease
	// TracePanic: a defect was recorded; Event.Defect is the panic value.
	// Event.Link is the call-exit of the capability call the panic left, if
	// one was open.
	TracePanic
	// TraceHandler: a catch arm handled a typed failure (emitted code).
	// Event.Link is the occurrence it caught: a failed call-exit, a deadline
	// or a child's body-exit; 0 when the failure was raised without one.
	TraceHandler
	// TraceConstruct: a wire was built (emitted code).
	TraceConstruct
	// TraceCall: a capability call begins (emitted code, Task.TraceCall). An
	// addition to #124.
	TraceCall
	// TraceInterrupt: a task observed a stop at an interruption point (I1)
	// (emitted code). An addition to #124.
	TraceInterrupt
	// TraceCallExit: a capability call ended (emitted code,
	// Task.TraceCallExit, or the runtime when a defect leaves the call).
	// Event.Link is the call; Event.Cause is its error, nil for ok, and
	// Event.Defect the panic value that left it. An addition to #124.
	TraceCallExit
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
	TraceCallExit:      "call-exit",
}

// String returns the vocabulary name of k.
func (k EventKind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "unknown"
}

// Event is one trace event. It carries identities, never argument or result
// values: an occurrence ID, the task and its parent, a definition ID, a
// source site, and for an exit or a defect the error and panic value. The
// values reach the Tracer; the rttrace writer serializes them by type only.
type Event struct {
	Kind EventKind
	// ID is the event's occurrence ID, unique in its traced tree. The runtime
	// takes it from one atomic counter per tree when the event is raised, so
	// when one event happens before another its ID is smaller. A smaller ID
	// alone does not mean happens-before.
	ID uint64
	// Link is the ID of the occurrence the event refers to, 0 for none: see
	// the kinds. The runtime records it; a reader never infers it.
	Link uint64
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
	// Cause is the exit of a body-exit event, in canonical form, or the
	// error of a call-exit.
	Cause error
	// Defect is the panic value of a panic event or of a call-exit a defect
	// left.
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
// and no emission point exists on the poll. Everything else here exists only
// on a traced tree.
type tracer struct {
	sink   Tracer
	broken atomic.Bool
	next   atomic.Uint64 // the last occurrence ID handed out

	mu    sync.Mutex
	tasks map[uint64]taskLinks // by task ID; see taskLinks
}

// taskLinks is what the runtime remembers about one task to record links
// instead of leaving them to a reader's inference. An entry lives from the
// task's first linkable event until its owner observes or forgets it.
type taskLinks struct {
	// pending is the task's latest failure, interruption or defect
	// occurrence that no handler has consumed. A failure travels from where
	// it is raised to a handler or to the body exit by return, inside one
	// task, so the latest unconsumed one is the one in flight.
	pending uint64
	// exit is the task's body-exit occurrence, and failed whether that exit
	// was an error, for the owner's observation.
	exit   uint64
	failed bool
	// calls are the capability calls open in the task, innermost last.
	calls []openCall
}

type openCall struct {
	id        uint64
	def, site string
}

// raise assigns e the next occurrence ID and delivers it. It returns the ID,
// 0 when the tracer is broken.
func (h *tracer) raise(e *Event) uint64 {
	if h.broken.Load() {
		return 0
	}
	e.ID = h.next.Add(1)
	defer func() {
		if recover() != nil {
			h.broken.Store(true)
		}
	}()
	h.sink.Event(*e)
	return e.ID
}

// The entry operations below each take the lock once. They are spelled out
// rather than sharing one helper that takes a func: the func's *taskLinks
// argument made every entry escape, one allocation per event.

// setPending makes occ the pending occurrence of task id.
func (h *tracer) setPending(id, occ uint64) {
	h.mu.Lock()
	l := h.tasks[id]
	l.pending = occ
	h.tasks[id] = l
	h.mu.Unlock()
}

// pending returns the pending occurrence of task id; with take set it also
// clears it.
func (h *tracer) pending(id uint64, take bool) uint64 {
	h.mu.Lock()
	l, ok := h.tasks[id]
	occ := l.pending
	if take && ok && occ != 0 {
		l.pending = 0
		h.tasks[id] = l
	}
	h.mu.Unlock()
	return occ
}

// openCall pushes c on the open calls of task id.
func (h *tracer) openCall(id uint64, c openCall) {
	h.mu.Lock()
	l := h.tasks[id]
	l.calls = append(l.calls, c)
	h.tasks[id] = l
	h.mu.Unlock()
}

// closeCall pops call from the open calls of task id; a nonzero failed makes
// that call-exit the task's pending occurrence.
func (h *tracer) closeCall(id, call, failed uint64) {
	h.mu.Lock()
	l := h.tasks[id]
	if n := len(l.calls); n > 0 && l.calls[n-1].id == call {
		l.calls = l.calls[:n-1]
	}
	if failed != 0 {
		l.pending = failed
	}
	h.tasks[id] = l
	h.mu.Unlock()
}

// takeCalls removes and returns the open calls of task id.
func (h *tracer) takeCalls(id uint64) []openCall {
	h.mu.Lock()
	l, ok := h.tasks[id]
	open := l.calls
	if ok && len(open) > 0 {
		l.calls = nil
		h.tasks[id] = l
	}
	h.mu.Unlock()
	return open
}

// exited replaces the entry of task id by its body-exit.
func (h *tracer) exited(id, exit uint64, failed bool) {
	h.mu.Lock()
	h.tasks[id] = taskLinks{exit: exit, failed: failed}
	h.mu.Unlock()
}

// takeExit removes the entry of task id and returns its body-exit.
func (h *tracer) takeExit(id uint64) (exit uint64, failed bool) {
	h.mu.Lock()
	l := h.tasks[id]
	delete(h.tasks, id)
	h.mu.Unlock()
	return l.exit, l.failed
}

// forget drops the entry of task id.
func (h *tracer) forget(id uint64) {
	h.mu.Lock()
	delete(h.tasks, id)
	h.mu.Unlock()
}

// TracedRoot returns a root task whose tree reports its events to tr: every
// task started below it inherits tr when it starts. A nil tr gives an
// untraced root, the same as Root. The runtime never installs a tracer
// itself; emitted main decides (for example from an environment switch).
func TracedRoot(span string, tr Tracer) *Task {
	t := Root(span)
	if tr != nil {
		t.trace = &tracer{sink: tr, tasks: map[uint64]taskLinks{}}
	}
	return t
}

// Traced reports whether t reports events to a tracer.
func (t *Task) Traced() bool { return t.trace != nil }

// Trace reports an event that emitted code raises at a source site: a
// handler, a construct or an interrupt. def is a definition ID and site a
// source site, both compile-time constants. A handler event links the
// failure it caught; an interrupt becomes the task's pending occurrence. A
// capability call goes through TraceCall and TraceCallExit instead, and any
// other kind is not raised. With no tracer it costs one load and compare.
func (t *Task) Trace(k EventKind, def, site string) {
	if t.trace != nil {
		t.traceAt(k, def, site)
	}
}

// TraceCall reports the start of a capability call at a source site and
// returns its occurrence ID, which emitted code passes to TraceCallExit when
// the call returns. With no tracer it costs one load and compare and returns
// 0.
func (t *Task) TraceCall(def, site string) uint64 {
	if t.trace == nil {
		return 0
	}
	return t.traceCall(def, site)
}

// TraceCallExit reports that the capability call with occurrence ID call
// returned err (nil for ok). A failed or interrupted call becomes the task's
// pending occurrence, the one a handler or the body exit links. With no
// tracer it costs one load and compare.
func (t *Task) TraceCallExit(call uint64, def, site string, err error) {
	if t.trace != nil {
		t.traceCallExit(call, def, site, err)
	}
}

// The emission helpers below are never inlined. An Event literal built in a
// hot function (Run, Wait, a release) would grow that function's frame even
// when tracing is off: measured, Run's frame went from 0x60 to 0xc0 bytes and
// spawn+wait slowed by about 90 ns per task. So the hot functions pass only a
// kind and a pointer or two, and the Event is built here.

//go:noinline
func (t *Task) traceAt(k EventKind, def, site string) {
	switch k {
	case TraceConstruct:
		t.emit(&Event{Kind: k, Def: def, Site: site})
	case TraceHandler:
		caught := t.trace.pending(t.id, true)
		t.emit(&Event{Kind: k, Link: caught, Def: def, Site: site})
	case TraceInterrupt:
		if id := t.emit(&Event{Kind: k, Def: def, Site: site}); id != 0 {
			t.trace.setPending(t.id, id)
		}
	}
}

//go:noinline
func (t *Task) traceCall(def, site string) uint64 {
	id := t.emit(&Event{Kind: TraceCall, Def: def, Site: site})
	if id != 0 {
		t.trace.openCall(t.id, openCall{id, def, site})
	}
	return id
}

//go:noinline
func (t *Task) traceCallExit(call uint64, def, site string, err error) {
	id := t.emit(&Event{Kind: TraceCallExit, Link: call, Def: def, Site: site, Cause: err})
	var failed uint64
	if err != nil {
		failed = id
	}
	t.trace.closeCall(t.id, call, failed)
}

// traceTask raises a task event of t: start, cancel-request or release.
//
//go:noinline
func (t *Task) traceTask(k EventKind, cause error) {
	t.emit(&Event{Kind: k, Def: t.span, Cause: cause})
}

// traceExit raises the body-exit event of t with the occurrence its exit
// carries, and keeps the exit for the owner's observation.
//
//go:noinline
func (t *Task) traceExit(cause error) {
	var carried uint64
	if cause != nil {
		carried = t.trace.pending(t.id, false)
	}
	id := t.emit(&Event{Kind: TraceBodyExit, Link: carried, Def: t.span, Cause: cause})
	t.trace.exited(t.id, id, cause != nil)
}

// traceDefect raises the panic event of a defect recorded in t. A defect
// outside a release unwinds every capability call still open in t, so each
// gets its call-exit first, innermost first, and the panic links the
// innermost.
//
//go:noinline
func (t *Task) traceDefect(p any, inRelease bool) {
	var open []openCall
	if !inRelease {
		open = t.trace.takeCalls(t.id)
	}
	var left uint64
	for i := len(open) - 1; i >= 0; i-- {
		c := open[i]
		id := t.emit(&Event{Kind: TraceCallExit, Link: c.id, Def: c.def, Site: c.site, Defect: p})
		if left == 0 {
			left = id
		}
	}
	if id := t.emit(&Event{Kind: TracePanic, Link: left, Def: t.span, Defect: p, InRelease: inRelease}); id != 0 {
		t.trace.setPending(t.id, id)
	}
}

// childEvent delivers an event of child t raised by its owner's goroutine: an
// observation or a deadline. The owner is named explicitly, because a
// finished child has dropped its parent link. A deadline becomes the owner's
// pending occurrence: the owner receives Expired. An observation links the
// child's body-exit and ends the child's entry; with carry set, a failed
// exit becomes the owner's pending occurrence, because the owner receives
// that exit as its own error. Call it only when t.trace is set.
//
//go:noinline
func (t *Task) childEvent(k EventKind, owner *Task, carry bool) {
	e := Event{Kind: k, Task: t.id, Def: t.span}
	if owner != nil {
		e.Parent = owner.id
	}
	h := t.trace
	if k == TraceDeadline {
		if id := h.raise(&e); id != 0 && owner != nil {
			h.setPending(owner.id, id)
		}
		return
	}
	exit, failed := h.takeExit(t.id)
	e.Link = exit
	h.raise(&e)
	if carry && failed && exit != 0 && owner != nil {
		h.setPending(owner.id, exit)
	}
}

// traceForget ends the entry of child t when its owner joins it without
// observing it (an interrupted join). Call it only when t.trace is set.
//
//go:noinline
func (t *Task) traceForget() { t.trace.forget(t.id) }

// emit fills in t's identity and delivers e. It returns the occurrence ID, 0
// when the tracer is broken. Call it only when t.trace is set. The Event goes
// by pointer down to the Tracer call: passed by value, each frame held its own
// copy, a new task's body-exit outgrew the goroutine's first stack, and the
// stack copy measured about 2.5 µs per traced spawn.
func (t *Task) emit(e *Event) uint64 {
	e.Task = t.id
	if p := t.parent.Load(); p != nil {
		e.Parent = p.id
	}
	return t.trace.raise(e)
}
