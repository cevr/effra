# Pattern and guardrail index

Effra's public architecture is described through generic patterns. This index records the decisions that source reviews inform, without making an external application's structure part of the language contract. Read [NORTH_STAR.md](NORTH_STAR.md) and [GLOSSARY.md](GLOSSARY.md) alongside it.

## Patterns

| Pattern | Adopted direction | Boundary / receipt |
| --- | --- | --- |
| Lazy effect values | Explicit execution, checked failures and nominal requirements | Shared checker and both emitters; portable conformance tests |
| Native declaration import | Consume host signatures automatically | Primitive Go free functions work; behavior metadata is a reviewed assertion |
| Owned concurrency | A scope shuts down children before releasing resources | Go runtime and JS ownership adapter; cancellation is a request |
| Focused compiler tooling | CLI/MCP share checked contracts, revisions, lint and graphs | Single-file model; no persistent semantic workspace or checked editing yet |
| Closed application states | Payload-owning alternatives and exhaustive interpretation | Records, closed ADTs and matching are implemented; external decoding remains proposed |
| Explicit wire decoding | Static types do not validate stored or incoming data | Codec and migration library remains proposed |
| Dependent providers | Construction has its own requirements, failures and owner | Current providers are self-contained; sharing/cycles/acquisition remain open |
| Bounded event delivery | Track items, bytes and in-flight acknowledgement | Streams, queues and delivery budgets remain proposed |
| Durable command admission | Correlation, transactional receipts and outbox recovery | Application/storage obligations, not a syntax guarantee |
| Owned tests | Fresh case scope, assertions, completed shutdown and preserved causes | `ef test` works on Go/JS; causal latches and virtual time remain open |

See [showcases](docs/showcases.md), [pattern review](docs/research/effect-native-showcases.md), [tooling](docs/tooling.md), [testing](docs/testing.md) and [adoption gates](docs/contender-roadmap.md).

## Settled

- `.ef` text remains authoritative. Dependency graphs are rebuildable views with revision-scoped expression IDs.
- Compiler soundness diagnostics cannot be disabled by optional lint. Unchecked files do not receive authoritative expression types or dependency graphs.
- Lazy construction does not execute an effect. Unused local recipes receive lint advice; bare discarded recipes remain compiler errors.
- A managed timeout waits for shutdown. A process watchdog may force termination and must report cleanup as unconfirmed.
- Host signatures do not prove purity, cancellation, retained-reference safety or resource ownership. Partial native results survive explicit adaptation.
- Tests supply only assertions implicitly. Fixture services stay explicit, and live host/time capabilities require opt-in. This is a capability check, not an OS sandbox.
- Application-specific state machines, identity, transaction and overflow policies remain explicit even when syntax becomes shorter.

## To survey

- Introduce structured type identities, records, closed sums and checked matches without whole-program inference.
- Track handle ownership provenance so proven inner-scope escapes receive static diagnostics while borrowed outer handles remain valid.
- Add provider construction contracts, shared acquisition and cycle explanations; enrich the same dependency graph rather than inventing another analyzer.
- Add reasoned named lint suppressions with stale/unused checks, and revision-bound fixes validated before they are offered. Preserve comments in formatting.
- Build scheduler-backed test time, causal synchronization and scoped fixtures; prove they control sleeps and timeout operators together.
- Measure native import reuse and matched cold/warm/private-edit build regimes before making speed claims.
