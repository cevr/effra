# sketch9: a prototype compiler for the sketch-9 core

A spike. It parses a small Go-shaped subset of the sketch-9 language, checks
typed error rows and capability (`uses`) rows, and emits direct Go. It is its
own Go module (`effra.local/sketch9`), so the root `./...` never sees it.

```sh
go run ./cmd/sketchc check testdata/checkout.sketch
go run ./cmd/sketchc build testdata/checkout.sketch -out /tmp/gen -o /tmp/checkout
./accept.sh   # acceptance check
./bench.sh    # build-time receipt
```

## What it proves

- **The rows check by hand-written rules in about 800 lines.**
  - S101: an error that can escape (through `try`, a returned error value, or the rest of a `try … catch`) must be in the declared `error[...]` row.
  - S102: every capability a body calls, directly or through a callee, must be in `uses` or provided by an enclosing `with`. `timeout.After` requires `Scheduler`. `main` gets only `Console` from the host.
  - S103: a `catch` without `try` names every error in the row.
  - S105: a fallible call must be handled.
  - S106: a wire provider must implement the capability with no wider row.
  - S107–S110: missing return, type misuse, outside the subset, and Go's "declared and not used".
- **The checked program lowers to plain Go with one closure per timeout.** See the shape in `RECEIPT.md`.
  - Capabilities become Go interfaces whose methods take `*rt.Task` first.
  - Functions take a hidden `env *env` (one field per capability) and `t *rt.Task`.
  - `with` copies `env` and sets the wire's fields inline, with no closure.
  - `try` becomes `if err != nil { return zero, err }`.
  - `catch` becomes a type switch on the error.
  - Unions become an interface with a marker method.
  - `timeout.After` is the only Go func literal: the child task's body.
- **Interruption is cooperative and structured.**
  - Every capability call is preceded by a stop check.
  - `rt.After` stops its child on the deadline and waits for the child to finish before it returns `timeout.Expired`.
  - A panic in the child re-panics in the parent as a defect.
  - `rt` has its own tests with a hand-advanced test clock.
- **Edit-rebuild stays in the direct-Go range.** After a literal edit, the emitted checkout program rebuilds in about 0.23 s. Sketch-to-Go takes 4–7 ms (`RECEIPT.md`).

## What it does not prove

- **No generics, loops, channels, `defer` or borrows,** and none of the sketch-9 runtime rules (I1/I2, G1–G4, STM, batching, actors). Go keywords outside the subset parse as S001.
- **Types are names.** There is no inference and no row polymorphism; every function spells its row.
- **One file.** There are no packages and no Go interop.
- **The default arm of a total `catch` is not in the spec.** The spec says a total catch has no default arm. The emitter adds one that passes `rt.Interrupted` on, because interruption is not a typed error and must not be swallowed. An infallible function panics with it, and `rt.Recover` ends `main` with exit code 130.
- **The build-time numbers are for one small program on a shared host.** They are not a comparison with the 1024-operation compile-measure shapes.

## Prior art

**Effra's own Go emitter** (`internal/compiler/emit_go.go`, `callables.go`) lowers every effectful call to a recipe carrier, `efEffect[T]`, which is a `func(efContext) efExit[T]`. It builds the closure and then invokes it. `docs/specs/native-execution-lowering.md` records the cost: a 47.8 s Go build for 1024 sequential operations. It calls for a direct execution entry when the callee and contract are known. This prototype is that direct entry, designed in from the start: a call is a Go call, and a recipe exists only where the source asks for a deferred child (the timeout block). `accept.sh` rejects the `func() efEffect` and `)(ctx)` shapes.

**Borgo** (Rust-like syntax to Go; checked in `compiler/src/codegen.rs`) lowers an enum to a Go struct with an integer `tag` field and one field per constructor, and matches on `subject.tag == X`. A function returning `Result` is emitted as a Go `(T, error)` tuple, and `?` becomes `check := call; if !check.IsOk() { return zero, check.Err }` (`emit_try`, `emit_wrapped_try`). This prototype keeps Borgo's tuple-and-early-return for errors. For sums it uses an interface with a marker method instead of a tagged struct, so a type switch narrows the variant and each variant keeps its own fields.

**XGo (Go+)** (checked in `cl/expr.go`, `compileErrWrapExpr`) has no sum types. `expr?` returns the error from the enclosing function. `expr!` and `expr?:default` lower through a generated closure (`useClosure := v.Tok == token.NOT || v.Default != nil`). This prototype's `catch` covers the default case with a type switch inline, with no closure.

**Gleam** (Erlang and JS targets) represents `Result` as a plain tagged value, `{ok, V}`/`{error, E}` on Erlang and `Ok`/`Error` classes on JS. Custom types are tagged records or classes, and `use` desugars to a callback. The error is a value in a typed union, which this prototype keeps. The typed union becomes the `error[...]` row, which is checked rather than carried at run time. Unlike Gleam, the row is a set of independent struct types, not one declared sum per function.

**ReScript** erases its abstractions in output: variants become plain JS objects or ints, records become objects, and the output reads like hand-written JS. This prototype aims at the same erasure for Go. Rows and `uses` clauses vanish after checking; what remains is interfaces, structs, early returns and type switches a Go programmer would write. The Gleam and ReScript claims come from their documentation and were not re-checked from source here.
