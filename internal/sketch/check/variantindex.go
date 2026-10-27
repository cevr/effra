package check

import (
	"sort"
	"strconv"
	"strings"
)

// variantIndex finds a union's variant by instance identity (round-7 M4;
// D16: nothing before the solvers' counters is superlinear). It is a
// discrimination tree over the variants' types, built once per origin
// union and shared by every instance of it: each variant is read in
// preorder as a path of type constructors (a named origin and its arity,
// a basic type, a slice, an array length, ...). A type parameter of the
// union's declaration is a variable edge, matched against the instance's
// type argument; a constructor the tree does not key (a union, a machine,
// a row or signature that mentions a type parameter) is a wildcard edge,
// confirmed by one identity check.
//
// A case type follows its own path: at each node it takes the constructor
// edge its next sub-term names, and each variable edge whose bound type
// argument is identical to that sub-term. So a case resolves in time
// proportional to its own size, plus the variants that substitute to the
// same type, never by scanning an origin's variants or substituting them.
// The edges mirror identical, so a leaf reached without a wildcard is the
// variant identical to the case.
type variantIndex struct {
	root *vnode
	// first is the first variant of each generic origin, for a case that
	// names the generic type without type arguments.
	first map[*Named]int
}

type vnode struct {
	next   map[vsym]*vnode
	vars   []vvar
	wild   *vnode
	leaves []int
}

type vvar struct {
	tp   *TypeParam
	node *vnode
}

// vsym is a type constructor: identical types have the same constructor
// and pairwise identical sub-terms (typeops.go identical).
type vsym struct {
	kind byte
	ref  Type   // the origin of a named type, the basic type
	n    int64  // arity, array length, channel direction, comparable
	s    string // field or method names, variadic flag and row keys
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

// index returns the union's variant index, built on first use. ops counts
// the constructors inserted, for the tests' linear bound.
func (u *Union) index(ops *int) *variantIndex {
	if u.vindex != nil {
		return u.vindex
	}
	ix := &variantIndex{root: &vnode{}, first: map[*Named]int{}}
	for i, v := range u.Variants {
		vn, ok := v.(*Named)
		if !ok {
			continue
		}
		if _, seen := ix.first[vn.origin()]; !seen {
			ix.first[vn.origin()] = i
		}
		end := ix.insert(ix.root, v, ops)
		end.leaves = append(end.leaves, i)
	}
	u.vindex = ix
	return ix
}

// insert adds t's path below n and returns the node it ends at.
func (ix *variantIndex) insert(n *vnode, t Type, ops *int) *vnode {
	*ops++
	if tp, ok := t.(*TypeParam); ok {
		for _, v := range n.vars {
			if v.tp == tp {
				return v.node
			}
		}
		child := &vnode{}
		n.vars = append(n.vars, vvar{tp, child})
		return child
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

// lookup returns the variants whose path matches t, in variant order.
// bind gives a declaration's type parameter its instance's type argument.
func (ix *variantIndex) lookup(t Type, bind func(*TypeParam) Type, ops *int) []vcand {
	var out []vcand
	var walk func(n *vnode, p *vterms, exact bool)
	walk = func(n *vnode, p *vterms, exact bool) {
		*ops++
		if p == nil {
			for _, i := range n.leaves {
				out = append(out, vcand{i, exact})
			}
			return
		}
		for _, v := range n.vars {
			*ops++
			if identical(bind(v.tp), p.t) {
				walk(v.node, p.next, exact)
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
	if len(out) > 1 {
		sort.Slice(out, func(a, b int) bool { return out[a].i < out[b].i })
	}
	return out
}

// symOf returns t's constructor and sub-terms, or false for a type the
// tree does not key: a type parameter, a union, a machine, and a row,
// signature or interface whose identity is not decided by its sub-terms.
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
		if rowHasParam(t.Row) {
			return vsym{}, nil, false
		}
		return vsym{kind: vRow, s: strings.Join(t.Row.keys, "|")}, nil, true
	case *Signature:
		// Rows compare by their keys (Row.equal); a row that mentions a
		// type parameter is left to the identity check.
		if rowHasParam(t.Errors) || rowHasParam(t.Uses) {
			return vsym{}, nil, false
		}
		kids := make([]Type, 0, len(t.Params)+len(t.Results))
		for _, p := range t.Params {
			kids = append(kids, p.Type)
		}
		kids = append(kids, t.Results...)
		s := strconv.Itoa(len(t.Params)) + "," + strconv.Itoa(len(t.Results)) + "," + strconv.FormatBool(t.Variadic) +
			"e{" + strings.Join(t.Errors.keysOrNil(), "|") + "}u{" + strings.Join(t.Uses.keysOrNil(), "|") + "}"
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
