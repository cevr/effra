package format

import (
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/sketchtest"
	"effra.local/prototype/internal/sketch/syntax"
)

// nestedChains are small programs that nest every construct with
// punctuation the tree does not record as a position (commas, `|`, the `]`
// of an array type, the `[` and `]` of a map type, the `.` of a selector,
// `chan`, slice colons) inside chains of selectors, calls, indexes and
// literals.
var nestedChains = []struct{ name, src string }{
	{"selector call index chain", "package p\n\nfunc f() {\n\tx := a.b(c, d)[e].g(h...)[i:j].k[l:m:n]\n}\n"},
	{"array, map and chan types", "package p\n\nvar xs [N][]map[string][2]chan<- <-chan int\n\nvar ys = [...]int{1, 2}\n"},
	{"nested literals", "package p\n\nvar m = map[string][]T{\"a\": {{X: 1, Y: []int{2, 3}}, {X: 4}}, \"b\": nil}\n"},
	{"func types and results", "package p\n\nfunc f(a, b int, g func(x, y int) (int, error[E | go.Error]) needs (A, B)) (r int, e error[E]) needs C, D {\n\treturn g(a, b)\n}\n"},
	{"union and error rows", "package p\n\ntype U interface{ A | b.B | C[T, U] }\n\nfunc f() error[A | b.B] { return nil }\n"},
	{"type params and instantiation", "package p\n\ntype Pair[K comparable, V any] struct {\n\tk, l K\n\tv    V\n}\n\nvar p = Pair[string, int]{k: \"a\", v: 1}\n"},
	{"catch and type switch tuples", "package p\n\nfunc f() {\n\tr := try g(a, b) catch e {\n\tcase A, b.B:\n\t\tlog(e)\n\t}\n\tswitch x, y := r.(type), s.(type) {\n\tcase (A, _), (B, C):\n\tdefault:\n\t}\n}\n"},
	{"statements", "package p\n\nfunc f() {\nL:\n\tfor i := 0; i < n; i++ {\n\t\tif a, b := g(); a {\n\t\t\tcontinue L\n\t\t} else if b {\n\t\t\tbreak\n\t\t}\n\t}\n\tfor k, v := range m {\n\t\tch <- k\n\t\tx += v\n\t}\n\tselect {\n\tcase v, ok := <-ch:\n\t\t_ = v\n\tcase (<-done):\n\t}\n\tdefer close(ch)\n\tgo f(x)\n}\n"},
	{"wire and machine", "package p\n\nwire W provides A, B needs C {\n\tC, D = X{}\n\treplace e.E = Y{a, b}\n\tShared\n}\n\ntype M machine[S, E] {\n\tinitial Idle{}\n\tstep f\n}\n"},
	{"imports and vars", "package p\n\nimport (\n\t\"a\"\n\tb \"c\"\n\t. \"d\"\n\tgo \"net/url\"\n)\n\nvar (\n\tx, y = 1, 2\n\tz    = f(x, y)\n)\n"},
}

// TestCommentsAtEveryBoundary inserts each comment kind before every token
// of the nested chains, the grammar fixtures and the spec fences, and of
// every seventh token of the corpus, and checks each admitted variant: the
// output parses, keeps the tree and the place of every comment, and
// formats to itself (spec risk 4, review T1).
func TestCommentsAtEveryBoundary(t *testing.T) {
	type input struct {
		name, src string
		stride    int
	}
	var inputs []input
	for _, c := range nestedChains {
		if _, err := syntax.Parse(c.src); err != nil {
			t.Fatalf("%s does not parse: %v", c.name, err)
		}
		inputs = append(inputs, input{c.name, c.src, 1})
	}
	for _, g := range sketchtest.GrammarFixtures {
		inputs = append(inputs, input{g.Name, g.Src, 1})
	}
	fences, err := sketchtest.SpecFences()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fences {
		inputs = append(inputs, input{f.Name, f.Src, 1})
	}
	v0, _, err := sketchtest.Corpus()
	if err != nil {
		t.Fatal(err)
	}
	stride := 7
	if testing.Short() {
		stride = 31
	}
	for _, c := range v0 {
		inputs = append(inputs, input{c.Name, c.Src, stride})
	}
	checked, failed := 0, 0
	for _, in := range inputs {
		if _, err := syntax.Parse(in.src); err != nil {
			continue
		}
		for i, at := range sketchtest.Boundaries(in.src) {
			if i%in.stride != 0 {
				continue
			}
			for _, kind := range sketchtest.CommentKinds {
				src := in.src[:at] + kind + in.src[at:]
				if _, err := syntax.Parse(src); err != nil {
					continue
				}
				checked++
				out, err := Source(src)
				msg := ""
				switch {
				case err != nil:
					msg = "parsed but did not format: " + err.Error()
				default:
					if msg = contracts(src, out); msg == "" {
						if again, _ := Source(out); again != out {
							msg = "not idempotent"
						}
					}
				}
				if msg != "" {
					failed++
					if failed <= 10 {
						t.Errorf("%s, comment %q before offset %d: %s\n--- input\n%s\n--- output\n%s", in.name, strings.TrimSpace(kind), at, msg, src, out)
					}
				}
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d variants failed", failed, checked)
	}
	t.Logf("%d variants checked", checked)
}
