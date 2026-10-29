package check

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/syntax"
)

// TestWireInfo (spec D-A4; round-1 review M4): a named wire's entries are
// in Info, so the lowering reads the 196m table (Store.Get is Get(Mem))
// and never re-derives a resolution. Each with of the wire holds the
// wire's entries; a provider of one capability has its ConvOperations
// Implicit; an entry of two capabilities has a conversion for each; an
// embedding names the embedded wire.
func TestWireInfo(t *testing.T) {
	const src = `type Store { Get() string }
type Log { Put(s string) }
type Mem { V string }
func Get(m Mem) string { return m.V }
func Put(m Mem, s string) {}
func Run() string needs Store, Log {
	Log.Put("x")
	return Store.Get()
}
let Base = wire { Log: Mem{V: "log"} }
let Live = wire { Store: Mem{V: "pkg"}, Base }
let Both = wire { Store, Log: Mem{V: "both"} }
func a() string {
	return with Live { Run() }
}
func b() string {
	return with Both { Run() }
}
`
	file, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, diags := Check("main", []*syntax.File{file}, nil)
	if len(diags) > 0 {
		t.Fatalf("want a clean check, got %s", describeDiags(diags))
	}
	text := func(x syntax.Expr) string { return src[x.Pos():x.End()] }
	table := func(conv Conversion) string {
		var parts []string
		for _, op := range []string{"Get", "Put"} {
			if fn := conv.Table[op]; fn != nil {
				parts = append(parts, op+"="+fn.Name)
			}
		}
		return strings.Join(parts, ",")
	}
	live, both, base := pkg.Wires["Live"], pkg.Wires["Both"], pkg.Wires["Base"]
	if len(live.Entries) != 2 || live.Entries[1].Embedded != base {
		t.Fatalf("Live: want a provider entry and the embedding of Base, got %+v", live.Entries)
	}
	store := live.Entries[0]
	if text(store.Provider) != `Mem{V: "pkg"}` || len(store.Caps) != 1 || store.Caps[0].String() != "main.Store" {
		t.Fatalf("Live's provider entry: %s for %v", text(store.Provider), store.Caps)
	}
	if conv := store.Conversions[0]; conv.Kind != ConvOperations || table(conv) != "Get=Get" {
		t.Fatalf("Live's conversion: kind %d, table %s", conv.Kind, table(conv))
	}
	if conv, ok := pkg.Info.Implicit[store.Provider]; !ok || table(conv) != "Get=Get" {
		t.Fatalf("Live's provider has no Implicit conversion to Store")
	}
	if len(both.Entries) != 1 || len(both.Entries[0].Conversions) != 2 {
		t.Fatalf("Both: want one entry with two conversions, got %+v", both.Entries)
	}
	if got := table(both.Entries[0].Conversions[0]) + ";" + table(both.Entries[0].Conversions[1]); got != "Get=Get;Put=Put" {
		t.Fatalf("Both's conversions: %s", got)
	}
	withs := 0
	for x, w := range pkg.Info.Withs {
		withs++
		if len(w.Wires) != 1 {
			t.Fatalf("the with at %d binds %d named wires", x.Pos(), len(w.Wires))
		}
		if got, want := len(w.Entries), len(w.Wires[0].Entries); got != want || w.Entries[0] != w.Wires[0].Entries[0] {
			t.Fatalf("the with of %s holds %d entries, want its wire's %d", w.Wires[0].Name, got, want)
		}
	}
	if withs != 2 {
		t.Fatalf("want 2 with sites, got %d", withs)
	}
	if gaps := infoGaps(file, src, pkg.Info); len(gaps) > 0 {
		t.Fatalf("Info gaps:\n\t%s", strings.Join(gaps, "\n\t"))
	}
}
