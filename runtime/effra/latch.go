package effra

import (
	"context"
	"errors"
	"sync"
)

var errNilLatch = errors.New("invalid latch handle: nil")

// Latch is a portable, one-shot synchronization handle. Completion is
// idempotent and is shared by every waiter; waiting never consumes it.
type Latch struct {
	mu       sync.Mutex
	done     chan struct{}
	complete bool
}

// NewLatch creates an incomplete one-shot latch.
func NewLatch() *Latch { return &Latch{done: make(chan struct{})} }

// Signal completes the latch. It returns true only for the first completion.
func (l *Latch) Signal() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.complete {
		return false
	}
	l.complete = true
	close(l.done)
	return true
}

// IsSignaled reports whether the latch has completed.
func (l *Latch) IsSignaled() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.complete
}

// Await waits for completion without changing the latch. The caller's
// context controls only this waiter; cancellation leaves other waiters and
// the latch itself untouched.
func (l *Latch) Await(ctx context.Context) error {
	if l == nil {
		return errNilLatch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-l.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AwaitLatch adapts a latch wait to the managed Effect runtime.
func AwaitLatch(latch *Latch) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		if latch == nil {
			return Die[Unit](errNilLatch)
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
		if latch == nil {
			return Die[Unit](errNilLatch)
		}
		latch.Signal()
		return Succeed(Unit{})
	}
}
