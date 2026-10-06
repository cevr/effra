# Native codecs, HTTP and unary RPC

Status: implementation authorized by the owner on 2026-10-06 as the prerequisite for actual Effect framework server comparisons. This extends the [standard library direction](../standard-library.md) and [matched server benchmarks](server-benchmarks.md). It does not claim full Effect API parity.

## Outcome

An ordinary `.ef` application declares nominal domain data and typed effect handlers once, then exposes validated HTTP endpoints or unary RPC methods from bundled library contracts. The Go target produces a standalone executable. Canonical CLI/MCP inspection explains codecs, handler rows, transport contracts and their dependencies. Unsupported targets and protocol modes diagnose or reject explicitly.

The existing path-to-string `Http.serve` remains a transport control. Framework completion requires reusable contracts and real source programs, not raw JSON passthrough, duplicated handwritten signatures or compiler branches for benchmark endpoints.

## Shared language and library contracts

- Introduce a versioned bundled interface boundary with package-qualified identity. Load only referenced contracts; include interface versions in semantic revisions. An initial bundled-only loader must state that user package resolution is unavailable.
- Represent named function values through one structural type model: parameter types, success type, effect status, explicit failure and service rows. Check passing and invoking functions through typed parameters. Preserve lexical captures separately from invocation ownership. Replace the special HTTP Handler representation through this model without weakening existing programs.
- Support checked type applications needed by shipped witness/container interfaces, including `Codec<T>`. Resolve operations from the versioned contract rather than endpoint-specific checker branches. The [language abstraction contract](language-abstractions.md) requires explicit finite row parameters alongside function values, so helpers retain their argument contracts. General user-defined generics, lambdas and complex polymorphic row inference may remain separately gated with diagnostics; do not confuse explicit row parameters with unrestricted inference.
- Keep rows intact through endpoint registration and composition. Transport provision discharges transport authority, while domain services remain required. Only explicitly mapped typed failures become declared transport responses; defects and interruption remain distinct.
- Canonical inspection must expose these contracts consistently through check/inspect/query/graph and MCP. No separate tool inference from generated host source.

## Codec contract

A codec is a typed decode/encode witness with distinct wire and domain types. Rust-style structural derivation from canonical primitive and nominal data declarations is its starting point; Effect-style transformations are part of the required model, not a later substitute for it (owner clarification, 2026-10-06). Compiler-generated adapters connect source identity to reusable parsing, validation and encoding modules. The compiler does not embed whole runtime algorithms in emitted strings.

Keep three composable concerns: derive structural representation, validate/refine values, and transform between representations. A codec can decode a wire string into a nominal identifier or a transport record into a richer ADT; encoding has its own transformation and validation. Pure functions and checked effect functions use the same function/row model. Canonical inspection exposes domain type, wire type, decode failures/services and encode failures/services separately, including their contribution paths. A convenient public spelling must not erase any of these contracts. Structural derivation itself introduces no service requirements.

Composition combines the selected direction's failures and requirements. A decode-only service does not become an encoding requirement merely because both directions share one witness. Transformations remain lazy until the boundary executes them, run in its owning scope, and preserve interruption/defects distinctly from expected validation errors. Decode cancellation and failed encode cleanup must never publish a success response. User transformations are checked source functions, not arbitrary compile-time procedural macros.

Go-like surface decision after independent counsel: opt-in structural derivation should be concise, with the direction contracts carried by ordinary named decode/encode functions and exposed in full by inspection. First-profile renames, defaults and migrations use explicit wire types/functions rather than a growing field-attribute language. Keep named codec values explicit and allow several witnesses for one domain type: API versions and storage formats must not require nominal wrapper proliferation or global implicit witness selection. A default derived witness is a convenience, not a one-codec-per-type coherence restriction. Do not overload provider `impl` or introduce pure-function `throws` semantics merely to spell codec customization; use the common admitted function/recipe/Result contracts.

Derivation and format policy are separate. A profile chooses structural JSON rules; the matched Effect comparator currently ignores excess properties. A stricter profile may reject them, but must be separately named and tested. No counsel recommendation for strict defaults silently changes the comparator contract. A derived parent containing custom field transformations inherits their actual behavior; it cannot claim a structural inverse law just because its outer record was derived.

Do not assume every codec is a bijection. Specify identity round trips only for representations that support them; normalizing or lossy transformations need explicit canonicalization/equivalence laws and encode rejection where appropriate. Defaults, field renames and version migrations are explicit policy. Derivation cannot silently make a persisted wire-format change safe.

The first profile admits strings, booleans, unit where explicitly represented, full-range i64 via decimal strings, nested records, closed payload enums and declared payload failures. File, Fiber, providers, functions and effect recipes cannot cross this boundary. Recursive layouts remain unsupported. Plans must share canonical type DAG nodes; repeated substructure must not enumerate exponentially many paths.

Before accepting a codec implementation, record executable policy vectors for required and excess fields, null, duplicate keys, invalid UTF-8, unknown discriminators, integer range and spelling, escaping, body size and nesting limits. Go and the pinned Effect Schema comparator must apply the same profile. Strictness that the host parser cannot prove must be supplied by a shared admission layer or excluded explicitly from the accepted contract; do not silently label it validated.

Decode failure includes bounded field-path and reason information without dumping arbitrary input. Encoding has an honest failure contract. Parsing occurs once per boundary. Stable field/discriminator order establishes the canonical output bytes used by benchmarks.

Acceptance: two unrelated nominal records, a nested payload enum and a payload failure all round-trip or reject through public source programs; negative cases reject malformed or unsupported values. A payload codec specific to the benchmark does not meet this contract.

Transformation acceptance adds a pure string-to-nominal conversion, an explicit normalizing codec, and an effectful conversion with different decode/encode requirements. Test nested composition paths, encode-side rejection, missing service and undeclared failure diagnostics, cancellation with completed cleanup, direction-specific inspection and Go/JS parity. Exercise the same derived/transformed witness through an HTTP boundary and a non-server configuration or stored-data example. Do not add unrelated external work to a benchmark cohort; matched codecs must perform equivalent validation and conversion.

## HTTP contract

Separate managed HTTP transport from typed endpoint composition. Transport exposes method/path, bounded buffered request bodies, response status/headers/body, and explicit server limits. Endpoints declare method/path, input codec, success codec/status and each typed failure codec/status, and bind a typed effect handler. Route matching and response policy belong to reusable library code.

The first profile is HTTP/1.1 buffered JSON with exact static routes. It specifies success and failure responses plus 400 malformed input, 404 unknown route, 413 excessive body, 415 unsupported content type and 500 defect behavior. Wrong-method handling is an explicit endpoint profile choice: the pinned Effect framework comparator returns404, so its matched native cohort uses404 rather than silently comparing it with405. The shared wire corpus records exact status/body/content-type behavior. Match limits for Content-Length and chunked bodies. Explicit read-header/body/idle limits and admission bounds must be recorded with fixtures.

Every request has an owning scope linked to client disconnect and server cancellation. Shutdown stops admission, cancels active handlers, waits for children and finalizers, and only then reports completion. Response publication occurs after owned handler and encoding cleanup at the agreed boundary; cleanup defects cannot become success. Readiness and lifecycle receipts use explicit host facilities rather than sleep-based startup assumptions or workload-specific magic routes.

Acceptance: public `.ef` HTTP application, shared request/response corpus, invalid/oversized input, typed failure, client disconnect and shutdown with active work. No unobserved task remains after the completed shutdown receipt.

## Unary RPC contract

Define a reusable method table from request/success/failure codecs and typed effect handlers. Keep dispatch, serialization and transport separate. The initial profile uses pinned Effect's whole-message JSON RPC-over-HTTP transport: one Request per POST, a string request ID, and an array containing its terminal Exit response. It is not a claim of full JSON-RPC standard support.

Capture actual pinned Effect Request, Exit Success and Exit Failure messages as source-controlled vectors. Typed failures, defects and interruption must remain distinct. Match malformed envelopes, unknown tags, invalid payloads, body/admission bounds, cancellation and active shutdown. Batching, streaming, notifications and WebSocket are outside this profile and must be rejected consistently before domain work.

Acceptance requires the real pinned Effect RpcClient against the Effra server and a source/client fixture against the pinned Effect server. HTTP and RPC can reuse the same domain handler and codecs. Handwritten custom `{ok: ...}` framing is not a compatible implementation.

## Delivery graph and gates

1. Shared bundled interfaces, checked type applications and typed function values.
2. Reusable codec modules and canonical derivation.
3. Managed HTTP transport and typed endpoint composition.
4. Unary RPC dispatch/serialization on the same contracts.
5. Public fixtures, canonical tooling agreement and independent framework conformance review.
6. Matched framework measurements and fair profile-guided optimization.

Each logical implementation unit must compile and pass the full repository gate. Run race tests for native runtime changes. Independent review must challenge negative paths and compile scaling, rather than treating broad tests that contain one expected error as proof of every clause. Preserve source/build hashes and raw results. No scored benchmark runs while builds/tests are competing for the host.

The [benchmark contract](server-benchmarks.md) governs workload equivalence, resources, ordering and claims. A readiness flag alone is insufficient: framework admission depends on executable conformance receipts tied to source hashes. Full library parity remains separately tracked; completion here establishes a concrete buffered server slice.

## Upstream implementation and test provenance

The owner explicitly requested the Effect source and test suite as implementation inputs. Use the repository-cache workflow to inspect the upstream implementation and real application usage before adding library behavior. Pin reference tests to the same Effect release used by the JS target; record the upstream commit, license and per-file hashes. Retain the full upstream test/type-test corpus as an unchanged reference snapshot.

Copying a test is not porting it. Maintain a mapping from upstream behavior/test identifiers to runnable Effra Go/JS acceptance cases, deliberate semantic differences and unavailable facilities. Port applicable codec, HTTP/RPC, cancellation, scope and scheduling assertions at public seams. A copied TypeScript test that only exercises Effect cannot establish native Effra conformance. Public fixtures and documentation remain generic; application source and private test data are not copied into this repository.
