package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const signed64ProfileSource = `
fn counterAdvance(current: i64, delta: i64) -> i64 {
    current + delta
}
fn counterLiteral() -> i64 {
    9223372036854775807 + 1
}
fn counterNested(value: i64) -> i64 {
    (value + 1) + 1
}
fn negate(value: i64) -> i64 {
    -value
}
fn negateLeading() -> i64 {
    -0009223372036854775808
}
fn serverAvailable(quantity: i64, reserved: i64) -> bool {
    quantity - reserved > 0
}
fn serverBelowZero(quantity: i64) -> bool {
    quantity < 0
}
fn serverLiteral() -> bool {
    -9223372036854775808 - 1 == 9223372036854775807
}
effect fn main() -> string {
    if counterAdvance(9223372036854775807, 1) == -9223372036854775808 {
        if counterLiteral() == -9223372036854775808 {
            if counterNested(9223372036854775807) == -9223372036854775807 {
                if negate(-9223372036854775808) == -9223372036854775808 {
                    if negateLeading() == -9223372036854775808 {
                        if serverAvailable(-2, -3) {
                            if serverBelowZero(-1) {
                                if serverLiteral() { "ok" } else { "server literal" }
                            } else { "server less-than" }
                        } else { "server branch" }
                    } else { "leading minimum" }
                } else { "unary" }
            } else { "nested" }
        } else { "literal" }
    } else { "counter" }
}
`

func TestSigned64ProfileRunsTwoEffraCallersOnBothTargets(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(signed64ProfileSource, target)
			if !r.Checked {
				t.Fatalf("signed64 profile rejected: %+v", r.Diagnostics)
			}
			if target == "go" {
				if output := runGeneratedGo(t, r); output != "ok\n" {
					t.Fatalf("native callers returned %q", output)
				}
				return
			}
			output := runJS(t, signed64ProfileSource, `
const max = 9223372036854775807n;
const min = -9223372036854775808n;
if (__ef_function_counterAdvance(max, 1n) !== min) throw new Error("counter parameter overflow");
if (__ef_function_counterLiteral() !== min) throw new Error("counter literal overflow");
if (__ef_function_counterNested(max) !== -9223372036854775807n) throw new Error("nested operation did not wrap");
if (__ef_function_negate(min) !== min) throw new Error("minimum negation did not wrap");
if (__ef_function_negateLeading() !== min) throw new Error("leading minimum literal did not normalize");
if (!__ef_function_serverAvailable(-2n, -3n)) throw new Error("server comparison branch");
if (!__ef_function_serverBelowZero(-1n)) throw new Error("server less-than branch");
if ((await Effect.runPromise(__ef_function_main())) !== "ok") throw new Error("main caller mismatch");
`)
			if output != "" {
				t.Fatalf("JavaScript callers returned %q", output)
			}
		})
	}
}

func TestSigned64ProfileChecksLiteralsAndUnsupportedOperators(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, test := range []struct {
			name   string
			source string
			code   string
		}{
			{"positive minimum magnitude", `fn bad() -> i64 { 9223372036854775808 }`, "EF001"},
			{"positive minimum magnitude with leading zeros", `fn bad() -> i64 { 0009223372036854775808 }`, "EF001"},
			{"negative below minimum", `fn bad() -> i64 { -9223372036854775809 }`, "EF001"},
			{"string subtraction", `fn bad(value: string) -> string { value - "x" }`, "EF106"},
			{"mixed string and integer", `fn bad(value: string) -> string { value + 1 }`, "EF106"},
			{"mixed i64 and bool", `fn bad(value: i64) -> i64 { value + true }`, "EF106"},
			{"bool addition", `fn bad(value: bool) -> bool { value + true }`, "EF106"},
			{"string multiplication", `fn bad(value: string) -> string { value * "x" }`, "EF106"},
			{"mixed i64 and string multiplication", `fn bad(value: i64) -> i64 { value * "2" }`, "EF106"},
			{"bool division", `fn bad(value: bool) -> bool { value / true }`, "EF106"},
			{"mixed i64 and bool remainder", `fn bad(value: i64) -> i64 { value % true }`, "EF106"},
			{"unary plus", `fn bad(value: i64) -> i64 { value * +2 }`, "EF002"},
			{"division by literal zero", `fn bad(value: i64) -> i64 { value / 0 }`, "EF150"},
			{"remainder by negated zero", `fn bad(value: i64) -> i64 { value % -0 }`, "EF150"},
			{"division by unproven parameter", `fn bad(value: i64, divisor: i64) -> i64 { value / divisor }`, "EF150"},
			{"remainder by unproven parameter", `fn bad(value: i64, divisor: i64) -> i64 { value % divisor }`, "EF150"},
			{"division by computed divisor", `fn bad(value: i64, divisor: i64) -> i64 { if divisor == 0 { 0 } else { value / (divisor - 1) } }`, "EF150"},
			{"division on the zero branch", `fn bad(value: i64, divisor: i64) -> i64 { if divisor == 0 { value / divisor } else { 0 } }`, "EF150"},
			{"division where zero passes the guard", `fn bad(value: i64, divisor: i64) -> i64 { if divisor < 1 { value / divisor } else { 0 } }`, "EF150"},
			{"division where zero passes the negated guard", `fn bad(value: i64, divisor: i64) -> i64 { if divisor > -1 { 0 } else { 0 } } fn worse(value: i64, divisor: i64) -> i64 { if 0 <= divisor { value / divisor } else { 0 } }`, "EF150"},
			{"proof does not survive shadowing", `fn bad(value: i64, divisor: i64) -> i64 { if divisor == 0 { 0 } else { let divisor = value - value; value / divisor } }`, "EF150"},
			{"proof of a different binding", `fn bad(value: i64, divisor: i64) -> i64 { if value == 0 { 0 } else { value / divisor } }`, "EF150"},
		} {
			t.Run(target+"/"+test.name, func(t *testing.T) {
				r := CompileFor(test.source, target)
				if r.Checked || !hasCode(r, test.code) {
					t.Fatalf("unsupported numeric source was admitted: %+v", r.Diagnostics)
				}
				if _, _, err := r.Emit(false); err == nil {
					t.Fatal("diagnosed numeric source emitted")
				}
			})
		}
	}
}

func TestSigned64ProfilePreservesOperatorPrecedenceAndFormatting(t *testing.T) {
	source := `fn probe(value: i64) -> bool { -value + 1 <= value - 2 }`
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := FormatSource(formatted.Text); err != nil || again.Text != formatted.Text {
		t.Fatalf("formatted numeric source is not stable: %q %v", formatted.Text, err)
	}
	if !strings.Contains(formatted.Text, "-value + 1 <= value - 2") {
		t.Fatalf("formatter changed unary/binary spacing: %q", formatted.Text)
	}
	for _, target := range []string{"go", "js"} {
		r := CompileFor(formatted.Text+" effect fn main() -> void { void }", target)
		if !r.Checked {
			t.Fatalf("formatted numeric source rejected for %s: %+v", target, r.Diagnostics)
		}
	}
}

func TestSigned64ProfileEmitsPerOperationNormalizationAndStrictBigInt(t *testing.T) {
	r := CompileFor(signed64ProfileSource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(js, "BigInt.asIntN(64,"); count < 5 {
		t.Fatalf("JavaScript omitted per-operation normalization: count=%d\n%s", count, js)
	}
	if !strings.Contains(declarations, "counterAdvance: (arg_current: bigint, arg_delta: bigint) => bigint") || strings.Contains(declarations, "counterAdvance: (arg_current: number") {
		t.Fatalf("i64 declaration is not strict bigint: %s", declarations)
	}
	checkStrictTypeScript(t, declarations, `import { counterAdvance, serverAvailable } from "./generated.mjs";
const current: bigint = counterAdvance(1n, 2n);
const available: boolean = serverAvailable(4n, 2n);
void current; void available;
// @ts-expect-error i64 must reject number
counterAdvance(1, 2);
// @ts-expect-error i64 must reject mixed number/bigint
counterAdvance(1n, 2);`)
}

func TestSigned64ProfileGoConstantFailureIsNotPortableSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.constant.control\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nvar _ int64 = 9223372036854775807 + 1\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "build", "."); err == nil || !strings.Contains(string(output), "overflows") {
		t.Fatalf("Go constant-only overflow control unexpectedly compiled: %v\n%s", err, output)
	}
	result := CompileFor(`effect fn main() -> i64 { 9223372036854775807 + 1 }`, "go")
	if !result.Checked {
		t.Fatalf("Effra literal operation was rejected: %+v", result.Diagnostics)
	}
	if _, err := result.EmitGo(); err != nil {
		t.Fatal(err)
	}
}

func TestSigned64ProfileTargetNormalizationMutantFailsBehavior(t *testing.T) {
	r := CompileFor(`fn counter() -> i64 { 9223372036854775807 + 1 } effect fn main() -> string { if counter() == -9223372036854775808 { "ok" } else { "bad" } }`, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	mutant := strings.Replace(js, "BigInt.asIntN(64, (9223372036854775807n + 1n))", "(9223372036854775807n + 1n)", 1)
	if mutant == js {
		t.Fatal("normalization mutant did not alter generated JavaScript")
	}
	if output := runJSProbe(t, mutant, `if (await Effect.runPromise(__ef_function_main()) === "ok") throw new Error("normalization omission did not fail the causal control");`); output != "" {
		t.Fatalf("normalization mutant emitted unexpected output: %q", output)
	}
}

func TestSigned64LiteralOnlyUnaryNegationWrapsOnBothTargets(t *testing.T) {
	const source = `effect fn main() -> i64 {
    -(-9223372036854775808)
}`
	goResult := CompileFor(source, "go")
	if !goResult.Checked {
		t.Fatalf("Go rejected literal-only unary negation: %+v", goResult.Diagnostics)
	}
	if output := runGeneratedGo(t, goResult); output != "-9223372036854775808\n" {
		t.Fatalf("Go literal-only unary negation returned %q", output)
	}

	jsResult := CompileFor(source, "js")
	if !jsResult.Checked {
		t.Fatalf("JavaScript rejected literal-only unary negation: %+v", jsResult.Diagnostics)
	}
	if output := runJS(t, source, `
const minimum = -9223372036854775808n;
if (await Effect.runPromise(__ef_function_main()) !== minimum) {
    throw new Error("literal-only unary negation did not wrap");
}
`); output != "" {
		t.Fatalf("JavaScript literal-only unary negation returned %q", output)
	}
}
