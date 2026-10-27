// Package actorlib is the #101 spike's lowering target: the Go that the
// compiler would emit for the std `actor` package if that package is ordinary
// Effra library code. It is not the std package, and nothing outside the spike
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
// An Effra function with a task parameter takes *rt.Task first. An error row
// lowers to Go's error; a lone typed failure is returned as itself. A resource
// that Effra releases at scope end lowers to a Close call on every path where
// it is still owned. A message struct with a [Reply] field gets the generated
// Close of resources.md R3, which closes the reply. A value that crosses
// from one task to another (a message, a reply, an agent result, a state
// snapshot) is copied by snapshot, the stand-in for the copy the compiler
// emits per type (runtime.md Tasks rule 6).
package actorlib
