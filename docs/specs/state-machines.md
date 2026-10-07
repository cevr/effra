# Checked state machines

A machine supplies one form of [ordinary actor behavior](actors.md). Actors also support service handlers and receive loops without machine transitions. Reuse the shared owned mailbox/control/runtime core; this spec adds checked state/event decisions, entry lifetimes and work-completion identity. Supervision, durable addressing and persistence apply separately to either behavior.

Status: authorized implementation extension from owner direction on 2026-10-06; not current language support. Preserve Go-like simplicity, Effect lifetime guarantees and ordinary ADTs. Main machine prior art is [XState v6 PR5543](https://github.com/statelyai/xstate/pull/5543), next revision 2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5. Effect Machine supplies additional lifetime evidence; v5 is historical only. Independent source/test counsel supports this ordinary-function surface.

## Ordinary behavior, checked declaration

States and external events are nominal enums with payloads. Named functions perform step selection and completion handling using ordinary match and if expressions; they may be pure `fn` or `effect fn` with explicit failure and service rows (owner clarification, 2026-10-06). An entry function returns optional deferred work. One declaration binds these names and the initial state, giving the canonical plan a stable identity. One library runtime owns admission, entry, invocation and shutdown. No separate transition/guard/error-handling sub-language is needed.

General prerequisites are typed function/recipe values, finite rows, reusable generic enums and multi-subject/or-pattern matching. These serve other libraries too. The first profile permits one work recipe per entry and requires expected work failures to be explicitly recovered into a distinct outcome enum. Step and completion functions may declare expected failures directly or recover them into ordinary Step decisions. The union of step, completion and work service requirements is charged at spawn, with contribution paths retained; public spawning functions declare their own requirements. Step/completion failure rows remain part of the actor's terminal contract and the waiting call/observation contract. Immediate send reports admission only; it cannot pretend later execution has succeeded. Defects and interruption retain full Cause rather than becoming expected failures.

Illustrative, unimplemented syntax:

~~~rust
enum SessionState { Idle  Active { key: string } }
enum SessionEvent { Open { key: string } Close }

fn sessionStep(state: SessionState, event: SessionEvent) -> Step<SessionState, void> {
    match state, event {
        SessionState.Idle, SessionEvent.Open { key } => Step.Go(SessionState.Active { key })
        SessionState.Idle, SessionEvent.Close => Step.Ignore
        SessionState.Active { key: previous }, SessionEvent.Open { key } =>
            if key == previous { Step.Ignore } else { Step.Go(SessionState.Active { key }) }
        SessionState.Active { key }, SessionEvent.Close => Step.Go(SessionState.Idle)
    }
}

machine Session {
    initial SessionState.Idle
    step sessionStep
    enter sessionEnter
    complete sessionComplete
}
~~~

The other named functions have ordinary explicit signatures: enter is a pure fn taking State and returning an optional deferred Outcome recipe; complete takes State and Outcome and returns Step<State, Output>. All entry effects execute in that returned recipe under the entry scope. The declaration checks these against step and initial. Definition constructs data; spawning starts managed work. Outcome and Event are different types, so public send cannot forge invocation completion. Constructing a recipe is distinct from executing it.

The same step interface admits effectful decisions without a second machine API. For example, an ordinary service call can validate an event before choosing the next state:

~~~rust
error AccessUnavailable

service Access {
    effect fn mayOpen(key: string) -> bool raises {AccessUnavailable}
}

effect fn sessionStep(state: SessionState, event: SessionEvent)
    -> Step<SessionState, void> raises {AccessUnavailable} uses {Access} {
    match state, event {
        SessionState.Idle, SessionEvent.Open { key } =>
            if run Access.mayOpen(key) {
                Step.Go(SessionState.Active { key })
            } else { Step.Reject }
        SessionState.Active { key: previous }, SessionEvent.Open { key } =>
            if run Access.mayOpen(key) {
                if key == previous { Step.Stay(state) }
                else { Step.Go(SessionState.Active { key }) }
            } else { Step.Reject }
        SessionState.Idle, SessionEvent.Close => Step.Ignore
        SessionState.Active { key }, SessionEvent.Close => Step.Go(SessionState.Idle)
    }
}
~~~

This alternative replaces the pure sessionStep above; it is not a duplicate declaration in one source file. A pure function retains the normal purity check. An effect function explicitly executes service work with run; the runtime executes that function's returned recipe once for the admitted event.

## Explicit transition outcomes

The reusable bundled enum Step<State, Output> makes lifecycle intent explicit:

- Go(next) always closes the current entry and starts a new one, even for the same tag.
- Stay(next) updates data while retaining the entry, timers and captured work input. Its tag must stay the same: reject visible violations statically and enforce dynamic values with a defined runtime defect and completed cleanup.
- Ignore acknowledges without a state change. Reject leaves state unchanged and reports a typed rejection to a waiting caller.
- Done(output) publishes terminal output only after cleanup.

Every state/event pair has a policy through ordinary exhaustive matching. Named variant or-patterns reduce repetition while preserving missing-pair diagnostics when either enum grows. Alternatives must bind the same names/types. Catch-all arms remain outside the initial closed-data policy. Ordinary unreachable-arm and type diagnostics handle duplicates and bad payloads. Bound product checking and plan construction rather than enumerating histories or exponentially expanding shared paths.

Conditions are ordinary total if/else expressions. A pure step/complete cannot execute effects; an effectful one may perform explicit run calls, including calls used to decide a transition. Requirements and failures survive helpers, aliases and provider capture through the common function/row representation. A capability query does not execute hidden work or rerun an effectful decision to discover whether an event is enabled.

Program-time delays are ordinary Clock/Scheduler work returning an outcome; no machine timer syntax is needed. The first profile is flat states and one work per entry. Hierarchy, parallel regions, history, eventless stabilization, supervision, postponement, multiple simultaneous invocations and durable replay remain unsupported. Unknown syntax diagnoses normally.

## Ownership and publication

One managed actor fiber owns its mailbox and state-entry scope. Each entry has a distinct epoch. Invocation outcomes carry actor generation, entry epoch and work identity; validate them at dequeue, including exit then re-entry into the same tag. Discard stale outcomes and count them in bounded inspection. Cancellation does not replace this check: a result may already be queued. A foreign completion adapter also carries the epoch; fencing cannot undo an external side effect.

Step and completion evaluations are serialized, including their effects, in a temporary scope directly owned by the actor, never by the current entry. At most one evaluation is in flight. The current entry stays owned while a decision is evaluated; external events and work completions remain queued under the spawn budgets. A successful evaluation first closes its own scope, interrupting unjoined children and awaiting finalizers. Go and Done then close the old entry. Only successful completed cleanup permits commit and publication, followed by new entry work when applicable. Expected child interruption at scope close is not a fabricated failure. Work that must outlive the decision belongs in enter's recipe. Failure leaves the last stable state uncommitted, stops the actor after cleanup, and preserves the declared failure plus any cleanup cause. An application that wants to continue after a business failure explicitly recovers to Reject, Stay or another Step. No hidden retries or evaluation for inspection are allowed.

Stop cancels an in-flight evaluation and waits for its children/finalizers and the entry's cleanup; it prevents a subsequent commit. Stop does not drain: discard and count queued events/completions and complete their pending calls with Stopped. Drain is unsupported in the first profile. Commit and stop admission are serialized: Done committed first remains the single output observed by all waiters; stop admitted first prevents that commit. Stop during protected cleanup waits for cleanup and prevents new entry work. Cancelling a call waiter does not retract an already admitted event or abandon actor-owned work. State/output values cannot carry resources owned by the completed evaluation or a closing entry; Outcome values cannot carry entry-owned resources beyond that entry. Use the common ownership checker. State commit ordering does not roll back external writes performed by an effectful step. Transactions, idempotency and compensation remain explicit application/service contracts.

Go invalidates the old entry, requests cancellation and awaits its children/finalizers before publishing the next stable state and starting its work. Stay preserves entry identity. Completion of an invocation does not by itself release resources owned for the whole entry. Cleanup defects fail the pending acknowledgement and stop the actor after cleanup, preserving composite causes. Expected cancellation of replaced work is not a newly fabricated domain failure. Final output and stop completion follow actor cleanup; concurrent stop callers observe one completed exit, and interrupting a waiter does not abandon cleanup.

Spawn requires explicit mailbox item/byte budgets, chosen as for [any actor](actors.md#proposed-library-surface): finite, or an explicit `unbounded` that the `no-unbounded-mailbox` lint asks the owner to justify. First-profile send admits immediately or fails with MailboxFull/Stopped; it never waits for queue capacity. Call uses that admission policy, then interruptibly awaits the committed transition, including completed old-entry cleanup and admission of new work; acknowledgement does not wait for the work's eventual outcome. Reject reports a typed Rejected failure.

The shared actor adapter also requires explicit payload/result snapshot limits, chosen as for [any actor](actors.md#proposed-library-surface): finite, or an explicit `unbounded` that the `no-unbounded-mailbox` lint asks the owner to justify. MessageTooLarge is an explicit admission failure for send/call; an oversized final Output stops with terminal MessageTooLarge rather than publishing success. That fixed terminal failure is charged at spawn and included in result observation, alongside the declared step/completion row. Payload/Output admission follows the shared portable-data and closed-owner checks, not host shallow copying.

The machine adapter produces `Actor<Session>` for a named Session machine: the machine declaration contributes its canonical event/request protocol, rather than treating Event as a service. Public send/call/result types and rows follow from that declaration, including through an annotated handle parameter. Event/Outcome/Output identities remain canonical; entry/transition policy and lifetime provenance are additional checked evidence, not requirements on ordinary service actors. Common awaitExit returns a P-independent bounded portable exit snapshot, preserving cause structure with safe snapshots/redacted tags and explicit truncation, never raw factory-typed or closed-owner payloads. Internal Cause remains complete. Separate machine result observation returns Output or raises the declaration's terminal row/Stopped; other terminal causes become Stopped with bounded portable cause data. Inspectable contracts are derived from the named declaration rather than caller-written bookkeeping parameters:

| Operation | Expected failures and requirements |
| --- | --- |
| Send | MailboxFull, MessageTooLarge or Stopped; admission carries no later step failures. |
| Call | MailboxFull, MessageTooLarge, Stopped, Rejected and the step's declared failures. Only the originating event's call receives its step failure; other queued calls receive Stopped with inspectable terminal cause data. |
| Result observation | The union of step/completion failures, fixed terminal MessageTooLarge and Stopped; successful completion returns Output. Completion failures have no originating event call. Other terminal causes become Stopped with portable cause data. Common awaitExit instead returns the P-independent completed exit snapshot. Factory-specific typed recovery stays at the spawn-site row. |
| Spawn | Constructor and step/completion/work service requirements; constructor/constructor-child failures and step/completion/fixed MessageTooLarge terminal failures are conservatively charged, following the fork rule. Unobserved terminal Cause propagates at owner closure; observing it does not erase the charged row. |

Defects, interruption and composite causes retain the runtime cause structure through every operation. A channel's expected-failure row must not flatten a cleanup defect or reattribute another event's domain failure.

One work per entry reserves one bounded completion slot at entry start; it and external events use a common arrival sequence. A completion arriving during evaluation waits in the slot: Go/Done invalidates its epoch, while Stay preserves it and arrival order. Stop has a separate control signal. Saturation cannot prevent completion or shutdown. No actor handle is automatically supplied to work, but aliases may still reach it. SelfCall uses the shared blocking-dependency rule: reject a self-call from an in-flight step/completion evaluation, its descendants or cleanup, not actor identity alone. Independent entry work may call its own actor; a Go/Done decision closes the old entry and interrupts its waiter, which detaches without retracting the admitted event. Self-send through aliases remains nonblocking and never recursively processes a transition. A nonblocking stop request is allowed; any actor-owned execution, including entry work, awaiting its own actor exit produces the defined self-wait defect. Cross-actor waits may form cycles and remain outside the first profile's deadlock guarantee. Propagating a call chain alone cannot detect independently initiated mutual waits; do not advertise it as general cycle prevention. Ordinary explicit timeouts and cancellation remain available, with their normal rows and cleanup contract.

Tests use causal signals and repaired program-time scheduling; the independent watchdog remains wall clock. Unobservable foreign blocking remains an explicit deterministic-testing limitation.

## Inspection and boundaries

CLI/MCP project one canonical plan: source revision; State/Event/Outcome identity; initial state; function contracts and owners; possible Go/Stay/Ignore/Reject/Done edges; and terminal output. A bounded local walk identifies literal constructor targets; helper/dynamic results produce a conservative target set labeled as such. Never pretend this graph proves reachability, liveness or current enabledness. General product-match checking must be bounded without expanding all histories or shared type paths.

Runtime snapshots expose actor/entry identity, state tag, queue usage, lifecycle status (including an in-flight step/completion) and bounded causes/counters. They return the last committed state without waiting for application evaluation. The plan labels pure versus effectful functions, terminal-on-failure rows and directional contribution paths. Locally identified edges beneath an effectful condition carry that condition's contribution path; dynamic helper destinations remain conservative. Inspection never executes behavior, including pure steps; there is no enabledness query in the first profile. Payload exposure requires an explicit codec. Static possible edges, active runtime state and observed completed transitions are distinct.

Optional state snapshots use a named versioned codec and explicit restart/recovery policy. Scopes, fibers, service instances and in-flight host work are not serialized domain state. Starting from stored state invokes entry work again; idempotency, transactional admission, outbox and crash recovery remain separate obligations. No durable or exactly-once guarantee is implied.

## Acceptance and delivery

1. Land reviewed lifetime/scheduler foundations and ordinary function/row/generic data mechanisms. No opaque callback shortcut.
2. Implement product/or-pattern matching and the declaration with two unrelated examples: search/reload and timed lease/session or circuit breaker. Cover both pure and effectful step/completion functions. Isolate missing-pair, duplicate-arm, binding-consistency, invalid signature/payload, outcome/event confusion, invalid Stay, undeclared failure and missing-service diagnostics. Reject effect execution only in functions declared pure; preserve effectful function rows through helpers and actor observations. Compare literal edges and conservative helper targets through CLI/MCP; measure frontend/emission and adversarial checking costs.
3. Execute shared plans on Go/JS. Test FIFO and aliased self-send/call/self-wait, including children and cleanup; queue/completion saturation; exact evaluation-cleanup then entry-cleanup then publication order; Go versus Stay timing; already-queued stale completion; late foreign callback; full composite causes; concurrent stop; interrupted stop waiter; and terminal output after cleanup. Effectful-step cases must prove one evaluation at a time, at most one invocation per admitted event and exactly one when it is dequeued for evaluation while Running, no state publication before evaluation cleanup, originating-call versus queued-call failure attribution, completion-only terminal failures, explicit recovery versus terminal typed failure, stop during suspended evaluation/protected cleanup, queued-event discard counts, Done/stop linearization, preserved external writes on failure, and no effect execution or evaluation wait by graph/snapshot queries. Reject an effectful enter and escaping entry-owned Outcome values.
4. Exercise request-owned HTTP cancellation and a non-server actor through the same library. Map pinned v6/Effect Machine behaviors to passing/different/unsupported cases. Run full gates, native race checks, frontend/emission measurements and independent review before advertising support.

Decided by **Go-like local readability**, **Redesign From First Principles** and **Never Block on the Human**: one small declaration, ordinary functions for behavior and reusable bundled decisions. This replaces the earlier nested machine syntax sketch and does not close historical Wayfinder HITL tickets.
