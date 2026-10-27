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
	"unicode"
)

// Differential testing against go/types (round-4 request A, widened in
// round 5). Where Go's semantics apply, Go's checker is the oracle: a
// generator writes random programs in the common subset of Go and the 2026
// language, with no rows, so each program is both a Go file and an Ergo
// file, byte for byte. The programs hold:
//
//   - generic struct and interface types, instantiated inside type
//     declarations;
//   - generic functions of one or two type parameters with value
//     parameters and results;
//   - methods whose receiver names the type's parameter or renames it,
//     with parametric signatures, some bodiless, which instantiate the
//     receiver's own and other receivers' types (signature growth and
//     cross-receiver cycles, round-4 M1);
//   - instantiations as values, method values, call targets, in
//     parentheses, explicit with every
//     argument, partial, and inferred.
//
// Both checkers must agree on accept or reject; a rejection must be an
// instantiation cycle in both (any other go/types error is a generator
// bug and fails the test). On accepted programs, every expression go/types
// records as a value (identifiers, index expressions with one or several
// indices, parentheses, selectors, calls, composite literals) must carry
// the same type in Ergo's Info.Types, generic signatures with their type
// parameters and constraints; a type expression Ergo records must match
// too (round-4 M3). Prior art: Csmith-style differential testing; Go
// cross-checks types2 against go/types the same way.

// diffFunc is a generated generic function: its parameter count and its
// value parameter types, written over P and Q.
type diffFunc struct {
	ntp    int
	params []string
	result bool
}

// diffGen writes one program from rng.
type diffGen struct {
	rng    *rand.Rand
	b      strings.Builder
	types  int // generic struct types Gt0…
	ifaces int // generic interface types Gi0…
	funcs  []diffFunc
	vars   int
}

// typeExpr writes a type of depth at most d over the type parameters ps.
func (g *diffGen) typeExpr(ps []string, d int) string {
	leaves := []string{"int", "string"}
	leaves = append(leaves, ps...)
	leaves = append(leaves, ps...)
	if d == 0 || g.rng.Intn(3) == 0 {
		return leaves[g.rng.Intn(len(leaves))]
	}
	inner := g.typeExpr(ps, d-1)
	switch g.rng.Intn(10) {
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
	case 7:
		return "chan " + inner
	default:
		return fmt.Sprintf("Gt%d[%s]", g.rng.Intn(g.types), inner)
	}
}

// zeroType writes a type that has a zero value in Ergo (no function,
// interface or channel type, no bare type parameter in a non-generic
// context), over the type parameters ps.
func (g *diffGen) zeroType(ps []string, d int) string {
	leaves := []string{"int", "string"}
	leaves = append(leaves, ps...)
	if d == 0 || g.rng.Intn(3) == 0 {
		return leaves[g.rng.Intn(len(leaves))]
	}
	inner := g.zeroType(ps, d-1)
	switch g.rng.Intn(5) {
	case 0:
		return "[]" + inner
	case 1:
		return "[2]" + inner
	case 2:
		return "map[string]" + inner
	case 3:
		return "struct{ V " + inner + " }"
	default:
		return fmt.Sprintf("Gt%d[%s]", g.rng.Intn(g.types), inner)
	}
}

// substParams replaces the type parameter names in a type written by the
// generator with their arguments, token by token.
func substParams(t string, m map[string]string) string {
	var out strings.Builder
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	rs := []rune(t)
	for i := 0; i < len(rs); {
		if !word(rs[i]) {
			out.WriteRune(rs[i])
			i++
			continue
		}
		j := i
		for j < len(rs) && word(rs[j]) {
			j++
		}
		w := string(rs[i:j])
		if a, ok := m[w]; ok {
			w = a
		}
		out.WriteString(w)
		i = j
	}
	return out.String()
}

var tpNames = []string{"P", "Q"}

// targs writes type arguments for k parameters in the context ps.
func (g *diffGen) targs(ps []string, k int, zero bool) []string {
	out := make([]string, k)
	for i := range out {
		if zero {
			out[i] = g.zeroType(ps, 2)
		} else {
			out[i] = g.typeExpr(ps, 2)
		}
	}
	return out
}

// stmts writes k statements that instantiate generics in the context of
// the type parameters ps (none in Main).
func (g *diffGen) stmts(ps []string, k int) {
	for range k {
		choices := 5
		if len(ps) == 0 {
			choices = 10
		}
		switch g.rng.Intn(choices) {
		case 0:
			i := g.rng.Intn(len(g.funcs))
			fmt.Fprintf(&g.b, "\t_ = Fn%d[%s]\n", i, strings.Join(g.targs(ps, g.funcs[i].ntp, false), ", "))
		case 1:
			i := g.rng.Intn(len(g.funcs))
			fmt.Fprintf(&g.b, "\t_ = (Fn%d[%s])\n", i, strings.Join(g.targs(ps, g.funcs[i].ntp, false), ", "))
		case 2:
			fmt.Fprintf(&g.b, "\t_ = Gt%d[%s]{}\n", g.rng.Intn(g.types), g.typeExpr(ps, 2))
		case 3:
			j := g.rng.Intn(g.types)
			fmt.Fprintf(&g.b, "\tGt%d[%s]{}.Mt%d()\n", j, g.typeExpr(ps, 2), j)
		case 4:
			j := g.rng.Intn(g.types)
			fmt.Fprintf(&g.b, "\t_ = Gt%d[%s]{}.Ms%d\n", j, g.typeExpr(ps, 2), j)
		default:
			g.call()
		}
	}
}

// call writes a call of a generated function in Main, with arguments
// declared as variables of zero-valued types: explicit, in parentheses
// once or twice, partial, or inferred.
func (g *diffGen) call() {
	i := g.rng.Intn(len(g.funcs))
	f := g.funcs[i]
	ta := g.targs(nil, f.ntp, true)
	m := map[string]string{}
	for k := range f.ntp {
		m[tpNames[k]] = ta[k]
	}
	var args []string
	for _, p := range f.params {
		v := fmt.Sprintf("a%d", g.vars)
		g.vars++
		fmt.Fprintf(&g.b, "\tvar %s %s\n", v, substParams(p, m))
		args = append(args, v)
	}
	// occurs reports whether parameter k can be inferred from the
	// arguments.
	occurs := func(k int) bool {
		for _, p := range f.params {
			if substParams(p, map[string]string{tpNames[k]: "\x00"}) != p {
				return true
			}
		}
		return false
	}
	callee := fmt.Sprintf("Fn%d[%s]", i, strings.Join(ta, ", "))
	switch g.rng.Intn(5) {
	case 0:
		callee = "(" + callee + ")"
	case 1:
		callee = "((" + callee + "))"
	case 2:
		if f.ntp == 2 && occurs(1) {
			callee = fmt.Sprintf("Fn%d[%s]", i, ta[0])
		}
	case 3:
		all := true
		for k := range f.ntp {
			all = all && occurs(k)
		}
		if all {
			callee = fmt.Sprintf("Fn%d", i)
		}
	}
	lhs := "\t"
	if f.result {
		lhs = "\t_ = "
	}
	fmt.Fprintf(&g.b, "%s%s(%s)\n", lhs, callee, strings.Join(args, ", "))
}

func (g *diffGen) program() string {
	g.b.Reset()
	g.vars = 0
	g.types = 1 + g.rng.Intn(3)
	g.ifaces = 1 + g.rng.Intn(2)
	g.funcs = make([]diffFunc, 1+g.rng.Intn(3))
	g.b.WriteString("package p\n\n")
	for i := range g.types {
		field := "[]P"
		if g.rng.Intn(4) == 0 {
			// An instantiation inside a type declaration, behind a slice.
			field = "[]" + g.typeExpr([]string{"P"}, 2)
		}
		fmt.Fprintf(&g.b, "type Gt%d[P any] struct{ F %s }\n\n", i, field)
	}
	for i := range g.ifaces {
		fmt.Fprintf(&g.b, "type Gi%d[P any] interface{ M(%s) }\n\n", i, g.typeExpr([]string{"P"}, 1))
	}
	for i := range g.funcs {
		f := diffFunc{ntp: 1 + g.rng.Intn(2)}
		ps := tpNames[:f.ntp]
		for range g.rng.Intn(3) {
			f.params = append(f.params, g.zeroType(ps, 2))
		}
		f.result = len(f.params) > 0 && g.rng.Intn(2) == 0
		g.funcs[i] = f
	}
	for i, f := range g.funcs {
		ps := tpNames[:f.ntp]
		var tps, params []string
		for _, p := range ps {
			tps = append(tps, p+" any")
		}
		for k, p := range f.params {
			params = append(params, fmt.Sprintf("x%d %s", k, p))
		}
		res := ""
		if f.result {
			res = " " + f.params[0]
		}
		fmt.Fprintf(&g.b, "func Fn%d[%s](%s)%s {\n", i, strings.Join(tps, ", "), strings.Join(params, ", "), res)
		g.stmts(ps, g.rng.Intn(3))
		if f.result {
			g.b.WriteString("\treturn x0\n")
		}
		g.b.WriteString("}\n\n")
	}
	for i := range g.types {
		// The receiver names the type's parameter P, or renames it Q.
		rp := tpNames[g.rng.Intn(2)]
		ps := []string{rp}
		fmt.Fprintf(&g.b, "func (r Gt%d[%s]) Mt%d() {\n", i, rp, i)
		g.stmts(ps, g.rng.Intn(3))
		g.b.WriteString("}\n\n")
		// A parametric signature: it may grow the receiver's own type or
		// another receiver's, in a parameter, a result or an interface.
		pt := g.typeExpr(ps, 2)
		switch g.rng.Intn(3) {
		case 0:
			fmt.Fprintf(&g.b, "func (r Gt%d[%s]) Ms%d(x %s) %s\n\n", i, rp, i, pt, pt)
		case 1:
			fmt.Fprintf(&g.b, "func (r Gt%d[%s]) Ms%d(x %s) %s {\n\treturn x\n}\n\n", i, rp, i, pt, pt)
		default:
			fmt.Fprintf(&g.b, "func (r Gt%d[%s]) Ms%d(x %s) {\n}\n\n", i, rp, i, pt)
		}
	}
	g.b.WriteString("func Main() {\n")
	g.stmts(nil, 1+g.rng.Intn(4))
	g.b.WriteString("}\n")
	return g.b.String()
}

// goKey and ergoKey write a type in one normal form, so the two checkers'
// types compare as strings: package qualifiers and spacing are dropped, and
// a type parameter is written by its index in its declaration's list, so a
// receiver that renames its type's parameter still compares. A generic
// signature lists its parameters' constraints.
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
		return fmt.Sprintf("$%d", t.Index())
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
		var tps, ps, rs []string
		for i := range t.TypeParams().Len() {
			tps = append(tps, goKey(t.TypeParams().At(i))+" "+goKey(t.TypeParams().At(i).Constraint()))
		}
		for i := range t.Params().Len() {
			ps = append(ps, goKey(t.Params().At(i).Type()))
		}
		for i := range t.Results().Len() {
			rs = append(rs, goKey(t.Results().At(i).Type()))
		}
		s := "func(" + strings.Join(ps, ",") + ")(" + strings.Join(rs, ",") + ")"
		if len(tps) > 0 {
			s = "[" + strings.Join(tps, ",") + "]" + s
		}
		return s
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

// ergoKeys writes Ergo types in goKey's normal form; index maps each type
// parameter to its position in its declaration's list.
type ergoKeys struct{ index map[*TypeParam]int }

func newErgoKeys(pkg *Package) ergoKeys {
	k := ergoKeys{index: map[*TypeParam]int{}}
	add := func(tps []*TypeParam) {
		for i, tp := range tps {
			k.index[tp] = i
		}
	}
	for _, o := range pkg.Scope.names {
		switch o := o.(type) {
		case *Func:
			if o.Sig != nil {
				add(o.Sig.TParams)
			}
		case *TypeName:
			if n, ok := o.Type.(*Named); ok {
				add(n.TParams)
				for _, m := range n.mlist {
					if m.Sig != nil {
						add(m.Sig.TParams)
					}
				}
			}
		}
	}
	return k
}

func (k ergoKeys) key(t Type) string {
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
				parts[i] = k.key(a)
			}
			s += "[" + strings.Join(parts, ",") + "]"
		}
		return s
	case *TypeParam:
		if i, ok := k.index[t]; ok {
			return fmt.Sprintf("$%d", i)
		}
		return "?" + t.Name
	case *Slice:
		return "[]" + k.key(t.Elem)
	case *Array:
		return fmt.Sprintf("[%d]%s", t.Len, k.key(t.Elem))
	case *Map:
		return "map[" + k.key(t.Key) + "]" + k.key(t.Elem)
	case *Chan:
		return "chan " + k.key(t.Elem)
	case *Pointer:
		return "*" + k.key(t.Elem)
	case *Signature:
		var tps, ps, rs []string
		for _, tp := range t.TParams {
			cons := "interface{}"
			if tp.Constraint != nil {
				cons = k.key(tp.Constraint)
			}
			tps = append(tps, k.key(tp)+" "+cons)
		}
		for _, p := range t.Params {
			ps = append(ps, k.key(p.Type))
		}
		for _, r := range t.Results {
			rs = append(rs, k.key(r))
		}
		s := "func(" + strings.Join(ps, ",") + ")(" + strings.Join(rs, ",") + ")"
		if len(tps) > 0 {
			s = "[" + strings.Join(tps, ",") + "]" + s
		}
		return s
	case *Struct:
		var fs []string
		for _, f := range t.Fields {
			fs = append(fs, f.Name+" "+k.key(f.Type))
		}
		return "struct{" + strings.Join(fs, ";") + "}"
	case *Interface:
		var ms []string
		for _, m := range t.Methods {
			ms = append(ms, m.Name+k.key(m.Sig))
		}
		sort.Strings(ms)
		return "interface{" + strings.Join(ms, ";") + "}"
	}
	return fmt.Sprintf("?%T", t)
}

// span is an expression's byte range, the key that matches the two ASTs.
type span struct{ from, to int }

// diffOne checks src with both checkers and returns the disagreements; a
// rejection must be an instantiation cycle in both.
func diffOne(src string) []string { return diffCheck(src, true) }

// diffCheck checks src with both checkers and returns the disagreements.
// With cycleOnly, a program both reject must fail by an instantiation
// cycle in both; otherwise any rejection agrees (the inference programs
// fail by unification).
func diffCheck(src string, cycleOnly bool) []string {
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
	var ds []string
	for _, d := range diags {
		ds = append(ds, d.String())
	}
	if (len(goErrs) == 0) != (len(ds) == 0) {
		return []string{fmt.Sprintf("verdicts differ: go %q, ergo %q", goErrs, ds)}
	}
	if len(goErrs) > 0 && !cycleOnly {
		return nil
	}
	if len(goErrs) > 0 {
		// Both reject: the generated programs fail only by instantiation
		// cycles, so each checker must name one, and nothing else.
		var out []string
		for _, e := range goErrs {
			// A tab starts a continuation line of the error before
			// ("P instantiated as []P").
			if !strings.Contains(e, "instantiation cycle") && !strings.HasPrefix(e, "\t") && !strings.Contains(e, ": \t") {
				out = append(out, "go rejects the generated program for another reason (generator bug): "+e)
			}
		}
		for _, d := range ds {
			if !strings.Contains(d, "instantiation cycle") {
				out = append(out, "ergo rejects for another reason: "+d)
			}
		}
		return out
	}
	keys := newErgoKeys(pkg)
	ergoTypes := map[span]string{}
	for x, t := range pkg.Info.Types {
		ergoTypes[span{int(x.Pos()), int(x.End())}] = keys.key(t)
	}
	var out []string
	text := func(sp span) string { return src[sp.from:sp.to] }
	ast.Inspect(file, func(n ast.Node) bool {
		x, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		switch x.(type) {
		case *ast.Ident, *ast.IndexExpr, *ast.IndexListExpr, *ast.ParenExpr, *ast.SelectorExpr, *ast.CallExpr, *ast.CompositeLit:
		default:
			return true
		}
		tv, ok := info.Types[x]
		if !ok {
			return true
		}
		sp := span{fset.Position(x.Pos()).Offset, fset.Position(x.End()).Offset}
		gt := goKey(tv.Type)
		et, ok := ergoTypes[sp]
		switch {
		case !tv.IsValue():
			// A type expression: Ergo need not record it, but a type it
			// records must be the same.
			if ok && tv.IsType() && et != gt {
				out = append(out, fmt.Sprintf("%s (type expression): ergo %s, go %s", text(sp), et, gt))
			}
		case !ok:
			out = append(out, fmt.Sprintf("%s: ergo records no type; go %s", text(sp), gt))
		case et != gt:
			out = append(out, fmt.Sprintf("%s: ergo %s, go %s", text(sp), et, gt))
		}
		return true
	})
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

// TestDifferentialInfoNames: the reviewer's round-4 explicit and inferred
// call forms, checked by the same oracle, so the generic name inside an
// explicit instantiation keeps its generic type (round-4 M3).
func TestDifferentialInfoNames(t *testing.T) {
	for _, body := range []string{
		`func Fn[T any](x T) T { return x }
func F() int { return Fn[int](1) }`,
		`func Fn[T any](x T) T { return x }
func F() int { return ((Fn[int]))(1) }`,
		`func Fn[T any](x T) T { return x }
func F() int { return Fn(1) }`,
		`func Fn[A, B any](a A, b B) B { return b }
func F() string { return Fn[int, string](1, "a") }`,
		`func Fn[A, B any](a A, b B) B { return b }
func F() string { return Fn[int](1, "a") }`,
		`func Fn[T any](x T) T { return x }
func F() func(int) int { return Fn[int] }`,
	} {
		src := "package p\n\n" + body + "\n"
		if d := diffOne(src); len(d) > 0 {
			t.Errorf("%s:\n%s", body, strings.Join(d, "\n"))
		}
	}
	// Go reads the inside of parentheses as a value: a generic function
	// there, not instantiated inside, is an error in both checkers.
	for _, body := range []string{
		`func F() int { return (Fn)[int](1) }`,
		`func F() int { return (Fn)(1) }`,
		`func F() string { return (Fn2[int])(1, "a") }`,
		`func F() { _ = (Fn) }`,
	} {
		src := "package p\n\nfunc Fn[T any](x T) T { return x }\n\nfunc Fn2[A, B any](a A, b B) B { return b }\n\n" + body + "\n"
		_, ds := CheckSource(src)
		if g := goErrors(src); len(g) == 0 || len(ds) == 0 {
			t.Errorf("%s: go %q, ergo %v; both must reject", body, g, ds)
		}
	}
}

// inferProgram writes a program of value-argument inference through
// interfaces (round-5 M1, M3): concrete types whose method M has one of a
// few signatures, value or pointer receivers, some with a second method N;
// a named generic interface, a named wider interface and anonymous
// interfaces; functions whose parameter is an anonymous, a named generic
// or a wider interface; and Two[T](a, b T), called with two arguments in
// either order or with T written (the explicit controls). Every value is a
// parameter of Main, so no zero value is needed. The calls mix matching
// and mismatching arguments, so both verdicts occur.
func inferProgram(rng *rand.Rand) string {
	var b strings.Builder
	b.WriteString("package p\n\n")
	b.WriteString("type I0[T any] interface{ M(T) T }\n\n")
	b.WriteString("type W0 interface {\n\tM(int) int\n\tN()\n}\n\n")
	b.WriteString("type J0 interface{ M(int) int }\n\n")
	results := []string{"int", "string"}
	nS := 1 + rng.Intn(3)
	var values, types []string
	for k := range nS {
		r := results[rng.Intn(2)]
		fmt.Fprintf(&b, "type S%d struct{}\n\n", k)
		recv := fmt.Sprintf("S%d", k)
		if rng.Intn(3) == 0 {
			recv = "*" + recv
		}
		fmt.Fprintf(&b, "func (%s) M(v %s) %s { return v }\n\n", recv, r, r)
		if rng.Intn(2) == 0 {
			fmt.Fprintf(&b, "func (%s) N() {}\n\n", recv)
		}
		values = append(values, fmt.Sprintf("s%d S%d", k, k), fmt.Sprintf("p%d *S%d", k, k))
		// A borrow *T is only a parameter or receiver type in Ergo
		// (types.md, Borrows 1), so no explicit control writes one.
		types = append(types, fmt.Sprintf("S%d", k))
	}
	values = append(values, "w W0", "j J0", "i I0[int]", "a interface{ M(int) int }", "e interface{ M(string) string }", "n int")
	types = append(types, "W0", "J0", "I0[int]", "interface{ M(int) int }", "interface{ M(string) string }", "int")
	b.WriteString("func FnA[T any](x interface{ M(T) T }) {}\n\n")
	b.WriteString("func FnN[T any](x I0[T]) {}\n\n")
	b.WriteString("func FnW[T any](x interface {\n\tM(T) T\n\tN()\n}) {}\n\n")
	b.WriteString("func Two[T any](a T, b T) {}\n\n")
	fmt.Fprintf(&b, "func Main(%s) {\n", strings.Join(values, ", "))
	name := func(v string) string { return v[:strings.Index(v, " ")] }
	for range 1 + rng.Intn(2) {
		v := name(values[rng.Intn(len(values))])
		w := name(values[rng.Intn(len(values))])
		switch rng.Intn(5) {
		case 0:
			fmt.Fprintf(&b, "\tFnA(%s)\n", v)
		case 1:
			fmt.Fprintf(&b, "\tFnN(%s)\n", v)
		case 2:
			fmt.Fprintf(&b, "\tFnW(%s)\n", v)
		case 3:
			fmt.Fprintf(&b, "\tTwo(%s, %s)\n", v, w)
		default:
			fmt.Fprintf(&b, "\tTwo[%s](%s, %s)\n", types[rng.Intn(len(types))], v, w)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// TestDifferentialInference runs the inference generator over a fixed seed
// set: the verdicts agree with go/types, and on accepted programs every
// value expression's type does. Both verdicts must occur.
func TestDifferentialInference(t *testing.T) {
	accepted, rejected := 0, 0
	for seed := int64(0); seed < diffSeeds; seed++ {
		src := inferProgram(rand.New(rand.NewSource(seed)))
		if d := diffCheck(src, false); len(d) > 0 {
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

// FuzzDifferential runs both generators on any seed (go test -fuzz).
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
		src = inferProgram(rand.New(rand.NewSource(seed)))
		if d := diffCheck(src, false); len(d) > 0 {
			t.Fatalf("seed %d (inference):\n%s\n%s", seed, strings.Join(d, "\n"), src)
		}
	})
}
