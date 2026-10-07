# Host interop proposal

Binding boilerplate is a central adoption risk. Effra should consume existing host declarations automatically while making behavioral trust explicit. The wider host representation design remains open. The prototype now imports Go package functions automatically, with canonical native host types, explicit absence adaptation and complete result tuples; TypeScript imports remain a proposal.

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

The owner requires automatic interoperability with Go's standard protocols and native values. The [finite Go protocol contract](specs/go-protocol-interop.md) covers complete host types/results, receiver methods, native interface assignment, owned borrowing/adoption and callback bridges. Preserve optional method sets such as `WriterTo`/`ReaderFrom`, partial results and nil/error identity; keep resource behavior distinct from signature compatibility. Host types and complete returns (unit 1) and receiver methods, native interface assignment and the standard I/O and filesystem protocols (unit 2) are implemented below; owned resources and callbacks remain required follow-on units.

The prototype resolves modules and compiled export archives with `go list -deps -export -json`, then supplies an explicit archive lookup to `go/importer.ForCompiler` and checks with go/types. It does not use importer.Default or a nil lookup. `go list` exempts packages named on its command line from Go's import restrictions, so admission applies them for the generated main package (module `effra.generated`) and refuses a `package main`, a path with a `vendor` element followed by more path (a path that ends in `vendor` is an ordinary package), or an `internal` package whose parent tree does not contain the generated main package with `EF111` at the import declaration, whether or not a call uses it. A standard-library `internal` package is always refused: Go admits it only to importers inside GOROOT, and generated programs are never placed there, so generating beneath a custom GOROOT's source tree is not supported. Generated modules retain requirements/replacements and copy the source module’s go.sum. The CLI builds from that graph; local modules and a replaced transitive dependency have executable tests. Broader workspace/build-tag and remote SDK coverage remain work. The official go/importer documentation warns that its older default importers are not reliable module-aware loaders. Cache normalized dependency summaries keyed by module/build inputs, Go version, build tags, OS/architecture, and relevant imported type identities. Avoid dependency-source walks on each private edit.

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

Supported package functions are non-generic and non-variadic. `go/types` is the identity authority for every admitted parameter and result:

- `string`, `bool` and `int64` keep their Effra primitives. A present `[]byte` is `bytes`, but the slice is still nullable: its results adapt to `Option<bytes>` (nil is `None`, a present empty slice is `Some`), and a `bytes` argument is always a present slice. Native `int` and `uintptr` are distinct host scalars, never silently `i64`; an integer literal argument is admitted only within the range every Go platform gives the parameter (int32 for `int`). Other widths (`int32`, `uint8`, floats) diagnose.
- Exported, non-generic named types are canonical nominal host types identified by package path and name (`go:example.com/sdk.Client`), independent of the import alias. Pointers, slices and maps of admitted types are host types too. Unnamed structs, arrays, channels, function types, unexported types and types in packages generated code cannot import (`internal`, `main`) diagnose with the reason; `any` and unnamed interfaces whose methods generated code can spell are admitted.
- Source annotations use Go's own spelling, which is also what inspection displays: `sdk.Point`, `*sdk.Client`, `[]string`, `map[string]int`, `[]*sdk.Client`, and the predeclared `error`, `any`, `int` and `uintptr` (available only in a Go-importing program; source declarations of the same name win). Element types use Effra names where a primitive exists (`i64`, `bytes`).
- A nullable native result (pointer, slice including `[]byte`, map, interface, function, channel, or a named type over one) adapts to bundled `Option`: nil is `None`, every other value, including an interface holding a nil dynamic pointer, is `Some` with the original native value. A nullable parameter receives only a present value; `None` is never lowered to nil. There is no nil/null source value.

All imported calls are lazy and require Foreign, even familiar functions such as strings.ToUpper. Every native result is retained. Without a trailing error, one component is the call result and several are a tuple with positional fields `v0`…`vN`; `(T, bool)` stays a tuple, not Option. A trailing `error` produces `GoResult`: `.value` retains the complete partial value, `.hasError` reports Go's `err != nil` (a typed-nil error is an error), and `.error` is `Option<error>` holding the original native error, which can be passed back to Go helpers such as `errors.Is`. `.orFail()` explicitly adds GoError and preserves the whole partial value in its failure payload. Source failure payload inspection remains unsupported. A Go import implicitly loads bundled `effra/data` Option; source names its variants through its own `effra/data` import.

Methods follow Go's method sets. `run value.Method(args)` on any executed host value, a local or an expression such as `(run sdk.Open()).Close()`, is a lazy Foreign call. The receiver expression is evaluated once, with the arguments, when the recipe is constructed; the method runs on that original value each time the recipe executes. Native fields and method values are not admitted. Effra values are not addressable, so a pointer-receiver method needs a pointer (`*sdk.Client`); calling one on a `sdk.Point` value diagnoses rather than copying it to take an address, while a value-receiver method is callable on either. At a Go call boundary an argument is checked with Go's assignment rule, so a concrete host value is passed to a native interface (`io.Copy(sink, source)`) directly, without a wrapper: optional methods such as `WriterTo` and `ReaderFrom`, or `fs.ReadDirFS` for `fs.ReadDir`, keep their native dispatch. Elsewhere Effra identity is unchanged; this is not structural coercion between Effra types.

The standard I/O methods follow their documented contracts. Go's interface satisfaction identifies them, as `io.Copy` does: on a receiver implementing `io.Reader`, `io.ReaderAt` or `io.Writer`, `Read`, `ReadAt` and `Write` adapt their buffer and count. Effra `bytes` are immutable, so a read takes the buffer length (`run reader.Read(4096)`, `run file.ReadAt(16, offset)`) and returns `GoResult<bytes>` holding a fresh copy of the filled prefix. Data returned together with `io.EOF` or any other error stays in `.value` beside that error; no byte is dropped. `ReadAt` passes its offset through, so it neither moves nor depends on the reader's position. `Write` keeps its `int` count; a count below the buffer length with a nil error breaks `io.Writer`, and is reported as `io.ErrShortWrite` with the accepted count, as `io.Copy` and `bufio.Writer` report it. A count outside the buffer, or a negative read length, has no meaningful partial value and is a defect. Inspection names the `protocol` and the `buffer`, `filled` and `written` adaptations.

`value.as<T>()` on a native interface value is Go's comma-ok assertion. It returns the tuple `(Option<T>, bool)`: `v1` is the match status and `v0` the adapted value, so a matched typed-nil pointer is `(None, true)` and a failed match `(None, false)`; a non-nullable target is absent only on a failed match. An assertion Go would reject as impossible diagnoses.

Native `int` converts explicitly: `i64(n)` widens and is total; `int(x)` narrows an `i64` to `Option<int>`, `None` when the value does not fit the platform's int. A failed narrowing has no partial value to keep, so Option rather than a failure row; neither conversion is implicit.

The module-root `effra.bindings.json` attaches behavior assertions without restating a signature. A key is the go/types full name of the function or method declaration:

```json
{"effra.local/prototype/examples/sdk.Lookup":{"context":"fiber","cancellation":"cooperative"},
 "(*example.com/sdk.Client).Lookup":{"context":"fiber","cancellation":"cooperative"}}
```

A promoted method carries the contract of the method it promotes; an interface method such as `(io.Reader).Read` is a separate declaration.

`context: fiber` hides an actual first context.Context parameter and forwards the managed context. `cooperative` is allowed only with that forwarding; it is a reviewed assertion, not a type-system proof. Unclassified cancellation is reported as unknown. Metadata rejects unknown fields and invalid enum values when it loads; a key matching no used declaration has no effect. No signature implies purity or resource ownership.

A declared Go import also initializes its package, following Go's ordering and once-per-package semantics, before `main` or the test harness runs, whether or not a reachable call uses it. Pruning removes unreachable callers and bindings, never that initialization: a package no retained code names keeps a blank `_` import. Initialization is not a managed effect; it needs no `Foreign` provision, has no typed failure row and is not owned by a scope. Remove the import to remove its startup dependency. See [binary reachability](specs/binary-reachability.md#foreign-go-package-initialization).

CLI and MCP inspect/check report used binding signatures, forwarding, cancellation, provenance and each native parameter/result with its adaptation (`direct`, `present`, `option`, `error`, `context`, `receiver`); a method binding is named by its receiver, such as `(*sdk.Client).Close`. That display name is presentation only: each binding also carries its `identity`, the package-path-qualified receiver type plus the selected method (`go:(*example.com/sdk.Client).Close`, or `go:` plus the full name of a function), so two receivers that display alike stay distinct bindings; host types appear in the shared canonical type table with kind `host`. The application plan roots every reachable host type and its declaring package, including a method receiver's package that source never imports. Imported export bytes and normalized contracts participate in semantic revisions, conservatively including dependency changes; there is no persistent Effra import cache. `importMicros` exposes loader cost separately. Go builds never start a TS checker; JS rejects Go imports before invoking Go tools.

`examples/imports.ef` uses standard-library declarations and a real compiled local SDK fixture; `examples/host-types.ef` exercises nominal, nullable, typed-nil error and tuple results, methods, `io.Copy` and `fs.ReadDir` dispatch, reads and a short write over `examples/hosttypes`. Tests prove partial-value retention, context forwarding, replaced module resolution, and rejection after an imported return type changes. This fixture does not establish adoption of a complex third-party SDK. Full host object representation and TypeScript declaration consumption remain the next interop questions.
