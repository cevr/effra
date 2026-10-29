package check

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// RefWalkProblems checks src as one package with the class H reference walk
// traced (round 16). It reports every body node the walk did not visit, and
// every identifier the walk resolved differently from typing: typing
// resolves a walked identifier to a package function or var exactly when
// the walk does, and to the same object, except a composite-literal key the
// walk took as a value because it could not decide the literal's kind
// (reflits.go: conservative, an extra edge). An identifier typing resolves
// to a package function or var that the walk skipped is reported too,
// unless the walk saw it in a type or wire position and the checker
// reports an error there (a function used as a type).
func RefWalkProblems(src string) []string {
	file, err := syntax.Parse(src)
	if err != nil {
		return nil
	}
	c := newChecker("main", Std, prelude().scope)
	type walk struct {
		root syntax.Node
		w    *refWalker
	}
	var walks []walk
	refTrace = func(root syntax.Node, w *refWalker) {
		if w.c == c {
			walks = append(walks, walk{root, w})
		}
	}
	defer func() { refTrace = nil }()
	// Typed self-call edges (round 6 review M1): every call x.f(…) the
	// reporting pass resolves to a 196m function with inferred rows was
	// resolved to that function before the bodies were ordered (by the
	// walk from declarations, or by resolveSelfCalls), and a source with
	// no diagnostic leaves no call unresolved.
	var waited []*syntax.SelectorExpr
	var pre map[*syntax.SelectorExpr]*Func
	c.selfFinal = map[*syntax.SelectorExpr]*Func{}
	selfTrace = func(cc *checker, pending []*syntax.SelectorExpr, resolved map[*syntax.SelectorExpr]*Func) {
		if cc == c {
			waited, pre = pending, resolved
		}
	}
	defer func() { selfTrace = nil }()
	c.run([]*syntax.File{file})
	var out []string
	for x, fn := range c.selfFinal {
		if !fn.inferErrors && !fn.inferUses {
			continue
		}
		if slices.Contains(waited, x) && pre[x] != fn {
			out = append(out, fmt.Sprintf("%s at %d: typing resolves %s after the bodies were ordered, resolveSelfCalls gave %v", exprText(x), x.Pos(), depName(fn), pre[x]))
		}
	}
	if len(c.diags) == 0 {
		for _, x := range waited {
			if _, ok := pre[x]; !ok {
				out = append(out, fmt.Sprintf("%s at %d: a call of a source with no diagnostic that typing never resolved", exprText(x), x.Pos()))
			}
		}
	}
	pkgObject := func(o Object) any {
		switch o := o.(type) {
		case *Func:
			if c.pkg.Scope.names[o.Name] == Object(o) {
				return o
			}
		case *Var:
			if o.pkg {
				if pv := c.pkgVarOf(o); pv != nil {
					return pv
				}
			}
		}
		return nil
	}
	for _, wk := range walks {
		for _, x := range wk.w.retired {
			if fn, ok := c.selfFinal[x]; ok {
				out = append(out, fmt.Sprintf("%s at %d: the walk retired the call (its receiver names nothing), typing resolves %s", exprText(x), x.Pos(), depName(fn)))
			}
		}
		syntax.Inspect(wk.root, func(n syntax.Node) bool {
			if !wk.w.visited[n] {
				out = append(out, fmt.Sprintf("%T at %d: not visited by the reference walk", n, n.Pos()))
			}
			return true
		})
		for id, o := range wk.w.idents {
			u, ok := c.pkg.Info.Uses[id]
			if !ok {
				continue
			}
			if want := pkgObject(u); want != o && !(wk.w.undecided[id] && want == nil) {
				out = append(out, fmt.Sprintf("%s at %d: the walk resolves %v, typing %v", id.Name, id.Pos(), o, want))
			}
		}
		for id, u := range c.pkg.Info.Uses {
			if !wk.w.visited[id] {
				continue
			}
			if _, walked := wk.w.idents[id]; walked {
				continue
			}
			o := pkgObject(u)
			if o == nil {
				continue
			}
			if wk.w.nameIdents[id] && diagnosedAt(c.diags, int(id.Pos())) {
				// A function in a type or wire position (`A{}`, `with(A)`) is
				// an error there: not a value reference, no edge.
				continue
			}
			out = append(out, fmt.Sprintf("%s at %d: typing resolves the package object %s, the walk skipped it", id.Name, id.Pos(), id.Name))
		}
	}
	sort.Strings(out)
	return out
}

func diagnosedAt(diags []Diagnostic, off int) bool {
	for _, d := range diags {
		if d.Offset == off {
			return true
		}
	}
	return false
}

// PermuteDecls returns src with its top-level declarations after the
// imports in a random order (seed), each on its own line, and a function
// that maps an offset of either source to its declaration's index and the
// offset inside it (-1 and the offset itself outside every declaration).
// ok is false when src does not parse or has fewer than two declarations
// to permute.
func PermuteDecls(src string, seed uint64) (out string, origKey, permKey func(int) (int, int), ok bool) {
	return permuteDecls(src, seed, nil)
}

// PermuteWires is PermuteDecls for the wire declarations alone: they trade
// places among themselves, and every other declaration keeps its place.
func PermuteWires(src string, seed uint64) (out string, origKey, permKey func(int) (int, int), ok bool) {
	return permuteDecls(src, seed, func(d syntax.Decl) bool {
		_, ok := d.(*syntax.WireDecl)
		return ok
	})
}

// permuteDecls permutes the declarations that moves selects (every one
// when moves is nil) among their own places.
func permuteDecls(src string, seed uint64, moves func(syntax.Decl) bool) (out string, origKey, permKey func(int) (int, int), ok bool) {
	file, err := syntax.Parse(src)
	if err != nil {
		return "", nil, nil, false
	}
	var decls []syntax.Decl
	for _, d := range file.Decls {
		if _, imp := d.(*syntax.ImportDecl); imp {
			continue
		}
		decls = append(decls, d)
	}
	if len(decls) < 2 {
		return "", nil, nil, false
	}
	type span struct{ start, end int }
	orig := make([]span, len(decls))
	for i, d := range decls {
		orig[i] = span{int(d.Pos()), int(d.End())}
		if orig[i].start < 0 || orig[i].end > len(src) || orig[i].start >= orig[i].end {
			return "", nil, nil, false
		}
	}
	text := func(s span) string { return src[s.start:s.end] }
	header := src[:orig[0].start]
	order := rand.New(rand.NewPCG(seed, seed^0x0de5)).Perm(len(decls))
	if moves != nil {
		var slots []int
		for i, d := range decls {
			if moves(d) {
				slots = append(slots, i)
			}
		}
		if len(slots) < 2 {
			return "", nil, nil, false
		}
		order = make([]int, len(decls))
		for i := range order {
			order[i] = i
		}
		for i, j := range rand.New(rand.NewPCG(seed, seed^0x0de5)).Perm(len(slots)) {
			order[slots[i]] = slots[j]
		}
	}
	var b strings.Builder
	b.WriteString(header)
	perm := make([]span, len(decls))
	for _, i := range order {
		b.WriteString("\n")
		start := b.Len()
		b.WriteString(text(orig[i]))
		perm[i] = span{start, b.Len()}
	}
	b.WriteString("\n")
	key := func(spans []span) func(int) (int, int) {
		return func(off int) (int, int) {
			for i, s := range spans {
				if off >= s.start && off < s.end {
					return i, off - s.start
				}
			}
			return -1, off
		}
	}
	return b.String(), key(orig), key(perm), true
}

// OrderProblems checks src and permutations of its top-level declarations
// (seeds 1…n) and reports every permutation whose diagnostics, mapped back
// to their declarations, differ from the original's (round 16: a verdict
// does not depend on declaration order). An initialization cycle is
// reported at its first var in source order (Go's rule), so it is compared
// by its code and its sorted vars, not its position. An instantiation cycle
// is compared by count, and a source with a duplicate declaration by its
// verdict and duplicates (duplicatesOf).
func OrderProblems(src string, n int) []string {
	return orderProblems(src, n, PermuteDecls, func(s string) []Diagnostic { _, d := CheckSource(s); return d })
}

// orderProblems is OrderProblems for one permutation family and one way
// of checking.
func orderProblems(src string, n int, permute func(string, uint64) (string, func(int) (int, int), func(int) (int, int), bool), check func(string) []Diagnostic) []string {
	base := check(src)
	var out []string
	for seed := uint64(1); seed <= uint64(n); seed++ {
		permuted, origKey, permKey, ok := permute(src, seed)
		if !ok {
			return nil
		}
		diags := check(permuted)
		want, have := orderKeys(base, src, origKey), orderKeys(diags, permuted, permKey)
		if hasDuplicate(base) {
			// The kept declaration depends on the order: compare the verdict
			// and the duplicates only.
			want, have = duplicatesOf(want, len(base) > 0), duplicatesOf(have, len(diags) > 0)
		}
		if strings.Join(want, "\n") != strings.Join(have, "\n") {
			out = append(out, fmt.Sprintf("seed %d: diagnostics differ\noriginal: %v\npermuted: %v\n%s", seed, want, have, permuted))
		}
	}
	return out
}

func hasDuplicate(diags []Diagnostic) bool {
	for _, d := range diags {
		if _, ok := duplicateName(d.Message); ok {
			return true
		}
	}
	return false
}

// duplicatesOf keeps the verdict and the names of the duplicate package
// declarations, once each. A duplicate method is left out: whether its
// receiver names a type depends on which declaration is kept.
func duplicatesOf(keys []string, rejected bool) []string {
	out := []string{fmt.Sprintf("rejected %v", rejected)}
	seen := map[string]bool{}
	for _, k := range keys {
		_, name, ok := strings.Cut(k, " duplicate ")
		if !ok || strings.Contains(name, ".") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// duplicateName returns the name of a package-level duplicate declaration
// diagnostic. Go reports the later declaration, and the earlier one is the
// declaration the package keeps, so its position, and what follows from
// the kept declaration, depend on the order by definition.
func duplicateName(msg string) (string, bool) {
	for _, suffix := range []string{" is already declared in this package", " is already declared as a type in this package"} {
		if name, ok := strings.CutSuffix(msg, suffix); ok {
			return name, true
		}
	}
	for _, prefix := range []string{"wire ", "method "} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			if name, ok := strings.CutSuffix(rest, " is declared twice"); ok && (prefix == "wire " || strings.Contains(name, ".")) {
				return name, true
			}
		}
	}
	return "", false
}

func orderKeys(diags []Diagnostic, src string, key func(int) (int, int)) []string {
	var out []string
	for _, d := range diags {
		if d.Code == CodeSyntax {
			continue
		}
		if name, ok := duplicateName(d.Message); ok {
			out = append(out, fmt.Sprintf("%s duplicate %s", d.Code, name))
			continue
		}
		if i := strings.Index(d.Message, "refers to itself through "); i >= 0 {
			names := strings.Split(strings.TrimSuffix(d.Message[i+len("refers to itself through "):], " (Go's initialization order)"), ", ")
			sort.Strings(names)
			out = append(out, fmt.Sprintf("%s initialization cycle %s", d.Code, strings.Join(names, ",")))
			continue
		}
		if strings.HasPrefix(d.Message, "instantiation cycle: ") {
			// One diagnostic per cycle, at the edge where the walk of the
			// instantiation graph closes it, which follows the order (as in
			// go/types): compared by count.
			out = append(out, d.Code+" instantiation cycle")
			continue
		}
		decl, rel := key(d.Offset)
		out = append(out, fmt.Sprintf("%d+%d %s %s", decl, rel, d.Code, mapUsePosition(d.Message, src, key)))
	}
	sort.Strings(out)
	return out
}

// usePosition is the value use a class G S108 names (rows.go reportGroup).
var usePosition = regexp.MustCompile(` at (\d+):(\d+) inside its own recursive group; `)

// mapUsePosition maps the line:column of a class G value use in msg to its
// declaration and offset, as the diagnostic's own position.
func mapUsePosition(msg, src string, key func(int) (int, int)) string {
	m := usePosition.FindStringSubmatchIndex(msg)
	if m == nil {
		return msg
	}
	line, _ := strconv.Atoi(msg[m[2]:m[3]])
	col, _ := strconv.Atoi(msg[m[4]:m[5]])
	off := 0
	for range line - 1 {
		nl := strings.IndexByte(src[off:], '\n')
		if nl < 0 {
			break
		}
		off += nl + 1
	}
	decl, rel := key(off + col - 1)
	return msg[:m[2]] + fmt.Sprintf("%d+%d", decl, rel) + msg[m[5]:]
}
