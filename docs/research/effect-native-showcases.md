# Application patterns informing the language

Reviewed 2026-10-05 against actual service, orchestration, platform and testing implementations. Public documentation presents generic patterns; the examples are original and do not reproduce application code. External applications were inspected, not executed. Runnable Effra receipts live in the repository gate.

| Pattern | What a native language can simplify | What must remain explicit |
| --- | --- | --- |
| Service-backed notification | Service declarations, failure/requirement rows, checked provision | Configuration, authorization, startup failure, shared lifetime |
| Phase state and transition decisions | Closed sums, payload binding, exhaustive matching | Admission ownership and domain transition policy |
| Persisted events and interaction records | Nominal data declarations and codec derivation | Runtime decoding, schema versions, migration, identity/correlation |
| Latest-work replacement | Scope ownership and joined cancellation | Generation-safe publication and replacement policy |
| Replay/live event delivery | Scoped consumer syntax and typed streams | Subscribe-before-replay ordering, duplicate filtering, ACK budgets and overflow cleanup |
| Durable commands | Typed admissions and service contracts | Atomic receipt/event/projection/outbox transaction and crash recovery |
| Deferred infrastructure output | Typed expressions and dependency queries | Planning phase, unresolved graph nodes, permission binding and deployment ownership |
| Effect testing | Explicit fixture provision, fresh case ownership and assertions | Causal readiness, test scheduling, host isolation and subprocess termination |

## Guardrail lessons

Optional lint can detect a never-used local recipe or unnecessary provision. Correctness obligations such as closed error rows, missing services and unsupported host calls belong to the checker. Syntax heuristics do not prove bounded concurrency, finite retained bytes or safe callback lifetimes.

Resource acquisition must remain lazy and register cleanup atomically with its owner. Future shared memoization needs an independent producer owner: interrupting one waiter must not cancel every consumer's work. A memo wrapper alone cannot establish that policy.

Static handle escape checking now distinguishes proven inner-owned results from borrowed outer handles, including bounded function summaries, record/enum paths and conservative uncertainty when analysis budgets are exhausted. Runtime checks remain in place. This is bounded evidence, not complete borrow checking; typed callback-result provenance remains part of the first-class function work. The [foundation receipts](../receipts/foundations-2026-10-06/README.md) record the integrated boundary.

## Testing lessons

Dependency injection supplies fixture substitution. A runner adds case ownership and lifecycle accounting. One-shot signals establish when work starts or cleanup finishes; fixed sleeps do not. Virtual time must control scheduling, not merely replace a timestamp function. A real watchdog stays independent of virtual time and cannot claim successful finalization after forced process termination.

Temporary directories, console capture and subprocesses should be scoped fixtures with explicit identities and cleanup receipts. Host environment/network isolation requires separate controls; explicit capabilities are not an OS sandbox. Test doubles should preserve real service signatures and production orchestration paths.

## Current receipt

`examples/workflow.ef`, `examples/latest-task.ef` and `examples/testing.ef` run through public commands on Go and JS. The shared checker powers lint, local type queries, dependency graphs and test discovery. Codecs, streams, durable journals and fallible/memoized dependent provider initialization remain labeled as proposed in [showcases](../showcases.md) and [adoption gates](../contender-roadmap.md); records, closed ADTs, exhaustive match, typed payload errors and explicit dependency-capturing provider construction are implemented and covered by compiler conformance tests.
