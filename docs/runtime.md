# Managed Go runtime

The native prototype uses `runtime/effra`, a Go-standard-library runtime with typed lazy closures and managed goroutines. The JS target emits pinned Effect with a small ownership policy over Effect fibers, scopes and causes. Shared conformance tests establish child-before-parent cleanup, unobserved failure propagation, and timeout cleanup-defect preservation. Go imports, Files and Runtime remain Go-only (EF110); Http/LiveHttp runs on both targets. This is a tested common lifecycle subset, not complete provider parity.

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
| Console / Stdout | `log(string) -> void` | Explicit console capability |
| Clock / LiveClock | `sleep(i64) -> void` | Cancellation-aware millisecond wait; JS uses bigint input |
| Scheduler / LiveScheduler | `sleep(i64) -> void`, `advance(i64) -> void`, `awaitRegistration() -> void` | Explicit deadline authority; `TestScheduler` controls virtual sleeps/adjustment and `LiveScheduler` is only valid for live execution |
| Sync / TestSync | `latch() -> Latch`, `await(Latch) -> void`, `signal(Latch) -> void` | Shared one-shot synchronization; waiter interruption does not consume the handle |
| Env / LiveEnv | `get(string) -> string` | Empty string for absent values; this is not a presence test |
| Files / LiveFiles | `openRead(string) -> File`, `readText(File) -> string`, `readFile(string) -> string` | IoError; openRead attaches release to the current scope; readFile opens a narrower scope |
| Http / LiveHttp | `serve(string, Handler) -> void`, `listen(string, HttpLimits, HttpHandler) -> void`, `text(string) -> bytes` | IoError; owns listener, bounds requests, and waits for request cleanup on shutdown. Go and JS |
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

Each request executes inside a fresh managed scope, with cancellation linked to its connection and server lifetime. SIGTERM stops admission, cancels requests, and waits for handlers and their cleanup before returning. A foreign call ignoring cancellation can delay shutdown; there is no detached timeout escape. The Go entry currently reports interruption with exit status 1. `serve` is the raw path-to-text control: header reading has a five-second timeout and it reads no request bodies.

### Managed transport

`Http.listen(address, limits, handler)` is the bounded buffered HTTP/1.1 transport; [examples/http-transport.ef](../examples/http-transport.ef) is the public program. A reference to the global `Http` or `LiveHttp` (a service row, `provide<Http>`, an operation call or the provider value), as resolved by the checker's single binding phase before builtin data is registered, admits these builtin records. A local value or row parameter of either name is not a reference anywhere in its scope, including explicit constructor types. Only checked uses of `LiveHttp` emit the transport implementation:

```effra
record HttpRequest { method: string, path: string, contentType: string, body: bytes }
record HttpResponse { status: i64, contentType: string, body: bytes }
enum HttpReply { Respond { response: HttpResponse }, BadRequest, NotFound, UnsupportedMediaType }
record HttpLimits { maxBodyBytes: i64, readHeaderMillis: i64, readBodyMillis: i64, idleMillis: i64, maxActive: i64 }
```

The handler is an effect function `HttpRequest -> HttpReply`. Its service requirements flow into the server recipe and its typed failures are absorbed by the transport (inspection reports them as `AbsorbedFailures`). `path` is the request-target path exactly as received, without query or percent-decoding; for an absolute-form target it is the path after the authority (`/` when empty), and the query is removed first. `contentType` is empty when absent. Every limit is required; timeouts must lie in 1..2147483647 ms, `maxActive` in 1..2147483647 and `maxBodyBytes` in 0..2^53-1, otherwise the recipe dies before binding.

| Condition | Response |
| --- | --- |
| `Respond` | its status (200..599), body and Content-Type (omitted when empty; never sniffed) |
| `BadRequest` / `NotFound` / `UnsupportedMediaType` | 400 / 404 / 415, empty body, no Content-Type |
| admitted requests at `maxActive`, or shutdown begun | 503, `Connection: close`, handler not run |
| declared or chunked body over `maxBodyBytes` | 413, `Connection: close`, handler not run |
| malformed body framing | 400, `Connection: close`, handler not run |
| headers or body not received in time | connection closed without a response |
| handler failure, defect, cleanup failure, or invalid response | 500, empty body |
| `Respond` Content-Type outside visible ASCII, space and tab | 500, empty body (invalid response) |
| server shutdown cancels the handler | 503, `Connection: close`, after the request scope closed |
| client disconnect | handler cancelled; no further bytes |

A request holds its admission from body read until its response has been handed to the operating system or its connection closed, so `maxActive` also bounds buffered responses a slow client has not received. Shutdown stops admission, cancels and joins every request scope, then gives responses still being written `idleMillis` to complete before aborting their connections, and finally closes the listener. A client that stops reading therefore never holds shutdown open, and owned cleanup always completes before any transport abort. Go closes the listener at cancellation; JS keeps it open, answering 503, until the drain ends, because `node:http`'s `close()` would also destroy responses still being written. Bun's `node:http` accepts a whole response at once, so on Bun these response bounds are Bun's own buffering.

The Content-Type policy (visible ASCII, space and horizontal tab) is the intersection of what Go and Node publish unchanged; validation precedes any byte of the response, and a host rejection during publication becomes a 500 (or aborts a response already started) instead of escaping the transport.

Routing, method selection and media-type policy belong to the handler; the example answers a wrong method on a known path with 404, matching the selected profile. Every response is written only after the request scope, including handler resources and children, has closed, so a cleanup failure is never published as success. On JS the transport uses `node:http` (Node or Bun) with Effect fibers; the generated entry interrupts `main` on SIGINT/SIGTERM. General headers, query parameters, streaming bodies, typed endpoints and codecs remain future work.
