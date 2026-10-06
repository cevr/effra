package effra

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

const maxMilliseconds int64 = 2147483647

var errSchedulerClosed = errors.New("test scheduler is closed")

// timerHandle is the small timer surface consumed by Sleep and Timeout. A
// handle is cancellable so interrupted work cannot leave a stale wakeup in a
// virtual scheduler.
type timerHandle interface {
	C() <-chan time.Time
	Stop() bool
	acknowledge()
}

type timerDriver interface {
	newTimer(time.Duration) timerHandle
}

type liveTimerDriver struct{}

type liveTimer struct{ timer *time.Timer }

func (liveTimerDriver) newTimer(duration time.Duration) timerHandle {
	return &liveTimer{timer: time.NewTimer(duration)}
}
func (t *liveTimer) C() <-chan time.Time { return t.timer.C }
func (t *liveTimer) Stop() bool          { return t.timer.Stop() }
func (t *liveTimer) acknowledge()        {}

// TestScheduler is a serialized virtual timer driver. Timers are ordered by
// deadline and registration sequence. Advance only delivers timers already
// registered at the start of that operation; continuations register their
// next timer at the current logical time and are observed by the next
// registration barrier.
type TestScheduler struct {
	mu           sync.Mutex
	now          int64
	nextSequence uint64
	closed       bool
	timers       map[*virtualTimer]struct{}
	registered   uint64
	observed     uint64
	registration chan struct{}
}

type virtualTimer struct {
	scheduler *TestScheduler
	deadline  int64
	sequence  uint64
	done      chan time.Time
	ackDone   chan struct{}
	ackOnce   sync.Once
	fired     bool
}

// NewTestScheduler creates a fresh scheduler at logical time zero.
func NewTestScheduler() *TestScheduler {
	return &TestScheduler{timers: map[*virtualTimer]struct{}{}, registration: make(chan struct{})}
}

// NewTestClock is an alias for callers that describe the virtual driver as a
// clock. It is intentionally the same scheduler used by timeout and sleep.
func NewTestClock() *TestScheduler { return NewTestScheduler() }

func (s *TestScheduler) newTimer(duration time.Duration) timerHandle {
	milliseconds := duration.Milliseconds()
	if milliseconds < 0 || milliseconds > maxMilliseconds {
		return &virtualTimer{done: make(chan time.Time)}
	}
	s.mu.Lock()
	if s.closed || s.now > math.MaxInt64-milliseconds {
		s.mu.Unlock()
		return &virtualTimer{done: make(chan time.Time)}
	}
	s.nextSequence++
	timer := &virtualTimer{
		scheduler: s,
		deadline:  s.now + milliseconds,
		sequence:  s.nextSequence,
		done:      make(chan time.Time),
		ackDone:   make(chan struct{}),
	}
	s.timers[timer] = struct{}{}
	s.registered++
	signal := s.registration
	s.registration = make(chan struct{})
	immediate := timer.deadline <= s.now
	if immediate {
		delete(s.timers, timer)
		timer.fired = true
	}
	s.mu.Unlock()
	close(signal)
	if immediate {
		close(timer.done)
	}
	return timer
}

func (t *virtualTimer) C() <-chan time.Time { return t.done }
func (t *virtualTimer) acknowledge()        { t.ackOnce.Do(func() { close(t.ackDone) }) }
func (t *virtualTimer) Stop() bool {
	if t.scheduler == nil {
		return false
	}
	s := t.scheduler
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, pending := s.timers[t]; !pending {
		t.acknowledge()
		return false
	}
	delete(s.timers, t)
	t.acknowledge()
	return true
}

// Now returns the current logical time in milliseconds.
func (s *TestScheduler) Now() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// AwaitRegistration waits until a timer has been registered since the last
// observed scheduler advance/barrier. It is the explicit handshake needed to
// distinguish readiness of a worker from registration of its subsequent
// timer.
func (s *TestScheduler) AwaitRegistration(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return errSchedulerClosed
		}
		if s.registered > s.observed {
			s.observed = s.registered
			s.mu.Unlock()
			return nil
		}
		signal := s.registration
		s.mu.Unlock()
		select {
		case <-signal:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Advance moves logical time forward and delivers timers registered before
// the operation. It never waits for awakened fibers, so a cleanup fiber can
// make progress and register or cancel timers without a scheduler lock being
// held by the controller.
func (s *TestScheduler) Advance(milliseconds int64) error {
	if milliseconds < 0 {
		return errors.New("test scheduler duration must be non-negative")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errSchedulerClosed
	}
	if s.now > math.MaxInt64-milliseconds {
		s.mu.Unlock()
		return errors.New("test scheduler time overflow")
	}
	target := s.now + milliseconds
	s.now = target
	s.observed = s.registered
	s.mu.Unlock()
	for {
		s.mu.Lock()
		nextDeadline := target
		found := false
		for timer := range s.timers {
			if timer.deadline <= target && (!found || timer.deadline < nextDeadline) {
				nextDeadline = timer.deadline
				found = true
			}
		}
		if !found {
			s.now = target
			s.mu.Unlock()
			return nil
		}
		s.now = nextDeadline
		due := make([]*virtualTimer, 0)
		for timer := range s.timers {
			if timer.deadline <= s.now {
				delete(s.timers, timer)
				timer.fired = true
				due = append(due, timer)
			}
		}
		s.mu.Unlock()
		sort.Slice(due, func(i, j int) bool {
			if due[i].deadline != due[j].deadline {
				return due[i].deadline < due[j].deadline
			}
			return due[i].sequence < due[j].sequence
		})
		for _, timer := range due {
			close(timer.done)
			// The acknowledgment is emitted by Sleep/Timeout immediately after
			// selecting this timer, before any cleanup they may await. We wait
			// without holding the scheduler lock so continuations can register
			// their next timer and cleanup can cancel stale ones.
			<-timer.ackDone
		}
	}
}

// Close prevents new timers and future advancement. Existing timer channels
// remain pending so closing a scheduler cannot turn a cancelled or abandoned
// wait into a successful result.
func (s *TestScheduler) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.timers = map[*virtualTimer]struct{}{}
	signal := s.registration
	s.registration = make(chan struct{})
	s.mu.Unlock()
	close(signal)
	return nil
}

func (s *TestScheduler) newScopeDriver() timerDriver { return s }
