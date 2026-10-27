// Package actorlib is the #101 spike's lowering target: the Go that the
// compiler would emit for the std `actor` package if that package is ordinary
// Ergo library code. It is not the std package, and nothing outside the spike
// imports it.
//
// The source it lowers is in docs/research/actor-library/examples
// (actor.go.sketch is the package; counter, roster and breaker are its
// callers). The research note docs/research/actor-library.md records the
// verdict, the guarantees each part proves and the measured costs.
//
// # What is library code here
//
// Nothing in this package is known to a compiler. Every type is an ordinary
// generic type, every operation an ordinary function or method, built on
// runtime/rt tasks and scopes and on Go channels:
//
//   - [Start] runs a handler over a bounded mailbox and returns an owner
//     handle, [Actor]. The handler's requirements are captured at Start.
//   - [Address] is the copyable half: Send and Ask. It is not a resource.
//   - [Reply] is a one-shot reply address carried in a message, as in Gleam
//     OTP and Akka Typed. [Ask] types the reply from the message constructor
//     alone, so a wrong reply type is a Go compile error.
//   - [StartMachine] is a machine-backed actor written on top of Start.
//   - [StartAgent] and [Call] serve any state type with typed calls on a lent
//     *S, whose reply is the call's return value (Elixir Agent). A call runs on
//     the agent's task and receives it, so its waits observe the stop.
//
// # Lowering conventions
//
// An Ergo function with a task parameter takes *rt.Task first, and the
// lowering threads the current task where the source leaves it implicit
// ([Reply.Send] and [Reply.Fail] take the answering task). An error row
// lowers to Go's error; a lone typed failure is returned as itself. A resource
// that Ergo releases at scope end lowers to a Close call on every path where
// it is still owned. A message struct with resource fields gets the generated
// Close of resources.md R3, which releases each of them in reverse
// declaration order and runs every remaining release after a defect. Other
// aggregates get no hand-written drop glue here: a value is released only
// when it implements Close itself (research note, Gap A8). Every value the
// library itself releases (a refused message, an undeliverable answer or
// failure, a queued message, the state) goes through one helper, drop: it
// dispatches on the value held, not on its storage, releases an outcome by
// kind, and runs the release masked in an rt owner scope of the current
// task, so it completes under a stop and a panic in it is an in-release
// defect. A value that crosses
// from one task to another (an initial state, a message, a reply value or
// failure, an agent result or failure, a handler failure, a state snapshot)
// is copied by snapshot, the stand-in for the copy the compiler emits per
// type (runtime.md Tasks rule 6). An actor's exit goes through rt's owner
// scope (rt.Scope.End), so rt classifies every recovered panic.
package actorlib
