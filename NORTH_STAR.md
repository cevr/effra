# North star

Effra is becoming a language for servers with explicit effect contracts, fast native Go builds, straightforward host interop, and compiler-backed inspection for humans and agents. `.ef` is its source format; `ef` is its command.

Established 2026-10-05 from the owner's conversation, [design sketch](docs/design.md), [interop direction](docs/interop.md), and the checked prototype. Receipts below refer to baseline `4c497ac`; they identify evidence and gaps, not permanent line numbers. Wider language capabilities remain proposals until a runnable example and a guard establish them.

Current implementation receipt (2026-10-05; managed runtime foundation `5c8c31f`, import/lifecycle update `8b092fc`): `runtime/effra` and `examples/lifecycle.ef` establish managed Go ownership, cooperative cancellation, cleanup, and bounded current-scope inspection. `examples/go-interop` establishes the native Go adapter seam, retaining partial results; primitive Go package functions now import automatically with explicit Foreign capability and reviewed behavior metadata. CLI/MCP inspection still describes a single source file, and shared Go/JS lifecycle conformance covers ownership, unobserved failures and timeout cleanup defects; complete provider parity remains open. See [runtime contracts](docs/runtime.md).

## North stars

| North star | It holds when | A candidate breaks it when |
| --- | --- | --- |
| **Explicit contracts and clear guardrails** | Success, named failures, and nominal requirements can be read without expanding backend types. Missing requirements and undeclared failures produce diagnostics. Evidence: `internal/compiler/semantic.go:241`, [effect contracts](docs/design.md#the-type-system-is-the-main-feature); owner, 2026-10-05. | It silently enlarges a public contract, permits an unchecked call, or describes a runtime/trusted rule as statically proved. |
| **Operability and agent introspection** | CLI and MCP report the same checked contracts, target, source revision, and explanations. Evidence: `internal/compiler/semantic.go:36`, `internal/mcp/server.go:185`, [inspection design](docs/design.md#operability-and-agent-introspection); owner, 2026-10-05. | Understanding a type requires generated-code archaeology, a second analyzer disagrees with the compiler, or stale/unknown state is reported as current fact. Process-wide task inspection remains a gap. |
| **Exceptionally fast compilation** | Parse/check, imports, emission, Go compilation, and linking have separate measurements; private edits avoid unnecessary dependency work. Evidence: `internal/compiler/compiler_test.go:178`, `cmd/ef/main.go:232`, [compile-speed constraints](docs/design.md#compile-speed-is-a-language-constraint); owner, 2026-10-05. | It adds whole-program inference, dependency walks, or TypeScript checking to a Go-only build without measured need. Current single-file timings do not establish incremental or end-to-end performance. |
| **Native Go server programs** | The default build produces an independently runnable Go executable, with managed memory and access to the Go ecosystem. Evidence: `cmd/ef/main.go:174`, `internal/compiler/go_backend_test.go:37`; owner, 2026-10-05. | The Go path needs Node/Effect to execute, or server work acquires freestanding, kernel, or hard real-time requirements. |
| **Adoption through host interop** | Existing host declarations supply callable shapes; behavioral facts and trust remain explicit. Routine calls avoid handwritten signature duplication and unnecessary data conversions. Direction: [interop proposal](docs/interop.md), owner, 2026-10-05. Receipt: `internal/compiler/imports.go` consumes native primitive signatures; broader named host types and TS imports remain gaps. | It requires a wrapper declaration for every imported symbol, infers cancellation/purity from its shape, silently drops partial results, or hides an unsupported host type behind unchecked `any`. |
| **Owned lifetimes** | A scope owns children and resources; completion means owned child shutdown and cleanup have completed. Cancellation remains a request. Direction: [lifecycle contract](docs/design.md#concurrency-and-resources), owner request for cancellation/scopes, 2026-10-05. Receipt: `runtime/effra` and the JS policy in `internal/compiler/lifecycle.mjs` implement owned shutdown with conformance tests. | It treats context cancellation as completed shutdown, or returns from timeout while managed work has lost its owner. Protected cleanup and failure preservation need runtime evidence. |
| **Honest target capabilities** (drafted 2026-10-05) | One semantic model checks the portable subset; target-specific imports and capabilities remain visible. Evidence: `internal/compiler/semantic.go:90`, `internal/compiler/go_backend_test.go:37`, [target design](docs/design.md#javascript-effect-and-go-targets). | It promises equivalent lifecycle or interop behavior before conformance establishes it, or forces both targets through the same host ecosystem. |

## Tiebreaks

- **Structural correctness** beats **fast move-in-place** (owner AGENTS.md, 2026-10-05): deliver coherent changes in compiling units; reject shortcuts that hide ownership or duplicate authoritative semantics.
- **Explicit contracts** beat **terse syntax** (drafted 2026-10-05): reject shorter syntax that hides a requirement, target restriction, or foreign trust assertion.
- **Truthful target limits** beat **feature parity** (drafted 2026-10-05): diagnose unsupported operations until an adapter proves their contract.
- **Completed owned cleanup** beats **deadline punctuality** (drafted 2026-10-05): a managed timeout may exceed its deadline while waiting for cooperative shutdown; early return requires an explicit surviving owner.

## Owner rules

- Native Go executable is the default product path; JavaScript may emit Effect code (owner, 2026-10-05).
- Build for servers. Kernels, hard real-time, and freestanding/no-GC execution are outside the destination (owner, 2026-10-05).
- Preserve unrelated work; use isolated Rifts; push or publish only when explicitly requested (owner AGENTS.md, 2026-10-05).
- Run the full repository gate between logical units and before handoff. Performance claims require measured evidence (repository AGENTS.md).
- Keep Wayfinder's HITL decisions open until owner feedback; artifact completion is not decision closure (repository AGENTS.md).
- Keep `.ef` text authoritative and CLI/MCP on the compiler's semantic model (repository AGENTS.md; architecture direction, drafted 2026-10-05).
- Local fixtures and scratch artifacts are the default for live checks. Paid services, deployment, and production data are outside routine checks (drafted 2026-10-05).

## Sweeps

| Sweep | Serves | Scope | Method | Done when |
| --- | --- | --- | --- | --- |
| Compile performance | Exceptionally fast compilation | Frontend, imports, generation, backend | Run `go test ./internal/compiler -run '^$' -bench BenchmarkCompile10KLines -benchmem`; for changed build paths compare matched Effra/Go cold, warm, private-edit, public-edit, and dependency-edit fixtures, recording toolchain/hardware/cache regime and stage timings. | Each changed stage has a reproducible receipt; regressions are addressed or explicitly carried. Missing fixtures remain gaps, not passing results. |
| Lifecycle behavior | Owned lifetimes | Managed tasks, acquisition, cleanup, cancellation | Exercise parent/child shutdown, acquisition-vs-close, LIFO cleanup, panic/failure preservation, and cooperative foreign cancellation through the public runtime interface under `go test -race ./...`. Compare only supported target cases. | Each changed lifecycle ability has a behavior receipt, and every outstanding guarantee has a carried row. Go runtime evidence exists; JS lifecycle comparison remains open. |
| Inspection parity | Operability and agent introspection | CLI/MCP and advertised guards | Run `python3 scripts/smoke.py`; compare source contracts, revisions, target restrictions, and rejected stale/path-invalid requests. Check `project.describe` against implemented guards. | Supported queries agree, advertised capabilities match executable behavior, and new guardrails have a checked receipt. |

## Live check

- Drive: `bun run gate` builds `bin/ef` and exercises native Go, JS library/entry execution, and the real stdio MCP process through `scripts/smoke.py`.
- State: `./bin/ef inspect examples/main.ef greeting`, `./bin/ef explain examples/main.ef greeting`, and MCP `project.describe`/`project.check`. For Go runtime state, `examples/lifecycle.ef` executes Runtime.inspect; there is no out-of-process runtime endpoint yet.
- Stop: the smoke harness waits for its processes to exit; manual stdio MCP checks close stdin and wait for exit. Longer-lived server checks must record shutdown and scratch cleanup.

## Rejected

| Candidate | Why it stays rejected |
| --- | --- |
| Kernel or hard real-time execution profile | Outside the owner's revised server scope, 2026-10-05; [Wayfinder map](docs/wayfinder/issues/map.md#out-of-scope). |
| Treat host type declarations as proof of foreign behavior | Breaks **Explicit contracts and clear guardrails**; [interop trust distinction](docs/interop.md#behavior-contracts-without-signature-duplication). |
| Replace `.ef` with an authoritative graph database | Conflicts with the repository's source-authority rule; borrow focused semantic queries/edits instead ([design](docs/design.md#compiler-backed-mcp-and-semantic-editing)). |
