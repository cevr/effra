package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLayerSourceBudgetAdmitsOnlyNativeBoundedIdentityMetadata(t *testing.T) {
	for _, control := range []struct {
		name               string
		size, nodes        int
		dependent, refused bool
	}{
		{"long node identities", 5000, 20, false, true},
		{"empty graph long plan identity", 100000, 0, false, true},
		{"long dependency identities", 1000, 16, true, true},
		{"bounded dependency identities", 1000, 10, true, false},
	} {
		t.Run(control.name, func(t *testing.T) {
			name := "App" + strings.Repeat("x", control.size)
			var source strings.Builder
			for i := 0; i < control.nodes; i++ {
				fmt.Fprintf(&source, "service S%d { effect fn value() -> string } impl P%d for S%d", i, i, i)
				if control.dependent && i > 0 {
					requirements := []string{}
					for j := 0; j < i; j++ {
						requirements = append(requirements, fmt.Sprintf("S%d", j))
					}
					fmt.Fprintf(&source, " uses {%s}", strings.Join(requirements, ","))
				}
				source.WriteString(` { effect fn value() -> string { "ok" } }` + "\n")
			}
			fmt.Fprintf(&source, "layer %s {\n", name)
			for i := 0; i < control.nodes; i++ {
				fmt.Fprintf(&source, "S%d=P%d\n", i, i)
			}
			source.WriteString("}\neffect fn main() -> string {")
			if control.nodes > 0 {
				fmt.Fprintf(&source, "run S0.value().provide(%s)", name)
			} else {
				source.WriteString(`"ok"`)
			}
			source.WriteString("}")
			for _, target := range []string{"go", "js"} {
				r := CompileFor(source.String(), target)
				if hasCode(r, "EF133") != control.refused || r.Checked == control.refused {
					t.Fatal(target, r.Diagnostics)
				}
				if !control.refused {
					plan := r.FindLayer(name)
					metadata := len(plan.ID)
					for _, node := range plan.Nodes {
						metadata += 1 + len(node.ID)
						for _, dependency := range node.Dependencies {
							metadata += 1 + len(dependency)
						}
					}
					if metadata > maxLayerAssemblyMetadata {
						t.Fatal("admitted emitted NodeSpec identity bytes exceed runtime bound", metadata)
					}
				}
			}
		})
	}
}

func TestLayerProvisionRowsVisibilityAndInputPaths(t *testing.T) {
	for _, control := range []struct {
		name, source, code string
	}{
		{"public outputs", `effect fn use() -> string uses {Accounts,Invoice} { let account=run Accounts.name(); let invoice=run Invoice.name(); account+invoice } effect fn main() -> string { run use().provide(App) }`, ""},
		{"hidden output", `effect fn main() -> string { run Database.name().provide(App) }`, "EF108"},
		{"closed input", `layer Open { Accounts=AccountsLive } effect fn main() -> string { run Accounts.name().provide(Open) }`, "EF108"},
		{"borrowed input", `layer Open { Accounts=AccountsLive } effect fn use() -> string uses {Database} { run Accounts.name().provide(Open) }`, ""},
		{"unknown plan", `effect fn main() -> string { run Accounts.name().provide(Absent) }`, "EF102"},
		{"shadowed plan", `effect fn use(App:string) -> string uses {Accounts} { run Accounts.name().provide(App) }`, "EF135"},
		{"pure operand", `effect fn main() -> string { "value".provide(App) }`, "EF105"},
	} {
		t.Run(control.name, func(t *testing.T) {
			r := Compile(layerDiamondSource + control.source)
			if control.code == "" {
				if !r.Checked {
					t.Fatal(r.Diagnostics)
				}
			} else if !hasCode(r, control.code) {
				t.Fatal(r.Diagnostics)
			}
			if control.name == "closed input" {
				for _, diagnostic := range r.Diagnostics {
					if diagnostic.Code == "EF108" && len(diagnostic.Related) == 0 {
						t.Fatal("missing construction input path", diagnostic)
					}
				}
			}
			if control.name == "borrowed input" && !reflect.DeepEqual(r.Find("use").Actual.Services, []string{"Database"}) {
				t.Fatal(r.Find("use"))
			}
		})
	}
}

func TestLayerProvisionRefusesOwnedAndUnknownEscapeButRetainsOuterBorrow(t *testing.T) {
	for _, control := range []struct {
		name, source string
		escape       bool
	}{
		{"file", `layer FilesApp { Files=LiveFiles } effect fn main() -> File raises {IoError} { run Files.openRead("examples/fixture.txt").provide(FilesApp) }`, true},
		{"record file", `record Opened { file:File } effect fn open() -> Opened raises {IoError} uses {Files} { let file=run Files.openRead("examples/fixture.txt"); Opened {file:file} } layer FilesApp { Files=LiveFiles } effect fn main() -> Opened raises {IoError} { run open().provide(FilesApp) }`, true},
		{"outer borrow", `effect fn borrow(file:File) -> File { file } effect fn use(file:File) -> File { run borrow(file).provide(App) }`, false},
		{"pure result", `effect fn main() -> string { run Accounts.name().provide(App) }`, false},
	} {
		t.Run(control.name, func(t *testing.T) {
			r := Compile(layerDiamondSource + control.source)
			if hasCode(r, "EF123") != control.escape {
				t.Fatal(r.Diagnostics)
			}
			if !control.escape && !r.Checked {
				t.Fatal(r.Diagnostics)
			}
		})
	}
}

func TestLayerConfigurationQueriesAndProvisionGraph(t *testing.T) {
	source := `record Settings { label:string } enum Mode { Live Fixture } service Store { effect fn label() -> string } impl Memory(settings:Settings,mode:Mode) for Store { effect fn label() -> string { settings.label } } layer App { Store=Memory(Settings {label:"config"},Mode.Fixture()) } effect fn main() -> string { run Store.label().provide(App) }`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	node := r.FindLayer("App").Nodes[0]
	if len(node.Parameters) != 2 || len(node.Arguments) != 2 || node.Arguments[0].Type.Type.Kind != "record" || node.Arguments[1].Type.Type.Kind != "enum" {
		t.Fatal(node)
	}
	argument := r.Program.Layers[0].Entries[0].Value.Args[0]
	if info, err := r.TypeAt(argument.Span.Offset); err != nil || info.Type.Type.Kind != "record" {
		t.Fatal(info, err)
	}
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, edge := range graph.Edges {
		if edge.Kind == "provides-layer" && edge.To == r.FindLayer("App").ID {
			found = true
		}
	}
	if !found {
		t.Fatal("provision missing from canonical graph", graph)
	}
	inspection, err := r.LayerInspection("App", "test.ef")
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []struct {
		types []TypeNode
		rows  []RowNode
	}{{graph.Types, graph.Rows}, {inspection["types"].([]TypeNode), inspection["rows"].([]RowNode)}} {
		types, rows := map[string]bool{}, map[string]bool{}
		for _, definition := range view.types {
			types[definition.ID] = true
		}
		for _, definition := range view.rows {
			rows[definition.ID] = true
		}
		refs := []TypeRef{node.Constructor.Type, node.Constructor.Contract}
		for _, parameter := range node.Parameters {
			refs = append(refs, parameter.TypeRef)
		}
		for _, argument := range node.Arguments {
			refs = append(refs, argument.Type.Type, argument.Type.Contract)
		}
		for _, ref := range refs {
			for _, id := range append([]string{ref.ID, ref.Result}, ref.ArgIDs...) {
				if id != "" && !types[id] {
					t.Fatalf("layer response has dangling type %q", id)
				}
			}
			for _, id := range []string{ref.FailureRow, ref.ServiceRow} {
				if id != "" && !rows[id] {
					t.Fatalf("layer response has dangling row %q", id)
				}
			}
		}
	}
	for _, target := range []string{"go", "js"} {
		invalid := CompileFor(strings.Replace(source, "Store=Memory(Settings {label:\"config\"},Mode.Fixture())", "Store=Memory(settings:Settings {label:\"config\"},mode:Mode.Fixture())", 1), target)
		if !hasCode(invalid, "EF135") {
			t.Fatal(invalid.Diagnostics)
		}
	}
}

func TestLayerGraphCompatibilityBudgetChargesLayerMetadata(t *testing.T) {
	r := Compile(`effect fn main() -> string { "ok" }`)
	projection := r.ProjectAllTypes()
	if !projection.Complete {
		t.Fatal(projection)
	}
	projection.Limits.CompatibilityBytes = 512
	graph := &DependencyGraph{Layers: []LayerPlan{{Name: strings.Repeat("layer", 256)}}}
	if _, err := r.ValidateProjectionResponse(projection, graph); err == nil || !strings.Contains(err.Error(), "canonical compatibility metadata") {
		t.Fatal("layer metadata escaped its independent compatibility budget", err)
	}
	graph.Layers = nil
	if _, err := r.ValidateProjectionResponse(projection, graph); err != nil {
		t.Fatal("non-layer graph metadata should fit", err)
	}
}
