# Small executables and reachable library code

Status: owner-established north star and authorized implementation requirement, 2026-10-06. This is a delivery contract, not a claim about the current prototype's binary size.

## Outcome

Bundling a complete standard library with the compiler must not bundle that library into every application. Native executables include the runtime facilities reachable from the program and their real dependencies. Fluent spelling must have the same retention behavior as an equivalent ordinary call.

Keep language guarantees intact: size optimizations cannot remove cancellation checks, cleanup, diagnostics or required behavior. Go remains the native backend; use its compiler/linker elimination where sufficient and fix package/import/initialization boundaries where that mechanism cannot remove unused work.

## Reachability contract

- Start from the executable entry point and genuinely exported host entry points. Account for callbacks, function values, provider methods, error/codec plans and target-specific adapters through the common checked type/interface model.
- Emit only required native runtime modules. An unused facility must not retain its imports, package initialization, global registry entries, reflection metadata or generated codec tables. Importing an interface for checking does not make its implementation a runtime root.
- Reconcile previously emitted files when a program's reachable modules shrink. A stale generated HTTP file must not reintroduce imports after an HTTP-to-minimal rebuild. Use artifact-owned output identities/manifests or equivalent isolated emission; do not delete unknown user files or let one generated application silently replace another application's selected runtime.
- Bundle runtime source and checked interfaces with the compiler distribution, but select application emission separately. The compiler CLI's distribution size and the built application's size are distinct measurements.
- Lower statically resolved fluent calls to ordinary functions. Do not create a universal runtime object, method registry, reflective dispatch table or blanket init solely to support fluent syntax. This requirement does not introduce fluent syntax by itself.
- Keep conservative roots explicit when dynamic/foreign behavior needs them. Never drop callable behavior merely because a closed-world walk cannot see a call. Diagnose unsupported behavior or retain the documented finite implementation set; do not quietly retain the whole library as a default fallback.
- Use module/contract identity to share reusable codec/runtime implementations rather than copying them at every call site. Generic specialization needs code-size evidence as well as checking-time bounds.
- The JS target should preserve tree-shakable module boundaries and static imports. Report emitted module size, bundled/minified application size and external runtime dependencies separately; a tiny wrapper with an uncounted Effect dependency is not a small complete deployment.

Before application planning, `runtime/effra.Sources()` copied every runtime source file into generated Go modules, including the HTTP file and its imports when no HTTP operation was called. Linker removal of unreachable functions alone does not establish removal of package initialization, so selection happens at the source emission boundary below.

The preparatory source seam exposes `runtime/effra.SelectSources`, which closes an explicit root set over the `core`, `layers`, `sync`, `http`, `files`, `console`, `env`, `inspect`, and `interop` modules while `Sources()` remains the full admitted set bundled with the compiler. `SelectModules` is the catalog's single dependency authority: it closes roots over declared module dependencies, and `SelectSources` copies exactly the files of that closure. `layers` depends on `core`. `core` conservatively groups effect, scope, fiber, managed-signal, and scheduler sources because their ownership and scheduling types are mutually connected; this grouping is a source boundary, not an application reachability or binary-size claim. Empty roots select no sources, and unknown roots fail explicitly.

The compiler-owned application plan (`Result.ApplicationPlan`) computes the deterministic emission closure of one concrete entry mode: the checked effect `main` for ordinary builds, or the selected `test_` cases plus the harness fixture providers for tests. It reads checker resolutions, layer plans and canonical type nodes only, never graph or projection output. It retains reachable functions, operations, providers with every method body, selected public and hidden layer nodes with their effective replacements, data declarations and templates, foreign bindings with their Go imports, lowering helpers, and native runtime modules as `SelectSources` roots. Builtin services and providers declare the runtime modules their generated implementations reference. Callable values originate only at checked function-value references, so the retained references form the finite conservative target set of every dynamic call. Every source declaration is still checked. Planning is bounded, and `EF136` refuses an exhausted closure without returning a partial plan.

Native emission consumes one plan per generation. `Result.GoApplication(mode)` lowers only the plan's services and their context fields, builtin and source providers, layers, functions, data, error and template declarations, lowering helpers and Go imports; the test harness binds exactly the plan's harness fixtures. The same application supplies the immutable generated snapshot, which contains the generated main and exactly the plan's selected runtime sources, so a declaration cannot refer to an unselected module. Managed programs always retain `core`, which owns scopes, fibers, cancellation and the scheduler; that dependency is required behavior, not removable overhead. The CLI builds, runs and tests through that application; `ef check` and MCP `project.check` report each declared native entry mode's closed runtime modules and requirement counts, or its `EF136` diagnostic, and a refused build prints the diagnostic without publishing output. An HTTP-to-minimal rebuild of one source origin publishes a new generation whose files and transitive Go dependencies equal a fresh minimal build; earlier generations and other applications remain untouched. Tests assert deterministic module and declaration selection only. Size, symbol and timing evidence in the acceptance matrix remains outstanding.

## Acceptance matrix

Use source-controlled public programs with matched toolchains, target, CGO mode, build tags and linker flags:

| Program | Required evidence |
| --- | --- |
| Pure minimal entry | No unused HTTP/RPC/codec/platform implementation or initialization; compare with an equivalent minimal Go entry. |
| Managed effect without platform I/O | Only the required core ownership/cancellation runtime and its dependencies; all guarantees still pass. |
| Ordinary handler actor without platform I/O | Retain selected protocol/dispatch and shared ownership/control dependencies, with no mandatory machine tables, durable storage, HTTP/RPC/codec or platform adapters. |
| Machine-backed actor without platform I/O | Retain the same shared actor ownership/control core plus selected machine functions, data and entry/completion adapter. A second machine does not duplicate the actor runtime; unused persistence/deployment modules remain absent. |
| Codec-only consumer | Required codec plans retained once; HTTP/RPC/server initialization absent. |
| HTTP application | Required transport/codec/domain behavior retained; unrelated RPC, SQL, streams and other modules absent when available. |
| Equivalent direct and fluent calls | Same semantic roots and comparable artifact size; fluent spelling does not retain other operations. Run this case when fluent syntax is admitted. |
| Unused library growth | Adding an unused module, operation or witness does not add it to application imports/symbols or materially grow the executable. |
| Rebuild after removing a facility | Build HTTP then minimal into the same managed application output; selected modules, dependencies and symbols match a fresh minimal build. A separately generated application keeps a coherent runtime. |

Record generated module selection, transitive build dependencies and native symbol evidence alongside exact byte sizes. For stripped release builds, retain an identically configured unstripped companion for symbol inspection. Record compiler/runtime/library source identities, Go/JS versions, architecture, build flags and size measurement method. Do not use debug stripping, compression or externalizing a runtime as a substitute for proving unused-code elimination.

A machine declaration available only to checking/inspection does not root the actor runtime. Actual spawning roots its implementation and necessary core dependencies. Retain snapshot tables and explicitly named payload codecs only through their real consumers; avoid global actor registries and blanket initialization. The actor runtime may require core scheduling for owned progress: do not promise removal of a necessary dependency merely because application steps contain no explicit delay.

Establish per-fixture budgets from measured baselines rather than inventing a global target. Keep raw before/after bytes and losing results. Build-stage timings belong to the separate compile-performance suite; contention-independent byte counts do not justify compile-speed claims.

## Delivery

The source-module split must also work over outputs from the previous compiler. The current additive writer leaves the former `stdlib.go` alongside its replacement declarations: both fresh versions compile, but the upgrade fails. Implement isolated, owned complete generated modules before integrating that split. This output boundary can proceed before canonical application reachability, retaining current all-source emission. Full source origin, target and ordinary/test mode distinguish artifacts; interrupted publication, concurrent builds and unchanged reuse need explicit controls. Do not satisfy this requirement by deleting old output directories in the gate or special-casing a retired filename.

1. Implement module selection through the versioned bundled interface/runtime boundary and preserve both-target behavior.
2. Add minimal and managed-effect size/dependency fixtures, then codec and HTTP fixtures as those libraries land.
3. Integrate deterministic reachability checks into the gate and record build-size receipts through CLI/MCP capability inspection. Keep expensive toolchain/bundler matrix runs in an explicit required size-conformance command.
4. Recheck new library modules and any fluent API against the same matrix. No capability is called tree-shakable solely because its surface uses static types.
