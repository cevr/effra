# Managed Go runtime

The native prototype uses `runtime/effra`, a Go-standard-library runtime with typed lazy closures and managed goroutines. The JS target emits pinned Effect with a small ownership policy over Effect fibers, scopes and causes. Shared conformance tests establish child-before-parent cleanup, unobserved failure propagation, and timeout cleanup-defect preservation. Go imports, Files, Runtime and Http remain Go-only (EF110). This is a tested common lifecycle subset, not complete provider parity.

## Lifecycle contract

`scope { ... }` executes a block inside a new owning scope and preserves its result after cleanup. A scope goes Open → Closing → Closed: stop admissions, request child cancellation, complete adapter abort hooks and admitted acquisitions, wait for owned child cleanup, then release resources in reverse acquisition order. A successful acquisition that finishes during closure is released once before closure completes.

`fork recipe()` admits a child before starting its goroutine. Its service context is inherited lexically. `run child.join()` awaits its result, `run child.cancel()` requests cancellation, and `run child.interrupt()` requests cancellation and awaits completed cleanup. Normal interruption is acknowledged by interrupt; real child failures/cleanup defects are preserved. Unobserved child failures propagate at owner closure. The checker conservatively includes child failure rows at fork; joining does not erase them.

`.timeout(ms)` is lazy, adds `Timeout` and `Scheduler`, and runs inside an owned deadline scope. It waits for the operation's shutdown and cleanup, so elapsed time can exceed the deadline. In live Go execution it uses the process timer driver; in tests, the explicit scheduler driver controls both `Clock.sleep` and timeout deadlines. A provider that implements only `Clock.sleep` cannot control a timeout. Durations must be 0–2,147,483,647 milliseconds; invalid runtime durations are defects.

`Sync.latch()` creates an opaque, one-shot `Latch`. `Sync.signal(latch)` is idempotent, and `Sync.await(latch)` scopes cancellation to the waiting fiber. A nil handle is an invalid-latch defect. The Go `TestScheduler` exposes `AwaitRegistration` and strong `Adjust` operations: each deadline selection and target commit observes activity, wake reservations and runnable cleanup/publication continuations atomically. A runnable managed continuation blocks advancement; a continuation parked on a registered virtual timer or managed signal may remain pending, so a partial adjustment can return at its target before cleanup completes. A later adjustment or signal resumes that cleanup, while one adjustment drains every reachable intermediate deadline. Only managed waits participate in this observation; unmanaged goroutines and foreign blocking calls remain outside the virtual scheduler and can delay completion. `Advance` remains a compatibility alias for `Adjust`.

Cancellation is cooperative. Managed waits and generated effect boundaries observe Go context. Pure CPU work and blocking foreign APIs may delay shutdown. Go acquisition/registration and both targets’ scope finalizers mask logical cancellation; foreign acquisition must eventually return. An adapter can register an idempotent `Scope.OnCancel` hook before blocking work to unblock an API that needs an explicit close/abort. Hooks must return and must not close or wait on their own owning scope.

Exits preserve named failures, defects and interruption separately, including additional cleanup/child causes. `.catch<Tag>(value)` handles a lone matching failure; it does not erase accompanying cleanup defects. Managed panics become defects while cleanup runs. Fatal runtime/process termination and unmanaged goroutine panics are outside this protocol.

## Small standard library

| Service / provider | Operations | Contract |
| --- | --- | --- |
| Console / Stdout | `log(string) -> ()` | Explicit console capability |
| Clock / LiveClock | `sleep(i64) -> ()` | Cancellation-aware millisecond wait; JS uses bigint input |
| Scheduler / LiveScheduler | `sleep(i64) -> ()`, `advance(i64) -> ()`, `awaitRegistration() -> ()` | Explicit deadline authority; `TestScheduler` controls virtual sleeps/adjustment and `LiveScheduler` is only valid for live execution |
| Sync / TestSync | `latch() -> Latch`, `await(Latch) -> ()`, `signal(Latch) -> ()` | Shared one-shot synchronization; waiter interruption does not consume the handle |
| Env / LiveEnv | `get(string) -> string` | Empty string for absent values; this is not a presence test |
| Files / LiveFiles | `openRead(string) -> File`, `readText(File) -> string`, `readFile(string) -> string` | IoError; openRead attaches release to the current scope; readFile opens a narrower scope |
| Http / GoHttp | `serve(string, handler) -> ()` | IoError; owns listener and waits for request cleanup on shutdown |
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

Automatic source imports now consume Go export data for primitive package functions. Foreign/Host is the explicit capability for these deferred calls. See [interop](interop.md) and [imports example](../examples/imports.ef). Imported fields/methods and persistent signature caches remain unsupported.

## HTTP server

Run `./bin/ef run examples/http.ef` from the repository. It prints `listening http://127.0.0.1:PORT`; port zero lets the OS select an available port. Use that URL with `/health`, `/users/42`, `/users/slow`, `/users/missing`, or `/file`.

`Http.serve` accepts a reference to an effect function taking one string path and returning a string. Its declared service requirements flow into the server recipe. Request failures, defects and interruption become a generic HTTP 500 response; the recipe itself admits listener/startup IoError. The restricted handler reference is not a general higher-order type system.

Each request executes inside a fresh managed scope, with cancellation linked to its connection and server lifetime. SIGTERM stops admission, cancels requests, and waits for handlers and their cleanup before returning. A foreign call ignoring cancellation can delay shutdown; there is no detached timeout escape. The Go entry currently reports interruption with exit status 1. Header reading has a five-second timeout; routing, request bodies, streaming and configurable server policies remain future work.
