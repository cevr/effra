# Ordinary functions, checked machines and transforming codecs

Research receipt, 2026-10-06. These decisions guide the authorized implementation; they do not describe capabilities already shipped. The governing constraint is Go-like local readability with explicit Effect-style contracts. Source and test inspection informed this comparison; upstream suites were not executed for it.

## Machines

The main reference is [XState v6 PR5543](https://github.com/statelyai/xstate/pull/5543), `next` pinned at `2146ae26ebfc7e6a624b3a1f237f9e6ddc30b9f5`. Earlier v5 findings are historical. [Effect Machine](https://github.com/cevr/effect-machine/tree/176697cf20006f5f539bf04ff2e609bfe1563e22) supplies the complementary owned-lifetime and cleanup comparison.

| Evidence at the pinned revisions | Effra decision |
| --- | --- |
| XState v6 `docs/guards.md` and `docs/transitions.md` use ordinary functions and local control flow. | Transitions are ordinary functions over state/event ADTs. A small machine declaration binds the initial value and named `step`, `enter` and `complete` functions. Following the owner's clarification, step/completion may also be effectful; their execution and commit contract requires separate Effra tests. |
| `packages/core/src/utils.ts` converts function transitions to a `to` function; `src/graph/graph.ts` projects the static `target` field, falling back to the source state. This source path does not establish all dynamic destinations. | Project locally visible constructors; label unknown helper destinations conservatively. Never present the graph as a proof of exact reachability. |
| `packages/xstate-effect/src/requirements.types.test.ts` explicitly records that logic spawned inside a transition body is invisible to the machine's requirement type. Its requirement walker also has a finite recursion budget. | Check ordinary function bodies and preserve recipe rows. A checker budget exhaustion diagnoses; it cannot erase a service requirement. |
| XState actor/session and timer-occurrence identities reject obsolete work. Effect Machine tests cover re-entry and completed stop cleanup. | Each entry has an epoch; stale completions are discarded and counted. `Go` always re-enters, while `Stay` retains work and requires the same variant. Cleanup completes before the next entry is published. |
| XState's mailbox and its Effect adapter's queue are unbounded; Effect Machine's actor queues are also unbounded. | Bounded mailbox admission is an additional Effra policy requiring its own tests. One completion slot per entry and a separate stop path keep lifecycle progress possible under saturation. |

Keep timers as ordinary Clock work. Keep expected work failures as explicit outcome ADTs in the first profile; defects, interruption and composite cleanup causes retain their runtime meaning. Do not add nested transition syntax, a second guard language, or a separate schema for each state. Generic data, product/or-pattern matching and payload recovery are reusable prerequisites, with their own acceptance tests.

The original counsel proposed pure-only step/completion. The owner explicitly superseded that restriction: effectful decisions are supported, with serialized owned evaluation and explicit rows. Failed evaluation does not commit state; it still cannot undo external effects. Pure stepping remains available. Neither graph construction nor runtime inspection evaluates an effectful condition.

Focused effectful-step counsel clarifies evaluation cleanup before entry cleanup and commit, pure enter construction, call-local failure attribution, non-draining stop and nonblocking snapshots. These are Effra contracts requiring direct tests; XState v6's synchronous transitions and Effect Machine's recovered handler error channel do not establish them. Adopt the actor size fixture and shared runtime loop. Reject a bounded propagated call chain as a general deadlock guarantee: independently initiated mutual waits need more than causal ancestry. Retain alias-aware self-wait detection in the first profile and expose cross-actor cycle limits. Also qualify the proposed removal of Clock/Scheduler: necessary core scheduling remains a legitimate runtime dependency, while unused platform adapters stay absent. Decided by explicit contracts, measured evidence and Go-like local readability.

The [machine specification](../specs/state-machines.md) owns the exact supported profile. Hierarchy, parallel regions and durable execution are outside its first slice. A serializable state value does not establish durable workflow semantics.

## Codecs

Compare pinned [Effect4.0.1](https://github.com/Effect-TS/effect/tree/460272d30457f4697d8b8c52cad41caccbcace08), especially `Schema.ts`, `SchemaTransformation.ts`, `SchemaGetter.ts` and schema tests, with [Serde](https://github.com/serde-rs/serde/tree/6693a89cca77e0151437da1c7f890090b9ebf04c), especially derive generation, attribute conflict checking and conversion tests.

Adopt opt-in structural derivation and ordinary transformations between distinct wire/domain types. Each direction carries its own failures and services. Inspect those contracts and field contribution paths without making a caller spell an implementation tuple of type parameters.

Further pinned source inspection through the repo cache confirms `Schema.ts`'s Codec tracks distinct decoding/encoding services, while `SchemaGetter.ts`'s TransformEffect callback has the fixed SchemaIssue.Issue error channel and `Schema.decodeUnknownEffect` wraps that as SchemaError. Effra's named transformation failures are therefore an additional contract, not upstream parity. Preserve those failures through codec helpers and transport composition: malformed wire input, service failure while decoding and terminal error-response encoding are distinct boundaries. The native server spec supplies finite mappings and a nonrecursive terminal fallback. Upstream getter composition tests are source evidence only; the new typed-failure distinctions need Effra tests.

The independent design counsel proposed several additional policies. Applying **Redesign From First Principles** and the explicit-contract tiebreak gives these decisions:

| Proposal | Decision and reason |
| --- | --- |
| One global codec witness per domain type | Reject. Multiple named witnesses support API versions and storage representations without artificial wrapper types. |
| Reuse provider `impl` for codec witnesses | Defer. Ordinary typed functions and explicit codec values already provide the seam; preserve the existing provider meaning. |
| Fallible pure methods with new `raises` behavior | Do not introduce solely for codecs. Use admitted Result values or effect recipes and the shared recovery rules. |
| Strict unknown-field rejection as the sole JSON default | Reject as a silent comparator change. The matched server profile explicitly ignores excess fields; any stricter profile has a distinct identity and test matrix. |
| Every derived codec guarantees an inverse round trip | Qualify. Structural derivation inherits any field transformation's law. Normalization, loss and effectful work need explicit tests; no automatic reversibility claim. |
| Field attributes for every rename, default or migration | Defer. Start with an explicit wire record and ordinary transformation functions; add syntax only when independent callers justify it. |

Derivation plans share nominal type structure and diagnose unsupported or recursive layouts with field paths. A transformation runs lazily under an owner, without hidden retries or duplicated execution. The [native server contract](../specs/native-server-contracts.md) specifies directional contracts, policy versions, cancellation, inspection and the server/non-server acceptance cases.

## Prior art: Elixir, Erlang/OTP and MoonBit

Erlang/OTP `gen_statem` at `516126e9` is the closest production machine runtime ([pins](../../PRIOR_ARTS.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)). The [machine specification](../specs/state-machines.md#prior-art-erlangotp-gen_statem) records how Effra adapts its enter calls, `repeat_state` and `state_timeout`, and how a pure `enter` makes its run-time enter-change crash unrepresentable. Its `postpone` action, which retries an event after the next state change (`lib/stdlib/src/gen_statem.erl:L848-L857`), stays outside the first profile, matching the deferral of selective receive.

For codecs, Elixir's `JSON` module decodes objects into string-keyed maps with no struct-targeted decoding, and structs opt into encoding with `@derive {JSON.Encoder, only: [...]}` (`lib/elixir/lib/json.ex:L4-L33`, `L314-L326` at `91ee75bb`). MoonBit derives `ToJson`/`FromJson`, but its documentation calls the format "mainly for debugging", says argument behavior "is unstable", and changes the `Option` encoding depending on whether it is a direct struct field (`next/language/derive.md:L121-L199` at docs `8d9f3ba2`). Both confirm the decision above to keep opt-in derivation and explicit wire records instead of treating derived output as a stable public format. Elixir's allow-list `only:` form supports keeping field exposure explicit.

## Validation still required

These comparisons settle the initial design direction. Implementation must supply executable negative checks, Go/JS behavior, cancellation and completed cleanup, CLI/MCP parity, and bounded compiler-cost receipts. Copied reference tests remain reference material until each applicable behavior is mapped to a passing Effra test.
