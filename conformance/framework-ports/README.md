# Foldkit application and host-interoperability corpus

This corpus separates immutable upstream examples from authored ports. The byte-preserved Foldkit workspace snapshot is at [`../upstream/foldkit-d21db423`](../upstream/foldkit-d21db423); its `manifest.json` records the source commit, Git tree, included path modes, upstream blob IDs, byte counts, and SHA-256 hashes. The matching upstream MIT `LICENSE` and this capture's `NOTICE` are preserved there.

The source pin is Foldkit commit `d21db423ac0ba676b22aa7a7b4041ba409ec2c52`, package version `0.167.0`, with workspace Effect `4.0.0` and pnpm `11.8.0`. All 34 directories beneath the pinned `examples/` tree are captured, along with the tracked workspace packages, build/test support, lockfile, and root configuration. The vendored `repos/` reference trees are excluded; Foldkit's own repository guidance says package and example source must not import from them. No `.git`, installed dependencies, or generated caches are captured. The snapshot is reference data: it has not been installed or executed here and does not claim that Effra passes Foldkit's tests.

The host profiles are recorded by example in `manifest.json`. Its initial matrix marks each Foldkit directory `reference-only`; React DOM, Solid 2 DOM, solid-yield, Effra/Go, and Effra/Effect-JS rows remain `pending` until each has authored code and executable evidence. A copied upstream source file is not Effra coverage. Unit 1 has no executable runner, so its self-check rejects every `implemented-and-verified` host row; a later runner must own that status and record authored target and execution evidence. Ports live outside the immutable snapshot and must keep source behavior, target, and evidence distinct.

The proposed isolated host-tool versions are React/React DOM `19.3.0`, Solid/Solid Web `2.0.0-rc.13`, Vite `8.3.4`, and Vitest `5.0.3`. `solid-yield` is a source-pinned local workspace at `2f2431da101ffc1c0e3fde7b5aa4fcb6b009584c` because there is no npm release at that pin; its Effect example declares Effect `^3.22.0`. The official Solid Effect example also declares Effect `^3.22.0`; neither is an Effect 4.0.1 bridge. Effra's generated JS uses its pinned Effect `4.0.1`, and any React Atom hook package is pinned separately as `@effect/atom-react` `4.0.1` with Effect `4.0.1`. The Foldkit workspace's own `4.0.0` dependency does not change the Effra runtime pin. Any new package locks stay inside this corpus and are installed only when changed.

## Reproduce the source capture

Given a local Git checkout containing the pinned Foldkit commit:

```sh
python3 -B scripts/import_foldkit_corpus.py /path/to/foldkit --check
python3 -B scripts/import_foldkit_corpus.py --self-check
python3 -B scripts/test_import_foldkit_corpus.py
```

The first command compares every captured file and the complete 34-example inventory against the pinned Git objects. The offline self-check verifies the frozen per-file identities and the coverage matrix. The source capture can be regenerated only into a fresh destination with:

```sh
python3 -B scripts/import_foldkit_corpus.py /path/to/foldkit --capture
```

The repository gate runs the offline check and the standard-library-only importer controls. Those controls verify the complete pinned matrix, source-backed evidence paths, refusal of premature implementation claims, malformed-root and row diagnostics, symlink referents, and cleanup after a failed publication; they do not duplicate host behavior tests. Full gate receipts for this architecture lane are recorded outside the repository by `R/run-gate.sh` with an explicit `umask 022` and the shared validation lock. The captured pnpm lock/workspace files provide the upstream reference's declared toolchain, but the corpus does not install it or claim execution. Authored host packages use their own scoped lockfiles and Bun scripts.

## Effra and machine boundary

The current interop seam is one-way: Effra can build JavaScript modules and `.d.mts` declarations for TypeScript consumers. TypeScript imports into `.ef` remain unsupported (VW3), the browser client lifetime bridge is pending (VW4), and admitted JSX notation is still awaiting its implementation (VW6). The counter tranche therefore imports real compiled Effra exports from host TypeScript, and exercises generated declarations with a strict negative `number`-for-`bigint` consumer. It does not fabricate Effra JSX or a reverse import.

Foldkit, React, Solid, and solid-yield provide reference behavior or host rendering only. Effra's own state-machine and actor runtime is a separate implementation lane shared by Go and the pinned Effect-JS backend. The machine declaration is authorized design but is not supported by the base compiler/runtime in this corpus branch. Adoption fixtures must call that owned core when it becomes available; a Foldkit machine or host framework state store cannot stand in for it.

Machine adoption evidence is expected to cover serialized pure/effectful step evaluation; evaluation cleanup, entry cleanup, then commit/output; Go re-entry versus `Stay` preserving an entry; stale completion fencing by entry epoch; bounded mailbox item/byte admission with reserved completion and independent stop control; admission acknowledgement versus committed call/result; typed and composite causes; stop/discard and detached call waiters; external writes surviving local failure; and inspection that never executes behavior. These are local in-process contracts. They do not imply rollback of external effects, durable execution, exactly-once processing, or distributed actors.
