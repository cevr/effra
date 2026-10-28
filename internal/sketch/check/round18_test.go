package check

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// opsOf checks src as one package and returns its diagnostics and the
// operation counts of the reference walk's literals and the wire
// summaries.
func opsOf(t *testing.T, src string) (diags []Diagnostic, walk, wire int) {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker("main", Std, prelude().scope)
	c.run([]*syntax.File{file})
	return c.diags, c.walkOps, c.wireOps
}

// linear asserts that counts measured at sizes 1000, 4000 and 8000 grow
// linearly: each is at most k operations per unit, and the 8000 count is at
// most 8.5 times the 1000 count (a quadratic pass gives 64).
func linear(t *testing.T, what string, counts map[int]int, k int) {
	t.Helper()
	for n, ops := range counts {
		if ops > k*n {
			t.Errorf("%s: %d operations at n=%d, more than %d per unit", what, ops, n, k)
		}
	}
	if r := float64(counts[8000]) / float64(counts[1000]); r > 8.5 {
		t.Errorf("%s: 8000/1000 operation ratio %.1f, want linear growth (%v)", what, r, counts)
	}
	t.Logf("%s: %v", what, counts)
}

// TestWireChainLinear: an acyclic embedding chain of n empty wires costs
// linear work in the wire summaries (round 17 M1: every root and every
// embedding walked the whole chain).
func TestWireChainLinear(t *testing.T) {
	counts := map[int]int{}
	for _, n := range []int{1000, 4000, 8000} {
		var b strings.Builder
		b.WriteString("package p\nwire W0 {}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire W%d { W%d }\n", i, i-1)
		}
		fmt.Fprintf(&b, "func F() { with(W%d) {} }\n", n-1)
		diags, _, wire := opsOf(t, b.String())
		if len(diags) > 0 {
			t.Fatalf("n=%d: want acceptance, got %v", n, diags[0])
		}
		counts[n] = wire
	}
	linear(t, "wire chain", counts, 6)
}

// TestCapSetPersistent: every version of a capSet keeps its elements after
// later inserts, iterates in id order, and keeps the treap's order and heap
// invariants, so lookups stay logarithmic.
func TestCapSetPersistent(t *testing.T) {
	var versions []*capNode
	var sizes []int
	var root *capNode
	size := 0
	for i := range 2000 {
		id := int32((i * 7919) % 2003)
		var added bool
		root, added = capInsert(root, id, nil, capPriority(id))
		if added {
			size++
		}
		if i%97 == 0 {
			versions = append(versions, root)
			sizes = append(sizes, size)
		}
	}
	var check func(n *capNode, lo, hi int32) (count, depth int)
	check = func(n *capNode, lo, hi int32) (int, int) {
		if n == nil {
			return 0, 0
		}
		if n.id <= lo || n.id >= hi {
			t.Fatalf("id %d outside (%d, %d)", n.id, lo, hi)
		}
		for _, ch := range []*capNode{n.left, n.right} {
			if ch != nil && ch.pri > n.pri {
				t.Fatalf("heap order broken at %d", n.id)
			}
		}
		lc, ld := check(n.left, lo, n.id)
		rc, rd := check(n.right, n.id, hi)
		return lc + rc + 1, max(ld, rd) + 1
	}
	for i, v := range versions {
		count, depth := check(v, -1, 1<<30)
		if count != sizes[i] {
			t.Errorf("version %d: %d elements, want %d", i, count, sizes[i])
		}
		if depth > 60 {
			t.Errorf("version %d: depth %d for %d elements", i, depth, count)
		}
	}
	s := &capSet{root: root, size: size}
	prev := int32(-1)
	s.each(func(id int32, _ Type) {
		if id <= prev {
			t.Fatalf("each: %d after %d", id, prev)
		}
		prev = id
		if !s.hasID(id) {
			t.Fatalf("hasID(%d) false", id)
		}
	})
	if s.hasID(2003) {
		t.Error("hasID(2003) true")
	}
}

// TestWireEntriesLinear: wires whose summaries grow cost linear work: a
// chain whose every wire adds an entry (each wire binds all the entries
// before it), and one wire that embeds n wires of one entry each. The
// summaries share their embeddings' sets (capset.go), so each wire adds
// only its own entries; sorted rows copied per wire cost O(n²), and adding
// to them one capability at a time O(n³) (round 18).
func TestWireEntriesLinear(t *testing.T) {
	families := map[string]func(n int) string{
		"entry chain": func(n int) string {
			var b strings.Builder
			b.WriteString("package p\ntype P struct{}\nfunc (P) M() {}\n")
			for i := range n {
				fmt.Fprintf(&b, "type C%d interface{ M() }\n", i)
			}
			b.WriteString("wire W0 {\n\tC0 = P{}\n}\n")
			for i := 1; i < n; i++ {
				fmt.Fprintf(&b, "wire W%d {\n\tC%d = P{}\n\tW%d\n}\n", i, i, i-1)
			}
			fmt.Fprintf(&b, "func F() { with(W%d) { C0.M() } }\n", n-1)
			return b.String()
		},
		"wide embedding": func(n int) string {
			var b strings.Builder
			b.WriteString("package p\ntype P struct{}\nfunc (P) M() {}\n")
			for i := range n {
				fmt.Fprintf(&b, "type C%d interface{ M() }\nwire X%d {\n\tC%d = P{}\n}\n", i, i, i)
			}
			b.WriteString("wire W {\n")
			for i := range n {
				fmt.Fprintf(&b, "\tX%d\n", i)
			}
			fmt.Fprintf(&b, "}\nfunc F() { with(W) { C%d.M() } }\n", n-1)
			return b.String()
		},
	}
	for _, name := range []string{"entry chain", "wide embedding"} {
		counts := map[int]int{}
		for _, n := range []int{1000, 4000, 8000} {
			diags, _, wire := opsOf(t, families[name](n))
			if len(diags) > 0 {
				t.Fatalf("%s n=%d: want acceptance, got %v", name, n, diags[0])
			}
			counts[n] = wire
		}
		linear(t, name, counts, 10)
	}
}

// TestAnonymousStructLinear: keyed and positional anonymous struct literals
// of n fields, each a slice literal with elided children, cost linear work
// in the reference walk (round 17 M2: each element scanned the field list).
func TestAnonymousStructLinear(t *testing.T) {
	for _, keyed := range []bool{true, false} {
		counts := map[int]int{}
		for _, n := range []int{1000, 4000, 8000} {
			var b strings.Builder
			b.WriteString("package p\ntype S struct{ a int }\nfunc F() { _ = struct{\n")
			for i := range n {
				fmt.Fprintf(&b, " f%d []S\n", i)
			}
			b.WriteString("}{")
			for i := range n {
				if keyed {
					fmt.Fprintf(&b, "f%d: ", i)
				}
				b.WriteString("[]S{{a: 1}, {a: 2}}, ")
			}
			b.WriteString("} }\n")
			diags, walk, _ := opsOf(t, b.String())
			if len(diags) > 0 {
				t.Fatalf("keyed=%v n=%d: want acceptance, got %v", keyed, n, diags[0])
			}
			counts[n] = walk
		}
		linear(t, fmt.Sprintf("anonymous struct (keyed=%v)", keyed), counts, 12)
	}
}

// TestMissingLiteralType: a struct field's composite literal writes its
// type, keyed or positional, as in Go; the elements and keys of a slice,
// array or map literal may elide it (round 17 C1).
func TestMissingLiteralType(t *testing.T) {
	const decls = "package p\ntype S struct{ a int }\ntype Wrap[T any] struct{ value T }\n" +
		"type Long struct{ alpha int; beta int; gamma int; delta int }\n"
	for _, tc := range []struct{ body, want string }{
		{"_ = Wrap[S]{value: {a: 1}}", "missing type in composite literal; write S{a: 1}"},
		{"_ = Wrap[S]{{a: 1}}", "missing type in composite literal; write S{a: 1}"},
		{"_ = Wrap[[]S]{value: {{a: 1}}}", "missing type in composite literal; write []S{{a: 1}}"},
		{"_ = Wrap[Long]{value: {alpha: 10, beta: 20, gamma: 30, delta: 40}}", "missing type in composite literal; write Long{…}"},
		{"_ = Wrap[S]{value: {\na: 1}}", "missing type in composite literal; write S{…}"},
	} {
		src := decls + "func F() {\n" + tc.body + "\n}\n"
		_, diags := CheckSource(src)
		if len(diags) != 1 || diags[0].Code != CodeType || diags[0].Message != tc.want {
			t.Errorf("%s: want one S108 %q, got %v", tc.body, tc.want, diags)
		}
	}
	for _, ok := range []string{
		"_ = []S{{a: 1}}", "_ = [1]S{{a: 1}}", "_ = map[S]S{{a: 1}: {a: 2}}",
		"_ = Wrap[S]{value: S{a: 1}}", "_ = Wrap[[]S]{value: []S{{a: 1}}}",
	} {
		if _, diags := CheckSource(decls + "func F() {\n" + ok + "\n}\n"); len(diags) > 0 {
			t.Errorf("%s: want acceptance, got %v", ok, diags)
		}
	}
}
