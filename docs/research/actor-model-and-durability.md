# Actors, supervision and durability

Research dated 2026-10-06, extended after the owner clarified ordinary actors. Recommendation and design inference, **not implemented Effra support**. The [actor contract](../specs/actors.md) treats handlers and receive loops as ordinary runtime/library behavior. The [machine profile](../specs/state-machines.md) specializes it with pure/effectful steps, owned entries and bounded admission; it explicitly excludes supervision and durable replay. This note proposes separable capabilities without claiming their implementation. It follows [NORTH_STAR](../../NORTH_STAR.md), [library capability contracts](../specs/standard-library-capabilities.md) and the standing first-principles/asynchronous-supervision principles. Canonical terminology remains in [GLOSSARY](../../GLOSSARY.md).

## Recommendation

Make actors first-class in the **checked semantic model**: typed handles, messages, replies, terminal failures, service requirements, owners and inspection. Implement their local behavior with a small native owned runtime and modular libraries. Add explicit supervision as a library over actor factories and full terminal causes. Durable addressing, storage and deployment are separate opt-in providers. Keep durable workflow replay a distinct contract. Do not make a BEAM-like VM, distributed registry or storage engine a prerequisite for ordinary Effra programs.

This fits Go-like local control flow, the native Go default and the pinned JS/Effect target. It gives rooms, sessions, agents and jobs useful state/lifetime ownership without claiming Go goroutines have BEAM process isolation. Ordinary service/impl/effect contracts express behavior without requiring a machine or a second actor DSL. A machine adds checked transitions and entry lifetimes; an owned effect without a message interface remains a fiber. Inspect all through the same compiler model.

| Need | Smallest appropriate mechanism | Additional obligation |
| --- | --- | --- |
| Stateful activity within one process lifetime | Checked local actor and bounded mailbox | Completed cleanup; typed admission and reply behavior |
| Recover a failed local activity | Explicit supervisor and fresh actor factory | Restart classification, fresh acquisition, restart budget |
| Find one stateful entity across restarts/machines | Stable typed address plus durable-entity provider | Routing, incarnation fencing, storage acknowledgement, deduplication |
| Resume a multi-step business operation after crashes | Durable workflow history and versioned steps | Replay discipline, side-effect idempotency, history migration |
| Replace code used by live VM processes | VM code-loading/release protocol | Representation compatibility and old-code retirement |

These are separable capabilities. A stable key does not imply durable messages; a durable message does not imply an atomic business transaction; a restart does not resume a live stack frame.

| Owner | Responsibility |
| --- | --- |
| Language/checker | Message/reply identities, failure/service rows, transfer/owner admission, checked machine plan and diagnostics |
| Native owned runtime / JS Effect adapter | Explicit dispatch/concurrency policy, scopes, cancellation, mailbox limits, causes and incarnation checks; machine entry policy only when selected |
| Modular standard library | Actor factories, supervisor policies, monitors, addressing interfaces and explicit workflow combinators |
| Storage provider | Enforced transaction/fence, durable acknowledgement, inbox/reply/outbox records and codec/history migration |
| Deployment provider | Routing, process restart, lease qualification and versioned handover |
| Optional VM target | Live-code replacement and VM-specific isolation; a separate target with its own contract and costs |

## Primary-source comparison

Evidence labels below distinguish documentation claims, inspected source and Effra design inference. No upstream performance tests or conformance cases were executed.

**BEAM / OTP — documentation verified, OTP 29.1.1.** Erlang supports simultaneous current/old module code. Qualified calls enter current code; loading another version can purge old code and terminate processes still using it. This is a VM protocol, not a property of an actor handle. OTP supervisors offer one-for-one, one-for-all and rest-for-one strategies, with restart intensity/time limits and escalation. Borrow the explicit restart policy and dependency ordering; do not advertise portable Go replacement of active stack frames. [Code loading](https://www.erlang.org/doc/system/code_loading.html), [supervisor behavior](https://www.erlang.org/doc/system/sup_princ.html).

**Effect Cluster — pinned source verified.** At `460272d30457f4697d8b8c52cad41caccbcace08`, Entity exposes typed RPC clients and explicit concurrency, mailbox capacity and defect-retry options. Persisted and WithTransaction both default false; transactional handling depends on MessageStorage. No-op storage performs no persistence and uses the identity transaction. Sharding refuses persisted messages when storage is unavailable. Therefore Cluster is useful prior art for schema/address/storage boundaries, not proof that every request is durable or every handler is strictly serialized. [Entity.ts:123–163](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster/Entity.ts#L123), [ClusterSchema.ts:35–77](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster/ClusterSchema.ts#L35), [MessageStorage.ts:873–896](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster/MessageStorage.ts#L873), [Sharding.ts:1137–1180](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster/Sharding.ts#L1137). These were read with `git show` at the pin, not the cache's newer working tree.

**celld — current documentation and selected ordinary-handler example source, no engine audit.** Its ownership/data guarantees require conditional object-store writes, read-after-write consistency and ranged reads; epoch GC additionally depends on list-after-write consistency. Ownership epochs isolate stale writes into old storage prefixes. Acknowledgement waits for a durability proof and ownership evidence; deployment must provide a process supervisor. This is a concrete example of why actor durability includes storage and operational protocols. Its storage fence does not by itself authorize or fence writes to unrelated external services. [Guarantees and assumptions](https://celld.dev/docs/guarantees/). The documentation also describes conservative handling of ambiguous requests; application retry/idempotency still matters. At source pin `f2bf648663a610eefde71f3547ad61e9b896b1f0` (v0.6.1), [counter HTTP behavior](https://github.com/denoland/celld/blob/f2bf648663a610eefde71f3547ad61e9b896b1f0/examples/counter/index.js) is an ordinary class handler with storage and a named-object client, without a machine declaration. Example source does not prove engine crash semantics.

**Rivet Actors — documentation verified; selected persistence source inspected.** Actions run in parallel by default; queues provide concurrency control. Thus an async action can yield while another changes shared state: a single actor identity is not whole-action serialization. State mutations schedule throttled saves (default one second), independently of action boundaries. Immediate save waits for completion; ordinary save schedules and returns. Persisted state and ephemeral runtime objects are distinct. [Actions](https://rivet.dev/actors/docs/actions/), [state](https://rivet.dev/actors/docs/state/), [lifecycle](https://rivet.dev/actors/docs/lifecycle/).

The fetched source pin is `0247751bdafc82e72f3e4f52082fc2c1694a5c81` (`v2.3.20-60-g0247751b`). Inspected [native.ts:3302–3331](https://github.com/rivet-dev/actors/blob/0247751bdafc82e72f3e4f52082fc2c1694a5c81/rivetkit-typescript/packages/rivetkit/src/registry/native.ts#L3302) confirms immediate save awaits `actorRequestSaveAndWait`, while the ordinary branch calls `actorRequestSave`; [config.ts:1308](https://github.com/rivet-dev/actors/blob/0247751bdafc82e72f3e4f52082fc2c1694a5c81/rivetkit-typescript/packages/rivetkit/src/actor/config.ts#L1308) pins the default interval. This is not an audit of the native storage engine or proof of an atomic external side effect.

**Rivet Workflows — documentation verified.** Completed steps record results that replay can reuse. Actor-local APIs are restricted to step callbacks; orchestration and side effects are separated. Failure handling does not undo earlier state mutations. Version gates record the chosen branch in history. Borrow explicit history/step/version boundaries; do not silently turn ordinary Effra effects into replayable workflows. [Workflow overview](https://rivet.dev/workflows/docs/), [steps](https://rivet.dev/workflows/docs/steps/), [versioning](https://rivet.dev/workflows/docs/versioning/).

**Cloudflare Durable Objects — current documentation verified.** One active object instance provides a stateful address, but instances can share Worker globals and in-memory state is lost on eviction. Awaiting external I/O permits interleaving; storage input gates and output gates protect specific storage/acknowledgement boundaries, not arbitrary async handlers. Alarms are at-least-once, with bounded automatic retries; handlers can restart from the beginning. Shutdown callbacks are unavailable because their execution cannot be guaranteed. [Memory](https://developers.cloudflare.com/durable-objects/reference/in-memory-state/), [gates and external I/O](https://developers.cloudflare.com/durable-objects/best-practices/rules-of-durable-objects/), [alarms](https://developers.cloudflare.com/durable-objects/api/alarms/), [lifecycle](https://developers.cloudflare.com/durable-objects/concepts/durable-object-lifecycle/).

## Ownership and failure invariants for an Effra extension

The following are proposed Effra requirements, not assertions that the compared systems enforce them identically.

1. **Typed behavior.** Preserve protocol argument/reply identities and each operation's failure/service rows. Post reports admission and is limited to operations with empty expected failure rows; request/reply reports completed handler cleanup. A machine additionally reports an acknowledged transition and final Output. Monitoring reports full terminal Cause, including defects, interruption and cleanup failures. Spawn follows the fork rule for unobserved terminal failures. Network ingress decodes through explicit codecs; a static type is not authentication or wire validation.
2. **Bounded admission.** Require item/retained-byte and reply-byte budgets, plus explicit in-flight policy. Define ordering, rejection, stop and abandoned-waiter behavior. A payload count alone does not bound memory. Separate external admission from privileged shutdown so saturation cannot prevent cleanup. Reserved work-completion capacity is a machine-specific addition, not an ordinary actor invariant.
3. **Send ownership.** Start with resource-free immutable portable messages or an explicit checked transfer/copy discipline. An actor handle cannot legalize sending a borrowed file, mutable host pointer, closure capture or shorter-lived service. Mutable aliasing and shared DI services can still race across actors; a per-actor mailbox does not isolate those objects. Reject unsupported admission rather than promising general borrow checking or heap isolation.
4. **Supervised restart.** Monitor completed exits, classify normal completion/business failure/defect/interruption explicitly, and use a finite restart budget with program-time backoff and escalation. Each restart gets a new child scope, a fresh factory invocation and fresh provider acquisition. Close children and releases before replacement; preserve cleanup defects. Reusing a damaged materialized service requires an explicit, separately justified policy. Shutdown prevents new restarts; business-command retries are different from actor restarts.
5. **Honest isolation.** Managed Go panics can become defects at admitted boundaries. OOM, OS kill, fatal runtime failure and arbitrary unmanaged goroutine panics remain process failures. Native actors do not gain BEAM heap/process fault isolation. Use a deployment supervisor for process recovery; use OS-process isolation when a workload needs a stronger boundary. JS target behavior must stay explicit and use the pinned Effect runtime rather than an invented scheduler.
6. **Identity.** Separate stable address from the running incarnation epoch, and from the machine's entry epoch. Clients retain an address across restarts; callbacks, ownership claims and storage writes carry the incarnation/fence. Old replies cannot satisfy new calls. Authority requires enforcement at the mutation boundary, not merely checking the epoch earlier.

## Durable boundary and crash semantics

Design inference: define a provider transaction capable of recording admission identity/inbox, state version, reply and outbox intent together, with deduplication and a checked owner fence. Specify when the provider durably acknowledges, how duplicate requests retrieve prior results, and how records expire without admitting old duplicates accidentally. A generic database interface does not automatically provide this guarantee.

The dangerous interval is external success followed by a crash before recording success. An outbox ensures an intent is not lost but delivery may repeat. External writes need an idempotency key, external fencing support or an application compensation/reconciliation protocol. A storage lease alone cannot stop a stale owner charging a card or updating another database. Cancellation is not rollback; a timeout can leave committed work and an unknown result. “Exactly once” must name the transactional boundary and conditions, never cover arbitrary host effects.

Hibernation serializes codec-governed domain data, pending durable intent and versioned scheduling records. It does not serialize fibers, scopes, sockets, live service instances or Go/JS frames. Graceful passivation closes owned work before saving/handing over; crash recovery reconstructs data and reacquires services without assuming finalizers ran. Durable alarms/retries need identities and deduplication. Workflow replay additionally needs recorded nondeterminism, stable step identity, history bounds, codec/version migration and rejection of unrecorded effects; the existing service row alone cannot prove replay safety.

For native deployment, prefer versioned **process handover**: deploy compatible code, restore/migrate state, acquire a new fence, switch routing and retire the old owner. Specify incompatible codec/history refusal and rollback limits. This is not portable live-frame hot swapping. A BEAM backend would be a separate target research decision with different runtime/interop and artifact costs; it is unnecessary for the proposed local actor profile.

## Ordinary source surface

The ergonomics spike compared actual handler and receive-loop use. [Pinned typed handler behavior](https://github.com/rivet-dev/actors/blob/0247751bdafc82e72f3e4f52082fc2c1694a5c81/examples/hello-world-effect/src/actors/counter/live.ts) uses an ordinary wake factory returning protocol handlers, with construction capture and service requirements. [A queue-consumer loop](https://github.com/rivet-dev/actors/blob/0247751bdafc82e72f3e4f52082fc2c1694a5c81/examples/docs/actors-lifecycle/run-queue-consumer.ts) pulls messages and updates state through ordinary control flow. The [Effect-backed room example](https://github.com/rivet-dev/actors/blob/0247751bdafc82e72f3e4f52082fc2c1694a5c81/examples/chat-room-effect/src/actors/chat-room/live.ts#L94) explicitly comments out unimplemented message processing; that is not running receive-loop evidence. Pinned Effect Cluster [typed handlers and queue/replier construction](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/cluster/Entity.ts#L174) likewise offer distinct ordinary behaviors. Its queue adapter uses internal casts and separately correlated repliers; Effra must retain operation/reply identities through common checked interfaces rather than copy their erasure.

Cloudflare's [ordinary public-method RPC](https://developers.cloudflare.com/durable-objects/best-practices/create-durable-object-stubs-and-send-requests/) reinforces that addressable behavior does not need transitions. Current [Rivet queue documentation](https://rivet.dev/actors/docs/queues/) separates durable enqueue, receive-time removal and completion: completing resolves a waiting sender but does not make failed processing redeliverable. Effra must name admission, processing, reply cleanup and durable commit separately. These are inspected source/documentation claims, not executed upstream tests.

Illustrative only: generic actor APIs, protocol projections, immutable containers and ordinary loops below are unimplemented proposals. `RosterLive` is an ordinary implementation, not a machine. The initial sequential profile belongs to [the actor contract](../specs/actors.md).

```rust
service Roster {
    effect fn join(member: Member) -> Count raises { RoomFull }
}
effect fn roomExample() -> Count
    raises { RoomFull, MailboxFull, MessageTooLarge, Stopped }
{
    let room = run actors.spawn(RosterLive,
        ActorOptions { items: 128, bytes: 65536, replyBytes: 4096, policy: Sequential, stop: Discard })
    run room.client.join(Member { name: "Ada" })
}
```

The following deferred sketch illustrates pacing while a restricted inbox retains heterogeneous reply correlation. It does not by itself justify a receive API:

```ef
effect fn spoolLoop(inbox: Inbox<Spooler>) -> void uses { Clock } {
    while true {
        run inbox.handleNext()
        run Clock.sleep(5)
    }
}
```

This is not current `.ef` loop support or a new actor keyword. Initial loops must have empty expected failure rows. `handleNext` binds the constructed behavior and owns correlation/cleanup/reply; domain failures belong to the request. A useful receive profile must additionally demonstrate state carried in a loop or timed/source selection beyond handler pacing. Its general wait must be safe to cancel before dequeue, while actor-owned execution completes dispatch/cleanup/reply after dequeue; cancellation must never silently lose an admitted message. Controlled shutdown is a normal stopped exit after cleanup. Raw responder escape/double completion, split completion and multiple consumers remain unsupported until general affine/correlation evidence exists. Receive has its own deferred acceptance and does not block useful handler conformance.

The two unrelated acceptance callers are a room roster and a native device spooler. Compare them against an ordinary service protected by a fixed semaphore plus Ref/resource ownership. Negative controls must prove the actor's additional typed MailboxFull admission, caller cancellation detaching from admitted execution, stop/discard linearization and queue inspection. No machine state is fabricated. Initial messages/replies/domain failures are resource-free portable data; native pointers/slices/interfaces require a reviewed snapshot into domain data rather than shallow copying. Actor-owned host resources stay behind behavior; shared mutable DI remains a possible race. A final API derives full constructor, method, terminal and owner contracts without caller-written bookkeeping tuples. Supervision, durable entities and machine adapters wrap the same checked behavior through separate explicit capabilities. Examples are design sketches, not checked programs or completed API support.

## When it earns its cost

Use local actors when a room, session, agent or job has a meaningful identity, typed interaction, explicit concurrency, owned work and lifecycle inspection. Choose machine behavior when transitions and entry lifetimes add useful guarantees. Add supervision when rebuilding that activity from a fresh factory is safe and useful. Add durable entities when the identity/progress must survive a process and routing to one authority simplifies correctness. Add workflows when long-lived multi-step progress, approval or timed recovery needs replayable history.

Keep stateless HTTP handlers ordinary when each request already has an independent scope and a database transaction expresses its state change. An actor for every request adds routing/lifetime machinery without an owner that persists beyond it. High contention across many keys, global invariants and unbounded hot mailboxes can make actor partitioning the wrong boundary. Require two unrelated consumers and explicit operational tradeoffs before generalizing.

## Phased plan and executable proof obligations

| Phase | Deliverable | Evidence required before claiming support |
| --- | --- | --- |
| Ordinary local actor profile | Service/impl handlers on Go and JS; receive has a separate deferred closure | Portable payload/capture negatives, reply correlation, explicit admission versus reply, serialized effects/cleanup, cancel/stop/lifecycle and full inspection controls against semaphore-plus-Ref composition |
| Machine specialization | Checked local machine behavior on the shared actor core | Its FIFO, stale-entry, self-wait, saturation, entry cleanup, terminal failure-row and transition inspection acceptance corpus |
| Local supervision | Modular supervisor over fresh factories | Causal defect/expected-failure classification; full composite Cause; fresh acquisitions; cleanup-before-restart; stop/backoff races; budget escalation; dependency restart ordering; no implicit detached work |
| Durable single-host provider | Typed address, versioned codec and transactional inbox/state/reply/outbox | Kill between admission/state/reply/outbox stages; duplicate deliveries; ack-after-durable-commit; crash without finalizers; interrupted callers; restart/migration refusal; external idempotency controls |
| Addressed multi-host provider | Leases/routing/fencing behind explicit deployment/storage interfaces | Paused old owner, takeover and late writes; same-address new incarnation; partition/ambiguous acknowledgements; enforced external fence where claimed; restart supervisor; provider contract qualification |
| Workflow library | Explicit recorded steps and versioned history | Replay after every boundary; unrecorded-effect refusal; duplicate side effects; changed histories and codec/version migration; bounded retention; human waits and cancellation semantics |

Do not build the distributed framework in the current batch. Before each phase, define its finite admitted contract and compare actual native/JS tests to pinned reference behavior. Retain unsupported/different rows. Measure compile/emission/runtime and binary retention with matched fixtures only in the scheduled final benchmark phase: unused actor, persistence and deployment modules must stay out of minimal application binaries. No research-source inspection counts as one of the copied 746 upstream tests passing.

## Inspection and action capabilities

Design inference: expose one canonical graph of protocols, behavior/factories, owners, optional supervisors, dependents and complete failure/service contracts. Runtime snapshots add address/incarnation, lifecycle/restart status, mailbox item/byte/in-flight usage and bounded Cause/restart history. Entry epochs/state/transition/stale-completion facts occur only for machine behavior. Durable providers add storage version, pending intents and acknowledged progress with freshness limits. Payloads and history inputs/outputs require opt-in redacted codecs, cardinality/byte budgets and authorization; never expose service internals by default.

Inspection is read-only and cannot invoke a step, restart an actor, modify state or replay an effect. Separate typed actor-action capabilities authorize those mutations explicitly and record their actor/revision identity. Static possible transitions, observed transitions and current committed state remain distinct; none proves liveness or universal deadlock freedom.

## Research limits

Official web docs were read on 2026-10-06; their contents can change. Effect and selected Rivet source anchors are immutable pins above. No celld engine or BEAM VM source audit, deployment, crash experiment, benchmark, package installation or Effra implementation was performed. No product or HITL ticket is closed by this proposal. The publisher lane remains frozen and untouched.
