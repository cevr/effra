package check

// A capSet is a persistent set of capabilities, keyed by their identity in
// one type table: a treap whose priorities hash the ids, so its shape
// depends on its elements alone. Adding an element copies one path,
// O(log n), and leaves every earlier version intact, so a wire's binding
// summary shares its embeddings' summaries instead of copying them: along
// an embedding chain whose every wire adds an entry, the summaries cost
// O(n log n), where a sorted row per wire costs O(n²) (#196 round 18).
// Prior art: Gleam and Elixir share immutable maps between versions
// (im-rc HAMTs, Erlang maps); OCaml's Set is a persistent balanced tree.
type capSet struct {
	root *capNode
	size int
	tab  *typeTable // the table whose ids key the set; nil while empty
}

type capNode struct {
	id          int32
	t           Type
	pri         uint32
	left, right *capNode
}

func capPriority(id int32) uint32 {
	x := uint32(id)*0x9E3779B1 ^ 0x5bd1e995
	x ^= x >> 15
	x *= 0x2c1b3c6d
	x ^= x >> 12
	return x
}

// has reports whether t is an element. Like Row.has, it charges one step.
func (s *capSet) has(tab *typeTable, t Type) bool {
	if s == nil || s.size == 0 {
		return false
	}
	if s.tab != tab {
		panic("capSet: a lookup through another type table; convert it with localCaps")
	}
	tab.charge(1)
	return s.hasID(tab.id(t))
}

func (s *capSet) hasID(id int32) bool {
	n := s.root
	for n != nil {
		switch {
		case id < n.id:
			n = n.left
		case id > n.id:
			n = n.right
		default:
			return true
		}
	}
	return false
}

// with returns s with t added; s is unchanged.
func (s *capSet) with(tab *typeTable, t Type) *capSet {
	if s == nil {
		s = &capSet{}
	}
	if s.tab != nil && s.tab != tab {
		panic("capSet: elements of two type tables")
	}
	id := tab.id(t)
	root, added := capInsert(s.root, id, t, capPriority(id))
	if !added {
		return s
	}
	return &capSet{root: root, size: s.size + 1, tab: tab}
}

// capInsert returns n with (id, t) added and whether it was added. The
// nodes on the path are copies, so a returned node is never shared and a
// rotation may change it.
func capInsert(n *capNode, id int32, t Type, pri uint32) (*capNode, bool) {
	if n == nil {
		return &capNode{id: id, t: t, pri: pri}, true
	}
	if id == n.id {
		return n, false
	}
	m := *n
	if id < n.id {
		l, added := capInsert(n.left, id, t, pri)
		if !added {
			return n, false
		}
		m.left = l
		if l.pri > m.pri {
			m.left, l.right = l.right, &m
			return l, true
		}
		return &m, true
	}
	r, added := capInsert(n.right, id, t, pri)
	if !added {
		return n, false
	}
	m.right = r
	if r.pri > m.pri {
		m.right, r.left = r.left, &m
		return r, true
	}
	return &m, true
}

// each calls f on the elements in id order.
func (s *capSet) each(f func(id int32, t Type)) {
	if s == nil {
		return
	}
	var walk func(n *capNode)
	walk = func(n *capNode) {
		for n != nil {
			walk(n.left)
			f(n.id, n.t)
			n = n.right
		}
	}
	walk(s.root)
}

// capUnion returns the union of sets of one table, adding the elements of
// all but the largest to the largest: each union costs its smaller sides.
// work counts the elements added.
func capUnion(tab *typeTable, sets []*capSet) (out *capSet, work int) {
	largest := -1
	for i, s := range sets {
		if s != nil && s.size > 0 && (largest < 0 || s.size > sets[largest].size) {
			largest = i
		}
	}
	if largest < 0 {
		return &capSet{}, 0
	}
	out = sets[largest]
	for i, s := range sets {
		if i != largest {
			s.each(func(_ int32, t Type) { out = out.with(tab, t); work++ })
		}
	}
	return out, work
}
