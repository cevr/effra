package compiler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const bundledGreeting = `import Fns "effra/functions"
error MissingUser
service Users { effect fn name(id: string) -> string raises {MissingUser} }
impl Memory for Users {
 effect fn name(id: string) -> string raises {MissingUser} { "Ada" }
}
effect fn loadName(id: string) -> string raises {MissingUser} uses {Users} { run Users.name(id) }
effect fn greeting(id: string) -> string raises {MissingUser} uses {Users} { "Hello, " + (run Fns.call(loadName, id)) }
effect fn main() -> string { run greeting("42").provide<Users>(Memory).catch<MissingUser>("missing") }
`

const bundledConfiguration = `import Fns "effra/functions"
error MissingSetting
service Settings { effect fn read(key: string) -> string raises {MissingSetting} }
impl Fixture for Settings {
 effect fn read(key: string) -> string raises {MissingSetting} { "configured:" + key }
}
effect fn loadSetting(key: string) -> string raises {MissingSetting} uses {Settings} { run Settings.read(key) }
effect fn configuration(key: string) -> string raises {MissingSetting} uses {Settings} { run Fns.call(loadSetting, key) }
effect fn main() -> string { run configuration("port").provide<Settings>(Fixture).catch<MissingSetting>("missing") }
`

func TestBundledForwardingContracts(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(bundledGreeting, target)
		if !r.Checked {
			t.Fatalf("%s: %v", target, r.Diagnostics)
		}
		greeting := r.Find("greeting")
		if greeting == nil || strings.Join(greeting.Actual.Errors, ",") != "MissingUser" || strings.Join(greeting.Actual.Services, ",") != "Users" {
			t.Fatal(greeting)
		}
		if len(r.BundledBindings) != 1 || r.BundledBindings[0].Name != "call" || len(r.Program.BundledFunctions) != 1 {
			t.Fatal("unused sibling loaded", r.BundledBindings)
		}
		f := r.Find("Fns.call")
		if f == nil || f.Source != "source:effra/functions/call" || !strings.Contains(f.Identity, "effra/functions") {
			t.Fatal("missing qualified source", f)
		}
		if f.Span.Offset != strings.Index("effect fn call", "call") {
			t.Fatal("bundled offset shifted", f.Span)
		}
		query, err := r.TypeAt(strings.Index(bundledGreeting, "Fns.call") + len("Fns."))
		if err != nil || query.Type.Success != "string" || query.Type.Application.Callee != f.Identity {
			t.Fatal(query, err)
		}
		projection := r.CheckResponse()
		if projection["typeProjectionComplete"] != true || projection["producerIdentity"] != SemanticProducerIdentity {
			t.Fatal(projection)
		}
		wire, err := json.Marshal(projection)
		if err != nil || !strings.Contains(string(wire), "bundledBindings") {
			t.Fatal(err)
		}
		graph, err := r.Graph()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, edge := range graph.Edges {
			found = found || edge.To == "function:effra/functions.call" && edge.Kind == "calls"
		}
		if !found {
			t.Fatal("imported dependency missing", graph.Edges)
		}
	}
}

func TestBundledAliasIdentityAndSourceIsolation(t *testing.T) {
	a := Compile(bundledGreeting)
	b := Compile(strings.ReplaceAll(bundledGreeting, "Fns", "Other"))
	if !a.Checked || !b.Checked || a.Find("Fns.call").Identity != b.Find("Other.call").Identity || a.Find("Fns.call").Contract.Contract.ID != b.Find("Other.call").Contract.Contract.ID {
		t.Fatal(a.Diagnostics, b.Diagnostics)
	}
	source := `import A "effra/functions" import B "effra/functions"
fn call(value: string) -> string { "local:" + value }
effect fn echo(value: string) -> string { value }
effect fn main() -> string { let identity = B.identity; identity(run A.call(echo, call("ok"))) }`
	r := Compile(source)
	if !r.Checked || len(r.Program.BundledFunctions) != 2 {
		t.Fatal(r.Diagnostics, r.BundledBindings)
	}
	goSource, err := r.EmitGo()
	if err != nil || !strings.Contains(goSource, "efFunction_call") || !strings.Contains(goSource, "efBundledFunction_bundle_") {
		t.Fatal(err)
	}
	js, _, err := CompileFor(source, "js").Emit(true)
	if err != nil || strings.Contains(js, "as identity") || strings.Contains(js, "as call") && !strings.Contains(js, "__ef_function_call as call") {
		t.Fatal(err, js)
	}
	formatted, err := FormatSource(source)
	if err != nil || !Compile(formatted.Text).Checked {
		t.Fatal(err, formatted.Text)
	}
}

func TestBundledRefusesMissingRowsAndUnsupportedImports(t *testing.T) {
	for _, mutation := range []struct{ before, after, code string }{
		{`effect fn greeting(id: string) -> string raises {MissingUser} uses {Users}`, `effect fn greeting(id: string) -> string uses {Users}`, "EF107"},
		{`effect fn greeting(id: string) -> string raises {MissingUser} uses {Users}`, `effect fn greeting(id: string) -> string raises {MissingUser}`, "EF108"},
		{`"effra/functions"`, `"./functions"`, "EF126"},
		{`"effra/functions"`, `"effra/functions@2"`, "EF126"},
		{`Fns.call(loadName, id)`, `Fns.missing(loadName, id)`, "EF126"},
		{`import Fns "effra/functions"`, `import Fns "effra/functions" fn Fns() -> () { () }`, "EF101"},
	} {
		for _, target := range []string{"go", "js"} {
			r := CompileFor(strings.Replace(bundledGreeting, mutation.before, mutation.after, 1), target)
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == mutation.code
			}
			if r.Checked || !found {
				t.Fatalf("%s %s: %v", target, mutation.code, r.Diagnostics)
			}
		}
	}
	unused := Compile(`import Fns "effra/functions" effect fn main() -> string { "ok" }`)
	if !unused.Checked || len(unused.BundledBindings) != 0 || len(unused.Program.BundledFunctions) != 0 {
		t.Fatal(unused.Diagnostics)
	}
	shadow := Compile(`import Fns "effra/functions" record Local { identity: fn(string) -> string } fn plain(input: string) -> string { input } effect fn main() -> string { let Fns = Local(identity: plain); Fns.identity("local") }`)
	if !shadow.Checked || len(shadow.BundledBindings) != 0 {
		t.Fatal(shadow.Diagnostics, shadow.BundledBindings)
	}
}

func TestBundledJSExecution(t *testing.T) {
	if output := runJS(t, bundledGreeting, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "Hello, Ada\n" {
		t.Fatal(output)
	}
	if output := runJS(t, bundledConfiguration, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "configured:port\n" {
		t.Fatal(output)
	}
}

func TestBundledEmissionNamespaceCannotCollideWithUserNames(t *testing.T) {
	baseline := Compile(bundledGreeting)
	name := baseline.Program.BundledFunctions[0].EmissionName
	source := bundledGreeting + "\nfn " + name + "() -> string { \"user\" }\n"
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	goSource, err := r.EmitGo()
	if err != nil || strings.Count(goSource, "func efFunction_"+name+"(") != 1 {
		t.Fatalf("user/bundle native names collided: %v", err)
	}
	js, _, err := CompileFor(source, "js").Emit(false)
	if err != nil || strings.Count(js, "const __ef_function_"+name+" =") != 1 {
		t.Fatalf("user/bundle JS names collided: %v", err)
	}
}

func TestBundledNativeExecution(t *testing.T) {
	for _, fixture := range []struct{ source, output string }{{bundledGreeting, "Hello, Ada\n"}, {bundledConfiguration, "configured:port\n"}, {`import Fns "effra/functions" effect fn keep(file:File)->File{file} effect fn outer(file:File)->File{scope {run Fns.forwardFile(keep,file)}} effect fn main()->string{"managed ready"}`, "managed ready\n"}} {
		r := Compile(fixture.source)
		source, err := r.EmitGo()
		if err != nil {
			t.Fatal(err, r.Diagnostics)
		}
		dir := t.TempDir()
		if err = WriteRuntime(dir); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		binary := filepath.Join(dir, "native")
		if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
			t.Fatalf("%v\n%s", err, output)
		}
		if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != fixture.output {
			t.Fatalf("%v\n%s", err, output)
		}
	}
}
