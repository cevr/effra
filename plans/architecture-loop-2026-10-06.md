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

Go-like direction, transformation codec contract and machine plan/runtime/conformance tasks are recorded in the specs; tracker now contains29 valid issues, including reusable generic data and payload recovery prerequisites. Gleam/ReScript/TypeScript source/test comparison is in `docs/research/host-language-compilation.md`. XState v6/Effect Machine/Serde/Effect source comparison and independent design counsel are recorded in `docs/research/machines-and-transforming-codecs.md`; implementation remains open. The `@xstate/effect` requirement walker at the pinned branch stops child-machine inference at10 levels; Effra must diagnose an exhausted checking budget rather than silently erase a requirement.

CLI/MCP apply `f6c2f65` passed its gate and independent public probes. Astra review found one test-audit blocker: the new nested-row regression also passes against the old implementation because declared rows mask the intended bound. Replace it with isolated body and nested-name fixtures, asserting their dimensions; obtain second-model counsel before the consolidated fixup and merge. Production admission repairs were independently verified, not yet integrated.

CLI/MCP repair isolation: `workrift list` rejects the transferred unknown registry marker; `df -T` confirms the source lives on ext4. Use architecture-loop's Git worktree fallback at the exact absolute sibling path and copy its verified locked dependencies. No warm source, registry entry or unrelated dotfiles is changed.

Guardrails added: pending implementation receipts. Each missed static guarantee needs its own rejected-source test, and each bounded analysis needs adversarial scaling coverage.

### Carried

| Decision | From | Files | Status |
| --- | --- | --- | --- |
| Complete coverage, architecture/package/guardrail sweeps and per-batch second-model counsel | Prior establishment | All source areas | Open; schedule around in-flight implementation |
| Decide ergonomic candidates from independent counsel and source evidence | Owner request | Specs and shared type/function interfaces | Open; Claude2 session active |
| Obtain matched frontend/import/edit and server receipts | Prior ledger | Benchmarks and compiler | Open; no performance superiority claimed |
| Preserve distinctions between static proof, trusted adapter and runtime policy | All batches | Contracts, tooling and docs | Open until new capabilities have explicit receipts |

Counsel: requested second Claude instance active in Herdr pane `w1G:p2`, agent `effra-counsel`. Initial language-design counsel is separate from the required review of each completed apply batch.

Design counsel received in `/tmp/effra-claude2-language-counsel.md`. Root independently reproduced the Handler helper erasure: CLI checks the unprovided Users program and reports an empty main requirement row. Adopt the regular canonical type/function/finite-row mechanism in [the abstraction contract](../docs/specs/language-abstractions.md), decided by **Redesign From First Principles**. Keep explicit run. Carry generic outcomes and effectful recovery as finite subsequent contracts. Reject flattening composite Cause into a four-way Exit and defer new acquire/provide keywords or impl renaming until ordinary interfaces and real consumers justify them. Enum representation remains a measurement candidate, not a presumed speedup.

Framework fixture evidence records pinned Effect's wrong-method response as404. Adopt that explicit comparator profile in the new native server spec, decided by **Never Block on the Human** and matched-behavior requirements. No existing Effra endpoint wire contract changes: the richer profile is not implemented or published yet. Unknown-field and malformed-envelope policies must likewise be explicit; native conformance cannot claim stronger validation than the matched comparator provides.

Merges: none in this pass yet. No application changes have been pushed.

Live checks: baseline `./scripts/gate.sh` and `go test -race ./...` pass on Linux; a temporary public provider program executes identically on Go/JS and its CLI graph exactly matches stdio MCP. These checks do not approve the unmerged branches.

Direction update gate: `./scripts/gate.sh` exited0 on the unchanged integration source while documentation evolved, log `/tmp/effra-architecture-direction-gate.log`; source tests, CLI/MCP smoke and HTTP lifecycle smoke all pass. Current tracker and whitespace checks also pass after the new27-issue graph. This is not a gate for unmerged scheduler/ownership/benchmark implementations.

## Close

Unswept directories, structural findings, unfinished library contracts, To survey and Carried rows remain. No full architecture-loop completion is claimed.
