# Local Markdown tracker

The checked-in tracker is the local authoring surface. The repository also retains a dated hosted-input snapshot; its identities are historical evidence and do not describe the current captured identity mapping. Commands here operate on local files and do not query GitHub.

## Wayfinding operations

Issues live in `issues/*.md`. Each has a JSON metadata line in an HTML comment: immutable `id`, `title`, `status` (`open`/`closed`), `labels`, `parent`, `assignee`, and `blocked_by` issue IDs. This tracker has no native dependency API; `blocked_by` is its explicit fallback convention.

The map is the issue labelled `wayfinder:map`. Its body is the canonical index. Child decision tickets hold questions; resolutions are appended under `## Resolution comments`, never inserted into the question.

Query `python3 scripts/wayfinder.py frontier`: open, unassigned children whose blockers are closed, ordered by ID. `list` shows all children, including claimed/blocked tickets. `check` validates identities, references, and cycles. Claim a ticket by setting its assignee before work. Close only after resolution; append its named link and gist to the map. IDs are immutable; refer to titles in prose.

Serialize tracker changes through Git. Re-read metadata before editing; do not overwrite another session's claim. This local tracker is intended for one workspace, not distributed concurrent writes.

## Historical hosted migration input

The accepted `hosted-run-binding-2026-10-08` input records the hosted mapping captured for the historical migration on 2026-10-08. Its five verified identities (the map and children 2, 3, 4, and 73) describe that snapshot only. The archived statement that later publication could fill pending entries is also a record of that historical state, not a claim about the current captured identity map.

Run the historical check from the repository root:

```sh
python3 scripts/wayfinder_migration.py --input-snapshot hosted-run-binding-2026-10-08 --check
```

The selected bundle contains 72 issue inputs, the historical hosted mapping, the preparer source and the research record. The checker validates the archived bytes and does not fall back to live issue, mapping, preparer or research-record files. It preserves the original 71-issue/183-edge input wave from commit `4627f414414cc964840b53894b7161482687de69` separately from the later `run-binding` extension to 72 issues/184 edges. The snapshot descriptor binds the bundle to the retained input-manifest identity and binds the research record separately; the original manifest, receipt and response bytes remain unchanged.

For this historical run, the preparer produced deterministic payloads in the selected output snapshot:

- `issues.json` contains one payload per local issue with its source path and byte hash, metadata, and question/body text. Existing hosted identities use `preserve-existing`; pending identities use `create`. Historical local assignees are provenance and are not sent as hosted assignees.
- `resolution-comments.json` carries separate comments for closed records, retaining their original headings and text. Open records have no generated resolution comments.
- `edges.json` records parent and `blocked-by` edges. `ready-to-publish` means both endpoints had verified identities in the selected historical mapping; it does not mean the hosted relationship was read or created. Relationships remained `unverified` without a separate native-edge receipt.
- `unresolved-references.json` records local issue links without a hosted identity and repository links absent from the historical link revision. Repository links become exact GitHub `blob` or `tree` links only when the referenced Git object exists at that revision; otherwise the payload retains a `local-reference:` link and reason.
- `input-manifest.json` binds each issue's path, source-byte hash and parsed metadata, plus the preparer hash and historical hosted-mapping hash. Its identity is independent of generated payloads. The snapshot descriptor separately binds the research record; `manifest.json` records the historical link revision, input-snapshot hash, output hashes and counts.

In the 2026-10-08 mapping, five entries were verified and all other local issues were explicitly pending. The donor's note that a later publication could fill those entries describes that historical state only. Any such publication would require actual identities and a separately reviewed mapping and output snapshot; ordering does not supply an issue number or URL. Labels also retain local type distinctions: `implementation:*` adds `wayfinder:task`, `research:needed` adds `wayfinder:research`, and historical local assignees remain provenance rather than hosted-user assignments.

For example, the historical `issues/001.md` record was narrowly closed from its owner's recorded reaction to the public prototype; its question, prototype assets and separate resolution record remain. This did not close the backend, lifecycle or cost questions. `issues/formatter.md` remains a historical closed implementation task with its original resolution record represented separately; its receipts are not a performance claim.

## Current captured identities and source baseline (2026-10-09)

The current identity intake is the immutable 2026-10-08 capture `github-wayfinder-current-intake-2026-10-08`: 75 issue rows, 74 canonical identities, with only the closed duplicate #63 excluded as a duplicate of #62. The legitimate issue #5 remains local ID `001`. A separate read-only refresh on 2026-10-09 matched the captured raw response byte-for-byte. The captured issue status and body hashes are hosted observations; local status, assignee, resolution text, and relationship state remain separate.

The mapping [`current-hosted-identities-2026-10-09-source-reconciled.json`](migration/current-hosted-identities-2026-10-09-source-reconciled.json) binds those hosted identities to the current local issue-source hashes. `hostedBodySha256` remains bound to the captured hosted body, while `localSourceSha256` reflects the dated local source. In particular, [`native-execution-lowering.md`](issues/native-execution-lowering.md) and [`numeric-arithmetic.md`](issues/numeric-arithmetic.md) carry local source interpretations pinned to compiler commit `07d861f0296a216b78cd0c9e0a1ba2896a0d06d9`; neither note changes the captured hosted body or claims that the hosted issue was updated. The regenerated output uses the distinct identity `current-wayfinder-map-2026-10-09-source-reconciled`, so the prior `current-wayfinder-map-2026-10-09` outputs remain unchanged. Generated relationship status remains `unverified` until a separate relationship update is included in a new reviewed output.

## Research and spike acceptance

New spike or comparison records carry a compact evidence packet: immutable primary-source URLs or repository pins and inspected paths; the mechanism observed; the Effra choice and ordinary Go idiom; a TypeScript/Effect comparison when relevant; the named North Star fit and tradeoff; rejected alternatives; counterevidence or limits that could overturn the choice; and finite execution with two unrelated callers plus causal negative controls before a support claim. A spike is design/source evidence until the required execution, receipts and independent review exist. Historical notes and current executable evidence remain separate.

For a zero-cost abstraction record, the packet also names semantically equivalent explicit Go and TypeScript/Effect baselines with the same validation, ownership, cancellation and completed cleanup. It states whether the abstraction is erased, directly lowered or retained, and measures residual allocation, dispatch, checks, retained modules, executable bytes and runtime cost under matched source and toolchain conditions. Do not trade away a guardrail or turn one benchmark into a universal claim; these measurements join the benchmark-last ledger.

The native Go baseline is optimized idiomatic Go with the same contract; a native result below that baseline remains unresolved. The JavaScript packet records the pinned Effect-compatible ABI and userland version and every material measured lowering or specialization candidate, including static `match` dispatch and a generated effect runtime when applicable. Machine-written generated code is allowed as an implementation detail but must preserve typed errors, service rows, ownership, cancellation, scopes and completed cleanup. Possible speedups remain ambitions to measure, not current claims or universal multipliers. The packet records cold and warm engine evidence and rejected or losing alternatives.

Durable records include [portable i64 arithmetic](../research/numeric-arithmetic.md), [user-defined JSX pragmas and target-qualified host views](../research/jsx-pragmas-and-runtime-selection.md), [the lawful runtime contract and evidence categories](../research/lawful-runtime-contract.md), and [generalized `run` binding](../research/generalized-run-binding.md) with its [GET-backed hosted reconciliation](hosted-run-reconciliation.json).
