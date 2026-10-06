# Host interop proposal

Binding boilerplate is a central adoption risk. Effra should consume existing host declarations automatically while making behavioral trust explicit. The wider host representation design remains open. The prototype now imports Go package functions with primitive signatures automatically; TypeScript imports remain a proposal.

The [Gleam, ReScript and TypeScript source comparison](research/host-language-compilation.md) records implementation lessons and acceptance cases for target capabilities, host declarations, source maps and separate compilation. It does not imply these wider facilities are already shipped.

## Proposed default

Import host packages and their existing type declarations automatically. Retain their native values, named identities, methods, and data representations where appropriate. Generated calls and wrappers belong to the compiler. Avoid requiring users to restate every function/type in handwritten Effra declarations.

Go syntax is implemented; TS syntax is illustrative:

```rust
import go users "example.com/users"
import ts { createClient } from "@acme/sdk"
```

The two examples belong in modules for their respective targets. A Go import constrains that module to Go; a TS import constrains it to JS. Portable Effra services may have separate target providers. Imported Go and TS objects are not automatically interchangeable.

This requires host-aware representation and type compatibility to inform the language design. A closed set of Effra-only record/list/enum representations would otherwise force conversions at every foreign call. Portable user-defined ADTs still need an explicit host representation; efficient native use and portable serialization are separate concerns.

## Go integration

The owner requires automatic interoperability with Go's standard protocols and native values. The [finite Go protocol contract](specs/go-protocol-interop.md) covers complete host types/results, receiver methods, native interface assignment, owned borrowing/adoption and callback bridges. Preserve optional method sets such as `WriterTo`/`ReaderFrom`, partial results and nil/error identity; keep resource behavior distinct from signature compatibility. These are required follow-on units, not implemented support in the primitive importer below.

The prototype resolves modules and compiled export archives with `go list -deps -export -json`, then supplies an explicit archive lookup to `go/importer.ForCompiler` and checks with go/types. It does not use importer.Default or a nil lookup. Generated modules retain requirements/replacements and copy the source module’s go.sum. The CLI builds from that graph; local modules and a replaced transitive dependency have executable tests. Broader workspace/build-tag and remote SDK coverage remain work. The official go/importer documentation warns that its older default importers are not reliable module-aware loaders. Cache normalized dependency summaries keyed by module/build inputs, Go version, build tags, OS/architecture, and relevant imported type identities. Avoid dependency-source walks on each private edit.

Preserve Go named types, pointers, interfaces, methods, slices, maps, callbacks, and multiple returns as host types. Effra has no nil/null language value: native absence becomes an explicit closed alternative, normally Option. Retain host nil facts in inspection and preserve typed-nil error identity behind the boundary without exposing a nil pointer as an ordinary source value. [Absence and host boundaries](specs/absence-and-host-boundaries.md) specifies these distinctions; richer host adaptation remains pending. Shared mutation and callback retention need ownership/trust rules; Go's static types do not supply those rules.

Do not automatically discard the value in `(T,error)` or reinterpret `(T,bool)` as Option. A low-level imported call should preserve both values. A package contract or an explicit adapter may choose Result/error-channel semantics where partial successes are immaterial; that choice should not be guessed from the signature alone. Managed effect wrappers defer the actual call. Ordinary Go panics and interruption behavior need their own managed boundary protocol, not an invented typed error inferred from error return values.

Sources: [go/types](https://pkg.go.dev/go/types), [go/importer module-aware guidance](https://pkg.go.dev/go/importer), [go list export/module fields](https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules).

## TypeScript integration

Resolve .ts source and .d.ts declarations with TypeScript's checker and module resolution. Effra should consume a focused foreign type view, preserving object shapes, methods, overloads, null/undefined, generic instantiations, and host type identities where representable. Unsupported types should remain explicit foreign/opaque types or give targeted diagnostics; converting them to unchecked any would hide the boundary.

A TypeScript type-query bridge and content-mapper projection have distinct jobs. The projection allows TypeScript to check generated call shapes, report mapped diagnostics, and supply editor features. The bridge must expose enough structured resolved type information for Effra's own checking and inspection. The content-mapper protocol alone does not implement that bridge or define Effra semantics. Reimplementing all TypeScript conditional/mapped type computation inside Effra is not a credible fast-compiler plan.

Foreign declarations are trust inputs. Typed package declarations do not prove runtime value validity, absence of exceptions, cancellation behavior, purity, or callback lifetime. Declarations can support ergonomic automatic imports with visible provenance; a runtime decoder validates external data where a real value guarantee is required. Validation cannot prove an arbitrary callback's behavioral contract. Untyped/any results become unknown at a checked boundary; explicit unchecked access remains a visible escape hatch.

Emit native JS objects/arrays/functions for host-facing values where the representation contract allows it. Preserve class instances and receiver semantics. Avoid copying data merely to wrap it in an Effra-specific container. Changes to foreign mutable objects can invalidate refinements, so immutable views, conservative invalidation, copying, or a trusted boundary will still be needed; automatic structural typing alone does not solve aliasing.

Sources: [TypeScript structural compatibility and intentional unsoundness](https://www.typescriptlang.org/docs/handbook/type-compatibility.html), [content mappers](https://github.com/microsoft/typescript-go/pull/4712), [declaration-map follow-up](https://github.com/microsoft/TypeScript/pull/63936).

## Behavior contracts without signature duplication

A foreign signature describes callable/value shape, not whether calling it is pure, throws, honors cancellation, releases resources, retains a callback, or is safe to share across tasks. Import those shapes automatically; keep supplemental behavioral facts in versioned package contracts attached to symbol identities.

The conservative default should defer an unclassified foreign call and mark its foreign capability/trust boundary. JavaScript throws/rejections need an honest ForeignFailure or defect policy rather than a fabricated narrow error row. Go returned errors remain values until an explicit adaptation. Curated contracts can expose common APIs as pure functions or precise managed services. These contracts are reviewed trust assertions unless validated by stronger analysis; calling them compiler-proven would misstate the guarantee.

The initial target is one real Go package and one real TS package, used without hand-redeclaring their signatures, with direct field/method access, an honest failure boundary, and a test showing how dependency type changes surface. A design that only works for toy declarations does not establish the adoption story.

## Agent inspection and speed

Foreign inspection should report source package/version, host signature, the adapted Effra contract, target availability, representation/conversions, and which facts are checked, validated, assumed, or unknown. The same answers must be available through CLI and MCP.

Go-only compilation must not start a TypeScript checker. Cache host type summaries and reuse checker sessions for JS interop; editor projections should not force whole-project work on each build. Import/type-query cost belongs in compiler performance measurements.

## Declaration trust

Handwritten externals repeat signatures without proving implementation behavior. Routine APIs should import native declarations; exceptional unsupported interfaces can require an explicit trusted adapter. Opaque types need a usable field/method story or they merely relocate the wrapper burden.

## Implemented Go slice

```rust
import go strconv "strconv"

effect fn parse(text: string) -> bool raises {GoError} uses {Foreign} {
    run strconv.ParseBool(text).orFail()
}
```

Supported package functions are non-generic and non-variadic, with string, bool, int64 and []byte parameters, and no result, one primitive result, error, or `(primitive,error)` results. Go aliases of those types work; named host types, pointers, structs, interfaces, methods and other multi-results receive EF112 when called. These limits are explicit; there is no unchecked any fallback.

All imported calls are lazy and require Foreign, even familiar functions such as strings.ToUpper. A returned error produces `GoResult`: `.value` retains the returned value and `.hasError` exposes whether an error was returned. `.orFail()` explicitly adds GoError and preserves the native partial value in its failure payload. Source failure payload inspection remains unsupported.

The module-root `effra.bindings.json` attaches behavior assertions without restating a signature:

```json
{"effra.local/prototype/examples/sdk.Lookup":{"context":"fiber","cancellation":"cooperative"}}
```

`context: fiber` hides an actual first context.Context parameter and forwards the managed context. `cooperative` is allowed only with that forwarding; it is a reviewed assertion, not a type-system proof. Unclassified cancellation is reported as unknown. Metadata rejects unknown fields and invalid enum values. No signature implies purity or resource ownership.

CLI and MCP inspect/check report used binding signatures, forwarding, cancellation and provenance. Imported export bytes and normalized contracts participate in semantic revisions, conservatively including dependency changes; there is no persistent Effra import cache. `importMicros` exposes loader cost separately. Go builds never start a TS checker; JS rejects Go imports before invoking Go tools.

`examples/imports.ef` uses standard-library declarations and a real compiled local SDK fixture. Tests prove partial-value retention, context forwarding, replaced module resolution, and rejection after an imported return type changes. This fixture does not establish adoption of a complex third-party SDK. Full host object representation and TypeScript declaration consumption remain the next interop questions.
