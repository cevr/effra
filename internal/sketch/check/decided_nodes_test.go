package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestDecidedNodes: the checker reads the trees the decided parser builds
// (#196). Interpolation, `as` bindings, counting ranges and the implicit
// kinds of bracket parameters are checked; every other decided node it
// does not check yet is S109, never a panic and never silently accepted.
func TestDecidedNodes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // "code:line", sorted as the checker reports them
	}{
		{"an unquoted import path", `
import timeout
func f() timeout.Expired { return timeout.Expired{} }
func main() {}
`, nil},
		{"append and delete are library calls", `
func f(m Map[string, int64], xs []int64) []int64 {
	m.Delete("a")
	return xs.Append(1).Append(2)
}
func main() {}
`, nil},
		{"an untyped function literal parameter is #214's", `
func f() { _ = func(x) {} }
func main() {}
`, []string{"S109:2"}},
		{"an import of a std module lower-v1 brings", `
import task
func main() { task.Go() { 1 } }
`, []string{"S109:2"}},
		{"append checks its elements", `
func f(xs []int64) []int64 { return xs.Append("a") }
func main() {}
`, []string{"S108:2"}},
		{"delete checks its key", `
func f(m Map[string, int64]) { m.Delete(1) }
func main() {}
`, []string{"S108:2"}},
		{"append is only called", `
func f(xs []int64) { let g = xs.Append }
func main() {}
`, []string{"S104:2"}},
		{"interpolation is a string", `
func label(n int, ok bool) string { return "n = \(n), ok = \(ok), \("s"), \(7)" }
func main() {}
`, nil},
		{"interpolation reads each value", `
type Bang {}
func g() int fails Bang { return 1 }
func label() string { return "g = \(g())" }
func main() {}
`, []string{"S105:4"}},
		{"interpolation through try", `
type Bang {}
func g() int fails Bang { return 1 }
func label() string fails Bang { return "g = \(try g())" }
func main() {}
`, nil},
		{"interpolation is not untyped", `
type Name string
func label(n int) Name { return "\(n)" }
func main() {}
`, []string{"S108:3"}},
		{"as binds the variant", `
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
		{"as under _ binds the subject", `
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
		{"an unread as binding", `
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
		{"as in a tuple case", `
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
		{"catch arm with as", `
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
		{"a catch-all binding", `
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
		{"counting range", `
func sum(n int64) int64 {
	let mut s int64 = 0
	for i in 0..n {
		s += i
	}
	return s
}
func main() {}
`, nil},
		{"a range's bounds are integers", `
func f() {
	for i in "a".."b" {
		let _ = i
	}
}
func main() {}
`, []string{"S108:3"}},
		{"bracket kinds by position", `
func pass[A, E, R](f func() A fails E needs R) A fails E needs R { return try f() }
func main() {}
`, nil},
		{"a bracket parameter of two kinds", `
func k[E](a E) fails E { return }
func main() {}
`, []string{"S108:2"}},
		{"test declaration", `
test Adds {
	assert 1 + 1 == 2
}
func main() {}
`, []string{"S109:2"}},
		{"assert in a body", `
func f() {
	assert true
}
func main() {}
`, []string{"S109:3"}},
		{"law", `
type Monoid {
	Combine(a int, b int) int,
	law Assoc(a int, b int, c int) { assert true },
}
func main() {}
`, []string{"S109:4"}},
		{"variant declared in place", `
type U = Paid{ ID string } | Pending{}
func main() {}
`, []string{"S109:2", "S109:2"}},
		{"range value", `
func f() {
	let r = 1..2
}
func main() {}
`, []string{"S109:3"}},
		{"record pattern", `
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
		{"as in a case of several patterns", `
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := check.CheckSource(tc.src)
			var got []string
			for _, d := range diags {
				got = append(got, fmt.Sprintf("%s:%d", d.Code, d.Line))
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("got %v, want %v\n%v", got, tc.want, diags)
			}
		})
	}
}
