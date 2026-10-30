package v1

import (
	"ergo.local/prototype/runtime/rt"
)

// The bodies of the std packages task and scope (docs/spec/runtime.md,
// Tasks and Exit actions), over runtime/rt's task tree. rt gives the tree,
// the stop flag and its cascade, masks, causes and the drain of a task;
// this file adds what Ergo's source contract needs on top: the frame scope
// of one source function, the owner of each Task and its moves, a defect's
// stack, and a Wait that returns a typed failure and re-raises everything
// else.

// taskScopeKey is the rt context key of the running task's task scope.
type taskScopeKey struct{}

// Scope is the frame scope of one source function: a declared function, a
// function literal or the block of task.Go. The lowering makes one in each
// frame that starts a task or registers an exit action, and defers its End
// as the frame's first defer:
//
//	s := ergo.NewScope(t)
//	defer s.End(&err) // or s.End(nil) in a frame with no error row
//
// The tasks the frame owns and its exit actions are entries of one stack,
// released last registered first when the frame returns, on every path: so
// a task started after an exit action is stopped and drained before that
// action runs, and an action registered before a task runs after the
// task's own cleanup (laws R1, R2; structured lend, resources.md R27).
//
// Each running task also has a task scope, made at the task's boundary: it
// owns the Tasks that moved out of their frame (Task.Move) and drains them
// when the task ends.
type Scope struct {
	s     rt.Scope
	ended bool
	// exits holds the exits of the frame's exit actions: each action
	// recovers its own panic and its tasks' exits, and they join the
	// frame's exit here, flat, beside rt's own release reasons.
	exits rt.Cause
	// root is the task scope of the task the frame runs on, nil for a
	// frame made outside a task boundary; a task scope is its own root.
	root *Scope
	// action is the frame of the exit action End is running now: a task
	// or an exit action the block starts or registers belongs to it.
	action *Scope
}

// NewScope is the frame scope of a frame that runs on task t.
func NewScope(t *rt.Task) Scope {
	root, _ := t.Value(taskScopeKey{}).(*Scope)
	return Scope{s: rt.NewScope(t), root: root}
}

// current is the frame that owns what the code at s starts now: the frame
// of a running exit action, else s.
func (s *Scope) current() *Scope {
	for s.action != nil {
		s = s.action
	}
	return s
}

// End ends the frame. Defer it directly, so its recover sees the frame's
// panic. It records that panic (a fresh defect with its stack, a carried
// exit or an interruption) beside *err, then stops and drains each task the
// frame owns and has not consumed, and runs each exit action, last
// registered first, each masked (I2), keeping every defect they raise
// (Exit causes 4). A stop the task can observe beats success (Exit causes
// 5). Last it hands the exit to the frame: a lone typed failure into *err,
// which must be the frame's named error result and nil in a frame with no
// error row; any other exit is raised as a panic for the caller's boundary.
func (s *Scope) End(err *error) {
	p := recover()
	var e error
	if err != nil {
		e = *err
	}
	if p != nil {
		e = join(e, capture(s.s.Task(), p))
	}
	settle(err, s.end(e))
}

// end ends the scope with the exit e so far and returns the whole exit.
func (s *Scope) end(e error) error {
	s.ended = true
	s.s.End(&e)
	if len(s.exits.Reasons) > 0 {
		e = join(e, s.exits.Err())
	}
	return e
}

// OnExit is scope.OnExit() { body }: it registers body on the frame, once
// per call, as Go's defer does. body is the lowered block, a closure, so it
// reads its variables when it runs (Exit actions 4). It runs masked when
// the frame ends, on every path. The block is a frame of its own for the
// tasks it starts and the exit actions it registers: they end when the
// block ends (Tasks rule 7). A defect in it joins the frame's exit as a
// release defect, with its stack, and the frame's other reasons stay.
func (s *Scope) OnExit(body func()) {
	s = s.current()
	t := s.s.Task()
	s.s.Defer(func() {
		action := &Scope{s: rt.NewScope(t), root: s.root}
		prev := s.action
		s.action = action
		var e error
		func() {
			defer func() {
				if p := recover(); p != nil {
					e = captureIn(t, p, true)
				}
			}()
			body()
		}()
		s.action = prev
		if e = action.end(e); e != nil {
			s.exits.Add(e)
		}
	})
}

// Task is a started task, Ergo's Task[T, E]. Its methods are the prelude's
// operations, Wait() T fails E and Stop(), in the uniform Go shape, so
// *Task[T] is a value of the operations interface the lowering emits for
// Task[T, E]. A Task belongs to the task that started it: only that task's
// goroutine may Wait for it, Stop it or Move it. Its owner is one scope at
// a time: the frame that started it, until it moves.
type Task[T any] struct {
	h       *rt.Handle[T]
	task    *rt.Task // the task that started it
	owner   *Scope   // the scope that drains it, if nobody consumes it
	used    bool     // Wait or Stop ran, or its owner drained it
	drained bool     // its owner stopped it when the owner ended
}

// Go is task.Go() { body }: it starts body as a child of the frame's task,
// on its own goroutine, and returns at once (parse-v1 ruling 5: the block
// runs later). The frame owns the task (in an exit action, the action's
// frame does). span is the task's Ergo identity, the block's definition
// and source site, which a defect chain names (G2). The child starts
// interruptible, except inside an exit action, where rt shields it from
// the ambient stop so the cleanup completes. If the owner ends without
// consuming the task, it stops and drains it. Starting a task on a frame
// that has returned is a defect (law R9): its owner could never drain it.
func Go[T any](s *Scope, span string, body func(*rt.Task) (T, error)) *Task[T] {
	s = s.current()
	if s.ended {
		panic("task.Go: the frame that would own this task has returned (law R9)")
	}
	// The handle's own rt scope is never ended: the owner drains the task
	// through the entry own registers, which a move can retarget.
	holder := new(rt.Scope)
	*holder = rt.NewScope(s.s.Task())
	t := &Task[T]{task: s.s.Task()}
	t.h = rt.Go(holder, span, func(ct *rt.Task) (T, error) { return guarded(ct, body) })
	t.own(s)
	return t
}

// own makes s the task's owner: s drains the task when it ends, unless the
// task was consumed or moved to another owner first.
func (t *Task[T]) own(s *Scope) {
	t.owner = s
	s.s.Defer(func() {
		if t.owner == s && !t.used {
			t.used, t.drained = true, true
			t.h.Close() // Stop; a defect is raised for the releasing scope
		}
	})
}

// Move moves the task out of the scope that owns it: to the frame scope
// to, or, with to nil, to the task scope of the task that runs it. The
// lowering moves a Task value that leaves its function, by return or into
// a field or a container, to the task scope; the caller that receives it
// by return may move it on into its own frame. A move into a frame of
// another task is a defect until #197's moves exist (law R9), and so is a
// move into a scope that has ended. Move on a consumed task does nothing.
func (t *Task[T]) Move(to *Scope) {
	if t.used {
		return
	}
	if to == nil {
		if t.owner == nil || t.owner.root == nil {
			panic("task: Move with no task scope: the frame was not made inside a task boundary")
		}
		to = t.owner.root
	}
	switch {
	case to.s.Task() != t.task:
		panic("task: a Task moves into another task with #197 (law R9)")
	case to.ended:
		panic("task: a Task moves into a scope that has ended (law R9)")
	}
	t.own(to)
}

// Wait waits for the task to end and consumes it. It returns the task's
// value, or its typed failure when that failure is the task's whole exit
// (law R6: a catch handles only a lone typed failure). Any other exit is
// raised in the waiter: a defect with the chain and stack recorded in the
// child (Tasks 4), and an interruption. Wait is an interruption point of
// the waiter (I1): when the waiter is stopped first or while it waits, the
// task is stopped and drained, and Wait raises the interruption with the
// task's defects; it delivers neither the task's value nor its typed
// failure (law R4). A Wait after Wait or Stop, or on a task its owner
// already stopped, is a defect.
func (t *Task[T]) Wait() (T, error) {
	switch {
	case t.drained:
		panic("task: Wait on a task its owner stopped when it ended; a Task that leaves its function moves (Move)")
	case t.used:
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
// caller. Stop on a consumed task does nothing.
func (t *Task[T]) Stop() {
	if t.used {
		return
	}
	t.used = true
	if err := t.h.Stop(); err != nil {
		raise(err)
	}
}
