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
