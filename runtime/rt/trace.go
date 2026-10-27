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
	// exit, nil on success. Event.Origins holds one occurrence ID per reason
	// of the exit, in the order rttrace.Reasons lists them, each recorded
	// where that reason arose; 0 for a reason whose origin was not recorded.
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
	// Event.Link is the call-exit of the innermost capability call the panic
	// unwound, if one was open.
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
	// Task.TraceCallExit, or the runtime when an unwind leaves the call: a
	// defect, an interruption or a transported exit). Event.Link is the call;
	// Event.Cause is its error, nil for ok, and Event.Defect the panic value
	// that left it. An addition to #124.
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
	// Origins are a body-exit's per-reason links: see TraceBodyExit.
	Origins []uint64
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

	mu      sync.Mutex
	tasks   map[uint64]taskLinks // by task ID; see taskLinks
	defects map[*Frame]uint64    // a defect reason's panic event, keyed by its chain
}

// taskLinks is what the runtime remembers about one task to record links
// instead of leaving them to a reader's inference. An entry lives from the
// task's first linkable event until its owner observes or forgets it.
type taskLinks struct {
	// failure is the task's latest typed-failure occurrence that no handler
	// has consumed (a failed call-exit, the deadline of a timeout the task
	// owns, or a child's failed exit the task received), and failErr the
	// error it carried. A failure travels from where it is raised to a
	// handler or to the body exit by return, inside one task. The body exit
	// links a typed failure to it only when the exit's error is that same
	// error value. That holds for emitted code, which handles or returns
	// every failure it receives; hand-written Go that drops a failure and
	// returns an equal one would be linked to the dropped one.
	failure uint64
	failErr error
	// cancel is the task's first cancel-request, the origin of an
	// interruption in its exit.
	cancel uint64
	// exit is the task's body-exit occurrence and exitErr its exit, kept for
	// the owner's observation.
	exit    uint64
	exitErr error
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

// setFailure makes occ, which carried err, the latest failure of task id.
func (h *tracer) setFailure(id, occ uint64, err error) {
	h.mu.Lock()
	l := h.tasks[id]
	l.failure, l.failErr = occ, err
	h.tasks[id] = l
	h.mu.Unlock()
}

// takeFailure returns and clears the latest failure of task id.
func (h *tracer) takeFailure(id uint64) uint64 {
	h.mu.Lock()
	l, ok := h.tasks[id]
	occ := l.failure
	if ok && occ != 0 {
		l.failure, l.failErr = 0, nil
		h.tasks[id] = l
	}
	h.mu.Unlock()
	return occ
}

// setCancel records the first cancel-request of task id.
func (h *tracer) setCancel(id, occ uint64) {
	h.mu.Lock()
	l := h.tasks[id]
	if l.cancel == 0 {
		l.cancel = occ
		h.tasks[id] = l
	}
	h.mu.Unlock()
}

// openCall pushes c on the open calls of task id.
func (h *tracer) openCall(id uint64, c openCall) {
	h.mu.Lock()
	l := h.tasks[id]
	l.calls = append(l.calls, c)
	h.tasks[id] = l
	h.mu.Unlock()
}

// closeCall removes call from the open calls of task id and reports whether
// it was open. An unwind may have closed it already.
func (h *tracer) closeCall(id, call uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.tasks[id]
	for i := len(l.calls) - 1; i >= 0; i-- {
		if l.calls[i].id == call {
			l.calls = append(l.calls[:i], l.calls[i+1:]...)
			h.tasks[id] = l
			return true
		}
	}
	return false
}

// unwound removes and returns the open calls of task id above floor, the
// calls an unwind recovered by a region that began at that depth leaves.
// The returned slice is valid until the task opens another call.
func (h *tracer) unwound(id uint64, floor int) []openCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.tasks[id]
	if !ok || floor >= len(l.calls) {
		return nil
	}
	floor = max(floor, 0)
	open := l.calls[floor:]
	l.calls = l.calls[:floor]
	h.tasks[id] = l
	return open
}

// origins returns the per-reason links of task id's exit err, in the order
// rttrace.Reasons lists the reasons.
func (h *tracer) origins(id uint64, err error) []uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.tasks[id]
	// The latest failure is the origin of at most one reason: of an exit
	// that holds two equal failures, the first.
	failure := func(e error) uint64 {
		if occ := l.failure; occ != 0 && sameError(l.failErr, e) {
			l.failure = 0
			return occ
		}
		return 0
	}
	switch c := err.(type) {
	case nil:
		return nil
	case *Cause:
		if c == nil {
			return []uint64{failure(err)}
		}
		out := make([]uint64, len(c.Reasons))
		for i, r := range c.Reasons {
			switch r.Kind {
			case KindFailure:
				out[i] = failure(r.Err)
			case KindInterruption:
				out[i] = l.cancel
			case KindDefect:
				if len(r.Chain) > 0 {
					out[i] = h.defects[&r.Chain[0]]
				}
			}
		}
		return out
	}
	if err == Interrupted {
		return []uint64{l.cancel}
	}
	return []uint64{failure(err)}
}

// sameError reports whether a and b are the same error value. An error whose
// dynamic type cannot be compared is never the same.
func sameError(a, b error) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

// exited replaces the entry of task id by its body-exit.
func (h *tracer) exited(id, exit uint64, err error) {
	h.mu.Lock()
	h.tasks[id] = taskLinks{exit: exit, exitErr: err}
	h.mu.Unlock()
}

// takeExit removes the entry of task id and returns its body-exit.
func (h *tracer) takeExit(id uint64) (exit uint64, err error) {
	h.mu.Lock()
	l := h.tasks[id]
	delete(h.tasks, id)
	h.mu.Unlock()
	return l.exit, l.exitErr
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
		t.trace = &tracer{sink: tr, tasks: map[uint64]taskLinks{}, defects: map[*Frame]uint64{}}
	}
	return t
}

// Traced reports whether t reports events to a tracer.
func (t *Task) Traced() bool { return t.trace != nil }

// Trace reports an event that emitted code raises at a source site: a
// handler, a construct or an interrupt. def is a definition ID and site a
// source site, both compile-time constants. A handler event links the
// failure it caught. A capability call goes through TraceCall and
// TraceCallExit instead, and any other kind is not raised. With no tracer it
// costs one load and compare.
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
// returned err (nil for ok). A typed failure becomes the task's latest
// failure, the one a handler or the body exit links. A call an unwind has
// already closed is not reported again. With no tracer it costs one load and
// compare.
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
	case TraceConstruct, TraceInterrupt:
		t.emit(&Event{Kind: k, Def: def, Site: site})
	case TraceHandler:
		caught := t.trace.takeFailure(t.id)
		t.emit(&Event{Kind: k, Link: caught, Def: def, Site: site})
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
	if !t.trace.closeCall(t.id, call) {
		return
	}
	id := t.emit(&Event{Kind: TraceCallExit, Link: call, Def: def, Site: site, Cause: err})
	if err != nil && err != Interrupted && id != 0 {
		t.trace.setFailure(t.id, id, err)
	}
}

// traceTask raises a task event of t: start, cancel-request or release.
//
//go:noinline
func (t *Task) traceTask(k EventKind, cause error) {
	id := t.emit(&Event{Kind: k, Def: t.span, Cause: cause})
	if k == TraceCancelRequest && id != 0 {
		t.trace.setCancel(t.id, id)
	}
}

// traceExit raises the body-exit event of t with the origin of each reason
// of its exit, and keeps the exit for the owner's observation.
//
//go:noinline
func (t *Task) traceExit(cause error) {
	origins := t.trace.origins(t.id, cause)
	id := t.emit(&Event{Kind: TraceBodyExit, Origins: origins, Def: t.span, Cause: cause})
	t.trace.exited(t.id, id, cause)
}

// traceUnwind closes, innermost first, the capability calls that the panic p
// unwound before a region of t that began at depth floor recovered it: the
// calls opened inside that region, never an outer one. Each call-exit carries
// the outcome the recovery gives p: the transported exit, an interruption, or
// a defect. It returns the innermost call-exit, 0 if none.
//
//go:noinline
func (t *Task) traceUnwind(p any, floor int) uint64 {
	open := t.trace.unwound(t.id, floor)
	var first uint64
	for i := len(open) - 1; i >= 0; i-- {
		c := open[i]
		e := Event{Kind: TraceCallExit, Link: c.id, Def: c.def, Site: c.site}
		switch v := p.(type) {
		case transport:
			if v.err != nil {
				e.Cause = v.err
			} else {
				e.Defect = p
			}
		case error:
			if v == Interrupted {
				e.Cause = Interrupted
			} else {
				e.Defect = p
			}
		default:
			e.Defect = p
		}
		if id := t.emit(&e); first == 0 {
			first = id
		}
	}
	return first
}

// traceDefect raises the panic event of a defect recorded in t, linking left,
// the innermost call the panic unwound, and keys the event by the defect's
// chain so the exit that carries the defect links it.
//
//go:noinline
func (t *Task) traceDefect(p any, inRelease bool, left uint64, key *Frame) {
	id := t.emit(&Event{Kind: TracePanic, Link: left, Def: t.span, Defect: p, InRelease: inRelease})
	if id != 0 && key != nil {
		t.trace.mu.Lock()
		t.trace.defects[key] = id
		t.trace.mu.Unlock()
	}
}

// childEvent delivers an event of child t raised by its owner's goroutine: an
// observation or a deadline. The owner is named explicitly, because a
// finished child has dropped its parent link. A deadline becomes the owner's
// latest failure: the owner receives Expired. An observation links the
// child's body-exit and ends the child's entry; with carry set, an exit that
// is one typed failure becomes the owner's latest failure, because the owner
// receives that exit as its own error. Call it only when t.trace is set.
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
			h.setFailure(owner.id, id, Expired{})
		}
		return
	}
	exit, err := h.takeExit(t.id)
	e.Link = exit
	h.raise(&e)
	if carry && exit != 0 && owner != nil {
		if _, ok := Failure(err); ok {
			h.setFailure(owner.id, exit, err)
		}
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
