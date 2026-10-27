package syntax

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/sketchtest"
)

// TestMalformed: every malformed input is a located S001 on its line, never
// a passthrough.
func TestMalformed(t *testing.T) {
	for _, m := range sketchtest.MalformedTable {
		t.Run(m.Name, func(t *testing.T) {
			src := m.Src
			if m.Generate != nil {
				src = m.Generate()
			}
			file, err := Parse(src)
			if file != nil || err == nil {
				t.Fatal("parsed")
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("%T is not *Error", err)
			}
			if e.Code != CodeSyntax || !slices.Contains(m.Lines, e.Line) {
				t.Fatalf("%v: want S001 on line %v", e, m.Lines)
			}
			if e.Offset < 0 || e.Offset+e.Length > len(src) {
				t.Fatalf("span %d+%d outside the %d-byte source", e.Offset, e.Length, len(src))
			}
			line := strings.Count(src[:e.Offset], "\n") + 1
			column := e.Offset - (strings.LastIndexByte(src[:e.Offset], '\n') + 1) + 1
			if line != e.Line || column != e.Column {
				t.Fatalf("line %d column %d, want %d and %d from the offset", e.Line, e.Column, line, column)
			}
		})
	}
}

// TestUsesIsAnIdentifier (#217): `uses` is not a keyword and has no
// compatibility path (owner, 2026-10-27: "We don't need to give backwards
// compatible syntax parsing. This is still a prototype"). It is an ordinary
// name, so the old spelling of a needs clause gets the plain S001 any stray
// identifier gets, never a dedicated hint.
func TestUsesIsAnIdentifier(t *testing.T) {
	for _, c := range []struct {
		src, message string
		line, column int
	}{
		// `uses` parses as the result type, so the error is at A.
		{"package p\nfunc f() uses A {}\n", `expected end of declaration, found "A"`, 2, 15},
		{"package p\nwire W uses A {\n}\n", `expected "{", found "uses"`, 2, 8},
	} {
		_, err := Parse(c.src)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || e.Message != c.message || e.Line != c.line || e.Column != c.column {
			t.Errorf("%q: got %v, want %d:%d S001 %s", c.src, err, c.line, c.column, c.message)
		}
	}
	if _, err := Parse("package p\n\nvar uses = 1\n"); err != nil {
		t.Fatalf("uses is not a name: %v", err)
	}
	// needs and provides are keywords, so neither is a name.
	for _, word := range []string{"needs", "provides"} {
		if _, err := Parse("package p\n\nvar " + word + " = 1\n"); err == nil {
			t.Fatalf("%s is a name", word)
		}
	}
}

// TestRefusedForms: Go forms outside the language, and ambiguities that
// must resolve one way, each give S001 with a message naming the problem.
func TestRefusedForms(t *testing.T) {
	cases := []struct{ name, src, message string }{
		{"type alias", "package p\ntype A = B\n", "type aliases"},
		{"defined type", "package p\ntype A B\n", "struct, an interface or a machine"},
		{"slice type declaration", "package p\ntype A []T\n", "type parameter"},
		{"array type declaration", "package p\ntype A [4]int\n", "type"},
		{"method type parameters", "package p\nfunc (r R) M[T any]() {}\n", "method cannot have type parameters"},
		{"type assertion", "package p\nfunc f() {\n\tx := v.(T)\n}\n", "type assertions"},
		{"guard in an argument", "package p\nfunc f() {\n\tswitch g(v.(type)) {\n\t}\n}\n", "subject of a type switch"},
		{"guard in init", "package p\nfunc f() {\n\tswitch x := v.(type); x {\n\t}\n}\n", "subject of a type switch"},
		{"multi-subject without bindings", "package p\nfunc f() {\n\tswitch a.(type), b.(type) {\n\t}\n}\n", "expected"},
		{"tuple arity", "package p\nfunc f() {\n\tswitch x, y := a.(type), b.(type) {\n\tcase (A, B, C):\n\t}\n}\n", "3 types for 2"},
		{"single case in multi switch", "package p\nfunc f() {\n\tswitch x, y := a.(type), b.(type) {\n\tcase A:\n\t}\n}\n", "tuple"},
		{"local type", "package p\nfunc f() {\n\ttype T struct{}\n}\n", "package level"},
		{"catch default", "package p\nfunc f() {\n\tx := g() catch {\n\tdefault:\n\t}\n}\n", "default is not allowed"},
		{"error row as a parameter", "package p\nfunc f(e error[E]) {}\n", "result list"},
		{"paren needs on a declaration", "package p\nfunc f() needs (A, B) {}\n", "inside a list"},
		{"unparenthesised needs in a list", "package p\nfunc f(g func() needs A, B) {}\n", "parameter type"},
		{"variadic result", "package p\nfunc f() (...int) {}\n", "variadic"},
		{"variadic not last", "package p\nfunc f(a ...int, b int) {}\n", "variadic"},
		{"if header newline", "package p\nfunc f() {\n\tif x\n\t{\n\t}\n}\n", "newline"},
		{"trailing block in if header", "package p\nfunc f() {\n\tif g() { h() } {\n\t}\n}\n", "end of statement"},
		{"duplicate machine entry", "package p\ntype M machine[S, E] {\n\tinitial A{}\n\tinitial B{}\n\tstep f\n}\n", "duplicate"},
		{"missing step", "package p\ntype M machine[S, E] {\n\tinitial A{}\n}\n", "step entry"},
		{"union and method", "package p\ntype U interface{ A | B; M() }\n", "one list of types"},
		{"tilde constraint", "package p\nfunc f[T ~int]() {}\n", "unexpected character"},
		{"else without block", "package p\nfunc f() {\n\tif x {\n\t} else g()\n}\n", "else must be followed"},
		{"go keyword name", "package p\nfunc f() {\n\tgo := 1\n}\n", "expected an expression"},
		{"machine keyword name", "package p\nfunc f() {\n\tmachine := 1\n}\n", "expected an expression"},
		{"embedded struct field", "package p\ntype S struct {\n\tT\n}\n", "name and a type"},
		{"select case", "package p\nfunc f() {\n\tselect {\n\tcase x:\n\t}\n}\n", "send or a receive"},
		{"empty type arguments", "package p\nvar x T[]\n", "type argument"},
		{"three-index slice without max", "package p\nvar x = s[1:2:]\n", "final index"},
		// Review M4: Go's IdentifierList ":=" holds in a range clause too.
		{"range define of a selector", "package p\nfunc f() { for a.x := range xs {} }\n", "non-name on the left side of :="},
		{"range define of a parenthesised name", "package p\nfunc f() { for (i) := range xs {} }\n", "non-name on the left side of :="},
		// Review M5: error rows, catch arms and unions name types.
		{"error row of a slice type", "package p\nfunc f() error[[]int | *E] {}\n", "an error row names a type"},
		{"error row of a pointer", "package p\nfunc f() error[E | *F] {}\n", "an error row names a type"},
		{"union of composite types", "package p\ntype U interface { []int | map[string]int }\n", "a union variant names a type"},
		{"union variant pointer", "package p\ntype U interface { A | *B }\n", "a union variant names a type"},
		{"catch arm of a slice type", "package p\nfunc f() { g() catch { case []int: 1 } }\n", "a catch arm names a type"},
		// Review M6: [...] only as a composite literal's type.
		{"inferred length as a parameter type", "package p\nfunc f(a [...]int) {}\n", "expected a type"},
		{"inferred length without a literal", "package p\nvar x = [...]int\n", "array composite literal only"},
		// Review M7: an inserted semicolon before `{` leaves no condition.
		{"if condition then newline", "package p\nfunc f() {\n\tif x := g(); x\n\t{\n\t}\n}\n", "newline"},
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || !strings.Contains(e.Message, c.message) {
			t.Errorf("%s: got %v, want S001 with %q", c.name, err, c.message)
		}
	}
}

// TestAdmittedForms: forms that parse, with the shape the checker needs.
func TestAdmittedForms(t *testing.T) {
	parse := func(src string) *File {
		t.Helper()
		f, err := Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return f
	}
	body := func(src string) []Stmt {
		t.Helper()
		f := parse("package p\nfunc f() {\n" + src + "\n}\n")
		return f.Decls[0].(*FuncDecl).Body.List
	}

	// A composite literal in an if header needs parentheses (Go).
	if s := body("if x == (T{}) {\n}")[0].(*IfStmt); s.Body == nil {
		t.Fatal("no body")
	}
	// A trailing block after a call in a statement.
	call := body("with(Test) {\n\tserve()\n}")[0].(*ExprStmt).X.(*CallExpr)
	if call.Trailing == nil || len(call.Trailing.List) != 1 {
		t.Fatal("no trailing block")
	}
	// try in operand position extends to the right over the binary rest.
	ret := body("return a + try b * c")[0].(*ReturnStmt)
	sum := ret.Results[0].(*BinaryExpr)
	if try, ok := sum.Y.(*TryExpr); !ok || try.X.(*BinaryExpr).Op != MUL {
		t.Fatalf("try operand %#v", sum.Y)
	}
	// try … catch is one partial catch.
	def := body("x := try f() catch {\ncase A:\n\t0\n}")[0].(*AssignStmt)
	if _, ok := def.Rhs[0].(*TryExpr).X.(*CatchExpr); !ok {
		t.Fatal("try does not hold the catch")
	}
	// catch e with statement arms.
	c := body("r := f() catch e {\ncase A:\n\tlog(e)\n\treturn 1\n}")[0].(*AssignStmt).Rhs[0].(*CatchExpr)
	if c.Bind.Name != "e" || len(c.Arms[0].Body) != 2 {
		t.Fatalf("catch %#v", c)
	}
	// case (x): in an expression switch is an expression.
	sw := body("switch v {\ncase (x):\n}")[0].(*SwitchStmt)
	if _, ok := sw.Clauses[0].List[0].(*ParenExpr); !ok {
		t.Fatal("not a parenthesised expression")
	}
	// Multi-subject type switch with tuples, _ and default.
	ts := body("switch a, b := x.(type), y.(type) {\ncase (A, _), (B, C):\ndefault:\n}")[0].(*TypeSwitchStmt)
	if len(ts.Binds) != 2 || len(ts.Clauses[0].List) != 2 || ts.Clauses[1].List != nil {
		t.Fatalf("type switch %#v", ts)
	}
	// go as statement, block, qualifier.
	stmts := body("go f(x)\nt := go {\n\twork()\n}\ne := go.Error{}")
	if g := stmts[0].(*ExprStmt).X.(*GoExpr); g.Call == nil {
		t.Fatal("go call")
	}
	if g := stmts[1].(*AssignStmt).Rhs[0].(*GoExpr); g.Block == nil {
		t.Fatal("go block")
	}
	if sel := stmts[2].(*AssignStmt).Rhs[0].(*CompositeLit).Type.(*SelectorExpr); sel.X.(*Ident).Name != "go" {
		t.Fatal("go qualifier")
	}
	// func(a, b) int keeps two type-only parameters.
	lit := body("f(func(a, b) int { return 0 })")[0].(*ExprStmt).X.(*CallExpr).Args[0].(*FuncLit)
	if params := lit.Type.Params.List; len(params) != 2 || params[0].Names != nil {
		t.Fatalf("params %#v", params)
	}
	// Bodiless declaration and the go import.
	f := parse("package p\nimport (\n\tgo \"net/url\"\n\t. \"html\"\n)\n//ef:go net/url.Parse\nfunc parse(raw string) (url.URL, error[go.Error]) needs go.Foreign\n")
	imp := f.Decls[0].(*ImportDecl)
	if imp.Specs[0].Kind != ImportGo || imp.Specs[1].Kind != ImportDot {
		t.Fatal("import kinds")
	}
	fn := f.Decls[1].(*FuncDecl)
	if fn.Body != nil || fn.Doc == nil || fn.Doc.List[0].Text != "//ef:go net/url.Parse" {
		t.Fatal("bodiless declaration or directive")
	}
	if _, ok := fn.Type.Results.List[1].Type.(*ErrorRow); !ok {
		t.Fatal("error row")
	}
	// needs (A, B) in a function type inside a parameter list.
	run := parse("package p\nfunc run(f func() needs (A, B), g func() needs R) needs A, B, R {}\n").Decls[0].(*FuncDecl)
	if u := run.Type.Params.List[0].Type.(*FuncType).Needs; len(u.Names) != 2 || !u.Lparen.IsValid() {
		t.Fatal("parenthesised needs")
	}
	if len(run.Type.Needs.Names) != 3 {
		t.Fatal("declaration needs")
	}
	// A wire header: provides, needs, both in that order, or neither.
	wires := parse("package p\nwire A provides X, y.Y {\n\tS = M{}\n}\nwire B needs C {\n\tS = M{}\n}\nwire D provides X needs C, E {\n\tS = M{}\n}\nwire F {\n\tS = M{}\n}\n").Decls
	for i, want := range []struct{ provides, needs int }{{2, 0}, {0, 1}, {1, 2}, {0, 0}} {
		w := wires[i].(*WireDecl)
		provides, needs := 0, 0
		if w.Provides != nil {
			provides = len(w.Provides.Names)
		}
		if w.Needs != nil {
			needs = len(w.Needs.Names)
		}
		if provides != want.provides || needs != want.needs {
			t.Fatalf("wire %s: provides %d, needs %d; want %d and %d", w.Name.Name, provides, needs, want.provides, want.needs)
		}
	}
	// Grouped struct fields, several wire keys, an unconstrained parameter.
	g := parse("package p\ntype P[T] struct {\n\tA, B T\n}\nwire W {\n\tC, D = X{}\n\treplace E = Y{}\n\tShared\n}\n")
	if tp := g.Decls[0].(*TypeDecl).TypeParams.List[0]; tp.Type != nil || tp.Names[0].Name != "T" {
		t.Fatal("unconstrained type parameter")
	}
	entries := g.Decls[1].(*WireDecl).Body.Entries
	if len(entries[0].Keys) != 1 || entries[1].Kind != WireReplace || entries[2].Kind != WireEmbed {
		t.Fatal("wire entries")
	}
	// Review M5: a name with type arguments is still a type name.
	row := parse("package p\nfunc f() error[retry.Exhausted[E] | go.Error | E] {}\n").Decls[0].(*FuncDecl).Type.Results.List[0].Type.(*ErrorRow)
	if _, ok := row.Types[0].(*IndexExpr); !ok || len(row.Types) != 3 {
		t.Fatalf("error row %#v", row.Types)
	}
	// Review M6: `[...]T{…}` has an Ellipsis length.
	arrLit := parse("package p\nvar x = [...]int{1, 2}\n").Decls[0].(*VarDecl).Specs[0].Values[0].(*CompositeLit)
	if arr, ok := arrLit.Type.(*ArrayType); !ok || arr.Len.(*Ellipsis).Elt != nil || len(arrLit.Elts) != 2 {
		t.Fatalf("inferred length %#v", arrLit.Type)
	}
	// Review M7: inserted header semicolons, as go/parser reads them.
	hs := body("if x := g()\nx {\n}\nswitch g()\n{\ndefault:\n}\nfor i := 0\ni < 3\ni++ {\n}")
	if s := hs[0].(*IfStmt); s.Init == nil || s.Cond.(*Ident).Name != "x" {
		t.Fatalf("if header %#v", s)
	}
	if s := hs[1].(*SwitchStmt); s.Init == nil || s.Tag != nil {
		t.Fatalf("switch header %#v", s)
	}
	if s := hs[2].(*ForStmt); s.Init == nil || s.Cond == nil || s.Post == nil {
		t.Fatalf("for header %#v", s)
	}
	// Review M8: a parenthesised receive is a receive; the parentheses stay.
	sel := body("select {\ncase (<-ch):\ncase x = (<-ch):\n}")[0].(*SelectStmt)
	if _, ok := sel.Clauses[0].Comm.(*ExprStmt).X.(*ParenExpr); !ok {
		t.Fatal("parenthesised receive")
	}
	if _, ok := sel.Clauses[1].Comm.(*AssignStmt).Rhs[0].(*ParenExpr); !ok {
		t.Fatal("assigned parenthesised receive")
	}
	// Review M9: `L: ;` labels the empty statement, as in go/ast; the loop
	// after it is not labelled. `L: }` labels an implicit one.
	ls := body("L:;\nfor {\n\tbreak L\n}")
	if l, ok := ls[0].(*LabeledStmt); !ok || l.Stmt.(*EmptyStmt).Implicit {
		t.Fatalf("labelled empty statement %#v", ls[0])
	}
	if _, ok := ls[1].(*ForStmt); !ok || len(ls) != 2 {
		t.Fatalf("the loop after the label: %#v", ls)
	}
	if l := body("M:")[0].(*LabeledStmt); !l.Stmt.(*EmptyStmt).Implicit {
		t.Fatal("implicit empty statement")
	}
}

// TestNestingLimit: every recursive construct, and every chain that
// deepens the tree (selectors, calls, indexes, binary operators, else if),
// charges the nesting limit, so a tree deeper than MaxNesting is S001
// before any consumer walks it (review M3; go/parser charges the same).
func TestNestingLimit(t *testing.T) {
	chains := []struct {
		name       string
		src        func(n int) string
		deep, flat int
	}{
		{"parentheses", func(n int) string {
			return "package p\nvar x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + "\n"
		}, MaxNesting, 400},
		{"selectors", func(n int) string { return "package p\nvar x = a" + strings.Repeat(".a", n) + "\n" }, MaxNesting, 900},
		{"calls", func(n int) string { return "package p\nvar x = f" + strings.Repeat("()", n) + "\n" }, MaxNesting, 900},
		{"indexes", func(n int) string { return "package p\nvar x = a" + strings.Repeat("[0]", n) + "\n" }, MaxNesting, 900},
		{"binary operators", func(n int) string { return "package p\nvar x = 1" + strings.Repeat(" + 1", n) + "\n" }, MaxNesting, 900},
		{"unary operators", func(n int) string { return "package p\nvar x = " + strings.Repeat("!", n) + "x\n" }, MaxNesting, 900},
		{"else if", func(n int) string {
			return "package p\nfunc f() {\n\tif x {\n\t}" + strings.Repeat(" else if x {\n\t}", n) + "\n}\n"
		}, MaxNesting, 900},
		{"blocks", func(n int) string {
			return "package p\nfunc f() " + strings.Repeat("{", n) + strings.Repeat("}", n) + "\n"
		}, MaxNesting, 400},
		{"slice types", func(n int) string { return "package p\nvar x " + strings.Repeat("[]", n) + "int\n" }, MaxNesting, 900},
	}
	for _, c := range chains {
		if _, err := Parse(c.src(c.flat)); err != nil {
			t.Errorf("%s: %d levels: %v", c.name, c.flat, err)
		}
		_, err := Parse(c.src(c.deep + 1))
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || !strings.Contains(e.Message, "nesting deeper than") {
			t.Errorf("%s: %d levels: %v, want the nesting S001", c.name, c.deep+1, err)
		}
	}
}

// nestingAt parses src and returns the offset of its nesting S001, or -1
// when src parses. Any other result fails the test.
func nestingAt(t *testing.T, src string) int {
	t.Helper()
	_, err := Parse(src)
	if err == nil {
		return -1
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeSyntax || !strings.Contains(e.Message, "nesting deeper than") {
		t.Fatalf("%v, want the nesting S001 or a parse", err)
	}
	return e.Offset
}

// TestNestingLimitBoundsTheTree (review r2 M2): the limit bounds the height
// of the returned tree, not only the path the parser has open. A chain whose
// operand is itself deep counts the operand in full, and the S001 is at the
// first token past the limit.
func TestNestingLimitBoundsTheTree(t *testing.T) {
	// `var x = E` opens one level for E; `( … )` opens two (the operand and
	// the expression inside). So `(a` + 900 links + `)` + k links is
	// 1 + 2 + 900 + k levels: k = 97 is exactly the limit.
	chain := "package p\nvar x = (a" + strings.Repeat(".a", 900) + ")"
	if at := nestingAt(t, chain+strings.Repeat(".a", 97)+"\n"); at != -1 {
		t.Fatalf("1000 levels: S001 at %d", at)
	}
	if at, want := nestingAt(t, chain+strings.Repeat(".a", 900)+"\n"), len(chain)+2*97; at != want {
		t.Fatalf("1803 levels: S001 at %d, want %d (the 98th link after the parenthesis)", at, want)
	}

	// The reviewer's layered input: 32 groups of 900 links, each wrapped in
	// parentheses and extended (round-2-receipts/mixed-depth.ef).
	x := "a" + strings.Repeat(".a", 900)
	for range 31 {
		x = "(" + x + ")" + strings.Repeat(".a", 900)
	}
	if at := nestingAt(t, "package p\nvar x = "+x+"\n"); at == -1 {
		t.Fatal("the 32-group mixed chain parsed")
	}

	// A binary operand counts in full too, and so does a deep argument.
	for _, src := range []string{
		"package p\nvar x = (" + strings.Repeat("!", 600) + "a)" + strings.Repeat(" + a", 600) + "\n",
		"package p\nvar x = f(" + strings.Repeat("(", 300) + "a" + strings.Repeat(")", 300) + ")" + strings.Repeat("[0]", 500) + "\n",
		"package p\nvar x = " + strings.Repeat("!", 500) + "(a" + strings.Repeat(" + a", 500) + ")\n",
	} {
		if nestingAt(t, src) == -1 {
			t.Errorf("parsed: %.60s…", src)
		}
	}

	// The open path alone never refuses a tree within the limit: the
	// argument of the last of 900 links sits one level below the call.
	shallow := "package p\nvar x = a" + strings.Repeat(".a", 900) + "(" + strings.Repeat("(", 200) + "a" + strings.Repeat(")", 200) + ")\n"
	if at := nestingAt(t, shallow); at != -1 {
		t.Fatalf("a 902-level tree: S001 at %d", at)
	}
}

// TestNestingLimitAtHeadersAndOperators (review r3 M2, m2): the body brace
// of a control clause is a sibling of its header, never a link of the
// header's chain; a prefix operator past the limit is the S001's token.
func TestNestingLimitAtHeadersAndOperators(t *testing.T) {
	// The block, the statement and the header expression are 3 levels, so
	// 997 links are exactly the limit and the 998th link is past it.
	for _, keyword := range []string{"if", "switch", "for"} {
		prefix := "package p\nfunc f() { " + keyword + " a"
		for _, links := range []int{996, 997} {
			if at := nestingAt(t, prefix+strings.Repeat(".a", links)+" {} }\n"); at != -1 {
				t.Errorf("%s with %d links: S001 at %d", keyword, links, at)
			}
		}
		if at, want := nestingAt(t, prefix+strings.Repeat(".a", 998)+" {} }\n"), len(prefix)+2*997; at != want {
			t.Errorf("%s with 998 links: S001 at %d, want %d (the 998th link)", keyword, at, want)
		}
	}
	// `var x = E` opens 1 level and each prefix operator 1, so the 1000th
	// operator is past the limit.
	prefix := "package p\nvar x = "
	for _, op := range []string{"! ", "- ", "+ ", "^ ", "* ", "& ", "<- ", "try "} {
		if at := nestingAt(t, prefix+strings.Repeat(op, 999)+"a\n"); at != -1 {
			t.Errorf("%q × 999: S001 at %d", op, at)
		}
		if at, want := nestingAt(t, prefix+strings.Repeat(op, 1000)+"a\n"), len(prefix)+len(op)*999; at != want {
			t.Errorf("%q × 1000: S001 at %d, want %d (the 1000th operator)", op, at, want)
		}
	}
}

// genNested renders a random expression of postfix links, binary and unary
// operators and parentheses, and returns its height in levels: a link,
// operator or unary operator is one level, parentheses are two, and an
// argument or index is one more below its link (grammar.md, Parser rule 6).
// class is 0 for a primary expression, 1 for a unary one, 2 for a binary
// one; a lower class is needed in some operand positions.
func genNested(r *rand.Rand, budget int) (src string, height, class int) {
	paren := func(s string, h, c, limit int) (string, int) {
		if c > limit {
			return "(" + s + ")", h + 2
		}
		return s, h
	}
	if budget < 3 || r.IntN(8) == 0 {
		return "a", 0, 0
	}
	run := 1 + r.IntN(budget/2)
	switch r.IntN(6) {
	case 0:
		s, h, _ := genNested(r, budget-2)
		return "(" + s + ")", h + 2, 0
	case 1:
		s, h := paren(genNestedAt(r, budget-run, 0))
		return s + strings.Repeat(".a", run), h + run, 0
	case 2, 3:
		s, h := paren(genNestedAt(r, budget/2, 0))
		a, ha, _ := genNested(r, budget/2)
		open, close := "(", ")"
		if r.IntN(2) == 0 {
			open, close = "[", "]"
		}
		return s + open + a + close, 1 + max(h, 1+ha), 0
	case 4:
		s, h := paren(genNestedAt(r, budget-run, 1))
		return strings.Repeat("!", run) + s, h + run, 1
	default:
		s, h, _ := genNested(r, budget/2)
		var b strings.Builder
		b.WriteString(s)
		for range 1 + r.IntN(4) {
			y, hy := paren(genNestedAt(r, budget/4, 1))
			b.WriteString(" + " + y)
			h = max(h, hy) + 1
		}
		return b.String(), h, 2
	}
}

// genNestedAt is genNested with the class limit its caller needs, in the
// argument order of the caller's paren helper.
func genNestedAt(r *rand.Rand, budget, limit int) (string, int, int, int) {
	s, h, c := genNested(r, budget)
	return s, h, c, limit
}

// TestNestingLimitOnCompositions: for random compositions of links,
// operators and parentheses, a tree within the limit parses and one level
// more is S001 at the first token past the limit.
func TestNestingLimitOnCompositions(t *testing.T) {
	r := rand.New(rand.NewPCG(195, 3))
	exact, over := 0, 0
	// Each composition is a declaration's value (1 level above it) or a
	// control clause header (block, statement and header: 3 levels).
	contexts := []struct {
		open, close string
		above       int
	}{
		{"package p\nvar x = ", "\n", 1},
		{"package p\nfunc f() { if ", " {} }\n", 3},
		{"package p\nfunc f() { switch ", " {} }\n", 3},
		{"package p\nfunc f() { for ", " {} }\n", 3},
	}
	for i := range 400 {
		ctx := contexts[i%len(contexts)]
		s, h, c := genNested(r, 600+r.IntN(4400))
		if c > 0 {
			s, h = "("+s+")", h+2
		}
		// k more links reach the limit.
		prefix := ctx.open + s
		if ctx.above+h > MaxNesting {
			if nestingAt(t, prefix+ctx.close) == -1 {
				t.Fatalf("%d levels parsed: %.80s…", ctx.above+h, s)
			}
			over++
			continue
		}
		k := MaxNesting - ctx.above - h
		if at := nestingAt(t, prefix+strings.Repeat(".a", k)+ctx.close); at != -1 {
			t.Fatalf("%d levels: S001 at %d in %.80s…", MaxNesting, at, s)
		}
		if at, want := nestingAt(t, prefix+strings.Repeat(".a", k+1)+ctx.close), len(prefix)+2*k; at != want {
			t.Fatalf("%d levels: S001 at %d, want %d (the link past the limit) in %.80s…", MaxNesting+1, at, want, s)
		}
		exact++
	}
	if exact < 100 || over < 50 {
		t.Fatalf("the generator reached the limit %d times and passed it %d times", exact, over)
	}
	t.Logf("reached the limit exactly %d times, passed it %d times", exact, over)
}
