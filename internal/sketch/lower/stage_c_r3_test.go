package lower

import (
	"strings"
	"testing"
)

// Stage C round 2 review witnesses, run through the public CLI.

// M1, M2: a program that checks clean never panics with "comparing
// uncomparable type". Each route to equality over a union whose variant
// does not compare, or over a row value, is refused before Go; equality
// over a union whose variants all compare runs.
func TestErgoRunComparability(t *testing.T) {
	const decls = `type E { N []int }
type F {}
type V = E | F
func g() int fails E { E{N: []int{1}} }
func eq[T comparable](a T, b T) bool { a == b }
`
	for _, tc := range []struct{ name, src string }{
		{"row-tuple", `func f() int { g() catch { case err: if (err, 1) == (err, 1) { 1 } else { 0 } } }`},
		{"row-generic", `func f() int { g() catch { case err: if eq(err, err) { 1 } else { 0 } } }`},
		{"row-assigned-union", "func f() int {\n\tg() catch {\n\tcase err:\n\t\tlet v V = err\n\t\tif v == v { 1 } else { 0 }\n\t}\n}"},
		{"declared-union", "func same(a V, b V) bool { a == b }\nfunc f() int { if same(E{N: []int{1}}, F) { 1 } else { 0 } }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := decls + tc.src + "\nfunc main() needs Console { if f() == 1 { Console.Println(\"equal\") } }\n"
			out, err := ergoRun(t, src)
			if err == nil || !strings.Contains(out, "S108") || strings.Contains(out, "uncomparable") {
				t.Fatalf("want an S108 refusal; got %v\n%s", err, out)
			}
		})
	}
	wantRun(t, `type C { N int }
type F {}
type U = C | F
func eq[T comparable](a T, b T) bool { a == b }
func main() needs Console {
	let a U = C{N: 1}
	let b U = C{N: 1}
	let c U = F
	let m = Map[U, int]{a: 1}
	if a == b && eq(a, b) && (a, 1) == (b, 1) && a != c && len(m) == 1 {
		Console.Println("comparable union")
	}
}
`, "comparable union\n")
}
