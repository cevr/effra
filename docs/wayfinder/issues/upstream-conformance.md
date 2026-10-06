<!-- {"id": "upstream-conformance", "title": "Pinned upstream tests and executable conformance mapping", "status": "closed", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": "upstream_conformance", "blocked_by": []} -->
# Pinned upstream tests and executable conformance mapping

Copy the pinned Effect test and type-test corpus with license, original paths, revision and verified hashes. Inspect implementation and prior-art usage through the repo-cache workflow. Map upstream behavior cases to executable Effra acceptance tests and explicitly unimplemented/different contracts. Reference snapshots are not passing native conformance. See the [native server contract](../../specs/native-server-contracts.md).

Independent lane after the owner's benchmark deferral: integrate the already reviewed pinned importer/corpus without benchmark runner dependencies, then establish a maintained mapping to current actual foundation cases and explicit pending families. Later library/server tasks extend that same mapping when their behaviors become executable. Do not defer the compiler's conformance infrastructure with performance measurement.

## Resolution comments

2026-10-06: independently reviewed importer and maintained mapping integrated at62baa74 after final proof repaira395674. Licensed unchanged corpus contains746 reference files and26 license files; the gate verifies complete inventory and immutable hashes offline. Ten selected stable behavior IDs point to actual source anchors and qualified Go/JS acceptance or explicit differences/pending/unsupported states. Selected tests execute both targets, including a ten-hour virtual wait under15s watchdogs; copied references alone count as zero native passes.

Both Claude rounds consumed, required causal deep-JSON/test-time repairs verified by finite root/Astra closure. Branch and root full gates pass, logs `/tmp/effra-upstream-{branch,root}-integration-gate.log`, SHA25660e183866005494c16ab575d0ac542e1c712835d01699bcd8c5d7e87f47e22ba and0714e37576f0495039c9a93534510a079cf8a9b15365835d438c8b9c47ed0952. All31 legacy output nodes remain unchanged. [Maintained conformance documentation](../../conformance.md) describes admission/proof limits. Later library/server ports remain owned by their implementation tasks and extend this same mapping; no full Effect parity or performance claim.
