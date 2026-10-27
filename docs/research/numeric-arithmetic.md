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

Status: measurement record and decision input for [#81](https://github.com/cevr/effra/issues/81). It changes no compiler behaviour. The benchmark-last rule (#48, #62) applies: these numbers are decision evidence from one loaded host, not a performance claim.

### Question and hypothesis

#108 asks what an overflow-checked `+`, `-` and `*` costs against wrap in direct-Go output, and whether "checked by default" is affordable, as Tov claims for its release builds. The hypothesis was that a compare-based or `math/bits` check costs at most 10% on a tight arithmetic kernel.

**Measured: the hypothesis is refuted for scalar `int64` kernels on amd64.** The cheapest checked accounting sum runs at 2.2 to 2.5 times the wrap time, whether it panics or returns a typed failure. The cheapest check for one operator costs about 1.5 to 1.75 times wrap for `+`, 1.2 to 1.3 times for `-` and 2.0 to 2.4 times for `*` (medians across two runs). Three things were not measured: application code where arithmetic is a small share of the time, the other Go integer widths that #81 adds, and other architectures.

Wrap and checked arithmetic are **different policies**, not one contract at two costs. On `[]Item{{Qty: 1, Price: MaxInt64}, {Qty: 1, Price: 1}}` wrap returns `MinInt64` and every checked variant refuses the input (`TestAccountingSumOverflowControl`). The ratios below are therefore the price of the checked policy against the wrap policy. They are not abstraction overhead. The NORTH_STAR performance rule compares a construct with "semantically equivalent explicit Go" that keeps the same failure rows. For a checked Effra operator, that baseline is the hand-written checked Go loop, and `SumFailDirect` below is that loop.

### Method

The package [`overflow-bench`](overflow-bench/) holds hand-emitted Go. Each kernel has three policy variants. **wrap** uses the plain `int64` operators, the signed-64 wrap profile of the sketch ([types](../spec/types.md#basic-types), rule 2). **defect** checks and then panics, as the prototype emitter lowers a failure in a frame that cannot fail. **fail** checks and then returns a typed failure: a function with `error[Overflow]` lowers to a Go `(T, error)` result, and the error type is a value struct with a value-receiver `Error` method ([rows](../spec/rows.md); `internal/sketch/proto/emit.go`, `signature` and `fail`). The fail variants come in two shapes. The **helper** shape calls a small `(int64, error)` function per operation and follows it with `if err != nil { return 0, err }`, the emitter's shape for a fallible call. The **direct** shape (`SumFailDirect`) branches from each overflow predicate straight to `return 0, Overflow{}`, which is what a Go programmer writes by hand.

Each checked variant comes in check families that differ only in how the test is spelled:

| Family | `+` and `-` | `*` |
| --- | --- | --- |
| cmp | compare before the operation (CERT INT32-C): `b > 0 && a > MaxInt64-b \|\| b < 0 && a < MinInt64-b` | multiply, then divide back (`p/a != b`, plus the `-1 * MinInt64` case) |
| post | compare the wrapped result with an operand: `(s < a) != (b < 0)` | `bits.Mul64` |
| xor | test the sign bits of the wrapped result: `(s^a)&(s^b) < 0` | `bits.Mul64` |
| bits | `bits.Add64`/`bits.Sub64`; signed overflow is the carry out of bit 63 xor the carry into it | `bits.Mul64`; the signed high word must be the sign extension of the low word |
| range | as xor | plain `IMULQ` when both operands fit in 32 bits (one biased unsigned compare, `fits32`), else `bits.Mul64` |
| range-shift | as xor | as range, with the predicate spelled `(…)>>32 == 0` (`fits32Shift`) |
| range-outline | as xor | as range-shift, with the `bits.Mul64` slow path in a `//go:noinline` function |

The direct failure uses the range family's checks. range-shift and range-outline are the spellings that the round 1 review measured.

The kernels are:

1. **Accounting sum (the cost measurement).** `total += qty*price - discount` over 1,024 lines, one `*`, one `-` and one `+` per line. Quantity is at most 1,000, price at most 1e9 and the discount at most a tenth of the line, so no input overflows. `TestAccountingSumAgrees` requires every variant to return the `math/big` total with no failure, on the benchmark input and three other sizes, before the runner records any timing. `TestAccountingSumOverflowControl` gives one input that overflows at `*`, one at `-` and one at `+`; every checked variant must refuse each one and wrap must return the wrapped total, so no check is dead. `TestRangeSpellings` runs every accounting variant on single products over all pairs of the boundary operands, so the inline fast and slow paths meet `math/big` on both sides of the 32-bit boundary.
2. **Hash mix that relies on wrap (a policy comparison).** `h = h*31 + b` over a key, the polynomial string hash. It is excluded from the agreement check. For **lowercase ASCII keys**, `h*31` first leaves the `int64` range at byte index 12: a lowercase ASCII key of at most 12 bytes never overflows, and a longer one always does (`TestHashMixPolicy` checks every prefix up to 40 bytes against `math/big`, and the all-`a` and all-`z` keys as bounds). The threshold belongs to that input domain: 13 zero bytes never overflow, and the binary key `{11, 22, 0, 3, 18, 9, 5, 17, 18, 10, 4, 3, 8}` first overflows at the `+` (`TestHashMixOutsideLowercase`). The timed input is 256 lowercase ASCII keys of 12 bytes, where all variants agree, plus one 4,096-byte lowercase ASCII key for the wrap variants only.
3. **Operator kernels (attribution).** One checked operator per loop, defect style, over 1,024 values in [-1e9, 1e9], to attribute the accounting cost to an operator and a family.

`TestChecksAgreeWithBig` compares every check function with `math/big` on all pairs of 64 operands: the boundaries (`MinInt64`, `MaxInt64`, `±3037000500`, `±2^32` and others) and pseudo-random values of every magnitude. Hand mutants each fail a test: a flipped sign test, a dropped high-word correction, a dropped `-1 * MinInt64` case, a one-sided post test, a removed `+` check in `SumDefectXor`, a removed `-` check in `SumFailBits`, the removed `-` and `+` checks in `SumFailDirect`, a wrong shift in `fits32Shift`, and the removed `+` check in `HashFailBits` (killed by the binary key).

The runner `go run ./docs/research/overflow-bench/run` builds the test binary once, runs the tests (a failure stops the run), then takes two series. The contiguous series is `go test -bench . -benchmem -count 10 -benchtime 1s`. The interleaved series runs one process per sample, each round in a fresh shuffled order, 10 rounds, and divides each sample by the wrap sample of the same round (the paired ratio). The interleaved series is the canonical comparison, because the contiguous series runs each variant's samples back to back and load drifts between variants. The runner samples `/proc/loadavg` every 10 seconds and before each interleaved sample, and writes the `go build -gcflags=-S` and `-gcflags=-m` output. Every kernel is `//go:noinline`, so each has its own loop in the listing and pays the same call.

Two runs are on record. The canonical run is [`results/20261027T154229Z`](overflow-bench/results/20261027T154229Z/summary.md) at head `e73a0d5f`; all tables below are from it. The first run, [`results/20261027T150404Z`](overflow-bench/results/20261027T150404Z/summary.md) at head `eb2a3761`, predates the direct, range-shift and range-outline variants. Its ratios agree with the canonical run within the ranges (defect-range 2.14x contiguous and paired 2.23x [1.48, 2.68]; `+` xor 1.66x, `-` xor 1.24x, `*` range 2.42x paired).

### Host

AMD EPYC 9554P (16 logical CPUs, a shared workbox), Linux 6.12.93 x86_64, `go version go1.27.0 linux/amd64`, `GOAMD64=v1`. Both receipts record `package clean: false`. In each case the untracked entry was the run's own `results/` directory, which the runner creates before it checks the status; the first run also had the untracked C listing [`c/accounting.c`](overflow-bench/c/accounting.c), which the Go build ignores. The measured Go code is the committed head. The runner now excludes `results/` from that check.

The one-minute load average was 10.68 at the start of the canonical run. During the contiguous series it had a median of 12.53 (range 10.00 to 16.63, 44 samples); during the interleaved series it had a median of 10.81 (range 5.62 to 12.65, 45 samples), and the reading before each interleaved sample ranged from 5.58 to 12.97. Other sessions shared the host, so the ranges are wide and only the ratios carry the decision.

### Results

All variants allocate nothing: 0 B/op and 0 allocs/op in every row of both series.

Accounting sum, ns/op for 1,024 lines:

| Variant | Contiguous median [min, max] | vs wrap | Interleaved median [min, max] | Paired ratio median [min, max] |
| --- | ---: | ---: | ---: | ---: |
| wrap | 843 [621, 956] | 1.00x | 782 [717, 897] | 1.00x |
| defect-cmp | 4432 [3418, 5518] | 5.26x | 4060 [3087, 5069] | 5.24x [3.56, 6.85] |
| defect-post | 2690 [2387, 3199] | 3.19x | 2556 [2244, 2854] | 3.26x [2.62, 3.92] |
| defect-xor | 2341 [2090, 2612] | 2.78x | 2336 [1965, 2662] | 2.95x [2.61, 3.71] |
| defect-bits | 3302 [2952, 3504] | 3.92x | 3323 [2724, 3521] | 4.06x [3.62, 4.77] |
| **defect-range** | 1880 [1781, 2141] | **2.23x** | 1884 [1565, 2050] | **2.21x [2.08, 2.86]** |
| defect-range-shift | 2144 [1921, 2669] | 2.54x | 1910 [1632, 2109] | 2.48x [1.90, 2.79] |
| defect-range-outline | 2197 [2001, 2288] | 2.61x | 2172 [1727, 2388] | 2.59x [2.30, 3.23] |
| fail-cmp (helper) | 4763 [4361, 5215] | 5.65x | 4165 [3862, 4727] | 5.29x [4.53, 5.77] |
| fail-post (helper) | 3254 [2686, 3576] | 3.86x | 2879 [2612, 3086] | 3.47x [3.34, 4.12] |
| fail-xor (helper) | 3070 [2763, 3519] | 3.64x | 2610 [2360, 2786] | 3.19x [2.71, 3.77] |
| fail-bits (helper) | 3955 [3787, 4320] | 4.69x | 3610 [3063, 4332] | 4.50x [4.02, 6.04] |
| fail-range (helper) | 2866 [2461, 3681] | 3.40x | 2380 [1983, 2490] | 2.81x [2.62, 3.47] |
| **fail-direct** | 2094 [1941, 2355] | **2.48x** | 1835 [1564, 2007] | **2.30x [2.04, 2.54]** |

Direct comparisons between candidates, from the same rounds of the interleaved series (`samples.tsv`):

| Pair | Per-round ratio median [min, max] | First faster |
| --- | ---: | ---: |
| fail-direct / fail-range (helper) | 0.785 [0.692, 0.946] | 10 of 10 rounds |
| fail-direct / defect-range | 1.004 [0.852, 1.062] | 5 of 10 rounds |
| fail-range (helper) / defect-range | 1.232 [1.056, 1.373] | 0 of 10 rounds |
| defect-range-shift / defect-range | 1.000 [0.878, 1.210] | 5 of 10 rounds |
| defect-range-outline / defect-range | 1.143 [0.913, 1.272] | 1 of 10 rounds |

The typed failure costs nothing measurable over the panic when it is lowered directly: fail-direct and defect-range tie (1.004, each faster in half the rounds). The contiguous series shows fail-direct 11% slower than defect-range (2094 against 1880 ns/op), within the drift that the contiguous series cannot separate from the variant. The helper shape costs about a quarter more (fail-range against defect-range, 1.232; slower in every round). The premium that the first draft of this record attributed to typed failure belongs to the helper lowering, not to the failure. range-shift ties with range, and range-outline loses.

Operator kernels, ns/op for 1,024 operations:

| Operator | wrap | cmp | post | xor | bits | range |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `+` contiguous | 452 | 2034 (4.50x) | 913 (2.02x) | **692 (1.53x)** | 1084 (2.40x) | - |
| `+` interleaved, paired | 429 | 4.28x [4.11, 5.36] | 1.90x [1.66, 2.21] | **1.59x [1.39, 1.81]** | 2.58x [2.31, 2.64] | - |
| `-` contiguous | 448 | 1847 (4.13x) | 745 (1.66x) | **539 (1.20x)** | 1082 (2.42x) | - |
| `-` interleaved, paired | 451 | 4.12x [3.06, 5.13] | 1.82x [1.40, 2.54] | **1.22x [0.82, 1.52]** | 2.47x [1.68, 2.68] | - |
| `*` contiguous | 521 | 2659 (5.11x) | - | - | 1404 (2.70x) | **1216 (2.34x)** |
| `*` interleaved, paired | 504 | 4.93x [4.43, 5.83] | - | - | 2.89x [2.57, 3.05] | **2.25x [1.97, 2.74]** |

Hash mix, ns/op (lowercase ASCII keys):

| Input | wrap | explicit-wrap | defect-cmp | defect-bits | fail-cmp | fail-bits |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 256 keys of 12 bytes, contiguous | 3016 | 3160 (1.05x) | 5332 (1.77x) | 6111 (2.03x) | 7131 (2.36x) | 5762 (1.91x) |
| 256 keys of 12 bytes, interleaved, paired | 2548 | 1.01x [0.87, 1.11] | 1.77x [1.69, 2.22] | 2.19x [2.08, 2.42] | 2.56x [2.35, 3.68] | 2.29x [1.97, 2.79] |
| one key of 4,096 bytes, contiguous | 3628 | 3516 (0.97x) | panics | panics | `Overflow` | `Overflow` |
| one key of 4,096 bytes, interleaved, paired | 3660 | 0.98x [0.95, 1.05] | - | - | - | - |

Raw ns/op samples of the canonical run's contiguous series, in run order (the interleaved samples are in the [summary](overflow-bench/results/20261027T154229Z/summary.md) and `samples.tsv`, with the load before each sample):

```
AccountingSum/wrap: 666 621 761 842 826 843 862 871 956 929
AccountingSum/defect-cmp: 4371 5518 4325 4690 4444 4391 3418 5223 5036 4421
AccountingSum/defect-post: 2734 2738 2657 2800 2387 2641 3199 2723 2567 2612
AccountingSum/defect-xor: 2464 2090 2360 2315 2429 2535 2274 2322 2612 2213
AccountingSum/defect-bits: 3234 2995 2952 3208 3434 3398 3227 3388 3504 3369
AccountingSum/defect-range: 1781 1821 1919 1988 1994 1851 2141 1809 1908 1830
AccountingSum/defect-range-shift: 1921 2127 1972 2491 1947 2208 2232 2669 2046 2160
AccountingSum/defect-range-outline: 2288 2230 2205 2079 2138 2230 2126 2189 2001 2205
AccountingSum/fail-cmp: 5005 4739 4779 5215 4361 4727 4583 5148 4813 4747
AccountingSum/fail-post: 3576 3062 3172 3346 3196 3313 3480 3346 2977 2686
AccountingSum/fail-xor: 3216 2763 2956 3519 3433 3178 2973 2881 3101 3039
AccountingSum/fail-bits: 4108 3840 3926 3787 4115 3790 3984 4320 4011 3849
AccountingSum/fail-range: 3681 2757 2589 2694 2975 3386 3073 2640 2461 3559
AccountingSum/fail-direct: 2355 2268 2123 1959 2177 2064 2211 1941 2018 1968
HashMix/short/wrap: 3109 2824 2980 3201 2653 3122 3019 3223 3012 2973
HashMix/short/explicit-wrap: 3198 2925 3216 3168 3151 3114 2900 3265 3190 2711
HashMix/short/defect-cmp: 5205 5719 5588 5464 5481 4726 5459 4716 4926 4342
HashMix/short/defect-bits: 6203 6078 6399 6496 6239 5978 6043 6144 5643 5861
HashMix/short/fail-cmp: 6712 7547 7145 6655 7117 7757 8934 6804 8256 6364
HashMix/short/fail-bits: 6442 5425 5975 5772 5707 6303 6329 5751 5190 5605
HashMix/long/wrap: 3618 3494 3639 3726 3918 3574 3594 3963 3648 3493
HashMix/long/explicit-wrap: 3793 3503 3339 3494 3650 3721 3452 3529 3493 3740
Op/add/wrap: 439 408 469 460 464 417 489 443 444 480
Op/add/cmp: 1897 2346 2352 1946 1981 1861 2513 2010 2057 2346
Op/add/post: 963 847 945 1000 986 912 890 782 915 835
Op/add/xor: 660 680 732 716 690 712 670 694 644 702
Op/add/bits: 1136 1063 1037 1118 1067 1102 1111 1063 1062 1109
Op/sub/wrap: 460 432 480 461 436 472 389 436 443 452
Op/sub/cmp: 1870 1687 1852 1574 1613 1943 1794 1968 1842 1946
Op/sub/post: 903 735 745 755 835 743 783 690 745 669
Op/sub/xor: 491 517 554 540 543 568 526 538 520 547
Op/sub/bits: 1064 1100 1152 1099 1077 1148 1087 1026 1077 1018
Op/mul/wrap: 516 533 540 507 508 540 532 525 484 505
Op/mul/cmp: 2707 2785 2638 2586 2457 2799 2680 2514 2560 2730
Op/mul/bits: 1538 1445 1392 1418 1238 1436 1305 1312 1361 1416
Op/mul/range: 1293 1119 1255 1308 1266 1215 1193 1186 1217 1114
```

Losing variants stay in the record. **cmp** loses everywhere: its pre-check compiles to two data-dependent branches and two 64-bit immediates per `+` (4.1 to 4.5 times wrap), and its `*` pays an `IDIVQ` on every product. **bits** loses for `+` and `-`: `bits.Add64` gives the unsigned carry through `SETCS`, and the signed overflow needs six more instructions (`MOVBLZX`, three `XORQ`, `SHRQ`, `BTL`). **post** is close to xor for `+` but loses for `-`. The **helper** shape of the typed failure loses to the direct shape in every round. **range-outline** loses to range (per-round ratio 1.143). Its cause is unmeasured: every one of the 1,024 benchmark lines passes the 32-bit predicate, so the outlined slow path runs 0 times, and outlining also changes the frame, the register allocation and the code layout of the loop. This spike does not isolate which of those changes costs the difference. A smoke run from the first draft (not part of a receipt, load about 2 to 5) of a range helper that called `mulBits` inline measured 2,722 and 2,969 ns/op against 1,329 and 1,427 for bits. Wrapped in a defect helper, that check exceeds Go's inlining budget and every product becomes a `CALL`. The kept range kernels therefore emit the fast path inline, as an emitter writes code at the operation.

### What the Go compiler emits

From [`results/20261027T154229Z/asm.txt`](overflow-bench/results/20261027T154229Z/asm.txt) (`go build -gcflags=-S`; offsets and `kernels.go` lines kept, `XCHGL`/`NOP` inline marks dropped). The wrap loop is 9 instructions per line:

```
(kernels.go:213)  IMULQ  R8, DI
(kernels.go:213)  SUBQ   16(AX)(SI*8), DI
(kernels.go:213)  ADDQ   DI, CX
```

The direct failure is the fastest typed-failure lowering and the semantically equivalent hand-written Go. Its function has no frame (`NOSPLIT|NOFRAME`), its fast path is 30 instructions per line, and it forms the `(itab, data)` error pair only at each exit:

```
00039 (kernels.go:149)  MOVL   $2147483648, R10    // fits32: one biased unsigned compare
00045 (kernels.go:149)  LEAQ   (R10)(R8*1), R11
00049 (kernels.go:149)  LEAQ   (R10)(R9*1), R12
00053 (kernels.go:149)  ORQ    R12, R11
00056 (kernels.go:149)  MOVQ   $4294967296, R12
00066 (kernels.go:149)  CMPQ   R12, R11
00069 (kernels.go:409)  JLS    83                  // -> MULQ slow path
00071 (kernels.go:410)  IMULQ  R9, R8
00075 (kernels.go:418)  MOVQ   AX, CX
00078 (kernels.go:422)  MOVQ   DX, R11
00081 (kernels.go:410)  JMP    133
...
00133 (kernels.go:418)  MOVQ   16(CX)(DI*8), DX
00138 (kernels.go:418)  MOVQ   R8, DI
00141 (kernels.go:418)  SUBQ   DX, R8              // net = line - discount
00144 (kernels.go:419)  XORQ   DI, DX
00147 (kernels.go:419)  XORQ   R8, DI
00150 (kernels.go:419)  TESTQ  DI, DX
00153 (kernels.go:419)  JLT    191                 // -> return 0, Overflow{}
00155 (kernels.go:422)  LEAQ   (R11)(R8*1), DX     // next = total + net
00159 (kernels.go:423)  XORQ   DX, R11
00162 (kernels.go:423)  XORQ   DX, R8
00165 (kernels.go:423)  TESTQ  R8, R11
00168 (kernels.go:423)  JGE    11                  // loop
00174 (kernels.go:424)  XORL   AX, AX              // exit: return 0, Overflow{}
00176 (kernels.go:424)  LEAQ   go:itab.ob.Overflow,error(SB), BX
00183 (kernels.go:424)  LEAQ   runtime.zerobase(SB), CX
00190 (kernels.go:424)  RET
```

The helper failure materialises the error pair after every operation and then tests it, rather than branching straight to the return. That is the quarter it loses:

```
00131 (kernels.go:856)  JEQ    152
00133 (kernels.go:856)  LEAQ   go:itab.ob.Overflow,error(SB), AX
00140 (kernels.go:856)  XORL   R8, R8
00143 (kernels.go:856)  LEAQ   runtime.zerobase(SB), DX
00150 (kernels.go:380)  JMP    160
00152 (kernels.go:380)  XORL   AX, AX              // no error
00154 (kernels.go:380)  XORL   DX, DX
00160 (kernels.go:381)  TESTQ  AX, AX
00163 (kernels.go:381)  JNE    283
```

The defect-xor loop is 36 instructions per line, 9 of them register-to-register moves. `bits.Mul64` becomes `MULQ`, which takes its operand in `AX` and writes `DX:AX`, so the slice base and the index are moved out of `AX` and `DX` and back on every iteration. The signed high-word correction adds six instructions:

```
00060 (kernels.go:245)  MOVQ   AX, R9              // save slice base: MULQ needs AX
00063 (kernels.go:124)  MOVQ   DI, AX
00066 (kernels.go:245)  MOVQ   DX, R10             // save index: MULQ writes DX
00069 (kernels.go:124)  MULQ   R8
00075 (kernels.go:125)  SARQ   $63, DI             // signed high word = hi - (a>>63)&b - (b>>63)&a
00079 (kernels.go:125)  ANDQ   R8, DI
00082 (kernels.go:125)  SUBQ   DI, DX
00098 (kernels.go:126)  SARQ   $63, AX
00103 (kernels.go:126)  CMPQ   AX, DX
00106 (kernels.go:774)  JNE    199                 // -> runtime.gopanic
00116 (kernels.go:104)  SUBQ   DX, DI
00119 (kernels.go:105)  XORQ   SI, DX
00122 (kernels.go:105)  XORQ   DI, SI
00128 (kernels.go:105)  TESTQ  SI, DX
00131 (kernels.go:750)  JLT    180                 // -> runtime.gopanic
```

`bits.Add64` keeps the carry in a flag only long enough to copy it out (`ADDQ SI, CX; SETCS R8B; MOVBLZX R8B, R8`). A constant multiplier is the one case Go handles well: `h > MaxInt64/31 || h < MinInt64/31` folds into a single biased compare (`LEAQ (CX)(SI*1), DI; CMPQ R9, DI; JCS`), and the prove pass drops the negative half of the `+` check because the byte operand is known non-negative.

Go 1.27 offers user code no overflow-flag operation. The listing contains no `JOS`, `JOC`, `SETOS` or `SETOC` (`grep -c` gives 0), and a `GOAMD64=v3` build emits neither those nor `MULX` (also 0). For comparison, gcc 13.3 at `-O2` compiles the same kernel with `__builtin_*_overflow`, the builtins Tov emits, to 13 instructions per line with one `jo` per operation ([`c/accounting.c`](overflow-bench/c/accounting.c), compiled for its listing only, not timed):

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

A C or LLVM backend reads the overflow flag that `IMUL`, `SUB` and `ADD` already set. Direct-Go output must recompute the condition with ordinary instructions, and for `*` it must route the slow path through `MULQ` and its fixed registers. This is an instruction-count comparison. The C loop was not timed, so this record makes no matched claim about Tov's cost.

### Policy comparison: code that relies on wrap

Under wrap, the hash returns the same value as an independent `uint64` computation for every key. Under either checked policy, every lowercase ASCII key of 13 bytes or more fails at byte index 12: the defect variants panic with the overflow defect and the failure variants return `Overflow`. Lowercase ASCII keys of 12 bytes or fewer pass. A test suite with short lowercase keys would pass, and production input with longer keys would fail. Other byte values move the threshold (see the method). Under a checked default, such code must be rewritten with explicit wrapping operations. `HashExplicitWrap` uses named `wrapAdd` and `wrapMul` functions, which inline to the plain operators, and costs the same as wrap (1.05x and 0.97x contiguous, paired 1.01x and 0.98x). The rewrite is free at run time. Its cost is the review of every hash, checksum, PRNG and fixed-point routine.

### Prior art: how other languages lower the check

| Source | Default policy and spelling | Lowering observed or documented | Bearing on Effra |
| --- | --- | --- | --- |
| Tov `ae0f67f5`: `docs/spec.md` L23 (`int` is a "64-bit signed integer (overflow traps)"), L140 (overflow is a trap, not an error); README L185; `crates/tov/src/codegen/mod.rs` L1873-L1881 | Checked by default in release builds; `--unchecked` (`TOV_UNCHECKED`) wraps. | `TV_ADD`, `TV_SUB` and `TV_MUL` are `__builtin_add_overflow`, `__builtin_sub_overflow` and `__builtin_mul_overflow` under `__builtin_expect`, then `tv_trap`. A C compiler turns each into the operation plus one `jo` (the gcc listing above). | Inference, not a matched measurement: Tov's "affordable" claim rests on the flag-based check that a C backend reaches. Direct-Go output cannot reach it, so the claim does not transfer as is. |
| Goose `670fc91a`: `src/runtime/runtime.h` L440-L455, L563-L593, L594 (release macros); `docs/implementation.md` L4313-L4315 and L3966-L3968; translation guide L824 | Aborts in debug builds and wraps in release builds; unsigned always wraps. | The debug `gs_add_i64` and `gs_sub_i64` use the sign-xor test, and `gs_mul_i64` divides back: the xor and cmp families measured here. Its bounds-check pass proves `len - 1`, `i + 1` and `len + len` free of overflow from a 2^48 length cap and storage ranges. Its optimiser regroups an associative chain only under an explicit licence (§6.2), because regrouping moves where a debug build detects an overflow. | Corroborates xor as the portable check for `+` and `-`. The range proof is the lowering path that removes a check where facts allow it. |
| Rust (documentation, no pinned checkout: [Reference, integer overflow](https://doc.rust-lang.org/reference/behavior-not-considered-unsafe.html#integer-overflow); [Cargo `overflow-checks`](https://doc.rust-lang.org/cargo/reference/profiles.html#overflow-checks); [`i64` methods](https://doc.rust-lang.org/std/primitive.i64.html)) | Panics when `overflow-checks` is on (the dev profile) and wraps in release. Explicit `checked_add` (returns `None` on overflow), `overflowing_add`, `wrapping_add`, `saturating_add`, `strict_add` (always panics; stable since 1.91.0) and `Wrapping<T>`. | LLVM overflow intrinsics (`llvm.sadd.with.overflow` and others), selected to the operation plus `jo` on x86-64. | The named-method family is the explicit spelling for a wrap-default language. The debug/release split makes behaviour depend on the build, which one Effra source meaning rules out. |
| Swift (documentation: [Advanced Operators, overflow operators](https://docs.swift.org/swift-book/documentation/the-swift-programming-language/advancedoperators/#Overflow-Operators)) | Traps by default in optimised builds; `&+`, `&-` and `&*` wrap; `addingReportingOverflow` returns the value and a flag; `-Ounchecked` removes the checks. | LLVM overflow intrinsics: the operation, `jo` and a trap. | Checked by default is cheaper where the backend has the flag. The `&` operators are the wrapping spelling of a checked-default language. |
| Zig (documentation: [language reference, integer overflow](https://ziglang.org/documentation/master/#Integer-Overflow); [0.15.1 release notes, x86 backend](https://ziglang.org/download/0.15.1/release-notes.html#x86-backend)) | `+`, `-` and `*` are safety-checked illegal behaviour: a panic in Debug and ReleaseSafe, undefined behaviour in ReleaseFast and ReleaseSmall. `+%` wraps, `+\|` saturates, `@addWithOverflow` returns a tuple. | Backend-dependent. Since 0.15 most x86_64 Debug builds use Zig's self-hosted x86_64 backend (the LLVM backend stays the default on NetBSD, OpenBSD and Windows, or with `-fllvm`); release modes use LLVM. This record did not inspect the self-hosted backend's check code. | The semantics (checked by default, explicit wrapping and saturating operators) are separate from the backend. Undefined behaviour in a fast build is not an Effra outcome. |
| Go ([specification, integer overflow](https://go.dev/ref/spec#Integer_overflow); proposals [#19624](https://github.com/golang/go/issues/19624), [#30209](https://github.com/golang/go/issues/30209), [#30613](https://github.com/golang/go/issues/30613); [go-panikint](https://github.com/trailofbits/go-panikint)) | Wraps silently. #19624 (panic on overflow, which estimated a cost "similar to bounds-checking") was withdrawn in favour of #30209 (checked and wrapping integer types); neither is in the language. | No overflow-flag operation for user code (listings above). go-panikint, a modified compiler, inserts sign tests during SSA construction and calls `runtime.panicoverflow`: the xor family again. | The target fixes the instruction cost: each checked operation in emitted Go is several instructions, not one. |
| Elixir and Erlang/OTP (Elixir `91ee75bb` `lib/elixir/src/elixir_rewrite.erl` L86-L91; OTP `516126e9` `erts/emulator/beam/jit/x86/instr_arith.cpp` L175-L260, as cited by the round 1 review) | Arbitrary-precision integers; no wrap. | Elixir inlines arithmetic to Erlang BIFs. The OTP JIT runs a tagged-small fast path, tests the overflow flag and falls back to a bignum; proven-small ranges remove the test. A bignum still raises at its large implementation limit. | Removes the wrap question at the price of promotion to a variable-size integer, which is not Go `int64`. The JIT shows the flag-based path again. |
| MoonBit core `e96ede8` (`builtin/int64.mbt` L199-L269) | `Int64` `+`, `-` and `*` wrap. | Two's-complement intrinsics (`%i64_add`, `%i64_sub`, `%i64_mul`). | A semantic precedent for wrap in a typed language with a native backend, not a cost measurement. |
| Gleam `52e735c8` (`compiler-core/src/erlang.rs` L2760-L2762; `compiler-core/src/javascript/expression.rs` L2093-L2097; [language tour, Ints](https://tour.gleam.run/basics/ints/)) | On Erlang an `Int` is an arbitrary-precision BEAM integer; on JavaScript it is a JS Number (a double), with no overflow check. | The Erlang backend emits the Erlang `+`, `-`, `*` operators; the JavaScript backend emits the JS `+`, `-`, `*` operators on Numbers. | One source language, two integer semantics by target. Effra has one target and one semantics. |
| ReScript `30ce3698` (`compiler/core/js_exp_make.ml` L1606-L1670) | `int` is a 32-bit integer: no check. | `+` and `-` compute a JS Number and narrow with `\|0`. `*` folds constants, turns a power-of-two constant into a shift, and otherwise multiplies two JS Numbers and narrows with `\|0`. That product is exact only below 2^53, so `(2147483647 * 2147483647) \| 0` gives 0, where exact 32-bit modular multiplication gives 1. | Narrowing after host arithmetic wraps only where the host product is exact. Effra's Go `int64` operators are exact modulo 2^64. |

Dan Luu's [integer overflow measurement](https://danluu.com/integer-overflow/) (clang and gcc) separates three effects. With diagnostic overflow checks, bzip2 slowed by 28% for compression and 9% for decompression. With the trap-only form (`-fsanitize-undefined-trap-on-error`), the same checks cost about 1% and 0%, so most of the diagnostic cost came from the diagnostic path disturbing optimisation, which the article says later clang and gcc releases improved. A checked summing loop ran about 6 times slower than an unchecked loop that the compiler vectorised. The article reports separately that permutations of the loop where the unchecked version cannot use SSE were still 4 to 6 times slower, so that slowdown is not explained by lost vectorisation. Go does not vectorise these loops, so the vectorised comparison does not apply here. The loss measured here is scalar code generation: the extra instructions and register moves shown above.

### Decision input for #81

**Measured** (scalar `int64`, amd64, go1.27.0, one loaded host, two runs):

- Checked arithmetic refutes the 10% hypothesis on these kernels: 2.2 to 2.5 times wrap for the accounting sum with the best lowering, about 1.5 to 1.75 times for `+`, 1.2 to 1.3 times for `-` and 2.0 to 2.4 times for `*`.
- A typed failure lowered directly costs the same as a panic (paired 1.004, range 0.852 to 1.062). The helper lowering costs about a quarter more.
- The measured winners are the sign-xor test for `+` and `-` and, for `*`, a 32-bit range fast path in front of `bits.Mul64`. The CERT pre-check and divide-back lose everywhere.
- Go emits no overflow-flag instruction for any check; gcc emits one `jo` per operation for the same kernel.
- Code that relies on wrap must change under a checked default, and the change (explicit wrapping functions) costs nothing at run time.

**Inferred, not measured:**

- Narrower widths (#81's `int32`, `int16`, `int8` and the unsigned widths) were not timed. Their checks could be cheaper: an unsigned add needs only the carry that `bits.Add64` already gives, and a narrow product widened to 64 bits can be checked with one range compare. Their cost should be measured before it is assumed.
- The share of arithmetic in real Effra programs is unknown, so the application-level cost of a checked default is unknown. The kernels here are the worst case.
- Other architectures (arm64 has flag-setting `ADDS` and `SUBS`) were not inspected.
- The guarantee itself is a product choice, not a measurement: a checked default turns silent wrong values into typed failures (pillars 2 and 5), and a wrap default keeps Go's semantics and the lawful ring Z/2^64 (pillars 3 and 4). Wrapping `+` and `*` are associative and commutative, and `*` distributes over `+`, so a law suite (#125) can state those laws and a compiler can regroup sums and products. Wrapping `-` is the addition of the additive inverse (`a - b == a + (-b)`, with `-MinInt64 == MinInt64`); it is neither commutative nor associative (`(5 - 3) - 1 == 1`, `5 - (3 - 1) == 3`) and gets no free regrouping. Checked arithmetic is partial (`(MaxInt64 + 1) - 1` fails while `MaxInt64 + (1 - 1)` succeeds), which is why Goose needs an explicit licence to regroup.

On the performance rule: NORTH_STAR measures a construct against semantically equivalent explicit Go with the same failure rows. Wrapping Go is a different policy, so it is not that baseline for a checked operator. The equivalent hand-written Go is the checked loop, and `SumFailDirect` shows that a direct lowering of a checked Effra operator can match it. What the measurements do show is the price of choosing the checked policy over the wrap policy on scalar `int64` kernels: about 2.2 to 2.5 times on the accounting sum.

**Recommendation** (from the measured tradeoff, not forced by the performance rule):

1. **Keep wrap as the default** for `+`, `-` and `*` on `int64`. On this target a checked default would cost the accounting kernel about two times (2.2 to 2.5 times wrap), and a lone checked operator 1.2 to 2.4 times depending on the operator (see the operator table); no bound for other kernels is measured. The guarantee it buys can be had where it is wanted through an explicit checked function. This evidence does not reopen the signed-64 wrap profile under the "Reconsidering a prior" tiebreak. Apply the same default to the new widths unless their own measurement shows the check is cheap enough to change the tradeoff.
2. **Spell checked arithmetic as named functions, Rust style, not operators.** Swift's `&+` and Zig's `+%` are the wrapping spellings of checked-default languages. In a wrap-default language the explicit form is the checked one, and `checked_add`, `strict_add` and Go proposal #30209 are the precedents. The functions are ordinary library with honest rows, for example a checked add returning `(int64, error[Overflow])`. They need no compiler knowledge, so the "primitive function plus library" tiebreak applies and no new syntax is needed. Prefer the typed failure: lowered directly it costs the same as a defect, and it stays in the row.
3. **Lower each checked operation like `SumFailDirect`**: the sign-xor test for `+` and `-`; for `*`, the 32-bit range fast path in front of `bits.Mul64`; each predicate branching straight to the failure return, with no per-operation `(T, error)` helper result. Emit the check at the operation, or keep each helper inside Go's inlining budget. Do not use the CERT pre-check or divide-back.
4. **Use proofs to remove checks**, the same route the divisor proof takes for `/`. When the compiler knows the operand ranges (a narrower width widened to `int64`, a constant, a Goose-style length cap), the checked operation lowers to the bare operator. A constant multiplier already costs only one compare.
5. **Revisit** if Go gains an overflow-flag intrinsic, if the narrow widths measure much cheaper, or if an application-level measurement shows the share of arithmetic in real Effra programs is small enough that a checked default is affordable there.

This record replaces, for the Go target, the provisional numbers on the unmerged 2026-10-11 branch `wf10/spike-ovf` (which measured 2.5 to 3.1 times wrap for its Go defect variants and also covered the removed JavaScript target).
