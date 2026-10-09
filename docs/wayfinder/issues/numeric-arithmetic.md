<!-- {"id": "numeric-arithmetic", "title": "General numeric arithmetic and cross-target integer policy", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "generic-data", "assignee": null, "blocked_by": []} -->
# General numeric arithmetic and cross-target integer policy

Implement the first finite numeric profile required by the application corpus. The current checker refuses `i64 +` in `internal/compiler/semantic.go` while allowing string concatenation; a counter-specific workaround, bounded enum state or update moved into TypeScript is not an arithmetic implementation.

## Acceptance

- Specify signed 64-bit values as Go `int64` and JavaScript `bigint`, with literal bounds checked before emission and per-operation `BigInt.asIntN` normalization where the selected policy requires it. Keep Go constant-only overflow behavior from silently becoming the cross-target contract.
- Decide and implement the supported operator/profile matrix for addition, subtraction, multiplication, division, remainder, comparison, equality, literals and conversions, including overflow, divide-by-zero and mixed-width diagnostics. Keep later floating-point and wider numeric profiles separate.
- Run two unrelated Effra callers on Go and JS and retain a strict TypeScript consumer that sees `bigint`, plus negative controls for string-only concatenation, out-of-range literals, mixed numeric types and unsupported operations. Preserve explicit rows and diagnostics.
- Bind the policy to source/toolchain identities and full-gate receipts. The durable [portable i64 arithmetic record](../../research/numeric-arithmetic.md) owns the first signed profile, Go/JS comparison, rejected alternatives, causal controls and two-caller gate. This ticket establishes a finite prerequisite; it does not claim the complete numeric language or framework counter adoption.

Use the [Go constants and numeric types](https://go.dev/ref/spec#Constants) and [ECMAScript BigInt](https://tc39.es/ecma262/multipage/ecmascript-data-types-and-values.html#sec-ecmascript-language-types-bigint-type) rules as source inputs. No implementation or current support follows from this specification alone.
