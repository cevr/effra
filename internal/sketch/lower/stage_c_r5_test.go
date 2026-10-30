package lower

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage C round 4 review T1: forms the corpus guard found that checked
// clean and failed lowering with an internal compiler error or an
// ownerless refusal. Each now runs.
func TestErgoRunCorpusGuardForms(t *testing.T) {
	wantRun(t, `type S {}
type P {}
type J { M() int }
type A[T] { M() T }

func M(_ S) int { return 1 }
func M(_ *P) int { return 7 }
func FnI[T](x A[T]) T { return x.M() }
func use(j J) int { j.M() }

func unnamed(S, int) int { 2 }
func apply(f func(int, int) int) int { f(1, 2) }

func main() needs Console {
	let s = S
	let mut p = P
	let q = &p
	if unnamed(s, 1) == 2 && apply(func(a int, b int) int { b }) == 2 {
		Console.Println("unnamed parameters")
	}
	if use(q) == 7 {
		Console.Println("pointer source")
	}
	if FnI(s) == 1 && FnI(q) == 7 {
		Console.Println("generic operation type")
	}
}
`, "unnamed parameters\npointer source\ngeneric operation type\n")
}

// The forms of a later stage the guard met refuse at their site, with the
// stage that owns them, never an internal compiler error.
func TestCorpusGuardDeferredRefusals(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"generic-dot-call", "type G[T] { v T }\nfunc pick[T, U](g G[T], u U) U { u }\nfunc f(g G[int]) string { g.pick(\"x\") }\n", "a dot call to the generic function pick is lowered after check-v1 stage D (D-D8)"},
		{"parenthesised-instance", "func Fn[T](x T) T { x }\nfunc f() int { ((Fn[int]))(1) }\n", "an explicit instantiation used as a value is lowered after check-v1 stage E"},
		{"untyped-let", "let pattern\n", "a let with neither a type nor a value is decided in check-v1 stage D (D-D1)"},
		{"untyped-local-let", "func f() {\n\tlet x\n\t_ = x\n}\n", "a let with neither a type nor a value is decided in check-v1 stage D (D-D1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := lowerSource("r", tc.src, false)
			if len(o.diags) != 0 || o.err != nil || o.panicked != nil {
				t.Fatalf("want a clean check and a refusal; got diags %v, err %v, panic %v", o.diags, o.err, o.panicked)
			}
			if len(o.refused) != 1 || o.refused[0].Message != tc.want || undeferred(o.refused[0].Message) != "" {
				t.Fatalf("want the refusal %q, a documented deferral; got %v", tc.want, o.refused)
			}
		})
	}
}

// Round 4 review T2: the corpus reader enumerates every declaration form
// of a check test: a package-level const or var, a case table, a struct
// field, a call argument, a := in a function, and a literal after a
// named prefix.
func TestCheckCorpusEnumeration(t *testing.T) {
	dir := t.TempDir()
	src := "package check_test\n\n" +
		"const pkgConst = `func A() {}`\n" +
		"var pkgVar = \"func B() {}\"\n" +
		"const decls = \"type D {}\\n\"\n" +
		"var table = []struct{ src string }{{\"func C() {}\"}}\n" +
		"func TestX(t *testing.T) {\n" +
		"\tlocal := \"func E() {}\"\n" +
		"\tcheck(decls + \"func F(d D) {}\")\n" +
		"\t_ = local\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, files := checkCorpus(t, dir)
	if files != 1 {
		t.Fatalf("read %d files", files)
	}
	have := map[string]bool{}
	for _, s := range got {
		have[s] = true
	}
	for _, want := range []string{"func A() {}\n", "func B() {}\n", "type D {}\n", "func C() {}\n", "func E() {}\n", "type D {}\nfunc F(d D) {}\n"} {
		if !have[want] {
			t.Errorf("the corpus misses %q; it has %q", want, strings.Join(got, "|"))
		}
	}
}
