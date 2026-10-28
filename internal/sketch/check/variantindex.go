package check

import (
	"sort"
	"strconv"
	"strings"
)

// variantIndex finds a union's variant by instance identity (round-7 M4,
// round 9 class C; D16: nothing before the solvers' counters is
// superlinear). It is a discrimination tree over the variants' types,
// built once per origin union and shared by every instance of it: each
// variant is read in preorder as a path of type constructors (a named
// origin and its arity, a basic type, a slice, an array length, a
// signature's parameter and result counts, ...). Every edge is hashed:
//   - a constructor edge is keyed by the constructor;
//   - a variable edge, for a type parameter of the union's declaration, is
//     keyed by the parameter. A lookup reaches it through the instance's
//     bound identities: the parameters bound to a type identical to the
//     case's sub-term, indexed once per instance by identity key;
//   - a row-pattern edge, for a row that mentions a declaration parameter
//     (error[E] in func(A) error[E]), is keyed by the pattern. A lookup
//     reaches it through the bound row's identity: the instance's rows,
//     indexed once per instance and node by their keys;
//   - a wildcard edge, for a type the tree does not key (a union, a
//     machine, an interface listing a method twice), confirmed by one
//     identity check.
//
// The edges mirror identical (typeops.go), so a leaf reached without a
// wildcard is the variant identical to the case. As a safety net, every
// operation of a case lookup is charged to the switch's step counter
// (lookupSteps) and every insertion to the union's own counter; past
// totalityBudget the switch is S103, as the solvers are (rows.md,
// Totality 5). So no uncounted pass runs before the solvers.
type variantIndex struct {
	root *vnode
	// first is the first variant of each generic origin, for a case that
	// names the generic type without type arguments.
	first map[*Named]int
	// steps counts the insertion work, charged to the union; over means it
	// ran past totalityBudget, and every lookup in the index is refused.
	steps int
	over  bool
}

type vnode struct {
	next    map[vsym]*vnode
	vars    map[*TypeParam]*vnode
	rows    map[string]*vrow
	rowList []*vrow // the row patterns in insertion order (determinism)
	wild    *vnode
	leaves  []int
}

// vrow is a row-pattern edge: a row that mentions a declaration parameter.
type vrow struct {
	row  *Row
	node *vnode
}

// vsym is a type constructor: identical types have the same constructor
// and pairwise identical sub-terms (typeops.go identical).
type vsym struct {
	kind byte
	ref  Type   // the origin of a named type, the basic type
	n    int64  // arity, array length, channel direction, comparable
	s    string // field or method names, counts and flags, row keys
}

const (
	vNamed byte = iota + 1
	vBasic
	vSlice
	vArray
	vMap
	vChan
	vPointer
	vTuple
	vSig
	vStruct
	vIface
	vRow
)

// lookupSteps is a switch's step counter for resolving its cases: every
// operation of the variant index and of the case lookups is charged to it,
// against totalityBudget. ops also feeds the tests' variantOps count.
type lookupSteps struct {
	steps int
	over  bool
	ops   *int
}

func (b *lookupSteps) charge(w int) bool {
	*b.ops += w
	if b.over {
		return false
	}
	b.steps += w
	if b.steps > totalityBudget {
		b.over = true
		return false
	}
	return true
}

// index returns the union's variant index, built on first use. Its
// insertion work is charged to the index's own counter (and to ops).
func (u *Union) index(ops *int) *variantIndex {
	if u.vindex != nil {
		return u.vindex
	}
	ix := &variantIndex{root: &vnode{}, first: map[*Named]int{}}
	u.vindex = ix
	for i, v := range u.Variants {
		vn, ok := v.(*Named)
		if !ok {
			continue
		}
		if _, seen := ix.first[vn.origin()]; !seen {
			ix.first[vn.origin()] = i
		}
		end := ix.insert(ix.root, v, ops)
		if ix.over {
			return ix
		}
		end.leaves = append(end.leaves, i)
	}
	return ix
}

// insert adds t's path below n and returns the node it ends at.
func (ix *variantIndex) insert(n *vnode, t Type, ops *int) *vnode {
	*ops++
	ix.steps++
	if ix.steps > totalityBudget {
		ix.over = true
		return n
	}
	if tp, ok := t.(*TypeParam); ok {
		if n.vars == nil {
			n.vars = map[*TypeParam]*vnode{}
		}
		child := n.vars[tp]
		if child == nil {
			child = &vnode{}
			n.vars[tp] = child
		}
		return child
	}
	if ra, ok := t.(*RowArg); ok && ix.rowMentions(ra.Row, ops) {
		key := rowKey(ra.Row)
		if n.rows == nil {
			n.rows = map[string]*vrow{}
		}
		vr := n.rows[key]
		if vr == nil {
			vr = &vrow{row: ra.Row, node: &vnode{}}
			n.rows[key] = vr
			n.rowList = append(n.rowList, vr)
		}
		return vr.node
	}
	sym, kids, ok := symOf(t)
	if !ok {
		if n.wild == nil {
			n.wild = &vnode{}
		}
		return n.wild
	}
	if n.next == nil {
		n.next = map[vsym]*vnode{}
	}
	child := n.next[sym]
	if child == nil {
		child = &vnode{}
		n.next[sym] = child
	}
	for _, k := range kids {
		child = ix.insert(child, k, ops)
		if ix.over {
			return child
		}
	}
	return child
}

// rowMentions reports whether a row mentions a declaration parameter. The
// test walks the row's elements: each visited node, and the key join per
// element, is charged to the union's insertion counter (round-9 M1).
func (ix *variantIndex) rowMentions(r *Row, ops *int) bool {
	before := ix.steps
	mentions := rowHasParam(r, &ix.steps)
	ix.steps += len(r.Elems())
	*ops += ix.steps - before
	if ix.steps > totalityBudget {
		ix.over = true
	}
	return mentions
}

// vcand is a variant a case may name: exact when its path took no
// wildcard, so it is identical to the case.
type vcand struct {
	i     int
	exact bool
}

// vterms is the pending sub-terms of a case, a persistent stack.
type vterms struct {
	t    Type
	next *vterms
}

// candidates returns the variants whose path matches t, in variant order,
// and false when the switch's step counter ran out. Every step is charged
// before it is taken, in proportion to its work: a node visit and its
// constructor's sub-terms, the identity of a sub-term (identOf, per new
// type node), an identity check (the size of the case's sub-term), a bound
// row index (per row element).
func (s *switchUnion) candidates(ix *variantIndex, t Type, b *lookupSteps) ([]vcand, bool) {
	var out []vcand
	var walk func(n *vnode, p *vterms, exact bool)
	walk = func(n *vnode, p *vterms, exact bool) {
		if !b.charge(1) {
			return
		}
		if p == nil {
			for _, i := range n.leaves {
				if !b.charge(1) {
					return
				}
				out = append(out, vcand{i, exact})
			}
			return
		}
		if len(n.vars) > 0 {
			tps, ok := s.boundTo(p.t, b)
			if !ok {
				return
			}
			for _, tp := range tps {
				child := n.vars[tp]
				if child == nil {
					continue
				}
				// The identities are equal: identical walks the case's
				// sub-term once, charged by its size.
				if !b.charge(s.c.identSize(p.t)) {
					return
				}
				if identical(s.bind(tp), p.t) {
					walk(child, p.next, exact)
				}
			}
		}
		if len(n.rowList) > 0 {
			if ra, ok := p.t.(*RowArg); ok {
				idx, ok := s.rowsAt(n, b)
				if !ok || !b.charge(1+len(ra.Row.Elems())) {
					return
				}
				for _, child := range idx[rowKey(ra.Row)] {
					walk(child, p.next, exact)
				}
			}
		}
		if n.wild != nil {
			walk(n.wild, p.next, false)
		}
		if sym, kids, ok := symOf(p.t); ok {
			if !b.charge(len(kids)) {
				return
			}
			if child := n.next[sym]; child != nil {
				rest := p.next
				for k := len(kids) - 1; k >= 0; k-- {
					rest = &vterms{kids[k], rest}
				}
				walk(child, rest, exact)
			}
		}
	}
	walk(ix.root, &vterms{t, nil}, true)
	if b.over {
		return nil, false
	}
	if len(out) > 1 {
		sort.Slice(out, func(a, c int) bool { return out[a].i < out[c].i })
	}
	return out, true
}

// boundTo returns the declaration parameters whose bound type may be
// identical to t: those with t's identity, from the instance's bound
// identities. They are indexed once per instance (the switchUnion is shared
// by every subject of the instance, subjectUnion) and each identity is
// interned once per type (identOf). Outside an instance a parameter stands
// for itself. ok is false when the step counter ran out.
func (s *switchUnion) boundTo(t Type, b *lookupSteps) ([]*TypeParam, bool) {
	if s.m == nil {
		if tp, ok := t.(*TypeParam); ok {
			return []*TypeParam{tp}, true
		}
		return nil, true
	}
	if s.bound == nil {
		bound := map[int][]*TypeParam{}
		for _, tp := range s.tps {
			a, ok := s.m[tp]
			if !ok {
				continue
			}
			id, ok := s.c.identOf(a, b)
			if !ok || !b.charge(1) {
				return nil, false
			}
			bound[id] = append(bound[id], tp)
		}
		s.bound = bound
	}
	id, ok := s.c.identOf(t, b)
	if !ok {
		return nil, false
	}
	return s.bound[id], true
}

// rowsAt returns node n's row-pattern edges indexed by the identity of
// the rows the instance binds them to, built once per instance and node and
// charged per row element. ok is false when the step counter ran out.
func (s *switchUnion) rowsAt(n *vnode, b *lookupSteps) (map[string][]*vnode, bool) {
	if idx, ok := s.rows[n]; ok {
		return idx, true
	}
	idx := map[string][]*vnode{}
	for _, vr := range n.rowList {
		// Substituting the pattern costs its elements' sizes, interned
		// (and charged) first.
		w := 1
		for _, e := range vr.row.Elems() {
			if _, ok := s.c.identOf(e, b); !ok {
				return nil, false
			}
			w += s.c.identSize(e)
		}
		if !b.charge(w) {
			return nil, false
		}
		r := vr.row
		if s.m != nil {
			r = s.c.substRow(r, s.m)
		}
		k := rowKey(r)
		idx[k] = append(idx[k], vr.node)
	}
	if s.rows == nil {
		s.rows = map[*vnode]map[string][]*vnode{}
	}
	s.rows[n] = idx
	return idx, true
}

// rowKey is a row's identity: its sorted element keys (Row.equal).
func rowKey(r *Row) string { return strings.Join(r.keysOrNil(), "|") }

// identTable interns type identities for the whole check (round-9 M1):
// each type node gets a compact id, computed once and memoized on the type
// (by its pointer), from its constructor and its sub-terms' ids, so a
// node's key is O(its arity), never the string of a deep type. Identical
// types get the same id; the types the tree does not key share one id,
// confirmed by identical. size is the number of nodes, the cost of an
// identity check against the type.
type identTable struct {
	of   map[Type]identEntry
	keys map[string]int
}

type identEntry struct {
	id, size int
}

// identOf returns t's interned identity. A type seen before costs nothing;
// each new node is charged to b (one step plus its sub-terms) before its
// key is built, so a deep type costs its depth, once per check. ok is
// false when the step counter ran out.
func (c *checker) identOf(t Type, b *lookupSteps) (int, bool) {
	if e, ok := c.idents.of[t]; ok {
		return e.id, true
	}
	if c.idents.of == nil {
		c.idents.of = map[Type]identEntry{}
		c.idents.keys = map[string]int{}
	}
	var key string
	size := 1
	switch t := t.(type) {
	case *TypeParam:
		if !b.charge(1) {
			return 0, false
		}
		key = "$" + strconv.Itoa(t.id)
	default:
		sym, kids, ok := symOf(t)
		w := 1 + len(kids)
		if ra, isRow := t.(*RowArg); isRow {
			w += len(ra.Row.Elems()) // its key joins the elements' keys
		}
		if !b.charge(w) {
			return 0, false
		}
		if !ok {
			key = "?"
			break
		}
		var sb strings.Builder
		sb.WriteByte(sym.kind)
		switch r := sym.ref.(type) {
		case *Named:
			sb.WriteString(typeKey(r)) // the origin: no type arguments, O(1)
		case *Basic:
			sb.WriteString(strconv.Itoa(int(r.Kind)))
		}
		sb.WriteString("/" + strconv.FormatInt(sym.n, 10) + "/" + sym.s + "(")
		for i, kid := range kids {
			id, ok := c.identOf(kid, b)
			if !ok {
				return 0, false
			}
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(strconv.Itoa(id))
			size += c.idents.of[kid].size
		}
		sb.WriteByte(')')
		key = sb.String()
	}
	id, ok := c.idents.keys[key]
	if !ok {
		id = len(c.idents.keys) + 1
		c.idents.keys[key] = id
	}
	c.idents.of[t] = identEntry{id, size}
	return id, true
}

// identSize is the size of a type whose identity identOf interned: the
// cost of comparing or substituting it. It reads the memo only; callers
// intern the type first.
func (c *checker) identSize(t Type) int {
	if e, ok := c.idents.of[t]; ok {
		return e.size
	}
	return 1
}

// symOf returns t's constructor and sub-terms, or false for a type the
// tree does not key: a type parameter, a union, a machine, an interface
// listing a method twice. A signature's sub-terms are its parameters, its
// results and its two rows; a row is a constructor of its keys (Row.equal),
// and a row that mentions a declaration parameter becomes a row-pattern
// edge when inserted.
func symOf(t Type) (vsym, []Type, bool) {
	switch t := t.(type) {
	case *Named:
		return vsym{kind: vNamed, ref: t.origin(), n: int64(len(t.TArgs))}, t.TArgs, true
	case *Basic:
		return vsym{kind: vBasic, ref: t}, nil, true
	case *Slice:
		return vsym{kind: vSlice}, []Type{t.Elem}, true
	case *Array:
		return vsym{kind: vArray, n: t.Len}, []Type{t.Elem}, true
	case *Map:
		return vsym{kind: vMap}, []Type{t.Key, t.Elem}, true
	case *Chan:
		return vsym{kind: vChan, n: int64(t.Dir)}, []Type{t.Elem}, true
	case *Pointer:
		return vsym{kind: vPointer}, []Type{t.Elem}, true
	case *Tuple:
		return vsym{kind: vTuple, n: int64(len(t.Types))}, t.Types, true
	case *RowArg:
		return vsym{kind: vRow, s: rowKey(t.Row)}, nil, true
	case *Signature:
		kids := make([]Type, 0, len(t.Params)+len(t.Results)+2)
		for _, p := range t.Params {
			kids = append(kids, p.Type)
		}
		kids = append(kids, t.Results...)
		kids = append(kids, &RowArg{Row: t.Errors}, &RowArg{Row: t.Uses})
		s := strconv.Itoa(len(t.Params)) + "," + strconv.Itoa(len(t.Results)) + "," + strconv.FormatBool(t.Variadic)
		return vsym{kind: vSig, s: s}, kids, true
	case *Struct:
		names := make([]string, len(t.Fields))
		kids := make([]Type, len(t.Fields))
		for i, f := range t.Fields {
			names[i], kids[i] = f.Name, f.Type
		}
		return vsym{kind: vStruct, s: strings.Join(names, ",")}, kids, true
	case *Interface:
		// identical matches methods by name: the sub-terms are the method
		// signatures in name order. Duplicate names are left to the
		// identity check.
		ms := make([]*Method, len(t.Methods))
		copy(ms, t.Methods)
		sort.Slice(ms, func(a, b int) bool { return ms[a].Name < ms[b].Name })
		names := make([]string, len(ms))
		kids := make([]Type, len(ms))
		for i, m := range ms {
			if i > 0 && ms[i-1].Name == m.Name || m.Sig == nil {
				return vsym{}, nil, false
			}
			names[i], kids[i] = m.Name, m.Sig
		}
		cmp := int64(0)
		if t.Comparable {
			cmp = 1
		}
		return vsym{kind: vIface, n: cmp, s: strings.Join(names, ",")}, kids, true
	}
	return vsym{}, nil, false
}

// rowHasParam reports whether a row element mentions a type parameter,
// adding each visited type node to steps (the union's insertion counter).
func rowHasParam(r *Row, steps *int) bool {
	for _, e := range r.Elems() {
		if hasParam(e, steps) {
			return true
		}
	}
	return false
}

// hasParam reports whether t mentions a type parameter, adding each visited
// type node to steps.
func hasParam(t Type, steps *int) bool {
	*steps++
	switch t := t.(type) {
	case *TypeParam:
		return true
	case *Named:
		for _, a := range t.TArgs {
			if hasParam(a, steps) {
				return true
			}
		}
	case *RowArg:
		return rowHasParam(t.Row, steps)
	case *Slice:
		return hasParam(t.Elem, steps)
	case *Array:
		return hasParam(t.Elem, steps)
	case *Map:
		return hasParam(t.Key, steps) || hasParam(t.Elem, steps)
	case *Chan:
		return hasParam(t.Elem, steps)
	case *Pointer:
		return hasParam(t.Elem, steps)
	case *Tuple:
		for _, e := range t.Types {
			if hasParam(e, steps) {
				return true
			}
		}
	case *Struct:
		for _, f := range t.Fields {
			if hasParam(f.Type, steps) {
				return true
			}
		}
	case *Signature:
		for _, p := range t.Params {
			if hasParam(p.Type, steps) {
				return true
			}
		}
		for _, r := range t.Results {
			if hasParam(r, steps) {
				return true
			}
		}
		return rowHasParam(t.Errors, steps) || rowHasParam(t.Uses, steps)
	case *Interface:
		for _, m := range t.Methods {
			if m.Sig != nil && hasParam(m.Sig, steps) {
				return true
			}
		}
	case *Union:
		for _, v := range t.Variants {
			if hasParam(v, steps) {
				return true
			}
		}
	case *MachineType:
		return hasParam(t.State, steps) || hasParam(t.Signal, steps)
	}
	return false
}
