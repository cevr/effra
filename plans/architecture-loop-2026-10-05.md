# Architecture loop — 2026-10-05

Goal: establish the project's north stars and prior arts as the basis for building its glossary and architecture.

Scope: establishment only. This ledger does not claim a completed architecture sweep or the architecture-loop close rule. Runtime integration is continuing separately in the managed-runtime Rift; it must refresh baseline/gap receipts when merged.

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

`PRIOR_ARTS.md` is the authoritative comparison index. Its Settled section carries adopted/rejected verdicts with implementation status; its To survey section carries open research. Establishment reused the inspected sources in `docs/design.md` and `docs/interop.md` and refreshed focused Borgo, Effect and Zerolang source reads. It did not run their examples or resolve their remaining questions.

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

| Decision | From | Files | Status |
| --- | --- | --- | --- |
| Refresh ownership/type/inspection gap receipts after managed runtime integration | setup | NORTH_STAR.md, PRIOR_ARTS.md, this ledger | open |
| Run the first full architecture pass over all unswept directories and open prior-art questions | setup | Architecture areas and research in PRIOR_ARTS.md | open |
| Establish matched end-to-end build fixtures and select evidence-based budgets | setup | Compiler/build performance sweep | open |

## Close

Establishment complete when both project files, canonical glossary, repository pointers, draft owner questions, and a passing gate exist. The full architecture loop remains open: unswept directories, To survey questions, and carried rows remain.
