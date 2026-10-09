package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParameterDefaultSyntaxFormatsUnderFormatterTen(t *testing.T) {
	source := `fn ordinary(required: string, suffix:string="!") -> string { required }
fn marked(required required:string = "x") -> string { required }`
	first, err := FormatSource(source)
	if err != nil {
		t.Fatalf("format contextual parameter syntax: %v", err)
	}
	if FormatterIdentity != "effra/formatter-12" || !strings.Contains(first.Text, `ordinary(required: string,`) || !strings.Contains(first.Text, `fn marked(required required: string = "x")`) || !strings.Contains(first.Text, `suffix: string = "!"`) {
		t.Fatalf("formatter epoch or parameter formatting is stale: identity=%s text=%s", FormatterIdentity, first.Text)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Text != first.Text {
		t.Fatalf("parameter formatting was not idempotent: %q / %v", second.Text, err)
	}
}

func TestCheckedParameterFactsArePublicAndContextual(t *testing.T) {
	source := `import Defaults "effra/constants"
const LocalSuffix: string = Defaults.defaultSuffix
const Zero: i64 = 0
const Disabled: bool = false
service Settings {
    effect fn read(required key: string, suffix: string = Defaults.defaultSuffix) -> string
}
impl SettingsLive for Settings {
    effect fn read(required key: string, suffix: string) -> string { key + suffix }
}
fn ordinary(required: string = "x", budget: i64 = Zero, bound: string = LocalSuffix, maxActive: i64 = Zero, maxDepth: i64 = Zero, maxBodyBytes: i64 = Zero, enabled: bool = Disabled, empty: string = "") -> string { required }
fn marked(required required: string) -> string { required }`
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("checked parameter facts rejected: %+v", r.Diagnostics)
			}
			if r.SchemaVersion != SemanticSchemaVersion || SemanticSchemaVersion != 9 || TypeQuerySchemaVersion != 4 || interfaceSummarySchema != 5 || SemanticProducerIdentity != "effra/checker-abi-10/bundled-interface-5" {
				t.Fatalf("semantic contract epoch is stale: schema=%d query=%d interface=%d producer=%s", r.SchemaVersion, TypeQuerySchemaVersion, interfaceSummarySchema, SemanticProducerIdentity)
			}
			findSymbol := func(name string) *Symbol {
				t.Helper()
				for i := range r.Symbols {
					if r.Symbols[i].Name == name {
						return &r.Symbols[i]
					}
				}
				return nil
			}
			ordinary := findSymbol("ordinary")
			marked := findSymbol("marked")
			if ordinary == nil || marked == nil || len(ordinary.Params) != 8 || len(marked.Params) != 1 {
				t.Fatalf("public parameter symbols missing: %+v", r.Symbols)
			}
			if ordinary.Params[0].RequiredChoice || ordinary.Params[0].DefaultValue == nil || *ordinary.Params[0].DefaultValue != (ConstantValue{Kind: "string", Value: "x"}) {
				t.Fatalf("ordinary parameter named required became a role: %+v", ordinary.Params[0])
			}
			for i := 1; i < len(ordinary.Params); i++ {
				if ordinary.Params[i].DefaultValue == nil {
					t.Fatalf("same-spelled ordinary parameter was refused: %+v", ordinary.Params[i])
				}
			}
			if ordinary.Params[1].DefaultValue.Value != "0" || ordinary.Params[2].DefaultValue.Value != "!" || ordinary.Params[6].DefaultValue.Kind != "bool" || ordinary.Params[6].DefaultValue.Value != "false" || ordinary.Params[7].DefaultValue == nil || ordinary.Params[7].DefaultValue.Value != "" {
				t.Fatalf("resolved scalar defaults lost zero, alias or false: %+v", ordinary.Params)
			}
			serviceDefault := r.Program.Services[0].Methods[0].Params[1].DefaultValue
			implementationDefault := r.Program.Providers[0].Methods[0].Params[1].DefaultValue
			if serviceDefault == nil || serviceDefault.Value != "!" || implementationDefault != nil {
				t.Fatalf("service declaration is not the sole default owner: service=%+v implementation=%+v", serviceDefault, implementationDefault)
			}
			if !marked.Params[0].RequiredChoice || marked.Params[0].DefaultValue != nil {
				t.Fatalf("required-choice fact was not published: %+v", marked.Params[0])
			}
			if ordinary.Contract.Callable == nil || len(ordinary.Contract.Callable.Parameters) != len(ordinary.Params) || ordinary.Contract.Callable.Parameters[0].DefaultValue == nil {
				t.Fatalf("callable parameter projection is incomplete: %+v", ordinary.Contract.Callable)
			}
			if ordinary.Contract.Callable.Parameters[1].DefaultValue == nil || ordinary.Contract.Callable.Parameters[1].DefaultValue.Value != "0" {
				t.Fatalf("zero default was lost from callable type: %+v", ordinary.Contract.Callable.Parameters)
			}
			response, err := r.QueryType(TypeSelection{Symbol: "ordinary"})
			if err != nil || response.Selection.Symbol == nil || response.Selection.Target == nil || response.Selection.Target.Callable == nil || response.Response["querySchemaVersion"] != 4 {
				t.Fatalf("query omitted checked callable facts: selection=%+v err=%v", response.Selection, err)
			}
			if response.Selection.Presentation != `fn ordinary(required: string = "x", budget: i64 = 0, bound: string = "!", maxActive: i64 = 0, maxDepth: i64 = 0, maxBodyBytes: i64 = 0, enabled: bool = false, empty: string = "") -> string` {
				t.Fatalf("callable query presentation omitted checked parameter facts: %q", response.Selection.Presentation)
			}
			params := response.Selection.Target.Callable.Parameters
			if params[0].DefaultValue == nil || params[0].DefaultValue.Value != "x" || params[0].RequiredChoice {
				t.Fatalf("declaration query lost default facts: %+v", params[0])
			}
			parameterAt := strings.Index(source, "fn ordinary(") + len("fn ordinary(")
			parameterQuery, err := r.QueryType(TypeSelection{Offset: &parameterAt})
			if err != nil || parameterQuery.Selection.Target == nil || parameterQuery.Selection.Target.Parameter == nil || parameterQuery.Selection.Target.Parameter.DefaultValue == nil || parameterQuery.Selection.Target.Parameter.DefaultValue.Value != "x" {
				t.Fatalf("selected parameter query lost its checked contract fact: %+v err=%v", parameterQuery.Selection, err)
			}
			if parameterQuery.Selection.Presentation != `parameter required: string = "x"` {
				t.Fatalf("selected parameter presentation omitted its default: %q", parameterQuery.Selection.Presentation)
			}
			markedQuery, err := r.QueryType(TypeSelection{Symbol: "marked"})
			if err != nil || markedQuery.Selection.Presentation != `fn marked(required required: string) -> string` {
				t.Fatalf("required-choice role is absent from callable presentation: selection=%+v err=%v", markedQuery.Selection, err)
			}
			defaultAlias := "suffix: string = Defaults.defaultSuffix"
			defaultAt := strings.Index(source, defaultAlias)
			if defaultAt < 0 {
				t.Fatal("service default alias fixture was not found")
			}
			at := defaultAt + len("suffix: string = Defaults.")
			selected, err := r.QueryType(TypeSelection{Offset: &at})
			if err != nil || selected.Selection.Target == nil || selected.Selection.Target.Kind != "constant" || selected.Selection.Target.Constant == nil || selected.Selection.Target.Constant.Value != "!" || selected.Selection.Target.Module != "effra/constants" || selected.Selection.Target.Source == "" || selected.Selection.Target.LocationAvailable {
				t.Fatalf("default alias lost its checked lexical target: %+v err=%v", selected.Selection, err)
			}
			if len(r.Sources) < 2 || len(r.BundledBindings) == 0 {
				t.Fatalf("distributed default constant was not loaded into the source revision: %+v", r.Sources)
			}
		})
	}
	for _, source := range []string{
		`import Defaults "effra/constants" fn onlyDefault(value: string = Defaults.defaultSuffix) -> string { value }`,
		`import Defaults "effra/constants" service Settings { effect fn read(value: string = Defaults.defaultSuffix) -> string }`,
	} {
		r := Compile(source)
		foundDefault := false
		for _, binding := range r.BundledBindings {
			foundDefault = foundDefault || binding.Module == "effra/constants" && binding.Name == "defaultSuffix"
		}
		if !r.Checked || !foundDefault {
			t.Fatalf("default-only compiler-distributed alias did not enter the checked import revision: %+v %+v", r.Diagnostics, r.BundledBindings)
		}
	}
}

func TestLayerParameterFactsRemainInVersionedGraphView(t *testing.T) {
	source := `service Store { effect fn label() -> string }
impl Memory(label: string) for Store { effect fn label() -> string { label } }
layer Shared { Store = Memory("live") }
effect fn main() -> string { run Store.label().provide(Shared) }`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatalf("layer parameter GraphView source was rejected: %+v", r.Diagnostics)
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "layers"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.GraphView(request)
	if err != nil {
		t.Fatalf("build layer GraphView: %v", err)
	}
	facts := view.Data.Effra
	if facts.GraphViewVersion != 1 || facts.Snapshot.SchemaVersion != SemanticSchemaVersion || facts.ProducerIdentity != "effra/checker-abi-10/bundled-interface-5" {
		t.Fatalf("GraphView envelope or nested semantic qualification changed: view=%d snapshot=%d producer=%q", facts.GraphViewVersion, facts.Snapshot.SchemaVersion, facts.ProducerIdentity)
	}
	if SemanticSchemaVersion != 9 {
		t.Fatalf("semantic schema owner = %d, want 9", SemanticSchemaVersion)
	}
	var selectedParameter *Param
	for _, edge := range view.Edges {
		selection := edge.Data.Effra.Selection
		if edge.Data.Effra.Relation == "selects" && selection != nil && len(selection.Parameters) > 0 {
			selectedParameter = &selection.Parameters[0]
			break
		}
	}
	if selectedParameter == nil || selectedParameter.RequiredChoice || selectedParameter.DefaultValue != nil {
		t.Fatalf("selected layer parameter lost its checked ordinary/default facts: %+v", selectedParameter)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	var rawParameter map[string]any
	for _, rawEdge := range raw["edges"].([]any) {
		edge := rawEdge.(map[string]any)
		data := edge["data"].(map[string]any)
		effra := data["effra"].(map[string]any)
		if effra["relation"] != "selects" {
			continue
		}
		selection, ok := effra["selection"].(map[string]any)
		if !ok {
			continue
		}
		parameters, ok := selection["configurationParameters"].([]any)
		if ok && len(parameters) > 0 {
			rawParameter = parameters[0].(map[string]any)
			break
		}
	}
	if value, present := rawParameter["requiredChoice"]; !present || value != false {
		t.Fatalf("GraphView JSON omitted the explicit false requiredChoice field: %+v", rawParameter)
	}
	if value, present := rawParameter["defaultValue"]; !present || value != nil {
		t.Fatalf("GraphView JSON omitted the explicit null defaultValue field: %+v", rawParameter)
	}

	// Match the published limit to the encoded view, then require refusal one
	// byte below that exact serialized response size.
	exact := defaultGraphViewLimits
	for exact.ResponseBytes = 0; ; {
		request.limits = &exact
		fitted := view
		if exact.ResponseBytes > 0 {
			if fitted, err = r.GraphView(request); err != nil {
				t.Fatalf("exact GraphView response size was refused: %v", err)
			}
		}
		encoded, err := json.Marshal(fitted)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) == exact.ResponseBytes {
			break
		}
		exact.ResponseBytes = len(encoded)
	}
	less := defaultGraphViewLimits
	less.ResponseBytes = exact.ResponseBytes - 1
	request.limits = &less
	_, err = r.GraphView(request)
	requireGraphRefusal(t, err, GraphRefusalResponseLimit)
}

func TestParameterDefaultsUseCheckedScalarOwnerAndStayFullArity(t *testing.T) {
	refusals := []struct {
		name, source, code string
	}{
		{"unused marked default", `fn unused(required value: string = "x") -> string { "ok" }`, "EF102"},
		{"wrong scalar type", `fn unused(value: i64 = "x") -> i64 { 0 }`, "EF106"},
		{"computed value", `fn unused(value: i64 = 1 + 2) -> i64 { 0 }`, "EF102"},
		{"call value", `fn source() -> string { "x" } fn unused(value: string = source()) -> string { "ok" }`, "EF102"},
		{"effect value", `effect fn source() -> string { "x" } fn unused(value: string = run source()) -> string { "ok" }`, "EF102"},
		{"parameter reference", `fn unused(value: string, suffix: string = value) -> string { suffix }`, "EF102"},
		{"nominal constructor", `record Box { value: string } fn unused(value: Box = Box { value: "x" }) -> Box { value }`, "EF102"},
		{"unary alias", `const One: i64 = 1 fn unused(value: i64 = -One) -> i64 { value }`, "EF002"},
		{"marked and defaulted", `fn unused(required value: string = "x") -> string { value }`, "EF102"},
		{"unused service marked and defaulted", `service Settings { effect fn read(required key: string = "x") -> string }`, "EF102"},
		{"arbitrary module import", `import External "example/private" fn unused(value: string = External.default) -> string { value }`, "EF126"},
		{"implementation default", `service Settings { effect fn get(suffix: string) -> string } impl Live for Settings { effect fn get(suffix: string = "x") -> string { suffix } }`, "EF127"},
		{"service role mismatch required to ordinary", `service Settings { effect fn get(required key: string) -> string } impl Live for Settings { effect fn get(key: string) -> string { key } }`, "EF104"},
		{"service role mismatch ordinary to required", `service Settings { effect fn get(key: string) -> string } impl Live for Settings { effect fn get(required key: string) -> string { key } }`, "EF104"},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			r := Compile(refusal.source)
			if r.Checked || !hasCode(r, refusal.code) {
				t.Fatalf("parameter contract boundary was not enforced (%s): %+v", refusal.code, r.Diagnostics)
			}
		})
	}
	r := CompileFor(`fn render(value: string = "") -> string { value }`, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, declarations, err := r.Emit(false)
	if err != nil || !strings.Contains(declarations, `__ef_function_render: (arg_value: string) => string;`) {
		t.Fatalf("default incorrectly made the runtime declaration optional: %v\n%s", err, declarations)
	}
	checkStrictTypeScript(t, declarations, `import { render } from "./generated.mjs";
render("ok");
// @ts-expect-error Defaults do not make the foreign JavaScript ABI optional.
render();
// @ts-expect-error The full required ABI does not accept undefined either.
render(undefined);`)
	for _, target := range []string{"go", "js"} {
		minimum := CompileFor(`fn minimum(value: i64 = -9223372036854775808) -> i64 { value }`, target)
		if !minimum.Checked {
			t.Fatalf("direct negative minimum default was rejected for %s: %+v", target, minimum.Diagnostics)
		}
		positiveOverflow := CompileFor(`fn tooLarge(value: i64 = 9223372036854775808) -> i64 { value }`, target)
		if positiveOverflow.Checked || !hasCode(positiveOverflow, "EF106") {
			t.Fatalf("positive out-of-range default did not retain EF106 for %s: %+v", target, positiveOverflow.Diagnostics)
		}
	}
	// B2 binds the omitted argument to the declared constant; see
	// parameter_default_calls_test.go for the call contract.
	omitted := Compile(`fn render(value: string = "x") -> string { value } fn caller() -> string { render() }`)
	if !omitted.Checked {
		t.Fatalf("checked B2 binding refused an omitted defaulted argument: %+v", omitted.Diagnostics)
	}
}

func TestParameterDefaultAliasValueChangesRevisionAndInterfaceContent(t *testing.T) {
	contentHashWithoutSourceManifest := func(dto interfaceSummary) string {
		t.Helper()
		dto.ContentHash = ""
		dto.Sources = []SourceInfo{}
		encoded, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		return formatDigest(string(encoded))
	}
	compile := func(value string) (*Result, *Function, interfaceSummary) {
		t.Helper()
		source := `const Suffix: string = "` + value + `"
fn render(value: string = Suffix) -> string { value }`
		r := Compile(source)
		if !r.Checked {
			t.Fatalf("default alias rejected: %+v", r.Diagnostics)
		}
		f := r.Program.Functions[0]
		dto, err := exportInterfaceSummary(r.projector, currentModuleIdentity, "same-source-input", []*Function{f})
		if err != nil {
			t.Fatal(err)
		}
		return r, f, dto
	}
	first, firstFunction, firstSummary := compile("a")
	second, secondFunction, secondSummary := compile("b")
	if first.Revision == second.Revision || firstFunction.Identity != secondFunction.Identity || firstFunction.Contract.Callable.ID != secondFunction.Contract.Callable.ID || firstSummary.ContentHash == secondSummary.ContentHash || contentHashWithoutSourceManifest(firstSummary) == contentHashWithoutSourceManifest(secondSummary) {
		t.Fatalf("default value did not update source/interface identity or changed declaration identity: revisions=%s/%s function=%s/%s hashes=%s/%s", first.Revision, second.Revision, firstFunction.Identity, secondFunction.Identity, firstSummary.ContentHash, secondSummary.ContentHash)
	}
	if firstSummary.Declarations[0].Parameters[0].DefaultValue == nil || secondSummary.Declarations[0].Parameters[0].DefaultValue == nil || firstSummary.Declarations[0].Parameters[0].DefaultValue.Value != "a" || secondSummary.Declarations[0].Parameters[0].DefaultValue.Value != "b" {
		t.Fatalf("summary omitted resolved default values: %+v / %+v", firstSummary.Declarations, secondSummary.Declarations)
	}
	corrupt := firstSummary
	corrupt.Declarations = append([]summaryDeclaration{}, firstSummary.Declarations...)
	corrupt.Declarations[0].Parameters = append([]summaryParameter{}, firstSummary.Declarations[0].Parameters...)
	changed := *corrupt.Declarations[0].Parameters[0].DefaultValue
	changed.Value = "stale"
	corrupt.Declarations[0].Parameters[0].DefaultValue = &changed
	if err := first.projector.admitInterfaceSummary(corrupt, []*Function{firstFunction}); err == nil {
		t.Fatal("summary admission accepted a default value different from its checked declaration")
	}
	roleCompile := func(marked bool) (*Result, *Function, interfaceSummary) {
		t.Helper()
		parameter := "key: string"
		if marked {
			parameter = "required key: string"
		}
		r := Compile(`fn choose(` + parameter + `) -> string { key }`)
		if !r.Checked {
			t.Fatalf("required-choice role rejected: %+v", r.Diagnostics)
		}
		f := r.Program.Functions[0]
		dto, err := exportInterfaceSummary(r.projector, currentModuleIdentity, "same-role-input", []*Function{f})
		if err != nil {
			t.Fatal(err)
		}
		return r, f, dto
	}
	roleFree, freeFunction, freeSummary := roleCompile(false)
	roleMarked, markedFunction, markedSummary := roleCompile(true)
	if roleFree.Revision == roleMarked.Revision || freeFunction.Identity != markedFunction.Identity || freeSummary.ContentHash == markedSummary.ContentHash || contentHashWithoutSourceManifest(freeSummary) == contentHashWithoutSourceManifest(markedSummary) || !markedSummary.Declarations[0].Parameters[0].RequiredChoice {
		t.Fatal("required-choice role edit did not change revision/content while retaining declaration identity")
	}
}
