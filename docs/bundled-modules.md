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

## Decimal i64 text

The distributed `effra/i64` module converts between `i64` and decimal text and
provides the `I64ParseFailure` failure:

```rust
import I64 "effra/i64"

effect fn quantity(text: string) -> i64 raises { I64ParseFailure } {
    run I64.parse(text)
}

fn label(count: i64) -> string {
    I64.format(count)
}
```

`I64.format` is pure and total. `I64.parse` is an effect operation: text must
match Go's base-10 grammar `[+-]?[0-9]+` (leading zeros admitted, no
whitespace, prefixes or separators) and fit the signed 64-bit range. Syntax is
classified before range on both targets, and the failure message is
`invalid i64 syntax` or `i64 out of range`; it never echoes the input. Both
members have ordinary source signatures in the distribution, and only their
bodies are compiler-owned operations, so inspection, rows and interface
summaries treat them as ordinary bundled functions. Native output imports
`strconv` and JS output includes a helper only for the operations an
application reaches. There are no implicit `i64`/`string` conversions; see
[portable i64 arithmetic](research/numeric-arithmetic.md).

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
type: as a field or element of a value codec it is refused with EF138 at that
path (its field declaration is already EF102), and it is the derived type
only of a [failure witness](#failure-witnesses-and-typed-endpoints), which
must name a failure declared in its own module. Plans are limited to 4096
nodes and 65536 fields and variants.

`ef check` and MCP `project.check` report each witness under `codecs`, with
its domain and wire types and each direction's function, failures and
requirements, and its plan under `codecPlans` with stable node identities. A
native application retains a plan, its adapters, its generated writer and
the runtime `codec` module only through an executed direction. Decoding runs
on the plan; encoding runs the plan's generated writer, one Go function per
record, union and transform node, which reads the value in place and calls
each encode transformation with its typed domain value. A generated JS entry follows the
same selection: it compiles one codec per plan an executed direction reaches
and includes the shared engine once, only when such a plan exists, so an idle
entry has no witness and no engine and an encode-only entry omits the decode
witness. A library surface roots every direction of every witness: it exports
each witness as a frozen `{ decode, encode }` object and carries the engine
once whenever it derives any codec. Arrays, payload-failure codecs, error
accumulation and stricter excess-property profiles are not yet supported.

### Transformed witnesses

A witness may state a transformation between its derived type and a
distinct representation, and a parent witness composes transformed
witnesses explicitly:

```
derive emailJson = Json.codec<Email>(maxBodyBytes: 256, maxDepth: 1, decode: parseEmail, encode: renderEmail)
derive accountJson = Json.codec<Account>(maxBodyBytes: 4096, maxDepth: 3) with emailJson
```

`decode` and `encode` name ordinary first-order module functions of one
parameter, pure or effect, and are required together: `decode` takes the
representation and returns the derived type, `encode` takes the derived type
and returns the same representation. The representation's plan is derived
structurally, so the conversion is the only hand-written part. The two
directions need not be inverses; a normalizing witness may accept a legacy
spelling and always write the canonical one. The derived type itself must be
plain data: primitives, records, enums and generic data over them. A domain
that is or holds a resource (`File`, `Latch`, a fiber), a function or effect
recipe value is refused with EF138 at its path, because a decoded value
carries no ownership facts and must never smuggle a scope-owned resource
past the EF123 escape check. The check walks the domain's layout once per
type and charges one unit per type occurrence against a 4096-unit budget,
the plan node ceiling; a layout that needs more is refused with EF127.

Each direction function's rows are its structural failure plus the declared
failures and services of every transformation that direction runs, so a
decode-only service never appears on `encode`. A transformation's failure,
defect or interruption propagates unchanged and is never a
`JsonDecodeFailure`/`JsonEncodeFailure`.

Decoding validates the whole structure first and only then runs decode
transformations depth first in declared field order, each after the
transformations nested in its representation, so malformed input never
reaches a transformation or its services. Encoding runs each encode
transformation before those nested in its representation, as the bounded
writer reaches it, and retains nothing beyond the bounded output: once the
output exceeds `maxBodyBytes` it stops writing but still runs the remaining
transformations, so every transformation runs before a structural failure
is reported and a transformation's failure takes precedence over it.

### Failure witnesses and typed endpoints

A derive declaration of a failure declared in the module is a *failure
witness*. It is structural only (bounds, no transformations, no `with`),
has no `decode`/`encode` functions, and encodes the failure as
`{"_tag": "<Failure>", ...fields}`, the shape of a pinned Effect
`TaggedError` schema. It is used only as an endpoint's failure body.

`Http.endpoint` and `Http.routes` derive typed HTTP handlers over the
managed transport ([runtime contract](runtime.md#typed-endpoints)):

```
derive outOfStockJson = Json.codec<OutOfStock>(maxBodyBytes: 128, maxDepth: 1)
derive placeOrder = Http.endpoint(method: "POST", path: "/orders", input: orderJson, handler: place, output: receiptJson, status: 201) raises { OutOfStock: 409 outOfStockJson, Unknown: 422 }
derive api = Http.routes(placeOrder, health)
run Http.listen(address, limits, api)
```

Each declaration synthesizes an ordinary effect function of its name from
`HttpRequest` to `HttpReply`, so it is passed to `Http.listen`, called and
inspected like any handler. `method` (GET, POST, PUT, PATCH, DELETE),
`path` (an exact static path), `handler` and `status` (2xx) are required;
`input` and `output` name data witnesses. The handler takes the input's
domain type (or nothing) and returns the output's (or `void`); it may be
pure or effectful. `raises` maps every named failure the input's decode
transformations, the handler and the output's encode transformations can
raise to a 4xx/5xx status and optionally a failure witness of that failure.
An unmapped failure is `EF107`; mapping an unreachable failure, mapping one
twice, or mapping `JsonDecodeFailure`/`JsonEncodeFailure` is `EF138`.

The synthesized function's rows are every service those functions need,
every mapped failure and, when it encodes any body, the terminal
`JsonEncodeFailure`. A mapped failure is answered only when it is the whole
cause; a compound cause (a handler failure beside an unobserved failed child
or a cleanup defect) propagates complete, so a direct caller declares the
mapped failures, and `Http.listen` absorbs them as its generic 500. Inspection
(`endpoints` in CLI/MCP) lists each endpoint's method, path, status,
witnesses, handler, mapped responses with the stage and function that raise
them, its input profile and its terminal encoding policy.
Cancellation during a transformation stops the boundary after that
transformation's own scope closes; no later transformation runs.

`with` lists transformed witnesses of record and enum types. A listed
witness applies wherever its domain type occurs in the parent's
representation; its own representation uses only its own `with` list.
Selection is explicit: there is no global or implicit witness. A repeated,
self, structural, primitive-domain, ambiguous (two witnesses for one type),
unused or recursive entry is refused with EF138; an unknown witness or
function with EF102. A transform node adds no nesting of its own; its
representation's nesting counts against `maxDepth`.

Inspection reports a transformed witness's `representation`, its `with`
list, and each direction's `transformations`: each witness once, in the
direction's static plan order (decoding lists a nested witness before its
parent, encoding after it; fields and variants in declared order), with
the first plan path at which it applies, its function and that function's
own failures and requirements. The order describes the plan, not a run: a
value invokes only the witnesses on the union variants it holds, so one
selecting a later variant can invoke that variant's witness before a
witness listed earlier on another branch. Plans carry `transform` nodes
with the witness, child node, step index and both function identities;
structural nodes under a `with` scope have a scoped identity, so equal
scopes share a plan. [`examples/codec-transforms.ef`](../examples/codec-transforms.ef)
exercises pure, normalizing, refining and effectful witnesses, nested
composition and a configuration caller on Go and JS.
