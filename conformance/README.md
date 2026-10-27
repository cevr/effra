# Conformance inputs

The selected Effect cases cite Effect 4.0.1, commit `460272d30457f4697d8b8c52cad41caccbcace08`. Effra keeps no checkout or copy of upstream: each case records its upstream file, line and test label as provenance text. Effra's gate never executes the upstream suite and does not claim to pass it.

Effra owns these inputs:

| File | Contents |
| --- | --- |
| `effect-cases.json` | Selected upstream cases mapped to existing Effra acceptance tests, with status and limits. `go run ./scripts/conformance check` validates it. |
| `codecs/` | Codec policy vectors, with the outputs the pinned Effect runtime produced for them (`effect-*comparator.out.jsonl`), frozen on 2026-10-26 as golden files. The Go codec tests check against them; nothing runs Effect. |
| `http/` | Shared HTTP wire vectors: exact status/body/content-type exchanges, the limits each program declares, and lifecycle cases (body timeout, disconnect, shutdown with active work). `cmd/ergo` `TestHTTPWireVectors` runs every `*-vectors.json` over real sockets on Go. |
| `upstream/foldkit-d21db423/` | The pinned Foldkit workspace snapshot, a language-design reference corpus; `go run ./scripts/conformance foldkit --self-check` verifies it offline. |
| `size/` | Size-conformance fixtures, the Go controls, and recorded application receipts; see [size/README.md](size/README.md). |

[docs/conformance.md](../docs/conformance.md) explains the statuses, commands and pin-update procedure.
