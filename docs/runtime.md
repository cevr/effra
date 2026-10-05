# Managed Go runtime

The native prototype uses `runtime/effra`, a Go-standard-library runtime with typed lazy closures and managed goroutines. The sequential JS subset emits pinned Effect directly. Scopes, fibers, timeout, Files and Runtime are currently Go-only and checked with EF110; sharing surface syntax alone does not establish backend parity.

## Lifecycle contract

`scope { ... }` executes a block inside a new owning scope and preserves its result after cleanup. A scope goes Open → Closing → Closed: stop admissions, request child cancellation, complete adapter abort hooks and admitted acquisitions, wait for owned child cleanup, then release resources in reverse acquisition order. A successful acquisition that finishes during closure is released once before closure completes.

`fork recipe()` admits a child before starting its goroutine. Its service context is inherited lexically. `run child.join()` awaits its result, `run child.cancel()` requests cancellation, and `run child.interrupt()` requests cancellation and awaits completed cleanup. Normal interruption is acknowledged by interrupt; real child failures/cleanup defects are preserved. Unobserved child failures propagate at owner closure. The checker conservatively includes child failure rows at fork; joining does not erase them.

`.timeout(ms)` is lazy, adds Timeout, and runs inside an owned deadline scope. It waits for the operation's shutdown and cleanup, so elapsed time can exceed the deadline. It currently uses the native runtime's wall clock. It does **not** add a Clock service requirement or claim injected Clock.sleep controls deadlines; a replaceable deadline/time abstraction remains future work. Durations must be 0–2,147,483,647 milliseconds; invalid runtime durations are defects.

Cancellation is cooperative. Managed waits and generated effect boundaries observe Go context. Pure CPU work and blocking foreign APIs may delay shutdown. Acquisition/registration and finalizers mask logical cancellation; foreign acquisition must eventually return. An adapter can register an idempotent `Scope.OnCancel` hook before blocking work to unblock an API that needs an explicit close/abort. Hooks must return and must not close or wait on their own owning scope.

Exits preserve named failures, defects and interruption separately, including additional cleanup/child causes. `.catch<Tag>(value)` handles a lone matching failure; it does not erase accompanying cleanup defects. Managed panics become defects while cleanup runs. Fatal runtime/process termination and unmanaged goroutine panics are outside this protocol.

## Small standard library

| Service / provider | Operations | Contract |
| --- | --- | --- |
| Console / Stdout | `log(string) -> ()` | Explicit console capability |
| Clock / LiveClock | `sleep(i64) -> ()` | Cancellation-aware millisecond wait; JS uses bigint input |
| Env / LiveEnv | `get(string) -> string` | Empty string for absent values; this is not a presence test |
| Files / LiveFiles | `openRead(string) -> File`, `readText(File) -> string`, `readFile(string) -> string` | IoError; openRead attaches release to the current scope; readFile opens a narrower scope |
| Runtime / RuntimeLive | `inspect() -> string` | JSON metadata for the current owning scope |

Files use synchronized managed handles; using a handle after its owner closes produces IoError. This is a runtime guard, not region typing or proof against every mutable alias. Native reads are ordinary blocking Go file reads and may delay cancellation. File reading currently buffers the entire content; streaming/bounded I/O remains future work.

Runtime snapshots report scope ID/state, acquisition count, cancellation request, resource count/labels, and child states Running/Cancelling/Done. Child completion means its cleanup completed. Lists are limited to 100 entries, labels to 256 bytes, and truncation is reported. Closed scopes clear owned references. Snapshots are observational, not an atomic freeze of all tasks; labels may identify application resources. No payload dump, process-wide task traversal, wait-reason tracing, or MCP runtime control is exposed.

Try `./bin/ef run examples/lifecycle.ef`. Each provider remains explicit and replaceable through the same nominal service interface used by user implementations.

## Go interop seam

At the Go runtime interface, `FromGo(func(context.Context) (A,error))` constructs a deferred call. It passes the managed context and preserves both returned values in `GoResult[A]`; a returned error is initially native data. The adapter must honestly state whether its API observes context. No goroutine is spawned just to hide a blocking call, and no signature is treated as proof of cancellation or purity.

`OrFail(recipe)` explicitly adapts the returned error to GoError. Its payload contains both the native error and partial value. A panic remains a defect; observed context cancellation remains interruption. Resource-owning APIs use `AcquireRelease`, whose acquisition error is currently classified as IoError in this prototype; more general domain acquisition policies remain future work. APIs needing an abort hook register `OnCancel` before admission to blocking work.

The runnable [Go example](../examples/go-interop/main.go) calls real `io.ReadFull`, retaining its partial count and `unexpected EOF`, then demonstrates explicit error adaptation and a context-aware cancellation boundary:

```sh
go run ./examples/go-interop
# raw: count=3 data="abc" error=unexpected EOF
# adapted: GoError partial=3 error=unexpected EOF
# cancelled SDK: Timeout
```

This proves the Go runtime adapter seam. Automatic host imports from `.ef`, imported fields/methods, module-aware signature caches and supplemental package binding metadata are **not implemented**. The empty Foreign/Host capability is reserved for that future source binding layer; it does not grant an implemented foreign-call syntax. [interop.md](interop.md) remains the broader direction: import native declarations automatically and add behavior contracts without per-symbol signature boilerplate.
