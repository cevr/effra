# An effect-native language in practice

Effra's direction is **Go-like directness, algebraic data types, and explicit Effect-style contracts**. A function tells you what it returns, how it can fail and what services it needs. An owning scope tells you who must finish before it can return.

These examples come from reading real Effect application and infrastructure code. They use generic domains and original examples. The [source review](research/effect-native-showcases.md) records the snapshots, comparisons and boundaries.

| Showcase | Status |
| --- | --- |
| Service workflow and explicit composition | Runnable on Go and JS |
| Replacing an owned task | Runnable on Go and JS |
| ADTs, exhaustive matching and payload errors | Proposed; parser/checker/lowering work remains |
| Decoded event types and codecs | Proposed; runtime validation remains essential |
| Owned replay/live streams | Proposed library and stream support |
| Durable command processing | Proposed application services, not an automatic language guarantee |
| Infrastructure outputs and provider graphs | Design exploration; no Effra deployment framework exists |

All code below labeled **proposed** is design notation, not accepted prototype syntax. Proposed service names describe application ports, not an existing standard library.

| In Effect application code | Effra surface | Obligation that remains |
| --- | --- | --- |
| Effect type parameters, generators and yielded calls | `effect fn`, `throws`, `uses`, `run` | Checked contracts and lazy execution |
| Context.Service and provider composition | `service`, `impl`, explicit provision | Nominal identity, replaceable behavior and initialization lifetime |
| Tagged declarations, constructors and match helpers | Proposed `enum` and `match` | Payload checking and exhaustive coverage |
| Schema codecs and decode effects | Proposed explicit codec derivation and `Json.decode` | Runtime validation and wire compatibility |
| Fibers, scopes and interruption combinators | `scope`, `fork`, `join`, `interrupt` | Owned completion and preserved cleanup causes |
| Output mapping and resource references | Explored `output` expression block | A deployment graph and distinct evaluation phase |

## 1. A workflow with readable dependencies

**Runnable:** [workflow.ef](../examples/workflow.ef).

```rust
effect fn welcome(id: string) -> string
    throws {Denied, UserMissing, DeliveryFailed}
    uses {Access, Directory, Delivery}
{
    run Access.check(id)
    let name = run Directory.name(id)
    run Delivery.send("Welcome, " + name)
}
```

Effect applications express this with service tags, `Effect<A, E, R>`, generator bodies and provider composition. Effra makes the success, failures and dependencies a source contract. Each `run` is an execution boundary; constructing `welcome("42")` is still lazy.

The full example supplies three demo providers at the entry point, handles each admitted failure, and prints successful and denied outcomes. It sends no real mail. Omitting a provider produces EF108; omitting an admitted failure from the contract produces EF107. An Access requirement exposes a dependency; the provider must still implement the actual authorization policy.

```sh
./bin/ef run examples/workflow.ef
./bin/ef run examples/workflow.ef --target js
./bin/ef inspect examples/workflow.ef welcome
# queued: Welcome, Ada
# access denied
```

Parameterized providers with dependencies and shared scoped initialization would replace more `Layer` construction boilerplate. Those are future work: current user providers are self-contained.

## 2. ADTs make state and decisions explicit

**Proposed.** Gent uses schema-backed phase states and an exhaustive runtime projection. T3 Code uses tagged transition decisions that a later service interprets. Both are good candidates for native sums and matching.

```rust
enum RunState {
    Idle
    Running { runId: string }
    Waiting { runId: string, requestId: string }
}

fn status(state: RunState) -> string {
    match state {
        RunState.Idle => "idle"
        RunState.Running { runId } => "running " + runId
        RunState.Waiting { runId, requestId } => "waiting " + requestId
    }
}
```

The proposed checker knows each alternative and its payload. A Running value cannot omit its run ID; an Idle value cannot carry a stray pending request. Adding a fourth variant makes this complete match incomplete. Exhaustiveness is a compiler obligation, not a default branch that silently accepts every new case.

A decision value and an effect failure have separate jobs:

```rust
enum Transition {
    Reuse
    Restart
    Reject { reason: string }
}

error SessionRejected { reason: string }

effect fn describe(decision: Transition) -> string throws {SessionRejected} {
    match decision {
        Transition.Reuse => "reuse the session"
        Transition.Restart => "restart the session"
        Transition.Reject { reason } => fail SessionRejected { reason }
    }
}
```

`Reject` is ordinary policy data until this interpreter chooses to fail. Matching a value does not catch the failure channel. Payload errors, records and matches all require new compiler support; the prototype currently has payload-free errors and primitive values.

This representation prevents malformed state combinations. It does not prove that a transition is legal, that a request belongs to the current run, or that the state was committed atomically. Those rules belong to the domain service.

## 3. Boundary types still need decoding

**Proposed.** T3 Code validates stored event payloads, metadata and envelopes; Gent validates interaction records and decisions. Removing their schema imports must not remove those checks.

```rust
enum EventV1 {
    Started { runId: string }
    Text { runId: string, text: string }
    Finished { runId: string, result: string }
}

derive JsonCodec for EventV1

effect fn receive(input: bytes) -> EventV1 throws {DecodeError} {
    run Json.decode<EventV1>(input)
}
```

This proposes one declaration for a static sum and an explicitly requested codec. The codec must validate the discriminator and every payload field before returning EventV1. A cast or a tag check cannot substitute for decoding. Refined identifiers, limits and cross-field validation need explicit validators beyond the string fields shown here.

The wire format also needs a defined discriminator, field names and compatibility policy. V1 is a versioned boundary; changing an enum is not permission to reinterpret stored history. Migration/upcasting remains explicit. No blanket automatic JSON support or TypeScript structural soundness claim follows from having an ADT.

## 4. Replacement means completed cleanup

**Runnable:** [latest-task.ef](../examples/latest-task.ef).

```rust
effect fn replacement() -> string uses {Clock} {
    scope {
        let previous = fork search("old")
        run previous.interrupt()
        let current = fork search("new")
        run current.join()
    }
}
```

A production latest-work adapter has to connect host calls to a scope, replace a fiber and dispose that scope. Native syntax can make one replacement's ownership visible: interrupt the previous child, wait for its cleanup, then admit its replacement. The example's search function uses cancellation-aware Clock.sleep and prints `result: new` on both targets.

This chooses to wait for prior cleanup before replacement starts. A lower-latency policy may overlap generations under a surviving owner; it then needs generation checks before publishing results. This snippet does not claim to replicate a host adapter's concurrent admission or deduplication behavior. Duplicate-key handling, UI lifetime and stale-result publication remain explicit application policies.

Cancellation remains cooperative. Child failures and cleanup defects remain observable; a timeout or interrupt cannot silently erase them. See [runtime contracts](runtime.md).

## 5. A readable consumer of an owned event stream

**Proposed.** T3 Code's replay/live stream implementation handles subscription ordering, buffering, duplicate filtering, acknowledgements and overflow. Effra could simplify its consumer:

```rust
effect fn forward(cursor: i64) -> ()
    throws {FeedUnavailable, DecodeError, SendFailed}
    uses {EventLog, Client}
{
    scope {
        let feed = run EventLog.follow(cursor)
        while let Some(event) = run feed.next() {
            run Client.deliverAndAck(event)
        }
    }
}
```

Here EventLog.follow is a proposed library service that acquires a subscription into the current scope. It must subscribe and drain live events before reading the replay high-water mark, replay through that mark, then suppress duplicates. It must bound retained items and bytes, retain in-flight delivery charges through acknowledgement, and close its producer on overflow or interruption. Client.deliverAndAck names an actual protocol commitment; sending bytes alone is not an acknowledgement.

`run feed.next()` keeps managed execution explicit. Option, pattern loops, generic stream handles and scoped stream acquisition are not implemented yet. A goroutine plus an unbounded channel would not meet the proposed contract. The language can expose ownership and propagate failure; the library still owns this replay protocol.

## 6. Durable commands keep their transaction boundary

**Proposed.** An agent answer or background command must survive retries and restarts without being applied twice. ADTs can make the possible admission results easy to inspect:

```rust
enum Admission {
    Accepted { receiptId: string }
    Replayed { receiptId: string }
    Conflict { reason: string }
}

effect fn submit(command: Command) -> Admission
    throws {StorageError, InvalidCommand}
    uses {Journal}
{
    run Journal.admit(command)
}
```

Journal.admit is an application operation with an explicit contract: validate identity and input; atomically record the command receipt, authoritative events, projections and outbox work; return an existing receipt for an identical retry; reject a conflicting retry. A worker delivers outbox work under stable idempotency keys. Remote delivery may still be at least once unless the receiver deduplicates.

Scopes manage running work and cleanup. They do not supply durable transactions, first-answer-wins, crash recovery or exactly-once side effects. Effra should make the dependency and admission result readable while preserving that service boundary. This proposal combines patterns from the inspected applications; it is not a claim that every source uses this exact storage protocol.

## 7. Infrastructure has a different kind of laziness

**Design exploration.** Alchemy's Output.map constructs a dependency expression. Its resource graph and bindings connect deployment-time resources to runtime capabilities.

```rust
let api = Infra.service("api", ApiProgram)
let web = Infra.website("web", {
    apiUrl: output { api.url + "/v1" }
})
```

The proposed `output` block would construct a graph expression containing an unresolved Output<string>, not read a string immediately. Its dependencies must survive plan/diff so deployment can resolve fresh attributes. Resource identities, state, reconciliation, permissions and deploy/runtime bindings remain framework responsibilities.

This is deliberately separate from ordinary `run`: running an effect inside a server and resolving a deployment output have different phases. A provider could hide cloud binding ceremony behind a nominal service, but the compiler would need explicit target/phase rules, and the library would need resource sharing and binding policy. No cloud calls or deployments are part of these showcases.

## What to build next

The strongest next language slice is records, closed ADTs, payload errors and exhaustive match, followed by explicit codec derivation. It makes state machines and boundary handling clearer without requiring higher-order row inference or a new scheduler. Match checking should use declared variants and local payload types; Go lowering should use tagged data and switches, with JS lowering preserving the same discriminator and coverage.

An implementation receipt should include both-target examples, rejection of missing branches and wrong payloads, imported/public type inspection, and a decode test rejecting malformed external data. Keep compile-stage measurements separate and measure representative ADT/match fixtures before adding inference complexity. Scoped provider graphs and streams can then build on that data model. Infrastructure phase syntax needs a separate design decision.
