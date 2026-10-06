package effra

import (
	"fmt"
	"sync/atomic"
)

type ownedFiber interface {
	requestCancel()
	closeResult() Cause
	closeResultManaged(*TestScheduler, *schedulerContinuation) Cause
	snapshot() FiberSnapshot
}

type FiberSnapshot struct {
	ScopeID               uint64 `json:"scopeId"`
	State                 string `json:"state"`
	CancellationRequested bool   `json:"cancellationRequested"`
	Observed              bool   `json:"observed"`
}

func (f *Fiber[A]) snapshot() FiberSnapshot {
	cancelled := f.scope.ctx.Err() != nil
	state := "Running"
	if cancelled {
		state = "Cancelling"
	}
	select {
	case <-f.done:
		state = "Done"
	default:
	}
	return FiberSnapshot{f.scope.id, state, cancelled, f.observed.Load()}
}

type Fiber[A any] struct {
	owner     *Scope
	scope     *Scope
	done      chan struct{}
	completed *managedSignal
	exit      Exit[A]
	observed  atomic.Bool
}

func (f *Fiber[A]) requestCancel() { f.scope.cancel() }
func (f *Fiber[A]) Cancel()        { f.requestCancel() }
func (f *Fiber[A]) closeResult() Cause {
	<-f.done
	if f.observed.Load() {
		return nil
	}
	return f.exit.Cause()
}
func (f *Fiber[A]) closeResultManaged(scheduler *TestScheduler, continuation *schedulerContinuation) Cause {
	waiter, complete := f.completed.register(scheduler)
	if !complete {
		if scheduler != nil {
			scheduler.parkContinuation(continuation)
		}
		<-waiter.done
		if scheduler != nil {
			scheduler.unparkContinuation(continuation)
		}
		f.completed.consume(waiter)
	} else {
		f.completed.consume(waiter)
	}
	if f.observed.Load() {
		return nil
	}
	return f.exit.Cause()
}
func (f *Fiber[A]) accessible(fc *FiberContext) bool {
	for scope := fc.scope; scope != nil; scope = scope.parent {
		if scope == f.owner {
			return f.owner.IsOpen()
		}
	}
	return false
}
func Fork[A any](program Effect[A]) Effect[*Fiber[A]] {
	return func(fc *FiberContext) Exit[*Fiber[A]] {
		owner := fc.scope
		owner.mu.Lock()
		if owner.state != Open {
			owner.mu.Unlock()
			return Die[*Fiber[A]](fmt.Errorf("scope is closing"))
		}
		completed := newManagedSignal()
		f := &Fiber[A]{owner: owner, scope: newScopeWithDriver(fc.ctx, owner, fc.timerDriver()), completed: completed, done: completed.done}
		owner.children = append(owner.children, f)
		owner.mu.Unlock()
		_, virtual := fc.timerDriver().(*TestScheduler)
		if virtual {
			fc.timerDriver().(*TestScheduler).reserve()
		}
		go func() {
			runScopeWithCompletion(f.scope, program, virtual, func(exit Exit[A]) {
				f.exit = exit
				f.completed.signal()
			})
		}()
		return Succeed(f)
	}
}
func (f *Fiber[A]) Join() Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		if !f.accessible(fc) {
			return Die[A](fmt.Errorf("fiber owner is closed or not an ancestor"))
		}
		scheduler := fc.turnScheduler()
		waiter, complete := f.completed.register(scheduler)
		if complete {
			f.completed.consume(waiter)
			f.observed.Store(true)
			return f.exit
		}
		if scheduler == nil {
			select {
			case <-waiter.done:
				f.observed.Store(true)
				return f.exit
			case <-fc.ctx.Done():
				f.completed.cancel(waiter)
				return Interrupt[A](fc.ctx.Err())
			}
		}
		resume := fc.suspendScheduler()
		select {
		case <-waiter.done:
			resume()
			f.completed.consume(waiter)
			f.observed.Store(true)
			return f.exit
		case <-fc.ctx.Done():
			resume()
			f.completed.cancel(waiter)
			return Interrupt[A](fc.ctx.Err())
		}
	}
}

// Interrupt requests cancellation and waits for completed cleanup. Normal interruption is acknowledged.
func (f *Fiber[A]) Interrupt() Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		if !f.accessible(fc) {
			return Die[Unit](fmt.Errorf("fiber owner is closed or not an ancestor"))
		}
		f.Cancel()
		scheduler := fc.turnScheduler()
		waiter, complete := f.completed.register(scheduler)
		if !complete {
			if scheduler == nil {
				<-waiter.done
			} else {
				resume := fc.suspendScheduler()
				<-waiter.done
				resume()
			}
			f.completed.consume(waiter)
		} else {
			f.completed.consume(waiter)
		}
		f.observed.Store(true)
		cause := f.exit.Cause()
		if cause.OnlyInterrupts() {
			return Succeed(Unit{})
		}
		return FromCause[Unit](cause)
	}
}
