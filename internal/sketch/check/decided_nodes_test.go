package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// decidedFamilies is every node and field family the decided parser builds
// (spec D3; merge round step 3): each has at least one row in
// TestDecidedNodes, with its verdict, so a family without one fails the
// test. A family the 196a checker reads is checked; any other is S109
// naming its stage, with no S108 or S104 beside it (round 6 review M2).
var decidedFamilies = []string{
	"import path", "import go", "import std",
	"let", "let mut", "let without value", "let pattern", "let tuple pattern",
	"package let pattern", "package let tuple pattern",
	"wire declaration", "wire header", "wire value", "inline wire header", "wire entries",
	"with", "with parentheses", "with several wires",
	"record type", "operation type", "law", "union", "variant in place", "machine",
	"defined type", "type parameters", "ordered constraint",
	"fails and needs", "escaping", "tuple type", "tuple value", "Map type",
	"first-parameter function", "extern",
	"test", "assert",
	"if value", "match value", "value match", "type match", "tuple subject",
	"record pattern", "as pattern", "nested as pattern",
	"catch arm", "catch-all binding",
	"for in", "for pattern", "for tuple pattern", "counting range", "range value", "for condition",
	"assignment", "break and continue",
	"function literal", "untyped parameter",
	"interpolation", "float literal",
}

// TestDecidedNodes: the checker reads the trees the decided parser builds
// (#196). Each row names its family; every family has a row, and every
// row's verdict holds: never a panic, never silently accepted, never a
// cascade beside an S109.
func TestDecidedNodes(t *testing.T) {
	cases := []struct {
		family string
		name   string
		src    string
		want   []string // "code:line", sorted as the checker reports them
	}{
		{"import path", "an unquoted import path", `
import timeout
func f() timeout.Expired { return timeout.Expired{} }
func main() {}
`, nil},
		{"import go", "a Go import is #202's", `
import go strings
func main() {}
`, []string{"S109:2"}},
		{"import std", "an import of a std module lower-v1 brings", `
import task
func main() { task.Go() { 1 } }
`, []string{"S109:2"}},
		{"import std", "a std import adds no cascade", `
import task
type Bang {}
func a() { task.Go() { b() }; return Bang{} }
func b() { return a() }
func F() fails Bang { return b() }
func main() {}
`, []string{"S109:2"}},
		{"let", "let names a value", `
func f() int64 {
	let x int64 = 1
	let y = x + 1
	return y
}
func main() {}
`, nil},
		{"let mut", "let mut is assigned", `
func f() int64 {
	let mut x int64 = 1
	x += 2
	return x
}
func main() {}
`, nil},
		{"let without value", "let with a type and no value takes the zero value", `
func f() int64 {
	let x int64
	return x
}
func main() {}
`, nil},
		{"let pattern", "a record pattern after let is 196p's", `
type P { X int, Y int }
func f(p P) int {
	let P{X: x, Y: y} = p
	return x + y
}
func main() {}
`, []string{"S109:4"}},
		{"let tuple pattern", "a tuple pattern after let is 196t's, and its value is checked", `
func f() int {
	let (a, b) = (1, undefinedName)
	return a + b
}
func main() {}
`, []string{"S109:3", "S104:3"}},
		{"package let pattern", "a package-level record pattern is 196p's", `
type P { X int, Y int }
let P{X: a, Y: b} = P{X: 1, Y: "no"}
func f() int { return a + b }
func main() {}
`, []string{"S109:3"}},
		{"package let tuple pattern", "a package-level tuple pattern is never accepted unchecked", `
let (a, b) = (1, undefinedName)
func main() {}
`, []string{"S109:2"}},
		{"wire declaration", "let W = wire", `
type Clock { Now() int64 }
type Fixed { t int64 }
func Now(f Fixed) int64 { return f.t }
let W = wire { Clock: Fixed{t: 1} }
func f() int64 { return with W { Clock.Now() } }
func main() {}
`, nil},
		{"wire header", "a named wire's provides and needs", `
type Clock { Now() int64 }
type Log { Write(s string) }
type Fixed { t int64 }
func Now(f Fixed) int64 { return f.t }
let W = wire provides Clock needs Log { Clock: Fixed{t: 1} }
func main() {}
`, []string{"S109:6", "S109:6"}},
		{"wire value", "a wire value outside with is 196x's, with no cascade at its use", `
type Clock { Now() int64 }
type Fixed { t int64 }
func Now(f Fixed) int64 { return f.t }
func f() int64 {
	let w = wire { Clock: Fixed{t: 1} }
	return with w { Clock.Now() }
}
func main() {}
`, []string{"S109:6"}},
		{"inline wire header", "an inline wire's provides", `
type Clock { Now() int64 }
type Fixed { t int64 }
func Now(f Fixed) int64 { return f.t }
func f() int64 { return with wire provides Clock { Clock: Fixed{t: 1} } { Clock.Now() } }
func main() {}
`, []string{"S109:5"}},
		{"wire entries", "an entry of two keys, an embedding and a replace", `
type Clock { Now() int64 }
type Timer { Now() int64 }
type Fixed { t int64 }
func Now(f Fixed) int64 { return f.t }
let Base = wire { Clock, Timer: Fixed{t: 1} }
let W = wire { replace Clock: Fixed{t: 2}, Base }
func f() int64 { return with W { Clock.Now() + Timer.Now() } }
func main() {}
`, nil},
		{"with", "with W { … } discharges W's capabilities", `
type Log { Write(s string) }
type Quiet {}
func Write(q Quiet, s string) {}
let W = wire { Log: Quiet{} }
func f() { with W { Log.Write("x") } }
func main() {}
`, nil},
		{"with parentheses", "with (W)", `
type Log { Write(s string) }
type Quiet {}
func Write(q Quiet, s string) {}
let W = wire { Log: Quiet{} }
func f() { with (W) { Log.Write("x") } }
func main() {}
`, nil},
		{"with several wires", "with A, B is 196x's", `
type Log { Write(s string) }
type Quiet {}
func Write(q Quiet, s string) {}
let A = wire { Log: Quiet{} }
let B = wire { Log: Quiet{} }
func f() { with A, B { Log.Write("x") } }
func main() {}
`, []string{"S109:7"}},
		{"record type", "a record and an empty record", `
type P { X int, Y int }
type E {}
func f() P { _ = E{}; return P{X: 1, Y: 2} }
func main() {}
`, nil},
		{"operation type", "an operation type as a capability", `
type Log { Write(s string) }
func f() needs Log { Log.Write("x") }
func main() {}
`, nil},
		{"law", "a law in an operation type", `
type Monoid {
	Combine(a int, b int) int,
	law Assoc(a int, b int, c int) { assert true },
}
func main() {}
`, []string{"S109:4"}},
		{"union", "a union of declared records", `
type A {}
type B {}
type U = A | B
func f(u U) int {
	match u {
	case A:
		return 1
	case B:
		return 2
	}
}
func main() {}
`, nil},
		{"variant in place", "a variant declared in a union", `
type U = Paid{ ID string } | Pending{}
func main() {}
`, []string{"S109:2", "S109:2"}},
		{"machine", "a machine type", `
type Closed {}
type Open {}
type State = Closed | Open
type Trip {}
type Signal = Trip
func step(s State, sig Signal) Decision[State, string] {
	match s {
	case Closed:
		return Enter{Open{}}
	case Open:
		return Done{"open"}
	}
}
type Breaker machine[State, Signal] {
	initial: Closed{},
	step:    step,
}
func main() {}
`, nil},
		{"defined type", "a defined type is 196p's; its uses do not cascade", `
type Celsius float64
type Name string
func f(n int) Name { return "\(n)" }
func g() Celsius { return Celsius(1.5) }
func main() {}
`, []string{"S109:2", "S109:3"}},
		{"type parameters", "bracket kinds by position", `
func pass[A, E, R](f func() A fails E needs R) A fails E needs R { return try f() }
func main() {}
`, nil},
		{"type parameters", "a bracket parameter of two kinds", `
func k[E](a E) fails E { return }
func main() {}
`, []string{"S108:2"}},
		{"ordered constraint", "ordered is 196g's; its body does not cascade", `
func max[T ordered](a T, b T) T {
	if a > b {
		return a
	}
	return b
}
func f() int { return max(1, 2) }
func main() {}
`, []string{"S109:2"}},
		{"fails and needs", "a written row", `
type Bang {}
type Log { Write(s string) }
func f() int fails Bang needs Log {
	Log.Write("x")
	return Bang{}
}
func main() {}
`, nil},
		{"escaping", "an escaping function type is 196e's", `
func f(g escaping func(int) int) int { return g(1) }
func main() {}
`, []string{"S109:2"}},
		{"tuple type", "a tuple result type is 196t's", `
func pair() (int, string) { return pair() }
func main() {}
`, []string{"S109:2"}},
		{"tuple value", "a tuple value is 196t's", `
func f() { _ = (1, "a") }
func main() {}
`, []string{"S109:2"}},
		{"Map type", "append and delete are library calls", `
func f(m Map[string, int64], xs []int64) []int64 {
	m.Delete("a")
	return xs.Append(1).Append(2)
}
func main() {}
`, nil},
		{"Map type", "append checks its elements", `
func f(xs []int64) []int64 { return xs.Append("a") }
func main() {}
`, []string{"S108:2"}},
		{"Map type", "delete checks its key", `
func f(m Map[string, int64]) { m.Delete(1) }
func main() {}
`, []string{"S108:2"}},
		{"Map type", "append is only called", `
func f(xs []int64) { let g = xs.Append }
func main() {}
`, []string{"S104:2"}},
		{"first-parameter function", "x.f(a) reaches f on x's type", `
type Door { open bool }
func knock(d Door, n int) bool { return d.open }
func f(d Door) bool { return d.knock(1) }
func main() {}
`, nil},
		{"extern", "an extern go binding", `
extern go example.com/door.Fail func fail() int
func f() int { return fail() }
func main() {}
`, nil},
		{"test", "test declaration", `
test Adds {
	assert 1 + 1 == 2
}
func main() {}
`, []string{"S109:2"}},
		{"assert", "assert in a body", `
func f() {
	assert true
}
func main() {}
`, []string{"S109:3"}},
		{"if value", "an if in value position is 196r's", `
func f(c bool) int {
	let x = if c { 1 } else { 2 }
	return x
}
func main() {}
`, []string{"S109:3"}},
		{"match value", "a match in value position is 196r's", `
func f(n int) int {
	let x = match n {
	case 1: 10
	case _: 20
	}
	return x
}
func main() {}
`, []string{"S109:3"}},
		{"value match", "a match over literal patterns", `
func f(n int) int {
	match n {
	case 1:
		return 10
	case _:
		return 20
	}
}
func main() {}
`, nil},
		{"type match", "as binds the variant", `
type A { X int }
type B {}
type U = A | B
func f(u U) int {
	match u {
	case A as a:
		return a.X
	case B:
		return 0
	}
}
func main() {}
`, nil},
		{"as pattern", "as under _ binds the subject", `
type A {}
type B {}
type U = A | B
func g(u U) int { return 1 }
func f(u U) int {
	match u {
	case A:
		return 0
	case _ as rest:
		return g(rest)
	}
}
func main() {}
`, nil},
		{"as pattern", "an unread as binding", `
type A {}
type B {}
type U = A | B
func f(u U) int {
	match u {
	case A as a:
		return 1
	case B:
		return 0
	}
}
func main() {}
`, []string{"S110:7"}},
		{"tuple subject", "as in a tuple case", `
type A { X int }
type B {}
type U = A | B
func f(u U, v U) int {
	match (u, v) {
	case (A as a, A):
		return a.X
	case _:
		return 0
	}
}
func main() {}
`, nil},
		{"record pattern", "record pattern", `
type A { X int }
type B {}
type U = A | B
func f(u U) int {
	match u {
	case A{X: x}:
		return x
	case B:
		return 0
	}
}
func main() {}
`, []string{"S109:7"}},
		{"nested as pattern", "as in a case of several patterns", `
type A {}
type B {}
type U = A | B
func f(u U) int {
	match u {
	case A as a, B as b:
		return 0
	}
}
func main() {}
`, []string{"S109:7", "S109:7"}},
		{"catch arm", "catch arm with as", `
type NotFound { ID string }
type Gone {}
func load() int fails NotFound | Gone { return 1 }
func f() string {
	let n = load() catch {
	case NotFound as e:
		return e.ID
	case Gone:
		return "gone"
	}
	return "ok \(n)"
}
func main() {}
`, nil},
		{"catch-all binding", "a catch-all binding", `
type Gone {}
func load() int fails Gone { return 1 }
func f() int {
	return load() catch {
	case err:
		0
	}
}
func main() {}
`, []string{"S109:6"}},
		{"for in", "element and index bindings", `
func f(xs []int64, m Map[string, int64]) int64 {
	let mut s int64 = 0
	for x in xs {
		s += x
	}
	for i, x in xs {
		s += x + int64(i)
	}
	for v in m {
		s += v
	}
	return s
}
func main() {}
`, nil},
		{"for pattern", "a record pattern after for is 196p's; its body is still checked", `
type P { X int, Y int }
func f(ps []P) int {
	let mut s = 0
	for P{X: x, Y: y} in ps {
		s += x + y + undefinedA
	}
	return s
}
func main() {}
`, []string{"S109:5", "S104:6"}},
		{"for tuple pattern", "a tuple pattern after for is 196t's", `
func f(xs []int) int {
	let mut s = 0
	for (a, b) in xs {
		s += a + b
	}
	return s
}
func main() {}
`, []string{"S109:4"}},
		{"counting range", "counting range", `
func sum(n int64) int64 {
	let mut s int64 = 0
	for i in 0..n {
		s += i
	}
	return s
}
func main() {}
`, nil},
		{"counting range", "a range's bounds are integers", `
func f() {
	for i in "a".."b" {
		let _ = i
	}
}
func main() {}
`, []string{"S108:3"}},
		{"range value", "range value", `
func f() {
	let r = 1..2
}
func main() {}
`, []string{"S109:3"}},
		{"for condition", "for with a condition and for alone", `
func f() int {
	let mut n = 0
	for n < 3 {
		n += 1
	}
	for {
		return n
	}
}
func main() {}
`, nil},
		{"assignment", "= and op=", `
func f() int {
	let mut n = 1
	n = 2
	n *= 3
	return n
}
func main() {}
`, nil},
		{"break and continue", "break and continue in a loop", `
func f(xs []int) int {
	let mut s = 0
	for x in xs {
		if x < 0 {
			continue
		}
		if x > 9 {
			break
		}
		s += x
	}
	return s
}
func main() {}
`, nil},
		{"function literal", "a typed function literal", `
func f() int {
	let g = func(a int, b int) int { return a + b }
	return g(1, 2)
}
func main() {}
`, nil},
		{"untyped parameter", "an untyped function literal parameter is #214's", `
func f() { _ = func(x) {} }
func main() {}
`, []string{"S109:2"}},
		{"interpolation", "interpolation is a string", `
func label(n int, ok bool) string { return "n = \(n), ok = \(ok), \("s"), \(7)" }
func main() {}
`, nil},
		{"interpolation", "interpolation reads each value", `
type Bang {}
func g() int fails Bang { return 1 }
func label() string { return "g = \(g())" }
func main() {}
`, []string{"S105:4"}},
		{"interpolation", "interpolation through try", `
type Bang {}
func g() int fails Bang { return 1 }
func label() string fails Bang { return "g = \(try g())" }
func main() {}
`, nil},
		{"float literal", "a float literal is an untyped float constant", `
func f() float64 {
	let x float64 = 1.5
	let y = 2.5e-1 + 1
	return x * y * 2.0
}
func main() {}
`, nil},
		{"float literal", "a float constant an integer cannot hold", `
func f() int { return 1.5 }
func main() {}
`, []string{"S108:2"}},
	}
	covered := map[string]bool{}
	known := map[string]bool{}
	for _, f := range decidedFamilies {
		known[f] = true
	}
	for _, tc := range cases {
		if !known[tc.family] {
			t.Errorf("row %q names the family %q, which decidedFamilies does not list", tc.name, tc.family)
		}
		covered[tc.family] = true
		t.Run(tc.family+"/"+tc.name, func(t *testing.T) {
			_, diags := check.CheckSource(tc.src)
			var got []string
			for _, d := range diags {
				got = append(got, fmt.Sprintf("%s:%d", d.Code, d.Line))
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("got %v, want %v\n%v", got, tc.want, diags)
			}
			// The class H walk visits every node of the decided forms too.
			if p := check.RefWalkProblems(tc.src); len(p) > 0 {
				t.Fatalf("reference walk: %s", strings.Join(p, "; "))
			}
		})
	}
	for _, f := range decidedFamilies {
		if !covered[f] {
			t.Errorf("the decided family %q has no row", f)
		}
	}
}
