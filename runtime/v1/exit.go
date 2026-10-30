package v1

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"ergo.local/prototype/runtime/rt"
)

// Exits in lowered code (docs/spec/runtime.md, Exit causes; Host and
// lowering notes). A lowered function returns a typed failure as its last
// Go result, and nothing else travels that way. Every other exit travels as
// a Go panic, so no catch, which is a type switch over returned errors, can
// handle it (rows.md, catch rule 6):
//
//   - rt.Interrupted: an interruption, raised by a stopped wait (a closed
//     queue is not one: Push and Pop return the typed failure Closed);
//   - *exit: an exit already recorded at a boundary (a child's defects
//     re-raised by Wait or Stop, a frame's combined exit), carried whole;
//   - any other value: a fresh defect, a Go panic of the program or of an
//     extern.
//
// A boundary (a task's body, a frame scope's End, an exit action, Main)
// recovers the panic and records it with rt: a fresh defect becomes a
// *Panic that holds the Go stack at the panic, so the //line directives of
// the lowered code still name the .ergo line of the fault after the panic
// has crossed tasks and frames.

// Panic is a defect: a Go panic recovered at a boundary. Value is the
// panic's value and Stack the panicking goroutine's stack when the boundary
// recovered it, which still holds the frame that panicked.
type Panic struct {
	Value any
	Stack []byte
}

// String is the panic value's text, as Go's panic report writes it.
func (p *Panic) String() string { return fmt.Sprint(p.Value) }

// exit carries a recorded exit, in rt's canonical form, across lowered
// frames as a panic value. Its err is never nil and never a lone typed
// failure that a caller could have received as a returned error, except
// where a frame with no error row meets one (which the checker rules out);
// even then it is kept, never dropped.
type exit struct{ err error }

// capture turns p, a panic recovered at a boundary of task t, into an exit
// in canonical form: an interruption stays one, a carried exit is unpacked,
// and anything else is a fresh defect with t's chain (G2) and the stack.
func capture(t *rt.Task, p any) error {
	defect, carried := classify(p)
	if defect == nil {
		return carried
	}
	var c rt.Cause
	c.AddDefect(t, defect)
	return c.Err()
}

// classify sorts p, a recovered panic: a carried exit and an interruption
// are returned as the exit they carry; anything else is a fresh defect,
// returned as its *Panic with the Go stack at the panic. Call it inside the
// recover, while the panicking frame is still on the stack. It is the
// settle of rt.Release for a release that ends no frame of its own (a
// task's drain).
func classify(p any) (defect any, carried error) {
	switch e := p.(type) {
	case *exit:
		return nil, e.err
	case error:
		if e == rt.Interrupted {
			return nil, rt.Interrupted
		}
	}
	return &Panic{Value: p, Stack: debug.Stack()}, nil
}

// join combines two exits, a's reasons first; nothing is dropped.
func join(a, b error) error {
	var c rt.Cause
	c.Add(a)
	c.Add(b)
	return c.Err()
}

// raise re-raises err, an exit that is not a lone typed failure, in the
// calling frame: an interruption as rt.Interrupted, anything else as an
// *exit that the next boundary unpacks whole.
func raise(err error) {
	if err == rt.Interrupted {
		panic(rt.Interrupted)
	}
	panic(&exit{err})
}

// settle hands the frame's exit e to the frame: a lone typed failure into
// *err when the frame has an error row (err not nil), nothing on success,
// and every other exit raised as a panic.
func settle(err *error, e error) {
	if e == nil {
		if err != nil {
			*err = nil
		}
		return
	}
	if err != nil {
		if f, ok := rt.Failure(e); ok {
			*err = f
			return
		}
	}
	raise(e)
}

// guarded runs body as the body of task t and returns every exit as an
// error: a panic is recovered here, before rt's own boundary sees it, so a
// fresh defect keeps its stack and a carried exit merges flat.
func guarded[T any](t *rt.Task, body func(*rt.Task) (T, error)) (v T, err error) {
	defer func() {
		if p := recover(); p != nil {
			var zero T
			v, err = zero, capture(t, p)
		}
	}()
	return body(t)
}

// Main runs body, the lowered main, as the root task of the program and
// exits with the status of its exit (Exit causes, rule 6): 0 on success, 1
// for a typed failure, 2 when any reason is a defect, as Go's own panic
// report does, and 130 when every reason is an interruption. Each reason is
// written on standard error.
func Main(body func(*rt.Task) error) {
	os.Exit(report(os.Stderr, runMain(body)))
}

// runMain runs body as the root task and returns its canonical exit.
func runMain(body func(*rt.Task) error) error {
	_, err := rt.Run(rt.Root("main"), func(t *rt.Task) (struct{}, error) {
		return guarded(t, func(t *rt.Task) (struct{}, error) { return struct{}{}, body(t) })
	})
	return err
}

// report writes each reason of err to w and returns the exit status. A
// typed failure is written as Fail writes it; a defect as "panic: VALUE",
// then the chain of tasks that led to it when it is not the root alone
// (G2), then the stack at the panic; an interruption as "interrupted".
func report(w io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var reasons []rt.Reason
	switch e := err.(type) {
	case *rt.Cause:
		reasons = e.Reasons
	default:
		if err == rt.Interrupted {
			reasons = []rt.Reason{{Kind: rt.KindInterruption}}
		} else {
			reasons = []rt.Reason{{Kind: rt.KindFailure, Err: err}}
		}
	}
	defect, interruptedOnly := false, true
	for _, r := range reasons {
		switch r.Kind {
		case rt.KindFailure:
			interruptedOnly = false
			fmt.Fprintf(w, "error: %v\n", r.Err)
		case rt.KindDefect:
			defect, interruptedOnly = true, false
			where := ""
			if r.InRelease {
				where = " (in an exit action)"
			}
			fmt.Fprintf(w, "panic: %v%s\n", r.Value, where)
			if len(r.Chain) > 1 {
				for _, f := range r.Chain {
					fmt.Fprintf(w, "\ttask #%d %s\n", f.Task, f.Span)
				}
			}
			if p, ok := r.Value.(*Panic); ok && len(p.Stack) > 0 {
				fmt.Fprintf(w, "\n%s\n", p.Stack)
			}
		case rt.KindInterruption:
			fmt.Fprintln(w, "interrupted")
		}
	}
	switch {
	case defect:
		return 2
	case interruptedOnly:
		return 130
	default:
		return 1
	}
}
