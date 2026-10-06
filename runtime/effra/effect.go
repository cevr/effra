// Package effra implements the managed execution boundary used by generated Go.
package effra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Unit = struct{}
type Failure struct {
	Tag     string
	Payload any
}

// Cause preserves all failures, defects, and interruption reasons, including cleanup.
type Reason struct {
	Kind    string
	Failure *Failure
	Err     error
}
type Cause []Reason

func (c Cause) Error() string {
	parts := []string{}
	for _, r := range c {
		if r.Failure != nil {
			parts = append(parts, r.Failure.Tag)
		} else {
			parts = append(parts, fmt.Sprintf("%s: %v", r.Kind, r.Err))
		}
	}
	return strings.Join(parts, "; ")
}
func (c Cause) OnlyInterrupts() bool {
	if len(c) == 0 {
		return false
	}
	for _, r := range c {
		if r.Kind != "interrupt" {
			return false
		}
	}
	return true
}

type Exit[A any] struct {
	Value        A
	Failure      *Failure
	Defect       error
	Interrupted  bool
	InterruptErr error
	Additional   Cause
}

func Succeed[A any](value A) Exit[A]              { return Exit[A]{Value: value} }
func Fail[A any](tag string, payload any) Exit[A] { return Exit[A]{Failure: &Failure{tag, payload}} }
func Die[A any](err error) Exit[A]                { return Exit[A]{Defect: err} }
func Interrupt[A any](err error) Exit[A]          { return FromCause[A](Cause{{Kind: "interrupt", Err: err}}) }
func (e Exit[A]) IsFailure() bool {
	return e.Failure != nil || e.Defect != nil || e.Interrupted || len(e.Additional) > 0
}
func (e Exit[A]) Cause() Cause {
	out := Cause{}
	if e.Failure != nil {
		out = append(out, Reason{Kind: "failure", Failure: e.Failure})
	}
	if e.Defect != nil {
		out = append(out, Reason{Kind: "defect", Err: e.Defect})
	}
	if e.Interrupted {
		out = append(out, Reason{Kind: "interrupt", Err: e.InterruptErr})
	}
	return append(out, e.Additional...)
}
func FromCause[A any](cause Cause) Exit[A] {
	out := Exit[A]{}
	for _, r := range cause {
		switch {
		case r.Kind == "failure" && out.Failure == nil && out.Defect == nil && !out.Interrupted:
			out.Failure = r.Failure
		case r.Kind == "defect" && out.Failure == nil && out.Defect == nil && !out.Interrupted:
			out.Defect = r.Err
		case r.Kind == "interrupt" && out.Failure == nil && out.Defect == nil && !out.Interrupted:
			out.Interrupted = true
			out.InterruptErr = r.Err
		default:
			out.Additional = append(out.Additional, r)
		}
	}
	return out
}
func Propagate[B, A any](exit Exit[A]) Exit[B] { return FromCause[B](exit.Cause()) }
func withCleanup[A any](exit Exit[A], cleanup Cause) Exit[A] {
	if len(cleanup) == 0 {
		return exit
	}
	return FromCause[A](append(exit.Cause(), cleanup...))
}

type Effect[A any] func(*FiberContext) Exit[A]
type FiberContext struct {
	ctx    context.Context
	scope  *Scope
	driver timerDriver
	turn   *TestScheduler
}

func (f *FiberContext) Context() context.Context { return f.ctx }
func (f *FiberContext) Scope() *Scope            { return f.scope }
func (f *FiberContext) Checkpoint() error        { return f.ctx.Err() }
func (f *FiberContext) timerDriver() timerDriver {
	if f.driver != nil {
		return f.driver
	}
	if f.scope != nil && f.scope.driver != nil {
		return f.scope.driver
	}
	return liveTimerDriver{}
}

func (f *FiberContext) turnScheduler() *TestScheduler {
	if f == nil {
		return nil
	}
	return f.turn
}

func (f *FiberContext) suspendScheduler() func() {
	if f == nil || f.turn == nil {
		return func() {}
	}
	if scheduler, ok := f.timerDriver().(*TestScheduler); !ok || scheduler != f.turn {
		return func() {}
	}
	return f.turn.suspend()
}

// UseTestScheduler installs an explicit virtual timer driver for the current
// execution boundary and returns a restoration function for the provider
// boundary that installed it.
func (f *FiberContext) UseTestScheduler(scheduler *TestScheduler) func() {
	previous := f.driver
	if scheduler == nil {
		f.driver = liveTimerDriver{}
	} else {
		f.driver = scheduler
	}
	return func() { f.driver = previous }
}

// CurrentTestScheduler returns the virtual driver active at this execution
// boundary, when one is installed by a test harness or provider.
func CurrentTestScheduler(f *FiberContext) *TestScheduler {
	if f == nil {
		return nil
	}
	if scheduler, ok := f.timerDriver().(*TestScheduler); ok {
		return scheduler
	}
	return nil
}
func Invoke[A any](f *FiberContext, program Effect[A]) (exit Exit[A]) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			exit = Die[A](fmt.Errorf("panic: %v", panicValue))
		}
	}()
	if err := f.Checkpoint(); err != nil {
		return Interrupt[A](err)
	}
	exit = program(f)
	if err := f.Checkpoint(); err != nil && !exit.IsFailure() {
		return Interrupt[A](err)
	}
	return exit
}
func runScope[A any](scope *Scope, program Effect[A], admitted ...bool) Exit[A] {
	fc := &FiberContext{ctx: scope.ctx, scope: scope, driver: scope.driver}
	var finish func()
	if scheduler, ok := scope.driver.(*TestScheduler); ok {
		fc.turn = scheduler
		alreadyAdmitted := len(admitted) > 0 && admitted[0]
		finish = scheduler.enter(alreadyAdmitted)
		defer finish()
	}
	return withCleanup(Invoke(fc, program), scope.closeWithContext(fc))
}
func Run[A any](program Effect[A]) Exit[A] { return RunContext(context.Background(), program) }
func RunContext[A any](ctx context.Context, program Effect[A]) Exit[A] {
	return runScope(newScope(ctx, nil), program)
}

// RunContextWithScheduler executes a program with a fresh owner using the
// supplied virtual timer driver. The driver is inherited by nested scopes and
// owned fibers, so Sleep and Timeout share the same logical time.
func RunContextWithScheduler[A any](ctx context.Context, scheduler *TestScheduler, program Effect[A]) Exit[A] {
	if scheduler == nil {
		return Die[A](errors.New("nil test scheduler"))
	}
	return runScope(newScopeWithDriver(ctx, nil, scheduler), program)
}
func Scoped[A any](program Effect[A]) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		scope := newScopeWithDriver(fc.ctx, fc.scope, fc.timerDriver())
		child := &FiberContext{ctx: scope.ctx, scope: scope, driver: scope.driver, turn: fc.turn}
		return withCleanup(Invoke(child, program), scope.closeWithContext(child))
	}
}
func Catch[A any](program Effect[A], tag string, fallback func() A) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		out := Invoke(fc, program)
		if out.Failure != nil && out.Failure.Tag == tag && out.Defect == nil && !out.Interrupted && len(out.Additional) == 0 {
			return Succeed(fallback())
		}
		return out
	}
}
func Sleep(milliseconds int64) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		if milliseconds < 0 || milliseconds > maxMilliseconds {
			return Die[Unit](fmt.Errorf("invalid millisecond duration"))
		}
		timer := fc.timerDriver().newTimer(time.Duration(milliseconds) * time.Millisecond)
		defer timer.Stop()
		resume := fc.suspendScheduler()
		select {
		case <-timer.C():
			resume()
			if scheduler, ok := fc.timerDriver().(*TestScheduler); ok {
				scheduler.resumeWait(timer.(*virtualTimer))
			}
			timer.acknowledge()
			return Succeed(Unit{})
		case <-fc.ctx.Done():
			resume()
			if scheduler, ok := fc.timerDriver().(*TestScheduler); ok {
				scheduler.resumeWait(timer.(*virtualTimer))
			}
			return Interrupt[Unit](fc.ctx.Err())
		}
	}
}
func Timeout[A any](program Effect[A], milliseconds int64) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		if milliseconds < 0 || milliseconds > maxMilliseconds {
			return Die[A](fmt.Errorf("invalid millisecond duration"))
		}
		driver := fc.timerDriver()
		timer := driver.newTimer(time.Duration(milliseconds) * time.Millisecond)
		defer timer.Stop()
		ctx, cancel := context.WithCancel(fc.ctx)
		defer cancel()
		childScope := newScopeWithDriver(ctx, fc.scope, driver)
		childDone := make(chan Exit[A], 1)
		scheduler, virtual := driver.(*TestScheduler)
		if virtual {
			scheduler.reserve()
		}
		go func() { childDone <- runScope(childScope, program, virtual) }()
		suspend := fc.suspendScheduler()
		select {
		case out := <-childDone:
			suspend()
			if virtual {
				scheduler.resumeWait(timer.(*virtualTimer))
			}
			return out
		case <-timer.C():
			suspend()
			if virtual {
				scheduler.resumeWait(timer.(*virtualTimer))
			}
			timer.acknowledge()
			cancel()
			childScope.cancel()
			wait := fc.suspendScheduler()
			out := <-childDone
			wait()
			retained := Cause{}
			for _, reason := range out.Cause() {
				if reason.Kind != "interrupt" {
					retained = append(retained, reason)
				}
			}
			return FromCause[A](append(Cause{{Kind: "failure", Failure: &Failure{Tag: "Timeout", Payload: context.DeadlineExceeded}}}, retained...))
		case <-fc.ctx.Done():
			suspend()
			if virtual {
				scheduler.resumeWait(timer.(*virtualTimer))
			}
			cancel()
			childScope.cancel()
			wait := fc.suspendScheduler()
			out := <-childDone
			wait()
			retained := Cause{}
			for _, reason := range out.Cause() {
				if reason.Kind != "interrupt" {
					retained = append(retained, reason)
				}
			}
			return FromCause[A](append(Cause{{Kind: "interrupt", Err: fc.ctx.Err()}}, retained...))
		}
	}
}
