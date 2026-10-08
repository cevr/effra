package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// legacyGraphFixtures freeze the no-option `ef graph` JSON before GraphViewV1
// selection exists. The compiler-level encoding matches the CLI's
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
