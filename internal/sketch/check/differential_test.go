package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Differential testing against go/types (round-4 request A). Where Go's
// semantics apply, Go's checker is the oracle: a generator writes random
// programs in the common subset of Go and the 2026 language (generic
// types and functions, methods with receiver type parameters, interfaces
// whose methods mention type parameters, instantiations as values, as call
// targets and in parentheses, and instantiations inside type declarations)
// with no rows, so each program is both a Go file and an Ergo file, byte
// for byte. Both checkers must agree on accept or reject (instantiation
// cycles are the reason to reject), and on the type of every instantiated
// function expression. Prior art: Csmith-style differential testing; Go
// cross-checks types2 against go/types the same way.

// diffGen writes one program from rng.
type diffGen struct {
	rng    *rand.Rand
	b      strings.Builder
	types  int // generic struct types Gt0…
	ifaces int // generic interface types Gi0…
	funcs  int // generic functions Fn0…
}

// typeExpr writes a type of depth at most d; param is the type parameter
// in scope, or "" outside generic code.
func (g *diffGen) typeExpr(param string, d int) string {
	leaves := []string{"int", "string"}
	if param != "" {
		leaves = append(leaves, param, param)
	}
	if d == 0 || g.rng.Intn(3) == 0 {
		return leaves[g.rng.Intn(len(leaves))]
	}
	inner := g.typeExpr(param, d-1)
	switch g.rng.Intn(9) {
	case 0:
		return "[]" + inner
	case 1:
		return "[2]" + inner
	case 2:
		return "map[string]" + inner
	case 3:
		return "func(" + inner + ")"
	case 4:
		return "struct{ V " + inner + " }"
	case 5:
		return "interface{ M(" + inner + ") }"
	case 6:
		return fmt.Sprintf("Gi%d[%s]", g.rng.Intn(g.ifaces), inner)
	default:
		return fmt.Sprintf("Gt%d[%s]", g.rng.Intn(g.types), inner)
	}
}

// stmts writes k statements that instantiate generics.
func (g *diffGen) stmts(param string, k int) {
	for range k {
		t := g.typeExpr(param, 2)
		switch g.rng.Intn(6) {
		case 0:
			fmt.Fprintf(&g.b, "\tFn%d[%s]()\n", g.rng.Intn(g.funcs), t)
		case 1:
			fmt.Fprintf(&g.b, "\t_ = Fn%d[%s]\n", g.rng.Intn(g.funcs), t)
		case 2:
			fmt.Fprintf(&g.b, "\t(Fn%d[%s])()\n", g.rng.Intn(g.funcs), t)
		case 3:
			fmt.Fprintf(&g.b, "\t_ = Gt%d[%s]{}\n", g.rng.Intn(g.types), t)
		default:
			j := g.rng.Intn(g.types)
			fmt.Fprintf(&g.b, "\tGt%d[%s]{}.Mt%d()\n", j, t, j)
		}
	}
}

func (g *diffGen) program() string {
	g.b.Reset()
	g.types = 1 + g.rng.Intn(3)
	g.ifaces = 1 + g.rng.Intn(2)
	g.funcs = 1 + g.rng.Intn(3)
	g.b.WriteString("package p\n\n")
	for i := range g.types {
		field := "[]P"
		if g.rng.Intn(4) == 0 {
			// An instantiation inside a type declaration, behind a slice.
			field = "[]" + g.typeExpr("P", 2)
		}
		fmt.Fprintf(&g.b, "type Gt%d[P any] struct{ F %s }\n\n", i, field)
	}
	for i := range g.ifaces {
		fmt.Fprintf(&g.b, "type Gi%d[P any] interface{ M(%s) }\n\n", i, g.typeExpr("P", 1))
	}
	for i := range g.funcs {
		fmt.Fprintf(&g.b, "func Fn%d[P any]() {\n", i)
		g.stmts("P", g.rng.Intn(3))
		g.b.WriteString("}\n\n")
	}
	for i := range g.types {
		fmt.Fprintf(&g.b, "func (r Gt%d[P]) Mt%d() {\n", i, i)
		g.stmts("P", g.rng.Intn(3))
		g.b.WriteString("}\n\n")
	}
	g.b.WriteString("func Main() {\n")
	g.stmts("", 1+g.rng.Intn(3))
	g.b.WriteString("}\n")
	return g.b.String()
}

// goKey and ergoKey write a type in one normal form, so the two checkers'
// types compare as strings: parameter names, package qualifiers and
// spacing are dropped.
func goKey(t types.Type) string {
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		return t.Name()
	case *types.Named:
		s := t.Obj().Name()
		if args := t.TypeArgs(); args != nil && args.Len() > 0 {
			parts := make([]string, args.Len())
			for i := range parts {
				parts[i] = goKey(args.At(i))
			}
			s += "[" + strings.Join(parts, ",") + "]"
		}
		return s
	case *types.TypeParam:
		return t.Obj().Name()
	case *types.Slice:
		return "[]" + goKey(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), goKey(t.Elem()))
	case *types.Map:
		return "map[" + goKey(t.Key()) + "]" + goKey(t.Elem())
	case *types.Chan:
		return "chan " + goKey(t.Elem())
	case *types.Pointer:
		return "*" + goKey(t.Elem())
	case *types.Signature:
		var ps, rs []string
		for i := range t.Params().Len() {
			ps = append(ps, goKey(t.Params().At(i).Type()))
		}
		for i := range t.Results().Len() {
			rs = append(rs, goKey(t.Results().At(i).Type()))
		}
		return "func(" + strings.Join(ps, ",") + ")(" + strings.Join(rs, ",") + ")"
	case *types.Struct:
		var fs []string
		for i := range t.NumFields() {
			fs = append(fs, t.Field(i).Name()+" "+goKey(t.Field(i).Type()))
		}
		return "struct{" + strings.Join(fs, ";") + "}"
	case *types.Interface:
		var ms []string
		for i := range t.NumMethods() {
			m := t.Method(i)
			ms = append(ms, m.Name()+goKey(m.Type()))
		}
		sort.Strings(ms)
		return "interface{" + strings.Join(ms, ";") + "}"
	}
	return fmt.Sprintf("?%T", t)
}

func ergoKey(t Type) string {
	switch t := t.(type) {
	case *Basic:
		return t.Name
	case *Named:
		if t.Obj.Name == "any" && len(t.TArgs) == 0 {
			return "interface{}"
		}
		s := t.Obj.Name
		if len(t.TArgs) > 0 {
			parts := make([]string, len(t.TArgs))
			for i, a := range t.TArgs {
				parts[i] = ergoKey(a)
			}
			s += "[" + strings.Join(parts, ",") + "]"
		}
		return s
	case *TypeParam:
		return t.Name
	case *Slice:
		return "[]" + ergoKey(t.Elem)
	case *Array:
		return fmt.Sprintf("[%d]%s", t.Len, ergoKey(t.Elem))
	case *Map:
		return "map[" + ergoKey(t.Key) + "]" + ergoKey(t.Elem)
	case *Chan:
		return "chan " + ergoKey(t.Elem)
	case *Pointer:
		return "*" + ergoKey(t.Elem)
	case *Signature:
		var ps, rs []string
		for _, p := range t.Params {
			ps = append(ps, ergoKey(p.Type))
		}
		for _, r := range t.Results {
			rs = append(rs, ergoKey(r))
		}
		return "func(" + strings.Join(ps, ",") + ")(" + strings.Join(rs, ",") + ")"
	case *Struct:
		var fs []string
		for _, f := range t.Fields {
			fs = append(fs, f.Name+" "+ergoKey(f.Type))
		}
		return "struct{" + strings.Join(fs, ";") + "}"
	case *Interface:
		var ms []string
		for _, m := range t.Methods {
			ms = append(ms, m.Name+ergoKey(m.Sig))
		}
		sort.Strings(ms)
		return "interface{" + strings.Join(ms, ";") + "}"
	}
	return fmt.Sprintf("?%T", t)
}

// span is an expression's byte range, the key that matches the two ASTs.
type span struct{ from, to int }

// diffOne checks src with both checkers and returns the disagreements.
func diffOne(src string) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		return []string{"go cannot parse the generated program: " + err.Error()}
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	var goErrs []string
	conf := types.Config{Error: func(e error) { goErrs = append(goErrs, e.Error()) }}
	_, _ = conf.Check("p", fset, []*ast.File{file}, info)
	pkg, diags := CheckSource(src)
	var out []string
	if (len(goErrs) == 0) != (len(diags) == 0) {
		var ds []string
		for _, d := range diags {
			ds = append(ds, d.String())
		}
		out = append(out, fmt.Sprintf("verdicts differ: go %q, ergo %q", goErrs, ds))
		return out
	}
	if len(goErrs) > 0 {
		return nil
	}
	// The instantiated function expressions: Fn[X] and its parentheses.
	goTypes := map[span]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		if p, ok := n.(*ast.ParenExpr); ok {
			if ix, ok := ast.Unparen(p).(*ast.IndexExpr); ok {
				if id, ok := ix.X.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Fn") {
					goTypes[span{fset.Position(p.Pos()).Offset, fset.Position(p.End()).Offset}] = goKey(info.Types[p].Type)
				}
			}
			return true
		}
		ix, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		id, ok := ix.X.(*ast.Ident)
		if !ok || !strings.HasPrefix(id.Name, "Fn") {
			return true
		}
		tv, ok := info.Types[ix]
		if !ok {
			out = append(out, fmt.Sprintf("go records no type for %s", src[fset.Position(ix.Pos()).Offset:fset.Position(ix.End()).Offset]))
			return true
		}
		goTypes[span{fset.Position(ix.Pos()).Offset, fset.Position(ix.End()).Offset}] = goKey(tv.Type)
		return true
	})
	ergoTypes := map[span]string{}
	for x, t := range pkg.Info.Types {
		switch x.(type) {
		case *syntax.IndexExpr, *syntax.ParenExpr:
			ergoTypes[span{int(x.Pos()), int(x.End())}] = ergoKey(t)
		}
	}
	for sp, gt := range goTypes {
		et, ok := ergoTypes[sp]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: ergo records no type; go %s", src[sp.from:sp.to], gt))
		case et != gt:
			out = append(out, fmt.Sprintf("%s: ergo %s, go %s", src[sp.from:sp.to], et, gt))
		}
	}
	sort.Strings(out)
	return out
}

// diffSeeds is the fixed seed set the unit test runs; FuzzDifferential
// runs any seed.
const diffSeeds = 400

// TestDifferentialGoTypes runs the generator over a fixed seed set. Both
// verdicts must occur, so the run covers accepted and rejected programs.
func TestDifferentialGoTypes(t *testing.T) {
	accepted, rejected := 0, 0
	for seed := int64(0); seed < diffSeeds; seed++ {
		g := &diffGen{rng: rand.New(rand.NewSource(seed))}
		src := g.program()
		if d := diffOne(src); len(d) > 0 {
			t.Fatalf("seed %d:\n%s\n%s", seed, strings.Join(d, "\n"), src)
		}
		if _, diags := CheckSource(src); len(diags) == 0 {
			accepted++
		} else {
			rejected++
		}
	}
	if accepted < diffSeeds/10 || rejected < diffSeeds/10 {
		t.Errorf("the seeds cover %d accepted and %d rejected programs; the generator misses a verdict", accepted, rejected)
	}
}

// FuzzDifferential runs the generator on any seed (go test -fuzz).
func FuzzDifferential(f *testing.F) {
	for seed := int64(0); seed < 8; seed++ {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		g := &diffGen{rng: rand.New(rand.NewSource(seed))}
		src := g.program()
		if d := diffOne(src); len(d) > 0 {
			t.Fatalf("seed %d:\n%s\n%s", seed, strings.Join(d, "\n"), src)
		}
	})
}
