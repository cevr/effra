<!-- {"id": "upstream-conformance", "title": "Pinned upstream tests and executable conformance mapping", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": "upstream_conformance", "blocked_by": []} -->
# Pinned upstream tests and executable conformance mapping

Copy the pinned Effect test and type-test corpus with license, original paths, revision and verified hashes. Inspect implementation and prior-art usage through the repo-cache workflow. Map upstream behavior cases to executable Effra acceptance tests and explicitly unimplemented/different contracts. Reference snapshots are not passing native conformance. See the [native server contract](../../specs/native-server-contracts.md).

Independent lane after the owner's benchmark deferral: integrate the already reviewed pinned importer/corpus without benchmark runner dependencies, then establish a maintained mapping to current actual foundation cases and explicit pending families. Later library/server tasks extend that same mapping when their behaviors become executable. Do not defer the compiler's conformance infrastructure with performance measurement.
