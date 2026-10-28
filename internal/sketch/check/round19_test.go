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

// wireFamily writes one generated wire program of size n.
func wireFamily(shape string, n int) string {
	var b strings.Builder
	b.WriteString("package p\ntype P struct{}\nfunc (P) M() {}\n")
	caps := n
	if shape == "disjoint" || shape == "large-base chain" {
		caps = 2 * n
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
	default:
		panic(shape)
	}
	fmt.Fprintf(&b, "func F() { with(%s) { C0.M() } }\n", last)
	return b.String()
}

// TestWireBudget: on every embedding shape, the wire summaries' counted
// work stays within the package's budget, every treap node visited or
// copied was counted, and the verdict is the exact one (acceptance) or the
// one budget S111 (D16 as amended in round 18). The disjoint ladder's
// answer is quadratic, so it is refused at every size here; the chains and
// the fan-in are accepted.
func TestWireBudget(t *testing.T) {
	for _, family := range []struct {
		shape   string
		sizes   []int
		refused bool
	}{
		{"entry chain", []int{1000, 4000, 8000}, false},
		{"wide fan-in", []int{1000, 4000, 8000}, false},
		{"large-base chain", []int{1000, 4000, 8000}, false},
		{"replace diamond", []int{1000, 4000, 8000}, false},
		{"disjoint", []int{128, 256, 512, 2048}, true},
	} {
		for _, n := range family.sizes {
			diags, m := meterOf(t, wireFamily(family.shape, n))
			if m == nil {
				t.Fatalf("%s n=%d: no wire meter", family.shape, n)
			}
			if m.used > m.limit || m.visits+m.copies > m.used {
				t.Errorf("%s n=%d: %d steps of %d, %d visits and %d copies", family.shape, n, m.used, m.limit, m.visits, m.copies)
			}
			switch {
			case len(diags) == 0 && !family.refused:
			case len(diags) == 1 && diags[0].Code == CodeDuplicateCap && diags[0].Message == wireBudgetMessage && family.refused:
			default:
				t.Errorf("%s n=%d: want %s, got %v", family.shape, n, map[bool]string{false: "acceptance", true: "the budget S111"}[family.refused], diags)
			}
			t.Logf("%s n=%d: %d of %d steps (%.0f%%), %d visits, %d copies, %d diagnostics", family.shape, n, m.used, m.limit, 100*float64(m.used)/float64(m.limit), m.visits, m.copies, len(diags))
		}
	}
}

// TestWireBudgetRefusal: past the budget, the wire being summarised gets
// the one S111 and nothing else follows from it: a with of a refused wire
// discharges everything, and a wire body summarised later reports no
// duplicate (D3). A wire whose own body passes the budget gets the S111 at
// its name.
func TestWireBudgetRefusal(t *testing.T) {
	src := wireFamily("disjoint", 512) + "wire Dup {\n\tA3\n\tA2\n}\nfunc G() { with(Dup) { C0.M() } }\n"
	diags, m := meterOf(t, src)
	if len(diags) != 1 || diags[0].Message != wireBudgetMessage || !m.out {
		t.Fatalf("want one budget S111, got %v", diags)
	}
	line := strings.Split(src, "\n")[diags[0].Line-1]
	if !strings.HasPrefix(line, "wire ") {
		t.Errorf("the budget S111 is at %q, want a wire's name", line)
	}
	// Under the budget the same duplicate is found.
	diags, _ = meterOf(t, wireFamily("disjoint", 4)+"wire Dup {\n\tA3\n\tA2\n}\n")
	if len(diags) == 0 || diags[0].Code != CodeDuplicateCap || diags[0].Message == wireBudgetMessage {
		t.Errorf("want the duplicate S111 under the budget, got %v", diags)
	}
}

var usesKeyword = regexp.MustCompile(`\buses\b`)

// TestWireBudgetHeadroom: every fixture, witness, prototype fixture,
// corpus file and example uses under 10% of its wire budget, so the
// budget refuses only sources far from the ones written so far.
func TestWireBudgetHeadroom(t *testing.T) {
	var files []string
	for _, pattern := range []string{
		"testdata/fixtures/*/*.ef",
		"testdata/classf/*.ef",
		"testdata/totality/*.ef",
		"../../../prototypes/sketch9/testdata/*.sketch",
		"../../../prototypes/sketch9/testdata/*/*.sketch",
		"../syntax/testdata/corpus/*/*.sketch",
		"../syntax/testdata/corpus/*/*/*.sketch",
		"../syntax/testdata/corpus/*/*/*/*.sketch",
		"../../../examples/*/*/*.ef",
		"../../../docs/research/ef-migrate/*.ef",
	} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) < 300 {
		t.Fatalf("found %d sources", len(files))
	}
	worst, worstFile, metered := 0.0, "", 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := syntax.Parse(usesKeyword.ReplaceAllString(string(data), "needs"))
		if err != nil {
			continue
		}
		c := newChecker("main", Std, prelude().scope)
		c.run([]*syntax.File{file})
		if c.wires == nil {
			continue
		}
		metered++
		share := float64(c.wires.used) / float64(c.wires.limit)
		if share >= 0.10 {
			t.Errorf("%s: %d of %d wire steps (%.1f%%), want under 10%%", f, c.wires.used, c.wires.limit, 100*share)
		}
		if share > worst {
			worst, worstFile = share, f
		}
	}
	t.Logf("%d sources, %d with wires; the largest share is %.2f%% (%s)", len(files), metered, 100*worst, worstFile)
}
