# Portable i64 arithmetic

Status: adopted implementation direction, 2026-10-08. This is a source-backed design record for the finite portable profile. It does not claim that the Foldkit corpus runs or that any benchmark result exists.

## Question and current boundary

At the research baseline, Effra already admitted `i64` data and emitted JavaScript `bigint` declarations, but the checker accepted `+` only for strings. Its binary-expression guard admitted matching primitive `bool`, `string` and `i64` values for equality, while `+` remained string-only; see [`internal/compiler/semantic.go` around binary expression admission](../../internal/compiler/semantic.go). [`internal/compiler/syntax.go`](../../internal/compiler/syntax.go) checked positive integer tokens and reported `EF001` when a token exceeded the accepted range; general arithmetic, unary-minus/minimum-value handling and cross-target normalization were unsupported. Representation is therefore not arithmetic support. A counter-specific enum, a TypeScript reducer, or an unchecked host helper would bypass the language boundary.

The first finite unit covers signed `i64` addition, subtraction, unary minus, comparisons, equality and literal-range checks. The second unit (2026-10-09, below) adds multiplication, division, remainder and decimal text conversion. Unsigned widths, floating point and checked-overflow operations remain separate questions.

## Primary source comparison

| Pinned source and inspected path | Mechanism observed | Effra decision and limitation |
| --- | --- | --- |
| [Go specification: Integer overflow](https://go.dev/ref/spec#Integer_overflow), [Constants](https://go.dev/ref/spec#Constants) | A value of a signed integer type has fixed width; run-time signed arithmetic is deterministic two's-complement overflow. Constants are checked for representability before assignment or compilation. | Use Go `int64` as the native profile and signed 64-bit wrapping for admitted operations. Check literals before emission and ensure constant-only expressions cannot leave Go with a compile-time overflow that JavaScript accepts. The source rules do not choose Effra's cross-target policy by themselves; the wrapping choice is an explicit Effra decision. |
| [ECMAScript `BigInt.asIntN`](https://tc39.es/ecma262/multipage/numbers-and-dates.html#sec-bigint.asintn), especially the `asIntN` abstract operation | JavaScript `bigint` has arbitrary precision; `BigInt.asIntN(64, value)` maps a bigint to a signed 64-bit residue. JavaScript does not silently coerce bigint and number arithmetic. | Emit bigint at the public JS boundary and normalize every admitted arithmetic result with `BigInt.asIntN(64, ...)`. Reject number/bigint mixing. This keeps the JS representation honest while explicitly imposing Effra's fixed-width profile. |
| Pinned [Effect 4.0.1 `Schema.BigInt`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/Schema.ts#L3101-L3120) and [`BigInt.sum`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/BigInt.ts#L80-L150) | Effect validates an existing bigint and exposes ordinary dual arithmetic functions. Its schema is a data boundary; its math functions do not establish a fixed-width Effra policy. | Keep arithmetic as an ordinary checked Effra operator with no service or hidden failure row. A TypeScript/Effect consumer sees `bigint`; it must not be mistaken for an Effra `i64` unless the adapter states the width and normalization policy. Effect's safe bigint division returning `Option` is weighed and rejected for the operator in the second unit below. |

The inspected Effra paths are the checker and emitter seams (`internal/compiler/semantic.go`, `internal/compiler/emit_go.go`, `internal/compiler/emit.go`) plus the existing Go/JS conformance tests (`internal/compiler/go_backend_test.go`, `internal/compiler/compiler_test.go`). The finite implementation stays within those compiler seams and their target-paired conformance tests.

## Ordinary target idioms and choice

Ordinary Go writes an `int64` value, uses `+` or `-`, and returns an `error` only when the application chooses a checked domain rule. It does not need an arithmetic DSL. Ordinary TypeScript uses `bigint` operators or Effect's `BigInt.sum`/`BigInt.subtract`; it cannot mix `number` and `bigint`, and arbitrary precision means a width contract must be visible at the boundary. Effra adopts the readable Go expression shape while making the cross-target width rule explicit in checking and JS lowering.

The chosen profile is signed 64-bit wrap on each admitted addition and subtraction; direct unary minus is the only admitted unary operator. JavaScript uses `BigInt.asIntN(64, ...)`, and Go uses typed `int64` operations. Comparisons and equality operate on Effra values produced by the checked profile. Literals, including the minimum negative value, are range checked before either backend runs. Generated JS declarations expose `bigint`; under the existing trusted host adaptation boundary, a foreign caller must provide a representable signed64 `bigint`. The current ABI does not validate or normalize arbitrary incoming host values and never coerces them through `number`, so a foreign adapter that accepts wider values must enforce the range before calling an exported i64 function. Pure operators add no services, failures or ownership behavior. Divide-by-zero is settled by the divisor proof in the second unit; checked overflow errors still belong to later explicit operations rather than an implicit operator failure row.

This fits **Go-like simplicity through regular abstractions** because callers use ordinary expressions and one target-neutral primitive rule. It fits **explicit contracts and clear guardrails** because width, literal bounds and mixed-type rejection are visible. It preserves **native Go server programs** and **honest target capabilities** while keeping the JS declaration as `bigint`. The tradeoff is that silent wrapping can surprise domain authors; a checked domain operation must be an explicit library function and cannot be inferred from a plain operator. It also spends a small compiler/lowering cost on per-operation JS normalization to protect cross-target behavior, while leaving range validation at the existing foreign boundary.

## Rejected alternatives and counterevidence

- JavaScript `number` is rejected for `i64`: its precision boundary cannot represent the full signed range and would make the existing bigint declaration dishonest. Incoming foreign arguments therefore stay a trusted `bigint` contract until a separate checked adapter is designed.
- Unchecked arbitrary-precision bigint is rejected as the Effra `i64` operator policy: it would diverge from Go `int64` and make the same source produce different values.
- An implicit overflow failure is deferred: it changes the pure operator's failure row and would hide domain policy in a primitive. A later checked arithmetic operation can add a named failure explicitly.
- Moving the counter update into TypeScript or encoding it as an enum is rejected: it would test a host workaround rather than Effra arithmetic and would not prove the ordinary language boundary.
- The policy remains revisable if a required Go protocol, a pinned JS consumer or a measured application shows that wrapping cannot be made explicit enough, or if a future unsigned/checked numeric profile needs a distinct contract. Go's constant rules and JavaScript's arbitrary precision are counterevidence against treating a single host's default behavior as universal.

## Finite execution required

The prerequisite closes only after two unrelated Effra callers run on both Go and JS: a counter/domain update and a server quantity or accounting function. The suite must include maximum-plus-one, minimum-minus-one, nested arithmetic, negative literals, constant-only expressions, mixed string/integer and bool operands, out-of-range literals, and a strict TypeScript consumer that sees `bigint`. A causal mutant that removes JS normalization and a control that leaves a Go constant expression to the compiler must fail. Each receipt records source, compiler, toolchain, target, generated declarations, raw failures and full-gate status. The implementation and focused tests provide this finite unit; the full corpus, benchmark and remaining numeric profiles require separate receipts.

## Multiplication, division, remainder and conversions (2026-10-09)

Status: adopted and implemented for issue #77. The operator matrix extends the signed-64 wrap profile; the divide-by-zero policy is a checked divisor proof; decimal text conversion is a bundled module with a named failure.

### Evidence

| Source (pinned where a checkout exists) | Mechanism observed | Bearing on Effra |
| --- | --- | --- |
| [Go specification, Integer operators](https://go.dev/ref/spec#Integer_operators) (local copy `/usr/local/go/doc/go_spec.html` lines 4940-4977, go1.26.5; generated code builds with go1.27.0) | `x / y` truncates toward zero and `x % y` takes the dividend's sign (`-5 / 3 == -1`, `-5 % 3 == -2`). For the most negative value, `x / -1 == x` and `x % -1 == 0` by two's-complement overflow. A constant divisor must not be zero; a zero divisor at run time is a run-time panic. | Native `int64` already has the wrap profile for `*`, `/`, `%`, including `MIN / -1`. Its zero-divisor behavior is an untyped panic, which Effra must not admit as an ordinary operator outcome. |
| [ECMAScript `BigInt::divide` and `BigInt::remainder`](https://tc39.es/ecma262/multipage/ecmascript-data-types-and-values.html#sec-numeric-types-bigint-divide) | A zero divisor throws `RangeError`; the quotient rounds toward zero; the remainder takes the dividend's sign. Values are arbitrary precision, so `-(2n**63n) / -1n` is `2n**63n`. | Truncation agrees with Go. `/` needs `BigInt.asIntN(64, ...)` for the one overflowing quotient; `%` cannot leave the range (its magnitude is below the divisor's) and is emitted without normalization. A zero divisor would be a thrown host exception, i.e. a defect. |
| Effect 4.0.1 [`BigInt.ts`](https://github.com/Effect-TS/effect/blob/460272d30457f4697d8b8c52cad41caccbcace08/packages/effect/src/BigInt.ts) at `460272d` (checkout `~/.cache/repo/effect-ts/effect`) | `divide` (L178-L183) returns `Option.none()` for `0n`; `divideUnsafe` (L217-L220) and `remainder` (L927-L930, documented "Throws a `RangeError`") are the raw operators. `fromString` (L850-L858) trims and delegates to `BigInt(s)`, so `" 42 "` and `"0x2a"` parse and the failure is an uninformative `None`. | Option is Effect's answer where TypeScript cannot narrow a `bigint` to nonzero. It allocates and forces a match even for `n / 2`, and Effect's own `remainder` still throws. Its parser accepts host grammar that Go's `strconv` refuses, so cross-target agreement needs an explicit grammar. |
| Go 1.27 [`strconv.ParseInt`](https://pkg.go.dev/strconv#ParseInt) (`$GOROOT/src/strconv/number.go` L111-L137, `src/internal/strconv/atoi.go` L172-L210) | Base 10 accepts an optional `+`/`-` and ASCII digits; failures are `ErrSyntax` or `ErrRange`. The digit loop returns `ErrRange` as soon as the magnitude overflows, before later invalid characters are seen. | Adopt the base-10 grammar `[+-]?[0-9]+`. Classify syntax before range on both targets so `"99999999999999999999x"` is a syntax failure everywhere rather than following Go's scan order. |
| MoonBit core `e96ede8`: `builtin/int64.mbt` L200-L220 (`+` wraps), L254-L269 (`*` "truncated according to two's complement"), L274-L296 (`/` truncates toward zero, "Throws a panic if `other` is zero"), L302-L321 (`%` keeps the dividend's sign, same panic); `builtin/panic_test.mbt` L91-L101; moonbit-docs `8d9f3ba` `next/tutorial/for-go-programmers/index.md` L582-L615 | `Int64` arithmetic wraps like Go and truncates like Go, but a zero divisor aborts. The Go-programmer tutorial presents the safe form as a guard: Go's `if b == 0 { return 0, errors.New("division by zero") }; return a / b`, and MoonBit's `if b == 0 { raise ValueError(...) }; a / b`. | Agrees with Effra's wrap and truncation profile. The guard is the idiom both languages expect the programmer to write and neither checks; Effra's checker requires it instead of trusting it, and the zero case never reaches an abort. |
| Elixir `91ee75b`: `lib/elixir/lib/kernel.ex` L484-L518 (`div/2`), L1131-L1153 (`rem/2`), L1600-L1630 (`/` operator); Erlang/OTP `516126e`: `system/doc/design_principles/secure_coding.md` L131-L134 (BEAM integer arithmetic) | Integers are arbitrary precision: overflow occurs only at several megabits and raises instead of wrapping, so there is no `MIN / -1` case. `div/2` and `rem/2` truncate toward zero (`div(-99, 2) == -49`, `rem` takes the dividend's sign) and raise `ArithmeticError` when the divisor is `0`; `/` always returns a float and raises on `0`. The typespecs are `div(integer, neg_integer \| pos_integer)` and `rem(integer, neg_integer \| pos_integer)` (L514, L1149): the contract already excludes a zero divisor, but only Dialyzer reads it, and the call still compiles. | Truncation agrees with Go, BigInt and Effra. The zero-excluding typespec is the same contract as Effra's divisor proof; Elixir leaves it advisory and turns a violation into a raised exception, while Effra makes it a compile-time requirement. Arbitrary precision is rejected for `i64` for the reason given in the first unit. |
| Decimal parsing: MoonBit core `e96ede8` `internal/strconv/strconv_int.mbt` L94-L136 (`parse_int64`, re-exported by `string/strconv.mbt` L16-L17); Elixir `91ee75b` `lib/elixir/lib/integer.ex` L308-L361 (`Integer.parse/2`) and `lib/elixir/lib/string.ex` L3140-L3159 (`String.to_integer/1`) | MoonBit's `parse_int64` is a `raise` function with distinct syntax and range errors; its default base 0 admits prefixes and `_` separators, and like Go it reports a range error mid-scan. Elixir's `Integer.parse` returns `{integer, rest} \| :error` and accepts trailing text (`"34.5"` gives `{34, ".5"}`); `String.to_integer` raises `ArgumentError`. | A typed raised failure with a syntax/range distinction (MoonBit) is the shape Effra adopts, as an effect row rather than an untracked `raise`. Effra fixes base 10 with no separators so both targets share one grammar, refuses trailing text instead of returning a remainder, and classifies syntax before range. |
| Rust std docs: [`i64::checked_div`](https://doc.rust-lang.org/std/primitive.i64.html#method.checked_div), [`wrapping_div`](https://doc.rust-lang.org/std/primitive.i64.html#method.wrapping_div), [`impl Div<NonZero<u64>> for u64`](https://doc.rust-lang.org/std/num/struct.NonZero.html) (documentation, no pinned checkout) | `/` panics on zero and on `MIN / -1`; `checked_div` returns `Option`; `wrapping_div` wraps `MIN / -1` but still panics on zero; unsigned division by a `NonZero` divisor is total and never panics. | A type-level nonzero proof is accepted prior art for total division. Effra reaches the same totality with a flow proof over immutable bindings and no wrapper type. |
| Gleam [language tour, Ints](https://tour.gleam.run/basics/ints/) and `gleam/int.divide` (documentation) | `x / 0` evaluates to `0`; `int.divide` returns `Result(Int, Nil)`. | Silently returning a value is rejected (below); Result-returning division is the Option alternative again. |
| Zig [`@divTrunc`](https://ziglang.org/documentation/master/#divTrunc) (documentation) | The caller guarantees the denominator is nonzero and that `minInt / -1` does not occur; violations are safety-checked illegal behavior (a panic in safe builds, undefined in unsafe builds). | Shows the two hazards a total operator must close; Effra closes zero with a proof and `MIN / -1` with the wrap profile. |

The Effra paths are the parser (`internal/compiler/syntax.go`, multiplicative precedence 3 and prefix operands at 4), the checker (`semantic.go` binary and `if` cases, `divisor.go`), the JS emitter (`emit.go`, per-operation `BigInt.asIntN`), the formatter (`format.go`, multiplication-star parser fact) and the bundled module loader (`bundled.go`, `i64_text.go`).

### Decision

- `*` is admitted for matching `i64` operands with signed-64 wrap: Go `int64` wraps natively and JS emits `BigInt.asIntN(64, (a * b))`.
- `/` and `%` are admitted for matching `i64` operands only with a **divisor proof**: a nonzero integer literal (optionally negated), or a local binding inside an `if` branch whose comparison of that binding with an integer literal excludes zero. The rule is uniform: evaluate the comparison with the binding at zero; if it is false, zero cannot reach the then branch, and if it is true, zero cannot reach the else branch. `if d == 0 { ... } else { n / d }`, `if d > 0 { n / d }` and `if d < 0 { n % d }` are proofs; `if d < 1 { n / d }` is not. Bindings are immutable, so the fact holds for every use in the branch, and shadowing introduces a new binding without the proof. Every other divisor, including a literal zero, is `EF150`.
- `MIN / -1` is `MIN` and `MIN % -1` is `0` on both targets: the wrap profile already chosen for `+`, `-` and unary minus, and Go's specified behavior. JS normalizes `/` with `BigInt.asIntN`; `%` needs none.
- Both targets truncate toward zero; the conformance matrix proves it with negative dividends and divisors, independently of Go (`-7 / 2 == -3`, `-7 % 2 == -1`, `7 % -2 == 1`).
- `*`, `/`, `%` bind tighter than `+` and `-`, and prefix operands (unary `-`, `run`, `fork`) bind tighter than every binary operator, so `-a / b` is `(-a) / b` as in Go and JavaScript. Truncation makes this observable at the minimum: `(-MIN) / 2` is `MIN / 2`, while `-(MIN / 2)` is its negation.
- Explicit decimal conversion lives in the bundled module `effra/i64`: `I64.format(value: i64) -> string` is pure and total (Go `strconv.FormatInt(v, 10)`, JS `String(v)`), and `I64.parse(text: string) -> i64 raises { I64ParseFailure }` is an effect operation whose failure enters the caller's row. The grammar is Go's base-10 `[+-]?[0-9]+` (leading zeros admitted), syntax is classified before range, and both targets carry the same bounded message (`invalid i64 syntax` or `i64 out of range`) without echoing the input. Range is decided on the decimal text after stripping the sign and leading zeros (digit count, then a lexical compare with `9223372036854775807` or `9223372036854775808`), so neither target calls `BigInt` or keeps an input-sized `NumError` copy for unbounded input; JS `BigInt` over long text is super-linear and engine-limited, and its throw would surface as a defect. There are no implicit conversions: an `i64` where a `string` is expected, `string + i64`, and mixed `i64`/`bool` operators remain `EF106`. The existing native host conversions `i64(n)` and `int(x)` are unchanged.

This keeps **explicit contracts**: no pure operator gains a hidden failure row and no host panic or `RangeError` can escape as an untyped defect. It makes the invalid operation **unrepresentable** rather than reported, as the 2026-10-08 abstraction direction asks. It is **zero-cost**: an admitted division lowers to the bare host operator after the guard the programmer already writes in Go, with no `Option` allocation or residual check. It reads as **Go-like** source, because the proof is the idiomatic guard. Parsing is a boundary decode with a named failure, matching the derived JSON codec's `JsonDecodeFailure` and Effect's typed decode errors.

### Rejected alternatives

- **Go's run-time panic / JS `RangeError` / MoonBit abort / Elixir `ArithmeticError`** (raw operators): a zero divisor becomes an untyped defect or a raised exception outside the effect contract, and the two Effra targets would report it differently. Elixir's zero-excluding typespec shows the right contract but leaves it advisory.
- **`/` returning `Option<i64>`** (Effect `BigInt.divide`, Rust `checked_div`, Gleam `int.divide`): sound, but every division pays a match and an allocation, including `n / 2`; the operator would need the bundled `Option` without an import; and it encodes the proof the guard already gives. A user who wants it writes `if d == 0 { None } else { Some { value: n / d } }`.
- **A typed failure in the row** (`raises { DivideByZero }` on `/`): changes the pure operator's contract, forces pure arithmetic into effect functions, and is rejected for the same reason as an implicit overflow failure in the first unit.
- **Total division with a default result** (Gleam `x / 0 == 0`): silently produces a wrong value with no contract trace.
- **Safety-checked illegal behavior** (Zig `@divTrunc`): a panic in safe builds and undefined behavior otherwise; neither is an Effra outcome.
- **Literal-only divisors**: correct but impractical; averages, page counts and rates all divide by run-time values.
- **A `NonZero` brand or refinement type** (Rust `NonZero`, GDP-style proofs): heavier than needed for this operator. A flow proof over immutable locals gives the same totality without new declarations; a brand remains an option if proofs must cross function boundaries.
- **Checking `MIN / -1` as an overflow failure** (Rust `/` panic): inconsistent with the adopted wrap for `+`, `-` and unary minus; a checked overflow profile remains a separate explicit operation.
- **`string(n)` / `i64(text)` conversion spellings**: Go's `string(int)` is a rune conversion that `go vet` flags, and `i64(...)` already names the total native-int widening; a failing parse should not share a spelling with a total conversion. **`Option`-returning parse** (Effect `BigInt.fromString`) drops the syntax/range reason a server reports.

### Execution receipts

`internal/compiler/numeric_multiplicative_test.go` runs 364 boundary cases (all pairs over `MIN`, `MIN+1`, `-7`, `-2`, `-1`, `0`, `1`, `2`, `7`, `MAX-1`, `MAX`, plus hand-written truncation, precedence and literal-only rows) through the same Effra callers on Go and JS, checks strict TypeScript declarations (`bigint` only), and shows that removing the JS normalization of `*` or `/` changes behavior. `numeric_test.go` holds the negative controls (`EF150` for zero, unproven, computed, shadowed and wrong-branch divisors; `EF106` for mixed or non-`i64` operands) while `cmd/ef/i64_text_process_test.go` runs 23 parse vectors and 5 format vectors through `ef test` on both targets with identical outcomes and messages, and `i64_text_test.go` holds the implicit-conversion refusals, per-operation retention, strict declarations and a JS grammar mutant (without the explicit grammar, `BigInt` parses `" 1"`).

## Elixir and MoonBit comparison

MoonBit fixes widths and wraps. `Int` (32-bit) and `Int64` addition, subtraction and multiplication follow two's complement, so `2147483647 + 1` is `-2147483648` (core `builtin/intrinsics.mbt:L227-L287`, `builtin/int64.mbt:L199-L269` at `e96ede8b`). Division by zero panics for `Int` and `Int64`, while `Int` `%` by zero is documented as undefined (`intrinsics.mbt:L290-L337`, `int64.mbt:L282-L310`). This corroborates Effra's explicit wrapping profile; the panic and the undefined remainder are the implicit division policies that the divisor proof above rejects. Elixir's `div/2` and `/` raise `ArithmeticError` on zero, and `/` always returns a float (`lib/elixir/lib/kernel.ex:L483-L511`, `L1600-L1625` at `91ee75bb`). Elixir integers are BEAM integers, whose arithmetic is arbitrary precision and raises rather than wraps when it finally overflows at several megabits (`system/doc/design_principles/secure_coding.md:L131-L134` at OTP `516126e9`); OTP's memory guide distinguishes small from large integers (`system/doc/efficiency_guide/memory.md:L40-L51` at OTP `516126e9`). Arbitrary precision stays rejected for `i64` because it would diverge from Go `int64`. Pins are in [PRIOR_ART](../../PRIOR_ART.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09).

## Cost of checked overflow in emitted Go (#108, 2026-10-27)

Status: measurement record and decision input for [#81](https://github.com/cevr/effra/issues/81). It changes no compiler behaviour. The benchmark-last rule (#48, #62) applies: these numbers are decision evidence on one loaded host, not a performance claim.

### Question and hypothesis

#108 asks what an overflow-checked `+`, `-` and `*` costs against wrap in direct-Go output, and whether "checked by default" is affordable, as Tov claims for its release builds. The hypothesis was that a compare-based or `math/bits` check costs at most 10% on a tight arithmetic kernel. **The hypothesis is refuted for tight kernels.** The cheapest checked accounting sum runs at about 2.1 to 2.3 times the wrap time. The cheapest check for one operator costs 1.24 to 1.75 times wrap for `+` and `-` and about 2.0 to 2.7 times wrap for `*`. The second half of the hypothesis, "nothing elsewhere", was not measured: application code where arithmetic is a small share of the time is outside this spike.

### Method

The package [`overflow-bench`](overflow-bench/) holds hand-emitted Go. Each kernel has three policy variants. **wrap** uses the plain `int64` operators, the signed-64 wrap profile of the sketch ([types](../spec/types.md#basic-types), rule 2). **defect** checks and then panics, as the prototype emitter lowers a failure in a frame that cannot fail. **fail** checks and then returns a typed failure: a function with `error[Overflow]` lowers to a Go `(T, error)` result, the error type is a value struct with a value-receiver `Error` method, and each fallible call is followed by `if err != nil { return 0, err }` ([rows](../spec/rows.md); `internal/sketch/proto/emit.go`, `signature` and `fail`).

Each checked variant comes in check families that differ only in how the test is spelled:

| Family | `+` and `-` | `*` |
| --- | --- | --- |
| cmp | compare before the operation (CERT INT32-C): `b > 0 && a > MaxInt64-b \|\| b < 0 && a < MinInt64-b` | multiply, then divide back (`p/a != b`, plus the `-1 * MinInt64` case) |
| post | compare the wrapped result with an operand: `(s < a) != (b < 0)` | `bits.Mul64` |
| xor | test the sign bits of the wrapped result: `(s^a)&(s^b) < 0` | `bits.Mul64` |
| bits | `bits.Add64`/`bits.Sub64`; signed overflow is the carry out of bit 63 xor the carry into it | `bits.Mul64`; the signed high word must be the sign extension of the low word |
| range | as xor | plain `IMULQ` when both operands fit in 32 bits (one biased unsigned compare), else `bits.Mul64` |

The kernels are:

1. **Accounting sum (the cost measurement).** `total += qty*price - discount` over 1,024 lines, one `*`, one `-` and one `+` per line. Quantity is at most 1,000, price at most 1e9 and the discount at most a tenth of the line, so no input overflows. `TestAccountingSumAgrees` requires every variant to return the `math/big` total with no failure, on the benchmark input and three other sizes, before the runner records any timing. `TestAccountingSumOverflowControl` gives one input that overflows at `*`, one at `-` and one at `+`; every checked variant must refuse each one and wrap must return the wrapped total, so no check is dead.
2. **Hash mix that relies on wrap (a policy comparison).** `h = h*31 + b` over a key, the polynomial string hash. It is excluded from the agreement check. With lowercase letters, `h*31` first leaves the `int64` range at byte index 12, so a key of at most 12 bytes never overflows and a longer key always does (`TestHashMixPolicy` checks every prefix up to 40 bytes against `math/big`, and the all-`a` and all-`z` keys as bounds). The timed input is 256 keys of 12 bytes, where all variants agree, plus one 4,096-byte key for the wrap variants only.
3. **Operator kernels (attribution).** One checked operator per loop, defect style, over 1,024 values in [-1e9, 1e9], to attribute the accounting cost to an operator and a family.

`TestChecksAgreeWithBig` compares every check function with `math/big` on all pairs of 64 operands: the boundaries (`MinInt64`, `MaxInt64`, `±3037000500`, `±2^32` and others) and pseudo-random values of every magnitude. Six hand mutants (a flipped sign test, a dropped high-word correction, a dropped `-1 * MinInt64` case, a one-sided post test, a removed `+` check in `SumDefectXor`, a removed `-` check in `SumFailBits`) each fail a test. A mutant that removes the `+` check from `HashFailBits` survives, because the hash overflows only at `*`.

The runner `go run ./docs/research/overflow-bench/run` builds the test binary once, runs the tests (a failure stops the run), then takes two series. The contiguous series is `go test -bench . -benchmem -count 10 -benchtime 1s`. The interleaved series runs one process per sample, each round in a fresh shuffled order, 10 rounds, and divides each sample by the wrap sample of the same round (the paired ratio). It samples `/proc/loadavg` every 10 seconds and writes `go build -gcflags=-S` and `-gcflags=-m` output. Every kernel is `//go:noinline`, so each has its own loop in the listing and pays the same call. Results are written to [`results/20261027T150404Z`](overflow-bench/results/20261027T150404Z/summary.md).

### Host

AMD EPYC 9554P (16 logical CPUs, a shared workbox), Linux 6.12.93 x86_64, `go version go1.27.0 linux/amd64`, `GOAMD64=v1`, head `eb2a3761`. The receipt records `package clean: false` because the untracked C listing file [`c/accounting.c`](overflow-bench/c/accounting.c) was in the package directory; the Go build ignores it. The one-minute load average was 19.68 at the start. During the contiguous series it had a median of 4.68 (range 3.30 to 19.68, 41 samples); during the interleaved series it had a median of 5.02 (range 3.63 to 18.89, 41 samples). Other sessions shared the host, so the ranges below are wide and only the ratios carry the decision.

### Results

All variants allocate nothing: 0 B/op and 0 allocs/op in every row of both series.

Accounting sum, ns/op for 1,024 lines:

| Variant | Contiguous median [min, max] | vs wrap | Interleaved median [min, max] | Paired ratio median [min, max] |
| --- | ---: | ---: | ---: | ---: |
| wrap | 802 [704, 958] | 1.00x | 779 [649, 1296] | 1.00x |
| defect-cmp | 4008 [3530, 4187] | 5.00x | 3796 [3189, 6269] | 4.85x [3.43, 7.12] |
| defect-post | 2384 [2084, 2641] | 2.97x | 2535 [2192, 3387] | 3.45x [2.24, 4.28] |
| defect-xor | 2233 [2084, 2489] | 2.79x | 2328 [1945, 2720] | 2.72x [1.89, 3.59] |
| defect-bits | 2809 [2666, 3305] | 3.50x | 3068 [2634, 4854] | 3.86x [3.19, 5.37] |
| **defect-range** | 1714 [1581, 1895] | **2.14x** | 1801 [1498, 2030] | **2.23x [1.48, 2.68]** |
| fail-cmp | 4108 [3584, 4515] | 5.12x | 4066 [3524, 6160] | 4.82x [3.91, 8.20] |
| fail-post | 2940 [2566, 4145] | 3.67x | 2871 [2539, 4025] | 3.51x [3.08, 4.57] |
| fail-xor | 2298 [2245, 2855] | 2.87x | 2632 [2269, 3062] | 3.20x [2.13, 3.98] |
| fail-bits | 3376 [3179, 3797] | 4.21x | 3428 [3028, 3825] | 4.32x [2.44, 4.88] |
| **fail-range** | 2078 [1785, 2643] | **2.59x** | 2174 [1898, 2636] | **2.67x [2.03, 3.53]** |

Operator kernels, ns/op for 1,024 operations:

| Operator | wrap | cmp | post | xor | bits | range |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `+` contiguous | 356 | 1742 (4.90x) | 669 (1.88x) | **622 (1.75x)** | 995 (2.80x) | - |
| `+` interleaved, paired | 396 | 4.47x [4.16, 6.18] | 1.91x [1.24, 2.21] | **1.66x [1.12, 2.34]** | 2.64x [1.69, 3.20] | - |
| `-` contiguous | 398 | 1692 (4.25x) | 693 (1.74x) | **526 (1.32x)** | 983 (2.47x) | - |
| `-` interleaved, paired | 415 | 4.18x [3.21, 4.89] | 1.81x [1.40, 2.18] | **1.24x [1.02, 1.57]** | 2.52x [1.84, 3.15] | - |
| `*` contiguous | 537 | 2304 (4.29x) | - | - | 1358 (2.53x) | **1097 (2.04x)** |
| `*` interleaved, paired | 488 | 4.96x [4.15, 6.34] | - | - | 2.69x [2.40, 5.63] | **2.42x [2.09, 3.08]** |

Hash mix, ns/op:

| Input | wrap | explicit-wrap | defect-cmp | defect-bits | fail-cmp | fail-bits |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 256 keys of 12 bytes, contiguous | 2250 | 2304 (1.02x) | 4450 (1.98x) | 5028 (2.24x) | 6274 (2.79x) | 5128 (2.28x) |
| 256 keys of 12 bytes, interleaved, paired | 2394 | 0.99x [0.82, 1.16] | 1.92x [1.57, 2.90] | 2.34x [1.94, 2.84] | 2.46x [1.99, 3.14] | 2.32x [2.05, 3.39] |
| one key of 4,096 bytes, contiguous | 3577 | 3493 (0.98x) | panics | panics | `Overflow` | `Overflow` |
| one key of 4,096 bytes, interleaved, paired | 3573 | 1.00x [0.81, 1.05] | - | - | - | - |

Raw ns/op samples of the contiguous series, in run order (the interleaved samples are in the [summary](overflow-bench/results/20261027T150404Z/summary.md) and `samples.tsv`, with the load before each sample):

```
AccountingSum/wrap: 785 745 704 761 773 894 842 958 818 849
AccountingSum/defect-cmp: 3857 3828 4135 3530 4069 4084 3815 3948 4187 4129
AccountingSum/defect-post: 2399 2368 2188 2455 2084 2641 2329 2347 2480 2537
AccountingSum/defect-xor: 2094 2283 2252 2084 2214 2417 2489 2375 2117 2204
AccountingSum/defect-bits: 3272 3305 2957 2989 2831 2679 2704 2787 2724 2666
AccountingSum/defect-range: 1739 1581 1718 1895 1710 1660 1671 1733 1667 1816
AccountingSum/fail-cmp: 3584 4515 4416 4291 3633 3746 4161 4056 3923 4209
AccountingSum/fail-post: 2734 2873 2785 2566 3159 3315 4145 3097 3006 2611
AccountingSum/fail-xor: 2312 2245 2285 2486 2540 2273 2268 2462 2252 2855
AccountingSum/fail-bits: 3797 3638 3179 3390 3239 3517 3356 3441 3362 3225
AccountingSum/fail-range: 1785 2000 2148 2602 2643 1920 2010 2031 2125 2271
HashMix/short/wrap: 2294 2260 2215 2437 2253 2193 2246 2510 2185 2203
HashMix/short/explicit-wrap: 2300 2016 2235 2573 2083 2307 2422 2448 2369 2056
HashMix/short/defect-cmp: 4196 4553 4316 3988 4348 4881 4920 4623 4097 4642
HashMix/short/defect-bits: 5105 4952 5165 5226 4937 4745 4822 4838 5454 5975
HashMix/short/fail-cmp: 6493 6152 6448 6407 7227 6237 6193 6312 5499 5486
HashMix/short/fail-bits: 5197 5889 6598 4890 4904 4752 5218 5081 5176 4996
HashMix/long/wrap: 3533 3819 3513 3571 3611 3593 3545 3583 3561 3717
HashMix/long/explicit-wrap: 4072 3628 3531 3432 3493 3432 3493 3585 3469 3487
Op/add/wrap: 335 352 334 349 359 367 370 337 428 448
Op/add/cmp: 2031 1747 1735 1537 1563 1856 1671 1768 1736 1775
Op/add/post: 687 665 650 656 670 694 784 668 660 700
Op/add/xor: 667 592 633 612 592 631 649 612 604 638
Op/add/bits: 994 996 1063 955 997 976 960 986 1065 1029
Op/sub/wrap: 422 449 347 512 373 360 367 422 423 369
Op/sub/cmp: 1675 2130 1658 1642 1541 1708 1809 1762 1460 1794
Op/sub/post: 748 814 777 702 671 704 676 644 678 684
Op/sub/xor: 576 561 541 480 587 511 562 486 474 464
Op/sub/bits: 1050 921 940 998 1030 1108 969 960 1022 938
Op/mul/wrap: 472 532 500 505 528 561 548 542 596 557
Op/mul/cmp: 2845 2815 2497 2357 2187 2367 2162 2217 2251 2238
Op/mul/bits: 1441 1317 1357 1360 1376 1452 1494 1306 1328 1239
Op/mul/range: 1021 1013 1173 1132 1125 1069 1131 1133 1041 998
```

Losing variants stay in the record. **cmp** loses everywhere: its pre-check compiles to two data-dependent branches and two 64-bit immediates per `+` (4.2 to 4.9 times wrap), and its `*` pays an `IDIVQ` on every product. **bits** loses for `+` and `-`: `bits.Add64` gives the unsigned carry through `SETCS`, and the signed overflow needs six more instructions (`MOVBLZX`, three `XORQ`, `SHRQ`, `BTL`). **post** is close to xor for `+` but loses for `-`. A smoke run (not part of the receipt, load about 2 to 5) of a range helper that called `mulBits` inline measured 2,722 and 2,969 ns/op against 1,329 and 1,427 for bits: wrapped in a defect helper, the check exceeds Go's inlining budget and every product becomes a `CALL`. Outlining the slow path did not help, because the call itself costs most of the budget. The kept range kernels therefore emit the fast path inline, as an emitter writes code at the operation.

### What the Go compiler emits

From `results/20261027T150404Z/asm.txt` (`go build -gcflags=-S`, offsets and `kernels.go` lines kept, `XCHGL`/`NOP` inline marks dropped). The wrap loop is 9 instructions per line:

```
(kernels.go:201)  MOVQ   (AX)(SI*8), DI
(kernels.go:201)  MOVQ   8(AX)(SI*8), R8
(kernels.go:201)  IMULQ  R8, DI
(kernels.go:201)  SUBQ   16(AX)(SI*8), DI
(kernels.go:201)  ADDQ   DI, CX
```

The defect-xor loop is 36 instructions per line, 9 of them register-to-register moves. `bits.Mul64` becomes `MULQ`, which takes its operand in `AX` and writes `DX:AX`, so the slice base and the index are moved out of `AX` and `DX` and back on every iteration. The signed high-word correction adds six instructions. The `-` and `+` checks are `XORQ`, `XORQ`, `TESTQ` and a sign branch:

```
00060 (kernels.go:233)  MOVQ   AX, R9          // save slice base: MULQ needs AX
00063 (kernels.go:124)  MOVQ   DI, AX
00066 (kernels.go:233)  MOVQ   DX, R10         // save index: MULQ writes DX
00069 (kernels.go:124)  MULQ   R8
00075 (kernels.go:125)  SARQ   $63, DI         // signed high word = hi - (a>>63)&b - (b>>63)&a
00079 (kernels.go:125)  ANDQ   R8, DI
00082 (kernels.go:125)  SUBQ   DI, DX
00098 (kernels.go:126)  SARQ   $63, AX
00103 (kernels.go:126)  CMPQ   AX, DX
00106 (kernels.go:687)  JNE    199             // -> runtime.gopanic
00116 (kernels.go:104)  SUBQ   DX, DI          // net = line - discount
00119 (kernels.go:105)  XORQ   SI, DX
00122 (kernels.go:105)  XORQ   DI, SI
00128 (kernels.go:105)  TESTQ  SI, DX
00131 (kernels.go:663)  JLT    180             // -> runtime.gopanic
```

The range kernel guards a plain `IMULQ` with one biased unsigned compare, but materialises `$4294967296` in a register on every iteration and keeps the `MULQ` register shuffle on the fast path:

```
(kernels.go:149)  MOVL   $2147483648, R9
(kernels.go:149)  LEAQ   (R9)(DI*1), R10
(kernels.go:149)  LEAQ   (R9)(R8*1), R11
(kernels.go:149)  ORQ    R11, R10
(kernels.go:149)  MOVQ   $4294967296, R11
(kernels.go:149)  CMPQ   R11, R10
(kernels.go:263)  JLS    113             // -> MULQ slow path
(kernels.go:264)  IMULQ  R8, DI
```

The typed failure costs more than the defect because Go materialises the `(itab, data)` error pair and then tests it, rather than branching straight to the return:

```
(kernels.go:745)  JGE    157
(kernels.go:745)  LEAQ   go:itab.ob.Overflow,error(SB), AX
(kernels.go:745)  XORL   R8, R8
(kernels.go:745)  LEAQ   runtime.zerobase(SB), DX
(kernels.go:325)  JMP    161
(kernels.go:325)  XORL   AX, AX     // 157: no error
(kernels.go:325)  XORL   DX, DX
(kernels.go:326)  TESTQ  AX, AX     // 161
(kernels.go:326)  JNE    222
```

`bits.Add64` keeps the carry in a flag only long enough to copy it out (`ADDQ SI, CX; SETCS R8B; MOVBLZX R8B, R8`). A constant multiplier is the one case Go handles well: `h > MaxInt64/31 || h < MinInt64/31` folds into a single biased compare (`LEAQ (CX)(SI*1), DI; CMPQ R9, DI; JCS`), and the prove pass drops the negative half of the `+` check because the byte operand is known non-negative.

Go 1.27 offers user code no overflow-flag operation. The listing contains no `JOS`, `JOC`, `SETOS` or `SETOC` (`grep -c` gives 0), and `GOAMD64=v3` emits no `MULX` for `bits.Mul64` (also 0). For comparison, gcc 13.3 at `-O2` compiles the same kernel with `__builtin_*_overflow`, the builtins Tov emits, to 13 instructions per line with one `jo` per operation ([`c/accounting.c`](overflow-bench/c/accounting.c), compiled for its listing only, not timed):

```
.L17:
	movq	(%rdi), %rax
	imulq	8(%rdi), %rax
	jo	.L14
	subq	16(%rdi), %rax
	jo	.L14
	addq	%rdx, %rax
	movq	%rax, %rdx
	jo	.L14
	addq	$1, %rcx
	addq	$24, %rdi
	cmpq	%rcx, %rsi
	jne	.L17
```

That is the whole difference. A C or LLVM backend reads the overflow flag that `IMUL`, `SUB` and `ADD` already set. Direct-Go output must recompute the condition with ordinary instructions, and for `*` it must route through `MULQ` and its fixed registers.

### Policy comparison: code that relies on wrap

Under wrap, the hash returns the same value as an independent `uint64` computation for every key. Under either checked policy, every key of 13 bytes or more fails at byte index 12: the defect variants panic with the overflow defect and the failure variants return `Overflow`. Keys of 12 bytes or fewer pass. A test suite with short keys would pass, and production input with longer keys would fail. Under a checked default, such code must be rewritten with explicit wrapping operations. `HashExplicitWrap` uses named `wrapAdd` and `wrapMul` functions, which inline to the plain operators, and costs the same as wrap (0.98x and 1.02x contiguous, paired 0.99x and 1.00x). The rewrite is free at run time. Its cost is the review of every hash, checksum, PRNG and fixed-point routine.

### Prior art: how other languages lower the check

| Source | Default policy and spelling | Lowering observed or documented | Bearing on Effra |
| --- | --- | --- | --- |
| Tov `ae0f67f5`: `docs/spec.md` L23 (`int` is a "64-bit signed integer (overflow traps)"), L140 (overflow is a trap, not an error); README L185; `crates/tov/src/codegen/mod.rs` L1873-L1881 | Checked by default in release builds; `--unchecked` (`TOV_UNCHECKED`) wraps. | `TV_ADD`, `TV_SUB` and `TV_MUL` are `__builtin_add_overflow`, `__builtin_sub_overflow` and `__builtin_mul_overflow` under `__builtin_expect`, then `tv_trap`. A C compiler turns each into the operation plus one `jo` (the gcc listing above). | Tov's "affordable" claim rests on the flag-based check that a C backend reaches. Direct-Go output cannot reach it, so the claim does not transfer. |
| Goose `670fc91a`: `src/runtime/runtime.h` L440-L455, L563-L593, L594 (release macros); `docs/implementation.md` L4313-L4315 and L3966-L3968; translation guide L824 | Aborts in debug builds and wraps in release builds; unsigned always wraps. | The debug `gs_add_i64` and `gs_sub_i64` use the sign-xor test, and `gs_mul_i64` divides back: the xor and cmp families measured here. Its bounds-check pass proves `len - 1`, `i + 1` and `len + len` free of overflow from a length cap. Its optimiser regroups an associative chain only under an explicit licence (§6.2), because regrouping moves where a debug build detects an overflow. | Corroborates xor as the portable check for `+` and `-`. The range proof is the lowering path that removes a check where facts allow it. |
| Rust (documentation, no pinned checkout: [Reference, integer overflow](https://doc.rust-lang.org/reference/behavior-not-considered-unsafe.html#integer-overflow); [Cargo `overflow-checks`](https://doc.rust-lang.org/cargo/reference/profiles.html#overflow-checks); [`i64` methods](https://doc.rust-lang.org/std/primitive.i64.html)) | Panics when `overflow-checks` is on (the dev profile) and wraps in release. Explicit `checked_add` (returns `None` on overflow), `overflowing_add`, `wrapping_add`, `saturating_add`, `strict_add` (always panics; stable since 1.91.0) and `Wrapping<T>`. | LLVM overflow intrinsics (`llvm.sadd.with.overflow` and others), selected to the operation plus `jo` on x86-64. | The named-method family is the explicit spelling for a wrap-default language. The debug/release split makes behaviour depend on the build, which one Effra source meaning rules out. |
| Swift (documentation: [Advanced Operators, overflow operators](https://docs.swift.org/swift-book/documentation/the-swift-programming-language/advancedoperators/#Overflow-Operators)) | Traps by default in optimised builds; `&+`, `&-` and `&*` wrap; `addingReportingOverflow` returns the value and a flag; `-Ounchecked` removes the checks. | LLVM overflow intrinsics: the operation, `jo` and a trap. | Checked by default is affordable where the backend has the flag. The `&` operators are the wrapping spelling of a checked-default language. |
| Zig (documentation: [language reference, integer overflow](https://ziglang.org/documentation/master/#Integer-Overflow)) | `+`, `-` and `*` are safety-checked illegal behaviour: a panic in Debug and ReleaseSafe, undefined behaviour in ReleaseFast and ReleaseSmall. `+%` wraps, `+\|` saturates, `@addWithOverflow` returns a tuple. | LLVM overflow intrinsics in the safe modes. | The wrapping and saturating operators are the explicit forms. Undefined behaviour in a fast build is not an Effra outcome. |
| Go ([specification, integer overflow](https://go.dev/ref/spec#Integer_overflow); proposals [#19624](https://github.com/golang/go/issues/19624), [#30209](https://github.com/golang/go/issues/30209), [#30613](https://github.com/golang/go/issues/30613); [go-panikint](https://github.com/trailofbits/go-panikint)) | Wraps silently. #19624 (panic on overflow, which estimated a cost "similar to bounds-checking") was withdrawn in favour of #30209 (checked and wrapping integer types); neither is in the language. | No overflow-flag operation for user code (listings above). go-panikint, a modified compiler, inserts sign tests during SSA construction and calls `runtime.panicoverflow`: the xor family again. | The target fixes the cost: each checked operation in emitted Go is several instructions, not one. |
| Elixir and Erlang/OTP (see the comparison above) | Arbitrary-precision integers; no overflow. | Small integers are tagged words; the VM promotes to a bignum. | Removes the question at the price of a variable-size integer, which is not Go `int64`. |
| MoonBit core `e96ede8` (`builtin/int64.mbt` L200-L269) | `Int64` `+`, `-` and `*` wrap. | Plain two's-complement operations. | Corroborates wrap as the default of a typed language with a native backend. |
| Gleam, ReScript | Gleam on Erlang uses bignums; ReScript `int` is a 32-bit integer that wraps. | Host operations. | Neither checks by default. |

Dan Luu's [integer overflow measurement](https://danluu.com/integer-overflow/) (clang and gcc) is consistent: even with `jo`, a checked summing loop ran 4 to 6 times slower because the checks stopped vectorisation, while bzip2 slowed by 28% for compression and 9% for decompression. Go does not vectorise these loops, so that loss does not apply here. The loss measured here is scalar instruction count.

### Decision input for #81

Scored against the north-star pillars:

- **Performance (pillar 7)** decides it. Every construct must lower to code no slower than hand-written Go, and hand-written Go uses wrapping `+`. A checked default makes every `+`, `-` and `*` slower than the Go a programmer would write: by 2.1 to 2.3 times on the accounting kernel with the best lowering, and by 1.24 to 1.75 times for a lone `+` or `-`. The cost cannot fall to Tov's level while Effra emits Go, because Go exposes no overflow flag.
- **Laws and proofs (pillar 4)** favour wrap as the default. Wrapping `+`, `-` and `*` form the ring of integers modulo 2^64: associative, commutative and distributive, so a law suite (#125) can state them and a compiler can regroup. Checked arithmetic is partial: `(MaxInt64 + 1) - 1` fails while `MaxInt64 + (1 - 1)` succeeds. Goose needs an explicit licence in its specification to regroup a checked chain.
- **Error tracking and Effect guarantees (pillars 2 and 5)** favour a checked form that returns a typed failure in the row, not a defect. It costs 0.3 to 0.45 times wrap more than the defect form (fail-range 2.59x against defect-range 2.14x contiguous; 2.67x against 2.23x paired). The divide-by-zero decision above already rejected untyped panics from arithmetic.
- **Go simplicity and agent interpretability (pillars 3 and 6)**: wrap is Go's own semantics, and a checked call is visible at the operation where a reader or agent looks for it.

Recommendation for #81:

1. **Keep wrap as the default** for `+`, `-` and `*` on `int64` and on the new Go integer widths. This evidence does not reopen the signed-64 wrap profile under the "Reconsidering a prior" tiebreak; it confirms it for the Go target.
2. **Spell checked arithmetic as named functions, Rust style, not operators.** Swift's `&+` and Zig's `+%` are the wrapping spellings of checked-default languages. In a wrap-default language the explicit form is the checked one, and `checked_add`, `strict_add` and the Go proposal #30209 are the precedents. The functions are ordinary library with honest rows, for example a checked add returning `(int64, error[Overflow])`. They need no compiler knowledge, so the "primitive function plus library" tiebreak applies and no new syntax is needed. A defect form adds nothing that the typed failure lacks.
3. **Lower each checked operation with the measured winners**: the sign-xor test for `+` and `-`; for `*`, a 32-bit range fast path in front of `bits.Mul64`. Do not use the CERT pre-check or divide-back. Emit the check at the operation, or keep each helper inside Go's inlining budget: the range helper fell out of the budget and became a call per product.
4. **Use proofs to remove checks**, the same route the divisor proof takes for `/`. When the compiler knows the operand ranges (a narrower width from #81 widened to `int64`, a constant, a Goose-style length cap), the checked operation lowers to the bare operator. That is where Effra can beat hand-written Go, which writes the check or omits it. A constant multiplier already costs only one compare.
5. **Revisit** only if Go gains an overflow-flag intrinsic, or if an application-level measurement shows the share of arithmetic in real Effra programs is small enough that a checked default is affordable there. This spike measured neither.

This record replaces, for the Go target, the provisional numbers on the unmerged 2026-10-11 branch `wf10/spike-ovf` (which measured 2.5 to 3.1 times wrap for its Go defect variants and also covered the removed JavaScript target).
