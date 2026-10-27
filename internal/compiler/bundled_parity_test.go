package compiler

import (
	"reflect"
	"strings"
	"testing"
)

func TestBundledCallersMissingRowsAndLocalHelperEquivalence(t *testing.T) {
	for _, fixture := range []struct{ source, signature, name, failure, service string }{
		{bundledGreeting, "effect fn greeting(id: string) -> string raises {MissingUser} uses {Users}", "greeting", "MissingUser", "Users"},
		{bundledConfiguration, "effect fn configuration(key: string) -> string raises {MissingSetting} uses {Settings}", "configuration", "MissingSetting", "Settings"},
	} {
		imported := CompileFor(fixture.source, "go")
		localSource := strings.Replace(fixture.source, `import Fns "ergo/functions"`, `effect fn call<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} { run callback(input) }`, 1)
		localSource = strings.ReplaceAll(localSource, "Fns.call", "call")
		local := CompileFor(localSource, "go")
		if !imported.Checked || !local.Checked {
			t.Fatal(imported.Diagnostics, local.Diagnostics)
		}
		a, b := imported.Find(fixture.name).Actual, local.Find(fixture.name).Actual
		if a.Success != b.Success || !reflect.DeepEqual(a.Errors, b.Errors) || !reflect.DeepEqual(a.Services, b.Services) {
			t.Fatal("ordinary helper changed caller contract", a, b)
		}
		for _, removal := range []struct{ text, code string }{{" raises {" + fixture.failure + "}", "EF107"}, {" uses {" + fixture.service + "}", "EF108"}} {
			changed := strings.Replace(fixture.signature, removal.text, "", 1)
			for _, source := range []string{fixture.source, localSource} {
				bad := CompileFor(strings.Replace(source, fixture.signature, changed, 1), "go")
				if bad.Checked || !hasCode(bad, removal.code) {
					t.Fatalf("go %s: missing %s: %v", fixture.name, removal.code, bad.Diagnostics)
				}
			}
		}
	}
}

func TestBundledGraphUsesQualifiedReferencesAndFullProvenance(t *testing.T) {
	localGreeting := strings.Replace(bundledGreeting, `import Fns "ergo/functions"`, `effect fn call<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} { run callback(input) }`, 1)
	localGreeting = strings.ReplaceAll(localGreeting, "Fns.call", "call")
	for _, source := range []string{bundledGreeting, bundledConfiguration, localGreeting, `import Fns "ergo/functions"
fn apply(callback: fn(string) -> string, input: string) -> string { callback(input) }
effect fn main() -> string { apply(Fns.identity, "passed") }`} {
		r := CompileFor(source, "go")
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		graph, err := r.Graph()
		if err != nil || !graph.TypeProjectionComplete || !reflect.DeepEqual(graph.Sources, r.Sources) || !reflect.DeepEqual(graph.BundledInterfaces, append([]BundledInterfaceInfo{}, r.BundledInterfaces...)) || graph.ProducerIdentity != SemanticProducerIdentity {
			t.Fatal(err, graph)
		}
		nodes := map[string]GraphNode{}
		for _, node := range graph.Nodes {
			nodes[node.ID] = node
		}
		for _, edge := range graph.Edges {
			if _, exists := nodes[edge.From]; !exists {
				t.Fatal("dangling graph origin", edge)
			}
			if _, exists := nodes[edge.To]; !exists {
				t.Fatal("dangling graph destination", edge)
			}
		}
		if strings.Contains(source, "Fns.identity") {
			found := false
			for _, edge := range graph.Edges {
				found = found || edge.Kind == "references" && edge.To == "function:ergo/functions.identity"
			}
			if !found {
				t.Fatal("passed imported function missing from dependencies", graph.Edges)
			}
		}
	}
}

func TestBundledSelectedTemplateDefinitions(t *testing.T) {
	name := "LocalCodec"
	name = "Codec" // Go admits a distinct same-name local declaration.
	source := strings.Replace(bundledAnnotatedWitness, "record User", "record "+name+" { local: string } record User", 1)
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "Convert.witness") + len("Convert."))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range []TypeProjection{r.ProjectSymbol(r.Find("make")), r.ProjectExpression(info), {Types: graph.Types, Complete: graph.TypeProjectionComplete}} {
		if !projection.Complete {
			t.Fatal(projection.Error)
		}
		declarations := r.ProjectionDeclarations(projection)
		found := false
		for _, node := range projection.Types {
			if node.Kind != "application" {
				continue
			}
			resolved := false
			for _, declaration := range declarations {
				if declaration.Identity == node.Declaration {
					resolved = declaration.Kind == "template" && len(declaration.Fields) == 2 && len(declaration.TemplateParameters) == 4 && declaration.Source == "source:ergo/conversions/Codec"
				}
			}
			if !resolved {
				t.Fatalf("go application lacks qualified template definition: %+v %+v", node, declarations)
			}
			found = true
		}
		if !found {
			t.Fatal("fixture did not select an application")
		}
		for _, declaration := range declarations {
			if declaration.Kind == "record" && declaration.Name == name {
				t.Fatal("unused local same-name declaration entered selected closure", declaration)
			}
		}
	}
}

func TestBundledTemplateInspectionRespectsQualifiedBindings(t *testing.T) {
	r := Compile(strings.Replace(bundledUserWitness, "record User", "record Codec { local: string } record User", 1))
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	local, imported := r.FindDeclaration("Codec"), r.FindDeclaration("Convert.Codec")
	if local == nil || imported == nil || local.Kind != "record" || imported.Kind != "template" || local.Identity == imported.Identity || imported.Source != "source:ergo/conversions/Codec" {
		t.Fatal(local, imported)
	}
	if projection := r.ProjectDeclaration(imported); !projection.Complete {
		t.Fatal(projection.Error)
	}
	if r.FindDeclaration("Unknown.Codec") != nil {
		t.Fatal("unknown alias rebound by printed type name")
	}
}

func TestBundledUnusedImportsAddNoExecutableOrInitializationRoots(t *testing.T) {
	baseline := CompileFor(`effect fn main() -> string { "unused" }`, "go")
	unused := CompileFor(`import Fns "ergo/functions" import Convert "ergo/conversions" effect fn main() -> string { "unused" }`, "go")
	if !baseline.Checked || !unused.Checked || len(unused.Program.BundledFunctions) != 0 || len(unused.Program.BundledTemplates) != 0 || len(unused.BundledBindings) != 0 || len(unused.BundledInterfaces) != 0 {
		t.Fatal(unused.Diagnostics)
	}
	a, err := baseline.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	b, err := unused.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	a = strings.Replace(a, "Source revision: "+baseline.Revision, "Source revision: INPUT", 1)
	b = strings.Replace(b, "Source revision: "+unused.Revision, "Source revision: INPUT", 1)
	if a != b {
		t.Fatal("unused library import changed native implementation")
	}
}
