package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// infoCorpus is every program TestInfoComplete reads: the positive and
// spec fixtures and the stage fixtures copied into testdata/v1. A program
// with a diagnostic is not lowered, so only the clean ones count.
func infoCorpus(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pattern := range []string{"fixtures/positive/*.ergo", "fixtures/spec/*.ergo", "v1/*.pos.ergo"} {
		files, _ := filepath.Glob(filepath.Join("testdata", pattern))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out[f] = string(data)
		}
	}
	return out
}

// infoGaps lists what Info misses in a clean package (invariant 1) and
// the types it holds that are not final (invariant 2): every expression in
// a body or a package let has a Types entry, every identifier a Defs or
// Uses entry, every call a Calls entry, every type-match case a Patterns
// entry, every match and catch a Matches entry, every for-in a Loops entry
// and every with a Withs entry. Names that denote no object are not
// identifiers of the contract: a field or operation name after a dot or
// before a colon in a record literal, and the blank `_`.
func infoGaps(file *syntax.File, src string, info *Info) []string {
	var gaps []string
	at := func(n syntax.Node, what string) {
		line := 1 + strings.Count(src[:min(int(n.Pos()), len(src))], "\n")
		gaps = append(gaps, fmt.Sprintf("%d: %s", line, what))
	}
	notObject := map[*syntax.Ident]bool{}
	notValue := map[syntax.Expr]bool{}
	// skip marks a subtree that holds no value expression: a written type,
	// a pattern (Patterns holds it) or a with's wire (Withs holds it; an
	// inline wire's entries are stage E's).
	skip := func(n syntax.Node, objects bool) {
		syntax.Inspect(n, func(m syntax.Node) bool {
			if x, ok := m.(syntax.Expr); ok {
				notValue[x] = true
			}
			if id, ok := m.(*syntax.Ident); ok && objects {
				notObject[id] = true
			}
			return true
		})
	}
	var visit func(n syntax.Node) bool
	visit = func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.DeclStmt:
			for _, spec := range n.Decl.Specs {
				if spec.Type != nil {
					skip(spec.Type, false)
				}
			}
		case *syntax.InterpolatedString:
			for _, text := range n.Texts {
				notValue[text] = true
			}
		case *syntax.TypeGuard:
			notValue[n] = true
		case *syntax.SelectorExpr:
			notValue[n.Sel] = true
			notObject[n.Sel] = true
		case *syntax.KeyValueExpr:
			if id, ok := n.Key.(*syntax.Ident); ok {
				notObject[id] = true
				notValue[id] = true
			}
			notValue[n] = true
		case *syntax.CompositeLit:
			if n.Type != nil {
				skip(n.Type, false)
			}
		case *syntax.IndexExpr:
			// Fn[T]: the brackets hold type arguments, which Calls holds.
			if _, generic := info.Types[n.X].(*Signature); generic {
				for _, ix := range n.Indices {
					skip(ix, false)
				}
			}
		case *syntax.TypeSwitchStmt:
			if info.Matches[n] == nil {
				at(n, "a match with no Matches entry")
			}
			for _, cl := range n.Clauses {
				for _, item := range cl.List {
					if info.Patterns[item] == nil {
						at(item, "a case with no Patterns entry")
					}
					skip(item, false)
				}
			}
		case *syntax.SwitchStmt:
			if info.Matches[n] == nil {
				at(n, "a value match with no Matches entry")
			}
		case *syntax.CatchExpr:
			if info.Matches[n] == nil {
				at(n, "a catch with no Matches entry")
			}
			for _, arm := range n.Arms {
				for _, te := range arm.Types {
					if info.Patterns[te] == nil {
						at(te, "a catch arm with no Patterns entry")
					}
					skip(te, false)
				}
			}
		case *syntax.RangeStmt:
			if info.Loops[n] == nil {
				at(n, "a for-in with no Loops entry")
			}
		case *syntax.CallExpr:
			if info.Calls[n] == nil {
				at(n, "a call with no Calls entry")
			} else if k := info.Calls[n].Kind; k == CallPrelude || k == CallLen {
				notValue[n.Fun] = true // a built-in callee is no value
			}
			if id, ok := n.Fun.(*syntax.Ident); ok && id.Name == "with" {
				notObject[id] = true
				notValue[id] = true
				if info.Withs[n] == nil {
					at(n, "a with with no Withs entry")
				}
				for _, a := range n.Args {
					skip(a, true)
				}
			}
		case *syntax.FuncLit:
			skip(n.Type, false)
		}
		if _, isStmt := n.(syntax.Stmt); isStmt {
			return true // an if or match statement: Tails and Matches hold it
		}
		switch n := n.(type) {
		case *syntax.Ident:
			if n.Name != "_" && !notObject[n] && info.Defs[n] == nil && info.Uses[n] == nil {
				at(n, "the identifier "+n.Name+" with no Defs or Uses entry")
			}
			if !notValue[n] && info.Defs[n] == nil {
				if _, ok := info.Types[n]; !ok && n.Name != "_" {
					at(n, "the expression "+n.Name+" with no Types entry")
				}
			}
		case syntax.Expr:
			if t, ok := info.Types[n]; ok {
				if why := notFinal(t); why != "" {
					at(n, fmt.Sprintf("a %T whose type %s %s", n, t, why))
				}
			} else if !notValue[n] {
				at(n, fmt.Sprintf("a %T with no Types entry", n))
			}
		}
		return true
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *syntax.FuncDecl:
			if d.Body != nil {
				syntax.Inspect(d.Body, visit)
			}
		case *syntax.VarDecl:
			for _, spec := range d.Specs {
				for _, v := range spec.Values {
					syntax.Inspect(v, visit)
				}
			}
		}
	}
	sort.Strings(gaps)
	return gaps
}

// notFinal says why a type in Info is not final (invariant 2), or "".
func notFinal(t Type) string {
	switch {
	case isInvalid(t):
		return "is invalid"
	case containsInvalid(t):
		return "holds an invalid part"
	}
	return ""
}

// TestInfoComplete (spec D-A4): over every clean program of the corpus,
// Info is complete and final (invariants 1 and 2), and checking twice
// gives the same Calls, Loops and Matches (invariant 4).
func TestInfoComplete(t *testing.T) {
	clean := 0
	for name, src := range infoCorpus(t) {
		file, err := syntax.Parse(src)
		if err != nil {
			continue
		}
		pkg, diags := Check("main", []*syntax.File{file}, nil)
		if len(diags) > 0 {
			continue
		}
		clean++
		if gaps := infoGaps(file, src, pkg.Info); len(gaps) > 0 {
			t.Errorf("%s:\n\t%s", name, strings.Join(gaps, "\n\t"))
		}
		again, _ := Check("main", []*syntax.File{file}, nil)
		if a, b := infoDigest(pkg.Info), infoDigest(again.Info); a != b {
			t.Errorf("%s: two checks give different Info:\n%s\n----\n%s", name, a, b)
		}
	}
	if clean < 20 {
		t.Fatalf("only %d clean programs in the corpus", clean)
	}
}

// infoDigest writes Info's call, loop and match entries in source order.
func infoDigest(info *Info) string {
	var lines []string
	for x, call := range info.Calls {
		fn := ""
		if call.Func != nil {
			fn = call.Func.Name
		}
		lines = append(lines, fmt.Sprintf("%d call %d %s %v %s %s", x.Pos(), call.Kind, fn, call.TypeArgs, rowText(call.Fails), rowText(call.Needs)))
	}
	for s, l := range info.Loops {
		lines = append(lines, fmt.Sprintf("%d loop %d %s %s %d", s.Pos(), l.Domain, l.Elem, l.Pos, len(l.Bindings)))
	}
	for n, m := range info.Matches {
		lines = append(lines, fmt.Sprintf("%d match %v %d %v %v", n.Pos(), m.Subjects, m.Arms, m.Result, m.Partial))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestInfoContract reads the entries of one program the lowering needs:
// the call kinds and what each calls, a dot call's receiver, inferred type
// arguments, constant values, the implicit conversion of a value into an
// operation type with its table, loop domains and the defs of parameters.
func TestInfoContract(t *testing.T) {
	const src = `type Store { Get() string }
type Mem { V string }

func Get(m Mem) string { return m.V }

func Id[T](x T) T {
	return x
}

func NewStore(v string) Store { return Mem{V: v} }

func use(s Store, m Mem, xs []int8) int {
	let n = Id(xs)
	let total = len(n) + 2 * 3
	for i, x in xs {
		let y = int(x) + i
		s.Get()
		m.Get()
		total += y
	}
	for k in 0..total {
		total += k
	}
	return total
}
`
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, diags := Check("main", []*syntax.File{file}, nil)
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	info := pkg.Info
	calls := map[string]*Call{}
	for x, call := range info.Calls {
		calls[src[x.Pos():x.End()]] = call
	}
	want := func(text string, kind CallKind) *Call {
		t.Helper()
		call := calls[text]
		if call == nil || call.Kind != kind {
			t.Fatalf("%s: want call kind %d, got %+v", text, kind, call)
		}
		return call
	}
	if c := want("Id(xs)", CallFunc); c.Func.Name != "Id" || len(c.TypeArgs) != 1 || c.TypeArgs[0].String() != "[]int8" {
		t.Errorf("Id(xs): %+v", c)
	}
	want("len(n)", CallLen)
	if c := want("int(x)", CallConversion); c.Type.String() != "int" {
		t.Errorf("int(x): %+v", c)
	}
	if c := want("s.Get()", CallOp); c.Op == nil || c.Op.Name != "Get" || src[c.Recv.Pos():c.Recv.End()] != "s" {
		t.Errorf("s.Get(): %+v", c)
	}
	if c := want("m.Get()", CallFunc); c.Func.Name != "Get" || src[c.Recv.Pos():c.Recv.End()] != "m" {
		t.Errorf("m.Get(): %+v", c)
	}
	conv := false
	for x, v := range info.Values {
		if src[x.Pos():x.End()] == "2 * 3" && v.String() == "6" {
			conv = true
		}
	}
	if !conv {
		t.Errorf("Values has no 6 for 2 * 3")
	}
	found := false
	for x, c := range info.Implicit {
		if src[x.Pos():x.End()] == "Mem{V: v}" && c.Kind == ConvOperations && c.Table["Get"] != nil && c.Table["Get"].Name == "Get" {
			found = true
		}
	}
	if !found {
		t.Errorf("Implicit has no operations conversion of Mem{V: v} to Store: %v", info.Implicit)
	}
	domains := map[LoopDomain]int{}
	for _, l := range info.Loops {
		domains[l.Domain] = len(l.Bindings)
	}
	if domains[LoopSlice] != 2 || domains[LoopRange] != 1 {
		t.Errorf("Loops: %v", domains)
	}
	params := 0
	for id, o := range info.Defs {
		if v, ok := o.(*Var); ok && v.param && (id.Name == "s" || id.Name == "xs") {
			params++
		}
	}
	if params < 2 {
		t.Errorf("Defs misses the parameters s and xs")
	}
}
