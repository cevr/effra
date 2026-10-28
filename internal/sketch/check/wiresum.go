package check

import (
	"fmt"
	"math/bits"
	"sort"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Wire summaries under a counted budget (D16 as amended in round 18).
//
// Wire checking asks batch reachability questions over the embedding
// graph: which capabilities each wire reaches, and whether two entries for
// one capability reach one wire (S111). Their answers can be quadratic in
// the source (a ladder of disjoint chains), and batch reachability has no
// known linear algorithm, so no exact checker is linear on every input.
// The summaries therefore run on one deterministic budget per package,
// wireBudgetPerUnit × wireSize, counted before each unit of work
// (capset.go wireMeter). When the next step would pass it, the checker
// stops summarising: the wire it was working on gets S111
// (wireBudgetMessage), and every wire or body summarised after it adds no
// diagnostic of its own and binds an unknown set, so a with of it
// discharges everything (D3). The checker never accepts a wire it has not
// proved, as S103 never accepts a switch it has not proved total.
// Prior art: TypeScript stops instantiation after a fixed count (TS2589)
// and rustc refuses past recursion_limit and type_length_limit; both are
// deterministic, source-only budgets that refuse.

// wireBudgetPerUnit is K, the work allowed per unit of wire source: 128.
// One summary step is a treap node visited, copied or created, an element
// enumerated, or a probe. A capability added along a chain costs about
// 2·log2(n) + 2 steps for its path (visit and copy), and it is added a few
// times (its wire's own set, its component's union, a body summary), so a
// chain, a fan-in or a large base with a chain of 8000 wires uses 15 to
// 19% of the budget (TestWireBudget, measured). A small program pays a
// constant per wire, so K must also leave the corpus room: its largest
// share is 5.8% at 128 and was 11.6% at 64. The corpus, the fixtures and
// the examples use under 10% of their budget (TestWireBudgetHeadroom).
const wireBudgetPerUnit = 128

// wireBudgetMessage is the S111 of a wire whose summary passed the budget.
const wireBudgetMessage = "this wire's embedding graph is too large to check for duplicate entries; split it"

// meter returns the package's wire meter. Its budget is fixed by the
// source alone, at first use.
func (c *checker) meter() *wireMeter {
	if c.wires == nil {
		c.wires = &wireMeter{limit: wireBudgetPerUnit * c.wireSize()}
	}
	return c.wires
}

// wireSize is the size the wire budget is proportional to: the wire
// entries and embeddings of every wire declaration and literal, the
// capability names written in wire keys and in needs rows, and one.
func (c *checker) wireSize() int {
	size := 1
	for _, f := range c.files {
		syntax.Inspect(f.syntax, func(n syntax.Node) bool {
			switch n := n.(type) {
			case *syntax.WireEntry:
				size++
				if n.Kind != syntax.WireEmbed {
					size += 1 + len(n.Keys)
				}
			case *syntax.NeedsClause:
				size += len(n.Names)
			}
			return true
		})
	}
	return size
}

// wireBinds returns the capabilities a named wire binds: its entries and
// those of every wire its embeddings reach (wireSummaries); nil when its
// summary was refused.
func (c *checker) wireBinds(w *Wire) *capSet {
	c.wireSummaries()
	return w.binds
}

// A wireComp is a strongly connected component of the embedding graph and
// its binding summary. Two wires of one component lie on an embedding
// cycle, and so does a wire that embeds itself.
type wireComp struct {
	binds   *capSet // nil when refused
	unknown bool
}

// wireSummaries computes, once, every named wire's binding summary over
// the embedding graph: the strongly connected components (Tarjan) in
// reverse topological order, each component's summary the union of its
// members' own entries and its successors' summaries, shared by every
// member and every embedding. The result does not depend on which wire is
// asked first (round 17 M1; an embedding cycle is reported by
// checkWireCycles). Every step is metered; past the budget the wire being
// summarised is marked (Wire.budget, reported by checkWires) and every
// wire not summarised yet binds an unknown set.
// Prior art: go/types orders package initialization over its dependency
// graph once (initorder.go).
func (c *checker) wireSummaries() {
	if c.wireComp != nil {
		return
	}
	c.wireComp = map[*Wire]*wireComp{}
	m := c.meter()
	wires := make([]*Wire, 0, len(c.pkg.Wires))
	for _, w := range c.pkg.Wires {
		wires = append(wires, w)
	}
	sort.Slice(wires, func(i, j int) bool {
		if wires[i].file.index != wires[j].file.index {
			return wires[i].file.index < wires[j].file.index
		}
		return wires[i].Pos < wires[j].Pos
	})
	stopped := false
	refuse := func(w *Wire) {
		if !stopped {
			stopped, w.budget = true, true
		}
	}
	type node struct {
		index, low int
		onStack    bool
		embeds     []*Wire
	}
	nodes := make(map[*Wire]*node, len(wires))
	own := func(w *Wire, n *node) bool {
		saved := c.cur
		c.cur = w.file
		c.silent++
		defer func() { c.silent--; c.cur = saved }()
		set, known := &capSet{}, true
		for _, e := range w.Decl.Body.Entries {
			if !m.step(1) {
				return false
			}
			if e.Kind == syntax.WireEmbed {
				var inner *Wire
				if id, ok := e.Key.(*syntax.Ident); ok {
					inner = w.pkg.Wires[id.Name]
				}
				if inner == nil {
					known = false
					continue
				}
				n.embeds = append(n.embeds, inner)
				continue
			}
			for _, k := range append([]syntax.Expr{e.Key}, e.Keys...) {
				if t := c.rowElem(k, KindUses, nil); t != nil {
					if _, isParam := t.(*TypeParam); !isParam {
						var ok bool
						if set, ok = set.with(m, c.types, t); !ok {
							return false
						}
					}
				}
			}
		}
		w.own, w.unknown = set, !known
		return true
	}
	for _, w := range wires {
		n := &node{index: -1}
		nodes[w] = n
		if !stopped && !own(w, n) {
			refuse(w)
		}
	}
	index := 0
	var stack []*Wire
	var connect func(w *Wire)
	connect = func(w *Wire) {
		n := nodes[w]
		n.index, n.low = index, index
		index++
		stack = append(stack, w)
		n.onStack = true
		for _, e := range n.embeds {
			if stopped || !m.step(1) {
				refuse(w)
				return
			}
			d := nodes[e]
			if d.index < 0 {
				connect(e)
				if stopped {
					return
				}
				n.low = min(n.low, d.low)
			} else if d.onStack {
				n.low = min(n.low, d.index)
			}
		}
		if n.low != n.index {
			return
		}
		comp := &wireComp{}
		var members []*Wire
		for {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			nodes[v].onStack = false
			c.wireComp[v] = comp
			members = append(members, v)
			if v == w {
				break
			}
		}
		// The summary joins the members' entries and each successor
		// component's summary once, into the largest of them (capUnion).
		var sets []*capSet
		succ := map[*wireComp]bool{}
		for _, v := range members {
			sets = append(sets, v.own)
			comp.unknown = comp.unknown || v.unknown
			for _, e := range nodes[v].embeds {
				if !m.step(1) {
					refuse(w)
					break
				}
				if d := c.wireComp[e]; d != comp && !succ[d] {
					succ[d] = true
					sets = append(sets, d.binds)
					comp.unknown = comp.unknown || d.unknown
				}
			}
		}
		if !stopped {
			var ok bool
			if comp.binds, ok = capUnion(m, c.types, sets); !ok {
				refuse(w)
			}
		}
		if stopped {
			comp.binds, comp.unknown = nil, true
		}
		for _, v := range members {
			v.binds, v.unknown = comp.binds, comp.unknown
		}
	}
	if !stopped {
		for _, w := range wires {
			if nodes[w].index < 0 {
				connect(w)
				if stopped {
					break
				}
			}
		}
	}
	// Past the budget, every wire without a summary binds an unknown set.
	refused := &wireComp{unknown: true}
	for _, w := range wires {
		if c.wireComp[w] == nil {
			c.wireComp[w] = refused
			w.binds, w.unknown = nil, true
		}
	}
}

// embeddedBinds returns what the embedding of inner contributes to the
// body of the named wire self (nil for an inline wire), whether every wire
// it reaches exists and was summarised, and whether inner is on self's own
// embedding cycle. Such an embedding, on a reported cycle (S108, Wire 5),
// contributes the component's summary, and bodySummary counts none of it
// as a second entry, against the wire's own entries or its other
// embeddings: what it brings depends on how the cycle is fixed (D3; round
// 16 m3, round 18). A replace may still name it.
func (c *checker) embeddedBinds(inner, self *Wire) (binds *capSet, known, onCycle bool) {
	c.wireSummaries()
	comp := c.wireComp[inner]
	return comp.binds, !comp.unknown, self != nil && comp == c.wireComp[self]
}

// localCaps returns s keyed by this check's type table: a wire summary of
// another package is converted once, metered. ok is false when the budget
// ran out.
func (c *checker) localCaps(s *capSet) (*capSet, bool) {
	if s == nil || s.size == 0 || s.tab == c.types {
		return s, true
	}
	if out, ok := c.foreignCaps[s]; ok {
		return out, true
	}
	m := c.meter()
	out := &capSet{}
	if !s.each(m, func(_ int32, t Type) bool {
		var ok bool
		out, ok = out.with(m, c.types, t)
		return ok
	}) {
		return nil, false
	}
	if c.foreignCaps == nil {
		c.foreignCaps = map[*capSet]*capSet{}
	}
	c.foreignCaps[s] = out
	return out, true
}

// A bodySum is a wire body's summary, decided once: what it binds, whether
// that is known, and its S111 and replace findings, which wireBody reports
// on every pass.
type bodySum struct {
	binds    *capSet
	known    bool
	findings []wireFinding
}

type wireFinding struct {
	at        syntax.Node
	code, msg string
}

// bodySummary returns the summary of a named (self) or inline wire body;
// at is the wire's name or literal, where a budget refusal is reported.
// After a refusal a body binds an unknown set and has no finding (D3).
func (c *checker) bodySummary(body *syntax.WireBody, self *Wire, at syntax.Node) *bodySum {
	if s, ok := c.wireBodies[body]; ok {
		return s
	}
	c.wireSummaries()
	if c.wireBodies == nil {
		c.wireBodies = map[*syntax.WireBody]*bodySum{}
	}
	s := &bodySum{}
	c.wireBodies[body] = s
	m := c.meter()
	if m.out {
		return s
	}
	if !c.summarizeBody(s, body, self, m) {
		*s = bodySum{findings: []wireFinding{{at, CodeDuplicateCap, wireBudgetMessage}}}
	}
	return s
}

// summarizeBody decides a body's summary on the meter, and reports false
// when the budget ran out. Two embedded wires that bind one capability
// collide (S111) unless a replace entry of this body overrides it; an
// entry for a capability already bound, other than a replace, is S111; a
// replace of a capability no embedding binds is S108 (rows.md, Wire 1 and
// 7). The embeddings' summaries are joined into the largest, which is
// shared, not copied, and a collision is found against it or among the
// others, at the later embedding.
func (c *checker) summarizeBody(s *bodySum, body *syntax.WireBody, self *Wire, m *wireMeter) bool {
	s.known = true
	type collision struct {
		cap Type
		at  syntax.Expr
	}
	var reaches []*capSet
	var keys []syntax.Expr
	var cycle *capSet
	for _, e := range body.Entries {
		if !m.step(1) {
			return false
		}
		if e.Kind != syntax.WireEmbed {
			continue
		}
		inner := c.embeddedWire(e)
		if inner == nil {
			s.known = false
			continue
		}
		reach, known, onCycle := c.embeddedBinds(inner, self)
		if !known {
			s.known = false
		}
		if reach == nil {
			reach = &capSet{}
		}
		if onCycle {
			cycle = reach // the same summary for every such embedding
			continue
		}
		reaches = append(reaches, reach)
		keys = append(keys, e.Key)
	}
	largest := -1
	for i, r := range reaches {
		if !m.step(1) {
			return false
		}
		if largest < 0 || r.size > reaches[largest].size {
			largest = i
		}
	}
	embedded := &capSet{}
	if largest >= 0 {
		embedded = reaches[largest]
	}
	var collisions []collision
	seen := map[int32]bool{} // ids of the other embeddings so far
	var early []Type         // their capabilities before the largest
	for j, r := range reaches {
		if j == largest {
			var hits []Type
			for _, t := range early {
				found, ok := r.lookup(m, c.types.id(t))
				if !ok {
					return false
				}
				if found {
					hits = append(hits, t)
				}
			}
			if !m.step(len(hits)*bits.Len(uint(len(hits))) + 1) {
				return false
			}
			sort.Slice(hits, func(a, b int) bool { return c.types.id(hits[a]) < c.types.id(hits[b]) })
			for _, t := range hits {
				collisions = append(collisions, collision{t, keys[j]})
			}
			continue
		}
		if !r.each(m, func(id int32, t Type) bool {
			if !m.step(1) {
				return false
			}
			collide := seen[id]
			if !collide && largest >= 0 && j > largest {
				found, ok := reaches[largest].lookup(m, id)
				if !ok {
					return false
				}
				collide = found
			}
			if collide {
				collisions = append(collisions, collision{t, keys[j]})
			}
			if !seen[id] {
				seen[id] = true
				if j < largest {
					early = append(early, t)
				}
			}
			var ok bool
			embedded, ok = embedded.with(m, c.types, t)
			return ok
		}) {
			return false
		}
	}
	binds := embedded
	replaced := map[int32]bool{}
	c.silent++
	defer func() { c.silent-- }()
	for _, e := range body.Entries {
		if e.Kind == syntax.WireEmbed {
			continue
		}
		constructor := c.isConstructor(e.Value)
		for _, k := range append([]syntax.Expr{e.Key}, e.Keys...) {
			if !m.step(1) {
				return false
			}
			t := c.wireKey(k)
			if t == nil {
				continue
			}
			id := c.types.id(t)
			switch {
			case e.Kind == syntax.WireReplace:
				replaced[id] = true
				if constructor {
					break
				}
				inEmbedded, ok := embedded.lookup(m, id)
				if !ok {
					return false
				}
				// A replace may name what an embedding on the wire's own
				// cycle brings.
				inCycle, ok := cycle.lookup(m, id)
				if !ok {
					return false
				}
				if !inEmbedded && !inCycle {
					s.findings = append(s.findings, wireFinding{k, CodeType, fmt.Sprintf("replace %s names no entry of an embedded wire (rows.md, Wire 7)", t)})
				}
			case !constructor:
				found, ok := binds.lookup(m, id)
				if !ok {
					return false
				}
				if found {
					s.findings = append(s.findings, wireFinding{k, CodeDuplicateCap, fmt.Sprintf("%s has a second entry in this wire; only replace overrides an embedded entry (rows.md, Wire 1; README Gap 28)", t)})
				}
			}
			var ok bool
			if binds, ok = binds.with(m, c.types, t); !ok {
				return false
			}
		}
	}
	for _, k := range collisions {
		if !m.step(1) {
			return false
		}
		if !replaced[c.types.id(k.cap)] {
			s.findings = append(s.findings, wireFinding{k.at, CodeDuplicateCap, fmt.Sprintf("%s is bound by two embedded wires; replace one entry (rows.md, Wire 7)", k.cap)})
		}
	}
	out, ok := capUnion(m, c.types, []*capSet{binds, cycle})
	if !ok {
		return false
	}
	s.binds = out
	return true
}
