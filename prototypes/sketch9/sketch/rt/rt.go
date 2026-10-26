// Package rt is the runtime the sketch prototype copies into every program it
// emits. It holds tasks with cooperative interruption, the timeout combinator,
// and the host capabilities.
package rt

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Task is one unit of work that can be stopped. Stopping a task stops every
// task below it. Code checks Stopped before each capability call.
type Task struct {
	stop     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	children map[*Task]struct{}
	parent   *Task
}

// Root returns the task that runs main.
func Root() *Task { return &Task{stop: make(chan struct{})} }

// Child returns a task that stops when t, or any task above t, stops. Release
// it when its work ends.
func (t *Task) Child() *Task {
	c := &Task{stop: make(chan struct{}), parent: t}
	t.mu.Lock()
	if t.Stopped() {
		t.mu.Unlock()
		c.Stop()
		return c
	}
	if t.children == nil {
		t.children = map[*Task]struct{}{}
	}
	t.children[c] = struct{}{}
	t.mu.Unlock()
	return c
}

// release detaches a finished child from its parent.
func (t *Task) release() {
	if p := t.parent; p != nil {
		p.mu.Lock()
		delete(p.children, t)
		p.mu.Unlock()
	}
}

// Stop stops t and every task below it.
func (t *Task) Stop() {
	t.once.Do(func() {
		t.mu.Lock()
		close(t.stop)
		children := t.children
		t.children = nil
		t.mu.Unlock()
		for c := range children {
			c.Stop()
		}
	})
}

// Stopped reports whether t was stopped, directly or through an ancestor.
func (t *Task) Stopped() bool {
	select {
	case <-t.stop:
		return true
	default:
		return false
	}
}

// Check panics with Interrupted when t is stopped. Code that cannot fail calls
// it; a task boundary recovers the panic.
func (t *Task) Check() {
	if t.Stopped() {
		panic(Interrupted)
	}
}

type interrupted struct{}

func (interrupted) Error() string { return "interrupted" }

// Interrupted is the failure of a stopped task. It is not a typed error: no
// error row names it, and every catch passes it on.
var Interrupted error = interrupted{}

// Expired is timeout.Expired in source rows.
type Expired struct{}

func (Expired) Error() string { return "timeout.Expired" }

// Scheduler is the clock capability. Tests provide one they advance by hand.
type Scheduler interface {
	After(d time.Duration) <-chan time.Time
}

// LiveScheduler is the wall clock.
type LiveScheduler struct{}

func (LiveScheduler) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Console is the output capability the host provides to main.
type Console interface {
	Println(t *Task, args ...any)
}

// Stdout writes to standard output.
type Stdout struct{}

func (Stdout) Println(_ *Task, args ...any) { fmt.Println(args...) }

type outcome[T any] struct {
	v      T
	err    error
	defect any
}

// After runs body in a child task on its own goroutine. If the deadline wins
// it stops the child, waits for it to finish, and returns Expired. If the
// parent (or any ancestor) stops first, or stops while the body runs, it
// returns Interrupted. A panic in the body is a defect: After re-panics with it
// in the parent on every path, so no stop can hide it.
func After[T any](s Scheduler, parent *Task, d time.Duration, body func(*Task) (T, error)) (T, error) {
	var zero T
	if parent.Stopped() {
		return zero, Interrupted
	}
	child := parent.Child()
	defer child.release()
	done := make(chan outcome[T], 1)
	go func() {
		var o outcome[T]
		defer func() {
			if p := recover(); p != nil {
				if p == Interrupted {
					o.err = Interrupted
				} else {
					o.defect = p
				}
			}
			done <- o
		}()
		o.v, o.err = body(child)
	}()
	var o outcome[T]
	var cause error
	select {
	case o = <-done:
	case <-s.After(d):
		child.Stop()
		o = <-done
		cause = Expired{}
	case <-parent.stop:
		child.Stop()
		o = <-done
		cause = Interrupted
	}
	if o.defect != nil {
		panic(o.defect)
	}
	if cause == nil && parent.Stopped() {
		cause = Interrupted
	}
	if cause != nil {
		return zero, cause
	}
	return o.v, o.err
}

// Recover ends the program on a defect or an interruption that reached main.
// main defers it.
func Recover() {
	p := recover()
	if p == nil {
		return
	}
	if p == Interrupted {
		os.Exit(130)
	}
	fmt.Fprintln(os.Stderr, "defect:", p)
	os.Exit(2)
}
