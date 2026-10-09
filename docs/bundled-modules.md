# Compiler-distributed modules

Effra admits explicit portable imports from a finite compiler distribution.
`import Fns "effra/functions"` resolves the ordinary `Fns.call` forwarding
function, the pure `Fns.identity` function, the managed callback forwarding
function `Fns.forwardFile`, and `Fns.suffixed(input, suffix = "!")`, whose
constant default is bound at each caller that omits it (see
[constant parameter defaults](specs/language-abstractions.md#constant-parameter-defaults)).
Go package imports keep their
separate `import go` form. User package loading and version selection remain
unsupported and produce diagnostics without searching the filesystem or network.

`Fns.call` accepts a named effect callback from string to string. Its declared
row parameters preserve the callback's failures and requirements; execution
remains visible through `run`. The function body is ordinary checked `.ef`
source. Different import aliases refer to the same declaration identity.

The compiler indexes the available declarations, then loads only referenced
members and their declared dependency closure. Bundle source offsets belong to
separate source IDs, never to an injected user prelude. Check results include
the loaded bindings, exact source digests and a declared checker producer ABI.
This producer value identifies compatibility, not a reproducible binary hash.
User byte-offset queries continue to address the user's source only.

Referenced bodies have distinct target emission names and pass through the same
function, row and ownership checker as local helpers. Merely importing a module
does not select every implementation. The existing finite builtin prelude is
unchanged, and native applications emit only the bundled members their checked
entry reaches. This module boundary makes no binary-size or compilation-performance claim.

Each selected closure has a compiler-private versioned summary. Its strict
decoder requires all operational fields, including false, empty and absent
alternatives, and rejects incompatible schemas, stale bytes, invalid owners,
dangling references and cyclic callback graphs. Cached transport is immutable;
each check reconstructs occurrences and callback relations in its own arena.
Nominal/native identities must already have checked signature owners. Printed
names cannot admit another nominal or host type. Unsupported transient regions
and layouts produce an interface diagnostic.

Results distinguish source input, producer ABI, interface content hash, and a
hash of target function snippets generated in a deterministic isolated module
context. The implementation hash describes that generation context, not the
combined executable or runtime snapshot; final Go temporary numbering can also
depend on functions emitted earlier in the combined program.
Bodies still pass through the ordinary checker for emission, and their finalized
summaries must match the admitted transport. Borrowing and acquisition remain
distinct through `Fns.forwardFile`; callback forwarding does not grant an escape
from a closing scope. This boundary makes no cache-performance claim.

The finite distributed `effra/conversions` module also provides first-order
record-template `Codec` data and the source-backed `witness` factory described
in [the bundled-interface contract](specs/bundled-interfaces.md). Witness
construction is checked ordinary data construction; it does not implement
serialization, decoding, or round-trip laws. User generic declarations and
nested template data arguments remain unsupported. Public inspection JSON is
explanatory output and is never admitted as executable ownership proof.

## Structural JSON codecs

The distributed `effra/json` module provides the `codec` derivation and the
`JsonDecodeFailure` and `JsonEncodeFailure` failures. A derive declaration
names an explicit witness:

```
import Json "effra/json"

derive customerJson = Json.codec<Customer>(maxBodyBytes: 1024, maxDepth: 1)
```

The checker synthesizes two ordinary effect functions, `customerJson.decode`
(`string` to `Customer`, raising `JsonDecodeFailure`) and
`customerJson.encode` (`Customer` to `string`, raising `JsonEncodeFailure`).
They are called, passed and inspected like any other function; structural
derivation adds no service requirement. Several witnesses may derive one
type, and witnesses with the same type and bounds share one plan.

Derivation follows the `effra/json-structural-1` profile: strings, booleans,
`void` as `null`, full-range `i64` as a decimal string, first-order records
and closed payload enums discriminated by `_tag`. Excess properties are
ignored, the first issue stops decoding, and output follows declared field
order. Both bounds are required and are plan data; there is no default.
`maxBodyBytes` (1 to 1 GiB) bounds input and output bytes, and `maxDepth`
(1 to 512) bounds input nesting and must cover the type's own nesting. A
declaration that omits either bound is refused with EF138.
Functions, effect recipes, host and runtime types, bytes, generic
applications and empty enums are refused with EF138 and the field path at
which representation fails. A failure (`error`) declaration is not a value
type, so naming one as the derived type is refused earlier with EF102, like
an unknown type. Plans are limited to 4096 nodes and 65536 fields and
variants.

`ef check` and MCP `project.check` report each witness under `codecs`, with
its domain and wire types and each direction's function, failures and
requirements, and its plan under `codecPlans` with stable node identities. A
native application retains a plan, its adapters and the runtime `codec`
module only through an executed direction. A generated JS entry follows the
same selection: it compiles one codec per plan an executed direction reaches
and includes the shared engine once, only when such a plan exists, so an idle
entry has no witness and no engine and an encode-only entry omits the decode
witness. A library surface roots every direction of every witness: it exports
each witness as a frozen `{ decode, encode }` object and carries the engine
once whenever it derives any codec. Transformations, arrays, payload-failure
codecs, error accumulation and stricter excess-property profiles are not yet
supported.
