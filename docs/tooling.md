# Native developer tooling

The compiler's checked model supplies CLI and MCP answers. These are default capabilities, with no second analyzer or backend execution needed for lint/queries. Each invocation currently checks one file; Go imports may load host export data. There is no persistent workspace cache.

| CLI | MCP | Purpose |
| --- | --- | --- |
| `ef check FILE` | `project.check` | Non-disableable source admission diagnostics |
| `ef diagnostics FILE [--strict] [--json]` | `project.diagnostics` | One shared compiler/lint report with byte spans and UTF-16 ranges |
| `ef lint FILE [--strict]` | `project.lint` | Checked semantic advice; separate `checked` and `lintPassed` |
| `ef lint rules` | `lint.rules` | Stable codes, severity, names and rationale |
| `ef inspect FILE SYMBOL` | `code.inspect` | Declared/body contracts or nominal record, enum and error metadata |
| `ef explain FILE SYMBOL` | `code.explain` | Local contract contributions |
| `ef query FILE BYTE_OFFSET` | `code.typeAt` | Expression kind, type, and executed failure/requirement rows |
| `ef graph FILE` | `project.graph` | Dependencies, providers, calls and provision boundaries |
| `ef test FILE` | `project.tests` discovers cases | CLI executes; MCP remains read-only |

File commands accept `--target go|js`, defaulting to Go. Results contain semantic revision hashes. MCP file tools accept `expectedRevision` and reject stale snapshots.

## Diagnostic reports

`ef diagnostics` and `project.diagnostics` use the same compiler-owned report. It identifies the source with a canonical escaped `file:` URI and an `origin` such as `disk`, includes the exact semantic `revision`, selected `target`, `checked` admission state, `strict` policy, `policyPassed`, and deterministic findings. Every finding preserves its UTF-8 byte `span` and reports `code`, `origin`, optional lint `rule`, stable `severity` (`error`, `warning`, `information`, or `hint`), and message. A finding with a source location also has a valid zero-based UTF-16 `lsp.range`; `locationAvailable` is false when the compiler has no source location, such as an unsupported target diagnostic.

Compiler errors always fail policy. Strict mode changes only the policy decision for warnings; it does not change finding severity. Unchecked source has `lintAvailable: false` and an explicit `lintUnavailableReason`, while compiler findings remain available. Reasoned lint suppressions can remove optional advice but cannot hide compiler errors.

The CLI prints one finding per line by default and emits the complete report with `--json`. A policy failure exits 1, an invalid invocation exits 2, and file or compiler operation failures remain operational errors. MCP returns source errors as successful structured reports with `policyPassed: false`; source admission, stale-revision, path, and output-limit failures remain tool errors. MCP accepts at most 100 diagnostic findings and reports an explicit limit error above that boundary. The report's `totalCounts` and `returnedCount` are exact for every successful response.

## Lint

`EFL001 unused-recipe` warns when a local lazy effect binding is never referenced. Uses inside branches and nested scopes count; bindings in separate functions remain distinct. `let _ = recipe()` acknowledges intentional omission. No deletion fix is offered: constructing arguments may itself execute nested effects.

`EFL002 redundant-provision` suggests reviewing a boundary whose receiver has no requirement for that service. Stable boundaries may be intentional. `EFL003 unused-go-import` suggests removing an import with no admitted calls.

Warnings do not make checked source untyped. `--strict` makes warnings fail the lint command; suggestions remain non-failing. Compiler errors always fail, and optional advice is skipped on unchecked source. A named next-line suppression can acknowledge one optional rule on the following physical source line:

```text
// effra-lint-disable-next-line unused-recipe -- intentionally deferred hook
let forgotten = task()
```

The rule name must be known and the reason must be non-empty. Malformed, unknown, or unused suppressions are `EFL004 invalid-suppression` errors and fail lint in every mode. Suppressions are matched against semantic diagnostic spans in the same source revision; compiler correctness diagnostics cannot be suppressed. Comments remain source text, and no automatic deletion fix is offered.

## Local types

Offsets and spans are UTF-8 bytes. Queries currently address diagnostic anchors (for example, the `run` keyword, a call name, or `provide`) rather than entire expression ranges. They reject unchecked source and offsets outside checked anchors. Effect types distinguish deferred rows from rows executed while evaluating the expression.

Checked values also carry a canonical `type` reference. Nominal records, enums and errors retain declaration identity; `ef inspect FILE TYPE_NAME` returns their source fields or variants with UTF-8 spans. MCP bounds declaration lists, variants and fields and reports truncation explicitly.

```sh
OFFSET=$(python3 -c 'from pathlib import Path; s=Path("examples/latest-task.ef").read_bytes(); print(s.index(b"run previous"))')
ef query examples/latest-task.ef "$OFFSET"
```

## Dependencies and dependents

Graph nodes represent functions, nominal services, providers, provider methods, provider recipes, materialized provider values and lexical effect expressions. Edges include `requires`, `implements`, `calls`, `contains`, `adapts`, `materializes`, `originates` and `provides`. Incoming edges identify dependents. Canonical contracts appear on function/expression nodes.

For `examples/workflow.ef`, `welcome` requires `Directory`; `DemoDirectory` implements it; a provision expression adapts the receiver and supplies that provider. The receiver keeps its original contract while the provision node shows the remaining requirements. Explicit configured or dependency-capturing provider constructors appear as lazy recipes; each `run` materializes a provider value, while aliases of a materialized value retain its identity.

This is a single-file static composition graph, including deferred calls. It does not establish execution order or runtime allocations. Provider construction is explicit and non-memoized: a reused value is one graph identity, while repeated runs of one recipe are distinct values. Fallible acquisition, lifecycle-safe arbitrary capture, general sharing keys and cycle paths will extend this model when implemented. Expression IDs contain offsets and are scoped to the revision.

MCP limits graphs to 1,000 nodes and 2,000 edges; larger graphs fail explicitly. The legacy `project.lint` response keeps its separate bounded diagnostic arrays and truncation flags. Full CLI diagnostic reports remain available.

## Next capabilities

Canonical comment-preserving formatting, revision-bound checked edit plans, multi-file identities, editor integration, ownership provenance and runtime/source correlation remain planned. Compiler errors stay independent of optional style policy. A future test clock must share a scheduler with sleep/deadline primitives.

## Performance receipt

On 2026-10-05, Apple M4 Pro / macOS 27.0.1 / Go 1.27.1 arm64, three one-second warm runs of `BenchmarkCompile10KLines` measured 3.13–3.30 ms per parse/check for the synthetic 2,000-function, 10,000-line fixture (about 14.05 MB and 16,192 allocations). The preceding baseline measured 2.95–3.01 ms; this small measurement is not a controlled attribution of the difference or an end-to-end build ratio. Keep watching it as the checker grows.

`BenchmarkLintAndGraph10KLines` runs lint plus graph on an already checked fixture: 0.61–0.62 ms, about 1.16 MB / 6,132 allocations. It excludes parsing and import loading. Reproduce with `go test ./internal/compiler -run '^$' -bench 'Benchmark(Compile|LintAndGraph)10KLines' -benchmem -benchtime=1s -count=3`. Real import caching, cold builds, incremental edits and linking need separate fixtures.
