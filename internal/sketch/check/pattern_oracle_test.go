package check_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestPatternOracle (spec D-B4): the checker's totality and reachability
// verdicts on generated matches agree with brute-force enumeration of the
// subjects' values. Each trial declares a few unions whose variants are
// records of bool, int and later unions (so patterns nest), and matches one
// or two subjects with random cases: wildcards, bindings, bool and integer
// constants, bare type tests and keyed record patterns, tuples for two
// subjects, cases of several patterns, and sometimes a final `case _`. An
// int ranges over 0, 1, 2 and "any other value", so constants never cover
// it. The oracle expects S108 at exactly the patterns the patterns before
// them cover (a `case _` is not itself judged, rows.md Totality 5), S103 at
// the match exactly when some value matches no pattern, and nothing else.

type oKind int

const (
	oBool oKind = iota
	oInt
	oSum
)

type oType struct {
	kind oKind
	u    *oUnion
}

type oUnion struct {
	name     string
	variants []oVariant
}

type oVariant struct {
	name   string
	fields []oField
}

type oField struct {
	name string
	t    oType
}

// oVal is an abstract value: a bool's tag is 0 (false) or 1 (true); an
// int's 0, 1, 2 or 3 (any other value); a union's the variant index, with
// the variant's field values.
type oVal struct {
	tag  int
	kids []oVal
}

type oPatKind int

const (
	pWild oPatKind = iota
	pBind
	pConst
	pType
	pRecord
)

type oPat struct {
	kind   oPatKind
	tag    int
	name   string
	fields map[int]oPat // a record pattern's keyed fields
}

func (t oType) String() string {
	switch t.kind {
	case oBool:
		return "bool"
	case oInt:
		return "int"
	}
	return t.u.name
}

func oValues(t oType) []oVal {
	switch t.kind {
	case oBool:
		return []oVal{{tag: 0}, {tag: 1}}
	case oInt:
		return []oVal{{tag: 0}, {tag: 1}, {tag: 2}, {tag: 3}}
	}
	var out []oVal
	for vi, v := range t.u.variants {
		prods := [][]oVal{nil}
		for _, f := range v.fields {
			var next [][]oVal
			for _, p := range prods {
				for _, fv := range oValues(f.t) {
					next = append(next, append(append([]oVal(nil), p...), fv))
				}
			}
			prods = next
		}
		for _, p := range prods {
			out = append(out, oVal{tag: vi, kids: p})
		}
	}
	return out
}

func oMatches(p oPat, v oVal) bool {
	switch p.kind {
	case pWild, pBind:
		return true
	case pConst, pType:
		return v.tag == p.tag
	}
	if v.tag != p.tag {
		return false
	}
	for f, fp := range p.fields {
		if !oMatches(fp, v.kids[f]) {
			return false
		}
	}
	return true
}

type oGen struct {
	rng   *rand.Rand
	names int
	binds bool // bindings allowed (cases of one pattern)
}

func (g *oGen) pattern(t oType, depth int) oPat {
	k := g.rng.Intn(10)
	switch {
	case k < 2:
		return oPat{kind: pWild}
	case k < 3 && g.binds:
		g.names++
		return oPat{kind: pBind, name: fmt.Sprintf("x%d", g.names)}
	}
	switch t.kind {
	case oBool:
		return oPat{kind: pConst, tag: g.rng.Intn(2)}
	case oInt:
		return oPat{kind: pConst, tag: g.rng.Intn(3)}
	}
	vi := g.rng.Intn(len(t.u.variants))
	v := t.u.variants[vi]
	if len(v.fields) == 0 || depth > 2 || g.rng.Intn(3) == 0 {
		return oPat{kind: pType, tag: vi}
	}
	p := oPat{kind: pRecord, tag: vi, fields: map[int]oPat{}}
	for f := range v.fields {
		if len(p.fields) == 0 && f == len(v.fields)-1 || g.rng.Intn(2) == 0 {
			p.fields[f] = g.pattern(v.fields[f].t, depth+1)
		}
	}
	return p
}

func (p oPat) text(t oType) string {
	switch p.kind {
	case pWild:
		return "_"
	case pBind:
		return p.name
	case pConst:
		if t.kind == oBool {
			return []string{"false", "true"}[p.tag]
		}
		return fmt.Sprint(p.tag)
	case pType:
		return t.u.variants[p.tag].name
	}
	v := t.u.variants[p.tag]
	var keys []int
	for f := range p.fields {
		keys = append(keys, f)
	}
	sort.Ints(keys)
	var parts []string
	for _, f := range keys {
		parts = append(parts, v.fields[f].name+": "+p.fields[f].text(v.fields[f].t))
	}
	return v.name + "{" + strings.Join(parts, ", ") + "}"
}

func (p oPat) bindings(out *[]string) {
	if p.kind == pBind {
		*out = append(*out, p.name)
	}
	for _, f := range p.fields {
		f.bindings(out)
	}
}

// oUnions declares 1 to 3 unions; a variant's fields name bool, int or a
// later union, so the types nest without recursion.
func oUnions(rng *rand.Rand) []*oUnion {
	n := 1 + rng.Intn(3)
	us := make([]*oUnion, n)
	for i := n - 1; i >= 0; i-- {
		u := &oUnion{name: fmt.Sprintf("U%d", i)}
		for v := 0; v < 2+rng.Intn(2); v++ {
			vr := oVariant{name: fmt.Sprintf("V%d%c", i, 'a'+v)}
			for f := 0; f < rng.Intn(3); f++ {
				ft := oType{kind: oKind(rng.Intn(2))}
				if i+1 < n && rng.Intn(2) == 0 {
					ft = oType{kind: oSum, u: us[i+1+rng.Intn(n-i-1)]}
				}
				vr.fields = append(vr.fields, oField{name: fmt.Sprintf("F%d", f), t: ft})
			}
			u.variants = append(u.variants, vr)
		}
		us[i] = u
	}
	return us
}

func oDecls(us []*oUnion) string {
	var b strings.Builder
	for _, u := range us {
		var vs []string
		for _, v := range u.variants {
			var fs []string
			for _, f := range v.fields {
				fs = append(fs, f.name+" "+f.t.String())
			}
			vs = append(vs, v.name+"{"+strings.Join(fs, ", ")+"}")
		}
		fmt.Fprintf(&b, "type %s = %s\n\n", u.name, strings.Join(vs, " | "))
	}
	return b.String()
}

func TestPatternOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(1964))
	verdicts := map[string]int{}
	for trial := 0; trial < 4000; trial++ {
		us := oUnions(rng)
		pool := []oType{{kind: oBool}, {kind: oInt}}
		for _, u := range us {
			pool = append(pool, oType{kind: oSum, u: u}, oType{kind: oSum, u: u})
		}
		subj := []oType{pool[rng.Intn(len(pool))]}
		if rng.Intn(2) == 0 {
			subj = append(subj, pool[rng.Intn(len(pool))])
		}
		// The value vectors of the subjects.
		vecs := [][]oVal{nil}
		for _, s := range subj {
			var next [][]oVal
			for _, v := range vecs {
				for _, sv := range oValues(s) {
					next = append(next, append(append([]oVal(nil), v...), sv))
				}
			}
			vecs = next
		}
		if len(vecs) > 4000 {
			continue
		}
		g := &oGen{rng: rng}
		type item struct {
			pats []oPat
			line int
		}
		var src strings.Builder
		src.WriteString(oDecls(us))
		var params, names []string
		for i, s := range subj {
			params = append(params, fmt.Sprintf("s%d %s", i, s))
			names = append(names, fmt.Sprintf("s%d", i))
		}
		head := names[0]
		if len(names) > 1 {
			head = "(" + strings.Join(names, ", ") + ")"
		}
		fmt.Fprintf(&src, "func f(%s) {\n", strings.Join(params, ", "))
		line := strings.Count(src.String(), "\n") + 1
		matchLine := line
		fmt.Fprintf(&src, "\tmatch %s {\n", head)
		line++
		var items []item
		for range 1 + rng.Intn(6) {
			several := rng.Intn(5) == 0
			g.binds = !several
			count := 1
			if several {
				count = 2
			}
			var texts []string
			var binds []string
			for range count {
				it := item{line: line}
				var parts []string
				for _, s := range subj {
					p := g.pattern(s, 0)
					for len(subj) == 1 && p.kind == pWild {
						p = g.pattern(s, 0) // `case _` alone is the default case
					}
					it.pats = append(it.pats, p)
					parts = append(parts, p.text(s))
					p.bindings(&binds)
				}
				text := parts[0]
				if len(parts) > 1 {
					text = "(" + strings.Join(parts, ", ") + ")"
				}
				texts = append(texts, text)
				items = append(items, it)
			}
			fmt.Fprintf(&src, "\tcase %s:\n", strings.Join(texts, ", "))
			line++
			for _, b := range binds {
				fmt.Fprintf(&src, "\t\t_ = %s\n", b)
				line++
			}
		}
		withDefault := rng.Intn(5) == 0
		if withDefault {
			src.WriteString("\tcase _:\n")
		}
		src.WriteString("\t}\n}\n")
		// The oracle.
		matched := make([]bool, len(vecs))
		want := map[string]bool{}
		for _, it := range items {
			useful := false
			for vi, v := range vecs {
				all := true
				for j := range subj {
					if !oMatches(it.pats[j], v[j]) {
						all = false
						break
					}
				}
				if all && !matched[vi] {
					useful = true
					matched[vi] = true
				}
			}
			if !useful {
				want[fmt.Sprintf("S108:%d", it.line)] = true
			}
		}
		total := withDefault
		if !total {
			total = true
			for _, m := range matched {
				total = total && m
			}
		}
		if !total {
			want[fmt.Sprintf("S103:%d", matchLine)] = true
		}
		_, diags := check.CheckSource(src.String())
		got := map[string]bool{}
		for _, d := range diags {
			got[fmt.Sprintf("%s:%d", d.Code, d.Line)] = true
			if d.Code == "S103" && strings.Contains(d.Message, "too many cases") {
				t.Fatalf("trial %d: a small match reached the budget:\n%s", trial, src.String())
			}
		}
		if fmt.Sprint(sortedKeys(got)) != fmt.Sprint(sortedKeys(want)) {
			t.Fatalf("trial %d: got %v, the oracle says %v\n%s\n%s", trial, sortedKeys(got), sortedKeys(want), describe(diags), src.String())
		}
		switch {
		case !total:
			verdicts["not total"]++
		case len(want) > 0:
			verdicts["total with unreachable"]++
		default:
			verdicts["clean"]++
		}
	}
	t.Logf("verdicts: %v", verdicts)
	for _, v := range []string{"not total", "total with unreachable", "clean"} {
		if verdicts[v] < 50 {
			t.Errorf("only %d trials gave %q; the generator lost its spread", verdicts[v], v)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestMatchArmsLinear (D16): a match over a union of n variants, one case
// per variant, nested one level through a bool field, checks in counted
// work linear in n: the solvers' steps and the bytes the check allocates
// grow about 4x when n does (stage E's scale shape "match").
func TestMatchArmsLinear(t *testing.T) {
	src := func(n int) string {
		var b strings.Builder
		var vs []string
		for i := range n {
			vs = append(vs, fmt.Sprintf("V%d{On bool}", i))
		}
		fmt.Fprintf(&b, "type U = %s\n\nfunc f(u U) int {\n\tmatch u {\n", strings.Join(vs, " | "))
		for i := range n {
			fmt.Fprintf(&b, "\tcase V%d{On: true}:\n\t\treturn %d\n\tcase V%d{On: false}:\n\t\treturn 0\n", i, i, i)
		}
		b.WriteString("\t}\n}\n")
		return b.String()
	}
	steps := map[int]int{}
	for _, n := range []int{1000, 4000} {
		ts, us, d := check.SolverSteps(src(n))
		if len(d) > 0 || len(ts) != 1 {
			t.Fatalf("n=%d: diagnostics %s", n, describe(d))
		}
		steps[n] = ts[0] + us[0]
	}
	if r := float64(steps[4000]) / float64(steps[1000]); r > 4.5 {
		t.Errorf("4x the cases take %.2fx the solver steps (%d vs %d)", r, steps[4000], steps[1000])
	}
	bytes := func(s string) uint64 {
		best := uint64(0)
		for range 3 {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			check.CheckSource(s)
			runtime.ReadMemStats(&after)
			if b := after.TotalAlloc - before.TotalAlloc; best == 0 || b < best {
				best = b
			}
		}
		return best
	}
	small, large := bytes(src(1000)), bytes(src(4000))
	ratio := float64(large) / float64(small)
	t.Logf("steps %v; allocated bytes: n=1000 %d, n=4000 %d, ratio %.2f", steps, small, large, ratio)
	if ratio > 6 {
		t.Errorf("4x the cases allocate %.2fx the bytes (%d vs %d); the check is superlinear", ratio, large, small)
	}
}
