package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const layerDiamondSource = `
service Database { effect fn name() -> string }
service Accounts { effect fn name() -> string }
service Invoice { effect fn name() -> string }
impl Store(label: string) for Database { effect fn name() -> string { label } }
impl Fixture for Database { effect fn name() -> string { "fixture" } }
impl AccountsLive for Accounts uses {Database} { effect fn name() -> string { run Database.name() } }
impl InvoiceLive for Invoice uses {Database} { effect fn name() -> string { run Database.name() } }
layer Shared { Database = Store("live") }
layer AccountDomain { merge Shared; Accounts = AccountsLive }
layer InvoiceDomain { merge Shared; Invoice = InvoiceLive }
layer App provides {Accounts, Invoice} { merge AccountDomain, InvoiceDomain }
layer TestApp { merge App; replace Database = Fixture }
`

func TestLayerDiamondVisibilityAndReplacement(t *testing.T) {
	r := Compile(layerDiamondSource)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	app := r.FindLayer("App")
	if len(app.Nodes) != 3 || !reflect.DeepEqual(app.Provides, []string{"Accounts", "Invoice"}) || len(app.Requirements) != 0 {
		t.Fatalf("app: %+v", app)
	}
	var database LayerNode
	for _, node := range app.Nodes {
		if node.Service == "Database" {
			database = node
		}
	}
	if database.Public || len(database.Occurrences) != 2 || len(database.Incoming) != 2 {
		t.Fatalf("shared hidden database: %+v", database)
	}
	test := r.FindLayer("TestApp")
	for _, node := range test.Nodes {
		if node.Service == "Database" && (node.ID != database.ID || node.Public || node.Implementation != "Fixture" || len(node.Replacements) != 1) {
			t.Fatalf("replacement: %+v", node)
		}
	}
	response, err := r.LayerInspection("TestApp")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(encoded), database.ID) {
		t.Fatalf("inspection incomplete: %s %v", encoded, err)
	}
	graph, err := r.Graph()
	if err != nil || len(graph.Layers) != 5 {
		t.Fatalf("graph parity: %+v %v", graph, err)
	}
}

func TestLayerDiagnosticControls(t *testing.T) {
	controls := []struct{ name, source, code string }{
		{"distinct bindings", `layer Other { Database = Store("live") } layer Bad { merge Shared, Other }`, "EF130"},
		{"sibling replacements", `layer A { merge Shared; replace Database = Fixture } layer B { merge Shared; replace Database = Store("other") } layer Bad { merge A,B }`, "EF131"},
		{"wrong implementation", `layer Bad { Accounts = Fixture }`, "EF104"},
		{"missing config", `layer Bad { Database = Store }`, "EF106"},
		{"uses bound", `layer Bad uses {} { Accounts = AccountsLive }`, "EF134"},
		{"hidden output", `layer Bad provides {Database} { merge App }`, "EF134"},
		{"unavailable output", `layer Bad provides {Accounts} { merge Shared }`, "EF134"},
		{"unknown output", `layer Bad provides {Absent} { merge Shared }`, "EF102"},
		{"unknown bound", `layer Bad raises {Absent} { merge Shared }`, "EF102"},
		{"unknown replace", `layer Bad { merge Shared; replace Accounts = AccountsLive }`, "EF131"},
		{"same declaration replacement", `layer Bad { merge Shared; replace Database = Fixture; replace Database = Store("other") }`, "EF131"},
		{"merge cycle", `layer A { merge B } layer B { merge A }`, "EF132"},
		{"effect factory", `layer Bad { Database = buildStore() }`, "EF135"},
		{"dynamic config", `fn label() -> string { "x" } layer Bad { Database = Store(label()) }`, "EF135"},
		{"startup", `layer Bad { start initialize() }`, "EF002"},
		{"parameters", `layer Bad(config: string) { Database = Fixture }`, "EF002"},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			r := Compile(layerDiamondSource + control.source)
			found := false
			for _, diagnostic := range r.Diagnostics {
				if diagnostic.Code == control.code {
					found = true
					if diagnostic.Span.Length == 0 {
						t.Errorf("missing source anchor: %+v", diagnostic)
					}
				}
			}
			if r.Checked || !found {
				t.Fatalf("wanted %s, got %+v", control.code, r.Diagnostics)
			}
			if _, err := r.LayerInspection("App"); err == nil {
				t.Fatal("invalid source published inspection")
			}
		})
	}
}

func TestLayerOpenInputsAndResolvedReplacement(t *testing.T) {
	for _, source := range []string{
		`layer Open { Accounts = AccountsLive }`,
		`layer A { merge Shared; replace Database = Fixture } layer B { merge Shared; replace Database = Store("other") } layer Resolved { merge A,B; replace Database = Fixture }`,
	} {
		r := Compile(layerDiamondSource + source)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		if open := r.FindLayer("Open"); open != nil && !reflect.DeepEqual(open.Requirements, []string{"Database"}) {
			t.Fatalf("open inputs: %+v", open)
		}
	}
}

func TestLayerReplacementRecomputesConstructionCycles(t *testing.T) {
	source := layerDiamondSource + `
impl CycleStore for Database uses {Accounts} { effect fn name() -> string { run Accounts.name() } }
layer Cyclic { merge AccountDomain; replace Database = CycleStore }
`
	r := Compile(source)
	found := false
	for _, d := range r.Diagnostics {
		if d.Code == "EF132" {
			found = true
			if len(d.Related) < 2 {
				t.Fatalf("cycle needs construction path anchors: %+v", d)
			}
		}
	}
	if !found {
		t.Fatal(r.Diagnostics)
	}
	// Replacing the cycle-introducing constructor removes its old edge.
	r = Compile(strings.Replace(source, "layer Cyclic { merge AccountDomain; replace Database = CycleStore }", `layer Fixed { merge AccountDomain; replace Database = Fixture }`, 1))
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
}

func TestLayerFormatterRoundTrip(t *testing.T) {
	formatted, err := FormatSource(layerDiamondSource)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatSource(formatted.Text)
	if err != nil || second.Text != formatted.Text {
		t.Fatalf("formatter not idempotent:\n%s\n%s %v", formatted.Text, second.Text, err)
	}
	r := Compile(formatted.Text)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if !strings.Contains(formatted.Text, "provides { Accounts, Invoice }") {
		t.Fatal(formatted.Text)
	}
}
