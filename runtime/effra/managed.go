package effra

import (
	"context"
	"sync"
)

type managedResult[A any] struct {
	signal *managedSignal
	mu     sync.Mutex
	value  A
}

func newManagedResult[A any]() *managedResult[A] {
	return &managedResult[A]{signal: newManagedSignal()}
}

func (r *managedResult[A]) publish(value A) {
	r.mu.Lock()
	r.value = value
	r.mu.Unlock()
	r.signal.signal()
}

func (r *managedResult[A]) get() A {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value
}

// managedSignal is a one-shot completion handoff for language-owned work.
// Unlike a bare channel, it reserves scheduler wake capacity before publishing
// completion to a managed waiter. That reservation keeps a strong virtual-time
// adjustment from advancing while the waiter is between wakeup and its next
// managed operation.
type managedSignal struct {
	mu       sync.Mutex
	done     chan struct{}
	complete bool
	waiters  map[*managedWaiter]struct{}
}

// managedWaiter is registered by one managed execution turn. A signal owns a
// waiter once it removes it from the pending set; cancellation then releases
// the reservation when no continuation will consume it.
type managedWaiter struct {
	done      chan struct{}
	scheduler *TestScheduler
	reserved  bool
}

func newManagedSignal() *managedSignal {
	return &managedSignal{done: make(chan struct{}), waiters: map[*managedWaiter]struct{}{}}
}

func (s *managedSignal) register(scheduler *TestScheduler) (*managedWaiter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.complete {
		waiter := &managedWaiter{done: make(chan struct{}), scheduler: scheduler, reserved: scheduler != nil}
		if scheduler != nil {
			scheduler.reserveWake()
		}
		close(waiter.done)
		return waiter, true
	}
	waiter := &managedWaiter{done: make(chan struct{}), scheduler: scheduler}
	s.waiters[waiter] = struct{}{}
	return waiter, false
}

func (s *managedSignal) signal() bool {
	s.mu.Lock()
	if s.complete {
		s.mu.Unlock()
		return false
	}
	s.complete = true
	waiters := make([]*managedWaiter, 0, len(s.waiters))
	for waiter := range s.waiters {
		delete(s.waiters, waiter)
		waiter.reserved = true
		waiters = append(waiters, waiter)
		if waiter.scheduler != nil {
			waiter.scheduler.reserveWake()
		}
	}
	s.mu.Unlock()
	close(s.done)
	for _, waiter := range waiters {
		close(waiter.done)
	}
	return true
}

func (s *managedSignal) consume(waiter *managedWaiter) {
	if waiter == nil {
		return
	}
	s.mu.Lock()
	if !waiter.reserved {
		s.mu.Unlock()
		return
	}
	waiter.reserved = false
	scheduler := waiter.scheduler
	s.mu.Unlock()
	if scheduler != nil {
		scheduler.completeWake()
	}
}

func (s *managedSignal) cancel(waiter *managedWaiter) {
	if waiter == nil {
		return
	}
	s.mu.Lock()
	if _, waiting := s.waiters[waiter]; waiting {
		delete(s.waiters, waiter)
		s.mu.Unlock()
		return
	}
	if !waiter.reserved {
		s.mu.Unlock()
		return
	}
	waiter.reserved = false
	scheduler := waiter.scheduler
	s.mu.Unlock()
	if scheduler != nil {
		scheduler.completeWake()
	}
}

func (s *managedSignal) await(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *managedSignal) isComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.complete
}
