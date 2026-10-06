# Checked state machines

Status: authorized implementation extension from owner direction on 2026-10-06; not current language support. Preserve Go-like simplicity, Effect lifetime guarantees and ordinary ADTs. The first profile is a finite flat machine with explicit transitions and owned invocation. The source/test comparison is recorded in [PRIOR_ARTS](../../PRIOR_ARTS.md); independent design counsel can refine this contract before implementation.

Main machine prior art is the active [XState v6 PR5543](https://github.com/statelyai/xstate/pull/5543), `next` revision `2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5`, as explicitly requested by the owner. Earlier v5 observations remain historical and must be revalidated. Use v6's ordinary-function direction to reduce ceremony: the construct declares topology/ownership, while ordinary typed functions and statements express behavior. Do not mirror its complete framework surface. The illustrative syntax below is subject to this Go-like readability test before implementation.

## Shared semantic model

Use ordinary nominal enums for states and external events, with state-specific payloads. A checked machine declaration binds an initial constructor, transition clauses, named guards/work and terminal output. It elaborates to a canonical plan and ordinary checked functions. One library runtime owns admission, state entry, invocation and shutdown. Do not create another type checker, error channel or effect evaluator for machines.

The language construct earns its place by checking finite transition policy and exposing source-bound possible edges. A reducer library can consume the same plan/runtime. A helper returning the whole state enum yields conservatively broad possible targets; the compiler must not invent precise reachability through whole-program analysis.

Illustrative, unimplemented shape; constructor/pattern syntax will follow ordinary checked ADTs:

```rust
enum SearchState {
    Idle
    Loading { query: string }
    Ready { result: SearchResult }
    Failed { reason: string }
}
enum SearchEvent { Start { query: string } Cancel }

machine Search: SearchState receives SearchEvent uses {Catalog} {
    initial Idle
    state Idle {
        on Start { query } => Loading { query }
        on Cancel => ignore
    }
    state Loading { query } {
        invoke Catalog.search(query) {
            success result => Ready { result }
            failure SearchFailed { reason } => Failed { reason }
        }
        on Start { query } => reenter Loading { query }
        on Cancel => Idle
    }
    state Ready { result } {
        on Start { query } => Loading { query }
        on Cancel => Idle
    }
    state Failed { reason } {
        on Start { query } => Loading { query }
        on Cancel => Idle
    }
}
```

This sketch introduces no implicit execution outside a running actor. Machine definition constructs data; spawning an actor starts managed work in an explicit owner. Internal invocation outcomes are runtime envelopes, not forgeable variants of the external event enum.

## Finite transition contract

Every state/external-event pair has an explicit transition, ignore or reject policy; a state's `otherwise reject` covers remaining events. Ordered guards require a final fallback. Detect duplicate unguarded clauses and invalid state/event payloads. Use tag-indexed coverage and bounded plan construction rather than enumerating histories.

The first profile admits pure named guards. Effectful guards diagnose until their separate evaluation/query contract is implemented; no API may execute a hidden effect to answer whether an event is enabled. Expected invocation failures are handled explicitly into declared states/events or propagated through a declared terminal error row. Defects and interruption preserve their full Cause and cannot silently become ordinary failure strings. Requirements and failures survive helpers, aliases and provider capture through the common function/row representation.

Ordinary same-state-tag payload updates retain the current entry, timers and captured invocation input. Explicit re-entry closes that entry and starts a fresh one, even for the same tag. The distinction appears in source and graph output. Changes that require work to restart must use re-entry.

Initial supported behavior: flat states; ordered pure guards; explicit state policy; typed state-owned invocation; explicit re-entry; program-time delays; terminal output. Hierarchy, parallel regions, history, eventless stabilization, automatic supervision, postponed events and durable replay remain unsupported capabilities with clear diagnostics. Their absence cannot silently change source meaning.

## Ownership and publication

Each actor and each state entry has a distinct runtime identity. Completion, failure and timeout envelopes carry actor generation, entry epoch and invocation identity. Validate these at dequeue, including exit then re-entry into the same state tag. Cancellation does not replace this check: a result may already be queued. A foreign callback uses an epoch-bound admission adapter; fencing stops stale state mutation, not a remote side effect already performed.

Leaving an entry invalidates its completion admission, requests cancellation and awaits owned children/finalizers before publishing the next stable state and starting its work. Cleanup defects prevent a successful transition acknowledgement and retain full causes. Final output and stop completion follow actor cleanup. Concurrent stop callers observe the same completed result; interrupting one waiter does not abandon the actor's owned cleanup.

The mailbox serializes external events and self-send. Self-send queues behind the current transition and does not recurse. Waiting for one's own queued acknowledgement must be rejected when statically identifiable and produce a defined runtime failure through aliases. A full mailbox cannot block its own consumer: self-send has explicit nonblocking rejection or another separately bounded policy. Specify item and byte budgets, interruptible external admission, and a bounded control path so queue saturation cannot prevent cancellation or terminal invocation delivery. One invocation may publish at most one terminal completion.

No clock sleeps or guessed yields establish readiness. Program-time timers use the repaired shared scheduler; the real watchdog remains separate. Unsupported unobservable foreign waits remain explicit limitations of deterministic testing.

## Inspection

CLI/MCP consume the same canonical plan: source revision, nominal state/event identity, initial/final states, possible edges, ignore/reject and re-entry policy, named guard/work references, effect rows and owner relationships. Possible edges are distinct from currently enabled guards and observed completed transitions. Runtime snapshots include identity, entry epoch, status and bounded cause information without dumping secret payloads. A static graph is not an execution trace or liveness proof.

Optional state snapshots use an explicit versioned codec and recovery policy. Do not serialize scopes, fibers, service instances or in-flight host operations. Restoring domain state can restart effects; transactional admission, idempotency, outbox, migrations and crash recovery require their own storage contract. No durable or exactly-once claim follows from this primitive.

## Acceptance and delivery

1. Finish canonical functions/finite rows and reviewed lifetime/test foundations first. No opaque callback shortcut.
2. Compile a checked plan and pure stepping with two unrelated examples: search/reload and timed lease/session. Reject missing policy, incorrect payload, invalid target, duplicate clause, effect guard, unsupported statechart features, undeclared failure and missing service. Inspect the exact declared graph and bounded conservative helper targets.
3. Execute the same plans on Go and JS with a shared portable behavioral corpus. Test FIFO and self-send; full-mailbox admission; aliased self-call failure; cancellation and completed release before next entry; same-tag update versus re-entry; already-queued stale completion; late foreign callback; terminal error plus cleanup defect; concurrent stop and interrupted stop waiter; final output after cleanup; and timer re-entry using logical time.
4. Exercise a request-owned actor through a typed HTTP endpoint and a non-server in-memory actor. Test disconnect/stop completion and honest encode-failure policy. An HTTP fixture is an integration consumer, not a machine-specific compiler operation.
5. Map pinned Effect Machine/XState test behaviors to passing, deliberately different and unsupported Effra cases; copied reference tests are not parity. Run full gates, native race tests, bounded compile-cost checks and independent review. Only then update capability descriptions and examples to implemented.

Decided by **Redesign From First Principles** and **Never Block on the Human**: use a small checked surface over shared ordinary contracts, pure guards first, explicit bounded actor runtime and explicit lifetime receipts. This reversible design direction does not close historical Wayfinder HITL tickets.
