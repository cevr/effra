package proto

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"effra.local/prototype/internal/sketch/syntax"
)

// Parse parses src with the promoted parser and converts it through the
// bridge, reporting the first problem as the prototype's parser did
// ("NAME:LINE:COL: CODE MESSAGE"), so the moved checker tests run unchanged.
func Parse(name, src string) (*File, error) {
	f, err := syntax.Parse(src)
	if err != nil {
		var e *syntax.Error
		if !errors.As(err, &e) {
			return nil, err
		}
		return nil, fmt.Errorf("%s:%d:%d: %s %s", name, e.Line, e.Column, e.Code, e.Message)
	}
	out, diags := FromSyntax(f)
	if len(diags) > 0 {
		return nil, fmt.Errorf("%s", diags[0].Format(name))
	}
	out.Name = name
	return out, nil
}

func TestBridgeRefusesOutsideTheSubset(t *testing.T) {
	for _, b := range bridgeTable {
		t.Run(b.name, func(t *testing.T) {
			f, err := syntax.Parse(b.src)
			if err != nil {
				t.Fatalf("does not parse: %v", err)
			}
			out, diags := FromSyntax(f)
			if out != nil {
				t.Fatal("a file with refusals has an AST")
			}
			found := false
			for _, d := range diags {
				if d.Code != codeOutside {
					t.Fatalf("diagnostic %+v is not S109", d)
				}
				found = found || d.Pos.Line == b.line
			}
			if !found {
				t.Fatalf("no S109 on line %d: %v", b.line, diags)
			}
		})
	}
}

// TestBridgeRefusals covers the refusals the bridge table does not: each
// source parses, and the bridge refuses it with S109 on the marked line.
func TestBridgeRefusals(t *testing.T) {
	for name, src := range map[string]string{
		"wire needs":        "package p\nwire App needs A {\n\tStore = M{}\n}\n// LINE 2",
		"wire two keys":     "package p\nwire App {\n\tA, B = M{}\n}\n// LINE 3",
		"wire embed":        "package p\nwire App {\n\tShared\n}\n// LINE 3",
		"wire replace":      "package p\nwire App {\n\treplace A = M{}\n}\n// LINE 3",
		"trailing block":    "package p\nfunc main() {\n\tf() { g() }\n}\n// LINE 3",
		"statement arm":     "package p\nfunc main() {\n\tx := f() catch {\n\tcase E:\n\t\treturn\n\t}\n}\n// LINE 4",
		"named import":      "package p\nimport u \"net/url\"\n// LINE 2",
		"go import":         "package p\nimport go \"net/url\"\n// LINE 2",
		"bodiless":          "package p\nfunc f()\n// LINE 2",
		"unary":             "package p\nfunc main() {\n\tx := !y\n}\n// LINE 3",
		"index":             "package p\nfunc main() {\n\tx := y[0]\n}\n// LINE 3",
		"rune":              "package p\nfunc main() {\n\tx := 'r'\n}\n// LINE 3",
		"remainder":         "package p\nfunc main() {\n\tx := 1 % 2\n}\n// LINE 3",
		"increment":         "package p\nfunc main() {\n\tx++\n}\n// LINE 3",
		"default clause":    "package p\nfunc f(u U) {\n\tswitch x := u.(type) {\n\tdefault:\n\t}\n}\n// LINE 4",
		"pointer receiver":  "package p\nfunc (t *T) M() {}\n// LINE 2",
		"variadic":          "package p\nfunc f(xs ...int64) {}\n// LINE 2",
		"slice type":        "package p\nfunc f(xs []int64) {}\n// LINE 2",
		"generic type":      "package p\ntype Box[T] struct{ v T }\n// LINE 2",
		"qualified literal": "package p\nfunc main() {\n\tx := pkg.T{}\n}\n// LINE 3",
		"if init":           "package p\nfunc main() {\n\tif x := f(); x {\n\t}\n}\n// LINE 3",
		"var":               "package p\nvar x = 1\n// LINE 2",
	} {
		t.Run(name, func(t *testing.T) {
			var line int
			fmt.Sscanf(src[strings.LastIndex(src, "// LINE ")+len("// LINE "):], "%d", &line)
			f, err := syntax.Parse(src)
			if err != nil {
				t.Fatalf("does not parse: %v", err)
			}
			out, diags := FromSyntax(f)
			if out != nil || len(diags) == 0 || diags[0].Code != codeOutside || diags[0].Pos.Line != line {
				t.Fatalf("got %v, want S109 on line %d", diags, line)
			}
		})
	}
}

// TestBridgeDesugars: else if and grouped names convert exactly.
func TestBridgeDesugars(t *testing.T) {
	src := "package p\ntype P struct {\n\tA, B string\n}\nfunc f(a, b string) string {\n\tif a == b {\n\t\treturn a\n\t} else if a == \"x\" {\n\t\treturn b\n\t} else {\n\t\treturn \"\"\n\t}\n}\n"
	f, err := Parse("d.sketch", src)
	if err != nil {
		t.Fatal(err)
	}
	if fields := f.Types[0].Fields; len(fields) != 2 || fields[1].Name != "B" || fields[1].Type != "string" || fields[1].Pos != (Pos{3, 5}) {
		t.Fatalf("fields %+v", fields)
	}
	if params := f.Funcs[0].Params; len(params) != 2 || params[1].Name != "b" || params[1].Type != "string" {
		t.Fatalf("params %+v", params)
	}
	outer := f.Funcs[0].Body.Stmts[0].(*IfStmt)
	nested, ok := outer.Else.Stmts[0].(*IfStmt)
	if !ok || len(outer.Else.Stmts) != 1 || nested.Else == nil || nested.Pos != (Pos{8, 9}) {
		t.Fatalf("else if converted to %+v", outer.Else)
	}
}

// bridgeTable: programs that parse but use one construct the prototype
// checker does not model (spec D3). Each must get S109 at that construct's
// line, no syntax code, and must not build. A converter that dropped the
// construct would let the program build: a passthrough.
var bridgeTable = []struct {
	name string
	src  string
	line int
}{
	{"for-loop", "package p\n\nfunc main() needs Console {\n\tfor i := 0; i < 3; i++ {\n\t\tConsole.Println(\"x\")\n\t}\n}\n", 4},
	{"defer", "package p\n\nfunc main() needs Console {\n\tdefer Console.Println(\"done\")\n\tConsole.Println(\"x\")\n}\n", 4},
	{"local-var", "package p\n\nfunc main() needs Console {\n\tvar s = \"x\"\n\tConsole.Println(s)\n}\n", 4},
	{"go-task", "package p\n\nfunc work() needs Console {\n\tConsole.Println(\"w\")\n}\n\nfunc main() needs Console {\n\tt := go work()\n\tt.Stop()\n}\n", 8},
	{"generic-function", "package p\n\nfunc same[T comparable](x T) T {\n\treturn x\n}\n\nfunc main() needs Console {\n\tConsole.Println(same(\"x\"))\n}\n", 3},
	{"machine", "package p\n\ntype State interface{ Idle | Busy }\ntype Idle struct{}\ntype Busy struct{}\ntype Event interface{ Start | Halt }\ntype Start struct{}\ntype Halt struct{}\n\nfunc step(s State, e Event) string {\n\treturn \"x\"\n}\n\ntype M machine[State, Event] {\n\tinitial Idle{}\n\tstep    step\n}\n\nfunc main() needs Console {\n\tConsole.Println(\"x\")\n}\n", 14},
	{"catch-binding", "package p\n\ntype NotFound struct{ Name string }\n\nfunc find() (string, error[NotFound]) {\n\treturn NotFound{Name: \"x\"}\n}\n\nfunc main() needs Console {\n\tline := find() catch e {\n\tcase NotFound:\n\t\te.Name\n\t}\n\tConsole.Println(line)\n}\n", 10},
	{"func-literal", "package p\n\nfunc main() needs Console {\n\tf := func() string { return \"x\" }\n\tConsole.Println(f())\n}\n", 4},
	{"assignment", "package p\n\nfunc main() needs Console {\n\ts := \"a\"\n\ts = \"b\"\n\tConsole.Println(s)\n}\n", 5},
	{"multi-subject-switch", "package p\n\ntype A interface{ X | Y }\ntype X struct{}\ntype Y struct{}\n\nfunc pair(a A, b A) string {\n\tswitch p, q := a.(type), b.(type) {\n\tcase (X, X):\n\t\treturn \"xx\"\n\tcase (_, _):\n\t\treturn \"other\"\n\t}\n}\n\nfunc main() needs Console {\n\tConsole.Println(pair(X{}, Y{}))\n}\n", 8},
}
