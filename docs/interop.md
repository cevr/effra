# Host interop proposal

The user identified binding boilerplate as a central adoption risk, drawing on ReScript and Gleam, and prefers TypeScript's ability to consume an existing ecosystem. This document is an open architecture proposal. The prototype currently has no foreign imports.

## Proposed default

Import host packages and their existing type declarations automatically. Retain their native values, named identities, methods, and data representations where appropriate. Generated calls and wrappers belong to the compiler. Avoid requiring users to restate every function/type in handwritten Effra declarations.

Illustrative syntax only:

```rust
import go users "example.com/users"
import ts { createClient } from "@acme/sdk"
```

The two examples belong in modules for their respective targets. A Go import constrains that module to Go; a TS import constrains it to JS. Portable Effra services may have separate target providers. Imported Go and TS objects are not automatically interchangeable.

This requires host-aware representation and type compatibility to inform the language design. A closed set of Effra-only record/list/enum representations would otherwise force conversions at every foreign call. Portable user-defined ADTs still need an explicit host representation; efficient native use and portable serialization are separate concerns.

## Go integration

Load module-aware package types with the Go toolchain and go/types, initially using golang.org/x/tools/go/packages. The official go/importer documentation warns that its older importers are not a reliable module-aware loader. Cache normalized dependency summaries keyed by module/build inputs, Go version, build tags, OS/architecture, and relevant imported type identities. Avoid dependency-source walks on each private edit.

Preserve Go named types, pointers, interfaces, methods, slices, maps, callbacks, and multiple returns as host types. Expose nil possibility honestly rather than silently presenting a pointer as guaranteed present. Shared mutation and callback retention need ownership/trust rules; Go's static types do not supply those rules.

Do not automatically discard the value in `(T,error)` or reinterpret `(T,bool)` as Option. A low-level imported call should preserve both values. A package contract or an explicit adapter may choose Result/error-channel semantics where partial successes are immaterial; that choice should not be guessed from the signature alone. Managed effect wrappers defer the actual call. Ordinary Go panics and interruption behavior need their own managed boundary protocol, not an invented typed error inferred from error return values.

Sources: [go/types](https://pkg.go.dev/go/types), [go/importer module-aware guidance](https://pkg.go.dev/go/importer).

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

## Why the Gleam comparison matters

Gleam requires explicit type annotations on external declarations and documents that it cannot verify the foreign implementation's return types or even existence. Its external types are opaque, so manipulation generally needs external functions. That is a concrete precedent for the boilerplate/trust trade-off the user wants Effra to avoid; it does not establish that every Gleam integration is difficult.

Source: [Gleam externals guide](https://gleam.run/documentation/externals/).
