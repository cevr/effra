# User-defined lint rules

Status: authorized owner requirement, 2026-10-06. Users must be able to write and distribute their own rules without forking the compiler. Reuse the [shared diagnostics and full types](semantic-tooling.md), including exact source revisions and resolved binding identities. This is an implementation contract, not current plugin support.

## API and authority

A rule declares a namespaced identity, version, documentation, default severity, supported target/schema versions, validated options and required fact families. It receives a read-only snapshot and returns findings. The runner owns configuration, severity overrides, reasoned suppression, ordering, limits and projection to public diagnostics. A rule cannot mutate the checker, add proof, turn invalid source into checked source, remove compiler diagnostics or redefine reserved compiler/built-in rule IDs.

Expose source/token spans, typed syntax nodes, resolved declarations/bindings, canonical referenced types, callable failure/service rows, provider origins/dependencies and ownership evidence as supported fact families. A rule must not reconstruct import aliases, lexical shadowing or effect contracts from identifier spelling. Availability is explicit: a syntax-only rule can run on the supported recovered syntax; a rule requiring checked types does not run on unchecked source. Missing facts and unsupported schema versions are reported as unavailable or failed execution, never a clean rule pass. Initial implementation may require checked source for every external rule and explicitly report skipped rules.

Snapshots retain the compiler semantic revision. A separate analysis identity incorporates rule-pack executable/content hashes, versions, configuration and diagnostic policy; changing a lint option must invalidate lint results without pretending the program's type changed. Cache only complete results under this identity. Schema/version incompatibility and cyclic or unsupported fact requirements fail explicitly.

## Distribution and execution

The initial extension boundary is a versioned executable rule-pack protocol with a small Go SDK. A pack can be built and tested independently and loaded by the installed ef command, without a compiler rebuild or Go shared-library ABI dependency. The protocol is language-neutral, so a later Effra or JS authoring adapter can use the same facts; neither is required for Go users. Do not advertise an Effra-native rule SDK before the necessary library/IO support exists.

Use a static manifest for metadata and rule selection, plus an explicit executable and argument array. No shell command interpolation, implicit package download or automatic build during lint. One bounded request carries the chosen snapshot/fact projection and validated options; one bounded response identifies the request, snapshot, rule and findings. Use deterministic framing and document protocol limits. The runner validates namespaces, severity, message sizes, source ownership, ranges, related locations, edits and revision before accepting output. An executable cannot claim findings on a different source or masquerade as the compiler.

Each pack is project-supplied executable code, not a sandboxed proof engine. Run only explicitly selected packs (`--rules` or an explicitly supplied lint configuration); discovery/inspection alone does not execute them. MCP and LSP receive the same explicit configuration at server startup/initialization. Existing server sessions without custom rules remain read-only compiler analysis. Do not add a recurring interactive approval flow. Bound wall time/output, propagate cancellation and reap owned processes; a crashed, malformed or timed-out enabled pack makes analysis incomplete and fails its lint policy. Publish built-in findings even when an optional extension fails, with an explicit execution-status field.

Built-in checking and ordinary builds never start external packs. Custom lint cost is measured separately from frontend checking, including cold process startup and requested fact serialization. Run selected rules together rather than starting a process per AST node. Optional pack code is absent from application executables.

## Configuration and surfaces

Supply per-rule off/error/warning/information/hint settings, validated options and named presets. Keep default severity and configured severity distinguishable; strict policy changes failure behavior without changing displayed severity. Unknown rules/options, duplicate identities and reserved namespaces diagnose early. Existing reasoned next-line suppressions can name registered external rules and undergo the same malformed/unknown/unused checks; they never disable mandatory compiler diagnostics.

`ef lint`, `ef diagnostics`, MCP equivalents and LSP use the same enabled registry and analysis result. `ef lint rules` and MCP `lint.rules` expose selected pack identities, versions, option metadata, effective severity and fact requirements without running rule code. All reports identify enabled/disabled/skipped/failed rules sufficiently to distinguish no findings from incomplete analysis.

Findings may propose revision-bound text-edit suggestions. The first slice exposes validated previews only: diagnostic queries never rewrite source. Reject out-of-range, overlapping or cross-snapshot edits. Applying edits or advertising LSP code actions requires a separate checked-edit contract with stale-source guards; a proposed fix is not a proof of semantic preservation.

## Authoring and acceptance

Ship a small external Go rule-pack example and a source-fixture test runner. Fixtures contain real `.ef` programs and expected codes/severities/ranges, with admitted controls, rather than mocked AST objects that can hide resolution bugs. The pack receives the production snapshot through the same protocol used in CLI/MCP/LSP. Tests can assert suppression and suggested edits without mutating fixtures.

Demonstrate two unrelated rules using the same extension seam:

1. A project policy restricting a configured live provider to declared composition functions. Use resolved provider identity and provision edges; same-spelled local values must not match.
2. A project policy forbidding a configured failure in a selected public function contract. Inspect canonical nominal failure identity and complete rows, including helper forwarding; spelling aliases cannot bypass it.

Neither policy becomes a universal language restriction. User configuration names the permitted boundaries and severity. Compare semantically equivalent aliases, shadowing, negative cases and allowed exceptions; no name-only pseudo-type checker.

Deliver three gated units: versioned fact/registry/SDK seam; bounded executable runner plus source-fixture tests; CLI/MCP/LSP configuration/reporting, examples and docs. Cover malformed/oversized output, hanging/crashing process, unavailable facts, stale snapshot, invalid options/ranges, independent pack failures, duplicate IDs, suppression, precise Unicode positions and cross-surface parity. Preserve compiler errors even if a malicious test pack requests their removal. Retain raw cost receipts for disabled and enabled rules, with no unmeasured compilation-speed claim. Full gate and independent review precede closure.

## Prior-art decisions

Go's [analysis API](https://pkg.go.dev/golang.org/x/tools/go/analysis) separates analyzers, typed per-package facts and the driver; adopt explicit metadata/fact requirements and reusable reporting. Its supplied documentation leaves severity to drivers; Effra similarly centralizes configured severity while exposing defaults.

[Oxlint plugin authoring](https://oxc.rs/docs/guide/usage/linter/writing-js-plugins.html) supplies configurable rule metadata, reports and a testing interface. Borrow the authoring/test ergonomics, not a requirement to run JavaScript or duplicate type analysis.

Inspected Effect tooling source: `Effect-TS/tsgo` at `d211e109b14045164934c071120b92947a98c8a1`, `internal/rule/{rule,metadata,context}.go`; rule output is separate from checker attachment, with shared directive/severity emission. The owner's lint-plugin source was refreshed to `ca2ce6aeb7b148293b5662b7fb0613c52e81047c`; its rule builder, testing adapter and alias/suppression/test-isolation policies motivate resolved facts and real-source tests. These are source comparisons, not a claim that upstream exposes this exact external protocol.
