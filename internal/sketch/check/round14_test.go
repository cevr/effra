package check

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

// admissionGroup is one generated recursive admission graph (round 14,
// class G) with the verdict its source decides.
type admissionGroup struct {
	src  string
	want []string // CODE@LINE, sorted
	// pinned and written count the value-used members without and with
	// written rows; grew counts the pinned members whose body needs a row.
	pinned, written, grew int
}

// recursiveAdmission writes one recursive group f0…f(n-1): a ring of
// references makes it one strongly connected component, and extra edges
// are calls, value uses (_ = fj) and admission calls whose type argument
// is a member's function type (Choose(BoxOf(fx), BoxOf(fy))), the round-13
// witnesses' shape. Every value-used member either writes its row or not;
// the oracle is the source rule alone (rows.md, Capability rows 6):
//   - a member used as a value inside the group has the rows it writes,
//     and each row it does not write is empty;
//   - every other member's row is the least fixpoint over its calls;
//   - a value-used member with an unwritten row whose body needs more than
//     the empty row gets one S108 at its first value use; nothing else is
//     reported.
//
// Every body may also hold a nested literal whose parameter or local is
// spelled as a member (lexical shadowing, round 14 M1): no reference.
//
// Admission calls pair members of the same kind (both written or both
// unwritten), so every one is admitted and no type mismatch is expected.
// In the error family every member fails (try mark()), and a call
// statement (try fj()) only reaches a member whose row is certain to be
// non-empty; in the capability family a member charges Cap through Cap.M()
// or Choose, or through a call, so a pinned member may need nothing and is
// accepted unwritten.
func recursiveAdmission(seed uint64, errors bool) admissionGroup {
	rng := rand.New(rand.NewPCG(seed, seed^0x51ab))
	shadows := rand.New(rand.NewPCG(seed, seed^0x5ad0))
	n := 2 + rng.IntN(5)
	var g admissionGroup
	var b strings.Builder
	b.WriteString("package main\n")
	line := 1
	emit := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
		line++
	}
	if errors {
		emit("type Bang struct{}")
		emit("func mark() error[Bang]")
		emit("type Box[T any] interface{ M(T) }")
		emit("func BoxOf[T any](x T) Box[T]")
		emit("func Choose[T any](x T, y T) error[Bang]")
	} else {
		emit("type Cap interface{ M() }")
		emit("type Box[T any] interface{ M(T) }")
		emit("func BoxOf[T any](x T) Box[T]")
		emit("func Choose[T any](x T, y T) needs Cap")
		emit("func Same[T any](x T, y T) {}")
	}
	// Which members are used as values, and which of those write rows.
	value := make([]bool, n)
	written := make([]bool, n)
	for i := range n {
		value[i] = rng.IntN(3) != 0
	}
	allWritten := rng.IntN(3) == 0
	for i := range n {
		if value[i] {
			written[i] = allWritten || rng.IntN(2) == 0
		}
	}
	type stmt struct {
		call, use []int // members called, members used as values
		seed      bool  // the body fails or charges by itself
		choose    bool  // an admission call through Choose (fails or charges)
	}
	bodies := make([][]stmt, n)
	// partner picks a value-used member of the same kind as x.
	partner := func(x int) int {
		var same []int
		for j := range n {
			if value[j] && written[j] == written[x] {
				same = append(same, j)
			}
		}
		return same[rng.IntN(len(same))]
	}
	callable := func(j int) bool {
		// A try reaches only a member whose row is not pinned empty.
		return !errors || !value[j] || written[j]
	}
	for i := range n {
		next := (i + 1) % n
		var ring stmt
		switch {
		case value[next] && (rng.IntN(2) == 0 || !callable(next)):
			if rng.IntN(2) == 0 {
				ring = stmt{use: []int{next}}
			} else {
				ring = stmt{use: []int{next, partner(next)}, choose: errors || rng.IntN(2) == 0}
			}
		case callable(next):
			ring = stmt{call: []int{next}}
		default:
			ring = stmt{use: []int{next}}
		}
		bodies[i] = append(bodies[i], ring)
		for range rng.IntN(3) {
			j := rng.IntN(n)
			switch {
			case value[j] && rng.IntN(2) == 0:
				bodies[i] = append(bodies[i], stmt{use: []int{j, partner(j)}, choose: errors || rng.IntN(2) == 0})
			case value[j]:
				bodies[i] = append(bodies[i], stmt{use: []int{j}})
			case callable(j):
				bodies[i] = append(bodies[i], stmt{call: []int{j}})
			}
		}
		if errors || rng.IntN(3) == 0 {
			bodies[i] = append(bodies[i], stmt{seed: true})
		}
	}
	// A member that is called but never used as a value stays inferred; in
	// the error family it is fallible, since every body fails.
	firstUse := make([]int, n)
	for i := range n {
		row := ""
		if written[i] {
			if errors {
				row = " error[Bang]"
			} else {
				row = " needs Cap"
			}
		}
		emit("func f%d()%s {", i, row)
		for _, s := range bodies[i] {
			switch {
			case s.seed && errors:
				emit("\ttry mark()")
			case s.seed:
				emit("\tCap.M()")
			case len(s.call) > 0 && errors:
				emit("\ttry f%d()", s.call[0])
			case len(s.call) > 0:
				emit("\tf%d()", s.call[0])
			case len(s.use) == 1:
				emit("\t_ = f%d", s.use[0])
			default:
				fn := "Same"
				if s.choose {
					fn = "Choose"
				}
				prefix := ""
				if errors {
					prefix = "try "
				}
				emit("\t%s%s(BoxOf(f%d), BoxOf(f%d))", prefix, fn, s.use[0], s.use[1])
			}
			for _, j := range s.use {
				if firstUse[j] == 0 {
					firstUse[j] = line
				}
			}
		}
		// Lexical shadowing (round 14 M1): a parameter or a local of a
		// nested literal spelled as a member is no reference to it.
		if r := shadows.IntN(3); r < 2 {
			id := fmt.Sprintf("f%d", shadows.IntN(n))
			if shadows.IntN(4) == 0 {
				id = "local"
			}
			if r == 0 {
				emit("\tfunc(%s func()) { %s() }(func() {})", id, id)
			} else {
				emit("\tfunc() { %s := func() {}; %s() }()", id, id)
			}
		}
		emit("}")
	}
	g.src = b.String()
	// A member marked as a value may get no value use: it is then an
	// ordinary member, inferred or written.
	pinned := make([]bool, n)
	for i := range n {
		pinned[i] = firstUse[i] != 0 && !written[i]
		switch {
		case pinned[i]:
			g.pinned++
		case firstUse[i] != 0:
			g.written++
		}
	}

	// The oracle: a written row is the written one, a pinned row is empty,
	// and every other row is the least fixpoint over the calls.
	needs := make([]bool, n) // the body needs a non-empty row
	for changed := true; changed; {
		changed = false
		for i := range n {
			if needs[i] {
				continue
			}
			for _, s := range bodies[i] {
				hit := s.seed || s.choose
				for _, j := range s.call {
					if written[j] || needs[j] && !pinned[j] {
						hit = true
					}
				}
				if hit {
					needs[i], changed = true, true
					break
				}
			}
		}
	}
	for i := range n {
		if pinned[i] && needs[i] {
			g.grew++
			g.want = append(g.want, fmt.Sprintf("S108@%d", firstUse[i]))
		}
	}
	sort.Strings(g.want)
	return g
}

// TestRecursiveAdmissionGroups: generated recursive admission graphs get
// the verdict of their source: a group whose value-used members write
// their rows is accepted, and each value-used member with an unwritten row
// that its body needs gets the class G S108 at its first value use, never a
// chosen fixpoint (round 13 M1, O1).
func TestRecursiveAdmissionGroups(t *testing.T) {
	var accepted, refused, pinnedAccepted int
	for _, errors := range []bool{true, false} {
		for seed := range uint64(300) {
			g := recursiveAdmission(seed, errors)
			c, diags := checkFor(g.src)
			if c == nil {
				t.Fatalf("seed %d: %v\n%s", seed, diags, g.src)
			}
			var have []string
			for _, d := range diags {
				if d.Code == CodeType && !strings.Contains(d.Message, "used as a value inside its own recursive group") {
					t.Errorf("seed %d errors=%v: unexpected %s\n%s", seed, errors, d, g.src)
				}
				have = append(have, fmt.Sprintf("%s@%d", d.Code, d.Line))
			}
			sort.Strings(have)
			if strings.Join(have, " ") != strings.Join(g.want, " ") {
				t.Errorf("seed %d errors=%v: want %v, got %v\n%s", seed, errors, g.want, diags, g.src)
				continue
			}
			if len(c.widened) > 0 {
				t.Errorf("seed %d errors=%v: wider reporting rows for %v", seed, errors, c.widened)
			}
			switch {
			case len(g.want) > 0:
				refused++
			case g.pinned > 0:
				pinnedAccepted++
			default:
				accepted++
			}
		}
	}
	t.Logf("600 groups: %d accepted with written rows, %d accepted with pinned empty rows, %d refused", accepted, pinnedAccepted, refused)
	if accepted < 50 || refused < 50 || pinnedAccepted < 10 {
		t.Fatalf("the generator misses a verdict: %d accepted, %d pinned accepted, %d refused", accepted, pinnedAccepted, refused)
	}
}
