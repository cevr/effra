package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// meterOf checks src as one package and returns its diagnostics and its
// wire meter (nil when nothing was summarised).
func meterOf(t *testing.T, src string) ([]Diagnostic, *wireMeter) {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker("main", Std, prelude().scope)
	c.run([]*syntax.File{file})
	return c.sorted(), c.wires
}

// wireFamily writes one generated wire program of size n, in the decided
// syntax (fromOld).
func wireFamily(shape string, n int) string {
	var b strings.Builder
	b.WriteString("package p\ntype P struct{}\nfunc (P) M() {}\n")
	caps := n
	if shape == "disjoint" || shape == "large-base chain" {
		caps = 2 * n
	}
	if shape == "empty independent" || shape == "inline embeddings" {
		caps = 1
	}
	for i := range caps {
		fmt.Fprintf(&b, "type C%d interface{ M() }\n", i)
	}
	last := fmt.Sprintf("W%d", n-1)
	switch shape {
	case "entry chain": // every wire adds an entry
		b.WriteString("wire W0 {\n\tC0 = P{}\n}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire W%d {\n\tC%d = P{}\n\tW%d\n}\n", i, i, i-1)
		}
	case "wide fan-in": // one wire embeds n one-entry wires
		for i := range n {
			fmt.Fprintf(&b, "wire X%d {\n\tC%d = P{}\n}\n", i, i)
		}
		b.WriteString("wire W {\n")
		for i := range n {
			fmt.Fprintf(&b, "\tX%d\n", i)
		}
		b.WriteString("}\n")
		last = "W"
	case "disjoint": // the reviewer's ladder: W_i embeds two disjoint chains
		b.WriteString("wire A0 {\n\tC0 = P{}\n}\n")
		fmt.Fprintf(&b, "wire B0 {\n\tC%d = P{}\n}\n", n)
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire A%d {\n\tC%d = P{}\n\tA%d\n}\nwire B%d {\n\tC%d = P{}\n\tB%d\n}\n", i, i, i-1, i, n+i, i-1)
		}
		for i := range n {
			fmt.Fprintf(&b, "wire W%d {\n\tA%d\n\tB%d\n}\n", i, i, i)
		}
	case "large-base chain": // one large wire, then a chain adding one entry each
		b.WriteString("wire W0 {\n")
		for i := range n {
			fmt.Fprintf(&b, "\tC%d = P{}\n", i)
		}
		b.WriteString("}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire W%d {\n\tW%d\n\tC%d = P{}\n}\n", i, i-1, n+i-1)
		}
	case "replace diamond": // each layer joins two branches and replaces the repeated entry
		b.WriteString("wire W0 {\n\tC0 = P{}\n}\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "wire L%d { W%d }\nwire R%d { W%d }\nwire W%d {\n\tL%d\n\tR%d\n\treplace C0 = P{}\n}\n", i, i-1, i, i-1, i, i, i)
		}
	case "empty independent": // n wires with nothing in them (round 19 M1)
		for i := range n {
			fmt.Fprintf(&b, "wire W%d {}\n", i)
		}
		b.WriteString("func F() { with(wire{ C0 = P{} }) { C0.M() } }\n")
		return fromOld(b.String())
	case "inline fan-in": // an inline wire embeds n one-entry wires and replaces one
		for i := range n {
			fmt.Fprintf(&b, "wire X%d {\n\tC%d = P{}\n}\n", i, i)
		}
		b.WriteString("func F() {\n\twith(wire{\n")
		for i := range n {
			fmt.Fprintf(&b, "\t\tX%d\n", i)
		}
		b.WriteString("\t\treplace C0 = P{}\n\t}) { C0.M() }\n}\n")
		return fromOld(b.String())
	case "multi-key": // one entry binds n capabilities to one provider (round 20)
		b.WriteString("wire W {\n\t")
		for i := range n {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "C%d", i)
		}
		b.WriteString(" = P{}\n}\n")
		last = "W"
	case "inline embeddings": // an inline wire of n empty embeddings and no entry (round 20)
		b.WriteString("wire Empty {}\nfunc F() {\n\twith(wire{\n")
		for range n {
			b.WriteString("\t\tEmpty\n")
		}
		b.WriteString("\t}) {}\n}\n")
		return fromOld(b.String())
	default:
		panic(shape)
	}
	fmt.Fprintf(&b, "func F() { with(%s) { C0.M() } }\n", last)
	return fromOld(b.String())
}

// TestWireBudget: on every embedding shape, the wire pass's repeatable
// work stays within the package's budget and was charged before it ran,
// its single-pass work within wireSinglePerUnit per wire node (bounded),
// and the verdict is the exact one (acceptance) or the one budget S111
// (D16 as amended in rounds 18 and 20). The disjoint ladder's answer is
// quadratic, so it is refused at every size here; the other shapes are
// accepted.
func TestWireBudget(t *testing.T) {
	oc := countOps(t)
	for _, family := range []struct {
		shape   string
		sizes   []int
		refused bool
	}{
		{"entry chain", []int{1000, 4000, 8000}, false},
		{"wide fan-in", []int{1000, 4000, 8000}, false},
		{"large-base chain", []int{1000, 4000, 8000}, false},
		{"replace diamond", []int{1000, 4000, 8000}, false},
		{"empty independent", []int{1000, 4000, 8000}, false},
		{"inline fan-in", []int{1000, 4000, 8000}, false},
		{"multi-key", []int{1000, 8000}, false},
		{"inline embeddings", []int{1, 1000, 8000}, false},
		{"disjoint", []int{128, 256, 512, 2048}, true},
	} {
		for _, n := range family.sizes {
			src := wireFamily(family.shape, n)
			diags, c := checkLimited(t, src, nil)
			m := c.wires
			if m == nil {
				t.Fatalf("%s n=%d: no wire meter", family.shape, n)
			}
			oc.bounded(t, fmt.Sprintf("%s n=%d", family.shape, n), src, c)
			switch {
			case len(diags) == 0 && !family.refused:
			case len(diags) == 1 && diags[0].Code == CodeDuplicateCap && diags[0].Message == wireBudgetMessage && family.refused:
			default:
				t.Errorf("%s n=%d: want %s, got %v", family.shape, n, map[bool]string{false: "acceptance", true: "the budget S111"}[family.refused], diags)
			}
			t.Logf("%s n=%d: %d of %d steps charged (%.1f%%), %d repeatable and %d single-pass operations, %d diagnostics", family.shape, n, m.used, m.limit, 100*float64(m.used)/float64(m.limit), oc.repeat[m], oc.single[m], len(diags))
		}
	}
}

// TestWireBudgetRefusal: past the budget, the wire being summarised gets
// the one S111 and nothing else follows from it: a with of a refused wire
// discharges everything, and a wire body summarised later reports no
// duplicate (D3). A wire whose own body passes the budget gets the S111 at
// its name.
func TestWireBudgetRefusal(t *testing.T) {
	src := wireFamily("disjoint", 512) + "let Dup = wire {\n\tA3,\n\tA2,\n}\nfunc G() { with Dup { C0.M() } }\n"
	diags, m := meterOf(t, src)
	if len(diags) != 1 || diags[0].Message != wireBudgetMessage || !m.out {
		t.Fatalf("want one budget S111, got %v", diags)
	}
	line := strings.Split(src, "\n")[diags[0].Line-1]
	if !strings.HasPrefix(line, "let ") {
		t.Errorf("the budget S111 is at %q, want a wire's name", line)
	}
	// Under the budget the same duplicate is found.
	diags, _ = meterOf(t, wireFamily("disjoint", 4)+"let Dup = wire {\n\tA3,\n\tA2,\n}\n")
	if len(diags) == 0 || diags[0].Code != CodeDuplicateCap || diags[0].Message == wireBudgetMessage {
		t.Errorf("want the duplicate S111 under the budget, got %v", diags)
	}
}

var usesKeyword = regexp.MustCompile(`\buses\b`)

// TestWireBudgetHeadroom: every fixture, witness, prototype fixture,
// corpus file and example uses under 10% of its wire budget, so the
// budget refuses only sources far from the ones written so far. The
// prototype fixtures and the corpus are their decided translations (#196).
func TestWireBudgetHeadroom(t *testing.T) {
	var files []string
	for _, pattern := range []string{
		"testdata/fixtures/*/*.ergo",
		"testdata/classf/*.ergo",
		"testdata/totality/*.ergo",
		"../proto/testdata/*.ergo",
		"../proto/testdata/*/*.ergo",
		"../syntax/testdata/corpus/*/*.ergo",
		"../syntax/testdata/corpus/*/*/*.ergo",
		"../../../examples/*/*/*.ergo",
		"../../../docs/research/ef-migrate/*.ergo",
	} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) < 300 {
		t.Fatalf("found %d sources", len(files))
	}
	oc := countOps(t)
	worst, worstFile, metered := 0.0, "", 0
	worstOps, worstOpsFile := 0.0, ""
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := usesKeyword.ReplaceAllString(string(data), "needs")
		if _, err := syntax.Parse(src); err != nil {
			continue
		}
		_, c := checkLimited(t, src, nil)
		if c.wires == nil {
			continue
		}
		metered++
		oc.bounded(t, f, src, c)
		share := float64(c.wires.used) / float64(c.wires.limit)
		if share >= 0.10 {
			t.Errorf("%s: %d of %d wire steps (%.1f%%), want under 10%%", f, c.wires.used, c.wires.limit, 100*share)
		}
		if share > worst {
			worst, worstFile = share, f
		}
		if ops := float64(oc.repeat[c.wires]+oc.single[c.wires]) / float64(c.wires.limit); ops > worstOps {
			worstOps, worstOpsFile = ops, f
		}
	}
	t.Logf("%d sources, %d with wires; the largest charged share is %.2f%% (%s); the most operations of both classes, against the budget, %.2f%% (%s)", len(files), metered, 100*worst, worstFile, 100*worstOps, worstOpsFile)
}
