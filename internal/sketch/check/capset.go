package check

// A capSet is a persistent set of capabilities, keyed by their identity in
// one type table: a treap whose priorities hash the ids, so its shape
// depends on its elements alone. Adding an element copies one path and
// leaves every earlier version intact, so a wire's binding summary shares
// the largest of its parts instead of copying it (#196 round 18).
//
// Every operation of the wire summaries runs on a wireMeter: each node
// visited, each node copied or created and each element enumerated is
// counted before it is done, and the operation stops when the package's
// budget would be passed (D16 as amended in round 18; wire.go). Only the
// membership test of a with scope while a body is checked (has) is not
// wire-summary work and is not metered.
// Prior art: OCaml's Set and Erlang maps share structure between
// versions; TypeScript (TS2589) and rustc (recursion_limit) refuse past a
// deterministic, source-only budget instead of accepting unproven.
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

// A wireMeter counts the wire summaries' work against the package's
// budget. step reports whether n more units fit and counts them; once a
// step does not fit, out is set and every later step fails, so no work
// is done past the budget. visits and copies count, independently of the
// steps, the nodes the treap operations actually visited and copied or
// created: the tests check that each was counted (visits+copies <= used).
type wireMeter struct {
	used, limit    int
	out            bool
	visits, copies int
}

func (m *wireMeter) step(n int) bool {
	if m.out || m.used+n > m.limit {
		m.out = true
		return false
	}
	m.used += n
	return true
}

// has reports whether t is an element: a with scope's membership test
// while a body is checked. Like Row.has, it charges one lookup step.
func (s *capSet) has(tab *typeTable, t Type) bool {
	if s == nil || s.size == 0 {
		return false
	}
	if s.tab != tab {
		panic("capSet: a lookup through another type table; convert it with localCaps")
	}
	tab.charge(1)
	id := tab.id(t)
	for n := s.root; n != nil; {
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

// lookup is has for the wire summaries: one step per node visited. ok is
// false when the budget ran out.
func (s *capSet) lookup(m *wireMeter, id int32) (found, ok bool) {
	if s == nil {
		return false, true
	}
	for n := s.root; n != nil; {
		if !m.step(1) {
			return false, false
		}
		m.visits++
		switch {
		case id < n.id:
			n = n.left
		case id > n.id:
			n = n.right
		default:
			return true, true
		}
	}
	return false, true
}

// with returns s with t added; s is unchanged. ok is false when the
// budget ran out; s is then returned.
func (s *capSet) with(m *wireMeter, tab *typeTable, t Type) (out *capSet, ok bool) {
	if s == nil {
		s = &capSet{}
	}
	if s.tab != nil && s.tab != tab {
		panic("capSet: elements of two type tables")
	}
	id := tab.id(t)
	root, added, ok := capInsert(m, s.root, id, t, capPriority(id))
	if !ok {
		return s, false
	}
	if !added {
		return s, true
	}
	return &capSet{root: root, size: s.size + 1, tab: tab}, true
}

// capInsert returns n with (id, t) added and whether it was added. The
// nodes on the path are copies, so a returned node is never shared and a
// rotation may change it. A visit and a copy are each one step, counted
// first.
func capInsert(m *wireMeter, n *capNode, id int32, t Type, pri uint32) (out *capNode, added, ok bool) {
	if n == nil {
		if !m.step(1) {
			return nil, false, false
		}
		m.copies++
		return &capNode{id: id, t: t, pri: pri}, true, true
	}
	if !m.step(1) {
		return n, false, false
	}
	m.visits++
	if id == n.id {
		return n, false, true
	}
	var child *capNode
	if id < n.id {
		child, added, ok = capInsert(m, n.left, id, t, pri)
	} else {
		child, added, ok = capInsert(m, n.right, id, t, pri)
	}
	if !ok || !added {
		return n, false, ok
	}
	if !m.step(1) {
		return n, false, false
	}
	m.copies++
	c := *n
	if id < n.id {
		c.left = child
		if child.pri > c.pri {
			c.left, child.right = child.right, &c
			return child, true, true
		}
		return &c, true, true
	}
	c.right = child
	if child.pri > c.pri {
		c.right, child.left = child.left, &c
		return child, true, true
	}
	return &c, true, true
}

// each calls f on the elements in id order, one step per node, until f
// returns false. It returns false when the budget ran out or f stopped.
func (s *capSet) each(m *wireMeter, f func(id int32, t Type) bool) bool {
	if s == nil {
		return true
	}
	var walk func(n *capNode) bool
	walk = func(n *capNode) bool {
		for n != nil {
			if !walk(n.left) {
				return false
			}
			if !m.step(1) {
				return false
			}
			m.visits++
			if !f(n.id, n.t) {
				return false
			}
			n = n.right
		}
		return true
	}
	return walk(s.root)
}

// capUnion returns the union of sets of one table, adding the elements of
// all but the largest to the largest: each union costs its smaller sides,
// counted. ok is false when the budget ran out.
func capUnion(m *wireMeter, tab *typeTable, sets []*capSet) (out *capSet, ok bool) {
	largest := -1
	for i, s := range sets {
		if !m.step(1) {
			return nil, false
		}
		if s != nil && s.size > 0 && (largest < 0 || s.size > sets[largest].size) {
			largest = i
		}
	}
	if largest < 0 {
		return &capSet{}, true
	}
	out = sets[largest]
	for i, s := range sets {
		if i == largest {
			continue
		}
		if !s.each(m, func(_ int32, t Type) bool { out, ok = out.with(m, tab, t); return ok }) {
			return nil, false
		}
	}
	return out, true
}
