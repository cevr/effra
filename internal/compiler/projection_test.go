package compiler

import (
	"effra.local/prototype/internal/producer"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestProjectionJSONAccountingMatchesActualEncoding(t *testing.T) {
	values := []any{nil, true, 42, []string{}, []string(nil), map[string]any{"escaped": "\"\\\n<>&\u2028é", "nested": []any{false, 17, "ok"}}, TypeRef{ID: "t:one", ArgIDs: []string{"t:two"}}, Compile(`effect fn main() -> string { "ok" }`).CheckResponse()}
	for _, qualified := range []bool{false, true} {
		for _, declared := range []bool{false, true} {
			r := Compile(`effect fn main() -> string { "ok" }`)
			if qualified {
				identity := producer.Identity{Strength: "unavailable", ReuseScope: "process", Qualifier: "process:oracle", Reason: "unavailable"}
				if declared {
					identity.Declaration = producer.Declaration{GoVersion: "go1.27.0", Module: "effra.local/prototype", VCSModified: "true"}
				}
				if err := r.Qualify(identity); err != nil {
					t.Fatal(err)
				}
			}
			graph, err := r.Graph()
			if err != nil {
				t.Fatal(err)
			}
			values = append(values, graph, r.DiagnosticReport(SourceSnapshot{Text: `effect fn main() -> string { "ok" }`, Origin: "buffer"}, false), r.Lint(false), r.CheckResponse())
		}
	}
	values = append(values, struct {
		ProducerMetadata `json:"named"`
		Empty            Span `json:"empty,omitempty"`
	}{})
	values = append(values, struct{ *ProducerMetadata }{}, struct{ *ProducerMetadata }{&ProducerMetadata{}})
	type left struct {
		Name string `json:"same"`
	}
	type right struct {
		Name string `json:"same"`
	}
	// Build intentional JSON-name collisions reflectively: go vet correctly
	// rejects these shapes as production declarations, while encoding/json's
	// field-dominance rule still provides an independent accounting oracle.
	ambiguous := reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "Left", Type: reflect.TypeOf(left{}), Anonymous: true},
		{Name: "Right", Type: reflect.TypeOf(right{}), Anonymous: true},
	})).Elem()
	ambiguous.Field(0).Set(reflect.ValueOf(left{"hidden"}))
	ambiguous.Field(1).Set(reflect.ValueOf(right{"also hidden"}))
	direct := reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "Left", Type: reflect.TypeOf(left{}), Anonymous: true},
		{Name: "Name", Type: reflect.TypeOf(""), Tag: `json:"same"`},
	})).Elem()
	direct.Field(0).Set(reflect.ValueOf(left{"hidden"}))
	direct.Field(1).SetString("visible")
	values = append(values, ambiguous.Interface(), direct.Interface())
	for _, value := range values {
		actual, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if graph, ok := value.(*DependencyGraph); ok && graph.TypeProjectionUsage.ResponseBytes != len(actual) {
			t.Fatalf("graph published incorrect receipt: %d vs %d", graph.TypeProjectionUsage.ResponseBytes, len(actual))
		}
		size, err := encodedSize(value, len(actual))
		if err != nil || size != len(actual) {
			t.Fatalf("wire accounting differs for %T: %d vs %d (%v)", value, size, len(actual), err)
		}
		if _, err := encodedSize(value, len(actual)-1); err == nil {
			t.Fatalf("one byte overflow accepted for %T", value)
		}
		quoted, _ := json.Marshal(string(actual))
		size, err = encodedSizeMode(value, len(quoted), true)
		if err != nil || size != len(quoted) {
			t.Fatalf("MCP text accounting differs for %T: %d vs %d (%v)", value, size, len(quoted), err)
		}
	}
}

func TestSelectedTestCatalogKeepsWholeRefusalAndExecutionIndependent(t *testing.T) {
	var source strings.Builder
	source.WriteString("effect fn wide(")
	for i := 0; i < 2048; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		fmt.Fprintf(&source, "p%d: string", i)
	}
	source.WriteString(") -> string { \"wide\" }\neffect fn test_tiny() -> () { () }")
	r := Compile(source.String())
	if !r.Checked || r.TypeProjectionComplete {
		t.Fatal("fixture must check with whole-source refusal")
	}
	before, _ := json.Marshal(r.Symbols)
	errorBefore := r.TypeProjectionError
	tests, err := r.Tests()
	if err != nil {
		t.Fatal(err)
	}
	selected, projection := r.ProjectTestCatalog(tests)
	if !projection.Complete || len(selected) != 1 || selected[0].Name != "test_tiny" {
		t.Fatalf("selected test unavailable: %+v", projection)
	}
	after, _ := json.Marshal(r.Symbols)
	if string(before) != string(after) || r.TypeProjectionError != errorBefore || r.TypeProjectionComplete {
		t.Fatal("catalog mutated stored whole-source refusal")
	}
	if _, err := r.EmitGoTests(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.EmitJSTests(); err != nil {
		t.Fatal(err)
	}
	// A tiny budget must refuse publication while executable roots stay valid.
	r.TypeProjectionLimits = defaultProjectionLimits
	r.TypeProjectionLimits.CompatibilityBytes = 1
	if catalog, p := r.ProjectTestCatalog(tests); p.Complete || catalog != nil || p.Error == "" {
		t.Fatal("catalog budget refusal retained partial authority")
	}
	if _, err := r.EmitGoTests(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.EmitJSTests(); err != nil {
		t.Fatal(err)
	}
	pair := Compile("effect fn test_one() -> () { () }\neffect fn test_two() -> () { () }")
	pairTests, err := pair.Tests()
	if err != nil {
		t.Fatal(err)
	}
	budget := 0
	for _, test := range pairTests {
		_, p := pair.ProjectTestCatalog([]*Symbol{test})
		if !p.Complete {
			t.Fatal(p.Error)
		}
		budget = max(budget, p.Usage.CompatibilityBytes)
	}
	pair.TypeProjectionLimits.CompatibilityBytes = budget
	for _, test := range pairTests {
		if _, p := pair.ProjectTestCatalog([]*Symbol{test}); !p.Complete {
			t.Fatal("individual root no longer fits catalog budget", p.Error)
		}
	}
	if catalog, p := pair.ProjectTestCatalog(pairTests); p.Complete || catalog != nil || p.Error == "" {
		t.Fatal("combined catalog reset its compatibility budget per test")
	}
}

func TestCanonicalProjectionExactStructuralAndByteBoundaries(t *testing.T) {
	r := Compile(`error Bad
effect fn task(name: string) -> string raises {Bad} { name }
effect fn main() -> string { run task("ok").catch<Bad>("caught") }`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	symbol := r.Find("task")
	baseline := r.ProjectSymbol(symbol)
	if !baseline.Complete {
		t.Fatal(baseline.Error)
	}
	for _, field := range []string{"Nodes", "Edges", "RowLabels", "NameBytes", "CompatibilityBytes", "ResponseBytes"} {
		t.Run(field, func(t *testing.T) {
			r.TypeProjectionLimits = defaultProjectionLimits
			limit := reflect.ValueOf(&r.TypeProjectionLimits).Elem().FieldByName(field)
			used := reflect.ValueOf(baseline.Usage).FieldByName(field).Int()
			if used == 0 {
				t.Fatalf("fixture does not exercise %s", field)
			}
			limit.SetInt(used)
			at := r.ProjectSymbol(symbol)
			if !at.Complete {
				t.Fatalf("exact %s boundary refused: %s", field, at.Error)
			}
			limit.SetInt(used - 1)
			over := r.ProjectSymbol(symbol)
			if over.Complete || over.Error == "" || len(over.Types) != 0 || len(over.Rows) != 0 {
				t.Fatalf("%s overflow returned authority: %+v", field, over)
			}
		})
	}
	r.TypeProjectionLimits = defaultProjectionLimits
	info, err := r.TypeAt(strings.Index(`error Bad
effect fn task(name: string) -> string raises {Bad} { name }
effect fn main() -> string { run task("ok").catch<Bad>("caught") }`, "run task"))
	if err != nil {
		t.Fatal(err)
	}
	value := info.Type
	value.Contract.Scope = "other-revision"
	if p := r.ProjectValue(value); p.Complete || !strings.Contains(p.Error, "revision") {
		t.Fatalf("wrong snapshot accepted: %+v", p)
	}
	value = info.Type
	value.Contract.ID = "t:undefined"
	if p := r.ProjectValue(value); p.Complete || p.Error == "" {
		t.Fatalf("missing reference accepted: %+v", p)
	}
	value = info.Type
	value.FailureRow = "row:undefined"
	if p := r.ProjectValue(value); p.Complete || p.Error == "" {
		t.Fatalf("missing compatibility row reference accepted: %+v", p)
	}
}

func TestSelectedProjectionDoesNotMaterializeUnrelatedWideCompatibility(t *testing.T) {
	var source strings.Builder
	source.WriteString("effect fn wide(")
	for i := 0; i < 2048; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		fmt.Fprintf(&source, "p%d: string", i)
	}
	source.WriteString(") -> string { \"wide\" }\neffect fn main() -> string { \"ok\" }")
	r := Compile(source.String())
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	small := r.ProjectSymbol(r.Find("main"))
	if !small.Complete {
		t.Fatal(small.Error)
	}
	for _, node := range small.Types {
		if len(node.Args) > 0 {
			t.Fatalf("small selection included unrelated wide signature: %+v", node)
		}
	}
	wide := r.Find("wide")
	if r.ProjectSymbol(wide).Complete {
		t.Fatal("wide compatibility should refuse")
	}
	if wide.Contract.Callable != nil {
		t.Fatal("wide rejected callable parameters were allocated before admission")
	}
	info, err := r.TypeAt(strings.LastIndex(source.String(), `"ok"`))
	if err != nil {
		t.Fatal(err)
	}
	if !r.ProjectExpression(info).Complete {
		t.Fatal("small query refused")
	}
}

func TestAggregateCompatibilityRefusesBeforeMaterializingLaterSymbols(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&source, "effect fn f%d(", i)
		for p := 0; p < 96; p++ {
			if p > 0 {
				source.WriteByte(',')
			}
			fmt.Fprintf(&source, "p%d: string", p)
		}
		source.WriteString(") -> string { \"ok\" }\n")
	}
	source.WriteString("effect fn main() -> string { \"ok\" }")
	r := Compile(source.String())
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if !r.publicationRefused || r.ProjectAllTypes().Complete {
		t.Fatal("aggregate compatibility was admitted")
	}
	if usage := r.CheckResponse()["typeProjectionUsage"].(ProjectionUsage); usage.CompatibilityBytes <= r.projectionLimits().CompatibilityBytes {
		t.Fatal("refusal envelope erased the failed admission usage")
	}
	late := r.Symbols[39]
	if late.Contract.ProjectionError == "" || late.Contract.Callable != nil {
		t.Fatal("later compatibility materialized before aggregate admission")
	}
	selected := r.Find("f39")
	if selected.Contract.Callable == nil || !r.ProjectSymbol(selected).Complete {
		t.Fatal("individually small selected function refused")
	}
	if r.Symbols[39].Contract.Callable != nil || r.ProjectAllTypes().Complete {
		t.Fatal("selected projection mutated whole-source publication")
	}
	if _, err := r.EmitGo(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Emit(true); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedCanonicalProjectionContainsOnlyReachableDefinitions(t *testing.T) {
	source := `record Wanted { child: string }
record Unrelated { child: i64 }
effect fn selected() -> Wanted { Wanted { child: "ok" } }
effect fn main() -> string { let value = run selected() value.child }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("projection fixture did not check: %+v", r.Diagnostics)
	}
	projection := r.ProjectSymbol(r.Find("selected"))
	if !projection.Complete {
		t.Fatalf("selected projection was refused: %+v", projection)
	}
	ids := map[string]bool{}
	byID := map[string]TypeNode{}
	for _, node := range projection.Types {
		if ids[node.ID] {
			t.Fatalf("selected projection duplicated %q", node.ID)
		}
		ids[node.ID] = true
		byID[node.ID] = node
		if node.Name == "Unrelated" {
			t.Fatalf("selected projection included unrelated declaration: %+v", projection.Types)
		}
	}
	for _, node := range projection.Types {
		for _, child := range node.Args {
			if !ids[child] {
				t.Fatalf("selected projection has dangling child %q from %q", child, node.ID)
			}
		}
		if node.Result != "" && !ids[node.Result] {
			t.Fatalf("selected projection has dangling result %q from %q", node.Result, node.ID)
		}
	}
	if len(byID) == 0 || projection.Usage.Nodes != len(projection.Types) {
		t.Fatalf("selected projection usage does not describe its complete table: %+v", projection)
	}
	if len(projection.Rows) != 0 {
		for _, row := range projection.Rows {
			if row.ID == "" {
				t.Fatal("empty rows must be omitted")
			}
		}
	}
}

func TestCanonicalProjectionRetainsSharedDAGAndRefusesBrokenArenaReferences(t *testing.T) {
	c := newCheckedValueTestChecker()
	r := &Result{Revision: "snapshot", Checked: true, TypeProjectionLimits: defaultProjectionLimits}
	c.result = r
	root := c.internType("primitive", "string", nil)
	for i := 0; i < 256; i++ {
		root = c.internType("product", "", []TypeID{root, root})
	}
	r.canonical = c.canonicalSnapshot()
	value := ValueType{Contract: c.identityRef(root)}
	p := r.ProjectValue(value)
	if !p.Complete || len(p.Types) != 257 || p.Usage.Edges != 512 {
		t.Fatalf("DAG was expanded or refused: %+v", p)
	}
	ids := map[string]bool{}
	for _, node := range p.Types {
		if ids[node.ID] {
			t.Fatalf("duplicate node %s", node.ID)
		}
		ids[node.ID] = true
	}
	for _, node := range p.Types {
		if len(node.Args) == 0 {
			continue
		}
		if len(node.Args) != 2 || node.Args[0] != node.Args[1] || !ids[node.Args[0]] {
			t.Fatalf("DAG edge identity lost: %+v", node)
		}
	}
	c.node(root).Args[0] = invalidTypeID
	broken := r.ProjectValue(value)
	if broken.Complete || broken.Error == "" || len(broken.Types) != 0 {
		t.Fatalf("broken private arena published authority: %+v", broken)
	}
}

func TestFailurePayloadProjectionIncludesItsNominalClosure(t *testing.T) {
	r := Compile(`record Detail { message: string }
error Bad { detail: Detail }
effect fn task() -> string raises {Bad} { fail Bad { detail: Detail { message: "bad" } } }
effect fn main() -> string { run task().catch<Bad>("caught") }`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	p := r.ProjectSymbol(r.Find("task"))
	if !p.Complete {
		t.Fatal(p.Error)
	}
	d := r.ProjectionDeclarations(p)
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, node := range p.Types {
		ids[node.ID] = true
	}
	for _, declaration := range d {
		names[declaration.Name] = true
		for _, field := range declaration.Fields {
			if !ids[field.TypeRef.ID] {
				t.Fatalf("failure payload field has no definition: %+v", field)
			}
		}
	}
	if len(names) != 2 || !names["Bad"] || !names["Detail"] {
		t.Fatalf("failure payload closure incomplete: %v", names)
	}
}

func TestWholeSourceProjectionRefusalDoesNotInvalidateSemanticCheck(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 2100; i++ {
		source.WriteString("record R" + fmtInt(i) + " { value: string }\n")
	}
	for i := 0; i < 2100; i++ {
		source.WriteString("effect fn f" + fmtInt(i) + "() -> R" + fmtInt(i) + " { R" + fmtInt(i) + " { value: \"ok\" } }\n")
	}
	source.WriteString("effect fn main() -> string { let value = run f0() value.value }\n")
	r := Compile(source.String())
	if !r.Checked {
		t.Fatalf("large valid source was rejected by semantic checking: %+v", r.Diagnostics)
	}
	if r.TypeProjectionComplete || r.TypeProjectionError == "" {
		t.Fatalf("whole-source projection should refuse explicitly while remaining checked: complete=%t error=%q", r.TypeProjectionComplete, r.TypeProjectionError)
	}
	mainProjection := r.ProjectSymbol(r.Find("main"))
	if !mainProjection.Complete {
		t.Fatalf("small selected root was refused after whole-source overflow: %+v", mainProjection)
	}
	if _, err := r.EmitGo(); err != nil {
		t.Fatalf("Go emission changed when whole-source projection refused: %v", err)
	}
	if _, _, err := r.Emit(true); err != nil {
		t.Fatalf("JS emission changed when whole-source projection refused: %v", err)
	}
}
