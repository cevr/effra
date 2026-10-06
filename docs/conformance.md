# Upstream behavioral conformance

Effra keeps a licensed, byte-preserved Effect 4.0.1 test reference at commit `460272d30457f4697d8b8c52cad41caccbcace08`. Its **746 reference files and 26 license files are not 746 passing Effra tests**. The ordinary gate checks the complete inventory and independently pinned hashes offline; it never executes the copied TypeScript suite or its imported shell fixtures. Executable permission bits are not copied. Future native fixtures belong to their own Effra test owner.

[effect-cases.json](../conformance/effect-cases.json) is the maintained authority for selected behavioral comparisons. Each stable ID points to an exact pinned test file, declaration line and label, states its behavior and limits, and names the existing Effra test that exercises both Go and JS when applicable. The imported manifest authenticates the entire referenced file. Line anchors distinguish identical labels in different suites. IDs stay stable when a future upstream pin changes; review the behavior and update the anchor deliberately.

| Status | Meaning |
| --- | --- |
| `covered` | Named behavior has an existing shared Go/JS acceptance test. Read its limits; it is not a claim that the entire upstream case/API was ported. |
| `difference` | Existing acceptance exercises an explicit Effra policy that differs from the cited upstream case. The difference must stay described. |
| `pending` | A relevant behavior is planned but lacks admitted source-level acceptance. Internal runtime helpers do not count. |
| `unsupported` | The current admitted profile excludes the behavior. No passing pointer is permitted. |

Unlisted cases remain **reference-only, unassessed**. A source pointer records where evidence lives, not a stored result that stays passing forever. The validator checks pin, corpus integrity, exact upstream anchors, duplicate IDs/cases, status/evidence structure and named Go test declarations. It cannot prove semantic equivalence or infer from a Go function name that its assertions cover a claim; reviewing those assertions and executing them remain required. Arbitrary extra “passed” metadata is refused.

```sh
# Offline reference and mapping admission
python3 -B scripts/import_effect_conformance.py --self-check
python3 -B scripts/check_effect_conformance.py

# Execute the selected existing acceptance tests: real generated Go and JS
python3 -B scripts/check_effect_conformance.py --run

# Full gate also runs all Go tests, including the selected acceptance tests
./scripts/gate.sh

# Optional source-backed verification against immutable cached Git objects
python3 -B scripts/import_effect_conformance.py /path/to/effect-cache --check
```

The first selection covers child interruption and completed child-before-parent cleanup, preservation of an expected failure plus cleanup defect, and virtual-time admission of an unstarted fork. Timeout has an explicit difference row: Effra's nominal `Timeout` and required `Scheduler` differ from upstream `Cause.TimeoutError`; a supplemental timer-defect test ensures defects are not rewritten as timeouts. Existing lifecycle acceptance also checks unobserved child failure, but that assertion has no selected upstream case mapping yet and is not counted as another port.

Generic acquisition, Deferred, Ref and fixed-capacity semaphore cases remain pending. Parallel scope finalization and detached child lifetime remain unsupported. Scheduler duration/precision and the rest of the corpus are not implied by the selected virtual-time fixture. Each later library, codec, server or machine family can add its own selected cases to the same mapping instead of creating another parity table.

## Acquisition and scope source comparison

Pinned `packages/effect/src/internal/effect.ts` was read from the same immutable Git object, not the newer cache working tree. [scopeClose:3929–3994](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/internal/effect.ts#L3929) marks a scope closed, removes it from its parent and executes finalizers under protected close; sequential finalizers run in reverse registration order, while parallel finalization is an explicit separate policy. [acquireRelease:4134–4150](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/internal/effect.ts#L4134) protects acquisition/registration by default, with an explicit interruptible option and captured release context. [acquireUseRelease:4396–4408](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/internal/effect.ts#L4396) masks acquisition, restores interruptibility for use and registers release at the exit boundary.

The mapped interrupted-acquisition case asserts acquired=true, use=false, release=true. Effra's existing lifecycle fixture proves release ordering and composite causes through native/Effect providers; it does **not** exercise that interrupted-acquisition window through a generic Effra callback API. A future port must add causal acquisition/register/use interruption controls and ownership/error/service admission rather than mark the row covered because both runtimes have acquire/release functions.

Deferred first completion is distinct from recipe-storage `completeWith` or producer single-flight. Ref callback purity and resource-free payload placement need source admission tests beyond upstream mutable cell behavior. Ordinary Semaphore queued-cancellation cases must not be confused with PartitionedSemaphore or mutable-capacity cases in the same upstream file. These differences remain visible as later acceptance is added.

No upstream dependency tree was installed or run. Imported reference provenance, selected Effra correctness and runtime performance are separate evidence; this mapping makes no performance claim.
