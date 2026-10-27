package check

import "effra.local/prototype/internal/sketch/syntax"

// declKinds holds the solved kinds of the unconstrained bracket parameters
// of the package's generic type declarations.
type declKinds struct {
	kinds     map[*TypeName][]Kind
	conflicts map[*TypeName]map[string]bool
}

// kindVar is one bracket parameter of one declaration: a variable of the
// kind constraint system.
type kindVar struct {
	tn *TypeName
	i  int
}

// solveDeclKinds finds the kind of every unconstrained bracket parameter of
// every generic type declaration of the package before any declaration
// resolves (D6; round-2 M4). A parameter's kind follows its positions: in
// error[…] an error row, after the capability-row keyword a capability row,
// in a plain type position a type, and as the argument of a generic type
// whatever that type's parameter is. The last makes the kinds of recursive
// and mutually recursive declarations a system of equalities, solved here
// with union-find over all declarations at once; a parameter that no
// position constrains is a type. A parameter whose positions disagree is a
// conflict (S108 when the declaration resolves).
//
// Prior art: Haskell's kind inference solves a binding group's kinds
// together before checking it; Go has no kind inference, and Koka infers
// row kinds of type parameters from their use the same way.
func (c *checker) solveDeclKinds() {
	parent := map[kindVar]kindVar{}
	var find func(v kindVar) kindVar
	find = func(v kindVar) kindVar {
		p, ok := parent[v]
		if !ok || p == v {
			return v
		}
		r := find(p)
		parent[v] = r
		return r
	}
	seen := map[kindVar]map[Kind]bool{}
	observe := func(v kindVar, k Kind) {
		r := find(v)
		if seen[r] == nil {
			seen[r] = map[Kind]bool{}
		}
		seen[r][k] = true
	}
	union := func(a, b kindVar) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		parent[ra] = rb
		for k := range seen[ra] {
			if seen[rb] == nil {
				seen[rb] = map[Kind]bool{}
			}
			seen[rb][k] = true
		}
		delete(seen, ra)
	}
	// params returns the bracket parameter names of a local generic
	// declaration, in order.
	params := func(tn *TypeName) []string {
		var out []string
		if tn.Decl == nil || tn.Decl.TypeParams == nil {
			return nil
		}
		for _, f := range tn.Decl.TypeParams.List {
			for _, n := range f.Names {
				out = append(out, n.Name)
			}
		}
		return out
	}
	var decls []*TypeName
	for _, tn := range c.pkg.Types {
		if tn.Decl == nil || tn.Decl.TypeParams == nil {
			continue
		}
		decls = append(decls, tn)
		i := 0
		for _, f := range tn.Decl.TypeParams.List {
			for range f.Names {
				v := kindVar{tn, i}
				parent[v] = v
				switch {
				case f.Type != nil && isErrorName(f.Type):
					observe(v, KindErrors)
				case f.Type != nil:
					observe(v, KindType)
				}
				i++
			}
		}
	}
	for _, tn := range decls {
		saved := c.cur
		c.cur = tn.file
		names := params(tn)
		index := map[string]int{}
		for i, n := range names {
			index[n] = i
		}
		self := func(x syntax.Expr) (kindVar, bool) {
			id, ok := unparen(x).(*syntax.Ident)
			if !ok {
				return kindVar{}, false
			}
			i, ok := index[id.Name]
			return kindVar{tn, i}, ok
		}
		var w typeWalker
		w.f = func(id *syntax.Ident, k Kind) {
			if i, ok := index[id.Name]; ok {
				observe(kindVar{tn, i}, k)
			}
		}
		// index resolves an index expression's head: a local generic
		// declaration links each bare parameter argument to the head's
		// parameter; any other generic type gives its resolved kinds.
		w.index = func(ix *syntax.IndexExpr) bool {
			htn, hn := c.namedHead(ix.X)
			if hn == nil || htn.Pkg != c.pkg || htn.Decl == nil || htn.Decl.TypeParams == nil {
				return false
			}
			w.walk(ix.X, KindType)
			for i, a := range ix.Indices {
				slot := kindVar{htn, i}
				if _, ok := parent[slot]; !ok {
					w.walk(a, KindType)
					continue
				}
				args := []syntax.Expr{a}
				if b, ok := unparen(a).(*syntax.BinaryExpr); ok && b.Op == syntax.OR {
					args = []syntax.Expr{b.X, b.Y}
				}
				for _, arg := range args {
					if v, ok := self(arg); ok {
						union(v, slot)
					} else if len(args) == 1 {
						w.walk(arg, KindType)
					}
				}
			}
			return true
		}
		w.slots = c.slotKinds
		w.walk(tn.Decl.Spec, KindType)
		c.cur = saved
	}
	c.kinds = declKinds{kinds: map[*TypeName][]Kind{}, conflicts: map[*TypeName]map[string]bool{}}
	for _, tn := range decls {
		names := params(tn)
		ks := make([]Kind, len(names))
		for i, name := range names {
			s := seen[find(kindVar{tn, i})]
			switch {
			case len(s) > 1:
				if c.kinds.conflicts[tn] == nil {
					c.kinds.conflicts[tn] = map[string]bool{}
				}
				c.kinds.conflicts[tn][name] = true
			case len(s) == 1:
				for k := range s {
					ks[i] = k
				}
			default:
				ks[i] = KindType
			}
		}
		c.kinds.kinds[tn] = ks
	}
}
