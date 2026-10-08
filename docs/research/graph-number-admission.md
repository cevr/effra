# Exact graph-depth admission

Graph requests arrive at one compiler-owned admission point from the CLI and
MCP. `DecodeGraphJSON` uses Go's [`Decoder.UseNumber`](https://pkg.go.dev/encoding/json#Decoder.UseNumber),
so a JSON token can reach `ParseGraphRequest` as `json.Number` without first
losing its spelling. The request must decide whether that token is an exact
non-negative integer before it is narrowed to the hop-depth domain.

This boundary follows the comparison recorded in
`R/spike-exact-graph-number-admission-native-20261008.md`: Go's
[`strconv.ParseFloat`](https://pkg.go.dev/strconv#ParseFloat) is appropriate
for the native `float64` domain, but it rounds nearby decimal values and cannot
prove that the original JSON token was integral. The Rust
[`serde_json::Number`](https://docs.rs/serde_json/latest/serde_json/struct.Number.html)
API makes the same representation-versus-projection distinction; no Rust
dependency or arbitrary-precision package is needed here.

For `json.Number`, admission first validates JSON number syntax, computes the
decimal scale from the mantissa and exponent, and checks integrality by counting
mantissa trailing zeroes. Exponent magnitudes saturate at the input length, so
the check is linear in the supplied token and never expands an exponent or
allocates an unbounded integer. Only after this exact check does it call
`ParseFloat`; its existing range-error behavior is retained. Thus `1.0`,
`1e0`, `-0`, and exact forms such as `1.2300e2` remain supported, while
`1.00000000000000000000001`, `1e-999`, `-1e-999`, malformed manually supplied
numbers, and `1e400` retain invocation refusal. Exact values above the 64-hop
limit still receive the depth-limit refusal. Native `int` and `float64` callers
continue to be checked in their supplied Go domains because their original JSON
spelling is unavailable by then.

The focused controls cover native admission plus raw CLI and framed MCP input,
including zero, negative zero, tiny fractions, fractional tails near the hop
limit, valid decimal/exponent spellings, malformed numbers, and huge exponents.
