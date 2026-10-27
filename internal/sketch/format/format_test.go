package format

import (
	"errors"
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/sketchtest"
	"effra.local/prototype/internal/sketch/syntax"
)

// check asserts the formatter's properties on one parseable input: the
// output parses, formatting it again changes nothing, comments and tokens
// are kept (modulo semicolons and a comma before a closer), and whitespace
// perturbations format to the same text. It returns the formatted text.
func check(t *testing.T, name, src string, perturbations uint64) string {
	t.Helper()
	out, err := Source(src)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return ""
	}
	again, err := Source(out)
	if err != nil {
		t.Errorf("%s: the output does not parse: %v\n%s", name, err, out)
		return out
	}
	if again != out {
		t.Errorf("%s: not idempotent at line %d\n--- first\n%s\n--- second\n%s", name, firstDiffLine(out, again), out, again)
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") || strings.HasPrefix(out, "\n") {
		t.Errorf("%s: output does not end with exactly one newline or starts with a blank line", name)
	}
	for i, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, " \t") != line && !strings.Contains(line, "`") {
			t.Errorf("%s: trailing blanks on output line %d: %q", name, i+1, line)
			break
		}
	}
	c1, ok1 := sketchtest.Comments(src)
	c2, ok2 := sketchtest.Comments(out)
	if ok1 && ok2 {
		if i, a, b := sketchtest.FirstDiff(c1, c2); i >= 0 {
			t.Errorf("%s: comment %d changed: %q became %q", name, i+1, a, b)
		}
		t1, _ := sketchtest.Tokens(src)
		t2, _ := sketchtest.Tokens(out)
		if i, a, b := sketchtest.FirstDiff(t1, t2); i >= 0 {
			t.Errorf("%s: token %d changed: %q became %q", name, i+1, a, b)
		}
	}
	for seed := uint64(1); seed <= perturbations && ok1; seed++ {
		p, _ := sketchtest.Perturb(src, seed)
		got, err := Source(p)
		if err != nil {
			t.Errorf("%s: perturbation %d does not parse: %v", name, seed, err)
			break
		}
		if got != out {
			t.Errorf("%s: perturbation %d formats differently at line %d\n--- want\n%s\n--- got\n%s", name, seed, firstDiffLine(out, got), out, got)
			break
		}
	}
	return out
}

func firstDiffLine(a, b string) int {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		if i >= len(la) || i >= len(lb) || la[i] != lb[i] {
			return i + 1
		}
	}
	return 0
}

func TestPositiveInputs(t *testing.T) {
	inputs, err := sketchtest.Positive()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		check(t, in.Name, in.Src, 5)
	}
}

func TestSupersededInputs(t *testing.T) {
	_, superseded, err := sketchtest.Corpus()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range superseded {
		if _, err := syntax.Parse(in.Src); err == nil {
			check(t, in.Name, in.Src, 2)
		}
	}
}

// TestCommentPositions: comments in every position keep their order, and
// the layout is idempotent and canonical (spec risk 4).
func TestCommentPositions(t *testing.T) {
	src := `// file doc
package p // after the clause

/* block
   comment */
import (
	// before a spec
	"a" // after a spec
	/* inline */ "b"
	// before the paren
)

type T struct { // after the brace
	// before a field
	a int /* mid */ // end
	b string
	// before the brace
}

type U interface{ A /* x */ | B }

// doc
//ef:go net/url.Parse
func parse(raw string) (url.URL, error[go.Error /* row */ | E]) uses go.Foreign

func f(a /* first */ int, // after a
	b int, /* after b */
) ( /* results */ int) {
	x := g( // after the paren
		1, /* one */
		// own line in the arguments
		2,
	)
	if x { // after if
		y()
	} /* between */ else { // after else
		z()
	}
	switch v := x.(type) { // after switch
	// before the first case
	case A:
		a()
	// still A

	// about B
	case B:
	// still B, empty
	default:
		d()
		// end of default

	// before the closing brace
	}
	r := try h() catch e {
	case E1: // after the case
		log(e)
		// end of the arm
	}
	w := wire{ /* only */ A = X{} }
	return 1 /* after the result */
	// end of the body
}
// end of file
`
	out := check(t, "comments", src, 8)
	if testing.Verbose() {
		t.Log("\n" + out)
	}
}

func TestMalformedIsRefused(t *testing.T) {
	for _, m := range sketchtest.MalformedTable {
		src := m.Src
		if m.Generate != nil {
			src = m.Generate()
		}
		out, err := Source(src)
		var e *syntax.Error
		if out != "" || !errors.As(err, &e) || e.Code != syntax.CodeSyntax {
			t.Errorf("%s: got %q, %v; want no text and S001", m.Name, out, err)
		}
	}
}

// TestLayout pins the canonical layout of constructs and comment positions.
func TestLayout(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"gofmt layout",
			"package p\n\n\n\nimport (\n\"a\"; . \"b\"\n  go \"c\")\nfunc f( a,b int,\n) (int, error[A|B]) uses X,Y { return a*b+1 }\n",
			"package p\n\nimport (\n\t\"a\"\n\t. \"b\"\n\tgo \"c\"\n)\nfunc f(a, b int,\n) (int, error[A | B]) uses X, Y { return a*b + 1 }\n",
		},
		{
			"explicit semicolons become line breaks",
			"package p\n\ntype P struct{ A string; B string }\n\nfunc main() { with(wire{ A = X{}; B = Y{} }) { f() } }\n",
			"package p\n\ntype P struct {\n\tA string\n\tB string\n}\n\nfunc main() {\n\twith(wire{\n\t\tA = X{}\n\t\tB = Y{}\n\t}) { f() }\n}\n",
		},
		{
			"alignment",
			"package p\n\ntype T struct {\n\tn int64 // count\n\tlonger string\n}\n\nvar (\n\tfirst = \"a\"\n\tsecondName = \"b\"\n)\n\nwire W {\n\tA = X{}\n\tLonger = Y{}\n}\n\ntype M machine[S, E] {\n\tinitial Idle{}\n\tstep f\n}\n\nvar routes = R{\n\t\"a\": x,\n\t\"long\": y,\n}\n",
			"package p\n\ntype T struct {\n\tn      int64 // count\n\tlonger string\n}\n\nvar (\n\tfirst      = \"a\"\n\tsecondName = \"b\"\n)\n\nwire W {\n\tA      = X{}\n\tLonger = Y{}\n}\n\ntype M machine[S, E] {\n\tinitial Idle{}\n\tstep    f\n}\n\nvar routes = R{\n\t\"a\":    x,\n\t\"long\": y,\n}\n",
		},
		{
			"comments take the block's indentation",
			"package p\n\nfunc f() {\n        x()\n  // before the brace\n}\n",
			"package p\n\nfunc f() {\n\tx()\n\t// before the brace\n}\n",
		},
		{
			"comment before case: blank line decides the clause",
			"package p\n\nfunc f(v V) {\n\tswitch v {\n\tcase 1:\n\t\ta()\n\t// still case 1\n\n\t// about case 2\n\tcase 2:\n\t\tb()\n\t\t// end of case 2\n\t}\n}\n",
			"package p\n\nfunc f(v V) {\n\tswitch v {\n\tcase 1:\n\t\ta()\n\t\t// still case 1\n\n\t// about case 2\n\tcase 2:\n\t\tb()\n\t\t// end of case 2\n\t}\n}\n",
		},
		{
			"catch arms are clauses",
			"package p\n\nfunc f() {\n\tr := g() catch e {\n\t\tcase A: log(e)\n\t\t\tStatus(1)\n\t}\n}\n",
			"package p\n\nfunc f() {\n\tr := g() catch e {\n\tcase A:\n\t\tlog(e)\n\t\tStatus(1)\n\t}\n}\n",
		},
		{
			"statement blocks are multi-line, function bodies keep one line",
			"package p\n\nfunc f() { if x { return } }\nfunc g() {}\n",
			"package p\n\nfunc f() {\n\tif x {\n\t\treturn\n\t}\n}\nfunc g() {}\n",
		},
		{
			"continuation lines indent once",
			"package p\n\nfunc f() {\n\tx := a &&\nb ||\nc\n\tg(1,\n2)\n}\n",
			"package p\n\nfunc f() {\n\tx := a &&\n\t\tb ||\n\t\tc\n\tg(1,\n\t\t2)\n}\n",
		},
		{
			"labels outdent, raw strings stay",
			"package p\n\nfunc f() {\nouter:\n\tfor {\n\t\tbreak outer\n\t}\n\ts := `a\n  b`\n}\n",
			"package p\n\nfunc f() {\nouter:\n\tfor {\n\t\tbreak outer\n\t}\n\ts := `a\n  b`\n}\n",
		},
		{
			"CRLF input gives LF output",
			"package p\r\n\r\n// c\r\nfunc f() {}\r\n",
			"package p\n\n// c\nfunc f() {}\n",
		},
		{
			"directive stays attached",
			"package p\n\n//ef:go net/url.Parse\nfunc parse(raw string) (url.URL, error[go.Error]) uses go.Foreign\n",
			"package p\n\n//ef:go net/url.Parse\nfunc parse(raw string) (url.URL, error[go.Error]) uses go.Foreign\n",
		},
		{
			"unary operators never merge",
			"package p\n\nvar x = - -y\nvar z = a - -b\n",
			"package p\n\nvar x = - -y\nvar z = a - -b\n",
		},
	}
	for _, c := range cases {
		got := check(t, c.name, c.src, 3)
		if got != c.want {
			t.Errorf("%s:\n--- got\n%s--- want\n%s", c.name, got, c.want)
		}
	}
}
