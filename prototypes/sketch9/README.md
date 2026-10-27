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

## Kernel mode and traces (#188)

```sh
go run ./cmd/sketchc build -rt kernel testdata/trace/timeout.sketch -out /tmp/gen -o /tmp/timeout
/tmp/timeout                                  # records nothing
EFFRA_TRACE=/tmp/timeout.jsonl /tmp/timeout   # records the run as JSON Lines
go run ./cmd/sketchc trace testdata/trace/timeout.sketch /tmp/timeout.jsonl
go run ./cmd/sketchc trace -json testdata/trace/timeout.sketch /tmp/timeout.jsonl
```

- **`-rt kernel`** links the merged kernel runtime, `effra.local/prototype/runtime/rt`, instead of copying the embedded prototype runtime. The emitted `go.mod` replaces that module with the repository checkout. `sketchc` finds the checkout above the file or the working directory, or `-rtroot` names it. The default mode (`-rt embedded`) is unchanged. For all 40 accepted programs in `testdata/checkout.sketch` and `testdata/probes`, its `main.go` is byte-identical to the base `854f21e7` output.
- **Definition IDs come from the checker.** `NewModel` (`sketch/defs.go`) derives every ID from the checked declarations. A declaration is `<package>.<Name>`, a method is `<package>.<Type>.<Method>`, and a block is `<enclosing>/<construct>#<n>` (for example `latency.checkout/timeout.After#1`). Host definitions are `host.Console.Println`, `host.Scheduler.Sleep` and `timeout.Expired`. The emitter writes the IDs it uses as constants (`def__N`), with the source revision (SHA-256 of the file) and a site table for the task definitions. It writes no ID by hand and computes none at run time.
- **Events.** The kernel runtime raises the task events. Around each capability call, the emitted code raises `call` before it and `call-exit` after it, with the callee's ID, the call site and the call's occurrence ID, which it holds in a local (`c__N := t__.TraceCall(...)`, then `t__.TraceCallExit(c__N, ..., err)`). The call-exit carries the outcome: ok, the failure, or interrupted; the runtime adds a defect when a panic leaves the call. In every frame the interruption point is the `if t__.Stopped()` form, so it can raise `interrupt` at its site. A `with` raises `construct` with the wire's ID, and a catch arm raises `handler` with the handled error's ID; the runtime links the handler to the occurrence it caught. `main` reads `EFFRA_TRACE`. When it is unset, the root has no tracer and the program writes no file.
- **`sketchc trace [-json] FILE TRACE`** reads at most 8 MiB of TRACE, through a limit one byte past the budget, so a larger file is refused without being loaded. It validates the trace against the model of FILE before it explains anything, and refuses (with the reason) a trace with the wrong schema or revision, a header count that does not match, an unknown definition ID, a task event whose site or definition is not its task's, a point event the model has no point for, an event whose parent differs from its task's start, an event before its task's start (other than a stop request or a deadline), a repeated occurrence ID or exit, a parent graph with no single root, a cycle or a child started before its parent, a link to a missing or later occurrence or of the wrong kind, a link on a `body-exit`, two handlers of one occurrence, an exit reason whose `origin` cannot raise that reason (or that shares its origin with another reason), or a defect chain with an unknown definition, a frame of the wrong task or out of parent order. It prints the task tree, every capability call with its outcome and its span (call to call-exit), every event with its occurrence ID, link and reason origins, the cause trace of the root exit and the named omissions. The cause trace explains each reason of an exit from that reason's own recorded origin (a failure its failed call, deadline or child exit; a defect its panic; an interruption its stop request); without one it says "origin not recorded" for that reason and labels each candidate. The trace schema is `effra-trace-3`. `-json` prints the `trace-evidence-1` envelope (`schema`, `producer`, `revision`, `budgets`, `omissions`, `fingerprint`, then the tasks, the calls, the events and the cause trace). `testdata/trace/two-calls` and `two-timeouts` are the round-1 reviewer probes, each with a recorded trace.
- **Subset extension: `Scheduler.Sleep(d)`.** The timeout example needs a gateway slower than its deadline. A provider method cannot use a capability in this subset (S109), so the latency is in an ordinary function that `uses Scheduler`. `Sleep` takes a `duration` and has no error row, because an interruption is not a typed failure. Both runtimes lower it to `rt.Sleep(t, scheduler, d)`, which returns `Interrupted` when the task stops while it waits; the emitted code passes that on like any other interruption. Its argument type is checked by the existing S108 rule. `testdata/reject/sleep-not-duration.sketch` rejects an integer argument, and `testdata/probes/sleep-timeout.sketch` runs it under both outcomes of a timeout.

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
- **The subset narrowed after review round 1.** These would each have needed more lowering than a spike should carry:
  - a top-level `wire` must be closed: its providers require no capability from the caller and let no failure escape. A provider that needs the caller's capabilities, or can fail, goes in an inline `wire{}`, where the enclosing function pays for it. A wire that needs itself to build is rejected;
  - `&&` and `||` are rejected, because they need control flow around hoisted calls;
  - `==` works on basic types only, and there is one integer type, `int64`;
  - `with` and `timeout.After` blocks must end with an expression and cannot `return`;
  - a struct literal must set every field that has no zero value (a union, a capability, or a struct holding one);
  - host capabilities take only host providers (`Scheduler = LiveScheduler{}`).
- **Types are names.** There is no inference and no row polymorphism; every function spells its row.
- **One file.** There are no packages and no Go interop.
- **The default arm of a total `catch` is not in the spec.** The spec says a total catch has no default arm. The emitter adds one that passes `rt.Interrupted` on, because interruption is not a typed error and must not be swallowed. An infallible function panics with it, and `rt.Recover` ends `main` with exit code 130.
- **The build-time numbers are for one small program on a shared host.** They are not a comparison with the 1024-operation compile-measure shapes.

## Prior art

**Effra's own Go emitter** (`internal/compiler/emit_go.go`, `callables.go`) lowers every effectful call to a recipe carrier, `efEffect[T]`, which is a `func(efContext) efExit[T]`. It builds the closure and then invokes it. `docs/specs/native-execution-lowering.md` records the cost: a 47.8 s Go build for 1024 sequential operations. It calls for a direct execution entry when the callee and contract are known. This prototype is that direct entry, designed in from the start: a call is a Go call, and a recipe exists only where the source asks for a deferred child (the timeout block). `accept.sh` rejects the `func() efEffect` and `)(ctx)` shapes.

**Borgo** (Rust-like syntax to Go; checked at `3b9f0157`, `compiler/src/codegen.rs`) lowers an enum to a Go struct with an integer `tag` field plus payload fields, and matches on `subject.tag == X` (`emit_enum`, `:1117`). It has two `Result` paths. When the error argument is a Go interface, a `Result`-returning call is emitted as a Go `(T, error)` tuple, and `?` becomes an `err != nil` check and early return (`:1598`, `:1738`, `:1898`). Otherwise the `Result` value is kept, and `?` checks `IsOk()` on it (`emit_try`, `:1275`). This prototype uses only the tuple-and-early-return path, because every error row is a set of struct types that implement Go's `error`. For sums it uses an interface with a marker method instead of a tagged struct, so a type switch narrows the variant and each variant keeps its own fields. The size and allocation trade-off between the two sum representations is not measured here.

**XGo (Go+)** (checked at `26e013d1`, `cl/expr.go`, `compileErrWrapExpr`) uses Go error results. `expr?` returns the error from the enclosing function, inline. `expr!` and `expr?:default` lower through a generated closure (`useClosure := v.Tok == token.NOT || v.Default != nil`). This prototype's `catch` covers the default case with a type switch inline, with no closure. That one function says nothing about XGo sum types, and this README makes no claim about them.

**Gleam** (Erlang and JS targets) represents `Result` as a plain tagged value, `{ok, V}`/`{error, E}` on Erlang and `Ok`/`Error` classes on JS. Custom types are tagged records or classes, and `use` desugars to a callback. The error is a value in a typed union, which this prototype keeps. The typed union becomes the `error[...]` row, which is checked rather than carried at run time. Unlike Gleam, the row is a set of independent struct types, not one declared sum per function.

**ReScript** (checked at `30ce3698`, `compiler/ml/variant_runtime.ml`) erases its abstractions in output, with representation chosen per variant: payload-free constructors become string literal tags by default (or declared literal tags: numbers, booleans, null, undefined); constructors with payloads become tagged objects; and `@unboxed` variants drop the tag when their payload shapes can be told apart at run time. Records become objects, and the output reads like hand-written JS. This prototype aims at the same erasure for Go. Rows and `uses` clauses vanish after checking. What remains is interfaces, structs, early returns and type switches a Go programmer would write. The runtime contracts stay: interruption checks, the joined timeout child, and defect propagation. The Gleam claims come from its documentation and were not re-checked from source here.

**Elixir/OTP** (`lib/elixir/lib/task.ex`, `Task.shutdown/2`) shuts a task down and then consumes its monitor result, keeping an abnormal exit rather than reporting only "shut down". Round 1 of this prototype's review showed that `rt.After` threw away a drained child's defect on the deadline and parent-stop paths. It now re-panics with it on every path, as `Task.shutdown` keeps the exit. BEAM can also kill a process outright; the sketch's stop is cooperative, so the waited-for child always runs to the end of its own code.
