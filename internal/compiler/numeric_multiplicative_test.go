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

// signed64TailSource is a defaulted self-tail loop over the multiplicative
// operators: factor and modulus are omitted on every self call, `/` divides
// by a literal and `%` by a parameter proven nonzero by its guard, and `*`
// wraps on most iterations. A million iterations stay bounded only when
// the call is a loop.
const signed64TailSource = `fn mix(n: i64, factor: i64 = 6364136223846793005, acc: i64 = 1, modulus: i64 = 1000003) -> i64 {
    if n == 0 {
        acc
    } else {
        if modulus == 0 {
            acc
        } else {
            mix(n - 1, acc: acc * factor / 7 + n % modulus)
        }
    }
}
effect fn main() -> string {
    if mix(1000000) == EXPECTED { "ok" } else { "bad" }
}
`

func TestSigned64ArithmeticInDefaultedSelfTailLoop(t *testing.T) {
	factor, acc := int64(6364136223846793005), int64(1)
	for n := int64(1000000); n > 0; n-- {
		acc = acc*factor/7 + n%1000003
	}
	source := strings.Replace(signed64TailSource, "EXPECTED", i64Source(acc), 1)
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if lowerTail(r.Find("mix").function(t, r)) == nil {
		t.Fatal("defaulted self-tail call over signed arithmetic was not planned as a loop")
	}
	runGenericDataNative(t, source, "ok\n")
	native, _, err := emitGoApplication(CompileFor(source, "go"), GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(native, "for {") != 1 {
		t.Fatalf("expected one native loop:\n%s", native)
	}
}
