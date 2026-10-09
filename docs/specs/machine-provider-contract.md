# Runtime-independent machine plans and providers

Status: owner-directed design, 2026-10-08; the contract is specified here but is not implemented language or runtime support. The machine declaration and lifecycle laws remain in [checked state machines](state-machines.md). This document defines the boundary between their checked plan and an ordinary userland runtime provider.

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

Effra's first provider will use the shared owned actor core: bounded item/byte admission, reserved completion and stop controls, serialized evaluation, entry-epoch fencing, cleanup before state/output commit, and full Cause preservation. This is the intended execution boundary for the first provider; it does not claim that provider implementation is delivered, make the actor core a machine-only runtime or require every actor to have machine transitions. A second provider must consume the same checked plan through the same public facts and retain tested, refuted or unresolved evidence for its own runtime obligations under the [lawful runtime contract](../research/lawful-runtime-contract.md), never a proof claim.

## Separation and adoption

The units remain independently reviewable and have no cyclic ownership:

| Unit | Owns | Does not claim |
| --- | --- | --- |
| Declaration and checking | State/event declarations, callable rows, finite transition checking and diagnostics | Runtime execution or provider choice |
| Plan lowering | Provider-independent canonical plan representation for Go/JS and inspection | A builtin machine engine |
| Provider contract | Typed plan/provider boundary and negative controls | A completed provider or lifecycle conformance |
| First provider | Planned Effra provider using the shared owned actor core for execution, admission and cleanup laws | A delivered provider, universal provider behavior or external transactions |
| Cross-provider conformance | Two unrelated programs on two providers with unchanged source/compiler selection | Durability, exactly-once delivery or rollback |

Adoption requires two unrelated machine programs, such as search/reload and timed lease/session or circuit breaker, executed by the planned first provider and a separately authored provider on the same target. Declarations and compiler configuration stay unchanged. Tests cover pure and effectful steps/completions, identity and rows, entry cleanup before commit, Go reentry versus Stay, stale completion fencing, bounded admission, stop/waiter control and composite causes. CLI/MCP inspection must observe the canonical plan and snapshots without running behavior. Missing requirements, incompatible identities, unsupported bindings and contract-erasing providers are causal negative controls.

Source sketches, copied fixtures and a plan snapshot are reference data. They become execution evidence only with actual target/host versions, strict ABI consumers where applicable, raw full-gate receipts and an independent frozen review. Advanced statecharts, durable execution and external transactional rollback remain separate capabilities.

## Prior art: Erlang/OTP behaviours

OTP's `gen_statem` is one engine in the standard library: a user module supplies callbacks, and the engine owns the loop, timeouts and postponement (`lib/stdlib/src/gen_statem.erl:L65-L71` at OTP `516126e9`). Elixir checks such callback modules through behaviours, where a missing callback is only a compile-time warning (`lib/elixir/pages/references/typespecs.md:L279-L354` at Elixir `91ee75bb`). Effra inverts the ownership: the compiler produces a provider-independent plan that any userland provider can execute, and an incompatible provider is a checked diagnostic rather than a warning. This follows the owner's machine direction and **Capabilities as layers** in [NORTH_STAR](../../NORTH_STAR.md). Pins are in [PRIOR_ARTS](../../PRIOR_ARTS.md#elixir-with-erlangotp).
