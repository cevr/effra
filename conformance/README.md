# Conformance inputs

`upstream/effect` is a git submodule pinned to Effect 4.0.1, commit `460272d30457f4697d8b8c52cad41caccbcace08`. It is upstream's own repository, not an Effra copy. Its tests are a reference only: Effra's gate never executes them and does not claim to pass the upstream suite. Check it out once per clone, worktree or Rift with `scripts/init_upstream.sh`; set `EFFRA_UPSTREAM_MIRROR` to a local Effect clone to stay offline.

Effra owns the metadata beside it:

| File | Contents |
| --- | --- |
| `effect-upstream.manifest.json` | The selected 746 reference files and their 26 licenses, the nearest-license mapping and a sha256 per file, plus the canonical integrity root. `go run ./scripts/conformance import` recomputes it from the pinned commit's git objects and refuses any difference. |
| `effect-cases.json` | Selected upstream cases mapped to existing Effra acceptance tests, with status and limits. |
| `codecs/` | Codec policy vectors compared against the pinned Effect runtime. |
| `size/` | Size-conformance fixtures, the Go and TypeScript/Effect controls, and recorded application receipts; see [size/README.md](size/README.md). |

[docs/conformance.md](../docs/conformance.md) explains the statuses, commands and pin-update procedure.
