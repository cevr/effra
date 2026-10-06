package effra

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitSchedulerSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("managed operation did not complete")
	}
}

func TestTestSchedulerControlsSleepAndRegistration(t *testing.T) {
	scheduler := NewTestScheduler()
	started := make(chan struct{})
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			close(started)
			return Invoke(fc, Sleep(100))
		})
	}()
	waitSchedulerSignal(t, started)
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("sleep registration: %v", err)
	}
	if err := scheduler.Advance(99); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("sleep completed before its deadline")
	default:
	}
	if err := scheduler.Advance(1); err != nil {
		t.Fatal(err)
	}
	if out := <-done; out.IsFailure() {
		t.Fatalf("sleep failed at deadline: %+v", out)
	}
}

func TestTestSchedulerRejectsInvalidAdjustment(t *testing.T) {
	scheduler := NewTestScheduler()
	for _, duration := range []int64{-1, maxMilliseconds + 1} {
		if err := scheduler.Adjust(duration); err == nil {
			t.Fatalf("adjust accepted invalid duration %d", duration)
		}
	}
}

func TestTestSchedulerAdvancesSequentialSleepAtIntermediateDeadlines(t *testing.T) {
	scheduler := NewTestScheduler()
	points := make(chan int64, 2)
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			if out := Invoke(fc, Sleep(20)); out.IsFailure() {
				return out
			}
			points <- scheduler.Now()
			if out := Invoke(fc, Sleep(30)); out.IsFailure() {
				return out
			}
			points <- scheduler.Now()
			return Succeed(Unit{})
		})
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := scheduler.Adjust(50); err != nil {
		t.Fatal(err)
	}
	for expected, want := range []int64{20, 50} {
		select {
		case point := <-points:
			if point != want {
				t.Fatalf("sleep %d resumed at %d", expected+1, point)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("sleep %d did not resume", expected+1)
		}
	}
	if out := <-done; out.IsFailure() {
		t.Fatalf("sequential sleeps failed: %+v", out)
	}
}

func TestTestSchedulerWaitsForLatchContinuationBeforeAdvancing(t *testing.T) {
	scheduler := NewTestScheduler()
	latch := NewLatch()
	points := make(chan int64, 2)
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			first := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
				if out := Invoke(child, Sleep(20)); out.IsFailure() {
					return out
				}
				return Invoke(child, SignalLatch(latch))
			}))
			if first.IsFailure() {
				return Propagate[Unit](first)
			}
			second := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
				if out := Invoke(child, AwaitLatch(latch)); out.IsFailure() {
					return out
				}
				points <- scheduler.Now()
				if out := Invoke(child, Sleep(30)); out.IsFailure() {
					return out
				}
				points <- scheduler.Now()
				return Succeed(Unit{})
			}))
			if second.IsFailure() {
				return Propagate[Unit](second)
			}
			if out := Invoke(fc, first.Value.Join()); out.IsFailure() {
				return Propagate[Unit](out)
			}
			return Invoke(fc, second.Value.Join())
		})
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := scheduler.Adjust(50); err != nil {
		t.Fatal(err)
	}
	for expected, want := range []int64{20, 50} {
		select {
		case point := <-points:
			if point != want {
				t.Fatalf("latch continuation %d resumed at %d, want %d", expected+1, point, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("latch continuation %d did not resume", expected+1)
		}
	}
	select {
	case out := <-done:
		if out.IsFailure() {
			t.Fatalf("latch sequence failed: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("latch sequence did not finish")
	}
}

func TestTestSchedulerWaitsForJoinContinuationBeforeAdvancing(t *testing.T) {
	scheduler := NewTestScheduler()
	resumed := make(chan int64, 1)
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			first := Invoke(fc, Fork(Sleep(20)))
			if first.IsFailure() {
				return Propagate[Unit](first)
			}
			second := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
				if out := Invoke(child, first.Value.Join()); out.IsFailure() {
					return out
				}
				resumed <- scheduler.Now()
				return Invoke(child, Sleep(30))
			}))
			if second.IsFailure() {
				return Propagate[Unit](second)
			}
			return Invoke(fc, second.Value.Join())
		})
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := scheduler.Adjust(50); err != nil {
		t.Fatal(err)
	}
	select {
	case point := <-resumed:
		if point != 20 {
			t.Fatalf("join continuation resumed at %d, want 20", point)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("join continuation did not resume")
	}
	select {
	case out := <-done:
		if out.IsFailure() {
			t.Fatalf("join sequence failed: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("join sequence did not finish")
	}
}

func TestTestSchedulerWaitsForScopeCleanupContinuationBeforeAdvancing(t *testing.T) {
	scheduler := NewTestScheduler()
	resumed := make(chan int64, 1)
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			out := Invoke(fc, Scoped(func(child *FiberContext) Exit[Unit] {
				return Invoke(child, AcquireRelease("cleanup", func(context.Context) (Unit, error) {
					return Unit{}, nil
				}, func(_ Unit, cleanup context.Context) error {
					result := RunContextWithScheduler(cleanup, scheduler, Sleep(20))
					if result.IsFailure() {
						return result.Cause()
					}
					return nil
				}))
			}))
			if out.IsFailure() {
				return out
			}
			resumed <- scheduler.Now()
			return Invoke(fc, Sleep(30))
		})
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("cleanup registration: %v", err)
	}
	if err := scheduler.Adjust(50); err != nil {
		t.Fatal(err)
	}
	select {
	case point := <-resumed:
		if point != 20 {
			t.Fatalf("scope continuation resumed at %d, want 20", point)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scope continuation did not resume")
	}
	select {
	case out := <-done:
		if out.IsFailure() {
			t.Fatalf("scope sequence failed: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scope sequence did not finish")
	}
}

func TestTestSchedulerWaitsForTimeoutContinuationBeforeAdvancing(t *testing.T) {
	scheduler := NewTestScheduler()
	resumed := make(chan int64, 1)
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			out := Invoke(fc, Timeout(Sleep(20), 100))
			if out.IsFailure() && (out.Failure == nil || out.Failure.Tag != "Timeout") {
				return out
			}
			resumed <- scheduler.Now()
			return Invoke(fc, Sleep(30))
		})
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("timeout registration: %v", err)
	}
	if err := scheduler.Adjust(50); err != nil {
		t.Fatal(err)
	}
	select {
	case point := <-resumed:
		if point != 20 {
			t.Fatalf("timeout continuation resumed at %d, want 20", point)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout continuation did not resume")
	}
	select {
	case out := <-done:
		if out.IsFailure() {
			t.Fatalf("timeout sequence failed: %+v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout sequence did not finish")
	}
}

func TestTimeoutUsesSchedulerAndWaitsForCancelledCleanup(t *testing.T) {
	scheduler := NewTestScheduler()
	cleanup := make(chan struct{})
	started := make(chan struct{})
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, func(fc *FiberContext) Exit[Unit] {
			return Invoke(fc, Timeout(func(child *FiberContext) Exit[Unit] {
				close(started)
				out := Invoke(child, AcquireRelease("cleanup", func(context.Context) (Unit, error) {
					return Unit{}, nil
				}, func(Unit, context.Context) error {
					close(cleanup)
					return nil
				}))
				if out.IsFailure() {
					return out
				}
				return Invoke(child, Sleep(100))
			}, 50))
		})
	}()
	waitSchedulerSignal(t, started)
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("timeout registration: %v", err)
	}
	if err := scheduler.Advance(49); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("timeout completed before deadline")
	default:
	}
	if err := scheduler.Advance(1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleanup:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout did not release child resource")
	}
	out := <-done
	if out.Failure == nil || out.Failure.Tag != "Timeout" || len(out.Cause()) != 1 {
		t.Fatalf("wrong timeout result: %+v", out)
	}
}

func TestTimeoutWithExplicitDeadlinePreservesChildValue(t *testing.T) {
	scheduler := NewTestScheduler()
	out := RunContextWithScheduler(context.Background(), scheduler, TimeoutWithEffect(
		func(*FiberContext) Exit[string] { return Succeed("completed") },
		Sleep(100),
	))
	if out.IsFailure() || out.Value != "completed" {
		t.Fatalf("child result was lost: %+v", out)
	}
}

func TestTimeoutWithWorkWinnerRetainsTimerCleanupDefect(t *testing.T) {
	timerStarted := make(chan struct{})
	timerDefect := errors.New("timer cleanup defect")
	out := Run(TimeoutWithEffect(
		func(*FiberContext) Exit[string] {
			<-timerStarted
			return Succeed("completed")
		},
		func(fc *FiberContext) Exit[Unit] {
			close(timerStarted)
			<-fc.Context().Done()
			return withCleanup(Interrupt[Unit](fc.Context().Err()), Cause{{Kind: "defect", Err: timerDefect}})
		},
	))
	if !out.IsFailure() || out.Defect == nil || !errors.Is(out.Defect, timerDefect) {
		t.Fatalf("timer cleanup defect was discarded after work won: %+v", out)
	}
	if out.Failure != nil || out.Interrupted {
		t.Fatalf("work winner was rewritten as timeout or interruption: %+v", out)
	}
}

func TestCancelledSleepUnregistersItsTimer(t *testing.T) {
	scheduler := NewTestScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(ctx, scheduler, func(fc *FiberContext) Exit[Unit] {
			close(started)
			return Invoke(fc, Sleep(100))
		})
	}()
	waitSchedulerSignal(t, started)
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatalf("sleep registration: %v", err)
	}
	cancel()
	if out := <-done; !out.Interrupted {
		t.Fatalf("cancelled sleep was not interrupted: %+v", out)
	}
	if err := scheduler.Advance(100); err != nil {
		t.Fatal(err)
	}
	barrierCtx, barrierCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer barrierCancel()
	if err := scheduler.AwaitRegistration(barrierCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale timer remained registered: %v", err)
	}
}
