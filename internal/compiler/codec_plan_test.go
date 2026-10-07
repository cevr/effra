package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	rt "effra.local/prototype/runtime/effra"
)

var testCodecBounds = CodecPlanBounds{MaxBodyBytes: 1 << 20, MaxDepth: rt.CodecMaxDepth}

// derivePlanOf derives the plan of a type spelled in checked source, as a
// derive declaration does after its type is resolved.
func derivePlanOf(t *testing.T, source, typeName string, bounds CodecPlanBounds) (*CodecPlan, string) {
	t.Helper()
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	c := r.Program.semantic
	return c.deriveCodecPlan(rt.CodecProfileJSON, c.canonicalRef(c.typeRef(typeName)), typeName, bounds)
}

const codecPlanSource = `record User {
    id: i64
    name: string
    admin: bool
}

record Note {
    text: string
    marker: void
}

enum Event {
    Created { user: User }
    Renamed { user: User, note: Note }
    Closed
}
`

func TestCodecPlanIsAStableSharedDAG(t *testing.T) {
	event, refusal := derivePlanOf(t, codecPlanSource, "Event", testCodecBounds)
	if refusal != "" {
		t.Fatal(refusal)
	}
	ids := []string{}
	for _, node := range event.Nodes {
		ids = append(ids, node.ID)
	}
	want := []string{"enum:module:file:module:Event", "record:module:file:module:User", "primitive:i64", "primitive:string", "primitive:bool", "record:module:file:module:Note", "primitive:void"}
	if !slices.Equal(ids, want) || event.Root != 0 || event.Nesting != 2 || event.Bounds != testCodecBounds {
		t.Fatalf("plan order or bounds: %v %+v", ids, event)
	}
	if event.ID != "codec-plan:effra/json-structural-1:enum:module:file:module:Event:1048576:512" {
		t.Fatalf("plan identity: %s", event.ID)
	}
	variants := event.Nodes[0].Variants
	if len(variants) != 3 || variants[0].Tag != "Created" || variants[0].Fields[0].Node != 1 || variants[1].Fields[0].Node != 1 || len(variants[2].Fields) != 0 {
		t.Fatalf("variants must share the User node: %+v", variants)
	}
	if note := event.Nodes[5]; note.Fields[0].Node != 3 || note.Fields[1].Node != 6 {
		t.Fatalf("Note must share the string primitive with User: %+v", note)
	}
	encode := func(plan *CodecPlan) string {
		encoded, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	again, _ := derivePlanOf(t, codecPlanSource, "Event", testCodecBounds)
	if encode(again) != encode(event) {
		t.Fatalf("derived plans are not deterministic:\n%s\n%s", encode(again), encode(event))
	}
	// Moving an unrelated declaration changes no plan: order follows the
	// derived type's own declared fields.
	note := "record Note {\n    text: string\n    marker: void\n}\n"
	moved, _ := derivePlanOf(t, strings.Replace(codecPlanSource, note, "", 1)+note, "Event", testCodecBounds)
	if encode(moved) != encode(event) {
		t.Fatalf("plan depends on declaration placement:\n%s\n%s", encode(moved), encode(event))
	}
}

// A chain whose every record holds the next one twice has 2^depth paths.
// The plan visits each type once, so it stays linear and fast.
func TestCodecPlanDerivationIsLinearInSharedTypes(t *testing.T) {
	const depth = 40
	var source strings.Builder
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&source, "record R%d { left: R%d, right: R%d }\n", i, i+1, i+1)
	}
	fmt.Fprintf(&source, "record R%d { leaf: string }\n", depth)
	start := time.Now()
	plan, refusal := derivePlanOf(t, source.String(), "R0", testCodecBounds)
	if refusal != "" {
		t.Fatal(refusal)
	}
	if len(plan.Nodes) != depth+2 || plan.Nesting != depth+1 {
		t.Fatalf("shared chain plan has %d nodes and nesting %d", len(plan.Nodes), plan.Nesting)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("derivation enumerated paths: %s", elapsed)
	}
}

// Recursive layouts (EF119) and a variant field named _tag (EF120) are
// refused by the checker before any plan is derived; the derivation keeps
// its own guards for both.
func TestCodecPlanRefusesUnrepresentableTypes(t *testing.T) {
	cases := []struct {
		name, source, root, refusal string
		bounds                      CodecPlanBounds
	}{
		{"callable field", "record Job { run: effect fn() -> void }", "Job", "effect fn() -> void at Job.run is a function or effect recipe and cannot cross a codec boundary", testCodecBounds},
		{"pure callable field", "record Rule { check: fn(string) -> bool }", "Rule", "fn(string) -> bool at Rule.check is a function or effect recipe", testCodecBounds},
		{"host field", "record Handle { file: File }", "Handle", "File at Handle.file is a host or runtime type and cannot cross a codec boundary", testCodecBounds},
		{"nested host field", "record Inner { latch: Latch }\nrecord Outer { inner: Inner }", "Outer", "Latch at Outer.inner.latch is a host or runtime type", testCodecBounds},
		{"variant host field", "record Inner { latch: Latch }\nenum Mode { Held { inner: Inner } }", "Mode", "Latch at Mode.Held.inner.latch", testCodecBounds},
		{"bytes", "record Blob { data: bytes }", "Blob", "bytes at Blob.data is not representable in this profile", testCodecBounds},
		{"generic application", "import Data \"effra/data\"\nrecord Maybe { value: Data.Option<string> }", "Maybe", "Data.Option<string> at Maybe.value is generic data", testCodecBounds},
		{"empty enum", "enum Never { }\nrecord Holder { never: Never }", "Holder", "Never at Holder.never has no variants to decode", testCodecBounds},
		{"nesting beyond maxDepth", "record A { b: B }\nrecord B { c: C }\nrecord C { text: string }", "A", "plan nesting 3 exceeds maxDepth 2", CodecPlanBounds{MaxBodyBytes: 64, MaxDepth: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, refusal := derivePlanOf(t, tc.source, tc.root, tc.bounds)
			if plan != nil || !strings.Contains(refusal, tc.refusal) {
				t.Fatalf("want refusal %q, got %q and %+v", tc.refusal, refusal, plan)
			}
		})
	}
	// The boundary itself is admitted: nesting equal to maxDepth.
	if _, refusal := derivePlanOf(t, "record A { b: B }\nrecord B { text: string }", "A", CodecPlanBounds{MaxBodyBytes: 64, MaxDepth: 2}); refusal != "" {
		t.Fatalf("nesting equal to maxDepth refused: %s", refusal)
	}
}

func TestCodecPlanRefusesStructuralBudgets(t *testing.T) {
	var nodes strings.Builder
	nodes.WriteString("record N0 { leaf: string }\n")
	for i := 1; i < maxCodecPlanNodes; i++ {
		fmt.Fprintf(&nodes, "record N%d { next: N%d }\n", i, i-1)
	}
	// N4094 has exactly 4096 nodes: 4095 records and the string primitive.
	// Its nesting stays within no bound but the node ceiling.
	unbounded := CodecPlanBounds{MaxBodyBytes: 1, MaxDepth: maxCodecPlanNodes}
	if plan, refusal := derivePlanOf(t, nodes.String(), fmt.Sprintf("N%d", maxCodecPlanNodes-2), unbounded); refusal != "" || len(plan.Nodes) != maxCodecPlanNodes {
		t.Fatalf("node ceiling must be admitted: %q", refusal)
	}
	// One more record is refused at the leaf, and the refusal renders a
	// bounded path that keeps its root and innermost fields.
	_, refusal := derivePlanOf(t, nodes.String(), fmt.Sprintf("N%d", maxCodecPlanNodes-1), unbounded)
	if !strings.HasPrefix(refusal, "plan exceeds 4096 nodes at N4095.(4081 fields).next.") || !strings.HasSuffix(refusal, ".next.leaf") || len(refusal) > 256 {
		t.Fatalf("node budget: %q", refusal)
	}
	var edges strings.Builder
	const records, fields = 256, 256
	for i := 0; i < records; i++ {
		fmt.Fprintf(&edges, "record E%d {", i)
		for f := 0; f < fields; f++ {
			fmt.Fprintf(&edges, " f%d: string,", f)
		}
		edges.WriteString(" }\n")
	}
	edges.WriteString("record All {")
	for i := 0; i < records; i++ {
		fmt.Fprintf(&edges, " e%d: E%d,", i, i)
	}
	edges.WriteString(" }\n")
	if _, refusal := derivePlanOf(t, edges.String(), "All", testCodecBounds); !strings.Contains(refusal, "plan exceeds 65536 fields and variants") {
		t.Fatalf("edge budget: %q", refusal)
	}
	// Variants are edges under the same ceiling as fields, so a plan the
	// compiler admits is one the runtime engine admits, and one variant more
	// is refused at that variant. The mixed record holds the enum last, so
	// its variants are the edges that cross the ceiling.
	wide := func(variants int) string {
		var source strings.Builder
		source.WriteString("enum Wide {")
		for v := 0; v < variants; v++ {
			fmt.Fprintf(&source, " V%d,", v)
		}
		source.WriteString(" }\n")
		return source.String()
	}
	mixed := func(variants int) string {
		var source strings.Builder
		source.WriteString(wide(variants) + "record Mixed {")
		for f := 0; f < maxCodecPlanEdges-1-3; f++ {
			fmt.Fprintf(&source, " f%d: string,", f)
		}
		source.WriteString(" wide: Wide }\n")
		return source.String()
	}
	boundaries := []struct {
		name, root      string
		source          func(int) string
		admitted        int
		refusalLocation string
	}{
		{"variants alone", "Wide", wide, maxCodecPlanEdges, "Wide.V65536"},
		{"record fields and variants", "Mixed", mixed, 3, "Mixed.wide.V3"},
	}
	for _, tc := range boundaries {
		t.Run(tc.name, func(t *testing.T) {
			plan, refusal := derivePlanOf(t, tc.source(tc.admitted), tc.root, testCodecBounds)
			if refusal != "" {
				t.Fatalf("exactly %d edges must be admitted: %q", maxCodecPlanEdges, refusal)
			}
			runtimePlan := runtimeCodecPlanOf(plan)
			if _, err := rt.CompileCodec(runtimePlan); err != nil {
				t.Fatalf("runtime refused a plan at the edge ceiling: %v", err)
			}
			if _, refusal := derivePlanOf(t, tc.source(tc.admitted+1), tc.root, testCodecBounds); refusal != "plan exceeds 65536 fields and variants at "+tc.refusalLocation {
				t.Fatalf("one variant past the ceiling: %q", refusal)
			}
			// The runtime engine refuses the same plan with one more variant,
			// so both ceilings are the same edge.
			union := &runtimePlan.Nodes[slices.IndexFunc(runtimePlan.Nodes, func(node rt.CodecNode) bool { return node.Kind == rt.CodecUnion })]
			union.Variants = append(union.Variants, rt.CodecVariant{Tag: "Extra"})
			if _, err := rt.CompileCodec(runtimePlan); err == nil || !strings.Contains(err.Error(), "more than 65536 fields and variants") {
				t.Fatalf("runtime admitted a plan past the edge ceiling: %v", err)
			}
		})
	}
	// A derive declaration reports the refusal as EF138 at its type.
	source := "import Json \"effra/json\"\n" + wide(maxCodecPlanEdges+1) + "derive wideJson = Json.codec<Wide>\n"
	r := Compile(source)
	at := strings.LastIndex(source, "Wide>")
	if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != codecDerivationCode || r.Diagnostics[0].Span.Offset != at || r.Diagnostics[0].Span.Length != len("Wide") ||
		r.Diagnostics[0].Message != "codec wideJson cannot derive effra/json-structural-1 for Wide: plan exceeds 65536 fields and variants at Wide.V65536" {
		t.Fatalf("derive past the edge ceiling: %+v", r.Diagnostics)
	}
}

// A derived plan is admitted by the runtime engine's own validator, which
// checks sharing, reachability, acyclicity and nesting independently.
func TestDerivedCodecPlansAreAdmittedByTheRuntimeEngine(t *testing.T) {
	plan, refusal := derivePlanOf(t, codecPlanSource, "Event", CodecPlanBounds{MaxBodyBytes: 4096, MaxDepth: 2})
	if refusal != "" {
		t.Fatal(refusal)
	}
	if _, err := rt.CompileCodec(runtimeCodecPlanOf(plan)); err != nil {
		t.Fatalf("runtime refused a derived plan: %v", err)
	}
}

// runtimeCodecPlanOf converts a derived plan into the runtime engine's plan
// with inert adapters, so the engine's validator checks the derived structure.
func runtimeCodecPlanOf(plan *CodecPlan) rt.CodecPlan {
	runtimePlan := rt.CodecPlan{Profile: plan.Profile, Bounds: rt.CodecBounds{MaxBodyBytes: plan.Bounds.MaxBodyBytes, MaxDepth: plan.Bounds.MaxDepth}, Root: plan.Root}
	fields := func(declared []CodecPlanField) []rt.CodecField {
		out := []rt.CodecField{}
		for _, field := range declared {
			out = append(out, rt.CodecField{Name: field.Name, Node: field.Node})
		}
		return out
	}
	for _, node := range plan.Nodes {
		converted := rt.CodecNode{Kind: node.Kind}
		if node.data != nil {
			converted.Type = node.ID
			converted.Construct = func(int, []any) any { return nil }
			converted.Project = func(any) (int, []any) { return 0, nil }
			converted.Fields = fields(node.Fields)
			for _, variant := range node.Variants {
				converted.Variants = append(converted.Variants, rt.CodecVariant{Tag: variant.Tag, Fields: fields(variant.Fields)})
			}
			if node.Kind == rt.CodecUnion {
				converted.Fields = nil
			}
		}
		runtimePlan.Nodes = append(runtimePlan.Nodes, converted)
	}
	return runtimePlan
}
