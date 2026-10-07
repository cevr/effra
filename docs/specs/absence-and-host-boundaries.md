# Absence without nil language values

Owner requirement, 2026-10-06: do not represent nil in Effra. This contract also excludes a bare null language value, nullable ordinary types and observable uninitialized fields. It sharpens [generic data](language-abstractions.md), [Go protocols](go-protocol-interop.md) and future TypeScript interop. It does not prohibit target runtime internals, protocol JSON null or explicitly tagged wire data.

## Ordinary values

An admitted `T` is an initialized value of that type. Required record/variant fields and function results cannot silently receive target zero values. Optionality is an ordinary closed alternative; bundled `Option<T>` has `None` and `Some(T)`, with exhaustive matching and the same type/ownership rules as other enums. `Some` cannot contain a missing or unchecked native reference. No postfix nullable type, implicit default nil or unchecked unwrap is introduced.

Current source lacks a nil/null literal and checks required record fields. Ordinary generic closed enums now express absence through bundled Option with checked constructors, exhaustive single-subject matches and retained payload ownership. The wider nullable host bridge still requires implementation; generic data admission is not evidence of host adaptation. A `void` result is a present no-value completion, not absence. `Option<void>` still distinguishes `Some { value: void }` from `None`; its target carrier (`struct{}{}` or `undefined`) does not erase the tag.

## Native Go boundary

Keep original Go type/assignment authority and concrete values. Nullable native results adapt to Option or an equally explicit specified alternative; a nil pointer, map, slice or function must never be returned as an ordinary Effra host reference. Distinguish native nil from a present empty container when native round-trip semantics depend on that distinction. Unsupported adaptation diagnoses before emission.

Native interfaces need a separate rule: a nil interface is absent, while a nonnil interface holding a nil dynamic payload is still a present Go interface. Preserve that distinction as opaque host-boundary state. Do not expose its nil dynamic pointer as an admitted source pointer; a downcast/result projection to such a pointer must produce absence. A present interface does not prove its methods are safe, nonblocking or cancellation-aware. Managed invocation preserves the actual foreign defect/trust policy.

Preserve a downcast/helper's match status separately from its adapted pointer value: a successful match yielding a typed-nil pointer and a failed match are different native outcomes. Neither outcome exposes a nil source pointer. This follows the complete-result rule, rather than collapsing both into one None result.

In particular, an error interface containing a typed nil pointer is an error under Go's `err != nil` rule. Preserve the original error identity behind an admitted opaque native error handle, useful partial results and supported `errors.Is`/`errors.As` behavior. Do not convert it to `None` or success. Source code uses checked alternatives and error helpers, never `err != nil`. The opaque handle is an actual source value; it is not permission to expose or dereference its underlying nil pointer.

Optional native parameters may lower `None` to Go nil only under their explicit imported boundary contract. Required nonnullable source parameters never do so implicitly. A compiler can synthesize shape-preserving presence adaptation and native calls, avoiding handwritten per-package wrappers; unwrap/proof and owner facts remain checked. Present native values retain their original concrete identity and optional methods. Representation wrappers cannot hide `WriterTo`/`ReaderFrom` or replace the underlying object passed to native Go calls.

No signature alone establishes semantic presence. Pointer-typed fields inside a imported struct or interface downcasts need the same boundary adaptation. Native containers/shared aliases require fresh checking when values enter source; a prior observation cannot justify a later unchecked load after host mutation. A full lifetime or arbitrary foreign race-freedom proof is outside this contract.

## Wire and JavaScript boundaries

JSON null is wire data. A codec may map it to None under an explicit representation policy, or preserve it as a named `JsonValue.Null` alternative. Neither creates a bare null Effra value. Required fields reject null/missing input where their codec contract requires a present value. Missing, null and empty are not interchangeable by default.

Future TS/JS interop must adapt null/undefined/optional properties into checked alternatives. Where host round-trip behavior distinguishes them, preserve that distinction through a specified tagged representation instead of collapsing both to None. A declaration is a trust input; runtime absence checks occur at the admitted boundary. JSON-RPC null IDs and inspection JSON null fields keep their protocol meaning.

## Causal acceptance

- Separate source cases reject bare nil, bare null, nullable annotations, missing required fields/results and unchecked Option extraction on both targets. A present `void` value and explicit domain Missing/Found alternatives remain valid.
- Generic Option preserves exact payload types, exhaustive matching and owner provenance. Some of a shorter-lived acquired resource cannot escape its owner; a borrowed present value remains admitted where proven.
- Actual native calls distinguish nil pointer from nonnil pointer, nil/empty slice and map, nil interface from typed-nil dynamic payload, nil error from typed-nil error with partial results, and successful typed-nil matches from failed matches. CLI/MCP show the same boundary/presence facts without publishing a nullable source type.
- Native error identity tests exercise the real Go helper boundary. Native reference tests alone establish zero Effra passes. Unknown/unsupported adaptation diagnoses rather than producing an opaque unusable value.
- Native method fast paths and concrete object identity survive present-value adaptation. Mutation/downcast controls cannot use stale presence evidence. JS wire tests preserve required-field refusal and explicitly chosen absence policy.

These gates belong to generic-data and host-types, with their full review/gates and later managed-resource controls. No new syntax or runtime guarantee is claimed by this document.
