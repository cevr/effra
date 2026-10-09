package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/producer"
	lintsdk "effra.local/prototype/lint"
)

const lintFactSource = `error Denied
service Access {
    effect fn check(id: string) -> void raises { Denied }
}
impl DemoAccess for Access {
    effect fn check(id: string) -> void raises { Denied } {
        if id == "x" { fail Denied } else { void }
    }
}
effect fn helper(id: string) -> void raises { Denied } uses { Access } {
    run Access.check(id)
}
effect fn main() -> void {
    let Stdout = DemoAccess
    let access = Stdout
    run helper("a").catch<Denied>(void).provide<Access>(access)
    run helper("b").catch<Denied>(void).provide<Access>(Stdout)
}
`

func marshalFacts(t *testing.T, snapshot *lintsdk.Snapshot) []byte {
	t.Helper()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLintFactsAreDeterministicAndVersioned(t *testing.T) {
	first, second := Compile(lintFactSource), Compile(lintFactSource)
	if !first.Checked {
		t.Fatal(first.Diagnostics)
	}
	a := marshalFacts(t, first.LintFacts())
	if !bytes.Equal(a, marshalFacts(t, first.LintFacts())) || !bytes.Equal(a, marshalFacts(t, second.LintFacts())) {
		t.Fatal("equal checked sources produced different fact bytes")
	}
	snapshot := first.LintFacts()
	if snapshot.Schema != (lintsdk.Schema{Name: lintsdk.FactSchemaName, Version: lintsdk.FactSchemaVersion}) || !snapshot.Checked || snapshot.Source.Bytes != len(lintFactSource) {
		t.Fatalf("schema/source: %+v %+v", snapshot.Schema, snapshot.Source)
	}
	if !slices.Equal(snapshot.Families, lintsdk.Families()) || len(snapshot.Unavailable) != 0 {
		t.Fatalf("families: %v %v", snapshot.Families, snapshot.Unavailable)
	}
	if snapshot.Semantic != (lintsdk.Semantic{SchemaVersion: SemanticSchemaVersion, Revision: first.Revision, Target: "go"}) {
		t.Fatalf("unqualified semantic snapshot: %+v", snapshot.Semantic)
	}
	identity := producer.Identity{Strength: "unavailable", Qualifier: "process:lint-facts", ReuseScope: "process", Reason: "test"}
	if err := first.Qualify(identity); err != nil {
		t.Fatal(err)
	}
	qualified := first.LintFacts()
	if qualified.Semantic.Producer != "process:lint-facts" || qualified.Semantic.ReuseScope != "process" || qualified.Semantic.Revision != first.Revision {
		t.Fatalf("producer qualification: %+v", qualified.Semantic)
	}
	// Facts are ordered: callables and edges by source offset, declarations
	// by identity.
	for i := 1; i < len(snapshot.Callables); i++ {
		if snapshot.Callables[i-1].Span.Offset > snapshot.Callables[i].Span.Offset {
			t.Fatalf("callables are unordered: %+v", snapshot.Callables)
		}
	}
	if !slices.IsSortedFunc(snapshot.Providers, func(a, b lintsdk.Provider) int { return strings.Compare(a.Identity, b.Identity) }) {
		t.Fatal("providers are unordered")
	}
	js := CompileFor(lintFactSource, "js").LintFacts()
	if js.Semantic.Target != "js" || bytes.Equal(marshalFacts(t, js), a) {
		t.Fatal("target is not part of the snapshot")
	}
}

func TestLintFactsResolveIdentitiesNotSpelling(t *testing.T) {
	r := Compile(lintFactSource)
	snapshot := r.LintFacts()
	demo := snapshot.ProviderNamed("DemoAccess")
	stdout := snapshot.ProviderNamed("Stdout")
	denied := snapshot.FailureNamed("Denied")
	timeout := snapshot.FailureNamed("Timeout")
	if demo == nil || demo.Builtin || demo.Service != serviceIdentity("Access") || stdout == nil || !stdout.Builtin || stdout.Span != nil {
		t.Fatalf("providers: %+v %+v", demo, stdout)
	}
	if denied == nil || denied.Builtin || denied.Span == nil || timeout == nil || !timeout.Builtin || timeout.Identity != "error:builtin:Timeout" {
		t.Fatalf("failures: %+v %+v", denied, timeout)
	}
	for _, declaration := range r.Declarations {
		if declaration.Kind == "error" && declaration.Name == "Denied" && declaration.Identity != denied.Identity {
			t.Fatalf("failure identity differs from its declaration identity: %s %s", declaration.Identity, denied.Identity)
		}
	}
	if len(snapshot.Provisions) != 2 {
		t.Fatalf("provisions: %+v", snapshot.Provisions)
	}
	// A local alias and a local spelled like the builtin Stdout provider
	// both resolve to DemoAccess.
	access, shadowed := snapshot.Provisions[0], snapshot.Provisions[1]
	for _, provision := range []lintsdk.Provision{access, shadowed} {
		if provision.Provider != demo.Identity || provision.Service != serviceIdentity("Access") || provision.Kind != lintsdk.ProvisionDirect || !slices.Contains(provision.Receiver, serviceIdentity("Access")) {
			t.Fatalf("provision: %+v", provision)
		}
	}
	if !strings.Contains(lintFactSource[shadowed.Span.Offset:], "(Stdout)") {
		t.Fatalf("provision order: %+v", snapshot.Provisions)
	}
	main := snapshot.FunctionNamed("main")
	helper := snapshot.FunctionNamed("helper")
	if main == nil || helper == nil || access.Callable != main.Identity {
		t.Fatalf("callables: %+v %+v", main, helper)
	}
	if !slices.Equal(helper.Body.Failures, []string{denied.Identity}) || !slices.Equal(helper.Declared.Failures, []string{denied.Identity}) || len(main.Body.Failures) != 0 {
		t.Fatalf("rows: helper %+v main %+v", helper.Body, main.Body)
	}
	if len(helper.Contributions) == 0 || !slices.Contains(helper.Contributions[0].Members, denied.Identity) {
		t.Fatalf("contributions: %+v", helper.Contributions)
	}
	method := snapshot.Callable(r.checkedProviders["DemoAccess"].Methods[0].Identity)
	if method == nil || method.Kind != lintsdk.CallableProviderMethod || method.Owner != demo.Identity || !slices.Equal(method.Body.Failures, []string{denied.Identity}) {
		t.Fatalf("provider method: %+v", method)
	}
	var shadow *lintsdk.Binding
	for i, binding := range snapshot.Bindings {
		if binding.Kind == "let" && binding.Name == "Stdout" {
			shadow = &snapshot.Bindings[i]
		}
	}
	if shadow == nil || shadow.Callable != main.Identity || len(shadow.Uses) != 2 {
		t.Fatalf("shadowing binding: %+v", snapshot.Bindings)
	}
	var operationCall bool
	for _, call := range snapshot.Calls {
		if call.Caller == helper.Identity && call.Kind == lintsdk.CallOperation && call.Callee == r.checkedServices["Access"].Methods[0].Identity {
			operationCall = true
		}
	}
	if !operationCall {
		t.Fatalf("calls: %+v", snapshot.Calls)
	}
}

func TestLintFactsAvailability(t *testing.T) {
	unchecked := Compile(`effect fn main() -> void { run missing() }`)
	snapshot := unchecked.LintFacts()
	if snapshot.Checked || len(snapshot.Families) != 0 || len(snapshot.Unavailable) != len(lintsdk.Families()) {
		t.Fatalf("unchecked: %+v", snapshot)
	}
	for _, family := range lintsdk.Families() {
		if snapshot.UnavailableReason(family) != lintsdk.ReasonUncheckedSource {
			t.Fatalf("%s: %q", family, snapshot.UnavailableReason(family))
		}
	}
	unparsed := Compile(`effect fn main( {`)
	if facts := unparsed.LintFacts(); facts.Checked || facts.UnavailableReason(lintsdk.FamilyCallables) != lintsdk.ReasonUncheckedSource {
		t.Fatalf("unparsed: %+v", facts)
	}

	r := Compile(lintFactSource)
	selected := r.LintFacts(lintsdk.FamilyImports)
	if !slices.Equal(selected.Families, []lintsdk.Family{lintsdk.FamilyImports}) || selected.Callables != nil || selected.UnavailableReason(lintsdk.FamilyCallables) != lintsdk.ReasonNotRequested {
		t.Fatalf("selection: %+v", selected)
	}

	// Exhausted lexical facts make bindings unavailable, never empty.
	budget := Compile(`effect fn task() -> string { "ok" } effect fn main() -> void { let forgotten = task(); void }`)
	budget.lexical.complete = false
	facts := budget.LintFacts()
	if facts.Has(lintsdk.FamilyBindings) || facts.UnavailableReason(lintsdk.FamilyBindings) != lintsdk.ReasonFactBudget || !facts.Has(lintsdk.FamilyCallables) {
		t.Fatalf("budget: %v %v", facts.Families, facts.Unavailable)
	}
	if lint := budget.Lint(false); len(lint.LintDiagnostics) != 1 || lint.LintDiagnostics[0].Rule != "unused-recipe" {
		t.Fatalf("built-in unused-recipe depends on the bindings family: %+v", lint)
	}
}

func TestLintRegistryReservesBuiltinRules(t *testing.T) {
	registry, err := LintRegistry()
	if err != nil {
		t.Fatal(err)
	}
	config, err := lintsdk.ParseConfig([]byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(config)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	info := configuration.Inspect()
	if len(info) != len(LintRules()) {
		t.Fatalf("inspection: %+v", info)
	}
	for _, rule := range LintRules() {
		index := slices.IndexFunc(info, func(entry lintsdk.RuleInfo) bool { return entry.Rule == rule.Name })
		entry := info[index]
		want := lintsdk.Severity(rule.Severity)
		if rule.Severity == "suggestion" {
			want = lintsdk.SeverityHint
		}
		if !entry.Builtin || entry.Code != rule.Code || entry.DefaultSeverity != want || entry.Fixed != (rule.Name == suppressionRule) {
			t.Fatalf("%s: %+v", rule.Name, entry)
		}
	}
	for _, document := range []string{`{"version":1,"rules":{"invalid-suppression":"off"}}`, `{"version":1,"rules":{"no-such-rule":"error"}}`, `{"version":1,"rules":{"effra/unused-recipe":"error"}}`} {
		config, _ := lintsdk.ParseConfig([]byte(document))
		if _, problems := registry.Configure(config); len(problems) != 1 {
			t.Fatalf("%s accepted: %+v", document, problems)
		}
	}
}

// Built-in diagnostics are unchanged by expressing rules over facts: on
// every corpus source, the fact rules report exactly what the replaced
// scoped predicates reported. Suppression, severity and ordering stay in
// the unchanged Lint projection.
func TestBuiltinFactRulesMatchReplacedPredicates(t *testing.T) {
	type source struct{ name, text, dir string }
	sources := []source{
		{"nested provisions", `service Ticker { effect fn now() -> i64 } impl Fixed for Ticker { effect fn now() -> i64 { 1 } }
enum Mode { On { label: string } Off }
effect fn task(mode: Mode) -> string uses { Ticker } { match mode { Mode.On { label } => { let pending = Ticker.now(); run Console.log(label).provide<Console>(Stdout); label } Mode.Off => "off" } }
effect fn main() -> void {
let unused = task(Mode.On { label: "a" }).provide<Ticker>(Fixed)
let both = run Console.log("x").provide<Console>(Stdout).provide<Console>(Stdout).provide<Ticker>(Fixed)
if true { run Console.log("y").provide<Ticker>(Fixed).provide<Console>(Stdout) } else { void }
}`, "."},
		{"imports", `import go strings "strings"
import go strconv "strconv"
effect fn main() -> string { run strings.ToUpper("x").provide<Foreign>(Host) }`, "."},
		{"provider methods", `service Greeter { effect fn greet() -> string }
impl Loud for Greeter { effect fn greet() -> string { run Console.log("hi").provide<Console>(Stdout).provide<Console>(Stdout); "HI" } }
effect fn main() -> void { let g = run Greeter.greet().provide<Greeter>(Loud); void }`, "."},
	}
	for _, pattern := range []string{"../../examples/*.ef", "../../examples/lintpack/testdata/*.ef", "bundled/*/*.ef"} {
		files, err := filepath.Glob(pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: %v", pattern, err)
		}
		for _, file := range files {
			text, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, source{file, string(text), filepath.Dir(file)})
		}
	}
	findings := map[string]int{}
	for _, source := range sources {
		r := CompileAt(source.text, "go", source.dir)
		if !r.Checked {
			// witness.ef needs its module's Codec, and i64/parse.ef names the
			// failure its module admits; both check only through an import.
			if strings.HasSuffix(source.name, "missing-service.ef") || strings.HasSuffix(source.name, "witness.ef") || strings.HasSuffix(source.name, filepath.Join("i64", "parse.ef")) {
				continue
			}
			t.Fatalf("%s did not check: %v", source.name, r.Diagnostics)
		}
		want := replacedBuiltinPredicates(r)
		var got []string
		snapshot := r.LintFacts(lintsdk.FamilyDeclarations, lintsdk.FamilyProvisions, lintsdk.FamilyImports)
		for _, rule := range []*lintsdk.Rule{redundantProvisionRule, unusedGoImportRule} {
			reported, err := r.applyBuiltinRule(rule, snapshot)
			if err != nil {
				t.Fatalf("%s: %s: %v", source.name, rule.Name, err)
			}
			for _, finding := range reported {
				got = append(got, fmt.Sprintf("%s %+v %s", rule.Name, finding.Span, finding.Message))
			}
			findings[rule.Name] += len(reported)
		}
		slices.Sort(got)
		if !slices.Equal(want, got) {
			t.Fatalf("%s:\nreplaced %q\nfacts    %q", source.name, want, got)
		}
	}
	if findings["redundant-provision"] < 3 || findings["unused-go-import"] < 1 {
		t.Fatalf("parity corpus is too thin: %v", findings)
	}
}

// replacedBuiltinPredicates is the pre-SDK redundant-provision and
// unused-go-import logic, retained as the parity oracle.
func replacedBuiltinPredicates(r *Result) []string {
	var out []string
	var block func(*Block)
	var expr func(*Expr)
	expr = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Kind == "provide" && !slices.Contains(e.Left.Type.Services, e.Name) {
			out = append(out, fmt.Sprintf("redundant-provision %+v receiver does not require %s", spanFact(e.Span), e.Name))
		}
		forEachExprChild(e, expr)
		for _, arm := range e.Arms {
			block(arm.Body)
		}
		block(e.Then)
		block(e.Else)
	}
	block = func(b *Block) {
		if b == nil {
			return
		}
		for _, s := range b.Statements {
			expr(s.Value)
			expr(s.Payload)
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body)
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			block(f.Body)
		}
	}
	for _, imp := range r.Program.Imports {
		if !r.Program.UsedImports[imp.Alias] {
			out = append(out, fmt.Sprintf("unused-go-import %+v no function of Go import %s is referenced; the import still initializes its Go package, so remove it only if that initialization is unneeded", spanFact(imp.Span), imp.Alias))
		}
	}
	slices.Sort(out)
	return out
}

// Built-in advice is not bounded by the custom-rule output limit: every
// redundant provision is a suggestion, suppression applies before policy,
// and lint passes under strict policy however many there are.
func TestBuiltinLintHasNoCustomOutputLimit(t *testing.T) {
	provisions := func(count int, suppressed bool) string {
		var source strings.Builder
		source.WriteString("effect fn task() -> void { void }\neffect fn main() -> void {\n")
		if suppressed {
			source.WriteString("// effra-lint-disable-next-line redundant-provision -- deliberately redundant\n")
		}
		for range count {
			source.WriteString("run task().provide<Console>(Stdout)\n")
		}
		source.WriteString("}\n")
		return source.String()
	}
	for _, run := range []struct {
		count, reported int
		suppressed      bool
	}{
		{lintsdk.MaxFindingsPerRule, lintsdk.MaxFindingsPerRule, false},
		{lintsdk.MaxFindingsPerRule + 1, lintsdk.MaxFindingsPerRule + 1, false},
		{lintsdk.MaxFindingsPerRule + 2, lintsdk.MaxFindingsPerRule + 1, true},
	} {
		result := Compile(provisions(run.count, run.suppressed))
		if !result.Checked {
			t.Fatal(result.Diagnostics)
		}
		lint := result.Lint(true)
		if !lint.LintPassed || lint.Errors != 0 || lint.Suggestions != run.reported || len(lint.LintDiagnostics) != run.reported {
			t.Fatalf("%d provisions: passed=%v errors=%d suggestions=%d diagnostics=%d", run.count, lint.LintPassed, lint.Errors, lint.Suggestions, len(lint.LintDiagnostics))
		}
		for _, diagnostic := range lint.LintDiagnostics {
			if diagnostic.Rule != "redundant-provision" {
				t.Fatalf("%d provisions: %+v", run.count, diagnostic)
			}
		}
	}
}
