package rt

import (
	"sync"
	"sync/atomic"
)

// maxChain bounds the ancestry a defect records (G2, rule 3).
const maxChain = 32

var nextID atomic.Uint64

// Task is one unit of work in the task tree. A body receives its *Task and
// polls it at interruption points. The task's own goroutine is the only one
// that masks it, reads its context values or starts children from it; any
// goroutine may interrupt it.
type Task struct {
	// stopped is the stop flag, set by Interrupt from any goroutine.
	stopped atomic.Bool
	// regions is the stack of open mask regions (regions.go). Only the owner
	// goroutine reads or writes it, so masking costs no atomic operation.
	regions regionStack
	id      uint64
	span    string
	parent  atomic.Pointer[Task] // live link; cleared when t finishes
	values  *values              // G1 context; owner goroutine only

	// shielded is fixed before t is linked: t was started while its owner was
	// inside a masked region, so the owner's ambient stop does not cascade
	// into it. The cascade reads only this field, never the owner's mask.
	shielded bool

	mu    sync.Mutex
	stop  chan struct{} // made by Done on first use, closed by Interrupt
	first *Task         // first child, guarded by mu

	prev, next *Task // siblings, guarded by parent.mu
}

// Root returns a task with no parent: the task that runs main. span names it
// in defect chains.
func Root(span string) *Task {
	return &Task{id: nextID.Add(1), span: span}
}

// init links t as a child of parent. A child starts interruptible (pinned
// Effect 4: fork is interruptible unless asked to inherit); masked starts it
// inside one masked region, for GoMasked.
//
// A child is release-owned and shielded when the nearest enclosing masked
// region of parent that is not a restore is a protecting one: a release, a
// defer body or a GoMasked body, but not Acquire's registration step
// (regions.go). The ambient stop that cascades from parent does not reach it, whether the stop came before or
// after the start, so the release runs to completion (I2, R9). Its handle's
// Stop, a timeout and its scope's drain still interrupt it directly. This is
// Trio's shielded cancel scope and Effect's uninterruptible finalizer. Any
// other child starts stopped when parent is stopped.
func (t *Task) init(parent *Task, span string, masked bool) {
	t.id = nextID.Add(1)
	t.span = span
	t.parent.Store(parent)
	t.values = parent.values
	t.shielded = parent.regions.shields()
	if masked {
		t.regions.push(protecting)
	}
	parent.mu.Lock()
	if !t.shielded && parent.stopped.Load() {
		t.stopped.Store(true)
	}
	t.next = parent.first
	if t.next != nil {
		t.next.prev = t
	}
	parent.first = t
	parent.mu.Unlock()
}

// detach unlinks a finished child from its parent (runtime.md Tasks, rule 1)
// and drops its reference to the parent, so a finished task retains no
// ancestors (G2, rule 3). Every descendant has finished by now, so no chain
// walk can still pass through t.
func (t *Task) detach() {
	p := t.parent.Load()
	p.mu.Lock()
	if t.prev != nil {
		t.prev.next = t.next
	} else {
		p.first = t.next
	}
	if t.next != nil {
		t.next.prev = t.prev
	}
	t.prev, t.next = nil, nil
	p.mu.Unlock()
	t.parent.Store(nil)
}

// ID is the task's number, unique in the process.
func (t *Task) ID() uint64 { return t.id }

// Span is the span or definition ID the task was started under (G2).
func (t *Task) Span() string { return t.span }

// Parent is the task that started t while t runs, and nil for a root or a
// finished task. It is a live link, not a record: a defect keeps its ancestry
// as copied frames. Safe from any goroutine.
func (t *Task) Parent() *Task { return t.parent.Load() }

// Chain copies t and its live ancestors as frames, from t to the root, at
// most 32. Call it from t's goroutine while t runs; a defect records it then.
func (t *Task) Chain() []Frame {
	var frames []Frame
	for x := t; x != nil && len(frames) < maxChain; x = x.parent.Load() {
		frames = append(frames, Frame{Task: x.id, Span: x.span})
	}
	return frames
}

// Interrupt requests that t and every task below it stop, except the subtree
// of a shielded child (see init), which only a direct Interrupt reaches. It
// does not wait.
// Each task observes the request at its next interruption point outside a
// mask. Interrupt is idempotent and safe from any goroutine.
func (t *Task) Interrupt() {
	t.mu.Lock()
	if !t.stopped.Load() {
		t.stopped.Store(true)
		if t.stop != nil {
			close(t.stop)
		}
		// Locks are taken ancestor first; detach takes only the parent's
		// lock, so the walk cannot deadlock with a finishing child.
		for c := t.first; c != nil; c = c.next {
			if !c.shielded {
				c.Interrupt()
			}
		}
	}
	t.mu.Unlock()
}

// Stopped is the interruption point (I1): it reports whether t was asked to
// stop and is not masked. While no stop is requested it costs one atomic load;
// the region state it reads after that is owner-local.
func (t *Task) Stopped() bool { return t.stopped.Load() && t.regions.top == interruptible }

// StopRequested reports whether a stop was requested, masked or not.
func (t *Task) StopRequested() bool { return t.stopped.Load() }

// Masked reports whether t is inside a masked region. Call it from the task's
// own goroutine.
func (t *Task) Masked() bool { return t.regions.top != interruptible }

// Check is the interruption point for code with no error row: it panics with
// Interrupted when Stopped would return true. The task boundary recovers it.
func (t *Task) Check() {
	if t.stopped.Load() && t.regions.top == interruptible {
		panic(Interrupted)
	}
}

// Done returns a channel that is closed when a stop is requested, for a
// blocking wait that must also wake on a stop (I1, rule 5). Inside a masked
// region it returns nil, so the wait is uninterruptible (I2). Call it from the
// task's own goroutine.
func (t *Task) Done() <-chan struct{} {
	if t.regions.top != interruptible {
		return nil
	}
	t.mu.Lock()
	if t.stop == nil {
		t.stop = make(chan struct{})
		if t.stopped.Load() {
			close(t.stop)
		}
	}
	ch := t.stop
	t.mu.Unlock()
	return ch
}
