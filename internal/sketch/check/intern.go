package check

import (
	"encoding/binary"
	"sort"
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
// each type object is interned once.
type typeTable struct {
	of      map[Type]internEntry // memo by type object
	nodes   map[nodeKey]int32    // structure -> id
	sizes   []int32              // sizes[id]: the number of nodes, saturated
	foreign map[*Row]*Row        // rows of another table, re-interned here
	empty   int32                // the empty row's id

	// epoch advances whenever the rows of a signature under inference
	// change (rows.go). A memo entry that depends on such a signature is
	// valid only in the epoch it was made in; every other entry is stable.
	epoch int32

	b *lookupSteps
	// lookupBuilds counts the nodes built while b was set (the tests'
	// measure of identity work inside a lookup).
	lookupBuilds int
}

type internEntry struct {
	id    int32
	epoch int32 // 0: stable
}

// nodeKey is one node of a type: its constructor and its children's ids.
type nodeKey struct {
	kind byte
	ref  any    // a named origin, a basic type, a type parameter, or the type itself
	n    int64  // arity, array length, channel direction, comparable
	s    string // field or method names, counts and flags
	kids string // the children's ids, 4 bytes each
}

// The constructors beyond the variant index's (variantindex.go).
const (
	vParam byte = iota + 100
	vFresh      // a type identical only to itself: a union, a machine
)

// maxSize saturates node counts: a type that shares subterms can have a
// tree exponentially larger than its graph.
const maxSize = 1 << 30

func newTypeTable() *typeTable {
	return &typeTable{
		of:      map[Type]internEntry{},
		nodes:   map[nodeKey]int32{},
		sizes:   []int32{0},
		foreign: map[*Row]*Row{},
		epoch:   1,
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

// changed records that a signature under inference got new rows.
func (tab *typeTable) changed() { tab.epoch++ }

// id returns t's identity. Identical types (typeops.go) have equal ids.
func (tab *typeTable) id(t Type) int32 {
	id, _ := tab.intern(t)
	return id
}

// size is the number of nodes of t (saturated): the cost of substituting it.
func (tab *typeTable) size(t Type) int {
	return int(tab.sizes[tab.id(t)])
}

func (tab *typeTable) intern(t Type) (int32, bool) {
	if t == nil {
		return 0, false
	}
	if ra, ok := t.(*RowArg); ok {
		return tab.rowID(ra.Row), false
	}
	if e, ok := tab.of[t]; ok && (e.epoch == 0 || e.epoch == tab.epoch) {
		return e.id, e.epoch != 0
	}
	kind, ref, n, s, kids := shape(t)
	sig, isSig := t.(*Signature)
	id, unstable := tab.build(kind, ref, n, s, kids, isSig && sig.inferred)
	tab.remember(t, id, unstable)
	return id, unstable
}

// named returns the id of the instance of orig with targs, without building
// the instance: the instance cache looks it up first (decl.go instantiate).
func (tab *typeTable) named(orig *Named, targs []Type) (int32, bool) {
	return tab.build(vNamed, orig, int64(len(targs)), "", targs, false)
}

// build returns the id of a node from its constructor and its children,
// interning the children first. The node is charged before its key is
// built. unstable reports an id valid in this epoch only.
func (tab *typeTable) build(kind byte, ref any, n int64, s string, kids []Type, unstable bool) (int32, bool) {
	tab.charge(1 + len(kids))
	if tab.b != nil {
		tab.lookupBuilds++
	}
	buf := make([]byte, 4*len(kids))
	size := int64(1)
	for i, kid := range kids {
		id, u := tab.intern(kid)
		unstable = unstable || u
		binary.LittleEndian.PutUint32(buf[4*i:], uint32(id))
		size += int64(tab.sizes[id])
	}
	return tab.node(nodeKey{kind: kind, ref: ref, n: n, s: s, kids: string(buf)}, size), unstable
}

// remember memoizes t's id on the type object.
func (tab *typeTable) remember(t Type, id int32, unstable bool) {
	e := internEntry{id: id}
	if unstable {
		e.epoch = tab.epoch
	}
	tab.of[t] = e
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
		ms := make([]*Method, len(t.Methods))
		copy(ms, t.Methods)
		sort.Slice(ms, func(a, b int) bool { return ms[a].Name < ms[b].Name })
		names := make([]string, len(ms))
		kids := make([]Type, len(ms))
		for i, m := range ms {
			if i > 0 && ms[i-1].Name == m.Name || m.Sig == nil {
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

// rowID is a row's identity: the set of its elements' ids.
func (tab *typeTable) rowID(r *Row) int32 {
	if r.Empty() {
		if tab.empty == 0 {
			tab.empty = tab.node(nodeKey{kind: vRow}, 1)
		}
		return tab.empty
	}
	r = tab.local(r)
	if r.rid != 0 {
		return r.rid
	}
	tab.charge(1 + len(r.ids))
	buf := make([]byte, 4*len(r.ids))
	size := int64(1)
	for i, id := range r.ids {
		binary.LittleEndian.PutUint32(buf[4*i:], uint32(id))
		size += int64(tab.sizes[id])
	}
	r.rid = tab.node(nodeKey{kind: vRow, kids: string(buf)}, size)
	return r.rid
}

// local returns r with ids of this table: r itself when it was built here,
// else its elements re-interned here once.
func (tab *typeTable) local(r *Row) *Row {
	if r.Empty() || r.tab == tab {
		return r
	}
	if l, ok := tab.foreign[r]; ok {
		return l
	}
	l := tab.row(r.elems...)
	tab.foreign[r] = l
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
