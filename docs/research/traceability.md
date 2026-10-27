# Traceability by construction

Spike [#188](https://github.com/cevr/effra/issues/188), parent [#192](https://github.com/cevr/effra/issues/192). Branch `wf10/trace-188`, base `854f21e7`. Status: the runtime hook is designed to merge; the emission, the query and `sketchc trace` are a prototype in `prototypes/sketch9`; `ef trace` and MCP `code.trace` are proposed. Raw measurements are in the unit directory `/home/exedev/.cache/baton/wf10/trace-188` (`bench-r1.txt`, `bench-calls-r1.txt`, `examples-r1.txt`, `inline-r1.txt`). Each claim is labelled measured, inferred or guess.

Owner, 2026-10-26: "what I want as a North Star is traceability, you know, observability. … and also really strong focus on agent interpretability, like that they can really understand the code and interact with it". The checker already knows every effectful call site, its rows, its owner and its definition. So Effra can trace by construction. Effect needs a hand-written `withSpan` at each site that it wants in a trace.

## Summary

- **The checker names every event.** `NewModel` (`prototypes/sketch9/sketch/defs.go`) derives a definition ID for every declaration and block from the checked file. The emitter writes the IDs it uses as Go constants. The runtime raises task events keyed by the task's span, which is the definition ID. Emitted code raises capability `call`, `interrupt`, wire `construct` and catch `handler` events at their sites. No ID is written by hand or computed at run time (measured: `TestModelIDsAreDerivedAndStable`; inferred from the code: no `runtime.Caller`).
- **Off is free.** With no tracer, `runtime/rt` allocates what base `854f21e7` allocates (0, 3 and 0 allocs/op), and the time differences against base are inside the box's noise (measured). The poll has no emission point and stays one atomic load, inlinable (measured: `inline-r1.txt`). The trace switch adds about 62 KB to each kernel-mode example binary (measured).
- **On is bounded.** One recorded event costs about 60 ns (measured). The recorder keeps a fixed number of events, never blocks, and counts drops.
- **A query reads the run in the model's words.** `sketchc trace` checks a recorded run against the checked file. It refuses a trace from another revision or program and prints the cause trace of the root exit with definition IDs, rows and sites. The `-json` form is the `trace-evidence-1` envelope, which shares its envelope fields with the review evidence schema of #156.

## Answers to the ticket

- **Q1, event model.** See [Event model](#event-model), [Causality](#causality), [Cost when off](#cost-when-off) and [Sampling](#sampling). The runtime raises seven of #124's thirteen kinds for tasks. Emitted code raises `construct` and `handler` and two additions, `call` and `interrupt`. Every event has a task, its parent, a definition ID and a site. Off costs no measurable time and no allocation. The default is off. When on, every event of the run is kept up to the bound.
- **Q2, runtime.** The hook is in the merged kernel runtime `runtime/rt` (#198). It reads the real task tree that #124's laws and #200's G2 read. The tracer pointer is inherited at task start, as G1 context is. A law checker reads the same stream through the same vocabulary (see [Causality](#causality) for the partial order).
- **Q3, export.** See [OpenTelemetry mapping](#opentelemetry-mapping) and [In-memory recorder](#in-memory-recorder). The mapping is written by hand, with no SDK dependency. The bounded recorder in `runtime/rt/rttrace` serves tests and the JSON Lines writer.
- **Q4, query surface.** See [Query surface](#query-surface). `sketchc trace` prototypes `ef trace` and MCP `code.trace`. One schema serves both.
- **Q5, cause traces.** See [Cause traces](#cause-traces). The query follows a handled or escaping failure back to its deadline, child exit or call, with definition IDs, rows and sites.
- **Q6, privacy and budgets.** See [Privacy and budgets](#privacy-and-budgets). Events carry no values. Event, byte and series budgets bound the trace.

## Pillar scores

| Pillar | Score | Why |
| --- | --- | --- |
| P1 Traceability and observability | Strong | Every task and capability call is traced with no source change, keyed to a checked definition and a site (measured on three examples). |
| P2 Effect's core guarantees | Neutral to positive | The hook does not change exits, masks or releases. A panicking tracer is disabled and never becomes a defect (`TestTracePanickingTracerIsDisabled`). Events expose R4, R5 and R8 orderings to a law checker. |
| P3 Go simplicity | Positive | The source has no `withSpan`. The emitted Go adds one `t__.Trace(...)` line before each capability call and a constant table. |
| P4 Laws and proofs | Positive | The kinds are #124's vocabulary, so the law suite can read a real run. The trace states a partial order and never presents `seq` as a total order. Nothing is proved. |
| P5 Error and dependency tracking | Strong | A cause trace names the failure's definition and the row of the call or block that could raise it, taken from the checked model. |
| P6 Agent interpretability | Strong | The runtime and the checked model share one ID space per revision; `ef inspect` joins it with #203 (proposed). An agent answers "why did this time out" in the model's terms ([preview](../previews/2026-10-27-traceability.md)). |
| P7 Performance | Positive | Off: no allocation and no measured time against base. On: about 60 ns an event. The switch adds about 62 KB per binary, after the writer dropped `encoding/json` (which cost 1.48 MB). |

## Event model

Events are records of one run. Each has a `kind`, a `task` (a number above 0), a `parent` (0 for the root), a `def` (a definition ID) and a `site` (`<file>.sketch:<line>:<col>`). It may have `seq`, `time` and `cause`. The runtime raises the task events itself. Emitted code raises the point events at its own site through `Task.Trace`.

| Run event (ticket list) | Kind | Raised by | #124 vocabulary | Notes |
| --- | --- | --- | --- | --- |
| Task start | `start` | `rt.Run`, for the root and every child | `start` | The task's def is its span: `<package>.main` for the root, the block ID for a timeout child. |
| Task stop | `body-exit` | `rt.Run`, after `settle` | `body-exit` | `cause` lists the reasons by type: failure, defect (with its chain), interrupted. Success has no cause. |
| `Wait` | `observe` | `Handle.Wait` (completed), `Handle.Stop`, the join in `After` | `observe` | An interrupted join records none (law R4). |
| Interrupt request | `cancel-request` | `Task.Interrupt`, once for each task the cascade reaches; `init` for a child started under a stopped owner | `cancel-request` | Recorded before the stop flag is set, so it precedes every observation (`TestTraceCancelRequestPrecedesObservation`). |
| Interrupt observed | `interrupt` | emitted code, at the interruption point before a capability call and at `Scheduler.Sleep` | addition | See below. |
| Timeout | `deadline` | `rt.AfterSpan`, when the timer wins | `deadline` | It belongs to the child the deadline stops; its parent is the owner (law R8). |
| Capability call | `call` | emitted code, before each capability operation | addition | `def` is the operation (`latency.Gateway.Authorize`, `host.Scheduler.Sleep`); `site` is the call. |
| Typed failure | `body-exit` cause, and `handler` | `rt.Run`; emitted catch arm | `body-exit`, `handler` | A failure that crosses a task boundary appears in the exit. A catch arm records the error it handled. |
| Defect | `panic`, and the `body-exit` cause | `Cause.defect` | `panic` | The cause keeps the G2 chain as task IDs and definition IDs, at most 32 frames. |
| `defer`/`Close` | `release` | `Scope.runRelease`, on every exit of a release | `release` | Keyed by the owner task: a release has no definition of its own in the kernel API yet. |
| Wire build | `construct` | emitted code, after a named wire's function or an inline wire's fields | `construct` | `def` is the wire (`latency.Live`) or the inline wire's block ID. |
| Machine decision | none | — | none | The sketch has no machines. A `decide` event would be an addition, keyed by the machine's step definition (follow-up). |

**The two additions.** `call` is the effect boundary that the `uses` row names. It answers "what did this run do" and links each run event to the operation's error row in the model. `interrupt` records which interruption point (I1) observed a stop. For a law checker it refines R5: `cancel-request` precedes `interrupt`, which precedes the task's `body-exit`. A checker that knows only #124's thirteen kinds may ignore both, because neither changes an ordering the laws state (inferred).

**Kinds not raised yet.** `admit` would record the owner linking a child, before `start`. The `start` event already carries the owner, so `admit` waits for R9's late-fork rejection checks. `cleanup` would mark a scope's release phase, and `publish` would mark the moment a joiner can see an exit (`close(done)`, after `body-exit`). Both wait for R2 checks that need them. `adjust` has no sketch construct.

**Raised from emitted code, not from `Stopped`.** An emission inside `Task.Stopped` would stop it from inlining and add a branch to every poll. The emitted code writes the poll as `if t__.Stopped() { t__.Trace(rt.TraceInterrupt, …); return … }`, so the event costs nothing until the stop is seen (measured: the poll is unchanged).

## Identifiers

- **Definition ID.** A declaration is `<package>.<Name>` (`latency.checkout`, `latency.OrderNotFound`, `latency.Live`). A method is `<package>.<Type>.<Method>` (`latency.Gateway.Authorize` for the capability operation, `latency.FakeGateway.Authorize` for the provider). A block is `<enclosing ID>/<construct>#<n>`, where `n` counts that construct inside the enclosing definition in source order (`latency.checkout/timeout.After#1`, `latency.report/wire#1`). Host definitions are `host.Console.Println`, `host.Scheduler.Sleep` and `timeout.Expired`. The IDs are readable on purpose: an agent and a person read the same string. Measured: two checks of the same source give the same table and the same emitted program (`TestModelIDsAreDerivedAndStable`). Inferred from the derivation, which uses names and ordinals, not offsets: the IDs survive formatting and edits to other definitions.
- **Source site.** `<file>.sketch:<line>:<col>` of the declaration, or of the start of the call expression. It is the same file name the `//line` directives use, so a Go stack frame, a profile and a trace event name the same line.
- **Call-site ID.** A point event is identified by `(kind, def, site)` under the revision. The model lists every such point (`Model.Points`), with its enclosing definition. `sketchc trace` resolves each recorded point to that entry and refuses an event that matches none.
- **Semantic revision.** The SHA-256 of the source bytes, hex encoded: the digest `ef` already reports as a revision (`internal/compiler/bundled.go`, the graph's `revision` and `scope`). A formatting edit changes it, which is right, because sites change with it.
- **Revision placement, decided by measurement.** The trace header carries the revision once; events carry plain definition IDs. Qualifying each event would add 65 bytes per event: 585 to 845 bytes on the three example traces, about 46% (inferred from the measured sizes in `examples-r1.txt`). The query checks the header revision before it reads any event, so the qualification is checked once.
- **Join with `ef inspect`.** `ef type --definition` today takes hash IDs (`t:dc0347e1f7f5c4d9`) scoped by the revision. Its graph nodes use offset paths (`expression:function:main:987:…`). Neither survives an edit elsewhere in the file. The recommendation for #203 is that `ef inspect` report the readable definition ID beside its hash ID, per revision. Then a trace event, a defect chain and an inspect answer name the same definition. A type lookup by definition ID already requires `expectedRevision`, which is the same rule the trace header enforces.

## Causality

- **Parent task.** A task's `start` event records its parent, and the writer copies that parent onto every later event of the task, because a finished task drops its live parent link (G2 rule 3).
- **Owner scope.** In the sketch, the owner of a timeout child is the task that ran `timeout.After`, and `deadline` and `observe` name it as the parent. A release names its owner task. Scopes have no IDs of their own yet; a scope ID joins when `defer` and `Close` lower to `rt.Scope` in the sketch.
- **Cause chain.** A `body-exit` cause keeps every reason in order (failure, defect, interrupted), and a defect keeps its chain of at most 32 frames as task and definition IDs (G2). The query follows a failure through child exits to the call whose row names it.
- **Partial order.** `seq` is the recorder's arrival order. It is a total order of arrivals, not of the run. Two events are ordered only when the runtime orders them:
  - events of one goroutine (for example `deadline` before the child's `cancel-request`, both in `After`);
  - a task's `start` before its other events, except a `cancel-request` for a child born stopped, which precedes its start;
  - a `cancel-request` before every observation of that stop (the event is recorded before the flag is stored);
  - a child's `body-exit` before its owner's `observe` and every later owner event (`body-exit` precedes `close(done)`, and the owner waits on `done`).
  The recorder reserves `seq` with one atomic add, so `seq` agrees with each of these orders (inferred from Go's memory model). It says nothing about unordered pairs, such as two sibling tasks. #124's reference checker compares allowed partial orders and never a total order, and the trace states this in the envelope's `order` field.

## Cost when off

What an emitted program pays with no recorder, measured on base `854f21e7` and head (see [Measured overhead](#measured-overhead)):

- **Allocations.** None added. Poll, spawn+wait and acquire+release allocate 0, 3 and 0 objects, the base counts (measured: `TestTraceOffAddsNoAllocs` and the benchmarks). The tracer pointer took the word that moving `shielded` beside `stopped` freed, so `Task` stays 136 bytes and a started `Handle[struct{}]` stays 176 bytes, in the same size class (measured: `TestTraceHookKeepsTaskSize`).
- **Time.** The poll has no emission point. `go build -gcflags=-m ./runtime/rt/` still reports `can inline (*Task).Stopped` and `can inline (*Task).Trace` (measured: `inline-r1.txt`). Spawn adds one pointer copy and three compares (start, exit, observe). A release adds one compare. Each emitted point adds one inlined compare: `BenchmarkTraceCallOff` measured 1.28 ns min, the same as an empty poll loop. Against base, the off/base ratios sit inside the noise band of the hand-written baseline that runs the same code in both trees (measured).
- **A trap found and fixed.** The first version built an `Event` literal inside `Run`. That grew Run's frame from 0x60 to 0xc0 bytes even with tracing off, and spawn+wait measured about 90 ns slower than base (measured: `bench-r1-prefix.txt`). Every emission point now calls a small `//go:noinline` helper that builds the event.
- **Binary size.** The definition-ID constants and the site table are a few hundred bytes per example (inferred from the emitted source). The trace switch (the `rttrace` recorder and writer, linked by `main`) adds about 62 KB to each example: 2,649,099 bytes with the switch against 2,586,999 without it for `checkout` (measured). A first writer used `encoding/json` and added 1.48 MB, so the writer now encodes its four fixed shapes by hand (measured).

## Sampling

**Decided default: off.** With no `EFFRA_TRACE`, the root has no tracer, the program raises no event and writes no file. That costs nothing measurable (above).

**When on: keep every event, up to the bound, with no per-event sampling.** A cause trace and a law check need the whole causal chain inside a root: the deadline, the request, the observation and both exits. Dropping events at random would cut chains and leave the query guessing (inferred). The bound (4,096 events by default) protects memory, and a full recorder names its drops.

**Measured basis.** One event costs about 60 ns: acquire+release went from 30 to 94 ns by minimum with one `release` event, and `BenchmarkTraceCallOn` measured 62.9 ns min. An empty spawn+wait raises four events and costs about 1,090 ns traced against about 793 ns off, by minimum (+37%). The three examples run 9 to 13 events each, and their wall times on and off are equal within noise (25 runs each). About a quarter of an event's cost is the clock read (CPU profile of the traced acquire).

**Capability-call events: on by default whenever tracing is on.** Off, a call point costs one inlined compare (1.28 ns, measured). On, it costs about 63 ns (measured). Calls are 2 to 3 of the 9 to 13 events in the examples (measured). A capability operation is the effect boundary the rows name, and it usually does I/O that costs microseconds or more (guess). Without call events, the query could not say which operation a task was in when its deadline passed.

**Per-root sampling: deferred.** A server would sample whole request roots, one in N, and keep every event of a sampled root. That needs request roots, which arrive with #201's server work (follow-up).

## OpenTelemetry mapping

Written by hand. `runtime/rt` has no SDK dependency, and an exporter is a follow-up. Attribute names are from the OpenTelemetry semantic conventions at `8a6e3935` (after v1.44.0).

| Effra | OpenTelemetry |
| --- | --- |
| A run (one root task) | One trace. `trace_id`: 16 random bytes made by the exporter when the root starts, or the incoming W3C `traceparent` when a server root continues a caller's trace. |
| A task | One span. `span_id`: 8 bytes derived from the task ID and the run (the task ID alone is unique per process). Name: the definition ID. Start and end time: the `start` and `body-exit` events. |
| Parent task | The parent span: `parent_span_id` is the parent task's `span_id`. The root's parent is the incoming remote span, if any. |
| Capability call | A child span of the task's span, from `call` to the next event of the task (or an event on the task's span, when the exporter keeps calls flat). Name: the operation's definition ID, such as `latency.Gateway.Authorize`. |
| Definition | `code.function.name` = the definition ID. |
| Site | `code.file.path` = the `.sketch` file, `code.line.number` and `code.column.number` = the site. |
| Revision | A resource attribute, `effra.revision`, set once per process (an Effra-specific name; OTel has none). |
| `deadline`, `cancel-request`, `interrupt`, `release`, `construct`, `handler`, `observe` | Span events on the task's span, named by kind, with `code.*` attributes for the site. |
| `body-exit` success | Status `Unset`. |
| `body-exit` with a failure | Status `Error`, `error.type` = the failure's definition ID (for example `timeout.Expired`). A failure that a catch handled leaves the status alone, as the conventions say for handled errors (`recording-errors.md:39`). |
| `body-exit` interrupted | Status `Unset`, attribute `effra.exit = interrupted`: a requested stop is not an error. |
| Defect (`panic`) | Status `Error`, an `exception` event with `exception.type` = the panic value's type and no `exception.message` by default (privacy). The G2 chain goes in `code.stacktrace` as definition IDs and sites, not Go frames. |

## In-memory recorder

`rttrace.Recorder` (`runtime/rt/rttrace/rttrace.go`) is an `rt.Tracer`:

- **Bound.** It keeps the first `Cap` events of a run (default 4,096, about 450 KB of preallocated slots; inferred from the slot size of 112 bytes). A recorder exists only when a run records.
- **Drop policy.** When it is full, it drops every later event and counts it. The tail of a run longer than the bound is missing and says so: the header carries `dropped`, and the query adds a `dropped` omission and allows parents it cannot resolve. A ring that keeps the latest events (Go's `FlightRecorder`) suits long-running servers better. It is deferred because it needs a lock or a seqlock per slot.
- **Never blocks.** One atomic add reserves a slot, the event is copied in, and an atomic store publishes it. No lock, channel or I/O sits on the emitting path (`TestTraceRecorderIsBounded`: 16 goroutines emit 16,000 events into a capacity of 8, and all finish).
- **Reads.** `Records()` returns the published events in arrival order and counts the slots still being written as in flight. Read it after the traced tree has finished. `Reset` is for benchmarks and runs only while no task runs.
- **Writer.** `WriteFile` writes one header line (`schema` `effra-trace-1`, `revision`, `producer`, `capacity`, `events`, `dropped`) and one JSON line per event. It fills a task event's site from the program's site table and a task's parent from its start event.
- **Tests.** `TestTraceEventsCarryIDsAndParents`, `TestTraceTimeoutRecordsDeadline` (under the `Virtual` clock) and `TestTraceCancelRequestPrecedesObservation` read the real recorder.

## Query surface

**Prototype: `sketchc trace [-json] FILE TRACE`** (`prototypes/sketch9/sketch/trace.go`). It loads FILE through the checker, builds the model and computes the revision. Then it reads the trace and refuses it (non-zero exit) when:

- the header is missing, or its schema is not `effra-trace-1`;
- the revision differs from FILE's;
- the header's event count differs from the lines in the trace (truncated or edited);
- an event names a definition ID the model does not have;
- a task event's site is not its definition's site, or a task kind names a definition that is not a task;
- a point event matches no `(kind, def, site)` point of the model;
- a task names a parent that never started, unless events were dropped.

It prints the task tree, every event with its enclosing definition, the cause trace of the root exit and the omissions. It never fills a gap: what it cannot see, it names. Missing values, direct function calls, dropped events, events in flight and missing exits are all named.

**Evidence envelope.** `-json` prints `trace-evidence-1`. Its envelope fields are those of #156's `review-evidence-1` spec: `schema`, `producer`, `revision`, `budgets`, `omissions` and `fingerprint`. The fingerprint is the SHA-256 of the canonical JSON of the answer without it. #156 has no merged schema. Whichever of #156 and the trace work lands second reuses these six fields with these meanings, and adds only its own payload (`tasks`, `events`, `causeTrace` here; roots and the evidence tree there).

**Proposed: `ef trace` and MCP `code.trace`.** One query, two transports, one schema, as `ef inspect` and its MCP tool share the checker model today:

- `ef trace FILE TRACE [--json] [--task N] [--why DEF]` prints the same answer as `sketchc trace`. `--why` starts the cause trace at a failure definition.
- MCP `code.trace` takes `{file, trace, expectedRevision?, task?, why?}` and answers with the `trace-evidence-1` envelope. Like `code.type`, it refuses a request whose `expectedRevision` differs from the source. Like Replay.io's MCP server, an agent starts from the overview (the task tree and the cause trace) and asks for detail by task or definition.
- Both read only recorded files. A live view of a running program is `ef ps` (#201, G3).

## Cause traces

The query explains the root exit. For each reason the root exits with, or for each failure a catch handled on the way, it walks back through the run and the model:

- **`timeout.Expired`** comes from a deadline of a timeout block that the handling task owns. The answer names the `deadline` event, the block's definition, spelling, site and row, and then the child's `call`, `cancel-request`, `interrupt` (where it observed the stop) and `body-exit`.
- **A typed failure** comes from a child whose exit names it, recursively, and from a `call` event whose operation's error row in the model contains it. The row is a checked fact, so the answer says "raised by call `lookup.Orders.Find` … whose row is `error[lookup.OrderNotFound]`", not a guess from strings.
- **A defect** prints its chain as task and definition IDs with each definition's site: the G2 chain text of `docs/spec/runtime.md` with definition IDs in place of spans.

Effect's `Cause.pretty` prints every reason of a cause with stack frames, and its spans come from `withSpan` calls or captured `Error` stacks. Here the frames are definition IDs and checked sites, and the links between reasons come from the task tree and the model. Real output for the timeout example is in the [preview](../previews/2026-10-27-traceability.md).

## Privacy and budgets

- **No values by default.** An event carries IDs, sites, kinds, task numbers, a monotonic time and, for an exit, the reasons by type name. The writer renders a failure as its type name (`OrderNotFound`), never its fields. A defect is its panic value's type. `TestWriteJSONLNamesTypesNotValues` checks that a field value (`secret-7`) never appears. The query's envelope states the omission (`values`).
- **`Redacted` (#67).** When values join a trace (an opt-in follow-up), a value of a `Redacted` type renders as its type name only, on every path: event attributes, defect values and OTel `exception.message`.
- **Series budget (#69).** The definition IDs and kinds are a closed, compile-time set: a program's possible event series are its task definitions and its points: 7 to 12 points for each example (inferred by counting the capability calls, wire builds and catch arms of each file). They fit #69's series budget by construction. Task numbers and times are values in a series, never labels. Free-form labels, such as the per-request attributes of #69, count against that budget.
- **Event budget.** The recorder's capacity: 4,096 by default, named in the header and the envelope's `budgets.events`.
- **Byte budget.** The query reads at most 8 MiB and refuses a larger trace. An event line is about 120 to 130 bytes (measured: 1,270 to 1,818 bytes for 9 to 13 events plus a header of about 177 bytes), so a full default recorder writes about 0.5 MB (inferred).

## Decision table

| Decision | Verdict | Reason | Pillar |
| --- | --- | --- | --- |
| Raise task events in `runtime/rt`, with the tracer pointer inherited at task start | ADOPT | The trace must come from the real task tree that #124 and #200 read. Off costs no allocation and no measured time. | P1, P4, P7 |
| Event kinds from #124's vocabulary, plus `call` and `interrupt` | ADOPT | The law suite reads the same stream; the two additions refine orderings the laws already state. | P4, P1 |
| Definition IDs derived by the checker, emitted as constants | ADOPT | The claim under test: IDs from checked declarations, stable per revision, never typed by hand or computed at run time. | P6, P1 |
| Revision once in the header, plain IDs in events | ADOPT | Per-event qualification adds about 46% to each example trace; the query checks the header first. | P7, P6 |
| Default sampling off; when on, keep every event up to the bound | ADOPT | Complete chains for cause traces and laws; about 60 ns an event. | P1, P7 |
| Capability-call events on whenever tracing is on | ADOPT | They are the effect boundary the rows name; 1.28 ns off, about 63 ns on. | P1, P5 |
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
| Full record and replay as the default | REJECT | It records every instruction; Effra records checked effect boundaries. | P7 |

## Measured overhead

Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64, shared with other agents (load average 6 to 13 during the runs). Read the minimum.

- Microbenchmarks: `go test -run '^$' -bench 'Poll|SpawnWait|AcquireRelease' -benchmem -count 2 ./runtime/rt/`, five rounds on head and five on a `git archive` copy of base `854f21e7` in a scratch directory, interleaved, so 10 samples per row and tree (`bench-r1.txt`). Calls: `go test -run '^$' -bench 'TraceCall' -benchmem -count 10 ./runtime/rt/` (`bench-calls-r1.txt`).
- Examples: `measure-examples.sh` (in the unit directory, with `bench.sh` and `strip_recorder.py`) builds each example with `sketchc build -rt kernel` and runs it 25 times off and 25 times on, interleaved, timing each process with `date +%s%N` (`examples-r1.txt`). Wall time is the median of 25 runs and includes process start.

| Row | base `854f21e7` (min / median) | off: head, no tracer (min / median) | on: head, recorder (min / median) | allocs/op off (base) | allocs/op on | events, trace bytes on |
| --- | --- | --- | --- | --- | --- | --- |
| Poll on a running task, ns/op | 1.522 / 1.766 | 1.516 / 1.819 | 1.456 / 1.631 | 0 (0), 0 B | 0, 0 B | 0 events |
| Spawn + wait of an empty task, ns/op | 769.3 / 929.2 | 793.1 / 899.8 | 1090.0 / 1295.0 | 3 (3), 320 B | 3, 320 B | 4 events (start, body-exit, observe, release) |
| Acquire + release of a no-op resource, ns/op | 29.59 / 36.49 | 30.05 / 35.30 | 93.89 / 110.70 | 0 (0), 0 B | 0, 0 B | 1 event (release) |
| `checkout` example, wall time median of 25 runs, ms | — | 3.29 | 2.75 | — | — | 9 events, 1,269 bytes |
| `timeout` example, wall time median of 25 runs, ms | — | 105.41 | 104.73 | — | — | 13 events, 1,818 bytes |
| `failure` example, wall time median of 25 runs, ms | — | 3.13 | 2.95 | — | — | 9 events, 1,297 bytes |

Comparison of the off path against base `854f21e7` (measured unless labelled):

- **Allocations.** Off equals base on every row: 0, 3 and 0 allocs/op, and 0, 320 and 0 B/op.
- **Time.** Off/base ratios, min / median: poll 1.00 / 1.03, spawn+wait 1.03 / 0.97, acquire+release 1.02 / 0.97. The hand-written spawn+wait baseline runs the same code in both trees, and its head/base ratio was 1.11 / 0.97. So the box's noise is wider than every off/base difference. Inferred: tracing off costs no measurable time.
- **Poll.** `go build -gcflags=-m ./runtime/rt/` reports `can inline (*Task).Stopped`, and its body is unchanged: `t.stopped.Load() && t.regions.top == interruptible` (`inline-r1.txt`).
- **On.** One event costs about 60 to 65 ns: acquire+release with one `release` event went from 30.05 to 93.89 ns by minimum, and `BenchmarkTraceCallOn` measured 62.88 / 66.26 ns against 1.28 / 1.46 ns for `BenchmarkTraceCallOff` (`bench-calls-r1.txt`). Spawn+wait with four events went from 793 to 1,090 ns by minimum (+37%). Allocations stay at the off counts, because the recorder's slots are preallocated.
- **Examples.** Wall time includes process start (about 2 to 3 ms) and, for `timeout`, the 100 ms deadline. On and off differ by less than the run-to-run spread (minimums: `checkout` 2.24 off and 1.98 on, `timeout` 103.42 and 103.52, `failure` 2.33 and 2.05 ms). Inferred: recording 9 to 13 events costs nothing measurable in a whole program.
- **Binary size** (kernel mode, measured): `checkout` 2,649,099 bytes with the trace switch, 2,586,999 bytes without it (a copy of the emitted `main.go` with the recorder removed), 2,548,752 bytes in embedded mode. The switch costs 62,100 to 62,372 bytes across the three examples. The kernel runtime costs about 38 KB over the embedded prototype runtime.
- **Earlier runs.** `bench-r1-prefix.txt` is the run that found Run's frame growth. `bench-r1-c1.txt` is the run at the first runtime commit, before the `cancel-request` ordering fix. The fix changed no instruction on the benchmarked paths (inferred from the diff: it moves an emission inside `Interrupt` and adds one to a branch that only a stopped owner reaches).

## Prior art

- **Effect `withSpan`, `Tracer` and `Cause`** (effect-smol `3a1128c7`, `packages/effect/src/Effect.ts:8338` `withSpan`, `Tracer.ts:28` the `Tracer` interface, `Tracer.ts:254-260` `captureStackTrace`, `Cause.ts:1159` `pretty`). A span exists where the programmer writes `withSpan`, and its source location comes from a captured `Error` stack. Effra takes the guarantees: one tracer per runtime, spans nested by fiber, and a `Cause` that keeps every reason. It replaces the hand-written span with a checked definition and a compile-time site.
- **Encore** (`c2881447`, `runtimes/go/appruntime/exported/trace2/events.go:17-52`). Static analysis of a Go application finds every API and infrastructure call, and the runtime traces them with a fixed event vocabulary. This is the closest precedent for tracing by construction. Effra keys events to definitions and to the runtime-law vocabulary instead of infrastructure kinds.
- **Go `runtime/trace` and pprof labels** (go1.27.0, `runtime/trace/annotation.go:38,95,122`, `flightrecorder.go:48`, `runtime/pprof/label.go:56`). Tasks with parents, carried by `context`; regions; a flight recorder that keeps the latest window; goroutine labels for profiles. Effra already has the task tree, so its trace reads the tree. The flight recorder is the model for the deferred ring recorder. The `//line` directives make Go's own traces and profiles name `.sketch` lines too.
- **Elixir `:telemetry` and BEAM tracing** (`:telemetry` `ce4213ee`, `src/telemetry.erl:71-83,105,202,367`; OTP `516126e9` `lib/runtime_tools/src/dbg.erl`; recon 2.5.6 `c2a76855`). `:telemetry` runs handlers in the emitting process and detaches a handler that raises: Effra disables a panicking tracer the same way. BEAM's `dbg` traces any function by module, function and arity, and recon bounds such traces for production. Elixir's lowering path: `Logger` macros capture `__CALLER__` (`lib/logger/lib/logger.ex:1108`, Elixir `91ee75bb`), so file, line, module and function are compile-time metadata, and `:compile_time_purge_matching` (`:290`) removes calls at compile time. Effra's call sites are compile-time constants in the same way.
- **OpenTelemetry** (semantic conventions `8a6e3935`, `model/code/registry.yaml`, `docs/general/recording-errors.md`). The export target. See [OpenTelemetry mapping](#opentelemetry-mapping).
- **Temporal event history** (API `d21dd97a`, `temporal/api/history/v1/message.proto:1198-1210,530-536`). Each event has an ID, and a timeout event names the events that scheduled and started the work. Effra links by task, parent and definition IDs instead, and keeps a partial order because a run is concurrent.
- **Replay.io, rr and Pernosco** (rr `c145f2f8`; [Pernosco overview](https://pernos.co/about/overview/) and [Replay docs](https://docs.replay.io/), read 2026-10-27). Omniscient debugging: record once, then query any point; Replay serves a recording to agents through MCP, starting from an overview. That is the direction for `ef trace` and `code.trace`. Effra records only checked effect boundaries, at about 60 ns an event, so it can stay on in tests.
- **Fai's shared query engine** (`3e03dd1`, README:188 Salsa queries, :253 `fai query`). One query engine serves the CLI, the LSP and agents with versioned JSON. `ef trace` and `code.trace` share one schema and the checker model in the same way.
- **Hapsland's review projection** (`a0773f7`, `CONTEXT.md:49-67`: review-projection fingerprints and source fingerprints as equality evidence). The `trace-evidence-1` fingerprint plays the same role: a consumer can reuse or discard an answer by its fingerprint.
- **Compile-to-host languages: Gleam, ReScript and MoonBit have no built-in tracing.** None has a runtime tracer or span construct; each leans on its host's tools. Their lowering paths:
  - **Gleam** (`52e735c8`, `compiler-core/src/erlang.rs:1146-1180`, `:1205-1245`). On the BEAM target, a Gleam function is an Erlang function, so `dbg`, recon and `:telemetry` (through FFI) see it by module and name. The compiler bakes `file`, `module`, `function` and `line` into every `panic`, `todo` and `let assert` error, and `echo` lowers to a call that carries the file path and line. That is construction-time source identity for defects, and Effra's defect chain does the same with definition IDs.
  - **ReScript** (`30ce3698`, `compiler/core/js_source_map.ml`). It emits readable JavaScript and Source Map v3, with source locations attached to JS IR nodes, so the host's devtools and profilers map back to `.res` lines. Effra's `//line` directives are the Go equivalent. ReScript adds no runtime events.
  - **MoonBit** (docs `8d9f3ba2`, `next/toolchain/moon/commands.md:100` `-g/--debug`, `coverage.md`). It emits debug information for its wasm, JS and native backends and instruments branches for coverage at compile time. It adds no runtime tracer either.
  - **Elixir** (above) is the one peer whose host, the BEAM, traces every function. Its compile-time logging metadata and compile-time purging are the closest model for Effra's constant IDs and zero-cost off path.

## Follow-up units

| Unit | Ticket | Scope | Estimate |
| --- | --- | --- | --- |
| G2 chain with sites | #200 | Print a defect chain with definition IDs and sites from the model (runtime.md G2 rule 1, the start site); normative text for the event kinds and the partial order in `docs/spec/runtime.md`. | 1 day |
| Trace laws | #124 | Feed recorded runs to the RS1 reference checker; add `admit`, `cleanup` and `publish` where R2 and R9 need them; check `interrupt` and `call` as refinements. | 3–4 days |
| `ef trace` and MCP `code.trace` | new (after #203) | The query of this note on the main compiler's model, readable definition IDs beside hash IDs in `ef inspect`, one schema in CLI and MCP. | 3 days |
| OTLP export | new | An `rttrace` sink that writes OTLP JSON by the mapping above; W3C `traceparent` for server roots. | 2 days |
| Std `trace`, `log` and `metric` | #69 | User spans and attributes on the same event stream, the series budget, opt-in values through `Redacted` (#67). | 1 week |
| Live view and per-root sampling | #201 | `ef ps` from the live tree; sample request roots one in N; a ring recorder for long runs. | 3 days |
| Machine decision events | new (with the machine runtime) | A `decide` event per machine step, keyed by the step definition, with the decision kind (`Enter`, `Stay`, `Ignore`, `Reject`, `Done`); justify it as an addition to #124. | 1 day |
| Review evidence | #156 | Adopt the shared envelope fields of `trace-evidence-1`, or move them to a shared package if #156 lands first. | 0.5 day |
