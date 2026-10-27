# Standard library outline

Status: **designed, not yet implemented**, except `timeout.After` and the prelude's `Console` and `Scheduler`, which the prototype has ([runtime.md](runtime.md#timeouts)).

The standard library is the "Effect for Go" track, [#206](https://github.com/cevr/ergo/issues/206). Its module map is the contract: each family states the Effect modules it covers, the construct it uses and its tier. Modules are restated in Ergo's idioms, not ported API for API. Effect is prior art for the guarantees, not a dependency (owner, 2026-10-26). Go names are kept wherever Go has one. Definitions are in the [glossary](../../GLOSSARY.md).

**Tiers.**
- **kernel**: a checker or runtime rule the module needs.
- **library**: ordinary Ergo code over the kernel, with no compiler branch keyed on its identity.
- **host**: a host package, the only code that calls Go.

## Module map

| Family | Ticket | Effect counterpart | Construct or package | Tier |
| --- | --- | --- | --- | --- |
| Tasks and supervision | [#207](https://github.com/cevr/ergo/issues/207) | Fiber, `Effect.all`/`forEach` concurrency, `race`/`raceAll`, FiberSet, FiberMap, FiberHandle, Supervisor | `go`, `Task`, `tasks.Map`, `tasks.Set`, `tasks.Race` | kernel + library |
| Scope and resources | [#65](https://github.com/cevr/ergo/issues/65) | Scope, acquireRelease, ensuring, onExit, addFinalizer, Layer.scoped, forkScoped | automatic release ([resources.md](resources.md)); `build.Scope` for dynamic registration (README Gap 13) | kernel |
| Schedule, retry and timeout | [#179](https://github.com/cevr/ergo/issues/179) | Schedule, retry, repeat, timeout, Clock.sleep, Duration | `schedule`, `retry.On`, `timeout.After(d) { }`, duration literals | library |
| Channels, queues and streams | [#64](https://github.com/cevr/ergo/issues/64) | Queue, PubSub, Mailbox, Stream, Sink, Channel | `chan`, `for range`, iterators that are resources | kernel + library |
| Schema and codecs | [#49](https://github.com/cevr/ergo/issues/49) | Schema, Arbitrary | `ParseX`/`Wire` by name, `json.Codec` | library + coherence rule |
| Sync and STM | [#208](https://github.com/cevr/ergo/issues/208) | Ref, SynchronizedRef, Deferred, Semaphore, Latch, TxRef, TxQueue, TxSemaphore | `sync.Ref`, `sync.Atomically() { }`, `sync.Tx` | kernel + library |
| Batching, caching and pools | [#68](https://github.com/cevr/ergo/issues/68) | Request, RequestResolver, Cache, ScopedCache, Pool, KeyedPool, RcRef, RcMap, ScopedRef | `batch.Resolve` in `wire` | library |
| Config | [#67](https://github.com/cevr/ergo/issues/67) | Config, ConfigProvider, Redacted | `config`, `Redacted[T]` | library |
| Observability | [#69](https://github.com/cevr/ergo/issues/69) | Tracer, Logger, Metric, LogLevel, References | `with(trace.Span(…))`, `log`, `metric` | library on kernel G1/G2 ([#200](https://github.com/cevr/ergo/issues/200)) |
| Clock, random and testing | [#209](https://github.com/cevr/ergo/issues/209) | Clock, Random, Console, TestClock, TestServices, TestContext, `it.effect` | prelude capabilities, `testing.VirtualClock` in `wire` | kernel seams + library |
| HTTP | [#51](https://github.com/cevr/ergo/issues/51) | HttpClient, HttpServer, HttpRouter, HttpApi | `func(http.Request) http.Response`, total `catch` | library + host |
| RPC | [#53](https://github.com/cevr/ergo/issues/53) | Rpc, RpcGroup | over `http` and `json` | library |
| HTML | [#212](https://github.com/cevr/ergo/issues/212) | (none; Lustre and Elm prior art) | `html` functions | library |
| Actors | [#57](https://github.com/cevr/ergo/issues/57) | cluster Entity (OTP and Gleam prior art) | `actor.Start(machine, n)` | library |
| Host packages | [#210](https://github.com/cevr/ergo/issues/210) | platform FileSystem, Path, Command, Socket; sql | `os`, `fs`, `net`, `sql` with cancel hooks | host |
| Core data | [#211](https://github.com/cevr/ergo/issues/211) | Option, Array, Record, HashMap, HashSet, Chunk, Order, Equivalence, Equal, Hash, Data | prelude `Option`, `slices`, `maps`, `cmp` | library + built-in constraints |

Out of scope for now: durable workflows.

## Prelude

The prelude is in scope in every package without an import: `Option`, `Task`, `Decision`, `Clock`, `Scheduler`, `Random` and `Console`. The four capabilities are ordinary interfaces bound by `wire` (decision #102), so a test replaces any of them ([#209](https://github.com/cevr/ergo/issues/209)).

## Packages by origin

- **Same as Go:** `strings`, `slices`, `maps`, `cmp`, `math`, `time`, `os`, `fs`, `net`, `sql`.
- **Same name, extended:**
  - `http`: client, handlers, routes, server limits;
  - `json`: `Codec`, `Decode`, `Encode`, `ParseX`/`Wire`;
  - `sync`: `Ref`, `TxRef`, `Atomically`, `Latch`;
  - `testing`: `T`, `VirtualClock`, `Discard`, `Idle`.
- **New:** `tasks`, `schedule` (the schedule algebra, with laws), `retry`, `timeout`, `trace`, `log`, `metric`, `batch`, `actor`, `html`.

## Tasks

[#207](https://github.com/cevr/ergo/issues/207). `tasks.Map(xs, limit, f)` keeps input order and runs at most `limit` calls at once; `limit >= 1` is a precondition. It waits for whichever task ends first, so a failure stops the rest promptly. It collects into `[]Option[B]` slots. `tasks.Race` is n-ary. `tasks.Set` is G4 ([runtime.md](runtime.md#g4-tasksset)). The race and fail-fast laws pass as tests. *Prior art:* Go `errgroup` with `SetLimit`; Effect `Effect.forEach` with `concurrency`; Elixir `Task.async_stream` keeps order with `ordered: true`.

## Sync and STM

[#208](https://github.com/cevr/ergo/issues/208).

1. `sync.Ref`, `Latch`, `Semaphore` and `Deferred`. A blocking wait is an interruption point.
2. `sync.TxRef[T]`, `sync.Atomically() { … }` and `sync.Retry()`. `Load` and `Store` need the `sync.Tx` capability, which only `Atomically` provides.
3. A transaction can re-run, so its body is a replayable parameter ([types.md](types.md#function-values)): its footprint holds only `sync.Tx`, and I/O inside a transaction is an error.
4. `Retry` rolls back, registers the read set and waits, interruptibly, until a `TxRef` it read changes. No lock-freedom or starvation-freedom is claimed.

*Prior art:* Haskell STM, ZIO STM, Effect `TxRef`; Go offers mutexes only.

## Batching

[#68](https://github.com/cevr/ergo/issues/68). A provider implements `FindMany`, and `wire` wraps it: `Users = batch.Resolve(UsersDB, batch.Window(2ms), batch.Max(100))`. Go has no scheduler tick to batch on, so the window is explicit. Duplicate keys share one result. A stopped waiter leaves the batch; the batch is cancelled when no waiter is left. "One query" holds only when every call arrives inside the window. *Prior art:* DataLoader states its scheduling policy; Haxl, ZIO Query and Effect `RequestResolver` batch per round.

## HTTP

[#51](https://github.com/cevr/ergo/issues/51). A handler is an ordinary `func(http.Request) http.Response`. A `catch` without `try` must cover every error, so a handler cannot leave a decode, handler or encode failure without a status. Routes use Go 1.22 `ServeMux` patterns: `"POST /orders": placeOrder`. Tests call `api.Serve(req)` with no server. Declared difference: an unmatched method answers 405, as `ServeMux` does. Open trade: handlers lose codec identity, so `ergo inspect` derives endpoint metadata from signatures and literal statuses in `catch` arms. *Prior art:* Effect `HttpApi` declares endpoints; Go `net/http` handlers; Phoenix controllers.

## HTML

[#212](https://github.com/cevr/ergo/issues/212). Owner decision, 2026-10-26: HTML is "Functions", not markup syntax. There is no markup grammar.

```go
import . "html"

func OrderRow(o Order) Node {
	return Tr(Class(statusClass(o.Status)),
		Td(Text(o.Member.Name)),
		Td(Int(o.Quantity.Wire())),
	)
}
```

1. The `html` package is ordinary functions: elements (`Tr`, `Td`, `P`, …), attributes (`Class`, `Href`, …), `Text`, `Int`, `Each` and `When`. A component is a function that returns `html.Node`.
2. An element takes `...Child`, where `Child` is the closed union `Attr | Node`. A void element such as `Br` or `Img` takes only `...Attr`, so it cannot get children.
3. Escaping is decided by type. `Text` escapes its string; only an `html.Node` is inserted as markup. `Href` and `Src` take a `url.Safe`, not a string. `Raw` takes an `html.Trusted`, which only `html.Trust` builds, so `ergo inspect` lists every trust site.
4. **Lowering.** `html.Node` is a render function that writes to a buffered writer; `Text` escapes at write time, so no intermediate string is built. Folding static subtrees at compile time is a later, measured optimisation. `http.HTML(status, node)` renders a node into a response.

*Prior art:* Gleam Lustre (`html.td([], [text(name)])`), whose surface Ergo takes; Elm `Html`, which escapes by type with separate attribute and child lists, merged here into one `...Child` union to keep Go's call shape; templ (Go), whose render-function representation Ergo takes, not its syntax; Phoenix HEEx (Elixir), whose compile-time validation Ergo gets from closed types instead of a parser.

## Host packages

[#210](https://github.com/cevr/ergo/issues/210). `os`, `fs`, `net` and `sql` are the only standard-library code that calls Go. They are reviewed. Every blocking call has a cancel hook, or is recorded as an uninterruptible host wait that `ergo inspect` lists (G8). Each package states the Go targets it supports. File handles are resources ([resources.md](resources.md)). Through host packages and `import go`, every effect appears in a footprint ([rows.md](rows.md#footprints)).

## Testing

[#209](https://github.com/cevr/ergo/issues/209). `testing.VirtualClock` provides `Clock` and `Scheduler` and jumps to the next timer when every task in the test is blocked. `T.Idle()` reports the tasks that keep a test from reaching idle. The test harness is an ordinary `wire`, so production code needs no change to run on virtual time. *Prior art:* Go `testing/synctest` bubbles; Trio `MockClock(autojump_threshold=0)`; Effect `TestClock`.
