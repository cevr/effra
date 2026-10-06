package effra

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type scopeState string

const (
	Open    scopeState = "Open"
	Closing scopeState = "Closing"
	Closed  scopeState = "Closed"
)

var scopeIDs atomic.Uint64

// Scope serializes admission with shutdown. Acquisitions remain tracked through release.
type Scope struct {
	mu           sync.Mutex
	id           uint64
	state        scopeState
	ctx          context.Context
	cancel       context.CancelFunc
	parent       *Scope
	children     []ownedFiber
	resources    []resource
	hooks        []func() error
	acquiring    sync.WaitGroup
	acquisitions int
	done         chan struct{}
	hooksDone    chan struct{}
	outcome      Cause
	driver       timerDriver
}
type resource struct {
	name    string
	release func(context.Context) error
}

func newScope(ctx context.Context, parent *Scope) *Scope {
	driver := timerDriver(liveTimerDriver{})
	if parent != nil && parent.driver != nil {
		driver = parent.driver
	}
	return newScopeWithDriver(ctx, parent, driver)
}
func newScopeWithDriver(ctx context.Context, parent *Scope, driver timerDriver) *Scope {
	if driver == nil {
		driver = liveTimerDriver{}
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Scope{id: scopeIDs.Add(1), state: Open, ctx: ctx, cancel: cancel, parent: parent, driver: driver, done: make(chan struct{}), hooksDone: make(chan struct{})}
	context.AfterFunc(ctx, func() {
		s.mu.Lock()
		hooks := append([]func() error{}, s.hooks...)
		s.mu.Unlock()
		outcome := Cause{}
		for _, hook := range hooks {
			outcome = append(outcome, defectReason(hook())...)
		}
		s.mu.Lock()
		s.outcome = append(s.outcome, outcome...)
		s.mu.Unlock()
		close(s.hooksDone)
	})
	return s
}
func (s *Scope) IsOpen() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state == Open }
func protected(action func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("cleanup panic: %v", value)
		}
	}()
	return action()
}
func defectReason(err error) Cause {
	if err == nil {
		return nil
	}
	return Cause{{Kind: "defect", Err: err}}
}

// OnCancel registers an idempotent adapter hook before admitting blocking work.
func (s *Scope) OnCancel(hook func() error) error {
	var once sync.Once
	var outcome error
	run := func() error { once.Do(func() { outcome = protected(hook) }); return outcome }
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != Open || s.ctx.Err() != nil {
		return fmt.Errorf("scope is closing")
	}
	s.hooks = append(s.hooks, run)
	return nil
}
func (s *Scope) Close() Cause {
	s.mu.Lock()
	if s.state != Open {
		done := s.done
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		out := append(Cause{}, s.outcome...)
		s.mu.Unlock()
		return out
	}
	s.state = Closing
	children := append([]ownedFiber{}, s.children...)
	s.mu.Unlock()
	s.cancel()
	for _, child := range children {
		child.requestCancel()
	}
	outcome := Cause{}
	<-s.hooksDone
	// No new Add is possible after Closing. Successful late acquisitions finish release before Done.
	s.acquiring.Wait()
	for _, child := range children {
		cause := child.closeResult()
		if !cause.OnlyInterrupts() {
			outcome = append(outcome, cause...)
		}
	}
	s.mu.Lock()
	resources := append([]resource{}, s.resources...)
	outcome = append(outcome, s.outcome...)
	s.mu.Unlock()
	cleanupContext := context.WithoutCancel(s.ctx)
	for i := len(resources) - 1; i >= 0; i-- {
		r := resources[i]
		outcome = append(outcome, defectReason(protected(func() error { return r.release(cleanupContext) }))...)
	}
	s.mu.Lock()
	s.outcome = append(Cause{}, outcome...)
	s.resources = nil
	s.hooks = nil
	s.children = nil
	s.state = Closed
	close(s.done)
	s.mu.Unlock()
	return append(Cause{}, outcome...)
}
func AcquireRelease[A any](name string, acquire func(context.Context) (A, error), release func(A, context.Context) error) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		s := fc.scope
		s.mu.Lock()
		if s.state != Open {
			s.mu.Unlock()
			return Die[A](fmt.Errorf("scope is closing"))
		}
		s.acquiring.Add(1)
		s.acquisitions++
		s.mu.Unlock()
		defer func() { s.mu.Lock(); s.acquisitions--; s.mu.Unlock(); s.acquiring.Done() }()
		// Logical interruption is masked across acquire/register. Foreign acquisition must eventually return.
		value, err := acquire(context.WithoutCancel(fc.ctx))
		if err != nil {
			return Fail[A]("IoError", err)
		}
		var once sync.Once
		var releaseErr error
		finalize := func(ctx context.Context) error {
			once.Do(func() { releaseErr = protected(func() error { return release(value, ctx) }) })
			return releaseErr
		}
		s.mu.Lock()
		if s.state == Open {
			s.resources = append(s.resources, resource{name, finalize})
			s.mu.Unlock()
			return Succeed(value)
		}
		s.mu.Unlock()
		cleanup := defectReason(finalize(context.WithoutCancel(fc.ctx)))
		s.mu.Lock()
		s.outcome = append(s.outcome, cleanup...)
		s.mu.Unlock()
		return Interrupt[A](context.Canceled)
	}
}

// Snapshot describes runtime-owned state, not arbitrary Go goroutines or payloads.
type Snapshot struct {
	ID                    uint64          `json:"id"`
	State                 string          `json:"state"`
	Children              int             `json:"children"`
	Resources             []string        `json:"resources"`
	Acquisitions          int             `json:"acquisitions"`
	CancellationRequested bool            `json:"cancellationRequested"`
	ResourceCount         int             `json:"resourceCount"`
	ChildStates           []FiberSnapshot `json:"childStates"`
	Truncated             bool            `json:"truncated"`
}

func (s *Scope) Snapshot() Snapshot {
	s.mu.Lock()
	names := []string{}
	labelsTruncated := false
	for _, r := range s.resources {
		if len(names) == 100 {
			break
		}
		name := r.name
		if len(name) > 256 {
			name = name[:256]
			labelsTruncated = true
		}
		names = append(names, name)
	}
	count := len(s.children)
	limit := min(count, 100)
	children := append([]ownedFiber{}, s.children[:limit]...)
	out := Snapshot{ID: s.id, State: string(s.state), Children: count, Resources: names,
		Acquisitions: s.acquisitions, CancellationRequested: s.ctx.Err() != nil,
		ResourceCount: len(s.resources), Truncated: labelsTruncated || count > 100 || len(s.resources) > 100,
		ChildStates: []FiberSnapshot{}}
	s.mu.Unlock()
	for _, child := range children {
		out.ChildStates = append(out.ChildStates, child.snapshot())
	}
	return out
}
