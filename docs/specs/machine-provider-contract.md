# Runtime-independent machine plans and providers

> **Status:** describes the retired `.ef` language, removed 2026-10-28 by [#205](https://github.com/cevr/ergo/issues/205); kept as design reference. The [retirement record](../receipts/retire-ef.md) lists what was removed.

> **Go is the only target (2026-10-26).** The owner removed the JavaScript target ("we're removing the js interop id say"; [north star](../../NORTH_STAR.md#go-is-the-only-target-owner-2026-10-26)). Clauses below that name JavaScript, TypeScript declarations, Bun, Node or a pinned Effect runtime or ABI record the earlier two-target contract. They are not requirements or acceptance targets; Effect stays prior art for the guarantees.

Status: owner-directed design, 2026-10-08; the contract is specified here but is not implemented language or runtime support. A [draft plan interface](#draft-plan-interface-2026-10-09) against the checked plan was added on 2026-10-09. The machine declaration and lifecycle laws remain in [checked state machines](state-machines.md). This document defines the boundary between their checked plan and an ordinary userland runtime provider.

## Boundary

A checked machine declaration lowers to a provider-independent **machine plan**. The plan is a finite, source-revisioned value containing the nominal State, Event, Outcome and Output identities; the initial state; named initial, step, entry and completion callable values; their success, failure and service rows; possible Go/Stay/Ignore/Reject/Done edges; entry and work identity rules; and the inspection facts needed by CLI/MCP. The plan carries checked callable values and ownership evidence, not compiler AST access or a hidden execution engine.

Checking owns declaration validity, nominal identity, exhaustive finite decisions, public rows, source locations and the plan's revision. Lowering owns the representation of those facts for a target. A provider owns execution, admission, scopes, cleanup, scheduling and runtime observations. Inspection reads the plan or a runtime snapshot and never executes a step, entry, completion or provider callback.

The plan must remain usable when the provider changes. Provider selection is an ordinary library, function or service operation: the compiler emits the plan and never recognises a provider by identity or name (owner direction 2026-10-09). This is not analogous to `.catch` and `.provide`, which are compiler-known postfix constructs that stay syntax ([audit](../research/compiler-known-constructs-audit.md#key-findings)). The compiler and parser do not dispatch to a default machine engine, and an unsupported provider or binding is a normal checked diagnostic. A provider cannot change the plan's nominal identities, declared rows, transition decisions or source revision while executing it.

## Typed provider contract

The exact public API remains implementation work. Its finite boundary must expose only the facts and callable values required by a provider:

- plan identity and source revision;
- nominal state, event, outcome and output identities, with initial value and checked transition edges;
- typed pure or effectful step, entry and completion values, including their declared failures, services and ownership/lifetime evidence;
- entry-epoch and work identity rules, terminal output and inspection projection; and
- target/profile constraints and provider obligations.

Provider construction and execution retain ordinary service requirements and failure rows. A provider does not introspect private compiler structures, invoke hidden builtin hooks, erase `R`/requirements, or accept unchecked JavaScript passthrough. It may reject an incompatible identity, missing requirement, unsupported binding or contract-erasing adapter through the normal language diagnostics. An adapter that changes a callable's ownership, failures, services or output shape must be visible at its own typed boundary.

Ergo's first provider will use the shared owned actor core: bounded item/byte admission, reserved completion and stop controls, serialized evaluation, entry-epoch fencing, cleanup before state/output commit, and full Cause preservation. This is the intended execution boundary for the first provider; it does not claim that provider implementation is delivered, make the actor core a machine-only runtime or require every actor to have machine transitions. A second provider must consume the same checked plan through the same public facts and retain tested, refuted or unresolved evidence for its own runtime obligations under the [lawful runtime contract](../research/lawful-runtime-contract.md), never a proof claim.

## Draft plan interface (2026-10-09)

Status: draft. Nothing in this section is implemented, and no runtime exists. It fixes the shape of the boundary against the checked plan that [the declaration checker now publishes](state-machines.md#implementation-status-2026-10-09). Sketches use ordinary Ergo spelling, but they are not admitted syntax or library surface until plan lowering lands.

### Two layers of plan

A provider receives two layers. Neither layer gives access to the compiler.

1. **Plan facts.** These are compiler-proved data, identical to the `machines` entry of `project.check` and `ergo inspect`, and they are portable values:
   - `id` and `revision`;
   - the `state`, `event` and `output` identities and their variants;
   - `initial`;
   - `functions`: role, identity, `pure` or `effect`, failures and services;
   - `transitions`: from-set, trigger-set, decision, `literal`, `current` or `conservative` targets, guard flag, helper chain and span;
   - `states` with `initial` and `enterable`;
   - `terminalFailures`, `requirements`, `terminal`, `evidence` and `limitations`.

   A provider may read these facts to build tables, to refuse a plan it cannot run or to label snapshots. It must not treat them as a schedule. `conservative` and `any` mean that only executing the step decides.
2. **Plan values.** These are the typed callable values and the initial value that execution needs. The lowered value has a bundled generic type whose parameters the declaration instantiates. The type covers the admitted step profile:

   ```
   // sketch: bundled provider surface (module not chosen), not yet admitted
   record Plan<S: type, E: type, O: type, F: raises, R: uses> {
       facts: PlanFacts
       initial: S
       step: effect fn(S, E) -> decision<S, O> raises { F } uses { R }
   }
   ```

   A pure step is the same callable with empty rows, so one provider serves both kinds. When the `enter` binding is admitted (#19, #52, #11 and #47), `Plan` gains `Outcome`, the entry recipe and `complete` with their own rows. That is a versioned change to the bundled type, never a silent widening.

### Why explicit parameters, and who writes them

The decision a step returns is a closed protocol that every provider matches as data and never extends. It is decision syntax: the predeclared `decision<S, O>`, written `enter e`, `stay e`, `ignore`, `reject` and `done e` and matched with the same words. Plan transition kinds are the same words, `enter`, `stay`, `ignore`, `reject` and `done`, plus the conservative `any`, so a provider reads one vocabulary in source, plan and graph. [Decisions](state-machines.md#decisions) records the syntax and its migration from the bundled `Step` enum, and the [closed-protocol notes](state-machines.md#closed-protocol) record where provider-specific behaviour goes.


Provider code is generic library code. It must preserve each machine's rows without whole-program inference, so it takes the finite type and row parameters that the [language abstraction contract](language-abstractions.md) already provides, and solves them from the plan argument at a direct call. Authors of machines and their callers never write those parameters. The declaration name, used where a `Plan` is expected, lowers to its plan value, just as a function name is a function value. Until lowering lands, that use is EF141 rather than a passthrough. `Actor<Session>` stays the caller-facing handle that names the declaration (#11). This separates two jobs. The spike's six-argument structural handle was rejected as a caller's type, and it survives only as the provider's internal signature.

A provider is an ordinary service, so it is selected and provided like any other:

```
// sketch
service MachineRuntime {
    effect fn start<S: type, E: type, O: type, F: raises, R: uses>(plan: Plan<S, E, O, F, R>) -> Running<S, E, O, F>
        raises { MachineStartFailure }
        uses { R }
}
```

`uses { R }` is the spawn charge: starting a machine requires the step's services wherever the provider runs it. `F` reaches `Running` so that terminal failures arrive at waiting observations rather than being erased. The operation names only row parameters, as service operations must; the provider's own construction requirements belong to its `impl ... uses { ... }`.

### Provider obligations

Each obligation reuses the [lawful runtime](../research/lawful-runtime-contract.md) vocabulary, and each provider and target records an assurance category for it. A machine-specific law vocabulary is not introduced.

| Obligation | Rows it rests on | First provider expectation |
| --- | --- | --- |
| Starting a provider or plan runs no step (construction is inert) | R11 | Tested law |
| Owned actor: the provider's scope owns every evaluation and entry, and closing the scope stops admission and awaits cleanup | P4, P6, R2, R9, R10 | Tested law |
| Serial evaluation: at most one step at a time, at most one invocation per admitted event, exactly one per event dequeued while running | P2, P6, P7 | Tested law |
| Bounded admission and reserved stop/completion capacity | P9 (RS2 public wait), R10 | Unresolved until RS2 |
| Cleanup before commit: evaluation and entry cleanup finish before a state or output is published | P4, P5, R1, R2, R5 | Tested law |
| `enter` re-enters a new epoch, `stay` keeps the entry, and the runtime defect fires when a dynamic `stay` changes the tag | P1 (defect), R15 | Tested law |
| Entry-epoch fencing: a stale completion is discarded | P8, R4 | Unresolved until entry work is admitted |
| Exact causes: typed failures, defects and cleanup failures keep their identity, and a terminal failure reaches waiters | P1, P3, R3, R6 | Tested law |
| Inspection reads plan facts and the last committed snapshot, and never waits for or runs evaluation | (plan facts only) | Nominally checked |

A refuted obligation refuses the provider for that target. An unresolved one stays visible in inspection and receipts with its justifier. A provider can never upgrade a compiler fact. Exhaustive decisions, the static `stay` rule and declared rows are compiler-proved and are not provider claims.

### Negative controls

These hold once lowering lands, all through ordinary diagnostics:

- *Missing requirement.* Starting a plan whose step uses `Probe` without `Probe` provided is the ordinary missing-service diagnostic at the `start` call.
- *Incompatible identity.* Passing `Plan<Breaker, Signal, string, …>` where a provider or caller expects `Plan<Query, QueryEvent, i64, …>` is an ordinary type mismatch. The plan's identity is nominal, never structural over variant names.
- *Unsupported binding.* A provider that declares it supports only the step profile refuses a plan whose facts include `enter` with a typed `MachineStartFailure`, never by ignoring the binding. Today the checker refuses `enter` first (EF141).
- *Contract-erasing provider.* An implementation of `start` whose result drops `F`, or whose `uses` omits `R`, fails conformance against the operation (EF104 or the row diagnostics). An adapter that changes rows must be visible at its own typed boundary.
- *Forged plan.* Building a `Plan` record by hand is refused, because only lowering of a checked declaration constructs one: `facts.evidence` is `checked-static-machine-plan`, and the record is opaque to user construction. This rests on the [opaque-value evidence](../research/opaque-values-and-evidence.md) work. Until that work lands, this control is unresolved and the draft must not ship.

### Against the `gen_statem` engine

The engine loop in `gen_statem` (OTP `516126e9`, `lib/stdlib/src/gen_statem.erl`, the *state transition* sequence) is the closest existing provider. It shows which duties belong to the provider and which this contract moves elsewhere:

- *Serial evaluation and queueing* are the engine's in `gen_statem`: one process, one callback at a time, `next_event` insertion ahead of the queue. The provider owns these here too, plus bounded admission, which `gen_statem` leaves to the mailbox.
- *State change detection* is a runtime term comparison in `gen_statem`. Here the plan's decision says it, `enter` or `stay`, and the static `stay` rule is a compiler fact the provider must not re-derive. A provider still owes the runtime defect for a dynamic `stay` that changes the tag.
- *State enter calls* run after a state change and may not change state (`enter_action()`). Entry work here is the same shape, but the provider also owns the entry's scope and cleans it up before committing the next state. `gen_statem` has no per-state ownership, so this obligation is new.
- *Postpone and time-outs* (`postpone`, `event_timeout`, `state_timeout` and generic time-outs) are engine features in `gen_statem`. They are outside the first profile. When added, a state time-out should be entry-owned work, so the entry epoch cancels it, rather than a provider-private timer that bypasses the plan.
- *Termination* in `gen_statem` is `{stop, Reason}` plus `terminate/3`. Here it is `done output` after cleanup, or a terminal typed failure that waiting observations receive, and a provider must keep the complete cause.

### Prior art

- *XState v6.* `ActorLogic` with `transition`/`getInitialSnapshot` and `createActor(logic)` separates logic from the runtime with many type parameters that users rarely write. Ergo's split is the same, except that the decision is a checked `decision<S, O>` value rather than a snapshot.
- *Erlang gen_statem.* A callback module supplies `init` and the state functions, the `gen_statem` behaviour owns the process, and `keep_state` corresponds to `stay`. The provider here plays the behaviour's part. See the comparison below.
- *Elixir.* There is no machine abstraction in core; Elixir programs use `:gen_statem` directly or a wrapper library. `GenServer` shows the same split between behaviour and process.
- *effect-machine and Effect.* `Machine<State, Event, R>` keeps services in the type, which is the model for `uses { R }` as the spawn charge.
- *Akka Typed.* `Behavior[T]` is a value that a separate `ActorSystem` runs, and `Behaviors.same` is a runtime keep-state marker. Ergo checks the narrowed case statically.
- *Rust typestate.* Illegal transitions are unrepresentable for statically known states. The checked plan carries that guarantee as facts, and the provider never re-derives it.
- *MoonBit and Roc.* The platform/application split, where the application is pure description and the platform owns effects, matches the plan and provider split: the provider is the swappable platform for one plan.

## Separation and adoption

The units remain independently reviewable and have no cyclic ownership:

| Unit | Owns | Does not claim |
| --- | --- | --- |
| Declaration and checking | State/event declarations, callable rows, finite transition checking and diagnostics | Runtime execution or provider choice |
| Plan lowering | Provider-independent canonical plan representation for Go and inspection | A builtin machine engine |
| Provider contract | Typed plan/provider boundary and negative controls | A completed provider or lifecycle conformance |
| First provider | Planned Ergo provider using the shared owned actor core for execution, admission and cleanup laws | A delivered provider, universal provider behavior or external transactions |
| Cross-provider conformance | Two unrelated programs on two providers with unchanged source/compiler selection | Durability, exactly-once delivery or rollback |

Adoption requires two unrelated machine programs, such as search/reload and timed lease/session or circuit breaker, executed by the planned first provider and a separately authored provider on the same target. Declarations and compiler configuration stay unchanged. Tests cover pure and effectful steps/completions, identity and rows, entry cleanup before commit, `enter` re-entry versus `stay`, stale completion fencing, bounded admission, stop/waiter control and composite causes. CLI/MCP inspection must observe the canonical plan and snapshots without running behavior. Missing requirements, incompatible identities, unsupported bindings and contract-erasing providers are causal negative controls.

Source sketches, copied fixtures and a plan snapshot are reference data. They become execution evidence only with actual target/host versions, strict ABI consumers where applicable, raw full-gate receipts and an independent frozen review. Advanced statecharts, durable execution and external transactional rollback remain separate capabilities.

## Prior art: Erlang/OTP behaviours

OTP's `gen_statem` is one engine in the standard library: a user module supplies callbacks, and the engine owns the loop, timeouts and postponement (`lib/stdlib/src/gen_statem.erl:L65-L71` at OTP `516126e9`). Elixir checks such callback modules through behaviours, where a missing callback is only a compile-time warning (`lib/elixir/pages/references/typespecs.md:L279-L354` at Elixir `91ee75bb`). Ergo inverts the ownership: the compiler produces a provider-independent plan that any userland provider can execute, and an incompatible provider is a checked diagnostic rather than a warning. This follows the owner's machine direction and **Capabilities as layers** in [NORTH_STAR](../../NORTH_STAR.md). Pins are in [PRIOR_ART](../../PRIOR_ART.md#elixir-with-erlangotp).
