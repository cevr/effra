package compiler

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// signed64MultiplicativeCallers are ordinary Effra callers of `*`, `/` and
// `%`. Division and remainder take a runtime divisor only behind a guard that
// excludes zero, so the same functions run every boundary case on both
// targets.
const signed64MultiplicativeCallers = `
fn multiply(left: i64, right: i64) -> i64 {
    left * right
}
fn quotient(dividend: i64, divisor: i64) -> i64 {
    if divisor == 0 { 0 } else { dividend / divisor }
}
fn remainder(dividend: i64, divisor: i64) -> i64 {
    if 0 == divisor { 0 } else { dividend % divisor }
}
fn pageCount(items: i64, pageSize: i64) -> i64 {
    if pageSize > 0 { (items + pageSize - 1) / pageSize } else { 0 }
}
fn negativeStep(total: i64, step: i64) -> i64 {
    if step < 0 { total % step } else { total }
}
fn halfNegated(value: i64) -> i64 {
    -value / 2
}
fn mixedPrecedence(a: i64, b: i64) -> i64 {
    a + b * 3 % 5 - -a / 2
}
fn literalMinimumQuotient() -> i64 {
    -9223372036854775808 / -1
}
fn literalMinimumRemainder() -> i64 {
    -9223372036854775808 % -1
}
fn literalOverflowProduct() -> i64 {
    9223372036854775807 * 2
}
`

var signed64Boundaries = []int64{math.MinInt64, math.MinInt64 + 1, -7, -2, -1, 0, 1, 2, 7, math.MaxInt64 - 1, math.MaxInt64}

type signed64Case struct {
	call     string
	expected int64
}

func i64Source(value int64) string { return strconv.FormatInt(value, 10) }

// signed64Cases is the boundary matrix. Expected values come from Go's own
// int64 operators, which wrap and truncate toward zero; the hand-written
// truncation rows pin that reference independently of Go.
func signed64Cases() []signed64Case {
	cases := []signed64Case{
		{"quotient(-7, 2)", -3}, {"remainder(-7, 2)", -1},
		{"quotient(7, -2)", -3}, {"remainder(7, -2)", 1},
		{"quotient(-7, -2)", 3}, {"remainder(-7, -2)", -1},
		{"quotient(-9223372036854775808, -1)", math.MinInt64}, {"remainder(-9223372036854775808, -1)", 0},
		{"multiply(9223372036854775807, 2)", -2}, {"multiply(-9223372036854775808, -1)", math.MinInt64},
		{"quotient(5, 0)", 0}, {"remainder(5, 0)", 0},
		{"pageCount(10, 3)", 4}, {"pageCount(9, 3)", 3}, {"pageCount(9, 0)", 0},
		{"negativeStep(-7, -2)", -1}, {"negativeStep(7, 2)", 7},
		{"halfNegated(-9223372036854775808)", -4611686018427387904},
		{"halfNegated(7)", -3},
		{"mixedPrecedence(-7, 4)", -7 + 4*3%5 - (7)/2},
		{"literalMinimumQuotient()", math.MinInt64}, {"literalMinimumRemainder()", 0},
		{"literalOverflowProduct()", -2},
	}
	for _, left := range signed64Boundaries {
		for _, right := range signed64Boundaries {
			cases = append(cases, signed64Case{"multiply(" + i64Source(left) + ", " + i64Source(right) + ")", left * right})
			if right != 0 {
				cases = append(cases,
					signed64Case{"quotient(" + i64Source(left) + ", " + i64Source(right) + ")", left / right},
					signed64Case{"remainder(" + i64Source(left) + ", " + i64Source(right) + ")", left % right})
			}
		}
	}
	return cases
}

// signed64CaseProgram chains one checker function per case, so a mismatch
// names its case and the nesting depth stays constant.
func signed64CaseProgram(cases []signed64Case) string {
	var out strings.Builder
	out.WriteString(signed64MultiplicativeCallers)
	for index, test := range cases {
		next := `"ok"`
		if index+1 < len(cases) {
			next = fmt.Sprintf("case%d()", index+1)
		}
		fmt.Fprintf(&out, "fn case%d() -> string {\n    if %s == %s { %s } else { %q }\n}\n", index, test.call, i64Source(test.expected), next, test.call)
	}
	out.WriteString("effect fn main() -> string {\n    case0()\n}\n")
	return out.String()
}

func TestSigned64MultiplicativeOperatorsAgreeOnBothTargets(t *testing.T) {
	cases := signed64Cases()
	source := signed64CaseProgram(cases)
	if r := CompileFor(source, "go"); !r.Checked {
		t.Fatalf("multiplicative callers rejected: %+v", r.Diagnostics)
	} else if output := runGeneratedGo(t, r); output != "ok\n" {
		t.Fatalf("native boundary case failed: %q", output)
	}
	var probe strings.Builder
	probe.WriteString("const mismatches = [];\n")
	for _, test := range cases {
		call := test.call
		for _, name := range []string{"multiply", "quotient", "remainder", "pageCount", "negativeStep", "halfNegated", "mixedPrecedence", "literalMinimumQuotient", "literalMinimumRemainder", "literalOverflowProduct"} {
			call = strings.Replace(call, name+"(", "__ef_function_"+name+"(", 1)
		}
		call = jsBigIntArguments(call)
		fmt.Fprintf(&probe, "if (%s !== %sn) mismatches.push(%q);\n", call, i64Source(test.expected), test.call)
	}
	probe.WriteString(`if (mismatches.length > 0) throw new Error(mismatches.join("; "));
if ((await Effect.runPromise(__ef_function_main())) !== "ok") throw new Error("main caller mismatch");
`)
	if output := runJS(t, source, probe.String()); output != "" {
		t.Fatalf("JavaScript boundary probe returned %q", output)
	}
}

// jsBigIntArguments rewrites the integer arguments of a case call as bigint
// literals for a direct JavaScript call.
func jsBigIntArguments(call string) string {
	open := strings.Index(call, "(")
	if open < 0 || strings.HasSuffix(call, "()") {
		return call
	}
	arguments := strings.Split(strings.TrimSuffix(call[open+1:], ")"), ", ")
	for index, argument := range arguments {
		arguments[index] = argument + "n"
	}
	return call[:open+1] + strings.Join(arguments, ", ") + ")"
}

func TestSigned64MultiplicativeDeclarationsAreStrictBigInt(t *testing.T) {
	r := CompileFor(signed64MultiplicativeCallers, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, normalized := range []string{"BigInt.asIntN(64, (__ef_local_left * __ef_local_right))", "BigInt.asIntN(64, (__ef_local_dividend / __ef_local_divisor))"} {
		if !strings.Contains(js, normalized) {
			t.Fatalf("JavaScript omitted %s\n%s", normalized, js)
		}
	}
	if strings.Contains(js, "BigInt.asIntN(64, (__ef_local_dividend % __ef_local_divisor))") {
		t.Fatalf("remainder cannot leave the signed range and must not pay for normalization\n%s", js)
	}
	checkStrictTypeScript(t, declarations, `import { multiply, quotient, remainder } from "./generated.mjs";
const product: bigint = multiply(3n, -4n);
const whole: bigint = quotient(-7n, 2n);
const rest: bigint = remainder(-7n, 2n);
void product; void whole; void rest;
// @ts-expect-error i64 multiplication must reject number
multiply(3, 4);
// @ts-expect-error i64 division must reject mixed number/bigint
quotient(7n, 2);
// @ts-expect-error i64 remainder returns bigint, not number
const wrong: number = remainder(7n, 2n);
void wrong;`)
}

func TestSigned64MultiplicativeNormalizationMutantsFailBehavior(t *testing.T) {
	source := signed64MultiplicativeCallers + `effect fn main() -> void { void }`
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutant := range []struct{ normalized, raw, probe string }{
		{"BigInt.asIntN(64, (__ef_local_left * __ef_local_right))", "(__ef_local_left * __ef_local_right)", `if (__ef_function_multiply(9223372036854775807n, 2n) === -2n) throw new Error("multiplication mutant still wrapped");`},
		{"BigInt.asIntN(64, (__ef_local_dividend / __ef_local_divisor))", "(__ef_local_dividend / __ef_local_divisor)", `if (__ef_function_quotient(-9223372036854775808n, -1n) === -9223372036854775808n) throw new Error("division mutant still wrapped");`},
	} {
		changed := strings.Replace(js, mutant.normalized, mutant.raw, 1)
		if changed == js {
			t.Fatalf("mutant did not alter %s", mutant.normalized)
		}
		if output := runJSProbe(t, withProbeImports(changed), mutant.probe); output != "" {
			t.Fatalf("mutant probe returned %q", output)
		}
	}
}

func TestSigned64MultiplicativePrecedenceAndFormatting(t *testing.T) {
	source := "import go strings \"strings\"\nfn probe(a: i64, b: i64, r: *strings.Reader) -> i64 { -a/2+b*3%5 - a*-b }\n"
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := FormatSource(formatted.Text); err != nil || again.Text != formatted.Text {
		t.Fatalf("formatted multiplicative source is not stable: %q %v", formatted.Text, err)
	}
	if !strings.Contains(formatted.Text, "-a / 2 + b * 3 % 5 - a * -b") || !strings.Contains(formatted.Text, "r: *strings.Reader") {
		t.Fatalf("formatter changed operator or pointer spacing: %q", formatted.Text)
	}
	program, diagnostics := parse("fn probe(a: i64, b: i64) -> i64 { -a / b * 2 + a % b }")
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	body := program.Functions[0].Body.Statements[0].Value
	if got := renderOperatorTree(body); got != "(((-a) / b) * 2) + (a % b)" {
		t.Fatalf("multiplicative precedence = %s", got)
	}
}

func renderOperatorTree(e *Expr) string {
	switch e.Kind {
	case "binary":
		left, right := renderOperatorTree(e.Left), renderOperatorTree(e.Right)
		if e.Left.Kind == "binary" {
			left = "(" + left + ")"
		}
		if e.Right.Kind == "binary" {
			right = "(" + right + ")"
		}
		return left + " " + e.Name + " " + right
	case "unary":
		return "(" + e.Name + renderOperatorTree(e.Left) + ")"
	case "name":
		return e.Name
	default:
		return e.Text
	}
}
