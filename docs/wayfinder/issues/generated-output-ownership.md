<!-- {"id": "generated-output-ownership", "title": "Isolated owned native generated modules", "status": "open", "labels": ["implementation:task"], "parent": "binary-reachability", "assignee": "generated_output", "blocked_by": ["foundation-spec"]} -->
# Isolated owned native generated modules

Prepare the output boundary independently of canonical application reachability. Native builds now publish complete immutable modules under `dist/go/apps/<application-id>/generations/`, while legacy shared `dist/go` files remain untouched. The runtime source split exposed a concrete upgrade failure: the additive writer left the former `stdlib.go` beside its replacement files, producing duplicate declarations. Fresh builds alone do not cover this transition.

Give generated modules a full source-origin, target and build/test mode identity and an isolated, complete source snapshot. Publish only completed generations; validate any reused generated files against their ownership/content record. Preserve unknown or modified files and legacy generated directories. Do not add a filename-specific cleanup or delete unrecognized files to make the gate pass. A failed or interrupted generation must not damage a previous usable module or admit stale sources into the next build.

Keep executable output flags and application semantics stable. Preserve import module graphs, warm unchanged builds and Go/JS conformance. Runtime source selection remains all-source until the parent task supplies checked roots; this task does not establish smaller binaries.

Require public old-to-new rebuild, same-basename origins, ordinary/test isolation, unchanged reuse, shrinking source sets, unknown/modified file refusal, interrupted publication and concurrent publication controls. Retain exact generated source sets and raw gate/review receipts. Integrate this boundary before the runtime source split; that split's clean-build gate alone cannot close its upgrade obligation.
