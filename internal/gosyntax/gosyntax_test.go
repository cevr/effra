package gosyntax

import "testing"

func tree(t *testing.T, src string) string {
	t.Helper()
	printed, err := Tree([]byte("package p\n\n" + src + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return printed
}

// Programs that differ in meaning compare unequal, including syntax go/ast
// keeps only as a position; comments and layout do not count.
func TestTreeComparesProgramsNotLayout(t *testing.T) {
	for _, pair := range []struct{ name, a, b string }{
		{"switch init statement", "func f() bool { return false }\nfunc g() { switch f(); { case true: } }", "func f() bool { return false }\nfunc g() { switch f() { case true: } }"},
		{"type alias", "type T = int", "type T int"},
		{"call-site ellipsis", "func f(xs ...int) {}\nfunc g(xs []int) { f(xs...) }", "func f(xs ...int) {}\nfunc g(xs []int) { f(xs) }"},
	} {
		if tree(t, pair.a) == tree(t, pair.b) {
			t.Errorf("%s: %q and %q compare equal", pair.name, pair.a, pair.b)
		}
	}
	plain := tree(t, "func f() bool { return false }\nfunc g() { switch f(); { case true: } }\ntype T = int\nfunc h(xs ...int) { h(xs...) }")
	commented := tree(t, "func f() bool {\n//line a.ef:3\n\treturn false\n}\n\nfunc g() { switch f(); {\n//line a.ef:9\ncase true:\n} }\n\n// T is an alias.\ntype T = int\n\nfunc h(xs ...int) {\n\th(\n\t\txs...,\n\t)\n}")
	if plain != commented {
		t.Error("comments and layout change the compared program")
	}
}
