package compiler

import (
	"strings"
	"testing"
)

func TestSelectedTypeUsesOriginalNamesAndActualLexicalResolution(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(lexicalFixture, target)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		selectAt := func(needle, name string) SelectedType {
			t.Helper()
			offset := strings.Index(lexicalFixture, needle) + strings.Index(needle, name)
			response, err := r.SelectType(TypeSelection{Offset: &offset})
			if err != nil {
				t.Fatal(err)
			}
			if response["querySchemaVersion"] != 1 || response["revision"] != r.Revision || response["target"] != target || response["typeProjectionComplete"] != true {
				t.Fatalf("envelope: %+v", response)
			}
			selection := response["selection"].(SelectedType)
			if !selection.LocationAvailable {
				t.Fatal("original source name lost its location")
			}
			if selection.Expression != nil && selection.Expression.Type.Success != "string" {
				t.Fatalf("wrong checked type: %+v", selection)
			}
			return selection
		}
		declaration := selectAt("let label =", "label")
		use := selectAt("\n label\n", "label")
		if declaration.Kind != "bindingDeclaration" || use.Kind != "bindingUse" || declaration.Binding.ID != use.Binding.ID {
			t.Fatal("let declaration/use identity mismatch")
		}
		pattern := selectAt("value: label", "label")
		patternUse := selectAt("=> label +", "label")
		outer := selectAt("rendered + label", "label")
		if pattern.Binding.ID != patternUse.Binding.ID || pattern.Binding.ID == outer.Binding.ID || outer.Binding.Kind != "parameter" {
			t.Fatal("shadowed resolution invented a declaration")
		}
		config := selectAt("Prefix(prefix:", "prefix")
		configUse := selectAt("= prefix +", "prefix")
		if config.Binding.ID != configUse.Binding.ID || config.Binding.Kind != "configuration" {
			t.Fatal("provider capture declaration mismatch")
		}
		function := selectAt("fn local(", "local")
		if function.Kind != "declaration" || function.Symbol.Name != "local" {
			t.Fatal("function name treated as an expression")
		}
		// Existing diagnostic-anchor query remains unchanged and refuses names.
		offset := strings.Index(lexicalFixture, "let label =") + len("let ")
		if _, err := r.TypeAt(offset); err == nil {
			t.Fatal("compatibility query unexpectedly broadened")
		}
	}
}

func TestSelectedCanonicalDefinitionRequiresItsSnapshotAndPreservesBounds(t *testing.T) {
	r := Compile(`record Leaf { text: string } record Pair { first: Leaf, second: Leaf } fn pair() -> Pair { Pair(Leaf("a"), Leaf("b")) }`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	offset := strings.Index(`record Leaf { text: string } record Pair { first: Leaf, second: Leaf } fn pair() -> Pair { Pair(Leaf("a"), Leaf("b")) }`, `Pair(Leaf`)
	response, err := r.SelectType(TypeSelection{Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	id := response["selection"].(SelectedType).Expression.Type.Type.ID
	if id == "" {
		t.Fatal("selected type lost canonical identity")
	}
	definition, err := r.SelectType(TypeSelection{Definition: id, ExpectedRevision: r.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if definition["selection"].(SelectedType).LocationAvailable {
		t.Fatal("definition lookup fabricated source syntax")
	}
	seen := map[string]bool{}
	for _, node := range definition["types"].([]TypeNode) {
		if seen[node.ID] {
			t.Fatal("shared type expanded twice")
		}
		seen[node.ID] = true
	}
	if !seen[id] {
		t.Fatal("definition root absent")
	}
	for _, request := range []TypeSelection{{Definition: id}, {Definition: id, ExpectedRevision: "stale"}, {Definition: "type:missing", ExpectedRevision: r.Revision}, {}, {Symbol: "pair", Offset: &offset}} {
		if _, err := r.SelectType(request); err == nil {
			t.Fatalf("invalid selector admitted: %+v", request)
		}
	}
	limits := r.TypeProjectionLimits
	usage := response["typeProjectionUsage"].(ProjectionUsage)
	// Limits themselves are encoded in the envelope, so changing a decimal
	// limit can shorten it; use a strict interior bound, not a one-byte oracle.
	r.TypeProjectionLimits.ResponseBytes = usage.ResponseBytes / 2
	if _, err := r.SelectType(TypeSelection{Offset: &offset}); err == nil {
		t.Fatal("selected envelope response budget bypassed")
	}
	r.TypeProjectionLimits = limits
	r.TypeProjectionLimits.Nodes = 1
	if _, err := r.SelectType(TypeSelection{Definition: id, ExpectedRevision: r.Revision}); err == nil {
		t.Fatal("selected definition node budget bypassed")
	}
	r.TypeProjectionLimits = limits
	if _, err := r.SelectType(TypeSelection{Offset: &offset}); err != nil || !r.Checked {
		t.Fatalf("refusal corrupted checking: %v", err)
	}
}

func TestSelectedTypeRefusesUnavailableFactsWithoutGuessing(t *testing.T) {
	r := Compile(lexicalFixture)
	end := len(lexicalFixture)
	if _, err := r.SelectType(TypeSelection{Offset: &end}); err == nil {
		t.Fatal("EOF fabricated a type")
	}
	if _, err := r.SelectType(TypeSelection{Symbol: "notDeclared"}); err == nil {
		t.Fatal("unknown declaration fabricated")
	}
	r.lexical.complete = false
	offset := strings.Index(lexicalFixture, "let label") + len("let ")
	if _, err := r.SelectType(TypeSelection{Offset: &offset}); err == nil {
		t.Fatal("partial syntax facts claimed complete")
	}
	invalid := Compile(`fn invalid() -> string { missing }`)
	if _, err := invalid.SelectType(TypeSelection{Symbol: "invalid"}); err == nil {
		t.Fatal("unchecked source claimed authoritative type")
	}
}

func TestSelectedBindingMetadataIsChargedBeyondPlainExpression(t *testing.T) {
	name := "label" + strings.Repeat("x", 500)
	source := "fn local(input: string) -> string { let " + name + " = input; " + name + " }"
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	offset := strings.LastIndex(source, name)
	info, err := r.TypeAt(offset)
	if err != nil {
		t.Fatal(err)
	}
	projection := r.ProjectExpression(info)
	plain := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": true, "expression": info, "types": projection.Types, "rows": projection.Rows}
	r.AddSourceInputs(plain)
	usage, err := r.ValidateProjectionResponse(projection, plain)
	if err != nil {
		t.Fatal(err)
	}
	r.TypeProjectionLimits.NameBytes = usage.NameBytes + len(name)/2
	projection = r.ProjectExpression(info)
	if !projection.Complete {
		t.Fatal("positive plain expression control refused", projection.Error)
	}
	if _, err := r.ValidateProjectionResponse(projection, plain); err != nil {
		t.Fatal("positive plain envelope control refused", err)
	}
	if _, err := r.SelectType(TypeSelection{Offset: &offset}); err == nil {
		t.Fatal("binding name metadata escaped owning response budget")
	}
	if !r.Checked {
		t.Fatal("tooling refusal changed admission")
	}
}

func TestSelectedImportedDeclarationDoesNotBorrowRootSourceLocation(t *testing.T) {
	// The imported identity function and this unrelated root identity function
	// have exactly the same diagnostic anchor. Ownership, not coordinates,
	// decides whether an original node belongs to this source snapshot.
	r := Compile("fn identity(input: string) -> string { input }\nimport Fns \"effra/functions\"\nfn call(input: string) -> string { Fns.identity(input) }")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	local, imported := r.Find("identity"), r.Find("Fns.identity")
	if local == nil || imported == nil || local.Span != imported.Span {
		t.Fatal("fixture did not establish colliding spans")
	}
	response, err := r.SelectType(TypeSelection{Symbol: "Fns.identity"})
	if err != nil {
		t.Fatal(err)
	}
	selection := response["selection"].(SelectedType)
	if selection.LocationAvailable || selection.Extent.Length != 0 {
		t.Fatalf("import borrowed unrelated root extent: %+v", selection)
	}
	localView, err := r.SelectType(TypeSelection{Symbol: "identity"})
	if err != nil || !localView["selection"].(SelectedType).LocationAvailable {
		t.Fatal("local positive location control unavailable", err)
	}
}
