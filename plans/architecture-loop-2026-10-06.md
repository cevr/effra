# Architecture loop — 2026-10-06

Goal: retain the simplicity of Go, the guarantees of Effect, and the added benefits of ADTs and useful abstractions, similar to Borgo; fully implement the authorized Wayfinder specs and extend them with evidenced improvements.

This is an active loop ledger, not a completion receipt. The canonical prior-art file remains `PRIOR_ARTS.md`, matching project instructions. The previous ledger preserves historical receipts; current decisions and new pass coverage live here.

## Baseline

- HEAD: `00ca167` before this documentation refresh. Integration source is the transferred, independently gated foundation baseline; ownership and scheduler changes remain isolated.
- Count: `git ls-files ':(glob)cmd/**/*.go' ':(glob)internal/**/*.go' ':(glob)internal/**/*.mjs' ':(glob)runtime/**/*.go' ':(exclude,glob)**/*_test.go' | xargs wc -l`.
- Count excludes tests, generated output, dependencies, examples and upstream reference tests. JavaScript lifecycle policy is included because it is runtime source.

| Package | Lines | Files |
| --- | --- | --- |
| cmd/ef | 426 | 1 |
| internal/compiler | 4,266 | 11 |
| internal/mcp | 453 | 1 |
| runtime/effra | 708 | 6 |
| Total | 5,853 | 19 |

## Coverage

| Directory | Files | Mark | Pass |
| --- | --- | --- | --- |
| cmd/ef | 1 | swept pass1 | Complete source/package review in pass1/cli-mcp-review.md; source-admission fix pending |
| internal/compiler | 11 | unswept | Focused data/provider/ownership reviews only |
| internal/mcp | 1 | swept pass1 | Complete source/package review in pass1/cli-mcp-review.md; admission/detail-bound fixes pending |
| runtime/effra | 6 | unswept | Runtime tests and scheduler review only |

## Prior art

| Idea | Source | North star | Verdict |
| --- | --- | --- | --- |
| First-class closed data and exhaustive patterns in direct Go-targeted programs | Borgo pinned source and examples | Algebraic data and useful abstraction | Adopt direction; implemented data is separately verified, broader abstraction seam remains open |
| Explicit effect rows and nominal identity | Effect variance/tag/inference machinery | Explicit contracts and clear guardrails | Adopt native representation; finite reusable row parameters need a concrete spec |
| Pinned test corpus plus runnable ports | Effect4.0.1 test/type-test source | Owned lifetimes; clear guardrails | Adopt; import/mapping underway, reference copies do not establish passes |
| Runtime policies hidden behind reusable typed interfaces | Effect codecs/HTTP/RPC and application usage | Go-like simplicity through regular abstractions | Adopt; native server task graph owns implementation |

## Project sweeps

| Sweep | Pass | Result | Done when met? |
| --- | --- | --- | --- |
| Compile performance | 1 | Ownership review found exponential type-path expansion and quadratic summary passes | No; repair and quiet measurements pending |
| Application retention | 1 | Owner added small binary requirement; current emission copies every runtime source file | No; reachable-module implementation and size/symbol/dependency receipts pending |
| Lifecycle behavior | 1 | Linux baseline gate/races pass; scheduler and ownership remain in review | Existing subset only |
| Inspection parity | 1 | Public configured-provider Go/JS outputs agree; CLI/MCP graphs exactly equal, 45 nodes/58 edges | Existing subset only |
| Language ergonomics | 1 | Claude2 counsel active through Herdr; shared interface/codec/server specification recorded | No |
| Upstream behavioral conformance | 1 | Pinned 746-file reference corpus identified for import | No |
| Matched server performance | 1 | Actual framework fixtures and protocol corpus under implementation; host contention observed | No |

## Owner questions

No new blocking question. The requested north-star direction is owner-established. Existing Wayfinder HITL decisions remain open; implementation and reversible design choices continue under **Never Block on the Human**. No push or deployment is authorized.

## Pass 1

Reports: `~/.cache/architecture-loop/effra/pass1/`. Earlier remote source/review receipts remain under `~/Developer/personal/.effra-research/` and will be linked by the sweep brief.

Verdict: structural work remains. The loop is open.

| Batch | Workspace | Items |
| --- | --- | --- |
| Ownership repair | foundation-ownership | Seven independent correctness/scaling findings and isolated acceptance tests |
| Causal scheduler | foundation-testing | Preserve transferred edits; Linux public causal probes, cleanup causes and virtual-time authority |
| Upstream/framework fixtures | server-benchmarks | Pinned test import, actual Effect HTTP/RPC fixture and measurement admission |
| Native library contracts | Subsequent isolated units | Bundled interfaces, typed functions, codecs, HTTP and RPC; no endpoint-specific compiler facade |
| CLI/MCP boundary repair | cli-mcp-admission | Reject FIFO before blocking source open; bound nested body/contribution rows; paired public acceptance |
| Checked machines | Subsequent isolated units | Flat checked topology, ordinary ADTs/functions, owned actor runtime and target conformance after shared foundations |

| ID | Candidate | North star | Files | Risk | Status |
| --- | --- | --- | --- | --- | --- |
| A1 | Preserve Go-like simplicity through regular abstractions | Go-like simplicity through regular abstractions | NORTH_STAR.md, PRIOR_ARTS.md | Low | Direction recorded; decided by owner instruction and **Redesign From First Principles** |
| A2 | Repair proven ownership escapes and false borrow rejection | Owned lifetimes | compiler provenance/checker | High | Open implementation; independently reproduced |
| A3 | Bound ownership traversal and summary propagation | Exceptionally fast compilation | compiler provenance/checker | High | Open implementation; small source causes multi-second checking before repair |
| A4 | Use reusable typed library interfaces | Algebraic data and useful abstraction | compiler, bundled runtime | High | Spec/task graph established; implementation pending foundations |
| A5 | Preserve callback rows through helper parameters | Explicit contracts and clear guardrails | compiler function types, HTTP | High | Root confirmed missing Users still checks; required regression in native-interfaces |
| A6 | Preserve root parameter projections and already materialized borrowed owners | Owned lifetimes | compiler provenance | High | Rereview of668e5a9 blocked; O8/O9 in remote-ownership-rereview.md |
| A7 | Bound primitive-only shared type traversal | Exceptionally fast compilation | compiler provenance | High | Rereview of668e5a9 blocked; O10 tiny source still checks in seconds |
| A8 | Allow partial time advance while cleanup sleeps past the target | Owned lifetimes | runtime scheduler/scope | High | Independent public probe blocks Adjust10 while cleanup waits until20; repair required |
| A9 | Atomically recheck scheduler quiescence before selecting/finalizing time | Owned lifetimes | runtime scheduler | High | Repeated public join/deadline probes miss intermediate continuation sleeps; repair required |
| A10 | Reject special source nodes before blocking I/O and bound all nested inspection rows | Operability and agent introspection | cmd/ef, internal/mcp | Medium | CLI/MCP sweep complete; two reproduced P2 findings in pass1/cli-mcp-review.md; repair pending |
| A11 | Make state machines easy to express and inspect using checked data, transitions and owned work | Algebraic data and useful abstraction; Owned lifetimes | Shared semantic model and bundled machine runtime | High | Owner direction recorded; Effect Machine/XState source-and-test survey active; finite spec next |
| A12 | Derive structural codecs while retaining typed bidirectional transformations | Go-like simplicity through regular abstractions; Explicit contracts | Canonical types, bundled codecs and native server spec | High | Owner direction recorded; Rust/Serde and pinned Effect transformation survey active |
| A13 | Learn from Gleam/ReScript/TypeScript host compilation without inheriting tedious bindings | Adoption through host interop; Exceptionally fast compilation | Imports, backend, source mapping and build cache | Medium | Current sources fetched and pinned in PRIOR_ARTS; initial source/test inspection complete, focused comparison remains open |
| A14 | Prefer Go-like ordinary behavior and use XState v6 as main machine reference | Go-like simplicity through regular abstractions | Machine/codec specs, PRIOR_ARTS | Medium | Owner tiebreak recorded; active PR5543/next pinned2146ae26, Claude2 counsel redirected to v6; earlier v5 conclusions need revalidation |
| A15 | Make benchmark admission and measurements reflect the declared workload | Measured evidence; Native Go server programs | scripts/server_benchmark.py | High | Root bounded probes at3450286 confirm new connection per request, omitted offered queue latency and flag-only rigorous admission. Full independent review/counsel and fixup required; no scoring |
| A16 | Admit effectful machine steps and completion functions | Explicit contracts; Go-like local readability; Owned lifetimes | Machine plan/runtime specs and ordinary function rows | High | Owner clarification supersedes pure-only counsel: serialize actor-owned evaluation, preserve rows, commit after successful cleanup, never imply external rollback; acceptance added |
| A17 | Keep application binaries small through reachable module emission | Small application binaries | Bundled interfaces/runtime, native emission, size fixtures | High | Owner direction recorded; unused stdlib imports/init and fluent API retention have explicit negative checks in binary-reachability spec/task; implementation pending shared interface seam |
| A18 | Use raises consistently for expected failure rows | Explicit contracts; Go-like local readability | Shared parser, authored source, examples and docs | Low | Done e25c7f1,5185cc6,c78e5a6; integrated4abf034 after clear Astra/Claude2 review, both merge gates and live smoke; clean registered workspace removed |

Go-like direction, transformation codec contract and machine plan/runtime/conformance tasks are recorded in the specs; tracker now contains30 valid issues, including reusable generic data, payload recovery and binary-reachability prerequisites. Gleam/ReScript/TypeScript source/test comparison is in `docs/research/host-language-compilation.md`. XState v6/Effect Machine/Serde/Effect source comparison and independent design counsel are recorded in `docs/research/machines-and-transforming-codecs.md`; implementation remains open. The `@xstate/effect` requirement walker at the pinned branch stops child-machine inference at10 levels; Effra must diagnose an exhausted checking budget rather than silently erase a requirement.

CLI/MCP apply `f6c2f65` passed its gate and independent public probes. Astra review found one test-audit blocker: the new nested-row regression also passes against the old implementation because declared rows mask the intended bound. Replace it with isolated body and nested-name fixtures, asserting their dimensions; obtain second-model counsel before the consolidated fixup and merge. Production admission repairs were independently verified, not yet integrated.

Foundation rereviews: ownership `859f172` repairs original cases but is blocked by mixed65-field proof erasure and misattributed eager timeout arguments (`pass1/ownership-final-review.md`). Scheduler `ce3dbbd` repairs original cases but is blocked by nested cleanup/Scoped caller propagation, already-closing timeout cleanup and two-branch completion wait composition (`pass1/scheduler-final-review.md`). Follow-up repairs remain isolated. Independent scheduler gate also hit a60-second generated-Go watchdog that includes compilation; cause is undiagnosed, distinct from the deterministic managed-wait deadlocks. No merge approval follows from the implementation owners' earlier green gates.

Benchmark `3450286` independent review identifies nine fixture, lifecycle, measurement, admission and provenance findings (`pass1/benchmark-final-review.md`). Root verified the core source paths and bounded harness observations; Claude2 round1 is consolidated in `pass1/apply-benchmark-fixup.md`. Four gated repair units remain pending while the implementation owner completes the requested keyword migration. Preserve no-score status and repair before integration.

Benchmark repairs through9be0484 have a green final gate but fail final independent review (`pass1/benchmark-fixup-final-review.md`). Real five-cohort probes still find wire differences; synthetic negative controls expose early-success lifecycle acceptance, readiness child leakage, flag-only score reporting and permissive offline provenance. Root confirms the schema2 producer/schema1 consumer mismatch, report bypass, missing framework gate stage and incomplete offline pin/license checks directly in source. Source-backed746-file/26-license validation passes; that does not establish offline authenticity or native conformance. Claude2 fix-only round2 is in progress; consolidate all remaining clauses before further repair. No scored load, merge or performance superiority claim.

Claude2 benchmark round2 is complete, changes required. Adopt R1–R7 and Astra F1–F8; additional independent probes show duplicate-header divergence, actual Effect-node HTTP/RPC terminal publication before cleanup, and acceptance of fake204success. Accept optional O1 explicit matched cancellation grace, O2 cohort/scenario exit codes, O3 accurate early-exit wording, O4 explicit transport-header presence versus connection-behavior policy. Assigned only the first bounded provenance repair checkpoint (offline release identity/licenses, safe owned destination, schema consumer and executed package provenance); all runner/fixture/scoring clauses remain open. No third counsel round for this batch; root/Astra verify the remaining repairs. `pass1/apply-benchmark-provenance-final.md` holds the scoped assignment and full triage. Claude2 proceeds to separate diagnostics round1.

Shared diagnostics froze at e70d429 and passes implementation plus independent full gates, but review reproduced late realpath resolution attaching analyzed bytes to a replacement symlink destination. Preserve the requested lexical document URI with the byte snapshot; retain real CLI/MCP Unicode, limit and FIFO acceptance probes as permanent guards. `pass1/semantic-diagnostics-review.md` distinguishes this product defect from missing durable tests. First counsel round and consolidated repair precede integration; formatter syntax work stays separate.

An additional evidenced full-type snapshot requirement is producer identity: the exact owned-sibling source has the same source/import revision under de3ae027 and bfe0862 while admission differs. Current semantic.go hashes source bytes, with import contracts added separately. Keep that useful source identity, but qualify cached/type-reference facts with target/schema/compiler identity and report its strength. Added to the future canonical full-type unit, not the frozen diagnostics review. Decided by explicit inspectable contracts; no per-query Git or unmeasured build-cost addition.

Diagnostics counsel round1 adds independently proven lone-CR range/comment-admission inconsistency. Accept R1–R3; choose explicit LF/CRLF source support and actionable EF001 for standalone raw CR, while LSP conversion recognizes all editor line boundaries. Formatter follows the shared lexer policy. Optional notes: document unchecked suppression validation as unavailable, text column units and CLI exits/stdout; test built-in severity metadata exhaustively; carry Windows runtime URI verification without claiming it. Full triage in pass1/apply-diagnostics-fixup.md; one consolidated repair next.

Owner explicitly authorizes parallel claude2 Herdr sessions and closing their panes when complete (2026-10-06). Created two background sibling panes in the existing root tab with CLAUDE_CONFIG_DIR=/home/exedev/.claude2: effra-tooling-counsel w1G:p3 session364c6ca8-f8e9-4ad4-9939-ebae4f06fc60 for tooling contract design, and effra-native-counsel w1G:p4 session92032c01-111b-4cd4-8988-0ff6950fc881 for pinned native conformance mapping. Both are observed working, read-only scopes with separate report paths; close only these owned panes after retaining reports. Keep main continuation w1G:p1 and existing counsel w1G:p2 intact. No extra benchmark counsel round is started.

Ownership follow-up `4a534f5` is frozen with reported full gate and race checks but fails independent review: a capped same-path conditional loses its owned alternative when a borrowed alternative remains. Public native execution confirms a closed File reaches use. Require complete selected-path evidence, not mere existence of a concrete fact, before dropping bounded uncertainty (`pass1/ownership-followup-review.md`). The timeout eager-argument fix passes. CLI/MCP consolidated fixup accepts the isolated body/name regression and Unix-constrained FIFO tests from Claude2; it rejects speculative path-security expansion and unproven project.tests refactoring. The shared reader remains a cooperative workspace boundary, not a filesystem security sandbox.

Focused effectful-step counsel is triaged in the machine spec and research note: adopt explicit evaluation/entry cleanup order, call-local failure attribution, non-draining stop, reserved completion admission and nonblocking snapshots. Reject the suggested propagated call chain as a general deadlock guarantee, and qualify removal of core scheduling dependencies from actors. These are implementation contracts with direct acceptance cases, not upstream-proven guarantees. Decided by explicit contracts, measured evidence and Go-like local readability.

Pinned Effect4.0.1 source inspection through repo confirms directional codec requirements but fixed schema-issue failures. Effra's requested named transformation failures require separate source tests and explicit HTTP mappings, with a finite terminal error-encoder fallback; a service outage during decoding must not be silently classified as malformed input. Added this distinction to the native server spec and codec research without claiming runtime support or adding work to benchmark cohorts. Decided by explicit contracts and truthful upstream comparison.

Minimal binary baseline at `ff4b487`: public empty Effra entry is5,532,694 bytes versus1,891,532 for a bare Go size control under identical Go1.27.0/linux-amd64/CGO0/trimpath flags. The control does not supply managed guarantees; no full-gap attribution is made. `net/http` and `crypto/tls` initialization symbols remain linked despite no HTTP use. Raw receipt `/tmp/effra-size-baseline-2026-10-06/receipt.json`; tracked interpretation `docs/research/native-runtime-retention.md`. No build-time or runtime-performance claim.

CLI/MCP repair isolation: `workrift list` rejects the transferred unknown registry marker; `df -T` confirms the source lives on ext4. Use architecture-loop's Git worktree fallback at the exact absolute sibling path and copy its verified locked dependencies. No warm source, registry entry or unrelated dotfiles is changed.

Guardrails added: pending implementation receipts. Each missed static guarantee needs its own rejected-source test, and each bounded analysis needs adversarial scaling coverage.

### Carried

Owner requests an Effra formatter. Added finite pure-printer/CLI/MCP and subsequent LSP formatting tasks, decided by Go-like regularity and source authority. Current syntax groups declarations and retains comments separately, so ordered comment-aware syntax is a prerequisite; printing grouped semantic data would reorder source. Preserve line-sensitive lint-directive meaning as well as tokens. Formatting runs without typechecking/import loading, with one versioned style, bounded previews and stale-write guards. Go/Gleam source comparison is recorded in PRIOR_ARTS. No formatter command is claimed implemented.

Started independent formatter syntax unit in registered ext4-fallback worktree canonical-formatter fromd399b29, with locked dependencies copied. Its pure printer does not depend on semantic diagnostics; subsequent CLI/MCP adapters wait for reviewed diagnostic identity/position helpers. This parallel unit preserves frozen diagnostics and ownership branches, decided by Never Block on the Human. No command/example migration or editor capability is claimed yet.

Owner requests user-defined lint rules. Added `docs/specs/custom-lint.md` and a finite task over canonical types/LSP: versioned fact API, optional executable packs, Go SDK, source-fixture tests and shared rule reporting. Decide execution separately from compiler admission; custom failures cannot erase compiler errors. Refreshed the clean authorized lint-plugin checkout e3a5080→ca2ce6a via fast-forward and inspected rule metadata/testing plus new alias/evasion policies. No feature edits or dependency install in that warm source. Actual custom-rule implementation remains open; current diagnostics work keeps the producer identity extensible.

Owner tooling direction adds three finite tasks: shared diagnostics, canonical full-type/binding inspection and a thin stdio LSP adapter. Requirements and pinned protocol evidence are in `docs/specs/semantic-tooling.md`. Diagnostics can proceed independently; full types consume the shared native-interface representation instead of building a competing string-based analyzer. Decided by one inspectable contract and Go-like simplicity. No implemented LSP capability is claimed yet.

CLI/MCP final fix-only counsel clears42503d1; root independently confirms the split-fixture mutation controls. Integrating with raises now. Cosmetic failure-output compression is rejected for this batch: it changes no behavior and retaining the complete failed map aids diagnosis; the guard-specific dimension assertions provide the focused explanation. Ownership de3ae027 has raw full-gate/race receipts and is under independent review. Scheduler620e145 still needs continuation isolation: Claude2 round2 and root reproduce cleanup fork children inheriting their caller's wait token. Repair remains isolated; no third Claude2 round is requested.

| Decision | From | Files | Status |
| --- | --- | --- | --- |
| Complete coverage, architecture/package/guardrail sweeps and per-batch second-model counsel | Prior establishment | All source areas | Open; schedule around in-flight implementation |
| Decide ergonomic candidates from independent counsel and source evidence | Owner request | Specs and shared type/function interfaces | Open; Claude2 session active |
| Obtain matched frontend/import/edit and server receipts | Prior ledger | Benchmarks and compiler | Open; no performance superiority claimed |
| Preserve distinctions between static proof, trusted adapter and runtime policy | All batches | Contracts, tooling and docs | Open until new capabilities have explicit receipts |

Counsel: requested second Claude instance active in Herdr pane `w1G:p2`, agent `effra-counsel`. Initial language-design counsel is separate from the required review of each completed apply batch.

Design counsel received in `/tmp/effra-claude2-language-counsel.md`. Root independently reproduced the Handler helper erasure: CLI checks the unprovided Users program and reports an empty main requirement row. Adopt the regular canonical type/function/finite-row mechanism in [the abstraction contract](../docs/specs/language-abstractions.md), decided by **Redesign From First Principles**. Keep explicit run. Carry generic outcomes and effectful recovery as finite subsequent contracts. Reject flattening composite Cause into a four-way Exit and defer new acquire/provide keywords or impl renaming until ordinary interfaces and real consumers justify them. Enum representation remains a measurement candidate, not a presumed speedup.

Framework fixture evidence records pinned Effect's wrong-method response as404. Adopt that explicit comparator profile in the new native server spec, decided by **Never Block on the Human** and matched-behavior requirements. No existing Effra endpoint wire contract changes: the richer profile is not implemented or published yet. Unknown-field and malformed-envelope policies must likewise be explicit; native conformance cannot claim stronger validation than the matched comparator provides.

Merges: raises migration integrated at4abf034. Independent Astra full gate and six exact-span public probes pass; Claude2 approves with no required findings, verifies all authored examples against both targets and preserves expected unsupported-target diagnostics. Branch merge gate `/tmp/effra-raises-branch-integration-gate.log` and integration gate `/tmp/effra-raises-integrated-gate.log` both exit0, including real Go/JS execution, CLI/MCP and HTTP lifecycle smoke. The registered clean raises worktree was removed after exact-tip/ancestry checks; source branch and review receipts remain. No application changes have been pushed.

Raises counsel optional notes are not scope expansions: retain canonical failure-before-service row ordering and the existing generic parser diagnostic for malformed reversed rows; accepting a second ordering is unnecessary, and valid-order legacy source already has a targeted migration diagnostic. Retain the small permanent token-length/line/actionable-message regression as written; independent exact offset/column probes cover the three declaration positions and Unicode prefix. No observed span defect justifies another implementation round. Historical per-commit gates remain implementer-reported; the exact reviewed head and both integration gates were independently run. Later foundation branch fixtures must migrate their authored failure rows during integration; pinned upstream bytes and historical scratch receipts remain unchanged.

Live checks: baseline `./scripts/gate.sh` and `go test -race ./...` pass on Linux; a temporary public provider program executes identically on Go/JS and its CLI graph exactly matches stdio MCP. These checks do not approve the unmerged branches.

Direction update gate: `./scripts/gate.sh` exited0 on the unchanged integration source while documentation evolved, log `/tmp/effra-architecture-direction-gate.log`; source tests, CLI/MCP smoke and HTTP lifecycle smoke all pass. Current tracker and whitespace checks also pass after the new27-issue graph. This is not a gate for unmerged scheduler/ownership/benchmark implementations.

## Close

Latch follow-up is complete and integrated616f0b4. Accepted Claude2 N1–N3: remove misleading internal invalid-as-complete fallback and redundant private guards, name errInvalidLatch accurately, and remove the subsumed nil-only test. Reject N4 identical Go/JS prose: constructor guidance is useful for native handles; defect/cleanup behavior is the shared contract. Final fix-only counsel is clear at65a8882 with load-bearing public guard mutations. Branch/root full gates exit0, integrated runtime race20 passes, public causal Go/JS receipts both pass at revisionbdce4314956d3631fd4f9f195b74bc26fff16d021c02108f44a03f36fb991dae. Raw `/tmp/effra-latch-{branch-integration-gate,integrated-gate,integrated-live}.log` and causal JSON receipts retained. Closed finite foundation-testing task; no general foreign-operation quiescence claim. Root gate predates only subsequent documentation edits; current tracker separately validates36 issues.

Scheduler integratedcf3783c after final ac8f1e2 production approval and reviewed fast-first test additionf03f340. Branch/integration full gates exit0; integrated all-package race exits0. Raw `/tmp/effra-scheduler-{branch-integration-gate,integrated-gate,integrated-race}.log`; both `ef test examples/causal.ef --target go|js` runs pass all5 cases, common revisionbdce4314956d3631fd4f9f195b74bc26fff16d021c02108f44a03f36fb991dae. Verified clean independent checkout was trashed only after ancestry checks and a verified all-ref bundle at `pass1/foundation-testing-final.bundle`. No push.

Root runtime follow-up sweep found zero native Latch handles panic on direct signal, interrupt on live managed await, but falsely succeed on virtual managed await. New isolated guardrail batch4a27f3b makes invalid handles consistently fail and documents constructor use. Its fresh-worktree gate revealed two scheduler fixtures omitted their scratch-parent creation; corrected using the established runJS pattern. Original red logs, managed-mode red controls, runtime race20 and corrected fresh-output full gate retained under `/tmp/effra-latch-invalid-*`. Claude2 reviews this new handle/fixture batch; no third continuation review is requested.

Ownership de3ae027 remains blocked after final independent review: changing retained fact groups exposes a nested64-field escape previously rejected by4a534f5. Root independently reproduced both exact-head outcomes. A matching descendant cannot prove completeness of the selected subtree; repair is assigned at the representation/projection owner, with nested record/enum and safe-sibling controls. Review `pass1/ownership-completeness-final-review.md`; no merge approval. Helper/recipe negatives masked by whole-argument rejection do not count as substitution proof.

Subsequent bfe0862 fixes the nested projection but remains blocked by independent owned-sibling and borrowed-parameter-summary regressions. Root reproduced both before/after outcomes: a retained child-prefix marker fails to cover new omissions outside that prefix. Assigned a coherent bounded coverage representation repair across homogeneous/mixed normalization and consumers, with every-position/order/parameter controls. `pass1/ownership-subtree-final-review.md` and `apply-ownership-coverage.md` carry exact evidence; no merge or static guarantee claim from green implementation gates.

CLI/MCP admission and nested inspection guards are integrated at30cda4f (reviewed42503d1, branch integration1ba50d5). All new authored fixtures use raises. Branch and integration full gates exit0 (`/tmp/effra-cli-mcp-{branch-integration,integrated}-gate.log`); real Go/JS, MCP and HTTP smoke pass. Re-ran the complete original plus four isolated single-guard mutation table after migration: each fails exactly its intended101 case(s), while the unchanged implementation passes. Raw `/tmp/effra-cli-mcp-migrated-*.log`; focused integrated FIFO/inspection live checks also pass. Exact registered clean worktree removed with ancestry guard; branch and review receipts retained. No push.

Generated backend diagnostic is retained at `/tmp/effra-scheduler-build-diagnostic/`: exact1024-signal source, emitted Go, executable, commands, hashes, stage receipt and compiler profile. Public compile/run passed; backend build47.754s and direct execution3.894ms on the shared host are diagnostic observations, not scored comparisons. `docs/research/generated-go-build-cost.md` records77.61% cumulative sampled compiler CPU in ir.Reassigned and a direct-execution lowering candidate. Implement only after matched semantic/cost evidence.

Unswept directories, structural findings, unfinished library contracts, To survey and Carried rows remain. No full architecture-loop completion is claimed.
