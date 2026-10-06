package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const providerConstructionProbe = `
service Names { effect fn get(id: string) -> string }
service Greeting { effect fn hello(id: string) -> string }
impl NamesA for Names { effect fn get(id: string) -> string { "A" } }
impl NamesB for Names { effect fn get(id: string) -> string { "B" } }
impl Prefixed(prefix: string) for Greeting uses {Names} {
 effect fn hello(id: string) -> string uses {Names} {
  let name = run Names.get(id)
  prefix + name
 }
}
effect fn main() -> string {
 let greeting = run Prefixed("A: ").provide<Names>(NamesA)
 run Greeting.hello("42").provide<Names>(NamesB).provide<Greeting>(greeting)
}
`

func TestProviderConstructionHasDistinctContract(t *testing.T) {
	r := Compile(providerConstructionProbe)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	var prefixed *Provider
	for _, provider := range r.Program.Providers {
		if provider.Name == "Prefixed" {
			prefixed = provider
		}
	}
	if prefixed == nil {
		t.Fatal("missing Prefixed provider")
	}
	constructor := providerContract(prefixed)
	if constructor.Success != "provider:Greeting" || !constructor.Effect || !slicesEqual(constructor.Services, []string{"Names"}) {
		t.Fatalf("constructor contract: %+v", constructor)
	}
	main := r.Find("main")
	if main == nil || len(main.Contract.Services) != 0 {
		t.Fatalf("main contract should discharge the constructor requirement: %+v", main)
	}
}

func TestProviderCaptureAgreesAcrossGoAndJS(t *testing.T) {
	r := Compile(providerConstructionProbe)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
		"provider_test.go": []byte(`package main
import ("testing"; "fmt"; er "effra.generated/runtime")

func TestProviderInvocationUsesCurrentOwnerAndCancellation(t *testing.T) {
  out := er.Run(func(construction *er.FiberContext) er.Exit[struct{}] {
    started := make(chan struct{})
    var invocation *er.FiberContext
    names := efService_Names{m_get: func(string) efEffect[string] { return func(ctx efContext) efExit[string] {
      invocation = ctx.Runtime
      close(started)
      <-ctx.Runtime.Context().Done()
      return er.Interrupt[string](ctx.Runtime.Context().Err())
    }}}
    built := efProvider_Prefixed("A: ")(efContext{Runtime: construction, s_Names: &names})
    if built.IsFailure() { return er.Propagate[struct{}](built) }
    pending := efProvide_Greeting(efCall_Greeting_hello("42"), built.Value)
    child := efFork(pending)(efContext{Runtime: construction})
    if child.IsFailure() { return er.Propagate[struct{}](child) }
    <-started
    interrupted := efInterrupt(child.Value)(efContext{Runtime: construction})
    if interrupted.IsFailure() { return interrupted }
    if invocation == nil || invocation == construction || invocation.Scope() == construction.Scope() {
      return er.Die[struct{}](fmt.Errorf("provider retained construction runtime or scope"))
    }
    return er.Succeed(struct{}{})
  })
  if out.IsFailure() { t.Fatal(out.Cause()) }
}
`),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runGoCommand(dir, "test", "-race", ".")
	if err != nil {
		t.Fatalf("Go provider capture/cancellation: %v %s\n%s", err, output, goSource)
	}
	binary := filepath.Join(dir, "native")
	if output, err = runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("Go provider native build: %v %s", err, output)
	}
	output, err = exec.Command(binary).CombinedOutput()
	if err != nil || string(output) != "A: A\n" {
		t.Fatalf("Go provider executable: %v %q\n%s", err, output, goSource)
	}

	outputJS := runJS(t, providerConstructionProbe, `
const value = await Effect.runPromise(__ef_function_main());
if (value !== "A: A") throw new Error("provider capture: " + value);
let startedResolve;
const started = new Promise(resolve => startedResolve = resolve);
let constructionOwner;
let invocationOwner;
const blockingNames = {
  get: () => Effect.gen(function* () {
    invocationOwner = yield* __ef_owner;
    startedResolve();
    yield* Effect.never;
  })
};
await Effect.runPromise(__ef_scoped(Effect.gen(function* () {
  constructionOwner = yield* __ef_owner;
  const built = yield* Effect.provideService(__ef_provider_Prefixed("A: "), __ef_service_Names, blockingNames);
  const pending = Effect.provideService(__ef_call(__ef_service_Greeting, "hello", ["42"]), __ef_service_Greeting, built);
  const child = yield* __ef_fork(pending);
  yield* Effect.promise(() => started);
  yield* __ef_interrupt(child);
})));
if (!invocationOwner || invocationOwner === constructionOwner) throw new Error("provider retained construction owner");
console.log(value);
`)
	if outputJS != "A: A\n" {
		t.Fatalf("JS provider capture: %q", outputJS)
	}
}

func TestProviderTypeScriptShapeHidesCapturedRequirements(t *testing.T) {
	r := CompileFor(providerConstructionProbe, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	constructor := `declare const __ef_provider_Prefixed: (arg_prefix: string) => Effect.Effect<GreetingProvider, never, NamesRequirement>;`
	if !strings.Contains(declarations, constructor) {
		t.Fatalf("constructor declaration lost construction row:\n%s", declarations)
	}
	if strings.Contains(declarations, `readonly "hello": (arg_id: string) => Effect.Effect<string, never, NamesRequirement>`) {
		t.Fatalf("captured requirement leaked from provider method shape:\n%s", declarations)
	}
	if !strings.Contains(js, `Effect.provideService(__ef_autoScope`) || !strings.Contains(js, `__ef_service_Names`) {
		t.Fatalf("JS provider did not overlay captured service key:\n%s", js)
	}
	if strings.Contains(js, `Effect.provide(__ef_provider_Prefixed`) {
		t.Fatalf("JS provider replayed an entire context")
	}
}

func TestProviderGraphShowsConstructionAndProvisionEdges(t *testing.T) {
	r := Compile(providerConstructionProbe)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	var prefixed GraphNode
	found := false
	for _, node := range graph.Nodes {
		if node.ID == "provider:Prefixed" {
			prefixed = node
			found = true
		}
	}
	if !found || prefixed.Contract == nil || !slicesEqual(prefixed.Contract.Services, []string{"Names"}) {
		t.Fatalf("missing constructor contract in graph: %+v", prefixed)
	}
	want := map[string]bool{
		"provider:Prefixed->service:Names:requires": false,
	}
	for _, edge := range graph.Edges {
		key := edge.From + "->" + edge.To + ":" + edge.Kind
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for edge, present := range want {
		if !present {
			t.Fatalf("missing graph edge %s", edge)
		}
	}
	provision := false
	for _, edge := range graph.Edges {
		if edge.Kind == "provides" && edge.Service == "Greeting" {
			provision = true
			break
		}
	}
	if !provision {
		t.Fatal("missing provider value provision edge")
	}
}

func TestProviderGraphPreservesExplicitValueProvenance(t *testing.T) {
	source := `service Names { effect fn get(id: string) -> string }
service Greeting { effect fn hello(id: string) -> string }
impl NamesFixture for Names { effect fn get(id: string) -> string { id } }
impl Prefixed(prefix: string) for Greeting uses {Names} {
 effect fn hello(id: string) -> string { let name = run Names.get(id) prefix + name }
}
effect fn main() -> string {
 let shared = run Prefixed("shared:").provide<Names>(NamesFixture)
 let alias = shared
 let one = run Greeting.hello("one").provide<Greeting>(shared)
 let two = run Greeting.hello("two").provide<Greeting>(alias)
 let fresh = run Prefixed("fresh:").provide<Names>(NamesFixture)
 let three = run Greeting.hello("three").provide<Greeting>(fresh)
 one + two + three
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	providerValues := map[string]GraphNode{}
	for _, node := range graph.Nodes {
		if node.Kind == "provider-value" {
			providerValues[node.ID] = node
		}
	}
	if len(providerValues) != 2 {
		t.Fatalf("expected one node per explicit constructor call, got %d: %+v", len(providerValues), providerValues)
	}
	counts := map[string]int{}
	for _, edge := range graph.Edges {
		if edge.Kind == "provides" && edge.Service == "Greeting" {
			counts[edge.To]++
		}
	}
	if len(counts) != 2 {
		t.Fatalf("expected two provider-value identities, got %v", counts)
	}
	var sharedID string
	for id, count := range counts {
		if count == 2 {
			sharedID = id
		}
	}
	if sharedID == "" || len(providerValues[sharedID].Incoming) < 3 {
		t.Fatalf("shared provider value lost incoming dependents: %q %+v", sharedID, providerValues[sharedID])
	}
}

func TestProviderConfigurationAcceptsTypedRecordData(t *testing.T) {
	source := `record PrefixConfig { prefix: string }
service Names { effect fn get(id: string) -> string }
service Greeting { effect fn hello(id: string) -> string }
impl NamesFixture for Names { effect fn get(id: string) -> string { id } }
impl Configured(config: PrefixConfig) for Greeting uses {Names} {
 effect fn hello(id: string) -> string { let name = run Names.get(id) config.prefix + name }
}
effect fn main() -> string {
 let config = PrefixConfig { prefix: "cfg: " }
 let greeting = run Configured(config).provide<Names>(NamesFixture)
 run Greeting.hello("42").provide<Greeting>(greeting)
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runJS(t, source, `if (await Effect.runPromise(__ef_function_main()) !== "cfg: 42") throw new Error("wrong record configuration");`); output != "" {
		t.Fatalf("unexpected JS output: %s", output)
	}
	goSource, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(r.ModuleFile()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "record-config")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("record configuration Go build: %v\n%s\n%s", err, output, goSource)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != "cfg: 42\n" {
		t.Fatalf("record configuration Go run: %v\n%s", err, output)
	}
}

func TestProviderConstructionRejectsMissingConfigAndCapture(t *testing.T) {
	cases := []struct {
		name   string
		source string
		code   string
	}{
		{
			"wrong configuration type",
			`service Names { effect fn get(id: string) -> string } service Greeting { effect fn hello(id: string) -> string } impl Prefixed(prefix: string) for Greeting uses {Names} { effect fn hello(id: string) -> string { let name = run Names.get(id) prefix + name } } effect fn main() -> string uses {Names} { run Prefixed(true) }`,
			"EF106",
		},
		{
			"missing captured service",
			`service Names { effect fn get(id: string) -> string } service Greeting { effect fn hello(id: string) -> string } impl Prefixed(prefix: string) for Greeting uses {Names} { effect fn hello(id: string) -> string { let name = run Names.get(id) prefix + name } } effect fn main() -> string { let greeting = run Prefixed("A: "); run Greeting.hello("42").provide<Greeting>(greeting) }`,
			"EF108",
		},
		{
			"undeclared captured service",
			`service Names { effect fn get(id: string) -> string } service Other { effect fn get(id: string) -> string } service Greeting { effect fn hello(id: string) -> string } impl Prefixed(prefix: string) for Greeting uses {Names} { effect fn hello(id: string) -> string uses {Other} { let name = run Other.get(id) prefix + name } } effect fn main() -> string { () }`,
			"EF103",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if r.Checked || !hasCode(r, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, r.Diagnostics)
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
