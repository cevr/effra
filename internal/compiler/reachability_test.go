package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

func checkedApplicationPlan(t *testing.T, source string, mode GoGenerationMode) (*Result, *ApplicationPlan) {
	t.Helper()
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(mode)
	if err != nil {
		t.Fatal(err)
	}
	return r, plan
}

func exampleCompile(t *testing.T, file string) *Result {
	t.Helper()
	source, err := os.ReadFile("../../examples/" + file)
	if err != nil {
		t.Fatal(err)
	}
	r := CompileAt(string(source), "go", "../../examples")
	if !r.Checked {
		t.Fatal(file, r.Diagnostics)
	}
	return r
}

func exampleApplicationPlan(t *testing.T, file string, mode GoGenerationMode) (*Result, *ApplicationPlan) {
	t.Helper()
	r := exampleCompile(t, file)
	plan, err := r.ApplicationPlan(mode)
	if err != nil {
		t.Fatal(file, err)
	}
	return r, plan
}

func requirePlanned(t *testing.T, plan *ApplicationPlan, kind ApplicationRequirementKind, identities ...string) {
	t.Helper()
	for _, identity := range identities {
		if !plan.Requires(kind, identity) {
			t.Errorf("plan lacks %s %s; %s: %v", kind, identity, kind, plan.Identities(kind))
		}
	}
}

func requireUnplanned(t *testing.T, plan *ApplicationPlan, kind ApplicationRequirementKind, identities ...string) {
	t.Helper()
	for _, identity := range identities {
		if requirement, found := plan.Requirement(kind, identity); found {
			t.Errorf("plan retains unreachable %s %s via %s (%s)", kind, identity, requirement.Via, requirement.Reason)
		}
	}
}

func requireProvenance(t *testing.T, plan *ApplicationPlan, kind ApplicationRequirementKind, identity, via, reason string) {
	t.Helper()
	requirement, found := plan.Requirement(kind, identity)
	if !found || requirement.Via != via || requirement.Reason != reason {
		t.Errorf("%s %s provenance = %+v (found %v), want via %s (%s)", kind, identity, requirement, found, via, reason)
	}
}

func symbolIdentity(t *testing.T, r *Result, name string) string {
	t.Helper()
	symbol := r.Find(name)
	if symbol == nil {
		t.Fatalf("missing checked symbol %s", name)
	}
	return symbol.Identity
}

func providerIdentity(t *testing.T, r *Result, name string) string {
	t.Helper()
	provider := r.checkedProviders[name]
	if provider == nil {
		t.Fatalf("missing checked provider %s", name)
	}
	return providerTypeRef(provider).Declaration
}

func operationIdentity(t *testing.T, r *Result, service, name string) string {
	t.Helper()
	if declaration := r.checkedServices[service]; declaration != nil {
		for _, operation := range declaration.Methods {
			if operation.Name == name {
				return operation.Identity
			}
		}
	}
	t.Fatalf("missing checked operation %s.%s", service, name)
	return ""
}

func providerMethodIdentity(t *testing.T, r *Result, provider, name string) string {
	t.Helper()
	if declaration := r.checkedProviders[provider]; declaration != nil {
		for _, method := range declaration.Methods {
			if method.Name == name {
				return method.Identity
			}
		}
	}
	t.Fatalf("missing checked provider method %s.%s", provider, name)
	return ""
}

func dataIdentity(kind, name string) string {
	return kind + ":" + currentModuleIdentity + ":module:" + name
}

func sameRuntimeModules(plan *ApplicationPlan, want ...rt.RuntimeModule) bool {
	got := plan.RuntimeModules()
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}

const applicationHigherOrderSource = `import Fns "effra/functions"
fn unused(input: string) -> string {
    input + "!"
}
effect fn echo(input: string) -> string {
    input
}
fn suffix(input: string) -> string {
    input + "?"
}
effect fn main() -> string {
    let forwarded = run Fns.call(echo, "a")
    forwarded + suffix("b")
}
`

func TestApplicationPlanRetainsDirectAndHigherOrderCalls(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationHigherOrderSource, GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	call := symbolIdentity(t, r, "Fns.call")
	echo := symbolIdentity(t, r, "echo")
	requireProvenance(t, plan, RequiresFunction, main, "", "entry")
	requireProvenance(t, plan, RequiresFunction, symbolIdentity(t, r, "suffix"), main, "call")
	requireProvenance(t, plan, RequiresFunction, call, main, "call")
	requireProvenance(t, plan, RequiresCallableValue, echo, main, "function-value")
	requireProvenance(t, plan, RequiresFunction, echo, main, "function-value")
	invocations := 0
	for _, requirement := range plan.Requirements {
		if requirement.Kind == RequiresDynamicCall && requirement.Via == call {
			invocations++
		}
	}
	if invocations != 1 {
		t.Fatalf("bundled callback invocation sites = %d: %v", invocations, plan.Identities(RequiresDynamicCall))
	}
	if got := plan.Identities(RequiresCallableValue); !slices.Equal(got, []string{echo}) {
		t.Fatalf("callable target set = %v, want only the taken function value", got)
	}
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "unused"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore) {
		t.Fatalf("runtime modules = %v", plan.RuntimeModules())
	}
}

const applicationStoredCallbackSource = `record State {
    name: string
}
record Operations {
    step: fn(State, string) -> State
}
record Unused {
    name: string
}
fn renamed(state: State, event: string) -> State {
    State {
        name: event
    }
}
fn untouched(state: State, event: string) -> State {
    State {
        name: state.name
    }
}
fn apply(step: fn(State, string) -> State, state: State, event: string) -> State {
    step(state, event)
}
fn advance(operations: Operations, state: State, event: string) -> State {
    let direct = operations.step(state, event)
    apply(operations.step, direct, event + ":forwarded")
}
effect fn main() -> string {
    let operations = Operations {
        step: renamed
    }
    let next = advance(operations, State {
            name: "old"
        }, "new")
    next.name
}
`

func TestApplicationPlanRetainsStoredAndForwardedCallbacks(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationStoredCallbackSource, GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	renamed := symbolIdentity(t, r, "renamed")
	advance := symbolIdentity(t, r, "advance")
	apply := symbolIdentity(t, r, "apply")
	requireProvenance(t, plan, RequiresCallableValue, renamed, main, "function-value")
	requirePlanned(t, plan, RequiresFunction, renamed, advance, apply)
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", "State"), dataIdentity("record", "Operations"))
	sites := map[string]int{}
	for _, requirement := range plan.Requirements {
		if requirement.Kind == RequiresDynamicCall {
			sites[requirement.Via]++
		}
	}
	if sites[advance] != 1 || sites[apply] != 1 || len(sites) != 2 {
		t.Fatalf("callable field and forwarded parameter invocations = %v", sites)
	}
	if got := plan.Identities(RequiresCallableValue); !slices.Equal(got, []string{renamed}) {
		t.Fatalf("callable target set = %v", got)
	}
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "untouched"))
	requireUnplanned(t, plan, RequiresDeclaration, dataIdentity("record", "Unused"))
}

func TestApplicationPlanRetainsExampleCallablesAndConfiguredProvider(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "callables-state.ef", GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	renamed := symbolIdentity(t, r, "renamed")
	configured := providerIdentity(t, r, "Configured")
	apply := providerMethodIdentity(t, r, "Configured", "apply")
	requireProvenance(t, plan, RequiresCallableValue, renamed, main, "function-value")
	requireProvenance(t, plan, RequiresProvider, configured, main, "provider-constructor")
	requirePlanned(t, plan, RequiresOperation, operationIdentity(t, r, "Transition", "apply"))
	invocations := 0
	for _, requirement := range plan.Requirements {
		if requirement.Kind == RequiresDynamicCall && requirement.Via == apply {
			invocations++
		}
	}
	if invocations != 1 {
		t.Fatalf("configured callback invocation in provider body = %d: %v", invocations, plan.Identities(RequiresDynamicCall))
	}
}

const applicationShadowingSource = `fn label(input: string) -> string {
    "top:" + input
}
record Tag {
    label: string
}
enum Choice {
    Named {
        label: string
    }
    Empty
}
service Names {
    effect fn label(input: string) -> string
}
impl Plain for Names {
    effect fn label(input: string) -> string {
        input
    }
}
fn upper(input: string) -> string {
    input + "^"
}
fn relabel(label: fn(string) -> string, input: string) -> string {
    label(input)
}
fn pick(choice: Choice) -> string {
    match choice {
        Choice.Named { label } => label
        Choice.Empty => "empty"
    }
}
effect fn main() -> string {
    let tag = Tag {
        label: "field"
    }
    let chosen = pick(Choice.Named {
            label: tag.label
        })
    let first = relabel(upper, chosen)
    run Names.label(first).provide<Names>(Plain)
}
`

func TestApplicationPlanFollowsCheckedResolutionNotSpelling(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationShadowingSource, GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	relabel := symbolIdentity(t, r, "relabel")
	operation := operationIdentity(t, r, "Names", "label")
	requireProvenance(t, plan, RequiresOperation, operation, main, "operation-call")
	requirePlanned(t, plan, RequiresFunction, relabel, symbolIdentity(t, r, "pick"))
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "Plain"), main, "provider-value")
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", "Tag"), dataIdentity("enum", "Choice"))
	requireProvenance(t, plan, RequiresCallableValue, symbolIdentity(t, r, "upper"), main, "function-value")
	if got := plan.Identities(RequiresDynamicCall); len(got) != 1 || !strings.HasPrefix(got[0], relabel+"@") {
		t.Fatalf("shadowing parameter must stay a dynamic call: %v", got)
	}
	label := symbolIdentity(t, r, "label")
	if label == operation {
		t.Fatalf("top-level function and service operation share identity %s", label)
	}
	requireUnplanned(t, plan, RequiresFunction, label)
	requireUnplanned(t, plan, RequiresCallableValue, label)
}

const applicationProviderSource = `service Names {
    effect fn get(id: string) -> string
}
service Greeting {
    effect fn hello(id: string) -> string
}
service Unused {
    effect fn get(id: string) -> string
}
impl NamesA for Names {
    effect fn get(id: string) -> string {
        "A"
    }
}
impl NamesB for Names {
    effect fn get(id: string) -> string {
        "B"
    }
}
impl NamesC for Names {
    effect fn get(id: string) -> string {
        "C"
    }
}
impl Prefixed(prefix: string) for Greeting uses { Names } {
    effect fn hello(id: string) -> string uses { Names } {
        let name = run Names.get(id)
        prefix + name
    }
}
impl Polite for Greeting {
    effect fn hello(id: string) -> string {
        "hello"
    }
}
impl Spare for Unused {
    effect fn get(id: string) -> string {
        id
    }
}
effect fn main() -> string {
    let greeting = run Prefixed("A: ").provide<Names>(NamesA)
    run Greeting.hello("42").provide<Names>(NamesB).provide<Greeting>(greeting)
}
`

func TestApplicationPlanRetainsPureConfiguredAndCapturingProviders(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationProviderSource, GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	hello := providerMethodIdentity(t, r, "Prefixed", "hello")
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "Prefixed"), main, "provider-constructor")
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "NamesA"), main, "provider-value")
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "NamesB"), main, "provider-value")
	requireProvenance(t, plan, RequiresOperation, operationIdentity(t, r, "Names", "get"), hello, "operation-call")
	requirePlanned(t, plan, RequiresService, serviceIdentity("Names"), serviceIdentity("Greeting"))
	requireUnplanned(t, plan, RequiresProvider, providerIdentity(t, r, "NamesC"), providerIdentity(t, r, "Polite"), providerIdentity(t, r, "Spare"))
	requireUnplanned(t, plan, RequiresService, serviceIdentity("Unused"))
	requireUnplanned(t, plan, RequiresOperation, operationIdentity(t, r, "Unused", "get"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore) {
		t.Fatalf("source providers require no native module: %v", plan.RuntimeModules())
	}
}

func TestApplicationPlanSelectsBuiltinRuntimeOnlyThroughReachableProviders(t *testing.T) {
	quiet := `impl Quiet for Console {
    effect fn log(message: string) -> void {
        void
    }
}
effect fn main() -> void {
    run Console.log("quiet").provide<Console>(Quiet)
}
`
	r, plan := checkedApplicationPlan(t, quiet, GoGenerationBuild)
	requirePlanned(t, plan, RequiresProvider, providerIdentity(t, r, "Quiet"))
	requirePlanned(t, plan, RequiresService, serviceIdentity("Console"))
	requireUnplanned(t, plan, RequiresProvider, providerIdentity(t, r, "Stdout"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore) {
		t.Fatalf("a source Console provider must not select the console module: %v", plan.RuntimeModules())
	}
	loud := strings.Replace(quiet, "provide<Console>(Quiet)", "provide<Console>(Stdout)", 1)
	r, plan = checkedApplicationPlan(t, loud, GoGenerationBuild)
	stdout := providerIdentity(t, r, "Stdout")
	requireProvenance(t, plan, RequiresRuntimeModule, string(rt.RuntimeModuleConsole), stdout, "provider-runtime")
	requireUnplanned(t, plan, RequiresProvider, providerIdentity(t, r, "Quiet"))
}

const applicationLayerSource = `service Store {
    effect fn label() -> string
}
service Account {
    effect fn label() -> string
}
impl LiveStore for Store {
    effect fn label() -> string {
        "live"
    }
}
impl FixtureStore(label: string) for Store {
    effect fn label() -> string {
        label
    }
}
impl SpareStore for Store {
    effect fn label() -> string {
        "spare"
    }
}
impl AccountLive for Account uses { Store } {
    effect fn label() -> string {
        run Store.label()
    }
}
layer Shared {
    Store = LiveStore
}
layer App provides { Account } {
    merge Shared;
    Account = AccountLive
}
layer Fixture {
    merge App;
    replace Store = FixtureStore("fixture")
}
layer Spare {
    Store = SpareStore
}
effect fn main() -> string {
    run Account.label().provide(Fixture)
}
`

func TestApplicationPlanRetainsSelectedHiddenAndReplacementLayerNodes(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationLayerSource, GoGenerationBuild)
	main := symbolIdentity(t, r, "main")
	fixture := layerID("Fixture")
	requireProvenance(t, plan, RequiresLayer, fixture, main, "layer-provision")
	requireProvenance(t, plan, RequiresRuntimeModule, string(rt.RuntimeModuleLayers), fixture, "layer-provision")
	nodes := plan.Identities(RequiresLayerNode)
	if len(nodes) != 2 {
		t.Fatalf("selected layer nodes = %v", nodes)
	}
	var store, account string
	for _, node := range nodes {
		requirement, _ := plan.Requirement(RequiresLayerNode, node)
		switch requirement.Reason {
		case "hidden-layer-node":
			store = node
		case "layer-node":
			account = node
		}
	}
	if store == "" || account == "" {
		t.Fatalf("hidden and public node provenance: %+v", plan.Requirements)
	}
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "FixtureStore"), store, "layer-replacement")
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "AccountLive"), account, "layer-binding")
	requirePlanned(t, plan, RequiresService, serviceIdentity("Store"), serviceIdentity("Account"))
	requirePlanned(t, plan, RequiresOperation, operationIdentity(t, r, "Store", "label"))
	requireUnplanned(t, plan, RequiresProvider, providerIdentity(t, r, "LiveStore"), providerIdentity(t, r, "SpareStore"))
	requireUnplanned(t, plan, RequiresLayer, layerID("Spare"), layerID("App"), layerID("Shared"))
}

func TestApplicationPlanRetainsExampleLayerReplacement(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "layers.ef", GoGenerationBuild)
	if got := plan.Identities(RequiresLayer); !slices.Equal(got, []string{layerID("Fixture")}) {
		t.Fatalf("provided layers = %v", got)
	}
	requirePlanned(t, plan, RequiresProvider, providerIdentity(t, r, "Memory"), providerIdentity(t, r, "AccountLive"), providerIdentity(t, r, "InvoiceLive"))
	hidden := 0
	for _, requirement := range plan.Requirements {
		if requirement.Kind == RequiresLayerNode && requirement.Reason == "hidden-layer-node" {
			hidden++
		}
	}
	if hidden != 1 || len(plan.Identities(RequiresLayerNode)) != 3 {
		t.Fatalf("layer nodes: %+v", plan.Requirements)
	}
}

const applicationForeignSource = `import go strings "strings"
import go strconv "strconv"
effect fn parse(text: string) -> bool raises { GoError } uses { Foreign } {
    run strconv.ParseBool(text).orFail()
}
effect fn trimmed(text: string) -> string uses { Foreign } {
    run strings.TrimSpace(text)
}
effect fn main() -> bool raises { GoError } {
    run parse("true").provide<Foreign>(Host)
}
`

func TestApplicationPlanNamesOnlyReachableForeignImports(t *testing.T) {
	r, plan := checkedApplicationPlan(t, applicationForeignSource, GoGenerationBuild)
	parse := symbolIdentity(t, r, "parse")
	requireProvenance(t, plan, RequiresForeign, "go:strconv.ParseBool", parse, "foreign-call")
	requireProvenance(t, plan, RequiresGoImport, "strconv", "go:strconv.ParseBool", "foreign-call")
	requireProvenance(t, plan, RequiresProvider, providerIdentity(t, r, "Host"), symbolIdentity(t, r, "main"), "provider-value")
	requirePlanned(t, plan, RequiresService, serviceIdentity("Foreign"))
	requirePlanned(t, plan, RequiresHelper, "foreign", "orFail")
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "trimmed"))
	requireUnplanned(t, plan, RequiresForeign, "go:strings.TrimSpace")
	requireUnplanned(t, plan, RequiresGoImport, "strings")
	// Both declarations root initialization; only strconv is named by code.
	requireProvenance(t, plan, RequiresGoInitialization, "strconv", "", "declared-foreign-import")
	requireProvenance(t, plan, RequiresGoInitialization, "strings", "", "declared-foreign-import")
	if !plan.includesGoImport("strconv") || plan.includesGoImport("strings") {
		t.Fatalf("emitted import aliases: %v", plan.goImports)
	}
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore, rt.RuntimeModuleInterop) {
		t.Fatalf("fallible foreign result must select interop only: %v", plan.RuntimeModules())
	}
	infallible := strings.Replace(applicationForeignSource, `effect fn main() -> bool raises { GoError } {
    run parse("true").provide<Foreign>(Host)
}`, `effect fn main() -> string {
    run trimmed(" text ").provide<Foreign>(Host)
}`, 1)
	r, plan = checkedApplicationPlan(t, infallible, GoGenerationBuild)
	requirePlanned(t, plan, RequiresGoImport, "strings")
	requireUnplanned(t, plan, RequiresGoImport, "strconv")
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "parse"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore) {
		t.Fatalf("an infallible foreign call needs no interop module: %v", plan.RuntimeModules())
	}
}

func TestApplicationPlanRetainsExampleHTTPCapabilities(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "http.ef", GoGenerationBuild)
	route := symbolIdentity(t, r, "route")
	requireProvenance(t, plan, RequiresCallableValue, route, symbolIdentity(t, r, "main"), "function-value")
	requireProvenance(t, plan, RequiresRuntimeModule, string(rt.RuntimeModuleHTTP), providerIdentity(t, r, "LiveHttp"), "provider-runtime")
	requirePlanned(t, plan, RequiresGoImport, "strings", "effra.local/prototype/examples/sdk")
	requirePlanned(t, plan, RequiresHelper, "timeout")
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore, rt.RuntimeModuleFiles, rt.RuntimeModuleHTTP, rt.RuntimeModuleInterop) {
		t.Fatalf("http runtime modules = %v", plan.RuntimeModules())
	}
}

const applicationModeSource = `service Directory {
    effect fn name(id: string) -> string
}
impl Fixture for Directory {
    effect fn name(id: string) -> string {
        "Ada"
    }
}
impl Live for Directory {
    effect fn name(id: string) -> string {
        "Live"
    }
}
effect fn greeting(id: string) -> string uses { Directory } {
    run Directory.name(id)
}
effect fn test_greeting() -> void raises { AssertionFailed } uses { Assert } {
    let actual = run greeting("42").provide<Directory>(Fixture)
    run Assert.equalText(actual, "Ada")
}
effect fn main() -> string {
    run greeting("42").provide<Directory>(Live)
}
`

func TestApplicationPlanSeparatesOrdinaryAndTestRoots(t *testing.T) {
	r, build := checkedApplicationPlan(t, applicationModeSource, GoGenerationBuild)
	test, err := r.ApplicationPlan(GoGenerationTest)
	if err != nil {
		t.Fatal(err)
	}
	main := symbolIdentity(t, r, "main")
	testGreeting := symbolIdentity(t, r, "test_greeting")
	greeting := symbolIdentity(t, r, "greeting")
	harness := []string{}
	for _, name := range testHarnessProviders {
		harness = append(harness, providerIdentity(t, r, name))
	}

	requireProvenance(t, build, RequiresFunction, main, "", "entry")
	requirePlanned(t, build, RequiresFunction, greeting)
	requirePlanned(t, build, RequiresProvider, providerIdentity(t, r, "Live"))
	requireUnplanned(t, build, RequiresFunction, testGreeting)
	requireUnplanned(t, build, RequiresProvider, append([]string{providerIdentity(t, r, "Fixture")}, harness...)...)
	requireUnplanned(t, build, RequiresService, serviceIdentity("Assert"), serviceIdentity("Sync"))
	if build.Mode != GoGenerationBuild || !sameRuntimeModules(build, rt.RuntimeModuleCore) {
		t.Fatalf("ordinary plan: %s %v", build.Mode, build.RuntimeModules())
	}

	requireProvenance(t, test, RequiresFunction, testGreeting, "", "test")
	requirePlanned(t, test, RequiresFunction, greeting)
	requirePlanned(t, test, RequiresProvider, providerIdentity(t, r, "Fixture"))
	for _, identity := range harness {
		requireProvenance(t, test, RequiresProvider, identity, "", "test-harness")
	}
	requirePlanned(t, test, RequiresService, serviceIdentity("Assert"), serviceIdentity("Clock"), serviceIdentity("Scheduler"), serviceIdentity("Sync"))
	requireUnplanned(t, test, RequiresFunction, main)
	requireUnplanned(t, test, RequiresProvider, providerIdentity(t, r, "Live"))
	if test.Mode != GoGenerationTest || !sameRuntimeModules(test, rt.RuntimeModuleCore, rt.RuntimeModuleSync) {
		t.Fatalf("test plan: %s %v", test.Mode, test.RuntimeModules())
	}
}

func TestApplicationPlanRetainsExampleTestCases(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "testing.ef", GoGenerationTest)
	tests, err := r.Tests()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		requireProvenance(t, plan, RequiresFunction, test.Identity, "", "test")
	}
	requirePlanned(t, plan, RequiresHelper, "scope", "fork", "fiber.join", "catch")
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("error", "Missing"))
	if _, err := r.ApplicationPlan(GoGenerationBuild); err == nil {
		t.Fatal("a source without main has no ordinary application root")
	}
}

func TestApplicationPlanRequiresConcreteCheckedRoots(t *testing.T) {
	r := Compile(`effect fn main() -> string {
    "ok"
}
`)
	if _, err := r.ApplicationPlan(GoGenerationTest); err == nil {
		t.Fatal("a source without tests has no test application root")
	}
	if _, err := r.ApplicationPlan(GoGenerationMode("library")); err == nil {
		t.Fatal("an unknown generation mode must be refused")
	}
	broken := Compile(`fn unreachable() -> string {
    true
}
effect fn main() -> string {
    "ok"
}
`)
	if broken.Checked {
		t.Fatal("unreachable declarations must still be fully checked")
	}
	if plan, err := broken.ApplicationPlan(GoGenerationBuild); err == nil || plan != nil {
		t.Fatalf("unchecked source produced a plan: %+v", plan)
	}
}

func TestApplicationPlanOmitsUnusedDeclarationsAndModules(t *testing.T) {
	r, plan := checkedApplicationPlan(t, `record Unused {
    name: string
}
enum Mode {
    On
    Off
}
error Broken
service Store {
    effect fn read() -> string raises { Broken }
}
impl Memory for Store {
    effect fn read() -> string raises { Broken } {
        fail Broken
    }
}
layer Stores {
    Store = Memory
}
fn helper(value: Unused) -> string {
    value.name
}
effect fn main() -> string {
    "ok"
}
`, GoGenerationBuild)
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "helper"))
	requireUnplanned(t, plan, RequiresDeclaration, dataIdentity("record", "Unused"), dataIdentity("enum", "Mode"), dataIdentity("error", "Broken"))
	requireUnplanned(t, plan, RequiresService, serviceIdentity("Store"))
	requireUnplanned(t, plan, RequiresProvider, providerIdentity(t, r, "Memory"))
	requireUnplanned(t, plan, RequiresLayer, layerID("Stores"))
	want := []ApplicationRequirement{
		{Kind: RequiresFunction, Identity: symbolIdentity(t, r, "main"), Reason: "entry"},
		{Kind: RequiresRuntimeModule, Identity: string(rt.RuntimeModuleCore), Reason: "native-entry"},
	}
	if !reflect.DeepEqual(plan.Requirements, want) {
		t.Fatalf("minimal closure = %+v", plan.Requirements)
	}
	sources, err := plan.RuntimeSources()
	if err != nil {
		t.Fatal(err)
	}
	core, err := rt.SelectSources(rt.RuntimeModuleCore)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(slices.Sorted(maps.Keys(sources)), slices.Sorted(maps.Keys(core))) {
		t.Fatalf("minimal runtime sources = %v", slices.Sorted(maps.Keys(sources)))
	}
	for _, unused := range []string{"layers.go", "latch.go", "http.go", "files.go", "interop.go", "console.go", "env.go", "inspect.go", "codec.go", "codec_json.go"} {
		if _, found := sources[unused]; found {
			t.Fatalf("minimal application selected %s", unused)
		}
	}
}

func TestApplicationPlanOmitsBundledMembersOnlyUsedByUnreachableCode(t *testing.T) {
	r, plan := checkedApplicationPlan(t, `import Fns "effra/functions"
effect fn keep(file: File) -> File {
    file
}
effect fn unreachable(file: File) -> File {
    run Fns.forwardFile(keep, file)
}
effect fn main() -> string {
    "ok"
}
`, GoGenerationBuild)
	if len(r.Program.BundledFunctions) != 1 {
		t.Fatalf("referenced bundled member was not checked: %d", len(r.Program.BundledFunctions))
	}
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "Fns.forwardFile"), symbolIdentity(t, r, "keep"), symbolIdentity(t, r, "unreachable"))
	requireUnplanned(t, plan, RequiresCallableValue, symbolIdentity(t, r, "keep"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore) {
		t.Fatalf("unreachable File code selected %v", plan.RuntimeModules())
	}

	r, plan = checkedApplicationPlan(t, `import Fns "effra/functions"
effect fn keep(file: File) -> File {
    file
}
effect fn main() -> string raises { IoError } {
    let file = run Files.openRead("missing").provide<Files>(LiveFiles)
    let same = run Fns.forwardFile(keep, file)
    run Files.readText(same).provide<Files>(LiveFiles)
}
`, GoGenerationBuild)
	requirePlanned(t, plan, RequiresFunction, symbolIdentity(t, r, "Fns.forwardFile"))
	requirePlanned(t, plan, RequiresCallableValue, symbolIdentity(t, r, "keep"))
	if !sameRuntimeModules(plan, rt.RuntimeModuleCore, rt.RuntimeModuleFiles) {
		t.Fatalf("reachable File code selected %v", plan.RuntimeModules())
	}
}

func TestApplicationPlanRetainsTemplatesOnlyWhenReachable(t *testing.T) {
	r, plan := checkedApplicationPlan(t, `import Data "effra/data"
fn unused() -> Data.Option<string> {
    Data.Option<string>.None {}
}
effect fn main() -> string {
    "ok"
}
`, GoGenerationBuild)
	templates := slices.Collect(maps.Keys(r.Program.semantic.templates))
	if len(templates) == 0 {
		t.Fatal("bundled template was not checked")
	}
	for _, template := range r.Program.semantic.templates {
		requireUnplanned(t, plan, RequiresDeclaration, template.Identity)
	}
	if len(plan.Identities(RequiresDeclaration)) != 0 {
		t.Fatalf("unreachable template retained: %v", plan.Identities(RequiresDeclaration))
	}

	r, plan = exampleApplicationPlan(t, "generic-users.ef", GoGenerationBuild)
	for _, template := range r.Program.semantic.templates {
		requirePlanned(t, plan, RequiresDeclaration, template.Identity)
	}
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", "User"), dataIdentity("record", "LookupProblem"))
}

func TestApplicationPlanIsDeterministicAndReadOnly(t *testing.T) {
	for _, tc := range []struct {
		file string
		mode GoGenerationMode
	}{
		{"main.ef", GoGenerationBuild},
		{"layers.ef", GoGenerationBuild},
		{"callables-state.ef", GoGenerationBuild},
		{"http.ef", GoGenerationBuild},
		{"generic-users.ef", GoGenerationBuild},
		{"testing.ef", GoGenerationTest},
	} {
		// r.canonical and the type arena hold pointers, so the oracle is a
		// detached deep rendering of the checked facts taken before any
		// planning, compared after the first and a repeated plan.
		r := exampleCompile(t, tc.file)
		facts := checkedFacts(r)
		first, err := r.ApplicationPlan(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		if checkedFacts(r) != facts {
			t.Fatalf("%s: the first plan mutated checked facts", tc.file)
		}
		again, err := r.ApplicationPlan(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		if checkedFacts(r) != facts {
			t.Fatalf("%s: a repeated plan mutated checked facts", tc.file)
		}
		_, fresh := exampleApplicationPlan(t, tc.file, tc.mode)
		if !reflect.DeepEqual(first, again) || !reflect.DeepEqual(first, fresh) {
			t.Fatalf("%s: plan is not deterministic", tc.file)
		}
		left, err := json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		right, err := json.Marshal(fresh)
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("%s: serialized plans differ", tc.file)
		}
		if !slices.IsSortedFunc(first.Requirements, compareApplicationRequirements) {
			t.Fatalf("%s: requirements are not in canonical order", tc.file)
		}
	}
}

// checkedFacts renders the canonical snapshot and type arena by value,
// following pointers, so a later mutation through any shared pointer shows.
func checkedFacts(r *Result) string {
	var b strings.Builder
	renderValue(&b, reflect.ValueOf(r.canonical), map[uintptr]int{})
	renderValue(&b, reflect.ValueOf(r.Program.semantic.typeNodes), map[uintptr]int{})
	return b.String()
}

func renderValue(b *strings.Builder, v reflect.Value, seen map[uintptr]int) {
	switch v.Kind() {
	case reflect.Invalid:
		b.WriteString("invalid")
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		if index, found := seen[v.Pointer()]; found {
			fmt.Fprintf(b, "&%d", index)
			return
		}
		seen[v.Pointer()] = len(seen)
		b.WriteString("&")
		renderValue(b, v.Elem(), seen)
	case reflect.Interface:
		renderValue(b, v.Elem(), seen)
	case reflect.Struct:
		b.WriteString("{")
		for i := 0; i < v.NumField(); i++ {
			b.WriteString(v.Type().Field(i).Name + ":")
			renderValue(b, v.Field(i), seen)
			b.WriteString(",")
		}
		b.WriteString("}")
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(b, "[%d:", v.Len())
		for i := 0; i < v.Len(); i++ {
			renderValue(b, v.Index(i), seen)
			b.WriteString(",")
		}
		b.WriteString("]")
	case reflect.Map:
		entries := make([]string, 0, v.Len())
		for iter := v.MapRange(); iter.Next(); {
			var entry strings.Builder
			renderValue(&entry, iter.Key(), map[uintptr]int{})
			entry.WriteString("=")
			renderValue(&entry, iter.Value(), map[uintptr]int{})
			entries = append(entries, entry.String())
		}
		slices.Sort(entries)
		fmt.Fprintf(b, "map%v", entries)
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		fmt.Fprintf(b, "%s(nil=%t)", v.Kind(), v.IsNil())
	case reflect.String:
		fmt.Fprintf(b, "%q", v.String())
	default:
		fmt.Fprintf(b, "%v", v)
	}
}

func applicationDAGSource(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&b, "record Node%d {\n    left: Node%d\n    right: Node%d\n}\n", i, i+1, i+1)
		fmt.Fprintf(&b, "fn step%d(input: string) -> string {\n    step%d(input) + step%d(input)\n}\n", i, i+1, i+1)
	}
	fmt.Fprintf(&b, "record Node%d {\n    label: string\n}\n", depth)
	fmt.Fprintf(&b, "fn step%d(input: string) -> string {\n    input\n}\n", depth)
	b.WriteString("fn shape(node: Node0) -> string {\n    \"shape\"\n}\n")
	b.WriteString("fn measure(shape: fn(Node0) -> string) -> string {\n    \"measured\"\n}\n")
	b.WriteString("effect fn main() -> string {\n    step0(\"x\") + measure(shape)\n}\n")
	return b.String()
}

func TestApplicationPlanExpandsSharedDAGsOnce(t *testing.T) {
	work := map[int]int{}
	for _, depth := range []int{24, 48} {
		r, plan := checkedApplicationPlan(t, applicationDAGSource(depth), GoGenerationBuild)
		for i := 0; i <= depth; i++ {
			requirePlanned(t, plan, RequiresFunction, symbolIdentity(t, r, fmt.Sprintf("step%d", i)))
			requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", fmt.Sprintf("Node%d", i)))
		}
		work[depth] = plan.Work
	}
	// Doubling a DAG whose path count is 2^depth must only double its work.
	if growth := work[48] - work[24]; growth > work[24]+64 {
		t.Fatalf("shared DAG work grew superlinearly: %v", work)
	}
}

func TestApplicationPlanRefusesExhaustedWork(t *testing.T) {
	r, plan := exampleApplicationPlan(t, "layers.ef", GoGenerationBuild)
	if plan.WorkLimit != maxApplicationPlanWork || plan.Work <= 0 || plan.Work > plan.WorkLimit {
		t.Fatalf("work accounting: %d/%d", plan.Work, plan.WorkLimit)
	}
	exact, err := r.applicationPlan(GoGenerationBuild, plan.Work)
	if err != nil || !reflect.DeepEqual(exact.Requirements, plan.Requirements) {
		t.Fatalf("exact budget must succeed: %v", err)
	}
	for _, limit := range []int{0, 1, plan.Work / 2, plan.Work - 1} {
		partial, err := r.applicationPlan(GoGenerationBuild, limit)
		var exhausted *ApplicationPlanError
		if partial != nil || !errors.As(err, &exhausted) || exhausted.Code != applicationPlanExhaustedCode {
			t.Fatalf("limit %d: plan %+v err %v", limit, partial, err)
		}
		if !strings.Contains(err.Error(), "EF136") || !strings.Contains(err.Error(), "work budget") {
			t.Fatalf("exhaustion diagnostic: %v", err)
		}
	}
}

// runtimeDeclarationModules maps every top-level runtime declaration to the
// catalog module that owns its file: the module with the smallest closure
// containing it.
func runtimeDeclarationModules(t *testing.T) map[string]rt.RuntimeModule {
	t.Helper()
	modules := []rt.RuntimeModule{rt.RuntimeModuleCore, rt.RuntimeModuleLayers, rt.RuntimeModuleSync, rt.RuntimeModuleHTTP, rt.RuntimeModuleFiles, rt.RuntimeModuleConsole, rt.RuntimeModuleEnv, rt.RuntimeModuleInspect, rt.RuntimeModuleInterop, rt.RuntimeModuleCodec}
	owners := map[string]rt.RuntimeModule{}
	closure := map[string]int{}
	for _, module := range modules {
		sources, err := rt.SelectSources(module)
		if err != nil {
			t.Fatal(err)
		}
		for name := range sources {
			if size, owned := closure[name]; !owned || len(sources) < size {
				owners[name], closure[name] = module, len(sources)
			}
		}
	}
	declarations := map[string]rt.RuntimeModule{}
	for name, source := range rt.Sources() {
		module, owned := owners[name]
		if !owned {
			t.Fatalf("runtime file %s is outside the module list", name)
		}
		file, err := goparser.ParseFile(gotoken.NewFileSet(), name, source, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil {
					declarations[declaration.Name.Name] = module
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						declarations[spec.Name.Name] = module
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							declarations[name.Name] = module
						}
					}
				}
			}
		}
	}
	return declarations
}

func runtimeReferenceModules(t *testing.T, declarations map[string]rt.RuntimeModule, node ast.Node) map[rt.RuntimeModule]bool {
	t.Helper()
	modules := map[rt.RuntimeModule]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "er" {
			module, declared := declarations[selector.Sel.Name]
			if !declared {
				t.Fatalf("emitted er.%s has no runtime declaration", selector.Sel.Name)
			}
			if module != rt.RuntimeModuleCore {
				modules[module] = true
			}
		}
		return true
	})
	return modules
}

func nonCoreModules(modules ...rt.RuntimeModule) map[rt.RuntimeModule]bool {
	out := map[rt.RuntimeModule]bool{}
	for _, module := range modules {
		if module != rt.RuntimeModuleCore {
			out[module] = true
		}
	}
	return out
}

// The builtin catalog's native metadata is the plan's only knowledge of
// builtin implementations, so it must name exactly the non-core runtime
// modules the emitted declarations reference.
func TestBuiltinNativeModulesMatchEmittedRuntimeReferences(t *testing.T) {
	// Each builtin is emitted only when retained, so parse every builtin's own
	// emitted declaration rather than one program's selection.
	var generated strings.Builder
	generated.WriteString("package main\n")
	for _, service := range builtins() {
		generated.WriteString(goServiceDeclaration(&Program{references: map[string]bool{"Http": true}}, service))
	}
	for _, provider := range builtinProviders() {
		implementation, found := builtinGoProviders[provider.Name]
		if !found {
			t.Fatalf("builtin provider %s has no native implementation", provider.Name)
		}
		generated.WriteString(implementation)
	}
	if len(builtinGoProviders) != len(builtinProviders()) {
		t.Fatalf("native implementations %d do not match builtin providers %d", len(builtinGoProviders), len(builtinProviders()))
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", generated.String(), goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	declarations := runtimeDeclarationModules(t)
	emitted := map[string]map[rt.RuntimeModule]bool{}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			emitted[declaration.Name.Name] = runtimeReferenceModules(t, declarations, declaration)
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				if spec, ok := spec.(*ast.TypeSpec); ok {
					emitted[spec.Name.Name] = runtimeReferenceModules(t, declarations, spec)
				}
			}
		}
	}
	for _, service := range builtins() {
		got := map[rt.RuntimeModule]bool{}
		names := []string{"efService_" + goIdent(service.Name), "efProvide_" + goIdent(service.Name)}
		for _, operation := range service.Methods {
			names = append(names, "efCall_"+service.Name+"_"+operation.Name)
		}
		for _, name := range names {
			modules, found := emitted[name]
			if !found {
				t.Fatalf("builtin service declaration %s was not emitted", name)
			}
			maps.Copy(got, modules)
		}
		if want := nonCoreModules(service.native...); !maps.Equal(got, want) {
			t.Errorf("service %s native modules = %v, emitted references %v", service.Name, want, got)
		}
	}
	for _, provider := range builtinProviders() {
		got, found := emitted["efProvider_"+provider.Name]
		if !found {
			t.Fatalf("builtin provider %s was not emitted", provider.Name)
		}
		if want := nonCoreModules(provider.native...); !maps.Equal(got, want) {
			t.Errorf("provider %s native modules = %v, emitted references %v", provider.Name, want, got)
		}
	}
}

// Every canonical node's emitted Go type may reference only non-core modules
// the plan selects for that node and its wrapped arguments, and every module
// the plan attributes to a node itself must appear in its emitted type.
// Recipe parameters are walked although an applied recipe emits only its
// result; their declared signature retains the same modules anyway.
func TestTypeRuntimeModulesMatchCanonicalGoTypes(t *testing.T) {
	r := Compile(`import go strconv "strconv"
effect fn parse(text: string) -> bool raises { GoError } uses { Foreign } {
    run strconv.ParseBool(text).orFail()
}
effect fn open(path: string) -> File raises { IoError } uses { Files } {
    run Files.openRead(path)
}
effect fn forward(callback: effect fn(File) -> File, file: File) -> File {
    run callback(file)
}
effect fn signal(latch: Latch) -> void uses { Sync } {
    run Sync.signal(latch)
}
effect fn main() -> string {
    scope {
        let child = fork parse("true").provide<Foreign>(Host).catch<GoError>(false)
        let parsed = run child.join()
        "ok"
    }
}
`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	declarations := runtimeDeclarationModules(t)
	c := r.Program.semantic
	var closure func(TypeID, map[TypeID]bool, map[rt.RuntimeModule]bool)
	closure = func(id TypeID, seen map[TypeID]bool, modules map[rt.RuntimeModule]bool) {
		node := c.node(id)
		if node == nil || seen[id] {
			return
		}
		seen[id] = true
		if module, ok := typeRuntimeModule(node); ok {
			modules[module] = true
		}
		switch node.Kind {
		case "record", "enum", "error", "application":
			// Nominal data emits a named Go type; its fields are expanded from
			// the retained declaration, not from this node.
			if node.Kind != "application" {
				return
			}
		}
		for _, argument := range node.Args {
			closure(argument, seen, modules)
		}
		closure(node.Result, seen, modules)
	}
	kinds := map[string]bool{}
	for index := range c.typeNodes {
		id := TypeID(index + 1)
		node := c.node(id)
		rendered := canonicalGoType(c, id, map[TypeID]bool{})
		expression, err := goparser.ParseExpr(rendered)
		if err != nil {
			t.Fatalf("type %d (%s) rendered %q: %v", id, node.Kind, rendered, err)
		}
		got := runtimeReferenceModules(t, declarations, expression)
		want := map[rt.RuntimeModule]bool{}
		closure(id, map[TypeID]bool{}, want)
		for module := range got {
			if !want[module] {
				t.Errorf("type %d (%s %s) renders %s with module %s the plan does not select: %v", id, node.Kind, node.Name, rendered, module, want)
			}
		}
		if module, ok := typeRuntimeModule(node); ok && !got[module] {
			t.Errorf("type %d (%s %s) renders %s without its planned module %s", id, node.Kind, node.Name, rendered, module)
		}
		if len(got) > 0 {
			kinds[node.Kind] = true
		}
	}
	for _, kind := range []string{"opaque", "goResult", "callable"} {
		if !kinds[kind] {
			t.Errorf("fixture has no %s node referencing a native module: %v", kind, kinds)
		}
	}
}
