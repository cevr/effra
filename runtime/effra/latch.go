package effra

import (
	"context"
	"errors"
)

var errInvalidLatch = errors.New("invalid latch handle")

// Latch is a portable, one-shot synchronization handle. Completion is
// idempotent and is shared by every waiter; waiting never consumes it.
// Construct handles with NewLatch; the zero value is an invalid handle.
type Latch struct {
	signal *managedSignal
}

// NewLatch creates an incomplete one-shot latch.
func NewLatch() *Latch {
	return &Latch{signal: newManagedSignal()}
}

// Signal completes the latch. It returns true only for the first completion.
func (l *Latch) Signal() bool {
	if l == nil || l.signal == nil {
		return false
	}
	return l.signal.signal()
}

// IsSignaled reports whether the latch has completed.
func (l *Latch) IsSignaled() bool {
	return l != nil && l.signal != nil && l.signal.isComplete()
}

// Await waits for completion without changing the latch. The caller's
// context controls only this waiter; cancellation leaves other waiters and
// the latch itself untouched.
func (l *Latch) Await(ctx context.Context) error {
	if l == nil || l.signal == nil {
		return errInvalidLatch
	}
	return l.signal.await(ctx)
}

// registerManaged adds a waiter owned by a scheduler-managed fiber. The bool
// reports an already-signaled latch, in which case no blocking handoff is
// needed. AwaitLatch validates the handle before using these internal helpers.
func (l *Latch) registerManaged(scheduler *TestScheduler) (*managedWaiter, bool) {
	return l.signal.register(scheduler)
}

func (l *Latch) consumeManaged(waiter *managedWaiter) {
	l.signal.consume(waiter)
}

func (l *Latch) cancelManaged(waiter *managedWaiter) {
	l.signal.cancel(waiter)
}

// AwaitLatch adapts a latch wait to the managed Effect runtime.
func AwaitLatch(latch *Latch) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		if latch == nil || latch.signal == nil {
			return Die[Unit](errInvalidLatch)
		}
		if scheduler := fc.turnScheduler(); scheduler != nil {
			waiter, complete := latch.registerManaged(scheduler)
			if complete {
				latch.consumeManaged(waiter)
				return Succeed(Unit{})
			}
			resume := fc.suspendScheduler()
			select {
			case <-waiter.done:
				resume()
				latch.consumeManaged(waiter)
				return Succeed(Unit{})
			case <-fc.Context().Done():
				resume()
				latch.cancelManaged(waiter)
				return Interrupt[Unit](fc.Context().Err())
			}
		}
		resume := fc.suspendScheduler()
		err := latch.Await(fc.Context())
		resume()
		if err != nil {
			return Interrupt[Unit](err)
		}
		return Succeed(Unit{})
	}
}

// SignalLatch completes a latch from the managed Effect runtime.
func SignalLatch(latch *Latch) Effect[Unit] {
	return func(*FiberContext) Exit[Unit] {
		if latch == nil || latch.signal == nil {
			return Die[Unit](errInvalidLatch)
		}
		latch.Signal()
		return Succeed(Unit{})
	}
}
