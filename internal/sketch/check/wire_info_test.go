package check

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"ergo.local/prototype/internal/sketch/syntax"
)

// wireInfoPrelude declares two capabilities, a provider record that
// satisfies both through its functions (196m), and a function that needs
// both.
const wireInfoPrelude = `type Store { Get() string }
type Log { Put(s string) }
type Mem { V string }
func Get(m Mem) string { return m.V }
func Put(m Mem, s string) {}
func Run() string needs Store, Log {
	Log.Put("x")
	return Store.Get()
}
`

// withSummary writes each with site of src, in source order, as the
// provider text bound to each discharged capability, then the overrides:
// "Live: Log=Mem{V: "log"} Store=Mem{V: "pkg"}; Store: Mem{V: "a"} by Mem{V: "b"}".
func withSummary(t *testing.T, src string) []string {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, diags := Check("main", []*syntax.File{file}, nil)
	if len(diags) > 0 {
		t.Fatalf("want a clean check, got %s", describeDiags(diags))
	}
	if gaps := infoGaps(file, src, pkg.Info); len(gaps) > 0 {
		t.Fatalf("Info gaps:\n\t%s", strings.Join(gaps, "\n\t"))
	}
	text := func(x syntax.Expr) string { return src[x.Pos():x.End()] }
	type site struct {
		pos  int
		line string
	}
	var sites []site
	for x, w := range pkg.Info.Withs {
		var parts []string
		for i, d := range w.Discharged {
			parts = append(parts, fmt.Sprintf("%s=%s", strings.TrimPrefix(typeString(d), "main."), text(w.Entries[i].Provider)))
		}
		line := w.Wires[0].Name + ": " + strings.Join(parts, " ")
		var over []string
		for _, o := range w.Overridden {
			over = append(over, strings.TrimPrefix(typeString(o.Cap), "main.")+": "+text(o.Entry.Provider)+" by "+text(o.By.Provider))
		}
		if len(over) > 0 {
			line += "; " + strings.Join(over, ", ")
		}
		sites = append(sites, site{int(x.Pos()), line})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].pos < sites[j].pos })
	var out []string
	for _, s := range sites {
		out = append(out, s.line)
	}
	return out
}

// TestWireInfo (spec D-A4; round-1 review M4, round-2 review M1): a named
// wire's bindings are in Info, so the lowering reads them and never
// re-derives a resolution. Each with holds exactly one provider entry per
// discharged capability, after replace and embedding; the providers a
// replace overrode are in Overridden with the entry that replaced each.
func TestWireInfo(t *testing.T) {
	cases := []struct {
		name, wires string
		want        []string
	}{
		{"own entries and an embedding", `let Base = wire { Log: Mem{V: "log"} }
let Live = wire { Store: Mem{V: "pkg"}, Base }
`, []string{`Live: Log=Mem{V: "log"} Store=Mem{V: "pkg"}`}},
		{"an entry of two capabilities", `let Both = wire { Store, Log: Mem{V: "both"} }
`, []string{`Both: Log=Mem{V: "both"} Store=Mem{V: "both"}`}},
		{"a replace of an embedded entry", `let Live = wire { Store: Mem{V: "live"}, Log: Mem{V: "log"} }
let Test = wire { replace Store: Mem{V: "test"}, Live }
`, []string{`Test: Log=Mem{V: "log"} Store=Mem{V: "test"}; Store: Mem{V: "live"} by Mem{V: "test"}`}},
		{"a replace through two levels of embedding", `let Shared = wire { Store: Mem{V: "live"} }
let Accounts = wire { Log: Mem{V: "log"}, Shared }
let Fixture = wire { replace Store: Mem{V: "fixture"}, Accounts }
`, []string{`Fixture: Log=Mem{V: "log"} Store=Mem{V: "fixture"}; Store: Mem{V: "live"} by Mem{V: "fixture"}`}},
		{"a replace of a replace", `let Shared = wire { Store: Mem{V: "live"}, Log: Mem{V: "log"} }
let Fixture = wire { replace Store: Mem{V: "fixture"}, Shared }
let Outer = wire { replace Store: Mem{V: "outer"}, Fixture }
`, []string{`Outer: Log=Mem{V: "log"} Store=Mem{V: "outer"}; Store: Mem{V: "fixture"} by Mem{V: "outer"}, Store: Mem{V: "live"} by Mem{V: "fixture"}`}},
		// A shared entry replaced for one capability still binds the other
		// (round-3 review m1): it is in Entries for Log, overridden for
		// Store only.
		{"a replace of one capability of a shared entry", `let Both = wire { Store, Log: Mem{V: "both"} }
let W = wire { replace Store: Mem{V: "one"}, Both }
`, []string{`W: Log=Mem{V: "both"} Store=Mem{V: "one"}; Store: Mem{V: "both"} by Mem{V: "one"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The with uses the last wire declared.
			lines := strings.Split(strings.TrimSpace(tc.wires), "\n")
			name := strings.Fields(lines[len(lines)-1])[1]
			src := wireInfoPrelude + tc.wires + "func a() string {\n\treturn with " + name + " { Run() }\n}\n"
			got := withSummary(t, src)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("want\n\t%s\ngot\n\t%s", strings.Join(tc.want, "\n\t"), strings.Join(got, "\n\t"))
			}
		})
	}
}

// TestWireInfoLinear (round 3, "Runaway allocation"): the bindings of a
// with are linear in the wires it reaches. A first resolution merged each
// embedded wire's override list into its embedder's, so a replaced diamond
// (each layer joins two branches over the layer below) doubled the list
// per layer: 2^n entries, which grew to 44 GB at n = 8000 in
// TestWireBudget. Each shape here checks in bounded memory, with one entry
// per discharged capability and one override per replace.
//
// Round-3 review M1: each wire is resolved once per package and every with
// reuses the result, so the reviewer's chain (n wires, each embedding the
// one before, one with per wire) is linear too. The counted work
// (Package.bindWork: wires, entries, and the nodes and entries a result
// reads) stays within bindWorkPerUnit × (wires + entries + the size of
// the bindings the withs hold).
func TestWireInfoLinear(t *testing.T) {
	const n = 4000
	for _, tc := range []struct {
		shape     string
		withs     int
		overrides int
	}{
		{"replace diamond", 1, n - 1},
		{"entry chain", 1, 0},
		{"large-base chain", 1, 0},
		{"wide fan-in", 1, 0},
		{"with chain", n, 0},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			src := ""
			if tc.shape == "with chain" {
				src = withChain(n)
			} else {
				src = wireFamily(tc.shape, n)
			}
			pkg, diags := CheckSource(src)
			if len(diags) > 0 {
				t.Fatalf("want a clean check, got %s", describeDiags(diags[:1]))
			}
			if len(pkg.Info.Withs) != tc.withs {
				t.Fatalf("want %d withs, got %d", tc.withs, len(pkg.Info.Withs))
			}
			units := len(pkg.Wires)
			for _, w := range pkg.Wires {
				units += len(w.Entries)
			}
			for _, w := range pkg.Info.Withs {
				units += len(w.Entries) + len(w.Overridden)
			}
			if pkg.bindWork > bindWorkPerUnit*units {
				t.Fatalf("the bindings cost %d steps for %d units, over %d per unit", pkg.bindWork, units, bindWorkPerUnit)
			}
			for _, w := range pkg.Info.Withs {
				if len(w.Entries) != len(w.Discharged) || len(w.Overridden) != tc.overrides {
					t.Fatalf("%d capabilities: %d entries, %d overrides (want %d)", len(w.Discharged), len(w.Entries), len(w.Overridden), tc.overrides)
				}
				for i, e := range w.Entries {
					if e == nil || typeString(e.Caps[0]) != typeString(w.Discharged[i]) {
						t.Fatalf("the entry for %s does not provide it", typeString(w.Discharged[i]))
					}
				}
			}
		})
	}
}

// bindWorkPerUnit bounds the counted binding work per wire, entry and
// binding held (TestWireInfoLinear).
const bindWorkPerUnit = 4

// withChain is the reviewer's chain (round-3 review M1): W0 binds Store,
// each Wi embeds W(i-1), and one function per wire runs with Wi.
func withChain(n int) string {
	var b strings.Builder
	b.WriteString("type Store { M() }\ntype P {}\nfunc M(_ P) {}\nlet W0 = wire { Store: P{} }\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "let W%d = wire { W%d }\n", i, i-1)
	}
	for i := range n {
		fmt.Fprintf(&b, "func F%d() { with W%d { Store.M() } }\n", i, i)
	}
	return b.String()
}

// TestWireInfoChainScale (round-3 review M1): checking the with chain at
// 4n takes under 8 times as long as at n (the acceptance rule's ratio),
// the best of three runs each, taken in turn so a load change on the
// machine (the gate runs packages in parallel) reaches both sizes.
func TestWireInfoChainScale(t *testing.T) {
	srcs := map[int]string{4000: withChain(4000), 16000: withChain(16000)}
	best := map[int]time.Duration{}
	for range 3 {
		for _, n := range []int{4000, 16000} {
			start := time.Now()
			if _, diags := CheckSource(srcs[n]); len(diags) > 0 {
				t.Fatalf("n = %d: want a clean check, got %s", n, describeDiags(diags[:1]))
			}
			if d := time.Since(start); best[n] == 0 || d < best[n] {
				best[n] = d
			}
		}
	}
	small, large := best[4000], best[16000]
	if ratio := float64(large) / float64(small); ratio >= 8 {
		t.Fatalf("t(16000) = %v is %.1f × t(4000) = %v; want under 8", large, ratio, small)
	} else {
		t.Logf("t(4000) = %v, t(16000) = %v, ratio %.1f", small, large, ratio)
	}
}

// TestWireInfoConversions: each provider entry has a conversion per
// capability (the 196m table: Store.Get is Get(Mem)), and a provider of
// one capability has its ConvOperations Implicit.
func TestWireInfoConversions(t *testing.T) {
	src := wireInfoPrelude + `let Live = wire { Store: Mem{V: "pkg"}, Log: Mem{V: "log"} }
let Both = wire { Store, Log: Mem{V: "both"} }
`
	pkg, diags := CheckSource(src)
	if len(diags) > 0 {
		t.Fatalf("want a clean check, got %s", describeDiags(diags))
	}
	table := func(conv Conversion) string {
		var parts []string
		for _, op := range []string{"Get", "Put"} {
			if fn := conv.Table[op]; fn != nil {
				parts = append(parts, op+"="+fn.Name)
			}
		}
		return strings.Join(parts, ",")
	}
	store := pkg.Wires["Live"].Entries[0]
	if conv := store.Conversions[0]; conv.Kind != ConvOperations || table(conv) != "Get=Get" {
		t.Fatalf("Live's Store conversion: kind %d, table %s", conv.Kind, table(conv))
	}
	if conv, ok := pkg.Info.Implicit[store.Provider]; !ok || table(conv) != "Get=Get" {
		t.Fatalf("Live's Store provider has no Implicit conversion")
	}
	both := pkg.Wires["Both"].Entries[0]
	if got := table(both.Conversions[0]) + ";" + table(both.Conversions[1]); got != "Get=Get;Put=Put" {
		t.Fatalf("Both's conversions: %s", got)
	}
}
