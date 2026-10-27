package examples

import (
	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/runtime/rt"
)

// BreakerState lowers `type State interface{ Closed | Open | HalfOpen }`.
type BreakerState interface{ isBreakerState() }

// Closed counts consecutive failures.
type Closed struct{ Failures int64 }

// Open remembers the endpoint that tripped it.
type Open struct{ Endpoint string }

// HalfOpen lets one probe through.
type HalfOpen struct{ Endpoint string }

func (Closed) isBreakerState()   {}
func (Open) isBreakerState()     {}
func (HalfOpen) isBreakerState() {}

// Signal lowers `type Signal interface{ Failure | Success | Retry | Retire }`.
type Signal interface{ isSignal() }

// Failure reports a failed call to an endpoint.
type Failure struct{ Endpoint string }

// Success reports a successful call.
type Success struct{}

// Retry asks an open breaker to probe its endpoint.
type Retry struct{}

// Retire ends the breaker.
type Retire struct{}

func (Failure) isSignal() {}
func (Success) isSignal() {}
func (Retry) isSignal()   {}
func (Retire) isSignal()  {}

// ProbeDown lowers `type ProbeDown struct{ Endpoint string }`.
type ProbeDown struct{ Endpoint string }

func (e ProbeDown) Error() string { return "probe down: " + e.Endpoint }

// Probe lowers the capability `type Probe interface{ Check(endpoint string) error[ProbeDown] }`.
// Capability methods take the task first (rows.md, Capability rows rule 7).
type Probe interface {
	Check(t *rt.Task, endpoint string) error
}

// breakerEnv is the hidden environment of a function with `uses Probe`.
type breakerEnv struct{ probe Probe }

// Trip is the number of consecutive failures that opens the breaker.
const Trip = 3

// BreakerDecision is the step's result type, Decision[State, string].
type BreakerDecision = actorlib.Decision[BreakerState, string]

// step lowers the sketch's `step`, a two-subject switch, as nested type
// switches. The interruption point before the capability call is I1.
func step(t *rt.Task, env *breakerEnv, s BreakerState, sig Signal) (BreakerDecision, error) {
	type enter = actorlib.Enter[BreakerState, string]
	type stay = actorlib.Stay[BreakerState, string]
	switch s := s.(type) {
	case Closed:
		switch sig := sig.(type) {
		case Failure:
			if s.Failures+1 >= Trip {
				return enter{Next: Open{Endpoint: sig.Endpoint}}, nil
			}
			return stay{Next: Closed{Failures: s.Failures + 1}}, nil
		case Success:
			return stay{Next: Closed{Failures: 0}}, nil
		}
	case Open:
		if _, ok := sig.(Retry); ok {
			if t.Stopped() {
				return nil, rt.Interrupted
			}
			if err := env.probe.Check(t, s.Endpoint); err != nil {
				return nil, err
			}
			return enter{Next: HalfOpen{Endpoint: s.Endpoint}}, nil
		}
	case HalfOpen:
		switch sig := sig.(type) {
		case Success:
			return enter{Next: Closed{Failures: 0}}, nil
		case Failure:
			return enter{Next: Open{Endpoint: sig.Endpoint}}, nil
		}
	}
	if _, ok := sig.(Retire); ok {
		return actorlib.Done[BreakerState, string]{Output: "retired"}, nil
	}
	if _, ok := s.(Open); ok {
		return actorlib.Reject[BreakerState, string]{}, nil
	}
	return actorlib.Ignore[BreakerState, string]{}, nil
}

// Breaker lowers `type Breaker machine[State, Signal] { initial Closed{0}; step step }`
// as a machine value. The step's `uses Probe` is captured here, where the
// value is built: the caller's environment at `actor.Start(Breaker{}, 16)`.
func Breaker(probe Probe) actorlib.Machine[BreakerState, Signal, string] {
	env := &breakerEnv{probe: probe}
	return actorlib.Machine[BreakerState, Signal, string]{
		Initial: Closed{Failures: 0},
		Step: func(t *rt.Task, s BreakerState, sig Signal) (BreakerDecision, error) {
			return step(t, env, s, sig)
		},
	}
}

// StartBreaker lowers `actor.Start(Breaker{}, 16)`.
func StartBreaker(s *rt.Scope, probe Probe, capacity int) actorlib.MachineActor[BreakerState, Signal, string] {
	return actorlib.StartMachine(s, "breaker", Breaker(probe), capacity)
}

// RetryOrReject lowers the sketch's
// `d := try b.Ask(Retry{}) catch { case ProbeDown: Reject{} }`.
func RetryOrReject(t *rt.Task, b actorlib.MachineAddress[BreakerState, Signal, string]) (BreakerDecision, error) {
	d, err := b.Ask(t, Retry{})
	if err != nil {
		if f, ok := rt.Failure(err); ok {
			if _, ok := f.(ProbeDown); ok {
				return actorlib.Reject[BreakerState, string]{}, nil
			}
		}
		return nil, err
	}
	return d, nil
}
