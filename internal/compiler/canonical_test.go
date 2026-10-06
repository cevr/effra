package compiler

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestCanonicalTypesUseSharedBoundedNodes(t *testing.T) {
	source := `record R { a: string }
effect fn get() -> R { R { a: "ok" } }
effect fn main() -> string { let child = fork get() let r = run child.join() r.a }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("canonical fixture did not check: %+v", r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "fork"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Type.Type.Kind != "fiber" || len(info.Type.Type.Args) != 1 || info.Type.Type.Args[0].Kind != "record" || info.Type.Type.Args[0].Name != "R" {
		t.Fatalf("fiber type lost canonical child: %+v", info.Type.Type)
	}
	seen := map[string]bool{}
	for _, node := range r.Types {
		if seen[node.ID] {
			t.Fatalf("duplicate canonical node %q", node.ID)
		}
		seen[node.ID] = true
	}
	for _, node := range r.Types {
		for _, arg := range node.Args {
			if !seen[arg] {
				t.Fatalf("node %s points at unpublished argument %s", node.ID, arg)
			}
		}
	}
	if len(r.Types) == 0 {
		t.Fatal("canonical type table is empty")
	}
	encoded, err := json.Marshal(info.Type.Type)
	if err != nil {
		t.Fatal(err)
	}
	jsonType := string(encoded)
	if !strings.Contains(jsonType, `"ref":"`) || !strings.Contains(jsonType, `"args":[`) || strings.Contains(jsonType, `"args":[{"`) {
		t.Fatalf("type projection is not a bounded reference list: %s", jsonType)
	}
}

func TestCanonicalRowsAndFinalExpressionFacts(t *testing.T) {
	source := `effect fn task() -> string { "ok" }
effect fn main() -> string { let pending = Console.log("deferred") run task() }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("row fixture did not check: %+v", r.Diagnostics)
	}
	if len(r.Rows) == 0 {
		t.Fatal("expected a shared row node for the deferred service contract")
	}
	rowLabels := []string{}
	for _, row := range r.Rows {
		if row.ID != rowNodeIDForLabels(row.Labels) {
			t.Fatalf("row identity does not match its canonical labels: %+v", row)
		}
		rowLabels = append(rowLabels, row.Labels...)
	}
	if !slices.Contains(rowLabels, "Console") {
		t.Fatalf("shared rows lost Console requirement: %+v", r.Rows)
	}
	offset := strings.Index(source, `"deferred"`)
	info, err := r.TypeAt(offset)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type.Success != "string" || info.Type.Type.ID == "" {
		t.Fatalf("final expression fact lost canonical type: %+v", info.Type)
	}
	facts := r.facts[findExprAt(r.Program, offset)]
	if !slices.Equal(info.Evaluation.Failures, facts.Evaluation.Failures) || !slices.Equal(info.ExecutedRequirements, facts.Executed.Requirements) {
		t.Fatalf("TypeAt did not use final expression facts: info=%+v facts=%+v", info, facts)
	}
}

func TestCanonicalPublicIDsAreDeterministic(t *testing.T) {
	source := `record R { value: string }
effect fn get() -> R { R { value: "ok" } }
effect fn main() -> string { let pending = Console.log("deferred") let child = fork get() let r = run child.join() r.value }`
	first, second := Compile(source), Compile(source)
	if !first.Checked || !second.Checked {
		t.Fatalf("determinism fixture did not check: first=%+v second=%+v", first.Diagnostics, second.Diagnostics)
	}
	firstJSON, err := json.Marshal(struct {
		Types []TypeNode `json:"types"`
		Rows  []RowNode  `json:"rows"`
	}{first.Types, first.Rows})
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(struct {
		Types []TypeNode `json:"types"`
		Rows  []RowNode  `json:"rows"`
	}{second.Types, second.Rows})
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("canonical public IDs changed across fresh compiles:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
}

func TestCanonicalRepeatedChildProjectionStaysBounded(t *testing.T) {
	c := &checker{
		typeIntern:     map[string]*semanticTypeNode{},
		typePublicIDs:  map[TypeID]string{},
		typePublicToID: map[string]TypeID{},
		nextTypeID:     1,
		rowIntern:      map[string]RowID{},
		nextRowID:      1,
	}
	leaf := c.internType("record", "Leaf", nil)
	root := leaf
	for i := 0; i < 256; i++ {
		root = c.internType("product", "", []TypeID{root, root})
	}
	ref := c.ref(root)
	if len(ref.Args) != 2 || len(ref.ArgIDs) != 2 {
		t.Fatalf("repeated child refs were not retained at the projection boundary: %+v", ref)
	}
	if len(ref.Args[0].Args) != 0 || ref.Args[0].ID != ref.Args[1].ID {
		t.Fatalf("repeated children expanded instead of sharing IDs: %+v", ref.Args)
	}
	if len(c.typePublicIDs) < 256 {
		t.Fatalf("expected stable IDs for the repeated-child graph, got %d", len(c.typePublicIDs))
	}
	encoded, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 1024 {
		t.Fatalf("shared repeated-child projection grew unexpectedly: %d bytes", len(encoded))
	}
}

func TestCanonicalProjectedRefsRoundTripThroughSameArena(t *testing.T) {
	c := &checker{
		typeIntern:     map[string]*semanticTypeNode{},
		typePublicIDs:  map[TypeID]string{},
		typePublicToID: map[string]TypeID{},
		nextTypeID:     1,
		rowIntern:      map[string]RowID{},
		nextRowID:      1,
	}
	leaf := c.internType("record", "Leaf", nil)
	nested := c.internType("fiber", "", []TypeID{c.internType("fiber", "", []TypeID{leaf})})
	projected := c.ref(nested)
	if got := c.canonicalRef(projected); got != nested {
		t.Fatalf("projected ref rebuilt a different node: got=%d want=%d ref=%+v", got, nested, projected)
	}
	refOnly := TypeRef{ID: c.typeNodeID(nested), Kind: "record", Name: "forged", ArgIDs: []string{"t:forged"}}
	if got := c.canonicalRef(refOnly); got != nested {
		t.Fatalf("canonical ref did not take precedence over projection fields: got=%d want=%d", got, nested)
	}
	if got := c.canonicalRef(TypeRef{ID: "t:unknown", Kind: "record", Name: "Leaf"}); got != invalidTypeID {
		t.Fatalf("unknown cross-arena ref was admitted as a reconstructed node: %d", got)
	}
}

func TestCallableDeclarationAndSignatureIdentityAreDistinct(t *testing.T) {
	source := `effect fn first(value: string) -> string { value }
effect fn second(value: string) -> string { value }
effect fn main() -> string { run first("ok") }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("callable identity fixture did not check: %+v", r.Diagnostics)
	}
	first, second := r.Find("first"), r.Find("second")
	if first == nil || second == nil || first.Contract.Callable == nil || second.Contract.Callable == nil {
		t.Fatalf("missing callable contracts: first=%+v second=%+v", first, second)
	}
	if first.Contract.Callable.ID == second.Contract.Callable.ID {
		t.Fatalf("declaration identities collapsed: first=%q second=%q", first.Contract.Callable.ID, second.Contract.Callable.ID)
	}
	if first.Contract.Callable.Signature == "" || first.Contract.Callable.Signature != second.Contract.Callable.Signature {
		t.Fatalf("structural callable signatures did not share: first=%q second=%q", first.Contract.Callable.Signature, second.Contract.Callable.Signature)
	}
}

func findExprAt(program *Program, offset int) *Expr {
	var found *Expr
	var block func(*Block)
	var expr func(*Expr)
	expr = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Span.Offset <= offset && offset < e.Span.Offset+e.Span.Length && (found == nil || e.Span.Length < found.Span.Length) {
			found = e
		}
		forEachExprChild(e, expr)
		for _, arm := range e.Arms {
			block(arm.Body)
		}
		block(e.Then)
		block(e.Else)
	}
	block = func(b *Block) {
		if b != nil {
			for _, statement := range b.Statements {
				expr(statement.Value)
				expr(statement.Payload)
			}
		}
	}
	for _, function := range program.Functions {
		block(function.Body)
	}
	return found
}
