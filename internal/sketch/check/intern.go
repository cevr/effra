package check

import (
	"cmp"
	"encoding/binary"
	"slices"
	"strconv"
	"strings"
)

// typeTable is the check's hash-consing table (class E): the one identity
// the checker uses to compare, cache and index types. A type's id comes
// from its constructor and its children's ids, one hash lookup per node, so
// identical types share one id and a node built from interned children
// costs O(its own fields). No whole type is ever serialized to compare or
// key it (typeString is for printing only).
//
// Interned types never change (class F). A function whose row is inferred
// has a row variable in its signature (RowVar); inference writes the solved
// row into the variable, never into a type. An id that depends on an
// unbound variable (one whose component is still being inferred) is kept
// only in the component's inference context: the scoped memos below, the
// checker's scoped instances and switch subjects, all discarded when a
// component is final (rustc's has_infer rule). A final variable is bound
// for good: an id resolves it to its solution and is memoized for the whole
// check. No memo is ever invalidated.
//
// The table lives on the checker, never in a package variable: ids follow
// the order in which this check first meets each type, so they depend on
// the source alone (D16). A row of another check (the prelude's, a std
// package's) is re-interned here on first use and never written to.
//
// While a switch resolves its cases, b is the switch's step counter: each
// new node is charged to it before it is built, and so is each row
// operation, so no identity work in a lookup is uncounted (rows.md,
// Totality 5). While a union's variant index is built, b is the union's
// counter. Elsewhere interning is linear in the types the check builds:
// each type object is interned once (once more after its component is
// final, when it held an unbound variable).
type typeTable struct {
	of       map[Type]int32    // memo by type object: ids no unbound variable affects
	nodes    map[nodeKey]int32 // structure -> id
	sizes    []int32           // sizes[id]: the number of nodes, saturated
	foreign  map[*Row]*Row     // rows of another table, or with variables now final, re-interned here
	resolved map[*Row]*Row     // rows whose variables are all final, resolved
	empty    int32             // the empty row's id

	// The component's inference context: ids and rows that depend on an
	// unbound variable. Cleared when a component is final.
	scoped     map[Type]int32
	scopedRows map[*Row]*Row

	// gen counts the finished components: an infer row built in an earlier
	// generation has stale ids.
	gen int32

	b *lookupSteps
	// lookupBuilds counts the nodes built while b was set (the tests'
	// measure of identity work inside a lookup).
	lookupBuilds int
	// sigBuilds counts the fields of signature nodes built (the tests'
	// measure of signature identity work, round 11 M2); sortCmps counts the
	// comparisons of sorts run while a counter is attached (round 11 M3).
	sigBuilds, sortCmps int
}

// nodeKey is one node of a type: its constructor and its children's ids.
type nodeKey struct {
	kind byte
	ref  any    // a named origin, a basic type, a type or row variable, or the type itself
	n    int64  // arity, array length, channel direction, comparable
	s    string // field or method names, counts and flags
	kids string // the children's ids, 4 bytes each
}

// The constructors beyond the variant index's (variantindex.go).
const (
	vParam byte = iota + 100
	vFresh      // a type identical only to itself: a union, a machine
	vVar        // a row variable (class F)
)

// maxSize saturates node counts: a type that shares subterms can have a
// tree exponentially larger than its graph.
const maxSize = 1 << 30

func newTypeTable() *typeTable {
	return &typeTable{
		of:         map[Type]int32{},
		nodes:      map[nodeKey]int32{},
		sizes:      []int32{0},
		foreign:    map[*Row]*Row{},
		resolved:   map[*Row]*Row{},
		scoped:     map[Type]int32{},
		scopedRows: map[*Row]*Row{},
	}
}

// charge charges w steps to the running lookup, if any.
func (tab *typeTable) charge(w int) {
	if tab.b != nil {
		tab.b.charge(w)
	}
}

// attach makes b the counter of the identity work that follows, and returns
// a function that restores the previous counter.
func (tab *typeTable) attach(b *lookupSteps) func() {
	prev := tab.b
	tab.b = b
	return func() { tab.b = prev }
}

// final binds a component's row variables for good and discards the
// inference context: every id that depended on an unbound variable.
func (tab *typeTable) final(vars []*RowVar) {
	for _, v := range vars {
		v.final = true
	}
	clear(tab.scoped)
	clear(tab.scopedRows)
	tab.gen++
}

// id returns t's identity. Identical types (typeops.go) have equal ids.
func (tab *typeTable) id(t Type) int32 {
	id, _ := tab.intern(t)
	return id
}

// size is the number of nodes of t (saturated): the cost of substituting it.
func (tab *typeTable) size(t Type) int {
	return int(tab.sizes[tab.id(t)])
}

// intern returns t's id, and whether it depends on an unbound variable (the
// id then holds only until that variable's component is final).
func (tab *typeTable) intern(t Type) (int32, bool) {
	switch t := t.(type) {
	case nil:
		return 0, false
	case *RowArg:
		return tab.rowID(t.Row)
	case *RowVar:
		// The variable itself, bound or not: a row resolves a final one
		// (rowID).
		return tab.node(nodeKey{kind: vVar, ref: t}, 1), false
	}
	if id, ok := tab.of[t]; ok {
		return id, false
	}
	if id, ok := tab.scoped[t]; ok {
		return id, true
	}
	kind, ref, n, s, kids := shape(t)
	if kind == vSig {
		tab.sigBuilds += 1 + len(kids)
	}
	id, infer := tab.build(kind, ref, n, s, kids)
	tab.remember(t, id, infer)
	return id, infer
}

// named returns the id of the instance of orig with targs, without building
// the instance: the instance cache looks it up first (decl.go instantiate).
func (tab *typeTable) named(orig *Named, targs []Type) (int32, bool) {
	return tab.build(vNamed, orig, int64(len(targs)), "", targs)
}

// build returns the id of a node from its constructor and its children,
// interning the children first. The node is charged before its key is
// built.
func (tab *typeTable) build(kind byte, ref any, n int64, s string, kids []Type) (int32, bool) {
	tab.charge(1 + len(kids))
	if tab.b != nil {
		tab.lookupBuilds++
	}
	buf := make([]byte, 4*len(kids))
	size := int64(1)
	infer := false
	for i, kid := range kids {
		id, inf := tab.intern(kid)
		infer = infer || inf
		binary.LittleEndian.PutUint32(buf[4*i:], uint32(id))
		size += int64(tab.sizes[id])
	}
	return tab.node(nodeKey{kind: kind, ref: ref, n: n, s: s, kids: string(buf)}, size), infer
}

// remember memoizes t's id on the type object: for the check, or for the
// component's inference when the id depends on an unbound variable.
func (tab *typeTable) remember(t Type, id int32, infer bool) {
	if infer {
		tab.scoped[t] = id
		return
	}
	tab.of[t] = id
}

// node returns the id of a node, adding it when new.
func (tab *typeTable) node(k nodeKey, size int64) int32 {
	if id, ok := tab.nodes[k]; ok {
		return id
	}
	id := int32(len(tab.sizes))
	tab.nodes[k] = id
	if size > maxSize {
		size = maxSize
	}
	tab.sizes = append(tab.sizes, int32(size))
	return id
}

// shape returns t's constructor and children, mirroring identity (typeops.go
// identical): a named type is its origin and its type arguments; a struct
// its field names and types in order; an interface its method names in
// order and their signatures; a signature its parameters, results and two
// rows (not its fallibility or its own type parameters); a type parameter,
// a basic type, a union and a machine are identical only to themselves.
// No case sorts: an interface's methods were sorted once, when its type
// was resolved (byName).
func shape(t Type) (kind byte, ref any, n int64, s string, kids []Type) {
	switch t := t.(type) {
	case *Named:
		return vNamed, t.origin(), int64(len(t.TArgs)), "", t.TArgs
	case *Basic:
		return vBasic, t, 0, "", nil
	case *TypeParam:
		return vParam, t, 0, "", nil
	case *Slice:
		return vSlice, nil, 0, "", []Type{t.Elem}
	case *Array:
		return vArray, nil, t.Len, "", []Type{t.Elem}
	case *Map:
		return vMap, nil, 0, "", []Type{t.Key, t.Elem}
	case *Chan:
		return vChan, nil, int64(t.Dir), "", []Type{t.Elem}
	case *Pointer:
		return vPointer, nil, 0, "", []Type{t.Elem}
	case *Tuple:
		return vTuple, nil, int64(len(t.Types)), "", t.Types
	case *Signature:
		kids := make([]Type, 0, len(t.Params)+len(t.Results)+2)
		for _, p := range t.Params {
			kids = append(kids, p.Type)
		}
		kids = append(kids, t.Results...)
		kids = append(kids, &RowArg{Row: t.Errors}, &RowArg{Row: t.Uses})
		s := strconv.Itoa(len(t.Params)) + "," + strconv.Itoa(len(t.Results)) + "," + strconv.FormatBool(t.Variadic)
		return vSig, nil, 0, s, kids
	case *Struct:
		names := make([]string, len(t.Fields))
		kids := make([]Type, len(t.Fields))
		for i, f := range t.Fields {
			names[i], kids[i] = f.Name, f.Type
		}
		return vStruct, nil, int64(len(t.Fields)), strings.Join(names, ","), kids
	case *Interface:
		if len(t.byName) != len(t.Methods) {
			// Not built by interfaceType or subst: keep it to itself.
			return vFresh, t, 0, "", nil
		}
		names := make([]string, len(t.byName))
		kids := make([]Type, len(t.byName))
		for i, m := range t.byName {
			if i > 0 && t.byName[i-1].Name == m.Name || m.Sig == nil {
				// The checker never builds one (a second method of a
				// name is reported and dropped): keep it to itself.
				return vFresh, t, 0, "", nil
			}
			names[i], kids[i] = m.Name, m.Sig
		}
		cmp := int64(0)
		if t.Comparable {
			cmp = 1
		}
		return vIface, nil, cmp, strings.Join(names, ","), kids
	}
	// A union, a machine, no value.
	return vFresh, t, 0, "", nil
}

// ---------------------------------------------------------------- rows

// rowID is a row's identity: the set of its elements' ids, a final row
// variable replaced by its solution. It depends on an unbound variable when
// the row holds one, or an element whose id does.
func (tab *typeTable) rowID(r *Row) (int32, bool) {
	if r.Empty() {
		if tab.empty == 0 {
			tab.empty = tab.node(nodeKey{kind: vRow}, 1)
		}
		return tab.empty, false
	}
	r = tab.local(r)
	if r.rid != 0 {
		return r.rid, false
	}
	ids, infer := r.ids, r.infer
	if r.vars {
		ids = make([]int32, 0, len(r.ids))
		for i, e := range r.elems {
			v, ok := e.(*RowVar)
			switch {
			case !ok:
				ids = append(ids, r.ids[i])
			case v.final:
				sol := tab.local(v.sol)
				if sol != nil {
					ids = append(ids, sol.ids...)
				}
			default:
				ids = append(ids, r.ids[i])
				infer = true
			}
		}
		tab.sortIDs(ids)
		ids = slices.Compact(ids)
		if len(ids) == 0 {
			id, _ := tab.rowID(nil)
			if !infer {
				r.rid = id
			}
			return id, infer
		}
	}
	tab.charge(1 + len(ids))
	buf := make([]byte, 4*len(ids))
	size := int64(1)
	for i, id := range ids {
		binary.LittleEndian.PutUint32(buf[4*i:], uint32(id))
		size += int64(tab.sizes[id])
	}
	id := tab.node(nodeKey{kind: vRow, kids: string(buf)}, size)
	if !infer {
		r.rid = id
	}
	return id, infer
}

// sortIDs sorts the ids of a row whose final variables were replaced by
// their solutions. Inside a lookup its comparisons are charged.
func (tab *typeTable) sortIDs(ids []int32) {
	if tab.b == nil {
		slices.Sort(ids)
		return
	}
	slices.SortFunc(ids, func(a, b int32) int {
		tab.sortCmps++
		tab.charge(1)
		return cmp.Compare(a, b)
	})
}

// local returns r with ids of this table that are current: r itself when it
// was built here and no element's id waits on an unbound variable, else its
// elements re-interned here, once for the check (a row of another check, or
// one whose variables are now final) or once for the component's inference.
func (tab *typeTable) local(r *Row) *Row {
	if r.Empty() || r.tab == tab && (!r.infer || r.gen == tab.gen) {
		return r
	}
	if l, ok := tab.foreign[r]; ok {
		return l
	}
	if l, ok := tab.scopedRows[r]; ok {
		return l
	}
	l := tab.row(r.elems...)
	if l.infer {
		tab.scopedRows[r] = l
	} else {
		tab.foreign[r] = l
	}
	return l
}

// row builds the row of elems.
func (tab *typeTable) row(elems ...Type) *Row {
	r := newRow()
	for _, e := range elems {
		r = r.with(tab, e)
	}
	return r
}

// set is r as a set question reads it (has, minus, subsetOf, equal): row
// variables resolved, ids of this table. with and union build rows and keep
// the variables.
func (tab *typeTable) set(r *Row) *Row { return tab.local(tab.resolve(r)) }

// resolve returns r with each row variable replaced by its solution so far:
// the row a body charges or a call escapes with, which holds no variable.
// A row whose variables are all final is resolved once for the check.
func (tab *typeTable) resolve(r *Row) *Row {
	if r == nil || !r.vars {
		return r
	}
	if out, ok := tab.resolved[r]; ok {
		return out
	}
	out := newRow()
	final := true
	for _, e := range r.elems {
		if v, ok := e.(*RowVar); ok {
			final = final && v.final
			out = out.union(tab, v.sol)
			continue
		}
		out = out.with(tab, e)
	}
	if final {
		tab.resolved[r] = out
	}
	return out
}
