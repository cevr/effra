package v1

import (
	"runtime/debug"

	"ergo.local/prototype/runtime/rt"
)

// The bodies of the std packages task and scope (docs/spec/runtime.md,
// Tasks and Exit actions), over runtime/rt's task tree. rt gives the tree,
// the stop flag and its cascade, masks, causes and the drain of a task its
// owner did not consume; this file adds what Ergo's source contract needs
// on top: the frame scope of one source function, a defect's stack, and a
// Wait that returns a typed failure and re-raises everything else.

// Scope is the frame scope of one source function: a declared function, a
// function literal or the block of task.Go. The lowering makes one in each
// frame that starts a task or registers an exit action, and defers its End
// as the frame's first defer:
//
//	s := ergo.NewScope(t)
//	defer s.End(&err) // or s.End(nil) in a frame with no error row
//
// The frame's tasks and its exit actions are entries of one stack, released
// last registered first when the frame returns, on every path: so a task
// started after an exit action is stopped and drained before that action
// runs, and an action registered before a task runs after the task's own
// cleanup (laws R1, R2; structured lend, resources.md R27).
type Scope struct {
	s     rt.Scope
	ended bool
	// exits holds the exits that exit actions re-raised from recorded exits
	// (a task's defects through Wait or Stop): rt's release recovery would
	// record such a carrier as one opaque defect, so they join the frame's
	// exit here, flat, beside rt's own release reasons.
	exits rt.Cause
}

// NewScope is the frame scope of a frame that runs on task t.
func NewScope(t *rt.Task) Scope { return Scope{s: rt.NewScope(t)} }

// End ends the frame. Defer it directly, so its recover sees the frame's
// panic. It records that panic (a fresh defect with its stack, a carried
// exit or an interruption) beside *err, then stops and drains each task the
// frame did not consume and runs each exit action, last registered first,
// each masked (I2), keeping every defect they raise (Exit causes 4). A stop
// the task can observe beats success (Exit causes 5). Last it hands the
// exit to the frame: a lone typed failure into *err, which must be the
// frame's named error result and nil in a frame with no error row; any
// other exit is raised as a panic for the caller's boundary.
func (s *Scope) End(err *error) {
	p := recover()
	var e error
	if err != nil {
		e = *err
	}
	if p != nil {
		e = join(e, capture(s.s.Task(), p))
	}
	s.ended = true
	s.s.End(&e)
	if len(s.exits.Reasons) > 0 {
		e = join(e, s.exits.Err())
	}
	settle(err, e)
}

// OnExit is scope.OnExit() { body }: it registers body on the frame, once
// per call, as Go's defer does. body is the lowered block, a closure, so it
// reads its variables when it runs (Exit actions 4). It runs masked when
// the frame ends, on every path; a defect in it joins the frame's exit as a
// release defect, with its stack, and the frame's other reasons stay.
func (s *Scope) OnExit(body func()) {
	s.s.Defer(func() {
		defer func() {
			if p := recover(); p != nil {
				if x, ok := p.(*exit); ok {
					s.exits.Add(x.err)
					return
				}
				if e, ok := p.(error); ok && e == rt.Interrupted {
					panic(p)
				}
				panic(&Panic{Value: p, Stack: debug.Stack()})
			}
		}()
		body()
	})
}

// Task is a started task, Ergo's Task[T, E]. Its methods are the prelude's
// operations, Wait() T fails E and Stop(), in the uniform Go shape, so
// *Task[T] is a value of the operations interface the lowering emits for
// Task[T, E]. Like rt's Handle, a Task belongs to the task that started it:
// only that task's goroutine may Wait for it or Stop it.
type Task[T any] struct {
	h    *rt.Handle[T]
	used bool // Wait or Stop ran
}

// Go is task.Go() { body }: it starts body as a child of the frame's task,
// on its own goroutine, owned by the frame scope s, and returns at once
// (parse-v1 ruling 5: the block runs later). span is the task's Ergo
// identity, the block's definition and source site, which a defect chain
// names (G2). The child starts interruptible, even inside an exit action
// (rt.Go). If the frame returns without consuming the task, End stops and
// drains it. Starting a task on a frame that has returned is a defect (law
// R9): its owner could never drain it.
func Go[T any](s *Scope, span string, body func(*rt.Task) (T, error)) *Task[T] {
	if s.ended {
		panic("task.Go: the frame that would own this task has returned (law R9)")
	}
	h := rt.Go(&s.s, span, func(t *rt.Task) (T, error) { return guarded(t, body) })
	return &Task[T]{h: h}
}

// Wait waits for the task to end and consumes it. It returns the task's
// value, or its typed failure when that failure is the task's whole exit
// (law R6: a catch handles only a lone typed failure). Any other exit is
// raised in the waiter: a defect with the chain and stack recorded in the
// child (Tasks 4), and an interruption. Wait is an interruption point of
// the waiter (I1): when the waiter is stopped first or while it waits, the
// task is stopped and drained, and Wait raises the interruption with the
// task's defects; it delivers neither the task's value nor its typed
// failure (law R4). A Wait after Wait or Stop is a defect.
func (t *Task[T]) Wait() (T, error) {
	if t.used {
		panic("task: Wait on a task that was already waited for or stopped")
	}
	t.used = true
	v, err := t.h.Wait()
	if err == nil {
		return v, nil
	}
	if f, ok := rt.Failure(err); ok {
		var zero T
		return zero, f
	}
	raise(err)
	panic("unreachable")
}

// Stop interrupts the task and waits, uninterruptibly, until it has ended,
// its exit actions included (law R5), and consumes it. The task's
// interruption and an abandoned typed failure are the expected result of
// the stop and are dropped (laws R3, R6); its defects are raised in the
// caller. Stop after Wait or Stop does nothing.
func (t *Task[T]) Stop() {
	if t.used {
		return
	}
	t.used = true
	if err := t.h.Stop(); err != nil {
		raise(err)
	}
}
