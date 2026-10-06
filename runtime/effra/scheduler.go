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
// deadline and registration sequence. Adjust drains every managed timer at or
// before its target, including timers registered by continuations awakened at
// intermediate deadlines. Managed fibers yield the scheduler turn while they
// wait, so adjustment does not depend on runtime.Gosched or wall-clock sleeps.
type TestScheduler struct {
	mu           sync.Mutex
	adjustMu     sync.Mutex
	now          int64
	nextSequence uint64
	closed       bool
	timers       map[*virtualTimer]struct{}
	registered   uint64
	observed     uint64
	registration chan struct{}
	turn         chan struct{}
	stateChanged chan struct{}
	active       int
	pendingWakes int
	// continuations counts only runnable cleanup/publication boundaries. A
	// boundary parked on a registered managed wait is represented by the same
	// token but is omitted from this count, so partial adjustment can reach the
	// wait's deadline.
	continuations int
}

type continuationState uint8

const (
	continuationRunnable continuationState = iota
	continuationParked
	continuationComplete
)

// schedulerContinuation identifies a managed cleanup/publication boundary
// across a nested RunContextWithScheduler call. Its state is read and changed
// under the owning TestScheduler's mutex.
type schedulerContinuation struct {
	scheduler *TestScheduler
	state     continuationState
}

type schedulerContinuationContextKey struct{}

func withSchedulerContinuation(ctx context.Context, continuation *schedulerContinuation) context.Context {
	if continuation == nil {
		return ctx
	}
	return context.WithValue(ctx, schedulerContinuationContextKey{}, continuation)
}

func schedulerContinuationFromContext(ctx context.Context) *schedulerContinuation {
	if ctx == nil {
		return nil
	}
	continuation, _ := ctx.Value(schedulerContinuationContextKey{}).(*schedulerContinuation)
	return continuation
}

type virtualTimer struct {
	scheduler   *TestScheduler
	deadline    int64
	sequence    uint64
	done        chan time.Time
	fired       bool
	wakePending bool
}

// NewTestScheduler creates a fresh scheduler at logical time zero.
func NewTestScheduler() *TestScheduler {
	turn := make(chan struct{}, 1)
	turn <- struct{}{}
	return &TestScheduler{
		timers:       map[*virtualTimer]struct{}{},
		registration: make(chan struct{}),
		turn:         turn,
		stateChanged: make(chan struct{}),
	}
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
func (t *virtualTimer) acknowledge()        {}
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

// reserve records a fiber admitted by Fork/Timeout before its goroutine starts.
// Keeping that admission visible prevents an external adjustment from
// observing a false idle state between admission and execution.
func (s *TestScheduler) reserve() {
	s.mu.Lock()
	s.active++
	s.signalStateLocked()
	s.mu.Unlock()
}

// enter gives a managed fiber the scheduler turn. The turn serializes
// virtual-time fibers without serializing the external test controller.
func (s *TestScheduler) enter(admitted bool) func() {
	if !admitted {
		// Admission must be visible before waiting for the serialized turn. This
		// closes the gap where an external adjustment could commit while a new
		// managed root was waiting to start and had not registered its timer.
		s.reserve()
	}
	<-s.turn
	s.mu.Lock()
	s.signalStateLocked()
	s.mu.Unlock()
	return func() { s.finish() }
}

func (s *TestScheduler) finish() {
	s.mu.Lock()
	if s.active > 0 {
		s.active--
	}
	s.signalStateLocked()
	s.mu.Unlock()
	s.turn <- struct{}{}
}

// suspend releases the scheduler turn while the current managed fiber waits
// on an external event. The returned function reacquires the turn before the
// caller continues, keeping adjustment quiescence observable.
func (s *TestScheduler) suspend() func() {
	s.mu.Lock()
	if s.active > 0 {
		s.active--
	}
	s.signalStateLocked()
	s.mu.Unlock()
	s.turn <- struct{}{}
	return func() {
		<-s.turn
		s.mu.Lock()
		s.active++
		s.signalStateLocked()
		s.mu.Unlock()
	}
}

func (s *TestScheduler) resumeWait(timer *virtualTimer) {
	s.mu.Lock()
	if timer != nil && timer.wakePending {
		timer.wakePending = false
		s.completeWakeLocked()
	}
	s.mu.Unlock()
}

// reserveWake records a managed continuation before its producer releases a
// wake channel. This is shared by timer and causal synchronization handoffs.
func (s *TestScheduler) reserveWake() {
	s.mu.Lock()
	s.pendingWakes++
	s.signalStateLocked()
	s.mu.Unlock()
}

// completeWake consumes one managed continuation reservation. Callers invoke
// it only after the continuation has either reacquired its turn or been
// canceled and removed, so adjustment cannot advance past an unregistered
// next wait.
func (s *TestScheduler) completeWake() {
	s.mu.Lock()
	s.completeWakeLocked()
	s.mu.Unlock()
}

func (s *TestScheduler) completeWakeLocked() {
	if s.pendingWakes > 0 {
		s.pendingWakes--
	}
	s.signalStateLocked()
}

// reserveContinuation keeps a managed synchronous boundary visible while it
// performs owned cleanup. The boundary starts runnable. If its cleanup parks
// on a registered managed wait, parkContinuation removes it from the runnable
// count until that wait wakes.
func (s *TestScheduler) reserveContinuation() *schedulerContinuation {
	continuation := &schedulerContinuation{scheduler: s, state: continuationRunnable}
	s.mu.Lock()
	s.continuations++
	s.signalStateLocked()
	s.mu.Unlock()
	return continuation
}

func (s *TestScheduler) parkContinuation(continuation *schedulerContinuation) {
	if continuation == nil || continuation.scheduler != s {
		return
	}
	s.mu.Lock()
	if continuation.state == continuationRunnable {
		continuation.state = continuationParked
		if s.continuations > 0 {
			s.continuations--
		}
		s.signalStateLocked()
	}
	s.mu.Unlock()
}

func (s *TestScheduler) unparkContinuation(continuation *schedulerContinuation) {
	if continuation == nil || continuation.scheduler != s {
		return
	}
	s.mu.Lock()
	if continuation.state == continuationParked {
		continuation.state = continuationRunnable
		s.continuations++
		s.signalStateLocked()
	}
	s.mu.Unlock()
}

func (s *TestScheduler) completeContinuation(continuation *schedulerContinuation) {
	if continuation == nil || continuation.scheduler != s {
		return
	}
	s.mu.Lock()
	if continuation.state == continuationRunnable && s.continuations > 0 {
		s.continuations--
	}
	continuation.state = continuationComplete
	s.signalStateLocked()
	s.mu.Unlock()
}

func (s *TestScheduler) signalStateLocked() {
	previous := s.stateChanged
	s.stateChanged = make(chan struct{})
	close(previous)
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

// Adjust moves logical time forward and drains every timer whose deadline is
// at or before the target. A timer continuation gets the scheduler turn
// before the next deadline is selected, so sequential sleeps observe their
// intermediate logical times during one adjustment.
func (s *TestScheduler) Adjust(milliseconds int64) error {
	s.adjustMu.Lock()
	defer s.adjustMu.Unlock()
	if milliseconds < 0 || milliseconds > maxMilliseconds {
		return errors.New("invalid millisecond duration")
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
	s.observed = s.registered
	s.mu.Unlock()
	for {
		s.mu.Lock()
		// Quiescence, deadline selection, and target publication are one
		// synchronized decision. A continuation may be absent from this count
		// only while parked on a registered managed wait.
		if s.active != 0 || s.pendingWakes != 0 || s.continuations != 0 {
			signal := s.stateChanged
			s.mu.Unlock()
			<-signal
			continue
		}
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
				timer.wakePending = true
				due = append(due, timer)
			}
		}
		s.pendingWakes += len(due)
		s.signalStateLocked()
		s.mu.Unlock()
		sort.Slice(due, func(i, j int) bool {
			if due[i].deadline != due[j].deadline {
				return due[i].deadline < due[j].deadline
			}
			return due[i].sequence < due[j].sequence
		})
		for _, timer := range due {
			close(timer.done)
		}
	}
}

// Advance is retained as a compatibility alias for Adjust. Both operations
// have the strong drain semantics; callers should prefer Adjust when the
// operation's deadline-draining behavior matters.
func (s *TestScheduler) Advance(milliseconds int64) error {
	return s.Adjust(milliseconds)
}

// adjustInFiber suspends the calling managed fiber while the external
// controller drains virtual time. It is the boundary used by generated
// Scheduler providers; calling Adjust directly from a test goroutine does not
// need this wrapper.
func (s *TestScheduler) adjustInFiber(fc *FiberContext, milliseconds int64) error {
	if fc == nil || fc.turnScheduler() != s {
		return errors.New("test scheduler is not active for this fiber")
	}
	resume := s.suspend()
	defer resume()
	return s.Adjust(milliseconds)
}

func (s *TestScheduler) awaitRegistrationInFiber(fc *FiberContext) error {
	if fc == nil || fc.turnScheduler() != s {
		return errors.New("test scheduler is not active for this fiber")
	}
	resume := s.suspend()
	defer resume()
	return s.AwaitRegistration(fc.Context())
}

// AdjustTestScheduler is the managed runtime boundary for the built-in
// Scheduler provider. It suspends the caller's turn while the controller
// drains virtual time, then resumes the caller before returning.
func AdjustTestScheduler(fc *FiberContext, scheduler *TestScheduler, milliseconds int64) Exit[Unit] {
	if scheduler == nil {
		return Die[Unit](errors.New("nil test scheduler"))
	}
	if err := scheduler.adjustInFiber(fc, milliseconds); err != nil {
		return Die[Unit](err)
	}
	return Succeed(Unit{})
}

// AwaitTestSchedulerRegistration is the managed registration barrier for the
// built-in Scheduler provider.
func AwaitTestSchedulerRegistration(fc *FiberContext, scheduler *TestScheduler) Exit[Unit] {
	if scheduler == nil {
		return Die[Unit](errors.New("nil test scheduler"))
	}
	if err := scheduler.awaitRegistrationInFiber(fc); err != nil {
		return Die[Unit](err)
	}
	return Succeed(Unit{})
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
	s.signalStateLocked()
	s.mu.Unlock()
	close(signal)
	return nil
}

func (s *TestScheduler) newScopeDriver() timerDriver { return s }
