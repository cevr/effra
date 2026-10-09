package effra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type rejected struct{ reason string }

// quiet is the payload type of a fieldless declared error.
type quiet struct{}

func TestRecoverBindsPayloadAndKeepsCompositeCauses(t *testing.T) {
	calls := 0
	handler := func(payload rejected) Effect[string] {
		calls++
		return func(*FiberContext) Exit[string] { return Succeed("recovered " + payload.reason) }
	}
	failing := func(exit Exit[string]) Effect[string] {
		return func(*FiberContext) Exit[string] { return exit }
	}
	matched := Run(Recover(failing(Fail[string]("Rejected", rejected{"payload"})), "Rejected", handler))
	if matched.IsFailure() || matched.Value != "recovered payload" || calls != 1 {
		t.Fatalf("payload not delivered: %+v", matched)
	}
	// A fieldless declared error carries no payload; the handler receives
	// that declaration's empty value.
	quietCalls := 0
	quietHandler := func(quiet) Effect[string] {
		quietCalls++
		return func(*FiberContext) Exit[string] { return Succeed("recovered quiet") }
	}
	empty := Run(Recover(failing(Fail[string]("Quiet", nil)), "Quiet", quietHandler))
	if empty.IsFailure() || empty.Value != "recovered quiet" || quietCalls != 1 {
		t.Fatalf("empty payload: %+v", empty)
	}
	for name, exit := range map[string]Exit[string]{
		"cleanup defect": FromCause[string](Cause{{Kind: "failure", Failure: &Failure{"Rejected", rejected{"x"}}}, {Kind: "defect", Err: errors.New("release")}}),
		"two failures":   FromCause[string](Cause{{Kind: "failure", Failure: &Failure{"Rejected", rejected{"x"}}}, {Kind: "failure", Failure: &Failure{"Rejected", rejected{"y"}}}}),
		"interruption":   Interrupt[string](context.Canceled),
		"defect":         Die[string](errors.New("boom")),
		"other failure":  Fail[string]("Busy", nil),
	} {
		out := Run(Recover(failing(exit), "Rejected", handler))
		if !out.IsFailure() || len(out.Cause()) != len(exit.Cause()) || calls != 1 {
			t.Fatalf("%s reached recovery: %+v", name, out)
		}
	}
	mismatched := Run(Recover(failing(Fail[string]("Rejected", "not a payload")), "Rejected", handler))
	if mismatched.Defect == nil || calls != 1 {
		t.Fatalf("mismatched payload was not a defect: %+v", mismatched)
	}
}

func TestRecoverCancellationRunsHandlerCleanup(t *testing.T) {
	var released, published atomic.Bool
	program := Recover(func(*FiberContext) Exit[string] { return Fail[string]("Rejected", rejected{"slow"}) }, "Rejected", func(rejected) Effect[string] {
		return Scoped(func(fc *FiberContext) Exit[string] {
			acquired := Invoke(fc, AcquireRelease("handler", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error { released.Store(true); return nil }))
			if acquired.IsFailure() {
				return Propagate[string](acquired)
			}
			<-fc.Context().Done()
			published.Store(true)
			return Succeed("late")
		})
	})
	out := Run(Timeout(program, 5))
	if out.Failure == nil || out.Failure.Tag != "Timeout" || !released.Load() {
		t.Fatalf("recovery handler cancellation skipped cleanup: %+v", out)
	}
	if !published.Load() {
		t.Fatal("handler did not observe cancellation")
	}
}
