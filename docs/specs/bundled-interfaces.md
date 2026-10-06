# Bundled interfaces and initialized witnesses

The compiler distributes finite `.ef` declarations under explicit module paths. `import Fns "effra/functions"` selects ordinary callable helpers; `import Convert "effra/conversions"` selects an ordinary first-order product and factory. Named functions, record fields, rows and ownership use the shared semantic model on Go and JavaScript. Import aliases are bindings, while declarations and template variables have qualified owners.

For a User/string conversion, a caller supplies both actual functions:

```ef
import Convert "effra/conversions"
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
effect fn main() -> string {
    let converter = Convert.witness(decode, encode)
    let user = run converter.decode("Ada")
    run converter.encode(user)
}
```

`witness` is checked source that initializes `Codec { decode: decode, encode: encode }`. The compiler implements record applications, checked callable shape constraints and ordinary field projection; it has no conversion execution instruction. A Switch/bool caller uses the same declarations with different domain, wire and function contracts. The witness does not implement serialization or prove laws such as round trips.

At a public boundary, spell all four slots: `Convert.Codec<User, string, effect fn(string) -> User raises {DecodeFailure} uses {Names}, effect fn(User) -> string raises {EncodeFailure} uses {Labels}>`. A shallow `callable effect fn(Wire) -> Domain` constraint checks mode, parameters and result, preserving the supplied full callable contract in its slot. Decode and encode rows remain independent. Constructing or forwarding the witness executes neither function; `run converter.decode(...)` executes the chosen recipe and contributes its own rows. Missing errors or services diagnose through the ordinary checker.

The finite implementation admits compiler-distributed first-order record templates and source factories, with at most eight parameters. Local witness applications infer directly from checked callable arguments. Public annotations require complete slots. User generic declarations, nested template data arguments, generic host layouts, recursive type computation and ambiguous inference diagnose. Required fields contain initialized values; missing fields and nil/null placeholders do not produce a witness. Existing finite records, enums, primitives and builtin File/Latch handles can occupy data slots; foreign opaque nominal layouts cannot.

Each callable field retains supplied declaration evidence, capture, ownership, child/evaluation and executed-row facts. Passing a witness through an ordinary helper retains field evidence through a declaration-owned parameter path. Borrowed handles and callback acquisitions therefore remain distinct when either field is invoked across a scope boundary. Unknown evidence stays conservative.

Public semantic inspection schema 5 adds qualified template parameters and shape references. Private interface and ownership schemas are version 2, with checker ABI 6. A bounded transport contains qualified template owners, variable/constraint references, full callable rows and complete initialized field occurrences. A receiving fresh arena admits those references against checked source owner tables. Strict decoding rejects missing/null/duplicate/unknown fields, stale identities, malformed shape slots, missing fields, field cycles and invalid parameter paths. Immutable cached transport carries no arena IDs or declaration pointers. Checking retained source bodies still verifies the final summary before emission.

Source closure, private interface content and implementation snippet identities are separate. The implementation identity covers the selected target's template declarations and ordinary named function snippets emitted in an isolated context. It is not a hash of the combined executable, runtime or final module; native temporary numbering can vary with earlier caller functions. Public CLI/MCP projections expose bounded canonical references and provenance, while private transport remains compiler-owned admission evidence.

Applications and canonical substitution are bounded. Type substitution visits at most 4096 memoized nodes and depth 64; field and ownership transports have depth 32 and 4096 entries, and each private transport is capped at 1 MiB. Exhaustion diagnoses rather than yielding an empty effect row or harmless ownership proof. Future layer/provider declarations must extend these ordinary owners and occurrences; this unit adds no layer or actor syntax/runtime.
