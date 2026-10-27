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
	if ra, ok := t.(*RowArg); ok && rowHasParam(ra.Row) {
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

// lookup returns the variants whose path matches t, in variant order, and
// false when the switch's step counter ran out.
func (s *switchUnion) candidates(ix *variantIndex, t Type, b *lookupSteps) ([]vcand, bool) {
	var out []vcand
	keys := map[Type]string{}
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
			for _, tp := range s.boundTo(p.t, keys, b) {
				child := n.vars[tp]
				if child == nil || !b.charge(1) {
					continue
				}
				if identical(s.bind(tp), p.t) {
					walk(child, p.next, exact)
				}
			}
		}
		if len(n.rowList) > 0 {
			if ra, ok := p.t.(*RowArg); ok {
				for _, child := range s.rowsAt(n, b)[rowKey(ra.Row)] {
					walk(child, p.next, exact)
				}
			}
		}
		if n.wild != nil {
			walk(n.wild, p.next, false)
		}
		if sym, kids, ok := symOf(p.t); ok {
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
// identical to t: those with t's identity key, from the instance's bound
// identities, indexed once. Outside an instance a parameter stands for
// itself.
func (s *switchUnion) boundTo(t Type, keys map[Type]string, b *lookupSteps) []*TypeParam {
	if s.m == nil {
		if tp, ok := t.(*TypeParam); ok {
			return []*TypeParam{tp}
		}
		return nil
	}
	if s.bound == nil {
		s.bound = map[string][]*TypeParam{}
		for _, tp := range s.tps {
			if a, ok := s.m[tp]; ok {
				b.charge(1)
				k := idKey(a, nil)
				s.bound[k] = append(s.bound[k], tp)
			}
		}
	}
	k, ok := keys[t]
	if !ok {
		k = idKey(t, keys)
	}
	return s.bound[k]
}

// rowsAt returns node n's row-pattern edges indexed by the identity of
// the rows the instance binds them to, built once per instance and node.
func (s *switchUnion) rowsAt(n *vnode, b *lookupSteps) map[string][]*vnode {
	if idx, ok := s.rows[n]; ok {
		return idx
	}
	if s.rows == nil {
		s.rows = map[*vnode]map[string][]*vnode{}
	}
	idx := map[string][]*vnode{}
	for _, vr := range n.rowList {
		if !b.charge(1) {
			break
		}
		r := vr.row
		if s.m != nil {
			r = s.c.substRow(r, s.m)
		}
		k := rowKey(r)
		idx[k] = append(idx[k], vr.node)
	}
	s.rows[n] = idx
	return idx
}

// rowKey is a row's identity: its sorted element keys (Row.equal).
func rowKey(r *Row) string { return strings.Join(r.keysOrNil(), "|") }

// idKey is an identity key: identical types have the same key (types the
// tree does not key share one), so a hash lookup by it, confirmed by
// identical, finds every identical type. memo caches the keys of one
// lookup's sub-terms; nil skips it.
func idKey(t Type, memo map[Type]string) string {
	if k, ok := memo[t]; ok {
		return k
	}
	var k string
	switch t := t.(type) {
	case *TypeParam:
		k = "$" + strconv.Itoa(t.id)
	default:
		sym, kids, ok := symOf(t)
		if !ok {
			k = "?"
			break
		}
		var sb strings.Builder
		sb.WriteByte(sym.kind)
		switch r := sym.ref.(type) {
		case *Named:
			sb.WriteString(typeKey(r))
		case *Basic:
			sb.WriteString(strconv.Itoa(int(r.Kind)))
		}
		sb.WriteString("/" + strconv.FormatInt(sym.n, 10) + "/" + sym.s + "(")
		for i, kid := range kids {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(idKey(kid, memo))
		}
		sb.WriteByte(')')
		k = sb.String()
	}
	if memo != nil {
		memo[t] = k
	}
	return k
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

// rowHasParam reports whether a row element mentions a type parameter.
func rowHasParam(r *Row) bool {
	for _, e := range r.Elems() {
		if hasParam(e) {
			return true
		}
	}
	return false
}

// hasParam reports whether t mentions a type parameter.
func hasParam(t Type) bool {
	switch t := t.(type) {
	case *TypeParam:
		return true
	case *Named:
		for _, a := range t.TArgs {
			if hasParam(a) {
				return true
			}
		}
	case *RowArg:
		return rowHasParam(t.Row)
	case *Slice:
		return hasParam(t.Elem)
	case *Array:
		return hasParam(t.Elem)
	case *Map:
		return hasParam(t.Key) || hasParam(t.Elem)
	case *Chan:
		return hasParam(t.Elem)
	case *Pointer:
		return hasParam(t.Elem)
	case *Tuple:
		for _, e := range t.Types {
			if hasParam(e) {
				return true
			}
		}
	case *Struct:
		for _, f := range t.Fields {
			if hasParam(f.Type) {
				return true
			}
		}
	case *Signature:
		for _, p := range t.Params {
			if hasParam(p.Type) {
				return true
			}
		}
		for _, r := range t.Results {
			if hasParam(r) {
				return true
			}
		}
		return rowHasParam(t.Errors) || rowHasParam(t.Uses)
	case *Interface:
		for _, m := range t.Methods {
			if m.Sig != nil && hasParam(m.Sig) {
				return true
			}
		}
	case *Union:
		for _, v := range t.Variants {
			if hasParam(v) {
				return true
			}
		}
	case *MachineType:
		return hasParam(t.State) || hasParam(t.Signal)
	}
	return false
}
