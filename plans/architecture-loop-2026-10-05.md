# Architecture loop — 2026-10-05

Goal: establish the project's north stars and prior arts as the basis for building its glossary and architecture.

## Owner update: useful primitives and first-class layers, 2026-10-06

Every architecture pass now seeks recurring production Effect ceremony that a small regular primitive can express explicitly, declaratively and delightfully. Compare an ordinary library solution, preserve visible execution/failure/service/ownership facts, and prove adoption with two unrelated runnable before/after callers and negative controls. Recorded in NORTH_STAR's new ergonomics sweep; no source sketch counts as implementation.

Refreshed five production application sources through the repository-cache workflow; exact public pins and generic comparisons are in PRIOR_ARTS and [layer research](../docs/research/layer-composition.md), with private pointers retained outside the repo. Source-only audits found explicit application graph/replacement machinery, manual bottom-up wiring, startup nodes without outputs, application-service reinjection, context-sensitive fresh instances and owned/borrowed resource distinctions. No application tests were run. Decided by **Redesign From First Principles**: adopt the owner's first-class graph assembly, not a second hidden provider framework. Reject upstream roots-only exposure, duplicate-provider precedence and failure erasure against explicit-contract/ownership north stars.

Added the [five-unit layer contract](../docs/specs/layers.md) and six open implementation tickets. Runtime and static-plan preparation may proceed independently through one agreed seam; fallible construction then consumes both plus canonical bundled interfaces. Replacements recompute edges/rows before acquisition/cycle checks, explicitly selected hidden startup effects remain, inherited application services are borrowed, and compatible context follows instance identity rather than structural equality. These are specified acceptance cases, not delivered guarantees. Existing HITL decisions remain open; benchmark work remains last; no push.

Independent design counsel and root source review refined this contract before implementation: fresh acquisition uses explicit separate builds; flat graphs have one input context per binding and no `merge fresh`. Startup nodes imply atomic publication, not sibling ordering; required migration runs in the publishing factory. Constructor completion retains its node owner and ordinary forked work, with child failure observed at closure in the initial profile. Visibility is the union of public paths, replacement is lexical and preserves visibility, exported layer inference is an owner-directed exception, and summaries include hidden/start nodes. Unit2 owns the typed plan/build seam; Unit3 also depends on public acquire/finalization. First-class provision closes its owned program and graph before returning. Pinned Effect first-requester cancellation is an explicit behavioral difference, not inherited parity.

Integrated callable Unit2 is ce45a63/tree03ed0ab. First exact root gate hit the unchanged60-second generated-Go continuation watchdog; unchanged isolated control also failed, a subsequent trace control passed54.64s and the full exact root retry passed. Retained logs/receipts live at `/tmp/effra-callables-unit2-root-integration-gate.log`, `/tmp/effra-root-long-continuation-stage-trace.log` and `/tmp/effra-callables-unit2-root-integration-gate-recheck.log` (successful SHA58c38df7b3611862e983dfa9bb5a82c212973d87ac490d93a969325b2e9a2570). Host pressure observations do not prove a sole cause or performance improvement; watchdog/source unchanged. All31legacy output nodes verified unchanged. Unit3 compiler-distributed interfaces now execute in an isolated Sol6.1medium lane from that exact gated root; it has not integrated or completed yet.

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

Actor-model comparison added2026-10-06 and refined by the owner: actors need not be machines. A dedicated native research subagent compared ordinary handlers and receive loops at immutable source pins, including newly inspected celld example source and pinned typed Effect handler/queue adapters. Decided by **Redesign From First Principles** and Go-like simplicity: use service/impl/effect/uses/raises/scope plus a runtime/library actor capability; a machine adds transitions/entry lifetimes. The [finite ordinary actor contract](../docs/specs/actors.md) specifies portable payloads/native-alias refusal, admission versus reply/commit, actor-owned work after caller cancellation, explicit sequential/stop policy and full CLI/MCP type inspection. No actor DSL earns a unique guarantee from this usage; restricted receive correlation precedes raw responders. Four open implementation units reuse the common semantic model and shared actor core. Supervision, durable addressing/persistence, workflow replay and VM hot-loading remain separate for either behavior. Source comparison and sketches are not executed actor support or a closed HITL decision; benchmark work remains last.

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

## Versioned LSP diagnostics integration

2026-10-06: preparatory diagnostics/documents adapter `3c807cb..11412a7` merged current root `9ada03c` at `6cbbed0`. Full branch/root gates pass; SHA256 `568bd9dbbe1380c5cfa66ae24de7c933984353ae454da533e155b1155f808288` and `5323e5c8330adf31fccdfe8d170d14d8cffaa5e9a34762a1f66a9b0e985261d2` in `/tmp/effra-lsp-{branch,root}-integration-gate.log`. Focused native race passes. Both Claude rounds consumed; URI alias admission, recoverable operational budget and raw-escape segmentation repairs have causal permanent RED/GREEN and independent process receipts. Shared diagnostics retain compiler codes, severities, UTF-16 ranges and exact buffer identity/version; no duplicate analyzer. All 31 legacy output nodes are unchanged; all controlled process sessions exited. Child task closed; full types/navigation/editor formatting remain open parent work. Synchronous checking cannot cancel the current import subprocess or promise total shutdown latency; current finite profile limits are [documented](../docs/lsp.md). No push, benchmark claim or HITL closure.

## Language/library and Go protocol constraint details

Owner clarification, 2026-10-06: no nil language values. The [absence contract](../docs/specs/absence-and-host-boundaries.md) prohibits bare nil/null, nullable ordinary types and observable uninitialized fields; generic Option is ordinary closed data. Host adaptation preserves native interface/error identity and partial results without exposing a nil source pointer. Match status remains distinct from adapted pointer presence. Protocol JSON null remains wire representation. Finite Astra design review found no required issue; accepted its explicit typed-nil match clarification. Actual CLI admission controls at clean binary `6cbbed0` reject five unsupported forms and accept unit/domain absence on each target (14 checks); this proves existing nongeneric source admission, not generic Option or richer host delivery. Host-types now depends on generic-data. Native Go reference fixture and focused race passed separately; copied native evidence remains zero Effra host passes. Historical source preparation is retained unchanged with an explicit superseding handoff. Root will retain this unit's full gate separately; no performance or completion claim follows from the design receipt.

Owner steering2026-10-06: distinguish Effect concepts that become regular language mechanisms from runtime primitives and library policies, and automatically preserve Go's standard native protocols/descriptors. [Partition](../docs/research/effect-language-boundary.md) uses immutable Effect4.0.1 source, not the moving cache head. [Go protocol contract](../docs/specs/go-protocol-interop.md) follows inspected Go1.27 source, current importer/runtime owners, Borgo interface inference and generic external SDK retention patterns. It defines four gated units under host-interop: complete native identities/results, methods/interface assignment, owned resource borrowing/cancellation, and explicit callbacks/inverse bridges. Source comparison is complete; implementation and executable acceptance remain open. No application-specific source was copied to public docs.

Decided by **Redesign From First Principles**: one canonical host graph with Go assignment authority preserves concrete identity and optional native methods; ownership/trust facts remain separate. A Reader method set cannot prove cooperative cancellation, a Close name cannot prove release authority, and a descriptor cannot become a portable owned File by numeric coercion. Regular typed functions/rows should carry library contracts; a runtime primitive need not create a keyword. Go-only builds remain independent of TypeScript; unused library modules remain excluded by the separate reachability contract.

## Canonical type and projection integration

2026-10-06: original native interface Unit 1 is independently clear at `dd4d9dc`. Canonical values distinguish functions, lazy recipes, executed results and fibers; carried rows and evaluation rows have separate authority. Shared schema 4 publication includes complete response-local type, row and nominal references, with bounded compatibility expansion. Selected inspection and test discovery remain available when unrelated whole-source publication is refused. CLI accounting matches compact output; MCP preserves exact scalar IDs, measures both result copies and refuses oversized success without partial authority. Target-internal and protocol null remain separate from the owner's no-nil language contract.

Root reconciled the explicit-absence documentation at `3f833c1`; its full branch gate passed with actual strict TypeScript checking through the existing task-owned shim. Original twelve repair receipts, finite Astra closure and final Claude2 round-two report are retained; the long-scheduler watchdog failure and subsequent unchanged-control passes remain visible without asserting its sole cause or a performance improvement. Root integration validation follows on the same reviewed source and is recorded separately. Native interface Units 2/3 and following producer/host/library/performance tasks remain open; no push or new dependency installation.

## Upstream corpus and behavior-map integration details

2026-10-06: first corpus53d7c939 and mapping3ecc248, with final causal proof repaira395674, merged current root7a6ebf3 into the independent branch at62baa74. Branch and root full gates pass, SHA25660e183866005494c16ab575d0ac542e1c712835d01699bcd8c5d7e87f47e22ba and0714e37576f0495039c9a93534510a079cf8a9b15365835d438c8b9c47ed0952 in `/tmp/effra-upstream-{branch,root}-integration-gate.log`. Licensed746 reference files and26 license files are unchanged source inputs, never counted as746 native passes. Ten selected anchored behavior IDs distinguish covered/different/pending/unsupported semantics; selected generated Go/JS acceptance executes. Both independent Claude rounds consumed; finite Astra closure verifies the deep-JSON causal refusal and ten-hour virtual-time control. All31 legacy output nodes preserved; no performance claim or push. Further library/server behavioral ports extend this authority.

## Runtime source-module integration details

2026-10-06: completed the preceding source-split carried item. Reviewed0e4f5be's source/catalog/cache/inventory repair merged current root802737d into runtime-modules at637313b, passed that branch gate, then integrated the same tree and passed the root gate. Independent exact move proof preserves11 declarations/comments and7 unchanged lifecycle/scheduler files; selected module closures compile with inherited caches and trimpath controls. The original additive-upgrade failure remains recorded; no legacy output was deleted to bypass it.

Root live check builds and runs public examples/main.ef: output `Hello, Ada` then `Unknown user`. Its completed immutable module contains12 split runtime sources and no retired stdlib; root legacy stdlib and all31 pre-existing output nodes retain identical bytes/type/mtime. Receipt `/tmp/effra-runtime-split-root-live.json`, gate logs `/tmp/effra-runtime-modules-{branch,root}-integration-gate.log` with SHA2569c7ba1e0ae4c3f6f2cfaf5541e00d7778c87b753ae18f8492752a19eb60e51a8 and08d3b3a7719e965f1c4922061f4c8f753191483f3a34a8954a05600bef580596. Compiler73.716s/runtime2.845s are gate durations, not comparative performance claims. All started live-check processes exited. Source-module child task closed; parent reachability, binary size and complete standard-library tasks remain open. Publisher checkout retired after clean-state/ancestry/gate/tracker guards; branch and external receipts retained. Other reviewed feature lanes remain independent.

## Ordinary callable and row integration

2026-10-06: native interface Unit 2 is independently reviewed through `c9f3536` and fast-forward integrated over root `f782a1b`. Three compiling/gated subdivisions introduce ordinary callable values, finite named rows and occurrence-specific callback ownership, followed by coherent causal repairs. Decided by **Redesign From First Principles**: regular typed functions carry callback/library contracts; canonical callable values remain distinct from lazy invocation recipes and executed results. Provider/record placement, helper forwarding and callback factories use this common representation rather than erased Handler rows or a library-specific type mechanism.

Both counsel rounds and finite Astra closure are retained. Acquiring recovery alternatives reject a closing-scope escape while borrowed alternatives remain usable. Full returned pure/effect/nested callback types preserve their own rows separately from factory evaluation. Invalid pure timeout source produces EF106 before materialization instead of an invariant panic; actual MCP handles the next ping and valid factory queries, and framed Go/JS LSP handles invalid then repaired edits. Strict TypeScript consumers execute through the existing installed task checker. Final branch gate SHA256 `441f6765d52829e8b3cf685cec871e2a55fd3ada1d45c751bd61bc27ad1d9583`, relevant compiler/CLI race `dc469ae5fc0b6b25d9f371346dd997e724ca2f34a246c3d1062717041df76941`, full MCP race `0a5a0a05fc88bd47857773b94f5665bbaa9a75218692ddda28ee65b47e8daa85`; raw `/tmp/effra-callables-timeout-*.log` and durable reports/probes remain. Root exact integration validation follows on this reviewed source and is retained separately. No performance conclusion follows from gate durations.

The [finite corpus](../docs/specs/native-callables-unit2-corpus.md) is the delivered syntax/limit contract. Mixed symbolic/named callback alternatives remain conservative; anonymous closures, recipe-typed parameters and higher-rank polymorphism remain diagnostics. Original native interface parent stays open for demand-loaded bundled contracts, lossless cross-arena summaries and the shared first-order template/application engine. Generic Option, nullable native adaptation, codecs/HTTP/RPC/machines, selective binary reachability and benchmarks remain later units. All31 legacy output nodes verified unchanged after integration; no push, dependency installation or HITL closure.

## Ordinary actor ergonomics and reviewed contracts

2026-10-06: the owner rejected machine-only actors. A dedicated scoped Sol6.1-medium spike compared pinned ordinary class/Effect handlers, receive loops and native-resource ownership. [Research](../docs/research/actor-model-and-durability.md) and [actor contracts](../docs/specs/actors.md) now use ordinary service/impl/effect/uses/raises/scopes; machines add transitions/entry lifetimes through an adapter. No actor DSL is justified by this usage. Supervision, durable addressing and persistence stay separate. CLI/MCP inspect the common protocol/ownership model without requiring syntax or executing behavior. Actor and machine implementation tasks remain open.

The initial 19-file contract commit dd6f59e passed the full exact root gate, `/tmp/effra-ordinary-actor-contract-gate.log`, SHA256 `1c7ffef536577774e5126d55f2f9f61251b62ba85fe15f10b21957609c0df513`. Independent Claude2 design counsel retained six finite actor corrections and cleared the revised layer design in its final layer round. Root applied protocol-kind/annotation identity, full Exit observation, conservative spawn terminal rows, fallible-post refusal, dependency-based self-wait checks and deferred receive acceptance. Adoption now compares semaphore-plus-Ref/resource composition, with causal actor-specific admission/cancellation/stop/inspection controls. The corrected unit requires its own full gate and final actor design recheck, retained outside source. No actor runtime, executable adoption, isolation, durability or performance support is claimed by these documentation gates.
