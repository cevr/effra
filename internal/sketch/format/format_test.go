package format

import (
	"errors"
	"fmt"
	"runtime"
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
	if msg := contracts(src, out); msg != "" {
		t.Errorf("%s: %s\n--- output\n%s", name, msg, out)
	}
	_, ok1 := sketchtest.Comments(src)
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

// contracts checks what formatting keeps between a parseable source and its
// formatted text, and describes the first broken one: the tree (without
// positions and comments) is the same, and every comment stays between the
// same two tokens, in order.
func contracts(src, out string) string {
	f1, err := syntax.Parse(src)
	if err != nil {
		return "the input does not parse: " + err.Error()
	}
	f2, err := syntax.Parse(out)
	if err != nil {
		return "the output does not parse: " + err.Error()
	}
	if a, b := treeShape(f1), treeShape(f2); a != b {
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		lo := max(0, i-80)
		return fmt.Sprintf("the tree changed at\n  in:  …%s\n  out: …%s", a[lo:min(len(a), i+80)], b[lo:min(len(b), i+80)])
	}
	s1, ok1 := sketchtest.Interleaved(src)
	s2, ok2 := sketchtest.Interleaved(out)
	if ok1 && ok2 {
		if i, a, b := sketchtest.FirstDiff(s1, s2); i >= 0 {
			return fmt.Sprintf("token or comment %d changed: %q became %q", i+1, a, b)
		}
	}
	return ""
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
	// The layout is pinned: each comment stays between the same two tokens
	// (`A /* x */ | B` keeps its comment before the `|`), an own-line
	// comment takes its block's indentation, and a comment between a
	// clause's last statement and the next case belongs to the clause
	// above unless a blank line separates them.
	want := strings.Replace(strings.Replace(src,
		"\t\ta()\n\t// still A\n", "\t\ta()\n\t\t// still A\n", 1),
		"\tcase B:\n\t// still B, empty\n", "\tcase B:\n\t\t// still B, empty\n", 1)
	if out := check(t, "comments", src, 8); out != want {
		t.Errorf("layout at line %d\n--- got\n%s\n--- want\n%s", firstDiffLine(out, want), out, want)
	}
}

// TestOutputBound (review M2): one long field name among many short ones
// starts its own alignment section, so the output stays near the input's
// size; and a bound below the output is refused before the text is built,
// so the allocation stays near the input's size too.
func TestOutputBound(t *testing.T) {
	src := "package p\n\ntype Big struct {\n\t" + strings.Repeat("L", 4096) + " int64\n" + strings.Repeat("\tx int64\n", 2000) + "}\n"
	out, err := SourceBounded(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > len(src)+len(src)/10 {
		t.Fatalf("output %d bytes for a %d-byte input: the long name pads its neighbours", len(out), len(src))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = SourceBounded(src, 1024)
	runtime.ReadMemStats(&after)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Limit != 1024 {
		t.Fatalf("bound 1024 gave %v", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 200*uint64(len(src)) {
		t.Fatalf("a refused %d-byte input allocated %d bytes", len(src), allocated)
	}
	// An exact bound is not a refusal.
	if exact, err := SourceBounded(src, len(out)); err != nil || exact != out {
		t.Fatalf("bound equal to the output: %v", err)
	}
	// Aligned columns of similar widths still align (gofmt).
	got, _ := Source("package p\n\ntype T struct {\n\tshort int\n\tsomewhatLongerName string\n}\n")
	if want := "package p\n\ntype T struct {\n\tshort              int\n\tsomewhatLongerName string\n}\n"; got != want {
		t.Fatalf("alignment\n%s", got)
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
			// Review M1: a comment inside a delimiter stays before its
			// closer, so no semicolon is inserted after the closer.
			"comments stay inside delimiters",
			"package p\nvar xs [ // c\n]int\nvar ys [ /* c\n d */ ]int\nvar m map[ /* k */ string /* v */ ]int\nvar s = a /* x */ .b\nvar c <- /* y */ chan int\n",
			"package p\nvar xs [ // c\n]int\nvar ys [ /* c\n d */ ]int\nvar m map[ /* k */ string /* v */]int\nvar s = a /* x */ .b\nvar c <- /* y */ chan int\n",
		},
		{
			// go/printer: no blank between a block comment and `,`, nor `)`
			// unless the comment follows the opener.
			"block comment before a comma or closer",
			"package p\nvar x = f(a /* x */ , b)\nvar y = f( /* y */ )\nvar z = f(a /* z */ )\n",
			"package p\nvar x = f(a /* x */, b)\nvar y = f( /* y */ )\nvar z = f(a /* z */)\n",
		},
		{
			"grouped names keep their line breaks",
			"package p\nfunc f(a,\nb int) {}\n\ntype T struct {\n\tx,\n\ty int\n}\n",
			"package p\nfunc f(a,\n\tb int) {}\n\ntype T struct {\n\tx,\n\t\ty int\n}\n",
		},
		{
			// Review M9: the explicit empty statement keeps the label; the
			// loop after it stays unlabelled.
			"labelled empty statement",
			"package p\nfunc f() { L:; for {break L} }\n",
			"package p\nfunc f() {\nL:\n\t;\n\tfor {\n\t\tbreak L\n\t}\n}\n",
		},
		{
			// Review M7: gofmt writes an inserted header semicolon as `; `.
			"header semicolons at line breaks",
			"package p\nfunc f() { if x := g()\nx {}; switch g()\n{default:}; for i := 0\ni < 3\ni++ {} }\n",
			"package p\nfunc f() {\n\tif x := g(); x {\n\t}\n\tswitch g(); {\n\tdefault:\n\t}\n\tfor i := 0; i < 3; i++ {\n\t}\n}\n",
		},
		{
			"parenthesised receives and inferred array length",
			"package p\nfunc f() { select { case (<-ch): case x = (<-ch): } }\nvar x = [...]int{1, 2}\n",
			"package p\nfunc f() {\n\tselect {\n\tcase (<-ch):\n\tcase x = (<-ch):\n\t}\n}\nvar x = [...]int{1, 2}\n",
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
