package effra

import (
	"context"
	"errors"
	"testing"
)

// A cancellation-aborted owner discards the ordinary typed failures of the
// children it had not observed; defects stay. A close that is not aborted
// raises them (design R6 X1).

func failingChild(tag string) Effect[string] {
	return func(*FiberContext) Exit[string] { return Fail[string](tag, nil) }
}

func parkedChild() Effect[string] {
	return func(fc *FiberContext) Exit[string] {
		<-fc.ctx.Done()
		return Interrupt[string](fc.ctx.Err())
	}
}

func onlyReason(t *testing.T, exit Exit[string], kind string) {
	t.Helper()
	cause := exit.Cause()
	if len(cause) != 1 || cause[0].Kind != kind {
		t.Fatalf("want a solitary %s, got %v", kind, cause)
	}
}

func TestNormalCloseRaisesUnobservedChildFailure(t *testing.T) {
	out := Run(func(fc *FiberContext) Exit[string] {
		child := Invoke(fc, Fork(failingChild("Boom")))
		<-child.Value.done
		return Succeed("done")
	})
	if out.Failure == nil || out.Failure.Tag != "Boom" || len(out.Cause()) != 1 {
		t.Fatalf("a normal close must raise the child failure: %+v", out)
	}
}

func TestBodyInterruptionDiscardsUnobservedChildFailure(t *testing.T) {
	// The body ends by interruption although nothing cancelled the owner: it
	// joined a cancelled sibling (b1).
	out := Run(func(fc *FiberContext) Exit[string] {
		failed := Invoke(fc, Fork(failingChild("Boom")))
		<-failed.Value.done
		parked := Invoke(fc, Fork(parkedChild()))
		parked.Value.Cancel()
		return Invoke(fc, parked.Value.Join())
	})
	onlyReason(t, out, "interrupt")
}

func TestRequestedCancellationDiscardsUnobservedChildFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := RunContext(ctx, func(fc *FiberContext) Exit[string] {
		child := Invoke(fc, Fork(failingChild("Boom")))
		<-child.Value.done
		cancel()
		return Succeed("done") // the body does not notice; the request is on the owner
	})
	if out.Failure != nil || out.Defect != nil {
		t.Fatalf("the cancelled owner raised %v", out.Cause())
	}
}

func TestNestedOwnerAbandonsItsRetainedChild(t *testing.T) {
	// The outer scope closes normally and cancels the slow fiber, which is a
	// cancellation-aborted owner of the failing grandchild (w2).
	failed := make(chan struct{})
	out := Run(func(fc *FiberContext) Exit[string] {
		slow := Invoke(fc, Fork(func(inner *FiberContext) Exit[string] {
			grandchild := Invoke(inner, Fork(failingChild("Boom")))
			<-grandchild.Value.done
			close(failed)
			<-inner.ctx.Done()
			return Interrupt[string](inner.ctx.Err())
		}))
		if slow.IsFailure() {
			return Propagate[string](slow)
		}
		<-failed
		return Succeed("done")
	})
	if out.IsFailure() {
		t.Fatalf("the abandoned grandchild failure escaped: %v", out.Cause())
	}
}

func TestAbortedCloseKeepsChildDefects(t *testing.T) {
	boom := errors.New("child defect")
	out := Run(func(fc *FiberContext) Exit[string] {
		bad := Invoke(fc, Fork(func(*FiberContext) Exit[string] { return Die[string](boom) }))
		<-bad.Value.done
		parked := Invoke(fc, Fork(parkedChild()))
		parked.Value.Cancel()
		return Invoke(fc, parked.Value.Join())
	})
	found := false
	for _, r := range out.Cause() {
		if r.Kind == "failure" {
			t.Fatalf("typed failure survived: %v", out.Cause())
		}
		if r.Kind == "defect" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the defect was discarded: %v", out.Cause())
	}
}

func TestTimeoutWinnerAbandonsTheTimedWork(t *testing.T) {
	out := Run(Timeout(func(fc *FiberContext) Exit[string] {
		child := Invoke(fc, Fork(failingChild("Boom")))
		<-child.Value.done
		<-fc.ctx.Done()
		return Fail[string]("Boom", nil) // the work's own failure racing the deadline
	}, 20))
	if out.Failure == nil || out.Failure.Tag != "Timeout" || len(out.Cause()) != 1 {
		t.Fatalf("want Timeout alone, got %v", out.Cause())
	}
}

// The request snapshot (Scope.Close reads the owner's context before it asks
// the children to stop) is isolated from the body predicate: the body here ends
// by an ordinary typed failure, and Invoke does not turn that into an
// interruption.

func TestRequestedCancellationAbortsAFailingBodyOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := RunContext(ctx, func(fc *FiberContext) Exit[string] {
		child := Invoke(fc, Fork(failingChild("Boom")))
		<-child.Value.done
		cancel()
		return Fail[string]("Body", nil)
	})
	onlyReason(t, out, "failure")
	if out.Failure == nil || out.Failure.Tag != "Body" {
		t.Fatalf("want the body failure alone, got %v", out.Cause())
	}
}

func TestScopeCloseSnapshotsTheRequestBeforeCancellingChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scope := newScope(ctx, nil)
	fc := &FiberContext{ctx: ctx, admission: ctx, scope: scope}
	child := Invoke(fc, Fork(failingChild("Boom")))
	<-child.Value.done
	cancel()
	for _, reason := range scope.Close() {
		if reason.Kind == "failure" {
			t.Fatalf("a cancelled owner kept the unobserved child failure: %v", reason)
		}
	}
}

func TestAbortedCloseKeepsCleanupFailures(t *testing.T) {
	release := errors.New("release failed")
	out := Run(func(fc *FiberContext) Exit[string] {
		acquired := Invoke(fc, AcquireRelease("resource", func(context.Context) (Unit, error) { return Unit{}, nil },
			func(Unit, context.Context) error { return release }))
		if acquired.IsFailure() {
			return Propagate[string](acquired)
		}
		failed := Invoke(fc, Fork(failingChild("Boom")))
		<-failed.Value.done
		parked := Invoke(fc, Fork(parkedChild()))
		parked.Value.Cancel()
		return Invoke(fc, parked.Value.Join())
	})
	var cleanup bool
	for _, r := range out.Cause() {
		if r.Kind == "failure" {
			t.Fatalf("typed child failure survived the aborted close: %v", out.Cause())
		}
		cleanup = cleanup || r.Kind == "defect"
	}
	if !cleanup {
		t.Fatalf("the cleanup failure was discarded: %v", out.Cause())
	}
}

// A layer node's owner follows the same rule (the Go side of
// TestJSLayerNodeOwnerIsCancellationAborted): construction that ends by
// interruption aborts the node owner, which discards the unobserved child
// failure, while a construction that fails normally raises it beside its own.
func TestLayerNodeOwnerIsCancellationAborted(t *testing.T) {
	provide := func(construct func(fc *FiberContext) Exit[Unit]) Exit[int] {
		plan := unitPlan([]Node[struct{}]{{Spec: NodeSpec{ID: "node"}, Construct: func(fc *FiberContext, _ *struct{}) Exit[Unit] {
			return construct(fc)
		}}}, func(Unit) struct{} { return struct{}{} })
		return Run(Provide(plan, Unit{}, func(*struct{}) Effect[int] { return func(*FiberContext) Exit[int] { return Succeed(1) } }))
	}
	tags := func(exit Exit[int]) []string {
		out := []string{}
		for _, reason := range exit.Cause() {
			if reason.Kind == "failure" {
				out = append(out, "failure:"+reason.Failure.Tag)
			} else {
				out = append(out, reason.Kind)
			}
		}
		return out
	}
	interrupted := provide(func(fc *FiberContext) Exit[Unit] {
		failed := Invoke(fc, Fork(failingChild("Boom")))
		<-failed.Value.done
		parked := Invoke(fc, Fork(parkedChild()))
		parked.Value.Cancel()
		return Propagate[Unit](Invoke(fc, parked.Value.Join()))
	})
	if got := tags(interrupted); len(got) != 1 || got[0] != "interrupt" {
		t.Fatalf("interrupted construction: got %v, want the interruption alone", got)
	}
	failed := provide(func(fc *FiberContext) Exit[Unit] {
		child := Invoke(fc, Fork(failingChild("Boom")))
		<-child.Value.done
		return Fail[Unit]("Construct", nil)
	})
	if got := tags(failed); len(got) != 2 || got[0] != "failure:Construct" || got[1] != "failure:Boom" {
		t.Fatalf("failed construction: got %v, want Construct then Boom", got)
	}
}
