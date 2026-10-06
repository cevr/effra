# Small executables and reachable library code

Status: owner-established north star and authorized implementation requirement, 2026-10-06. This is a delivery contract, not a claim about the current prototype's binary size.

## Outcome

Bundling a complete standard library with the compiler must not bundle that library into every application. Native executables include the runtime facilities reachable from the program and their real dependencies. Fluent spelling must have the same retention behavior as an equivalent ordinary call.

Keep language guarantees intact: size optimizations cannot remove cancellation checks, cleanup, diagnostics or required behavior. Go remains the native backend; use its compiler/linker elimination where sufficient and fix package/import/initialization boundaries where that mechanism cannot remove unused work.

## Reachability contract

- Start from the executable entry point and genuinely exported host entry points. Account for callbacks, function values, provider methods, error/codec plans and target-specific adapters through the common checked type/interface model.
- Emit only required native runtime modules. An unused facility must not retain its imports, package initialization, global registry entries, reflection metadata or generated codec tables. Importing an interface for checking does not make its implementation a runtime root.
- Bundle runtime source and checked interfaces with the compiler distribution, but select application emission separately. The compiler CLI's distribution size and the built application's size are distinct measurements.
- Lower statically resolved fluent calls to ordinary functions. Do not create a universal runtime object, method registry, reflective dispatch table or blanket init solely to support fluent syntax. This requirement does not introduce fluent syntax by itself.
- Keep conservative roots explicit when dynamic/foreign behavior needs them. Never drop callable behavior merely because a closed-world walk cannot see a call. Diagnose unsupported behavior or retain the documented finite implementation set; do not quietly retain the whole library as a default fallback.
- Use module/contract identity to share reusable codec/runtime implementations rather than copying them at every call site. Generic specialization needs code-size evidence as well as checking-time bounds.
- The JS target should preserve tree-shakable module boundaries and static imports. Report emitted module size, bundled/minified application size and external runtime dependencies separately; a tiny wrapper with an uncounted Effect dependency is not a small complete deployment.

The current `runtime/effra.Sources()` copies every runtime source file into generated Go modules. That includes the HTTP file and its imports even when no HTTP operation is called. Linker removal of unreachable functions alone does not establish removal of package initialization. This emission boundary needs implementation and binary evidence.

## Acceptance matrix

Use source-controlled public programs with matched toolchains, target, CGO mode, build tags and linker flags:

| Program | Required evidence |
| --- | --- |
| Pure minimal entry | No unused HTTP/RPC/codec/platform implementation or initialization; compare with an equivalent minimal Go entry. |
| Managed effect without platform I/O | Only the required core ownership/cancellation runtime and its dependencies; all guarantees still pass. |
| Managed actor without platform I/O | Retain the shared actor loop and required core ownership/scheduling dependencies, with no unused HTTP/RPC/codec/platform adapters. A second machine adds its functions, data and adapter rather than a duplicate actor runtime. |
| Codec-only consumer | Required codec plans retained once; HTTP/RPC/server initialization absent. |
| HTTP application | Required transport/codec/domain behavior retained; unrelated RPC, SQL, streams and other modules absent when available. |
| Equivalent direct and fluent calls | Same semantic roots and comparable artifact size; fluent spelling does not retain other operations. Run this case when fluent syntax is admitted. |
| Unused library growth | Adding an unused module, operation or witness does not add it to application imports/symbols or materially grow the executable. |

Record generated module selection, transitive build dependencies and native symbol evidence alongside exact byte sizes. For stripped release builds, retain an identically configured unstripped companion for symbol inspection. Record compiler/runtime/library source identities, Go/JS versions, architecture, build flags and size measurement method. Do not use debug stripping, compression or externalizing a runtime as a substitute for proving unused-code elimination.

A machine declaration available only to checking/inspection does not root the actor runtime. Actual spawning roots its implementation and necessary core dependencies. Retain snapshot tables and explicitly named payload codecs only through their real consumers; avoid global actor registries and blanket initialization. The actor runtime may require core scheduling for owned progress: do not promise removal of a necessary dependency merely because application steps contain no explicit delay.

Establish per-fixture budgets from measured baselines rather than inventing a global target. Keep raw before/after bytes and losing results. Build-stage timings belong to the separate compile-performance suite; contention-independent byte counts do not justify compile-speed claims.

## Delivery

1. Implement module selection through the versioned bundled interface/runtime boundary and preserve both-target behavior.
2. Add minimal and managed-effect size/dependency fixtures, then codec and HTTP fixtures as those libraries land.
3. Integrate deterministic reachability checks into the gate and record build-size receipts through CLI/MCP capability inspection. Keep expensive toolchain/bundler matrix runs in an explicit required size-conformance command.
4. Recheck new library modules and any fluent API against the same matrix. No capability is called tree-shakable solely because its surface uses static types.
