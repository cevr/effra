package check_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"effra.local/prototype/internal/sketch/check"
)

// reviewCases reproduce the round-1 review probes (round-1-findings.md) and
// the repairs' own cases. Each source is a package main body; want is the
// exact sequence of codes, "" for none.
var reviewCases = []struct {
	finding, name, want, src string
}{
	{"M1", "inference-chain-through-values", "S102", ""}, // built by largeSCC
	{"M2", "machine-initial-inferred", "S102", `type C interface{ Use() }
type S interface{ Idle }
type Idle struct{}
type E interface{ Ping }
type Ping struct{}
type M machine[S, E] { initial initial(); step step }
func initial() S { C.Use(); return Idle{} }
func step(s S, e E) Decision[S, int] { return Ignore{} }`},
	{"M2", "package-var-inferred", "S102", `type C interface{ Use() string }
var v = helper()
func helper() string { return C.Use() }`},
	{"M3", "error-row-package-var", "S101", `type Fault struct{}
func Identity(v Fault) Fault { return v }
var _ = func() error[Fault] { return Fault{} }`},
	{"M3", "generic-row-body-order", "S101", `type Fault[T] struct{ Value T }
func Identity(v Fault[int]) Fault[int] { return v }
func F() { _ = func() error[Fault[int]] { return Fault[int]{1} } }`},
	{"M3", "local-error-row", "S101", `type Fault struct{}
func Identity(v Fault) Fault { return v }
func F() { _ = func() error[Fault] { return Fault{} } }`},
	{"M4", "error-union-rethrow", "", `type A struct{}
type B struct{}
func Fail() error[A | B]
func F() error[A | B] { Fail() catch e { case A, B: return e } }`},
	{"M4", "catch-union-switch", "", `type A struct{}
type B struct{}
func Fail() error[A | B]
func F() { Fail() catch e { case A, B: switch _ := e.(type) { case A: return; case B: return } } }`},
	{"M4", "catch-union-switch-not-total", "S103", `type A struct{}
type B struct{}
func Fail() error[A | B]
func F() { Fail() catch e { case A, B: switch _ := e.(type) { case A: return } } }`},
	{"M4", "error-union-as-value", "S101", `type A struct{}
type B struct{}
func Fail() error[A | B]
func F() any { Fail() catch e { case A, B: return e }; return "ok" }`},
	{"M5", "generic-named-constraint-inferred", "S108", `type I interface { M() }
type S struct{}
func (S) M() {}
type Box[T I] struct{ X T }
func F() { _ = Box{1} }`},
	{"M5", "generic-constraint-method-row", "S108", `type A struct{}
type B struct{}
type I interface{ M() error[A] }
type S struct{}
func (S) M() error[B] { return B{} }
type Box[T I] struct{ X T }
func F() any { return Box{S{}} }`},
	{"M5", "generic-constraint-inferred-ok", "", `type I interface { M() }
type S struct{}
func (S) M() {}
type Box[T I] struct{ X T }
func F() any { return Box{S{}} }`},
	{"M6", "implicit-row-in-generic-argument", "", `func Await(t Task[T, E]) (T, error[E]) { return t.Wait() }`},
	{"M6", "bracket-row-in-generic-field", "", `type Box[T, E] struct { t Task[T, E] }
func Open(b Box[T, E]) (T, error[E]) { return b.t.Wait() }`},
	{"M6", "row-kind-conflict-still-reported", "S108", `func Bad(t Task[T, E], e E) T { return t.Wait() }`},
	{"M7", "generic-recursive-value", "S108", `type Wrap[T] struct { Value T }
type U struct{ Field Wrap[U] }
func F() U { return U{} }`},
	{"M7", "generic-recursive-constraint", "S108", `type Wrap[T] struct { Value T }
type U struct{ Field Wrap[U] }`},
	{"M7", "generic-recursive-through-slice", "", `type Wrap[T] struct { Values []T }
type U struct{ Field Wrap[U] }`},
	{"M7", "generic-self-recursive", "S108", `type L[T] struct { next L[T] }`},
	{"M7", "generic-growing-instance", "S108", `type L[T] struct { next L[[1]T] }`},
	{"M7", "generic-pair-cycle", "S108", `type A[T] struct { b B[T] }
type B[T] struct { a A[T] }`},
	{"M9", "short-declaration-reuse", "", `func F() int { x := 1; x, y := 2, 3; return x + y }`},
	{"M9", "short-declaration-reuse-type", "S108", `func F() int { x := 1; x, y := "s", 3; return x + y }`},
	{"M9", "duplicate-local-unused", "S108", `func F() { x := 1; x := 2; _ = x }`},
	{"M9", "parameter-reuse", "", `func F(x int) int { x, y := 2, 3; return x + y }`},
	{"M9", "blank-only", "S108", `func F() { _ := 1 }`},
	{"M9", "repeated-name", "S108", `func F() int { a, a := 1, 2; return a }`},
	{"M9", "reuse-from-call", "", `func two() (int, string) { return 1, "s" }
func F() string { x := 0; x, s := two(); _ = x; return s }`},
	{"M9", "reuse-from-call-type", "S108", `func two() (string, int) { return "s", 1 }
func F() int { x := 0; x, n := two(); return x + n }`},
	{"M9", "reuse-is-not-a-read", "S110", `func F() int { x := 1; x, y := 2, 3; return y }`},
	{"M9", "inner-block-shadows", "", `func F() int { x := 1; if true { x := "s"; _ = x }; return x }`},
	{"M10", "function-value-explicit", "", `func Id[T](x T) T { return x }
func F() func(int) int { return Id[int] }`},
	{"M10", "function-value-explicit-constraint", "S108", `type I interface{ M() }
func Id[T I](x T) T { return x }
func F() func(int) int { return Id[int] }`},
	{"M10", "function-value-wrong-type", "S108", `func Id[T](x T) T { return x }
func F() func(string) string { return Id[int] }`},
	{"M10", "function-value-called", "", `func Id[T](x T) T { return x }
func F() int { f := Id[int]; return f(3) + Id[int](4) }`},
	{"M10", "function-value-statement", "S110", `func Id[T](x T) T { return x }
func F() { Id[int] }`},
	{"M10", "function-value-implicit", "S108", `func Await[T](x T, t Task[T, E]) (T, error[E]) { return t.Wait() }
func F() { _ = Await[int] }`},
	{"M11", "index-negative", "S108", `func F(xs []int) int { return xs[-1] }`},
	{"M11", "index-out-of-bounds", "S108", `func F() int { return [1]int{7}[2] }`},
	{"M11", "make-negative", "S108", `func F() []int { return make([]int, -1) }`},
	{"M11", "make-capacity", "S108", `func F() []int { return make([]int, 2, 1) }`},
	{"M11", "slice-bounds", "S108", `func F(xs []int) []int { return xs[3:1] }`},
	{"M11", "string-index-out-of-bounds", "S108", `func F() byte { return "ab"[2] }`},
	{"M11", "array-slice-out-of-bounds", "S108", `func F() []int { a := [2]int{1, 2}; return a[:3] }`},
	{"M11", "string-slice-out-of-bounds", "S108", `func F() string { return "ab"[1:3] }`},
	{"M11", "bounds-ok", "", `func F(xs []int) []int { _ = [2]int{1, 2}[1]; _ = make([]int, 1, 2); return xs[1:3] }`},
	{"M12", "float-rounding", "S108", `func F() int { return 1 / int(float32(16777217) - float32(16777216)) }`},
	{"M12", "float-rounding-arithmetic", "S108", `func F() int { return 1 / int(float32(16777216) + 1 - float32(16777216)) }`},
	{"M12", "float64-exact-enough", "", `func F() int { return 1 / int(float64(16777217) - float64(16777216)) }`},
	{"M13", "embedded-collision-replaced", "", `type I interface{ M() }
type S struct{}
func (S) M() {}
wire A { I = S{} }
wire B { I = S{} }
wire W { A; B; replace I = S{} }
func F() { with(W) { I.M() } }`},
	{"M13", "embedded-collision-inline-replaced", "", `type I interface{ M() }
type S struct{}
func (S) M() {}
wire A { I = S{} }
wire B { I = S{} }
func F() { with(wire{ A; B; replace I = S{} }) { I.M() } }`},
	{"M13", "embedded-collision-unresolved", "S111", `type I interface{ M() }
type S struct{}
func (S) M() {}
wire A { I = S{} }
wire B { I = S{} }
wire W { A; B }
func F() { with(W) { I.M() } }`},
	{"M13", "wire-double-replace", "", `type I interface{ M() }
type S struct{}
func (S) M() {}
wire Base { I = S{} }
wire W { Base; replace I = S{}; replace I = S{} }
func F() { with(W) { I.M() } }`},
	{"M14", "named-wire-internal-capability", "", `type A interface { M() }
type B interface { N() }
type S struct{}
func (S) M() {}
func (S) N() {}
func Make() B uses A { A.M(); return S{} }
wire W { A = S{}; B = Make() }
func F() { with(W) { B.N() } }`},
	{"M14", "named-wire-internal-cycle", "S108", `type A interface { M() }
type B interface { N() }
type S struct{}
func (S) M() {}
func (S) N() {}
func MakeA() A uses B { B.N(); return S{} }
func MakeB() B uses A { A.M(); return S{} }
wire W { A = MakeA(); B = MakeB() }
func F() { with(W) { B.N() } }`},
	{"M14", "named-wire-caller-capability", "S102", `type A interface { M() }
type B interface { N() }
type S struct{}
func (S) N() {}
func Make() B uses A { A.M(); return S{} }
wire W { B = Make() }
func F() uses A { with(W) { B.N() } }`},
	{"M14", "named-wire-self-cycle", "S108", `type A interface { M() }
type S struct{}
func (S) M() {}
func MakeA() A uses A { A.M(); return S{} }
wire W { A = MakeA() }
func F() { with(W) { A.M() } }`},
	{"M14", "named-wire-three-cycle", "S108", `type A interface { M() }
type B interface { N() }
type C interface { O() }
type S struct{}
func (S) M() {}
func (S) N() {}
func (S) O() {}
func MakeA() A uses C { C.O(); return S{} }
func MakeB() B uses A { A.M(); return S{} }
func MakeC() C uses B { B.N(); return S{} }
wire W { A = MakeA(); B = MakeB(); C = MakeC() }
func F() { with(W) { B.N() } }`},
	{"M14", "named-wire-uses-embedded", "", `type A interface { M() }
type B interface { N() }
type S struct{}
func (S) M() {}
func (S) N() {}
func Make() B uses A { A.M(); return S{} }
wire Base { A = S{} }
wire W { Base; B = Make() }
func F() { with(W) { B.N() } }`},
	{"M14", "inline-wire-stays-open", "S102", `type A interface { M() }
type B interface { N() }
type S struct{}
func (S) M() {}
func (S) N() {}
func Make() B uses A { A.M(); return S{} }
func F() { with(wire{ A = S{}; B = Make() }) { B.N() } }`},
	{"M15", "main-through-alias", "S113", `func main() {}
func F() { run := main; run() }`},
	{"M15", "main-as-argument", "S113", `func main() {}
func call(f func()) { f() }
func F() { call(main) }`},
	{"M15", "main-returned", "S113", `func main() {}
func F() func() { return main }`},
	{"M15", "main-called-in-parens", "S113", `func main() {}
func F() { (main)() }`},
	{"M15", "local-main-is-ordinary", "", `func main() {}
func F() { main := func() {}; main() }`},
	{"M16", "with-default-hint", "", `type Box[T] struct{ X T }
func F() Box[int] { return with(wire{}) { Box{} } }`},
	{"M17", "map-index-field-assignment", "S108", `type S struct{ X int }
func F(m map[int]S) { m[0].X = 1 }`},
	{"M17", "map-index-assignment", "", `type S struct{ X int }
func F(m map[int]S, n map[int]int) { m[0] = S{}; m[1] = S{X: 2}; n[3] += S{}.X }`},
	{"M17", "map-index-field-increment", "S108", `type S struct{ X int }
func F(m map[int]S) { m[2].X++ }`},
	{"M17", "map-value-increment", "", `func F(m map[int]int) { m[0]++; m[1] += 2 }`},
	{"M17", "slice-element-field", "", `type S struct{ X int }
func F(xs []S) { xs[0].X = 1 }`},
	{"M17", "map-index-address", "S108", `type S struct{ X int }
func G(s *S) {}
func F(m map[int]S) { G(&m[0]) }`},
	{"M18", "unknown-wire-cascade", "S104", `func F() { with(Missing) { Console.Println("x") } }`},
	{"M18", "unknown-wire-cascade-in-wire", "S104", `func F() { with(wire{ Missing }) { Console.Println("x") } }`},
	{"M18", "unknown-wire-cascade-named", "S104", `wire W { Missing }
func F() { with(W) { Console.Println("x") } }`},
	{"N1", "pointer-receiver-parameter", "", `type I interface{ M() }
type S struct{}
func (s *S) M() {}
func Take(x I)
func F(s *S) { Take(s) }`},
	{"N1", "pointer-receiver-value", "S108", `type I interface{ M() }
type S struct{}
func (s *S) M() {}
func Take(x I)
func F(s S) { Take(s) }`},
	{"N2", "complex-types", "", `func F() complex128 { return 1 }
func G(a, b complex64) complex64 { return a*b + a/b - b }
func H(a complex128) bool { return a == a }`},
	{"N2", "complex-ordering", "S108", `func F(a, b complex128) bool { return a < b }`},
	{"N2", "complex-conversions", "", `func F(a complex128, b complex64) complex64 { return complex64(a) + b + complex64(complex128(2)) }`},
	{"N2", "float-to-complex-value", "S108", `func F(f float64) complex128 { return complex128(f) }`},
	{"N2", "complex-remainder", "S108", `func F(a, b complex128) complex128 { return a % b }`},
	{"N2", "complex64-overflow", "S108", `func F() complex64 { return 1 << 200 }`},
	{"N3", "map-duplicate-key", "S108", `func F() map[int]int { return map[int]int{1: 1, 1: 2} }`},
	{"N3", "map-duplicate-string-key", "S108", `func F() map[string]int { return map[string]int{"a": 1, "a": 2} }`},
	{"N3", "map-converted-key", "S108", `func F() map[int64]int { return map[int64]int{1: 1, int64(1): 2} }`},
	{"N3", "map-distinct-keys", "", `func F(k int) map[int]int { return map[int]int{1: 1, 2: 2, k: 3, k: 4} }`},
	{"N4", "paren-lhs", "", `func F() int { x := 1; (x) = 2; return x }`},
	// Cascades and accepted forms the review logged without a contract.
	{"D3", "unknown-import-cascade", "S104", `import "missing"
func F() { missing.Call() }`},
	{"D3", "unknown-named-import-cascade", "S104", `import m "missing/deep"
func F() { m.Call() }`},
	{"D3", "go-import-cascade", "S109", `import go "fmt"
func F() { fmt.Println("x") }`},
	{"D3", "unknown-import-type-cascade", "S104", `import "missing"
func F(x missing.T) {}`},
	{"D3", "unknown-type-unused-cascade", "S104", `func F() { var x Missing }`},
	{"D3", "unknown-callee-return-cascade", "S104", `func F() int { return missing() }`},
	{"D3", "invalid-field-cascade", "S108", `type S struct{ X int }
func F() S { return S{X: "x"} }`},
	{"D3", "switch-no-value-cascade", "S110", `type A struct{}
type B struct{}
type U interface { A | B }
func F(x U) int { switch v := x.(type) { case A: return 1; case B: return 2 } }`},
	{"D2", "inferred-no-value-return", "S108", `func f() {}
func F() { return f() }`},
	{"D2", "numeric-constraint-literal", "S108", `func Zero[T comparable](x T) T { return 0 }`},
	{"D6", "generic-two-rows", "", `type A struct{}
type B struct{}
func First() error[A]
func Second() error[B]
func Both(f func() error[E], g func() error[E]) error[E] { try f(); return g() }
func F() error[A | B] { return Both(First, Second) }`},
	{"D6", "generic-two-rows-too-wide", "S101", `type A struct{}
type B struct{}
func First() error[A]
func Second() error[B]
func Both(f func() error[E], g func() error[E]) error[E] { try f(); return g() }
func F() error[A] { return Both(First, Second) }`},
}

func codesOf(diags []check.Diagnostic) string {
	var codes []string
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return strings.Join(codes, " ")
}

func TestReviewCases(t *testing.T) {
	for _, c := range reviewCases {
		t.Run(c.finding+"/"+c.name, func(t *testing.T) {
			src := c.src
			if src == "" {
				src = largeSCC(100)
			} else {
				src = "package main\n" + src + "\n"
			}
			_, diags := check.CheckSource(src)
			if got := codesOf(diags); got != c.want {
				t.Errorf("want %q, got %q\n\t%s", c.want, got, describe(diags))
			}
		})
	}
}

// largeSCC is the M1 probe: f0 names f1…fn as values and calls C.Use(); each
// fi calls f(i-1); exported F calls fn, so the inferred row must reach F.
func largeSCC(n int) string {
	var b strings.Builder
	b.WriteString("package main\ntype C interface{ Use() }\nfunc f0() {\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "\t_ = f%d\n", i)
	}
	b.WriteString("\tC.Use()\n}\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "func f%d() { f%d() }\n", i, i-1)
	}
	fmt.Fprintf(&b, "func F() { f%d() }\n", n)
	return b.String()
}

// TestInferenceConverges: every function of the cycle gets the full row,
// whatever the cycle's length (M1).
func TestInferenceConverges(t *testing.T) {
	for _, n := range []int{10, 100, 400} {
		pkg, diags := check.CheckSource(largeSCC(n))
		if codesOf(diags) != "S102" {
			t.Fatalf("n=%d: want one S102, got %s", n, describe(diags))
		}
		for _, f := range pkg.Funcs {
			if f.Name == "F" || f.Name == "main" {
				continue
			}
			if !strings.Contains(f.Sig.String(), "main.C") {
				t.Fatalf("n=%d: %s has row %s, want main.C", n, f.Name, f.Sig)
			}
		}
	}
}

// oddCycleSwitch is the M8 probe: n subjects of union A | B, with cases that
// cover equal adjacent subjects around an odd cycle. It is total, and no case
// is redundant.
func oddCycleSwitch(n int) string {
	var b strings.Builder
	b.WriteString("package main\ntype A struct{}\ntype B struct{}\ntype U interface{A | B}\nfunc F(")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d U", i)
	}
	b.WriteString(") int {\nswitch ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("_")
	}
	b.WriteString(" := ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "x%d.(type)", i)
	}
	b.WriteString(" {\n")
	for i := 0; i < n; i++ {
		for _, variant := range []string{"A", "B"} {
			b.WriteString("case (")
			for j := 0; j < n; j++ {
				if j > 0 {
					b.WriteString(",")
				}
				if j == i || j == (i+1)%n {
					b.WriteString(variant)
				} else {
					b.WriteString("_")
				}
			}
			b.WriteString("): return 1\n")
		}
	}
	b.WriteString("}\n}\n")
	return b.String()
}

// TestTotalityCost: the odd-cycle family stays clean and cheap (M8, D16).
// The bound is generous for a loaded gate machine; before the repair the
// 21-subject case took about 4.9 s.
func TestTotalityCost(t *testing.T) {
	for _, n := range []int{11, 15, 19, 21, 41} {
		src := oddCycleSwitch(n)
		start := time.Now()
		_, diags := check.CheckSource(src)
		elapsed := time.Since(start)
		if len(diags) != 0 {
			t.Fatalf("n=%d: %s", n, describe(diags))
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("n=%d: %s, want under 500ms", n, elapsed)
		}
		t.Logf("subjects=%d bytes=%d elapsed=%s", n, len(src), elapsed)
	}
	// An even cycle is two-colourable with unequal neighbours: not total, and
	// an alternating vector is the witness. Its closing edge's two cases are
	// unreachable (the path before them already forces x0 != x19). No S107
	// follows the S103 (D3; round-2 N1).
	src := oddCycleSwitch(20)
	_, diags := check.CheckSource(src)
	if got := codesOf(diags); got != "S103 S108 S108" {
		t.Fatalf("even cycle: want S103 S108 S108, got %s", describe(diags))
	}
	if !strings.Contains(diags[0].Message, "(main.A, main.B, main.A, main.B") && !strings.Contains(diags[0].Message, "(main.B, main.A, main.B, main.A") {
		t.Fatalf("even cycle: want an alternating witness, got %s", diags[0].Message)
	}
}
