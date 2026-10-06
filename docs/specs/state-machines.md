# Checked state machines

Status: authorized implementation extension from owner direction on 2026-10-06; not current language support. Preserve Go-like simplicity, Effect lifetime guarantees and ordinary ADTs. Main machine prior art is [XState v6 PR5543](https://github.com/statelyai/xstate/pull/5543), next revision 2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5. Effect Machine supplies additional lifetime evidence; v5 is historical only. Independent source/test counsel supports this ordinary-function surface.

## Ordinary behavior, checked declaration

States and external events are nominal enums with payloads. Named functions perform step selection and completion handling using ordinary match and if expressions; they may be pure `fn` or `effect fn` with explicit failure and service rows (owner clarification, 2026-10-06). An entry function returns optional deferred work. One declaration binds these names and the initial state, giving the canonical plan a stable identity. One library runtime owns admission, entry, invocation and shutdown. No separate transition/guard/error-handling sub-language is needed.

General prerequisites are typed function/recipe values, finite rows, reusable generic enums and multi-subject/or-pattern matching. These serve other libraries too. The first profile permits one work recipe per entry and requires expected work failures to be explicitly recovered into a distinct outcome enum. Step and completion functions may declare expected failures directly or recover them into ordinary Step decisions. The union of step, completion and work service requirements is charged at spawn, with contribution paths retained; public spawning functions declare their own requirements. Step/completion failure rows remain part of the actor's terminal contract and the waiting call/observation contract. Immediate send reports admission only; it cannot pretend later execution has succeeded. Defects and interruption retain full Cause rather than becoming expected failures.

Illustrative, unimplemented syntax:

~~~rust
enum SessionState { Idle  Active { key: string } }
enum SessionEvent { Open { key: string } Close }

fn sessionStep(state: SessionState, event: SessionEvent) -> Step<SessionState, ()> {
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

The other named functions have ordinary explicit signatures: enter takes State and returns an optional deferred Outcome recipe; complete takes State and Outcome and returns Step<State, Output>. The declaration checks these against step and initial. Definition constructs data; spawning starts managed work. Outcome and Event are different types, so public send cannot forge invocation completion. Constructing a recipe is distinct from executing it.

The same step interface admits effectful decisions without a second machine API. For example, an ordinary service call can validate an event before choosing the next state:

~~~rust
error AccessUnavailable

service Access {
    effect fn mayOpen(key: string) -> bool raises {AccessUnavailable}
}

effect fn sessionStep(state: SessionState, event: SessionEvent)
    -> Step<SessionState, ()> raises {AccessUnavailable} uses {Access} {
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

Step and completion evaluations are serialized, including their effects, in a temporary scope owned by the actor. At most one evaluation is in flight. The current entry stays owned while a decision is evaluated; external events and work completions remain bounded and queued. Only a successful decision followed by completed evaluation cleanup may commit. Failure leaves the last stable state uncommitted, stops the actor after cleanup, and preserves the declared failure plus any cleanup cause for observers and pending calls. An application that wants to continue after a business failure explicitly recovers to Reject, Stay or another Step. No hidden retries or evaluation for inspection are allowed.

Stop cancels an in-flight evaluation and waits for its children/finalizers and the entry's cleanup; it prevents a subsequent commit. Cancelling a call waiter does not retract an already admitted event or abandon actor-owned work. State/output values cannot carry resources owned by the completed evaluation or a closing entry; use the common ownership checker. State commit ordering does not roll back external writes performed by an effectful step. Transactions, idempotency and compensation remain explicit application/service contracts.

Go invalidates the old entry, requests cancellation and awaits its children/finalizers before publishing the next stable state and starting its work. Stay preserves entry identity. Completion of an invocation does not by itself release resources owned for the whole entry. Cleanup defects fail the pending acknowledgement and stop the actor after cleanup, preserving composite causes. Expected cancellation of replaced work is not a newly fabricated domain failure. Final output and stop completion follow actor cleanup; concurrent stop callers observe one completed exit, and interrupting a waiter does not abandon cleanup.

Spawn requires explicit mailbox item/byte budgets. First-profile send admits immediately or fails with MailboxFull/Stopped; it never waits for queue capacity. Call uses that admission policy, then interruptibly awaits the committed transition, including completed old-entry cleanup and admission of new work; acknowledgement does not wait for the work's eventual outcome. Reject reports a typed Rejected failure.

One work per entry gives one bounded completion slot; it and external events use a common arrival sequence. Stop has a separate control signal. Saturation cannot prevent completion or shutdown. No actor handle is automatically supplied to work, but aliases may still reach it: execution carries actor identity so an aliased self-call, including during cleanup, fails with a defined SelfCall defect instead of deadlocking. Self-send through aliases remains nonblocking and never recursively processes a transition.

Tests use causal signals and repaired program-time scheduling; the independent watchdog remains wall clock. Unobservable foreign blocking remains an explicit deterministic-testing limitation.

## Inspection and boundaries

CLI/MCP project one canonical plan: source revision; State/Event/Outcome identity; initial state; function contracts and owners; possible Go/Stay/Ignore/Reject/Done edges; and terminal output. A bounded local walk identifies literal constructor targets; helper/dynamic results produce a conservative target set labeled as such. Never pretend this graph proves reachability, liveness or current enabledness. General product-match checking must be bounded without expanding all histories or shared type paths.

Runtime snapshots expose actor/entry identity, state tag, queue usage, lifecycle status (including an in-flight step/completion) and bounded causes/counters. The plan labels pure versus effectful functions and their directional contribution paths; it must not claim an effectful transition is currently enabled without executing application work. Payload exposure requires an explicit codec. Static possible edges, active runtime state and observed completed transitions are distinct.

Optional state snapshots use a named versioned codec and explicit restart/recovery policy. Scopes, fibers, service instances and in-flight host work are not serialized domain state. Starting from stored state invokes entry work again; idempotency, transactional admission, outbox and crash recovery remain separate obligations. No durable or exactly-once guarantee is implied.

## Acceptance and delivery

1. Land reviewed lifetime/scheduler foundations and ordinary function/row/generic data mechanisms. No opaque callback shortcut.
2. Implement product/or-pattern matching and the declaration with two unrelated examples: search/reload and timed lease/session or circuit breaker. Cover both pure and effectful step/completion functions. Isolate missing-pair, duplicate-arm, binding-consistency, invalid signature/payload, outcome/event confusion, invalid Stay, undeclared failure and missing-service diagnostics. Reject effect execution only in functions declared pure; preserve effectful function rows through helpers and actor observations. Compare literal edges and conservative helper targets through CLI/MCP; measure frontend/emission and adversarial checking costs.
3. Execute shared plans on Go/JS. Test FIFO and aliased self-send/call; queue/completion saturation; exact cleanup-before-entry/publication order; Go versus Stay timing; already-queued stale completion; late foreign callback; full composite causes; concurrent stop; interrupted stop waiter; and terminal output after cleanup. Effectful-step cases must prove one evaluation at a time, exactly one invocation per admitted event, no state publication before evaluation cleanup, explicit recovery versus terminal typed failure, stop during suspended evaluation, no commit after stop, preserved external writes on failure, and no effect execution by graph/snapshot queries.
4. Exercise request-owned HTTP cancellation and a non-server actor through the same library. Map pinned v6/Effect Machine behaviors to passing/different/unsupported cases. Run full gates, native race checks, frontend/emission measurements and independent review before advertising support.

Decided by **Go-like local readability**, **Redesign From First Principles** and **Never Block on the Human**: one small declaration, ordinary functions for behavior and reusable bundled decisions. This replaces the earlier nested machine syntax sketch and does not close historical Wayfinder HITL tickets.
