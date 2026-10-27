# Traceability by construction

Spike [#188](https://github.com/cevr/effra/issues/188), parent [#192](https://github.com/cevr/effra/issues/192). Branch `wf10/trace-188`, base `854f21e7`. Status: the runtime hook is designed to merge; the emission, the query and `sketchc trace` are a prototype in `prototypes/sketch9`; `ef trace` and MCP `code.trace` are proposed. Raw measurements are in the unit directory `/home/exedev/.cache/baton/wf10/trace-188` (round 1: `bench-r1.txt`, `bench-calls-r1.txt`, `examples-r1.txt`, `inline-r1.txt`; round 2: `bench-r2.txt`, `bench-calls-r2.txt`, `examples-r2.txt`). Each claim is labelled measured, inferred or guess.

Round 2 (after review) changed what the trace records. Each event now has an occurrence ID, each capability call has a `call-exit` with its outcome, and the runtime records the links between occurrences: a handler names the failure it caught, an exit the occurrence it carries. The query states a cause only along such a link and validates the whole trace before it explains it. Round 1 inferred causes from error rows and shared parents, and the review found it stating false origins (two calls of one operation both "raised" a failure; two timeouts' handlers each claimed both deadlines).

Owner, 2026-10-26: "what I want as a North Star is traceability, you know, observability. … and also really strong focus on agent interpretability, like that they can really understand the code and interact with it". The checker already knows every effectful call site, its rows, its owner and its definition. So Effra can trace by construction. Effect needs a hand-written `withSpan` at each site that it wants in a trace.

## Summary

- **The checker names every event.** `NewModel` (`prototypes/sketch9/sketch/defs.go`) derives a definition ID for every declaration and block from the checked file. The emitter writes the IDs it uses as Go constants. The runtime raises task events keyed by the task's span, which is the definition ID. Emitted code raises capability `call` and `call-exit`, `interrupt`, wire `construct` and catch `handler` events at their sites. No ID is written by hand or computed at run time (measured: `TestModelIDsAreDerivedAndStable`; inferred from the code: no `runtime.Caller`).
- **The runtime records causes; the query does not infer them.** Every event has an occurrence ID. A `handler` links the occurrence it caught, a `body-exit` the occurrence its exit carries, a `call-exit` its call (measured: `TestTraceHandlerLinksTheFailedCall`, `TestTraceHandlersLinkTheirOwnDeadlines` and the reviewer's two probes as `TestQueryTraceStatesOnlyRecordedLinks` and `TestKernelProbeTraces`).
- **Off is free.** With no tracer, `runtime/rt` allocates what base `854f21e7` allocates (0, 3 and 0 allocs/op), and the time differences against base are inside the box's noise (measured). The poll has no emission point and stays one atomic load, inlinable (measured: `inline-r1.txt`). The trace switch adds about 62 KB to each kernel-mode example binary (measured).
- **On is bounded.** One recorded event costs about 55 to 85 ns (measured; see [Measured overhead](#measured-overhead)). The recorder keeps a fixed number of events, never blocks, and counts drops.
- **A query reads the run in the model's words.** `sketchc trace` validates a recorded run against the checked file and refuses a malformed one with the reason. It prints the cause trace of the root exit with definition IDs, rows and sites, along recorded links only, and labels any other origin a candidate. The `-json` form is the `trace-evidence-1` envelope, which shares its envelope fields with the review evidence schema of #156.

## Answers to the ticket

- **Q1, event model.** See [Event model](#event-model), [Causality](#causality), [Cost when off](#cost-when-off) and [Sampling](#sampling). The runtime raises seven of #124's thirteen kinds for tasks. Emitted code raises `construct` and `handler` and three additions, `call`, `call-exit` and `interrupt`. Every event has an occurrence ID, a task, its parent, a definition ID and a site, and the runtime records the links between occurrences. Off costs no measurable time and no allocation. The default is off. When on, every event of the run is kept up to the bound.
- **Q2, runtime.** The hook is in the merged kernel runtime `runtime/rt` (#198). It reads the real task tree that #124's laws and #200's G2 read. The tracer pointer is inherited at task start, as G1 context is. A law checker reads the same stream through the same vocabulary (see [Causality](#causality) for the partial order).
- **Q3, export.** See [OpenTelemetry mapping](#opentelemetry-mapping) and [In-memory recorder](#in-memory-recorder). The mapping is written by hand, with no SDK dependency. The bounded recorder in `runtime/rt/rttrace` serves tests and the JSON Lines writer.
- **Q4, query surface.** See [Query surface](#query-surface). `sketchc trace` prototypes `ef trace` and MCP `code.trace`. One schema serves both.
- **Q5, cause traces.** See [Cause traces](#cause-traces). The query follows a handled or escaping failure back along recorded links to its deadline, child exit or failed call, with definition IDs, rows and sites. Where no link was recorded it says so and labels candidates.
- **Q6, privacy and budgets.** See [Privacy and budgets](#privacy-and-budgets). The serialized trace carries no values; the in-memory event does, for the tracer. Event and byte budgets bound the trace; the series budget is a design.

## Pillar scores

| Pillar | Score | Why |
| --- | --- | --- |
| P1 Traceability and observability | Strong | Every task and capability call is traced with no source change, keyed to a checked definition and a site (measured on three examples). |
| P2 Effect's core guarantees | Neutral to positive | The hook does not change exits, masks or releases. A panicking tracer is disabled and never becomes a defect (`TestTracePanickingTracerIsDisabled`). Events expose R4, R5 and R8 orderings to a law checker. |
| P3 Go simplicity | Positive | The source has no `withSpan`. The emitted Go adds a `t__.TraceCall(...)` line before and a `t__.TraceCallExit(...)` line after each capability call, and a constant table. |
| P4 Laws and proofs | Positive | The kinds are #124's vocabulary, so the law suite can read a real run. The trace states a partial order and never presents `seq` as a total order; the query validates each trace against the runtime's ordering rules before it answers. Nothing is proved. |
| P5 Error and dependency tracking | Strong | A cause trace names the failure's definition, the occurrence the runtime recorded as its origin, and the row of that call or block, taken from the checked model. |
| P6 Agent interpretability | Strong | The runtime and the checked model share one ID space per revision; `ef inspect` joins it with #203 (proposed). An agent answers "why did this time out" in the model's terms ([preview](../previews/2026-10-27-traceability.md)). |
| P7 Performance | Positive | Off: no allocation and no measured time against base. On: about 55 to 85 ns an event. The switch adds about 58 to 63 KB per binary, after the writer dropped `encoding/json` (which cost 1.48 MB). |

## Event model

Events are records of one run. Each has an `id` (its occurrence ID), a `kind`, a `task` (a number above 0), a `parent` (0 for the root), a `def` (a definition ID) and a `site` (`<file>.sketch:<line>:<col>`). It may have a `link` (the occurrence ID of the event it refers to), `seq`, `time` and `cause`. The runtime raises the task events itself. Emitted code raises the point events at its own site through `Task.TraceCall`, `Task.TraceCallExit` and `Task.Trace`.

| Run event (ticket list) | Kind | Raised by | #124 vocabulary | Notes |
| --- | --- | --- | --- | --- |
| Task start | `start` | `rt.Run`, for the root and every child | `start` | The task's def is its span: `<package>.main` for the root, the block ID for a timeout child. |
| Task stop | `body-exit` | `rt.Run`, after `settle` | `body-exit` | `cause` lists the reasons by type: failure, defect (with its chain), interrupted. Success has no cause. A failed exit links the occurrence it carries: a failed call, an interrupt, a panic, a child's deadline or a child's failed exit. |
| `Wait` | `observe` | `Handle.Wait` (completed), `Handle.Stop`, the join in `After` | `observe` | Links the child's `body-exit`. An interrupted join records none (law R4). |
| Interrupt request | `cancel-request` | `Task.Interrupt`, once for each task the cascade reaches; `init` for a child started under a stopped owner | `cancel-request` | Recorded before the stop flag is set, so it precedes every observation (`TestTraceCancelRequestPrecedesObservation`). |
| Interrupt observed | `interrupt` | emitted code, at the interruption point before a capability call and at `Scheduler.Sleep` | addition | See below. |
| Timeout | `deadline` | `rt.AfterSpan`, when the timer wins | `deadline` | It belongs to the child the deadline stops; its parent is the owner (law R8). |
| Capability call | `call`, then `call-exit` | emitted code, before and after each capability operation; the runtime, when a defect leaves the call | addition | `def` is the operation (`latency.Gateway.Authorize`, `host.Scheduler.Sleep`); `site` is the call. The `call-exit` links its call and carries the outcome: ok (no cause), the failure, interrupted or the defect. |
| Typed failure | `call-exit` or `body-exit` cause, and `handler` | emitted code; `rt.Run`; emitted catch arm | `body-exit`, `handler` | A failure that crosses a task boundary appears in the exit. A catch arm records the error it handled, and the runtime links the occurrence it caught. |
| Defect | `panic`, and the `body-exit` cause | `Cause.defect` | `panic` | The cause keeps the G2 chain as task IDs and definition IDs, at most 32 frames. A panic that left a capability call links that call's `call-exit`. |
| `defer`/`Close` | `release` | `Scope.runRelease`, on every exit of a release | `release` | Keyed by the owner task: a release has no definition of its own in the kernel API yet. |
| Wire build | `construct` | emitted code, after a named wire's function or an inline wire's fields | `construct` | `def` is the wire (`latency.Live`) or the inline wire's block ID. |
| Machine decision | none | — | none | The sketch has no machines. A `decide` event would be an addition, keyed by the machine's step definition (follow-up). |

**The three additions.** `call` is the effect boundary that the `uses` row names. It answers "what did this run do" and links each run event to the operation's error row in the model. `call-exit` records how that call ended, so a failure has a recorded origin and a call has a recorded span. `interrupt` records which interruption point (I1) observed a stop. For a law checker it refines R5: `cancel-request` precedes `interrupt`, which precedes the task's `body-exit`. A checker that knows only #124's thirteen kinds may ignore all three, because none changes an ordering the laws state (inferred).

**Links are recorded, not inferred.** The runtime keeps, for each traced task, the latest failure, interruption or defect occurrence that no handler has consumed. A failure travels from where it is raised to a handler or to the body exit by return, inside one task, so that occurrence is the one in flight (inferred from the lowering: `try` returns at once, and a catch arm either handles the error or passes it on). Hand-written Go on `runtime/rt` that drops a failure would leave it pending, and a later handler in that task would link it; the contract is stated on `taskLinks`. A `handler` links and consumes it; a `body-exit` links it. A completed `Wait` or `After` join carries a child's failed exit to the owner, and a deadline becomes the owner's occurrence, because the owner receives `Expired`. A failure that source code returns itself (not from a call or a child) has no event, so its handler links nothing and the query says the origin was not recorded.

**Kinds not raised yet.** `admit` would record the owner linking a child, before `start`. The `start` event already carries the owner, so `admit` waits for R9's late-fork rejection checks. `cleanup` would mark a scope's release phase, and `publish` would mark the moment a joiner can see an exit (`close(done)`, after `body-exit`). Both wait for R2 checks that need them. `adjust` has no sketch construct.

**Raised from emitted code, not from `Stopped`.** An emission inside `Task.Stopped` would stop it from inlining and add a branch to every poll. The emitted code writes the poll as `if t__.Stopped() { t__.Trace(rt.TraceInterrupt, …); return … }`, so the event costs nothing until the stop is seen (measured: the poll is unchanged).

## Identifiers

- **Definition ID.** A declaration is `<package>.<Name>` (`latency.checkout`, `latency.OrderNotFound`, `latency.Live`). A method is `<package>.<Type>.<Method>` (`latency.Gateway.Authorize` for the capability operation, `latency.FakeGateway.Authorize` for the provider). A block is `<enclosing ID>/<construct>#<n>`, where `n` counts that construct inside the enclosing definition in source order (`latency.checkout/timeout.After#1`, `latency.report/wire#1`). Host definitions are `host.Console.Println`, `host.Scheduler.Sleep` and `timeout.Expired`. The IDs are readable on purpose: an agent and a person read the same string. Measured: two checks of the same source give the same table and the same emitted program (`TestModelIDsAreDerivedAndStable`). Inferred from the derivation, which uses names and ordinals, not offsets: the IDs survive formatting and edits to other definitions.
- **Source site.** `<file>.sketch:<line>:<col>` of the declaration, or of the start of the call expression. It is the same file name the `//line` directives use, so a Go stack frame, a profile and a trace event name the same line.
- **Call-site ID.** A point event is identified by `(kind, def, site)` under the revision. The model lists every such point (`Model.Points`), with its enclosing definition. `sketchc trace` resolves each recorded point to that entry and refuses an event that matches none.
- **Occurrence ID.** The runtime numbers each event when it raises it, from one atomic counter per traced tree. The number is unique in the trace, and an event that happens before another has the smaller number: the counter's order is a linear extension of the happens-before order of the emission points (inferred from Go's memory model, where atomic operations are sequentially consistent). A smaller number alone does not mean happens-before. A call's `call` and `call-exit` share the call's occurrence ID through the `link`.
- **Semantic revision.** The SHA-256 of the source bytes, hex encoded: the digest `ef` already reports as a revision (`internal/compiler/bundled.go`, the graph's `revision` and `scope`). A formatting edit changes it, which is right, because sites change with it.
- **Revision placement, decided by measurement.** The trace header carries the revision once; events carry plain definition IDs. Qualifying each event would add 65 bytes per event: 585 to 845 bytes on the three example traces, about 46% (inferred from the measured sizes in `examples-r1.txt`). The query checks the header revision before it reads any event, so the qualification is checked once.
- **Join with `ef inspect`.** `ef type --definition` today takes hash IDs (`t:dc0347e1f7f5c4d9`) scoped by the revision. Its graph nodes use offset paths (`expression:function:main:987:…`). Neither survives an edit elsewhere in the file. The recommendation for #203 is that `ef inspect` report the readable definition ID beside its hash ID, per revision. Then a trace event, a defect chain and an inspect answer name the same definition. A type lookup by definition ID already requires `expectedRevision`, which is the same rule the trace header enforces.

## Causality

- **Parent task.** A task's `start` event records its parent, and the writer copies that parent onto every later event of the task, because a finished task drops its live parent link (G2 rule 3).
- **Owner scope.** In the sketch, the owner of a timeout child is the task that ran `timeout.After`, and `deadline` and `observe` name it as the parent. A release names its owner task. Scopes have no IDs of their own yet; a scope ID joins when `defer` and `Close` lower to `rt.Scope` in the sketch.
- **Cause chain.** A `body-exit` cause keeps every reason in order (failure, defect, interrupted), and a defect keeps its chain of at most 32 frames as task and definition IDs (G2). The query follows a failure along recorded links: a handler to what it caught, a child's exit to what it carried, down to the failed `call-exit`, the deadline or the panic.
- **Partial order.** `seq` is the recorder's arrival order. It is a total order of arrivals, not of the run. Two events are ordered only when the runtime orders them:
  - events of one goroutine (for example `deadline` before the child's `cancel-request`, both in `After`);
  - a task's `start` before its other events, except a stop requested before the task runs: a `cancel-request` may precede the `start` of a root (`TracedRoot`, `Interrupt`, then `Run`) and of an admitted child (one born under a stopped owner, or stopped by its owner or a deadline before its goroutine runs), and a child's `deadline` may precede its `start`;
  - a `cancel-request` before every observation of that stop (the event is recorded before the flag is stored);
  - a child's `body-exit` before its owner's `observe` and every later owner event (`body-exit` precedes `close(done)`, and the owner waits on `done`);
  - an event before every event that links it.
  The recorder reserves `seq` with one atomic add, and the runtime takes the occurrence ID with another, so both agree with each of these orders (inferred from Go's memory model). Neither says anything about unordered pairs, such as two sibling tasks. #124's reference checker compares allowed partial orders and never a total order, and the trace states this in the envelope's `order` field. The query refuses a trace that breaks one of these rules (see [Query surface](#query-surface)).

## Cost when off

What an emitted program pays with no recorder, measured on base `854f21e7` and head (see [Measured overhead](#measured-overhead)):

- **Allocations.** None added. Poll, spawn+wait and acquire+release allocate 0, 3 and 0 objects, the base counts (measured: `TestTraceOffAddsNoAllocs` and the benchmarks). The tracer pointer took the word that moving `shielded` beside `stopped` freed, so `Task` stays 136 bytes and a started `Handle[struct{}]` stays 176 bytes, in the same size class (measured: `TestTraceHookKeepsTaskSize`).
- **Time.** The poll has no emission point. `go build -gcflags=-m ./runtime/rt/` still reports `can inline (*Task).Stopped`, and reports `can inline` for `(*Task).Trace`, `(*Task).TraceCall` and `(*Task).TraceCallExit` (measured: `inline-r1.txt`; round 2: `inline-r2.txt`). Spawn adds one pointer copy and three compares (start, exit, observe). A release adds one compare. Each emitted point adds one inlined compare, so a capability call adds two: `BenchmarkTraceCallOff` (call and call-exit) measured 1.76 ns min, about the cost of an empty poll loop (one compare measured 1.28 ns in round 1) (`bench-calls-r2.txt`). The links cost nothing when off: all their state lives in the tracer, which an untraced tree does not have. Against base, the off/base ratios sit inside the noise band of the hand-written baseline that runs the same code in both trees (measured).
- **A trap found and fixed.** The first version built an `Event` literal inside `Run`. That grew Run's frame from 0x60 to 0xc0 bytes even with tracing off, and spawn+wait measured about 90 ns slower than base (measured: `bench-r1-prefix.txt`). Every emission point now calls a small `//go:noinline` helper that builds the event. Round 2 found the traced-side twin of this trap: passing the larger `Event` by value through three helper frames made a new task's body-exit outgrow the goroutine's first stack, and the stack copy cost about 2.5 µs per traced spawn (measured: CPU profile, `runtime.copystack` under `Recorder.Event`). The `Event` now goes by pointer.
- **Binary size.** The definition-ID constants and the site table are a few hundred bytes per example (inferred from the emitted source). The trace switch (the `rttrace` recorder and writer, linked by `main`) adds about 58 to 63 KB to each example: 2,674,231 bytes with the switch against 2,611,499 without it for `checkout` (measured, `examples-r2.txt`; round 1 measured about 62 KB). A first writer used `encoding/json` and added 1.48 MB, so the writer now encodes its four fixed shapes by hand (measured). The round-2 link logic lives in `runtime/rt` and is linked even without the switch: the kernel binary without the switch grew by about 24 KB (2,586,999 to 2,611,499 bytes for `checkout`, measured), a size cost with no time cost.

## Sampling

**Decided default: off.** With no `EFFRA_TRACE`, the root has no tracer, the program raises no event and writes no file. That costs nothing measurable (above).

**When on: keep every event, up to the bound, with no per-event sampling.** A cause trace and a law check need the whole causal chain inside a root: the deadline, the request, the observation and both exits. Dropping events at random would cut chains and leave the query guessing (inferred). The bound (4,096 events by default) protects memory, and a full recorder names its drops.

**Measured basis (round 2).** An event costs about 55 to 85 ns. An event with no link state costs about 56 ns: acquire+release went from 31.47 to 87.37 ns by minimum with one `release` event. A call or call-exit, which also updates the task's link state under the tracer's lock, costs about 80 ns: `BenchmarkTraceCallOn` measured 160.8 / 172.5 ns (min / median) for the pair. Round 1 measured about 63 ns per event; the occurrence ID's atomic add and the link state add about 17 ns to a linked event. An empty spawn+wait raises four events and costs 1,279 ns traced against 918 ns off, by minimum (+39%; round 1: +37%). The three examples run 11 to 16 events each, and their wall times on and off are equal within noise (25 runs each). About a quarter of an event's cost is the clock read (round-1 CPU profile of the traced acquire).

**Capability-call events: on by default whenever tracing is on.** Off, a call costs two inlined compares (1.76 ns min for the pair, measured). On, its two events cost about 161 ns (measured). The examples make 2 to 3 calls each, 4 to 6 of their 11 to 16 events (measured). A capability operation is the effect boundary the rows name, it is the recorded origin of a failure, and it usually does I/O that costs microseconds or more (guess). Without call and call-exit events, the query could not say which operation failed or which one a task was in when its deadline passed.

**Per-root sampling: deferred.** A server would sample whole request roots, one in N, and keep every event of a sampled root. That needs request roots, which arrive with #201's server work (follow-up).

## OpenTelemetry mapping

Written by hand. `runtime/rt` has no SDK dependency, and an exporter is a follow-up. Attribute names are from the OpenTelemetry semantic conventions at `8a6e3935` (after v1.44.0).

| Effra | OpenTelemetry |
| --- | --- |
| A run (one root task) | One trace. `trace_id`: 16 random bytes made by the exporter when the root starts, or the incoming W3C `traceparent` when a server root continues a caller's trace. |
| A task | One span. `span_id`: 8 bytes derived from the task ID and the run (the task ID alone is unique per process). Name: the definition ID. Start and end time: the `start` and `body-exit` events. |
| Parent task | The parent span: `parent_span_id` is the parent task's `span_id`. The root's parent is the incoming remote span, if any. |
| Capability call | A child span of the task's span, from `call` to its `call-exit` (the exit links the call, so the span's end is recorded, not guessed). Name: the operation's definition ID, such as `latency.Gateway.Authorize`. Status from the call-exit's outcome: `Unset` for ok, `Error` with `error.type` for a failure or a defect, `Unset` with `effra.exit = interrupted` for an interruption. A call with no `call-exit` in the trace (dropped) ends with its task and carries `effra.span = open`. |
| Definition | `code.function.name` = the definition ID. |
| Site | `code.file.path` = the `.sketch` file, `code.line.number` and `code.column.number` = the site. |
| Revision | A resource attribute, `effra.revision`, set once per process (an Effra-specific name; OTel has none). |
| `deadline`, `cancel-request`, `interrupt`, `release`, `construct`, `handler`, `observe` | Span events on the task's span, named by kind, with `code.*` attributes for the site. |
| Occurrence links | A span link (`Link`) from the handler's or the exit's span to the span that holds the linked occurrence, with `effra.link.kind` naming the linked event (`call-exit`, `deadline`, `body-exit`). |
| `body-exit` success | Status `Unset`. |
| `body-exit` with a failure | Status `Error`, `error.type` = the failure's definition ID (for example `timeout.Expired`). A failure that a catch handled leaves the status alone, as the conventions say for handled errors (`recording-errors.md:39`). |
| `body-exit` interrupted | Status `Unset`, attribute `effra.exit = interrupted`: a requested stop is not an error. |
| Defect (`panic`) | Status `Error`, an `exception` event with `exception.type` = the panic value's type and no `exception.message` by default (privacy). The G2 chain goes in `code.stacktrace` as definition IDs and sites, not Go frames. |

## In-memory recorder

`rttrace.Recorder` (`runtime/rt/rttrace/rttrace.go`) is an `rt.Tracer`:

- **Bound.** It keeps the first `Cap` events of a run (default 4,096 slots, about 524 KB preallocated; inferred from the slot size of 128 bytes in round 2, up from 112 with the occurrence ID and link). The bound counts slots, not bytes: a slot keeps the `rt.Event` whole, so an exit's error and a defect's panic value stay referenced until the recorder is dropped, and their size is not bounded. A recorder exists only when a run records.
- **Drop policy.** When it is full, it drops every later event and counts it. The tail of a run longer than the bound is missing and says so: the header carries `dropped`, and the query adds a `dropped` omission and allows parents it cannot resolve. A ring that keeps the latest events (Go's `FlightRecorder`) suits long-running servers better. It is deferred because it needs a lock or a seqlock per slot.
- **Never blocks.** One atomic add reserves a slot, the event is copied in, and an atomic store publishes it. No lock, channel or I/O sits on the emitting path (`TestTraceRecorderIsBounded`: 16 goroutines emit 16,000 events into a capacity of 8, and all finish).
- **Reads.** `Records()` returns the published events in arrival order and counts the slots still being written as in flight. Read it after the traced tree has finished. `Reset` is for benchmarks and runs only while no task runs.
- **Writer.** `WriteFile` writes one header line (`schema` `effra-trace-2`, `revision`, `producer`, `capacity`, `events`, `dropped`) and one JSON line per event with its `id` and, when set, its `link`. It fills a task event's site from the program's site table and a task's parent from its start event. Round 2 bumped the schema from `effra-trace-1`, because events gained `id`, `link` and the `call-exit` kind.
- **Tests.** `TestTraceEventsCarryIDsAndParents`, `TestTraceTimeoutRecordsDeadline` (under the `Virtual` clock), `TestTraceCancelRequestPrecedesObservation`, `TestTraceHandlerLinksTheFailedCall`, `TestTraceHandlersLinkTheirOwnDeadlines`, `TestTraceChildFailureLinksThroughBodyExit` and `TestTraceDefectClosesTheOpenCall` read the real recorder.

## Query surface

**Prototype: `sketchc trace [-json] FILE TRACE`** (`prototypes/sketch9/sketch/trace.go`). It loads FILE through the checker, builds the model and computes the revision. It opens TRACE and reads it through a limit of 8 MiB and one byte, so a larger trace is refused once the budget is read and is never loaded whole (measured: a 24 MiB trace is refused after 8,487,040 bytes of allocation, `TestTraceByteBudgetBeforeRead`; the round-1 reader allocated 33,678,368 bytes for a 32 MiB file). `QueryTrace` keeps its own length check. Then the query validates the whole trace before it explains any of it, and refuses it (non-zero exit, with the reason) when:

- the header is missing, its schema is not `effra-trace-2`, its revision differs from FILE's, or its event count differs from the lines in the trace (truncated or edited);
- an event names a definition ID the model does not have, a task event's site is not its definition's site, a task kind names a definition that is not a task, or a point event matches no `(kind, def, site)` point of the model;
- an event has no occurrence ID or repeats one, a cause is on a kind that has none, a reason is unknown, or a failure names no error type of the model;
- a task starts or exits twice, an event names a parent other than its task's start, a task event names a definition other than its task's, or an event comes before its task's start (except a `cancel-request` or `deadline`, which may: see [Causality](#causality));
- an `observe` has no `body-exit`, or comes before it;
- the parent graph has no single root, a task is its own parent, a child starts before its parent, or the parent links form a cycle (the start-order rule already excludes a cycle; the query walks with a visited set anyway);
- a task names a parent that never started, unless events were dropped, in which case the answer names the ancestry as unavailable;
- a link names a missing occurrence (unless events were dropped), a later occurrence, or an event of the wrong kind or task: a `call-exit` must link its own call, a `handler` a failed `call-exit` of its task with its type, a deadline of its child (for `timeout.Expired`) or a child's exit with its type, a failed `body-exit` its own failed call, interrupt or panic or its child's deadline or failed exit, an `observe` the child's exit, a `panic` a `call-exit` with a defect; a call exits twice; two handlers catch one occurrence; a successful exit carries a link;
- a defect chain names a definition the model does not have or that is not a task, names a task under another definition, puts a frame that is not the parent of the one before, stops short of the root with fewer than 32 frames, or does not pass through the task that reports it. A frame whose task never started is refused unless events were dropped, and then named as unavailable ancestry. A chain cut at 32 frames names the ancestors it lacks.

`TestQueryTraceRefuses` covers each rule with the expected reason, including the reviewer's cycle probe (a `body-exit` naming its own task as parent, which once overflowed the stack) and a valid outer event with an unknown nested frame (`unknown.made.up`). `TestQueryTraceAcceptsValidOrders` keeps the valid cancel-before-start orders. Every traversal of the explanation keeps a visited set, and every link points to an earlier occurrence, so no explanation can loop.

It prints the task tree, every capability call with its outcome and its span, every event with its occurrence ID, link and enclosing definition, the cause trace of the root exit and the omissions. It never fills a gap: what it cannot see, it names. Missing values, direct function calls, dropped events, events in flight, missing exits, unresolved links and unavailable ancestry are all named.

**Evidence envelope.** `-json` prints `trace-evidence-1`. Its envelope fields are those of #156's `review-evidence-1` spec: `schema`, `producer`, `revision`, `budgets`, `omissions` and `fingerprint`. The fingerprint is the SHA-256 of the canonical JSON of the answer without it. #156 has no merged schema. Whichever of #156 and the trace work lands second reuses these six fields with these meanings, and adds only its own payload (`tasks`, `calls`, `events`, `causeTrace` here; roots and the evidence tree there).

**Proposed: `ef trace` and MCP `code.trace`.** One query, two transports, one schema, as `ef inspect` and its MCP tool share the checker model today:

- `ef trace FILE TRACE [--json] [--task N] [--why DEF]` prints the same answer as `sketchc trace`. `--why` starts the cause trace at a failure definition.
- MCP `code.trace` takes `{file, trace, expectedRevision?, task?, why?}` and answers with the `trace-evidence-1` envelope. Like `code.type`, it refuses a request whose `expectedRevision` differs from the source. Like Replay.io's MCP server, an agent starts from the overview (the task tree and the cause trace) and asks for detail by task or definition.
- Both read only recorded files. A live view of a running program is `ef ps` (#201, G3).

## Cause traces

The query explains the root exit: each reason the root exits with, then each failure a catch handled on the way. It states a cause only along a link the runtime recorded:

- **A handler** names what it caught. For a failed call: "caught the failure `calls.Missing` of call `calls.Store.Get` at `two-calls.sketch:29:16` in `calls.load`, whose row is `error[calls.Missing]` (call seq 4, call-exit seq 5)". In the reviewer's probe the first call of the same operation returned ok, and the answer says nothing causal about it (measured: `TestQueryTraceStatesOnlyRecordedLinks`).
- **`timeout.Expired`** links the `deadline` of the handler's child. The answer names the block's definition, spelling, site and row, then what the trace records of that child: the calls that were open when the deadline passed (by occurrence order: the call's ID is below the deadline's and its exit's above), the `cancel-request`, the `interrupt` where it observed the stop and its `body-exit`. With two timeouts in a row, each handler names its own deadline (measured: the reviewer's probe, recorded and live).
- **A child's failure** links the child's `body-exit`, which links what it carried, down to the failed `call-exit`.
- **A defect** prints its chain as task and definition IDs with each definition's site (the G2 chain text of `docs/spec/runtime.md` with definition IDs in place of spans), then the `panic` and the capability call it left, if one was open.
- **No link.** A failure that source code returns itself has no event. The answer then says "origin not recorded" and lists each event of a compatible type as "candidate by type only" (measured: `TestQueryTraceLabelsCandidates`). It never says "raised by" without a link.
- **Call spans.** A call's duration is its `call-exit` time minus its `call` time, both read from the recorder's clock as each event arrives. A call with no `call-exit` in the trace is shown as open.

Effect's `Cause.pretty` prints every reason of a cause with stack frames, and its spans come from `withSpan`, named `Effect.fn` or captured `Error` stacks. Here the frames are definition IDs and checked sites, and the links between occurrences are recorded by the runtime. Real output for the timeout example is in the [preview](../previews/2026-10-27-traceability.md).

## Privacy and budgets

- **No values in the serialized trace by default.** A written event carries IDs, sites, kinds, task numbers, a monotonic time and, for an exit or a call outcome, the reasons by type name. The writer renders a failure as its type name (`OrderNotFound`), never its fields. A defect is its panic value's type. `TestWriteJSONLNamesTypesNotValues` checks that field values (`secret-7`, `secret-8`, `secret-9`) never appear. The query's envelope states the omission (`values`). In memory it is different: `rt.Event.Cause` and `rt.Event.Defect` hold the actual error and panic value, a `Tracer` receives them, and the `Recorder` keeps them until it is dropped. A custom tracer can read values; the privacy default is a property of the writer.
- **`Redacted` (#67), design.** When values join a trace (an opt-in follow-up), a value of a `Redacted` type would render as its type name only, on every path: event attributes, defect values and OTel `exception.message`. Nothing of this is built.
- **Series budget (#69), design.** The definition IDs and kinds are a closed, compile-time set: a program's possible event series are its task definitions and its points: 7 to 12 points for each example (inferred by counting the capability calls, wire builds and catch arms of each file). They would fit #69's series budget by construction. Task numbers, occurrence IDs and times are values in a series, never labels. Free-form labels, such as the per-request attributes of #69, would count against that budget. Nothing of this is built.
- **Event budget.** The recorder's capacity: 4,096 slots by default, named in the header and the envelope's `budgets.events`. It bounds the number of events, not the bytes that the values in kept events retain.
- **Byte budget.** The query reads at most 8 MiB and refuses a larger trace before it reads past the budget. An event line is about 133 to 140 bytes (measured: 1,716 to 2,400 bytes for 11 to 16 events plus a header of about 177 bytes), so a full default recorder writes about 0.57 MB (inferred).

## Decision table

| Decision | Verdict | Reason | Pillar |
| --- | --- | --- | --- |
| Raise task events in `runtime/rt`, with the tracer pointer inherited at task start | ADOPT | The trace must come from the real task tree that #124 and #200 read. Off costs no allocation and no measured time. | P1, P4, P7 |
| Event kinds from #124's vocabulary, plus `call`, `call-exit` and `interrupt` | ADOPT | The law suite reads the same stream; the three additions refine orderings the laws already state. | P4, P1 |
| Occurrence IDs and runtime-recorded links (handler, body-exit, call-exit, observe, panic) | ADOPT | A cause stated by the query must be a recorded fact. Round 1 inferred origins from rows and parents and stated false ones. The links cost nothing off; their cost on is in [Measured overhead](#measured-overhead). | P1, P5, P6 |
| Infer an origin from an error row or a shared parent | REJECT | A compatible row says a call could fail, not that this occurrence did; a shared parent does not say which failure a handler caught. Without a link the answer labels candidates. | P5, P6 |
| Validate the whole trace before explaining it | ADOPT | A malformed trace (a cycle, an unknown nested definition, a link of the wrong kind) is a refusal with a reason, never a crash or a confident wrong answer. | P4, P6 |
| Read the trace through a budget+1 limit | ADOPT | The byte budget holds while reading; a larger file is never loaded. | P7 |
| Definition IDs derived by the checker, emitted as constants | ADOPT | The claim under test: IDs from checked declarations, stable per revision, never typed by hand or computed at run time. | P6, P1 |
| Revision once in the header, plain IDs in events | ADOPT | Per-event qualification adds about 46% to each example trace; the query checks the header first. | P7, P6 |
| Default sampling off; when on, keep every event up to the bound | ADOPT | Complete chains for cause traces and laws; about 55 to 85 ns an event. | P1, P7 |
| Capability-call events on whenever tracing is on | ADOPT | They are the effect boundary the rows name and the recorded origin of a failure; 1.76 ns off for call and call-exit, about 161 ns on for the two events (measured). | P1, P5 |
| A panicking tracer is disabled, never a defect | ADOPT | `:telemetry` detaches a failing handler the same way. Instrumentation must not change exits. | P2 |
| A hand-encoded JSON Lines writer instead of `encoding/json` | ADOPT | `encoding/json` cost 1.48 MB per binary; the hand encoder costs about 62 KB. | P7 |
| `trace-evidence-1` with #156's envelope fields | ADOPT | One envelope for review and trace evidence; whichever lands second reuses it. | P6 |
| OpenTelemetry mapping written by hand, exporter later | ADOPT | No SDK in the runtime or in root `go.mod`; the mapping fixes names now. | P1, P3 |
| Emission inside `Task.Stopped` | REJECT | It would stop the poll from inlining. Emitted code raises `interrupt` only after a stop is seen. | P7 |
| Sites computed at run time (`runtime.Caller`) | REJECT | Costly, and not by construction: the checker already knows each site. | P6, P7 |
| Probabilistic per-event sampling | REJECT | It cuts causal chains; the query would have to guess. | P1, P4 |
| Per-root head sampling for servers | DEFER | Needs request roots (#201). | P1, P7 |
| Ring (latest-window) recorder | DEFER | Better for long runs; needs per-slot locking. Keep-first with named drops is enough for the spike. | P1 |
| Argument values in events, opt in, through `Redacted` | DEFER | Needs #67 and #69's budgets. | P1, P5 |
| `admit`, `cleanup`, `publish` events | DEFER | Wait for the R2 and R9 law checks that need them. | P4 |
| Full record and replay as the default | REJECT | rr records a run's nondeterministic inputs and reconstructs every instruction by replay; Effra records only checked effect boundaries and stays cheap enough to leave on in tests. | P7 |

## Measured overhead

Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64 (the module's toolchain; the unit directory's shell reports go1.26.5 in `bench-r2.txt`'s header, but every benchmark ran inside the module), shared with other agents (load average 4 to 13 during the round-2 microbenchmarks, 9 to 16 during the examples). Read the minimum.

- Microbenchmarks: `go test -run '^$' -bench 'Poll|SpawnWait|AcquireRelease' -benchmem -count 2 ./runtime/rt/`, five rounds on head and five on a `git archive` copy of base `854f21e7` in a scratch directory, interleaved, so 10 samples per row and tree (`bench-r2.txt`; round 1: `bench-r1.txt`). A focused spawn+wait run, six interleaved rounds of `-count 3` per tree, checks the off path's noisiest row (`bench-spawn-r2.txt`). Calls: `go test -run '^$' -bench 'TraceCall' -benchmem -count 10 ./runtime/rt/` (`bench-calls-r2.txt`).
- Examples: `measure-examples.sh` (in the unit directory, with `bench.sh` and `strip_recorder.py`) builds each example with `sketchc build -rt kernel` and runs it 25 times off and 25 times on, interleaved, timing each process with `date +%s%N` (`examples-r2.txt`). Wall time is the median of 25 runs and includes process start.

Round 2 (head `8c988195`):

| Row | base `854f21e7` (min / median) | off: head, no tracer (min / median) | on: head, recorder (min / median) | allocs/op off (base) | allocs/op on | events, trace bytes on |
| --- | --- | --- | --- | --- | --- | --- |
| Poll on a running task, ns/op | 1.550 / 1.870 | 1.451 / 1.889 | 1.235 / 1.447 | 0 (0), 0 B | 0, 0 B | 0 events |
| Spawn + wait of an empty task, ns/op | 885.7 / 1043.0 | 918.2 / 1126.5 | 1279.0 / 1541.0 | 3 (3), 320 B | 3, 320 B | 4 events (start, body-exit, observe, release) |
| Acquire + release of a no-op resource, ns/op | 32.55 / 40.80 | 31.47 / 36.65 | 87.37 / 103.70 | 0 (0), 0 B | 0, 0 B | 1 event (release) |
| Capability call (call and call-exit), ns/op | — | 1.758 / 1.981 | 160.8 / 172.5 | —, 0 B | 0, 0 B | 2 events |
| `checkout` example, wall time median of 25 runs, ms | — | 3.00 | 2.73 | — | — | 12 events, 1,768 bytes |
| `timeout` example, wall time median of 25 runs, ms | — | 106.61 | 108.23 | — | — | 16 events, 2,400 bytes |
| `failure` example, wall time median of 25 runs, ms | — | 3.27 | 2.91 | — | — | 11 events, 1,716 bytes |

Round 1 (head `668f9184`), for the deltas:

| Row | base (min / median) | off (min / median) | on (min / median) | events, trace bytes on |
| --- | --- | --- | --- | --- |
| Poll, ns/op | 1.522 / 1.766 | 1.516 / 1.819 | 1.456 / 1.631 | 0 |
| Spawn + wait, ns/op | 769.3 / 929.2 | 793.1 / 899.8 | 1090.0 / 1295.0 | 4 |
| Acquire + release, ns/op | 29.59 / 36.49 | 30.05 / 35.30 | 93.89 / 110.70 | 1 |
| Call event(s), ns/op | — | 1.28 / 1.46 (call only) | 62.88 / 66.26 (call only) | 1 |
| `checkout`, `timeout`, `failure` wall median off / on, ms | — | 3.29, 105.41, 3.13 | 2.75, 104.73, 2.95 | 9, 13, 9 events; 1,269, 1,818, 1,297 bytes |

Comparison (measured unless labelled):

- **Allocations.** Off equals base on every row: 0, 3 and 0 allocs/op, and 0, 320 and 0 B/op. On adds none either; the recorder's slots are preallocated, and the link state lives in the tracer's map, whose entries are values (one round-2 attempt with a closure-taking helper allocated once per event; it was replaced).
- **Off against base, round 2.** Ratios, min / median: poll 0.94 / 1.01, spawn+wait 1.04 / 1.08, acquire+release 0.97 / 0.90. The hand-written baselines that run the same code in both trees moved by 0.99 / 0.86 (spawn+wait), 0.90 / 0.79 (acquire+release) and 1.09 / 1.04 (poll's `ctx.Done` select) in the same run, so the box's noise is wider than every off/base difference. The focused spawn+wait run (18 samples per tree, load 7.6 to 9) measured off 873.7 / 936.9 ns against base 864.9 / 956.1 ns, a ratio of 1.01 / 0.98, while its hand-written baseline moved by 1.01 / 1.05 (`bench-spawn-r2.txt`). Inferred: tracing off still costs no measurable time. The off-path code changed in round 2 only by a carry argument on the owner-side helper and two `traceForget` compares on interrupted joins, which these benchmarks do not reach; Run's frame stayed 0x70 and `Stop`'s grew from 0x28 to 0x30 bytes.
- **On, round 2 against round 1.** A linked event (call, call-exit) costs about 80 ns against about 63 ns for round 1's call event: the occurrence ID's atomic add and the per-task link state under the tracer's lock. An event with no link state (release) stayed about 56 ns (round 1: about 64 ns, the same within noise). Spawn+wait traced went from +37% to +39% over off by minimum. The poll is unaffected.
- **Examples.** Wall time includes process start (about 2 to 3 ms) and, for `timeout`, the 100 ms deadline. On and off differ by less than the run-to-run spread (minimums: `checkout` 2.15 off and 2.09 on, `timeout` 104.46 and 103.48, `failure` 2.43 and 2.33 ms). Inferred: recording 11 to 16 events costs nothing measurable in a whole program. The traces grew from 9 to 13 events to 11 to 16 (the call-exits) and from about 125 to about 137 bytes per event line (the `id` and `link`).
- **Binary size** (kernel mode, measured): `checkout` 2,674,231 bytes with the trace switch, 2,611,499 bytes without it (a copy of the emitted `main.go` with the recorder removed), 2,548,840 bytes in embedded mode. The switch costs 58,420 to 63,204 bytes across the three examples. The kernel runtime now costs about 63 KB over the embedded prototype runtime, about 24 KB more than in round 1, for the link logic.
- **Poll.** `go build -gcflags=-m ./runtime/rt/` reports `can inline (*Task).Stopped`, and its body is unchanged: `t.stopped.Load() && t.regions.top == interruptible`.
- **Earlier runs.** `bench-r1-prefix.txt` is the run that found Run's frame growth. `bench-r1-c1.txt` is the run at the first runtime commit, before the `cancel-request` ordering fix. Round 2's stack-copy regression on the traced spawn (3,699 ns before the fix, 1,246 ns after, single runs) was measured during development and is not in a retained file.

## Prior art

- **Effect `withSpan`, `Tracer` and `Cause`** (effect-smol `3a1128c7`, `packages/effect/src/Effect.ts:8338` `withSpan`, `Tracer.ts:28` the `Tracer` interface, `Tracer.ts:254-260` `captureStackTrace`, `Cause.ts:1159` `pretty`). A span exists where the programmer writes `withSpan` or defines a named function with `Effect.fn` (`Effect.ts:13566-13684`: a named `Effect.fn` wraps each call of the function in a span; `fnUntraced` builds the same function without one), and its source location comes from a captured `Error` stack, which is configurable. Effra takes the guarantees: one tracer per runtime, spans nested by fiber, and a `Cause` that keeps every reason. It replaces the hand-written span with a checked definition and a compile-time site.
- **Encore** (`c2881447`, `runtimes/go/appruntime/exported/trace2/events.go:17-52`). Static analysis of a Go application finds every API and infrastructure call, and the runtime traces them with a fixed event vocabulary. This is the closest precedent for tracing by construction. Encore records fixed start and end event pairs for each operation, so a completion names the operation it ends; Effra's `call` and `call-exit` pair follows that model (round 2). Effra keys events to definitions and to the runtime-law vocabulary instead of infrastructure kinds.
- **Go `runtime/trace` and pprof labels** (go1.27.0, `runtime/trace/annotation.go:38,95,122`, `flightrecorder.go:48`, `runtime/pprof/label.go:56`). Tasks with parents, carried by `context`; regions; a flight recorder that keeps the latest window; goroutine labels for profiles. Effra already has the task tree, so its trace reads the tree. The flight recorder is the model for the deferred ring recorder. The `//line` directives make Go's own traces and profiles name `.sketch` lines too.
- **Elixir `:telemetry` and BEAM tracing** (`:telemetry` `ce4213ee`, `src/telemetry.erl:71-83,105,202,367`; OTP `516126e9` `lib/runtime_tools/src/dbg.erl`; recon 2.5.6 `c2a76855`). `:telemetry` runs handlers in the emitting process and detaches a handler that raises: Effra disables a panicking tracer the same way. BEAM's `dbg` traces any function by module, function and arity, and recon bounds such traces for production. Elixir's lowering path: `Logger` macros capture `__CALLER__` (`lib/logger/lib/logger.ex:1108`, Elixir `91ee75bb`), so file, line, module and function are compile-time metadata, and `:compile_time_purge_matching` (`:290`) removes calls at compile time. Effra's call sites are compile-time constants in the same way.
- **OpenTelemetry** (semantic conventions `8a6e3935`, `model/code/registry.yaml`, `docs/general/recording-errors.md`). The export target. See [OpenTelemetry mapping](#opentelemetry-mapping).
- **Temporal event history** (API `d21dd97a`, `temporal/api/history/v1/message.proto:1198-1210,530-536`). Each event has an ID, and a timeout event names the events that scheduled and started the work. Effra adopts the same rule in round 2: every event has an occurrence ID, and a handler, an exit, a call-exit, an observation and a panic record the ID of the occurrence they refer to. It keeps a partial order, because a run is concurrent, where Temporal's history is one serialized log.
- **Replay.io, rr and Pernosco** (rr `c145f2f8`; [Pernosco overview](https://pernos.co/about/overview/) and [Replay docs](https://docs.replay.io/), read 2026-10-27). Omniscient debugging. rr records only the nondeterministic inputs of a run (system call results, signals, scheduling decisions) and reconstructs every instruction by deterministic replay; Pernosco precomputes states and dataflow from such a recording; Replay records a browser run the same way and serves it to agents through MCP, starting from an overview. That is the direction for `ef trace` and `code.trace`. Effra replays nothing: it records only checked effect boundaries, at a measured cost per event (see [Measured overhead](#measured-overhead)), so it can stay on in tests. That is a statement about cost per event, not a benchmark against rr.
- **Fai's shared query engine** (`3e03dd1`, README:188 Salsa queries, :253 `fai query`). One query engine serves the CLI, the LSP and agents with versioned JSON. `ef trace` and `code.trace` share one schema and the checker model in the same way.
- **Hapsland's review projection** (`a0773f7`, `CONTEXT.md:49-67`: review-projection fingerprints and source fingerprints as equality evidence). The `trace-evidence-1` fingerprint plays the same role: a consumer can reuse or discard an answer by its fingerprint.
- **Compile-to-host languages: Gleam, ReScript and MoonBit have no built-in tracing.** None has a runtime tracer or span construct; each leans on its host's tools. Their lowering paths:
  - **Gleam** (`52e735c8`, `compiler-core/src/erlang.rs:1146-1180`, `:1205-1245`). On the BEAM target, a Gleam function is an Erlang function, so `dbg`, recon and `:telemetry` (through FFI) see it by module and name. The compiler bakes `file`, `module`, `function` and `line` into every `panic`, `todo` and `let assert` error, and `echo` lowers to a call that carries the file path and line. That is construction-time source identity for defects, and Effra's defect chain does the same with definition IDs.
  - **ReScript** (`30ce3698`, `compiler/core/js_source_map.ml`). It emits readable JavaScript and Source Map v3, with source locations attached to JS IR nodes, so the host's devtools and profilers map back to `.res` lines. Effra's `//line` directives are the Go equivalent. ReScript adds no runtime events.
  - **MoonBit** (docs `8d9f3ba2`, `next/toolchain/moon/commands.md:100` `-g/--debug`, `coverage.md`). It emits debug information for its wasm, JS and native backends and instruments branches for coverage at compile time. It adds no runtime tracer either.
  - **Elixir and Gleam** (above) both run on the BEAM, whose `dbg` traces any function by module, function and arity, so both get host function tracing for free; Gleam reaches `:telemetry` through FFI. Elixir's compile-time logging metadata and compile-time purging are the closest model for Effra's constant IDs and zero-cost off path. ReScript and MoonBit hosts give source maps, debug information and coverage, not function tracing.

## Follow-up units

| Unit | Ticket | Scope | Estimate |
| --- | --- | --- | --- |
| G2 chain with sites | #200 | Print a defect chain with definition IDs and sites from the model (runtime.md G2 rule 1, the start site); normative text for the event kinds and the partial order in `docs/spec/runtime.md`. | 1 day |
| Trace laws | #124 | Feed recorded runs to the RS1 reference checker; add `admit`, `cleanup` and `publish` where R2 and R9 need them; check `interrupt`, `call` and `call-exit` as refinements, and the occurrence links as recorded happens-before edges. | 3–4 days |
| `ef trace` and MCP `code.trace` | new (after #203) | The query of this note on the main compiler's model, readable definition IDs beside hash IDs in `ef inspect`, one schema in CLI and MCP. | 3 days |
| OTLP export | new | An `rttrace` sink that writes OTLP JSON by the mapping above; W3C `traceparent` for server roots. | 2 days |
| Std `trace`, `log` and `metric` | #69 | User spans and attributes on the same event stream, the series budget, opt-in values through `Redacted` (#67). | 1 week |
| Live view and per-root sampling | #201 | `ef ps` from the live tree; sample request roots one in N; a ring recorder for long runs. | 3 days |
| Machine decision events | new (with the machine runtime) | A `decide` event per machine step, keyed by the step definition, with the decision kind (`Enter`, `Stay`, `Ignore`, `Reject`, `Done`); justify it as an addition to #124. | 1 day |
| Raise events | new | A `raise` point event where source code returns a failure itself, so every handled failure has a recorded origin; today such a handler links nothing and the query lists candidates. | 0.5 day |
| Review evidence | #156 | Adopt the shared envelope fields of `trace-evidence-1`, or move them to a shared package if #156 lands first. | 0.5 day |
