# Wayfinder archive

Until 2026-10-09 the repository carried a second copy of the Wayfinder map: a local Markdown tracker and a hash-pinned migration apparatus that bound it to the GitHub issues. GitHub is now the only map (see [README](README.md)). The retired files remain reachable in Git history; the last commit that contains all of them is `43af514c` (`git show 43af514c:<path>`, or `git checkout 43af514c -- <path>` in a scratch worktree).

## What was retired

| Path at `43af514c` | What it recorded |
| --- | --- |
| `docs/wayfinder/issues/*.md` | The local tracker: 74 issues, one file per issue with a JSON metadata line (immutable slug `id`, `title`, `status`, `labels`, `parent`, `assignee`, `blocked_by`), the question/body text and `## Resolution comments`. `map.md` was the local map body. |
| `scripts/wayfinder.py` | Local frontier/list/check over those files, replaced by the GitHub-backed `cmd/wayfinder`. |
| `scripts/wayfinder_migration.py`, `scripts/test_wayfinder_migration.py`, `scripts/test_wayfinder_hosted_reconciliation.py` | The deterministic preparer and checker for the hosted migration, and their tests. |
| `docs/wayfinder/migration/snapshots/hosted-run-binding-2026-10-08/` | The historical hosted-run binding of 2026-10-08: a 72-issue input bundle (`inputs.tar`), the hosted mapping in which five identities were verified (map #1 and children #2, #3, #4, #73) and the rest pending, the preparer source and the research record. It preserved the original 71-issue/183-edge input wave from source commit `4627f414` separately from the `run-binding` extension to 72 issues/184 edges. |
| `docs/wayfinder/hosted-identities.json` | The 2026-10-08 hosted mapping used by that run: five verified identities, every other local issue `pending`. |
| `docs/wayfinder/migration/*.json` | The preparer outputs of that run: per-issue payloads, resolution comments, parent and blocked-by edges, unresolved references, input manifest and run manifest. |
| `docs/wayfinder/migration/snapshots/github-wayfinder-current-intake-2026-10-08/` | The current identity intake: a read of all 75 GitHub issues on 2026-10-08 (74 canonical identities; closed #63 excluded as a duplicate of #62; issue #5 kept as local id `001`), re-read byte-for-byte on 2026-10-09. |
| `docs/wayfinder/migration/current-hosted-identities-*.json`, `snapshots/current-wayfinder-map-2026-10-09*/` | The 2026-10-09 mapping from every local slug to its GitHub issue number, bound to captured hosted body hashes and local source hashes, and the regenerated outputs (`-source-reconciled` carried local notes for `native-execution-lowering` and `numeric-arithmetic` pinned to compiler `07d861f0`). |
| `docs/wayfinder/hosted-run-reconciliation.json`, `docs/wayfinder/receipts/` | GET-backed reconciliation of the generalized `run` research ticket (#73): the public JSON responses and receipts binding issue 73, its map membership and the original 71-item verifier. |

## Reading old references

GitHub issue bodies created by the migration start with HTML markers such as `<!-- effra-wayfinder-id: layer-plans -->`, `effra-wayfinder-source-sha256` and `effra-wayfinder-source-commit`. The id is the local slug (the file name under `docs/wayfinder/issues/` at `43af514c`); the hash and commit identify the local source the body was generated from. They are provenance only.

Local text that never reached GitHub was diffed issue by issue before retirement and handed to the orchestrator as pending GitHub updates; state, parent and blocked-by edges already matched GitHub for all 74 issues.
