<!-- {"id": "native-codecs", "title": "Checked nominal JSON codecs", "status": "open", "labels": ["implementation:task"], "parent": "native-server-spec", "assignee": null, "blocked_by": ["native-interfaces"]} -->
# Checked nominal JSON codecs

Implement reusable codecs derived from canonical type DAGs, with explicit policy vectors and matched Effect Schema behavior. See the [contract](../../specs/native-server-contracts.md).

Owner clarification, 2026-10-06: retain Rust-style structural derivation and Effect-style transformations. The witness distinguishes wire/domain types and decode/encode failure/service rows. Deliver structural derivation and typed composition/refinement in separate gated commits; preserve bounded DAG traversal and explicit encoding policy. Include a pure nominal conversion, a normalizing codec and an effectful direction-specific conversion, tested through both a server boundary and an unrelated data/configuration caller. Reference-source survey and independent design counsel refine the syntax; no existing support is implied.
