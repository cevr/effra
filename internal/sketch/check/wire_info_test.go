package check

import (
	"fmt"
	"sort"
	"strings"
	"testing"

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
// "Live: Log=Mem{V: "log"} Store=Mem{V: "pkg"}; Mem{V: "a"} by Mem{V: "b"}".
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
			over = append(over, text(o.Entry.Provider)+" by "+text(o.By.Provider))
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
`, []string{`Test: Log=Mem{V: "log"} Store=Mem{V: "test"}; Mem{V: "live"} by Mem{V: "test"}`}},
		{"a replace through two levels of embedding", `let Shared = wire { Store: Mem{V: "live"} }
let Accounts = wire { Log: Mem{V: "log"}, Shared }
let Fixture = wire { replace Store: Mem{V: "fixture"}, Accounts }
`, []string{`Fixture: Log=Mem{V: "log"} Store=Mem{V: "fixture"}; Mem{V: "live"} by Mem{V: "fixture"}`}},
		{"a replace of a replace", `let Shared = wire { Store: Mem{V: "live"}, Log: Mem{V: "log"} }
let Fixture = wire { replace Store: Mem{V: "fixture"}, Shared }
let Outer = wire { replace Store: Mem{V: "outer"}, Fixture }
`, []string{`Outer: Log=Mem{V: "log"} Store=Mem{V: "outer"}; Mem{V: "fixture"} by Mem{V: "outer"}, Mem{V: "live"} by Mem{V: "fixture"}`}},
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
func TestWireInfoLinear(t *testing.T) {
	const n = 4000
	for _, tc := range []struct {
		shape     string
		overrides int
	}{
		{"replace diamond", n - 1},
		{"entry chain", 0},
		{"large-base chain", 0},
		{"wide fan-in", 0},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			pkg, diags := CheckSource(wireFamily(tc.shape, n))
			if len(diags) > 0 {
				t.Fatalf("want a clean check, got %s", describeDiags(diags[:1]))
			}
			if len(pkg.Info.Withs) != 1 {
				t.Fatalf("want one with, got %d", len(pkg.Info.Withs))
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
