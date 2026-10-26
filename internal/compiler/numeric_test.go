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
	r := CompileFor(signed64ProfileSource, "go")
	if !r.Checked {
		t.Fatalf("signed64 profile rejected: %+v", r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "ok\n" {
		t.Fatalf("native callers returned %q", output)
	}
}

func TestSigned64ProfileChecksLiteralsAndUnsupportedOperators(t *testing.T) {
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
		t.Run(test.name, func(t *testing.T) {
			r := CompileFor(test.source, "go")
			if r.Checked || !hasCode(r, test.code) {
				t.Fatalf("unsupported numeric source was admitted: %+v", r.Diagnostics)
			}
		})
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
	r := CompileFor(formatted.Text+" effect fn main() -> void { void }", "go")
	if !r.Checked {
		t.Fatalf("formatted numeric source rejected for go: %+v", r.Diagnostics)
	}
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
}
