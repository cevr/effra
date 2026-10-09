package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// legacyGraphFixtures freeze the no-option `ef graph` JSON independently of
// GraphViewV1 selection. The compiler-level encoding matches the CLI's
// printProjectionJSON; producer metadata is absent because these results are
// not qualified by an executing artifact. The goldens have no regeneration
// switch: changing the legacy wire requires a deliberate version transition.
var legacyGraphFixtures = []struct {
	name, file, source string
}{
	{name: "workflow", file: "../../examples/workflow.ef"},
	{name: "layers", file: "../../examples/layers.ef"},
	{name: "layers-workflow", file: "../../examples/layers-workflow.ef"},
	{name: "callables-factory", file: "../../examples/callables-factory.ef"},
	{name: "latest-task", file: "../../examples/latest-task.ef"},
	{name: "provider-recipes", source: `service Users { effect fn get() -> string }
impl Configured(prefix: string) for Users { effect fn get() -> string { prefix } }
effect fn main() -> string {
  let recipe = Configured("ok")
  let first = run recipe
  let second = run recipe
  let alias = first
  run Users.get().provide<Users>(alias)
}`},
	{name: "layer-diamond", source: layerDiamondSource + `effect fn main() -> string { run Accounts.name().provide(TestApp) }`},
}

func legacyGraphBytes(t *testing.T, source, dir, target string) []byte {
	t.Helper()
	r := CompileAt(source, target, dir)
	if !r.Checked {
		t.Fatalf("fixture is not checked: %v", r.Diagnostics)
	}
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(graph); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestLegacyGraphSchemaVersionIsIndependentOfSemanticResult(t *testing.T) {
	result := Compile(`fn identity(value: string) -> string { value }`)
	if !result.Checked {
		t.Fatalf("fixture is not checked: %v", result.Diagnostics)
	}

	result.SchemaVersion++
	graph, err := result.Graph()
	if err != nil {
		t.Fatal(err)
	}
	if graph.SchemaVersion != GraphSchemaVersion {
		t.Fatalf("legacy graph schema followed semantic result schema: got %d, want graph schema %d", graph.SchemaVersion, GraphSchemaVersion)
	}
	if graph.SchemaVersion == result.SchemaVersion {
		t.Fatalf("fixture did not separate graph and semantic result schemas: graph %d, semantic %d", graph.SchemaVersion, result.SchemaVersion)
	}
}

func TestLegacyGraphPublishesCheckedParameterFactsAndProducerEpoch(t *testing.T) {
	result := Compile(`fn decorate(required value: string, suffix: string = "!") -> string { value + suffix }`)
	if !result.Checked {
		t.Fatalf("fixture is not checked: %v", result.Diagnostics)
	}
	if result.SchemaVersion != SemanticSchemaVersion || SemanticSchemaVersion != 8 {
		t.Fatalf("semantic result epoch = %d, want %d", result.SchemaVersion, SemanticSchemaVersion)
	}

	graph, err := result.Graph()
	if err != nil {
		t.Fatal(err)
	}
	if graph.SchemaVersion != GraphSchemaVersion {
		t.Fatalf("legacy graph epoch = %d, owner = %d", graph.SchemaVersion, GraphSchemaVersion)
	}
	if GraphSchemaVersion != 8 {
		t.Fatalf("legacy graph schema owner = %d; want 8", GraphSchemaVersion)
	}
	if graph.ProducerIdentity != SemanticProducerIdentity {
		t.Fatalf("legacy graph producer identity = %q, owner = %q", graph.ProducerIdentity, SemanticProducerIdentity)
	}
	if SemanticProducerIdentity != "effra/checker-abi-9/bundled-interface-4" {
		t.Fatalf("semantic producer identity owner = %q", SemanticProducerIdentity)
	}

	var callable *CallableType
	for _, node := range graph.Nodes {
		if node.ID == "function:decorate" && node.Contract != nil {
			callable = node.Contract.Callable
			break
		}
	}
	if callable == nil || len(callable.Parameters) != 2 {
		t.Fatalf("legacy graph callable contract missing: %+v", callable)
	}
	if !callable.Parameters[0].RequiredChoice || callable.Parameters[0].DefaultValue != nil {
		t.Fatalf("required-choice graph parameter = %+v", callable.Parameters[0])
	}
	if callable.Parameters[1].RequiredChoice || callable.Parameters[1].DefaultValue == nil || *callable.Parameters[1].DefaultValue != (ConstantValue{Kind: "string", Value: "!"}) {
		t.Fatalf("default graph parameter = %+v", callable.Parameters[1])
	}
}

func TestLegacyGraphJSONGolden(t *testing.T) {
	for _, fixture := range legacyGraphFixtures {
		for _, target := range []string{"go", "js"} {
			t.Run(fixture.name+"-"+target, func(t *testing.T) {
				source, dir := fixture.source, "."
				if fixture.file != "" {
					raw, err := os.ReadFile(fixture.file)
					if err != nil {
						t.Fatal(err)
					}
					source, dir = string(raw), filepath.Dir(fixture.file)
				}
				actual := legacyGraphBytes(t, source, dir, target)
				golden := filepath.Join("testdata", "graph", "legacy", fixture.name+"-"+target+".json")
				expected, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, expected) {
					t.Fatalf("legacy graph JSON changed for %s", golden)
				}
			})
		}
	}
}
