# Effect-native showcase source review

Reviewed 2026-10-05. Public sources were refreshed before reading. Gent was pulled with `git pull --ff-only` in a clean main checkout. Their examples were read, not deployed or executed. The new Effra examples were executed on both targets.

| Source | Inspected snapshot | Read for |
| --- | --- | --- |
| Alchemy | `1431ef2e1f9e09dfa9fb2c5e221821e51b727327` | Service/binding construction and Output expressions |
| T3 Code | `cfa4f765ec05950a032b6c1cf9cdfff0c2391545` | Decisions, decoded storage, replay/live streams and durable receipts |
| Gent | `5bc4bafd7b5a5374dbb0b9893c6d9f2d94c50fb7` | Phase state, exhaustive projection and admission ownership |

A private application was also inspected locally for latest-work, managed-process and service-adapter patterns. Only original generic examples are included; private implementation code, business rules and repository identifiers are not reproduced. Those observations are local evidence, not publicly reproducible source claims.

## Services and bindings

Alchemy's notification example separates a service declaration from construction of a topic, bound publisher and implementation. Native service declarations can reduce class/tag/generator ceremony; typed failures, dependent provider graphs, sharing and lifetime must remain explicit. [Service construction and operation](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/examples/aws-lambda/src/JobNotifications.ts#L19-L81).

The cloud binding supplies publish permissions and topic identity across deployment/runtime phases. This is not equivalent to importing a module. [Publish layer](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/packages/alchemy/src/AWS/SNS/PublishHttp.ts#L6-L14), [binding behavior](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/packages/alchemy/src/AWS/SNS/BindingHttp.ts#L80-L105).

The showcase workflow uses deterministic demo providers. It proves checked composition, not authorization correctness, cloud integration or durable delivery.

## ADTs and exhaustive interpretation

T3 Code declares tagged transition decisions; a later service translates rejection into an Effect failure. Effra sums could simplify the data while making exhaustive interpretation a compiler obligation. [Decision declaration and policy](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/ProviderSessionTransitionPolicy.ts#L13-L91), [rejection interpretation](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/ProviderSwitchService.ts#L168-L199).

Gent declares phase-specific payloads in Schema.TaggedUnion and derives a projection with Match.tagsExhaustive. Effra should preserve those constraints and coverage, rather than introduce parallel boolean/status state. [Phase state](https://github.com/cevr/gent/blob/5bc4bafd7b5a5374dbb0b9893c6d9f2d94c50fb7/packages/core/src/domain/agent-loop.ts#L61-L108), [exhaustive projection](https://github.com/cevr/gent/blob/5bc4bafd7b5a5374dbb0b9893c6d9f2d94c50fb7/packages/core/src/runtime/agent-loop.ts#L514-L534).

Effra ADTs, records, payload errors and exhaustive match remain proposed. No illustrative match is claimed as currently checked.

## Decoding is executable behavior

T3 Code decodes JSON payloads/metadata, the domain event and its stored envelope. A static sum cannot replace those checks; a codec must also define wire compatibility. [Event-store decoding](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/persistence/Layers/OrchestrationEventStore.ts#L89-L132).

Gent defines interaction records and JSON codecs for parameters and decisions; identity and ownership remain service obligations. [Interaction records and codecs](https://github.com/cevr/gent/blob/5bc4bafd7b5a5374dbb0b9893c6d9f2d94c50fb7/packages/core/src/domain/interaction.ts#L125-L194).

Alchemy's notification demo uses JSON.parse followed by a type assertion. That checks JSON syntax, not the asserted payload shape. Adding decoding would be an added runtime guarantee, not merely shorter syntax. [Demo parse boundary](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/examples/aws-lambda/src/JobNotifications.ts#L39-L52).

## Owned replacement and admission

The local latest-work adapter bridges host calls to a scope/fiber, tracks current-run identity, offers deduplication and disposes its scope. Effra's original example chooses a stricter policy: await previous interruption and cleanup before admitting its replacement. It does not reproduce concurrent admission, promise deduplication or overlapping generations. Existing runtime/conformance tests own the cleanup proof; the new example exercises the CLI.

Gent protects admission/start with an uninterruptible step and uses a resident scope where needed. A small fork must not leave a reservation without a worker or release its resident too early. [Admission and resident ownership](https://github.com/cevr/gent/blob/5bc4bafd7b5a5374dbb0b9893c6d9f2d94c50fb7/packages/core/src/runtime/agent-loop.ts#L1430-L1497).

## Replay/live streams

T3 Code subscribes and drains before reading the high-water mark, replays through it and filters duplicated live events. This belongs in a stream service, not generic loop syntax. [Replay/live assembly](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/LiveStreamBudget.ts#L285-L342).

Retention is bounded by items/bytes and remains charged while delivery is in flight. The next pull corresponds to RPC acknowledgement. Overflow closes the source scope and releases retained work. Effra's consumer must preserve those library policies and producer ownership. [Delivery and cleanup](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/LiveStreamBudget.ts#L139-L192), [owned producer](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/LiveStreamBudget.ts#L213-L250).

## Durability and idempotency

T3 Code reserves a receipt and commits events, projections, outbox work and the receipt in a transaction. A publication lane preserves commit/publication ordering; duplicate commands return their existing result. These are application/storage policies. [Transaction and publication lane](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/EventSink.ts#L231-L260), [receipt/event/outbox admission](https://github.com/pingdotgg/t3code/blob/cfa4f765ec05950a032b6c1cf9cdfff0c2391545/apps/server/src/orchestration-v2/EventSink.ts#L521-L575).

Journal.admit combines these ideas with explicit conflicting-retry behavior as a proposed generic contract. It does not claim that every source uses this exact protocol. Scopes and typed failures do not imply exactly-once remote delivery.

## Infrastructure outputs

Alchemy Output.map constructs ApplyExpr; fromEffect is inert until the deployment phase. The planner retains unresolved expressions rather than capturing values prematurely. An output block must retain graph/phase semantics. [Expression construction](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/packages/alchemy/src/Output.ts#L222-L254), [phase boundary](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/packages/alchemy/src/Output.ts#L43-L58), [unresolved expressions](https://github.com/alchemy-run/alchemy/blob/1431ef2e1f9e09dfa9fb2c5e221821e51b727327/packages/alchemy/src/Plan.ts#L979-L1004).

## Recommendation

Implement records, closed ADTs, payload errors and exhaustive match first, then explicit codecs. Dependent providers, stream protocols and infrastructure phase syntax need separate receipts. Keep frontend/import/backend timings distinct; shorter syntax is not evidence of faster compilation or correct runtime behavior.
