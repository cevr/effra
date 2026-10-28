package check

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// RefWalkProblems checks src as one package with the class H reference walk
// traced (round 16). It reports every body node the walk did not visit, and
// every identifier the walk resolved differently from typing: typing
// resolves a walked identifier to a package function or var exactly when
// the walk does, and to the same object. An identifier typing resolves to
// a package function that the walk skipped is reported too; a skipped
// composite-literal key that typing resolves to a package var is a map key
// (refs.go), not an edge.
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
	c.run([]*syntax.File{file})
	var out []string
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
			if want := pkgObject(u); want != o {
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
			if f, ok := pkgObject(u).(*Func); ok {
				out = append(out, fmt.Sprintf("%s at %d: typing resolves the package function %s, the walk skipped it", id.Name, id.Pos(), f.Name))
			}
		}
	}
	sort.Strings(out)
	return out
}

// PermuteDecls returns src with its top-level declarations after the
// imports in a random order (seed), each on its own line, and a function
// that maps an offset of either source to its declaration's index and the
// offset inside it (-1 and the offset itself outside every declaration).
// ok is false when src does not parse or has fewer than two declarations
// to permute.
func PermuteDecls(src string, seed uint64) (out string, origKey, permKey func(int) (int, int), ok bool) {
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
	_, base := CheckSource(src)
	var out []string
	for seed := uint64(1); seed <= uint64(n); seed++ {
		permuted, origKey, permKey, ok := PermuteDecls(src, seed)
		if !ok {
			return nil
		}
		_, diags := CheckSource(permuted)
		want, have := orderKeys(base, origKey), orderKeys(diags, permKey)
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

// duplicatesOf keeps the duplicate keys and the verdict.
func duplicatesOf(keys []string, rejected bool) []string {
	out := []string{fmt.Sprintf("rejected %v", rejected)}
	for _, k := range keys {
		if strings.Contains(k, " duplicate ") {
			out = append(out, k)
		}
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

func orderKeys(diags []Diagnostic, key func(int) (int, int)) []string {
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
		out = append(out, fmt.Sprintf("%d+%d %s %s", decl, rel, d.Code, d.Message))
	}
	sort.Strings(out)
	return out
}
