package v1

import (
	"ergo.local/prototype/runtime/rt"
)

// The bodies of the std packages task and scope (docs/spec/runtime.md,
// Tasks and Exit actions), over runtime/rt's task tree. rt gives the tree,
// the stop flag and its cascade, masks, causes and the drain of a task;
// this file adds what Ergo's source contract needs on top: the frame scope
// of one source function, the owner of each Task and its transfer by
// return, a defect's stack, and a Wait that returns a typed failure and
// re-raises everything else.

// Scope is the frame scope of one source function: a declared function, a
// function literal, the block of task.Go or the block of an exit action.
// The lowering makes one in each frame that starts a task, registers an exit
// action or receives a Task by return, and defers its End as the frame's
// first defer:
//
//	s := ergo.NewScope(t)
//	defer s.End(&err) // or s.End(nil) in a frame with no error row
//
// The tasks the frame owns and its exit actions are entries of one list,
// released last registered first when the frame returns, on every path: so
// a task started after an exit action is stopped and drained before that
// action runs, and an action registered before a task runs after the
// task's own cleanup (laws R1, R2; structured lend, resources.md R27).
// A Task's entry leaves the list when the Task is consumed (Wait, Stop) or
// transferred (Move), so the list holds only what the frame still owns.
type Scope struct {
	s     rt.Scope
	ended bool
	// last is the newest entry; n counts the entries.
	last *entry
	n    int
	// exits holds the exits of the entries: each exit action recovers its
	// own panic and its frame's exits, and a drained task's defects join
	// here, flat, in release order.
	exits rt.Cause
	// action is the frame of the exit action this frame runs now: a task
	// or an exit action that code at this frame starts or registers belongs
	// to it, through its whole run and its own release.
	action *Scope
}

// entry is one entry of a frame's list: an exit action or an owned task.
type entry struct {
	prev, next *entry
	action     func()
	task       owned
}

// owned is the side of a Task its owner drains.
type owned interface {
	// drain stops the task and waits until it has ended, and consumes it;
	// it returns the task's defects.
	drain() error
}

// NewScope is the frame scope of a frame that runs on task t.
func NewScope(t *rt.Task) Scope { return Scope{s: rt.NewScope(t)} }

// current is the frame that owns what the code at s starts now: the frame
// of the running exit action, followed down through nested ones, else s.
func (s *Scope) current() *Scope {
	for s.action != nil {
		s = s.action
	}
	return s
}

// push adds e as the newest entry.
func (s *Scope) push(e *entry) {
	e.prev, e.next = s.last, nil
	if s.last != nil {
		s.last.next = e
	}
	s.last = e
	s.n++
}

// remove takes e out of the list.
func (s *Scope) remove(e *entry) {
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.last = e.prev
	}
	e.prev, e.next = nil, nil
	s.n--
}

// End ends the frame. Defer it directly, so its recover sees the frame's
// panic. It records that panic (a fresh defect with its stack, a carried
// exit or an interruption) beside *err, then releases the entries, last
// first, masked (I2): it stops and drains each task the frame still owns
// and runs each exit action, keeping every defect they raise (Exit causes
// 4). A stop the task can observe beats success (Exit causes 5). Last it
// hands the exit to the frame: a lone typed failure into *err, which must be
// the frame's named error result and nil in a frame with no error row; any
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
	settle(err, s.end(e))
}

// end ends the scope with the exit e so far and returns the whole exit.
func (s *Scope) end(e error) error {
	s.ended = true
	if s.last != nil {
		m := s.s.Task().Mask()
		for s.last != nil {
			x := s.last
			s.remove(x)
			if x.task != nil {
				if err := x.task.drain(); err != nil {
					s.exits.Add(err)
				}
			} else {
				x.action() // it recovers its own panic
			}
		}
		m.Unmask()
	}
	if len(s.exits.Reasons) > 0 {
		e = join(e, s.exits.Err())
	}
	s.s.End(&e) // stop wins (Exit causes 5); rt holds no entry of this frame
	return e
}

// OnExit is scope.OnExit() { body }: it registers body on the frame, once
// per call, as Go's defer does. body is the lowered block, a closure, so it
// reads its variables when it runs (Exit actions 4). It runs masked when
// the frame ends, on every path. The block is a frame of its own for the
// tasks it starts and the exit actions it registers, nested ones included:
// they end when the block ends (Tasks rule 8). A defect in it joins the
// frame's exit as a release defect, with its stack, and the frame's other
// reasons stay. Registering on a frame that has returned is a defect.
func (s *Scope) OnExit(body func()) {
	s = s.current()
	if s.ended {
		panic("scope.OnExit: the frame that would run this exit action has returned")
	}
	t := s.s.Task()
	s.push(&entry{action: func() {
		action := &Scope{s: rt.NewScope(t)}
		prev := s.action
		s.action = action
		// The route to the action's frame stays through the block and
		// through the frame's own release, where nested exit actions run.
		defer func() { s.action = prev }()
		var e error
		func() {
			defer func() {
				if p := recover(); p != nil {
					e = captureIn(t, p, true)
				}
			}()
			body()
		}()
		if e = action.end(e); e != nil {
			s.exits.Add(e)
		}
	}})
}

// Task is a started task, Ergo's Task[T, E]. Its methods are the prelude's
// operations, Wait() T fails E and Stop(), in the uniform Go shape, so
// *Task[T] is a value of the operations interface the lowering emits for
// Task[T, E]. A Task belongs to the task that started it: only that task's
// goroutine may Wait for it, Stop it or Move it. Its owner is one frame at
// a time: the frame that started it, until a return transfers it.
type Task[T any] struct {
	h       *rt.Handle[T]
	task    *rt.Task // the task that started it
	owner   *Scope   // the frame that drains it, nil once consumed
	reg     *entry   // its entry in owner's list, nil once consumed
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
	// rt registers the handle on the scope it is given. That scope is a
	// throwaway: the owner drains the task through its own entry, which a
	// transfer moves and a consumption removes.
	holder := rt.NewScope(s.s.Task())
	t := &Task[T]{task: s.s.Task(), owner: s}
	t.h = rt.Go(&holder, span, func(ct *rt.Task) (T, error) { return guarded(ct, body) })
	t.reg = &entry{task: t}
	s.push(t.reg)
	return t
}

// Move transfers the task to the frame scope to, in one step, and returns
// it. It is the return of a Task: a function whose result holds a Task
// receives its caller's frame scope as a hidden destination argument, and
// the lowering writes `return c.Move(dest)`. In an exit action, the
// destination is the action's frame. The task leaves its old owner's list,
// so neither frame keeps a reference to it once it is consumed. A move into
// a frame of another task (law R9; #197), into a frame that has returned or
// to no frame is a defect. Move on a consumed task does nothing.
func (t *Task[T]) Move(to *Scope) *Task[T] {
	if t.used {
		return t
	}
	if to == nil {
		panic("task: Move with no destination frame: a function that returns a Task receives its caller's frame")
	}
	to = to.current()
	switch {
	case to.s.Task() != t.task:
		panic("task: a Task moves into another task with #197 (law R9)")
	case to.ended:
		panic("task: a Task moves into a frame that has returned (law R9)")
	}
	t.owner.remove(t.reg)
	to.push(t.reg)
	t.owner = to
	return t
}

// consume takes the task out of its owner's list.
func (t *Task[T]) consume() {
	t.used = true
	t.owner.remove(t.reg)
	t.owner, t.reg = nil, nil
}

// drain is the owner's release: its entry has left the list already.
func (t *Task[T]) drain() error {
	t.used, t.drained = true, true
	t.owner, t.reg = nil, nil
	return t.h.Stop()
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
		panic("task: Wait on a task its owner stopped when it ended; a returned Task moves to its caller (Move)")
	case t.used:
		panic("task: Wait on a task that was already waited for or stopped")
	}
	t.consume()
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
	t.consume()
	if err := t.h.Stop(); err != nil {
		raise(err)
	}
}
