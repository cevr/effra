package syntax

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/sketchtest"
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
		{"paren uses on a declaration", "package p\nfunc f() uses (A, B) {}\n", "inside a list"},
		{"unparenthesised uses in a list", "package p\nfunc f(g func() uses A, B) {}\n", "parameter type"},
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
	f := parse("package p\nimport (\n\tgo \"net/url\"\n\t. \"html\"\n)\n//ef:go net/url.Parse\nfunc parse(raw string) (url.URL, error[go.Error]) uses go.Foreign\n")
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
	// uses (A, B) in a function type inside a parameter list.
	run := parse("package p\nfunc run(f func() uses (A, B), g func() uses R) uses A, B, R {}\n").Decls[0].(*FuncDecl)
	if u := run.Type.Params.List[0].Type.(*FuncType).Uses; len(u.Names) != 2 || !u.Lparen.IsValid() {
		t.Fatal("parenthesised uses")
	}
	if len(run.Type.Uses.Names) != 3 {
		t.Fatal("declaration uses")
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
}
