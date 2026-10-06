# Architecture loop — 2026-10-05

Goal: establish the project's north stars and prior arts as the basis for building its glossary and architecture.

Scope: establishment and runtime integration receipts. This ledger does not claim a completed architecture sweep or the architecture-loop close rule. Runtime work was developed in the managed-runtime Rift, and current implementation evidence is distinguished from the initial baseline below.

## Baseline

- Code HEAD: `4c497ac` (before the documentation-only establishment commits).
- Count: `git ls-files ':(glob)cmd/**/*.go' ':(glob)internal/**/*.go' ':(glob)runtime/**/*.go' ':(exclude,glob)**/*_test.go' | xargs wc -l`. Baseline has no runtime directory yet. Source counts exclude tests, fixtures, generated output and dependencies.
- Directory coverage: use the same pathspec with `| xargs -n1 dirname | sort | uniq -c`.

| Package | Lines | Files |
| --- | --- | --- |
| cmd/ef | 238 | 1 |
| internal/compiler | 1,319 | 4 |
| internal/mcp | 301 | 1 |
| Total | 1,858 | 6 |

## Coverage

| Directory | Files | Mark | Pass |
| --- | --- | --- | --- |
| cmd/ef | 1 | unswept | Establishment read; no architecture sweep yet |
| internal/compiler | 4 | unswept | Establishment read; no architecture sweep yet |
| internal/mcp | 1 | unswept | Establishment read; no architecture sweep yet |

## Prior art

`PRIOR_ARTS.md` is the authoritative comparison index. Its Settled section carries adopted/rejected verdicts with implementation status; its To survey section carries open research. Establishment reused the inspected sources in `docs/design.md` and `docs/interop.md` and refreshed focused compiler, runtime and semantic-tooling source reads. It did not execute external application examples or resolve their remaining questions.

## Project sweeps

| Sweep | Pass | Result | Done when met? |
| --- | --- | --- | --- |
| Compile performance | setup | Existing frontend benchmark located; matched build scenarios pending | No |
| Lifecycle behavior | setup | Contract recorded; runtime integration remains in a separate Rift | No |
| Inspection parity | setup | Existing CLI/MCP smoke harness included in full gate | Existing subset only |

## Owner questions

These are draft interpretations for asynchronous correction, not blockers. No behavior is removed on their authority.

| Question | Raised | North stars or rule involved | Answer |
| --- | --- | --- | --- |
| Should target-specific operations stay explicit until shared conformance proves parity? | setup | Honest target capabilities | Drafted; pending owner correction |
| Does explicit contract/trust visibility win over shorter syntax? | setup | Explicit contracts beats terse syntax | Drafted; pending owner correction |
| Should managed timeout completion wait for cleanup, even past the nominal deadline? | setup | Completed owned cleanup beats deadline punctuality | Drafted; pending owner correction |
| Keep `.ef` text authoritative as semantic editing develops? | setup | Source-authority rule | Existing repository direction; drafted owner confirmation |
| Are local fixtures/scratch-only checks the standing default, with paid services and production access separately authorized? | setup | Live-check owner rule | Drafted; pending owner correction |

## Establishment

- Established `NORTH_STAR.md` and `PRIOR_ARTS.md` by the architecture-loop autonomous setup workflow. Drafts remain marked; decided by **Never Block on the Human**.
- Made `GLOSSARY.md` the canonical domain glossary, preserving the earlier definitions and adding the resolved lifecycle/interop terms. Added repository pointers; decided by **Redesign From First Principles**.
- Kept proposals and current guarantees distinct. The baseline compiler lacks host imports, scoped lifetimes, package caching, and runtime state inspection; this setup does not close those gaps.
- Validation: full `./scripts/gate.sh` and `git diff --check`, with final results recorded in the handoff. No push/publication or Wayfinder HITL closure.

## Carried

Actor-model comparison added2026-10-06 in response to the owner. Source research and selected immutable source checks are in [actors and durability](../docs/research/actor-model-and-durability.md). Decided by **Redesign From First Principles** and Go-like simplicity: retain the existing ordinary machine/ADT/effect core; propose modular supervision and explicit durability/deployment providers, with workflow replay and VM hot-loading separate. This is a carried design opportunity, not an expanded first machine implementation or a closed HITL decision. Actor-transfer isolation, fresh restart ownership, durable acknowledgement/fencing and crash-side-effect semantics require finite specifications and executable evidence before adoption.

| Decision | From | Files | Status |
| --- | --- | --- | --- |
| Refresh ownership/type/inspection gap receipts after managed runtime integration | setup | NORTH_STAR.md, PRIOR_ARTS.md, this ledger | done: runtime receipt below |
| Run the first full architecture pass over all unswept directories and open prior-art questions | setup | Architecture areas and research in PRIOR_ARTS.md | open |
| Establish matched end-to-end build fixtures and select evidence-based budgets | setup | Compiler/build performance sweep | open |

## Close

Establishment complete when both project files, canonical glossary, repository pointers, draft owner questions, and a passing gate exist. The full architecture loop remains open: unswept directories, To survey questions, and carried rows remain.

## Runtime integration receipt

- Runtime ownership: `c125a8f`; compiler/std-library integration and concrete examples: `5c8c31f`.
- Driven: native `examples/lifecycle.ef`, Go `examples/go-interop`, existing JS sequential examples, actual CLI and stdio MCP smoke harness.
- Observed: a scoped open file appears in Runtime.inspect; child interrupt and deadline shutdown finish; a real Go partial result survives error-channel adaptation; managed File rejects post-close reads. Source scopes/fibers/timeouts and Files/Runtime are explicitly Go-only.
- Timeout currently uses the native runtime clock; Clock.sleep remains replaceable. No claim of fake-time deadline control is made. Decided by **Redesign From First Principles**: advertised requirements must correspond to an actual consumed service.
- Coverage remains unswept; `runtime/effra` is a new unswept area. Runtime tests and a source review are receipts, not a complete architecture-loop pass or independent counsel.
- Remaining: automatic source-level host imports, real SDK adoption tests, open-row/package inference, process-wide runtime inspection, JS lifecycle conformance, and matched build benchmarks.
- Validation: `./scripts/gate.sh`, `go test -race ./...`, and `git diff --check` passed. The gate exercised native/JS sequential programs, Go lifecycle source, and the actual stdio MCP process. All check processes exited.
- Current benchmark: the existing 10k-line parse/check fixture measured 3.30–3.35 ms/op, about 14.05 MB and 16,176 allocations on Apple M4 Pro/Go 1.27.1. It measures neither imports nor end-to-end builds.

Current source count using the Baseline pathspec (after integration):

| Package | Lines | Files |
| --- | --- | --- |
| cmd/ef | 242 | 1 |
| internal/compiler | 1,537 | 5 |
| internal/mcp | 316 | 1 |
| runtime/effra | 637 | 5 |
| Total | 2,732 | 12 |

## Automatic imports, HTTP and JS lifecycle receipt

- Implemented primitive Go imports through module-resolved export archives, explicit Foreign recipes, raw GoResult partial values and explicit OrFail. Binding inspection separates native signatures from reviewed context/cancellation assertions. Semantic revisions include export bytes and normalized metadata. A dependency return-type edit is detected; replaced transitive module builds execute successfully.
- Added `examples/imports.ef` with standard-library calls and a compiled local SDK fixture. This is not a complex third-party SDK adoption receipt. Named host types/methods, broader module workspace/build-tag coverage, and TS imports remain open.
- Added `examples/http.ef` and a managed native HTTP provider. Public process tests check health, SDK route, failure response, scoped file read, deadline and SIGTERM shutdown. Runtime tests prove shutdown waits for request cleanup and releases the listener.
- JS scopes/fibers/deadlines now use an ownership policy over pinned Effect, with no additional scheduler. Both backends pass shared child-before-parent cleanup, unobserved failure and timeout cleanup-defect probes; the portable concurrency example runs on both. Files/Runtime/Http and Go imports remain Go-only.
- Verification: full gate and `go test -race ./...` pass. CLI/MCP used-binding metadata and imported revisions agree. Runtime HTTP is an independently gated checkpoint; integrated lowering and documentation are separate commits. No remote or publication is involved.
- Performance receipt: warm imported checks about 51 ms (46–47 ms loader), cached imported executable builds about 156 ms, no-import 10k-line frontend sample 3.65 ms. See prototype measurements for fixtures and limitations. Normalized import caching and matched edit benchmarks remain priorities.
- This records implementation progress, not a completed architecture-loop review or closed Wayfinder decisions. Remaining work includes complex host representations, TS declaration bridge, persistent caching, broader lifecycle/HTTP policies, and process-wide runtime inspection.

## Application showcase review

- Refreshed public source caches and the requested local source checkout. Read application implementations without changing unrelated work. Generic pattern findings are in `docs/research/effect-native-showcases.md`; original examples are in `docs/showcases.md`.
- Added runnable Go/JS workflows for three-service composition and completed task replacement. The public smoke checks execute both examples and inspect the workflow’s exact failure/requirement contract.
- Proposed ADTs, exhaustive match, payload errors, codec derivation, stream consumers, durable admission results and phase-aware infrastructure outputs remain clearly separated from implemented syntax. The recommendation is to implement the data-model slice before provider graphs/streams or deployment syntax.
- Preserved explicit distinctions: decision values versus failures; static sums versus runtime decoding; scope ownership versus durable transactions; effect execution versus deployment Output resolution. No third-party deployments or private application code were published.
- Full Effra gate passes. This is a source review and showcase receipt, not a completed architecture-loop sweep or a closure of HITL decisions.

## Contender assessment

- Added `docs/contender-roadmap.md`, grounding the adoption gaps in the same refreshed sources and current checker/importer/runtime seams. The existing primitive-only signatures, self-contained provider guard and pure fallback recovery are concrete port blockers.
- Proposed five build areas with exit gates: data/package contracts; real host objects and managed adapters; dependent provider construction and recovery; deep server capabilities; fast inspection/development. Measurements and tooling apply throughout, rather than after feature work.
- Proposed three adoption fixtures: JSON/SQL API, durable agent event bridge and process supervisor. Each must preserve the source protocol under failure and compare costs against an equivalent implementation. This is an assessment, not compiler/runtime implementation or proof of production readiness.
- Kept transactions, migrations, replay/ACK policies, correlation and deployment graphs with their owning libraries/application services. No HITL decision was closed.

## Native tooling and testing slice

- Shared compiler model now supplies lint/rules, byte-anchor type queries and a static function/service/provider graph through CLI/MCP. Compiler soundness errors remain mandatory; lint never reports unchecked types as authoritative.
- Added primitive assertions, checked test discovery and Go/JS execution with fresh case scopes, structured causes, bounded captured output and an independent real watchdog. Live host/time use is explicit. Virtual scheduling, causal synchronization, scoped fixtures and static handle escape provenance remain carried gaps.
- Public docs describe generic application patterns; no application-specific comparisons remain. ADTs, matching, codecs and dependent provider construction remain proposals.
- Gate receipts include public CLI/MCP parity, fixture substitution, assertion failure followed by a passing case, watchdog expiry and existing runtime lifecycle conformance. Owner decisions remain open.

## Owned generated-module integration

2026-10-06: generated-output batch ff625433..b86ccf22 merged current root40351f9 into its branch at2e23a894, passed the branch gate, then fast-forward integrated the same head and passed the root gate. Both counsel rounds' findings were consolidated; finite Astra source/evidence closure is `~/.cache/architecture-loop/effra/pass1/generated-output-b86-final-closure.md`. No third counsel round. Decided by **Redesign From First Principles**: complete immutable per-application modules replace additive runtime output without deleting or adopting old files.

Live checks passed for actual native CLI reuse, ordinary/test and same-basename identities, real inherited-workspace and module-mutation negative controls, physical symlink/.. roots, failed exclusive publication/retry and cooperating writer cleanup. Root independently captured/verified31 pre-existing output files' type, bytes and mtime across integration; legacy dist/go/runtime/stdlib.go remains unchanged. All root probe processes ended. Raw branch gate SHA256 f9c0561e83a6110d35973b0ae1a13873c15c7920f25b5b83b67ec7e999525ba5 and root gate SHA256 5a2262722fe8982d6c5153bcb25e127635c034f634871d1d84617bc7519b07a6 are retained in `/tmp/effra-generated-output-{branch,root}-integration-gate.log`. Root-owned tracker task closed after this proof.

Carried: integrate the already reviewed runtime source split over preserved legacy output; implement checked application reachability after native interfaces; establish actual size/dependency/symbol receipts in the final measurement phase. Hard-link unsupported filesystems, adversarial same-user path replacement, literal process-kill and power-loss durability remain outside the demonstrated publisher contract. Other feature lanes and benchmarks are not certified by this gate. No push, paid deployment or HITL closure.

## Runtime source-module integration

2026-10-06: completed the preceding source-split carried item. Reviewed0e4f5be's source/catalog/cache/inventory repair merged current root802737d into runtime-modules at637313b, passed that branch gate, then integrated the same tree and passed the root gate. Independent exact move proof preserves11 declarations/comments and7 unchanged lifecycle/scheduler files; selected module closures compile with inherited caches and trimpath controls. The original additive-upgrade failure remains recorded; no legacy output was deleted to bypass it.

Root live check builds and runs public examples/main.ef: output `Hello, Ada` then `Unknown user`. Its completed immutable module contains12 split runtime sources and no retired stdlib; root legacy stdlib and all31 pre-existing output nodes retain identical bytes/type/mtime. Receipt `/tmp/effra-runtime-split-root-live.json`, gate logs `/tmp/effra-runtime-modules-{branch,root}-integration-gate.log` with SHA2569c7ba1e0ae4c3f6f2cfaf5541e00d7778c87b753ae18f8492752a19eb60e51a8 and08d3b3a7719e965f1c4922061f4c8f753191483f3a34a8954a05600bef580596. Compiler73.716s/runtime2.845s are gate durations, not comparative performance claims. All started live-check processes exited. Source-module child task closed; parent reachability, binary size and complete standard-library tasks remain open. Publisher checkout retired after clean-state/ancestry/gate/tracker guards; branch and external receipts retained. Other reviewed feature lanes remain independent.
