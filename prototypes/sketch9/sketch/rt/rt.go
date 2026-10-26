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

// Task is one unit of work that can be stopped. A stopped parent stops its
// children. Code checks Stopped before each capability call.
type Task struct {
	parent *Task
	stop   chan struct{}
	once   sync.Once
}

// Root returns the task that runs main.
func Root() *Task { return &Task{stop: make(chan struct{})} }

// Child returns a task that stops when t stops or when it is stopped itself.
func (t *Task) Child() *Task { return &Task{parent: t, stop: make(chan struct{})} }

// Stop stops t and, through Stopped, every task below it.
func (t *Task) Stop() { t.once.Do(func() { close(t.stop) }) }

// Stopped reports whether t or any task above it was stopped.
func (t *Task) Stopped() bool {
	for ; t != nil; t = t.parent {
		select {
		case <-t.stop:
			return true
		default:
		}
	}
	return false
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
// parent stops first it does the same and returns Interrupted. A panic in the
// body is a defect: After re-panics with it in the parent.
func After[T any](s Scheduler, parent *Task, d time.Duration, body func(*Task) (T, error)) (T, error) {
	var zero T
	if parent.Stopped() {
		return zero, Interrupted
	}
	child := parent.Child()
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
	select {
	case o = <-done:
	case <-s.After(d):
		child.Stop()
		<-done
		return zero, Expired{}
	case <-parent.stop:
		child.Stop()
		<-done
		return zero, Interrupted
	}
	if o.defect != nil {
		panic(o.defect)
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
