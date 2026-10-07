package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFiberHandleIsNotAnOrdinaryResultArgument(t *testing.T) {
	source := `effect fn task() -> string { "hello" }
fn identity(text: string) -> string { text }
effect fn main() -> string { let child = fork task() identity(child) }`
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF106") {
		t.Fatalf("fiber handle was admitted as string argument: %+v", r.Diagnostics)
	}
}

func TestFiberOperationsConsumeOnlyTheirCanonicalCarriedRows(t *testing.T) {
	source := `error A
effect fn task() -> string raises {A} uses {Console} { "ok" }
effect fn main() -> void raises {A} uses {Console} {
 let a = fork task()
 let joined = run a.join()
 run a.interrupt()
 run a.cancel()
}`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("fiber operation fixture did not check: %+v", r.Diagnostics)
	}
	for _, tc := range []struct {
		marker      string
		failures    []string
		requirement []string
	}{
		{marker: "fork task", failures: []string{"A"}, requirement: []string{"Console"}},
		{marker: "run a.join", failures: []string{"A"}, requirement: nil},
		{marker: "run a.interrupt", failures: []string{"A"}, requirement: nil},
		{marker: "run a.cancel", failures: nil, requirement: nil},
	} {
		offset := strings.Index(source, tc.marker)
		if offset < 0 {
			t.Fatalf("missing fixture marker %q", tc.marker)
		}
		info, err := r.TypeAt(offset)
		if err != nil {
			t.Fatalf("%s: %v", tc.marker, err)
		}
		if !slices.Equal(info.ExecutedFailures, tc.failures) || !slices.Equal(info.ExecutedRequirements, tc.requirement) {
			t.Fatalf("%s consumed the wrong rows: failures=%v requirements=%v", tc.marker, info.ExecutedFailures, info.ExecutedRequirements)
		}
	}
}

func TestFiberBranchAndMatchEmitAndRunWithFiberShape(t *testing.T) {
	sources := []string{
		`effect fn task() -> string { "hello" }
effect fn main() -> string {
 let child = fork task()
 let selected = if true { child } else { child }
 run selected.join()
}`,
		`error A error B enum Choice { Left Right }
effect fn taskA() -> string raises {A} { "hello" }
effect fn taskB() -> string raises {B} { "world" }
effect fn main() -> string raises {A, B} {
	 let choice = Choice.Left()
	 let selected = match choice { Choice.Left => fork taskA() Choice.Right => fork taskB() }
	 run selected.join()
}`,
	}
	for i, source := range sources {
		t.Run(fmtInt(i), func(t *testing.T) {
			goResult := CompileFor(source, "go")
			if !goResult.Checked {
				t.Fatalf("Go fiber branch did not check: %+v", goResult.Diagnostics)
			}
			info, err := goResult.TypeAt(strings.Index(source, "fork"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Type.Success != "Fiber:string" || info.Type.Type.Kind != "fiber" {
				t.Fatalf("fiber projection lost the handle shape: %+v", info.Type)
			}
			goSource, application, err := emitGoApplication(goResult, GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			goDir := t.TempDir()
			if err := application.WriteRuntime(goDir); err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string][]byte{
				"go.mod":  []byte(goResult.ModuleFile()),
				"main.go": []byte(goSource),
			} {
				if err := os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			goOutput, err := runWithWatchdog(buildGoModule(t, goDir))
			if err != nil || string(goOutput) != "hello\n" {
				t.Fatalf("generated Go fiber branch: %v\n%s", err, goOutput)
			}

			jsResult := CompileFor(source, "js")
			if !jsResult.Checked {
				t.Fatalf("JS fiber branch did not check: %+v", jsResult.Diagnostics)
			}
			jsSource, _, err := jsResult.Emit(true)
			if err != nil {
				t.Fatal(err)
			}
			bun, err := exec.LookPath("bun")
			if err != nil {
				t.Fatal("Bun is required for generated fiber tests")
			}
			root := filepath.Join("..", "..")
			if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
				t.Fatal(err)
			}
			jsDir, err := os.MkdirTemp(filepath.Join(root, "dist"), "fiber-shape-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(jsDir) })
			jsPath := filepath.Join(jsDir, "fiber.mjs")
			if err := os.WriteFile(jsPath, []byte(jsSource), 0600); err != nil {
				t.Fatal(err)
			}
			jsOutput, err := exec.Command(bun, jsPath).CombinedOutput()
			if err != nil || string(jsOutput) != "hello\n" {
				t.Fatalf("generated JS fiber branch: %v\n%s", err, jsOutput)
			}
		})
	}
}

func TestHandlerBoundaryKeepsCallableRowsAndUsesNarrowCompatibility(t *testing.T) {
	base := `error NotFound
service Users { effect fn get(id: string) -> string raises {NotFound} }
impl TestUsers for Users { effect fn get(id: string) -> string { id } }
effect fn route(request: HttpRequest) -> HttpReply raises {NotFound} uses {Users} {
 let name = run Users.get(request.path)
 HttpReply.NotFound {}
}
fn limits() -> HttpLimits { HttpLimits { maxBodyBytes: 0, readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 1 } }
`
	for _, tc := range []struct {
		name        string
		suffix      string
		wantOK      bool
		wantEF108   bool
		wantEF107   bool
		wantMessage string
	}{
		{name: "direct missing all services", suffix: `effect fn main() -> void raises {IoError} { run Http.listen("127.0.0.1:0", limits(), route) }`, wantEF108: true, wantMessage: "missing service requirements: Http, Users"},
		{name: "local missing all services", suffix: `effect fn main() -> void raises {IoError} { let h = route run Http.listen("127.0.0.1:0", limits(), h) }`, wantEF108: true, wantMessage: "missing service requirements: Http, Users"},
		{name: "provided direct", suffix: `effect fn main() -> void raises {IoError} { run Http.listen("127.0.0.1:0", limits(), route).provide<Http>(LiveHttp).provide<Users>(TestUsers) }`, wantOK: true},
		{name: "missing handler failure", suffix: `effect fn main() -> HttpReply {
 let body = run Http.text("").provide<Http>(LiveHttp)
 run route(HttpRequest { method: "GET", path: "/", contentType: "", body: body }).provide<Users>(TestUsers)
}`, wantEF107: true, wantMessage: "undeclared failures: NotFound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(base + tc.suffix)
			if r.Checked != tc.wantOK {
				t.Fatalf("checked=%t want=%t diagnostics=%+v", r.Checked, tc.wantOK, r.Diagnostics)
			}
			if tc.wantEF108 && !hasCode(r, "EF108") {
				t.Fatalf("missing Users/Http requirement was not diagnosed: %+v", r.Diagnostics)
			}
			if tc.wantEF107 && !hasCode(r, "EF107") {
				t.Fatalf("missing handler failure was not diagnosed: %+v", r.Diagnostics)
			}
			if tc.wantMessage != "" && !hasDiagnosticMessage(r, tc.wantMessage) {
				t.Fatalf("diagnostics did not retain %q: %+v", tc.wantMessage, r.Diagnostics)
			}
			if !tc.wantOK {
				return
			}
			route := r.Find("route")
			if route == nil || route.Contract.Callable == nil || route.Contract.Callable.Result.Name != "HttpReply" {
				t.Fatalf("handler declaration was projected as an opaque result: %+v", route)
			}
			offset := strings.Index(base+tc.suffix, "route).provide")
			info, err := r.TypeAt(offset)
			if err != nil {
				t.Fatal(err)
			}
			if info.Type.Callable == nil || info.Type.Callable.Result.Name != "HttpReply" {
				t.Fatalf("handler expression lost its source return type: %+v", info.Type)
			}
			if info.Type.Contract.ID != route.Contract.Contract.ID {
				t.Fatalf("handler value did not retain the declaration contract: expression=%q declaration=%q", info.Type.Contract.ID, route.Contract.Contract.ID)
			}
		})
	}
}

func TestCallableValueControlFlowUsesCanonicalGoValueTypes(t *testing.T) {
	base := `effect fn route(path: string) -> string { path }
effect fn consume(handler: effect fn(string) -> string) -> string { run handler("/") }
`
	for _, tc := range []struct {
		name   string
		body   string
		needle string
	}{
		{
			name: "if",
			body: `effect fn main() -> string {
 let selected = if true { route } else { route }
 run consume(selected)
}`,
			needle: "efExit[func(string) efEffect[string]]",
		},
		{
			name: "scope",
			body: `effect fn main() -> string {
 let selected = scope { route }
 run consume(selected)
}`,
			needle: "efScoped(func(ctx efContext) efExit[func(string) efEffect[string]]",
		},
		{
			name: "match",
			body: `enum Choice { Left Right }
effect fn main() -> string {
 let selected = match Choice.Left() { Choice.Left => route Choice.Right => route }
 run consume(selected)
}`,
			needle: "efExit[func(string) efEffect[string]]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(base + tc.body)
			if !r.Checked {
				t.Fatalf("callable %s control flow did not check: %+v", tc.name, r.Diagnostics)
			}
			goSource, application, err := emitGoApplication(r, GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(goSource, tc.needle) {
				t.Fatalf("%s lowering did not render the canonical callable value type %q:\n%s", tc.name, tc.needle, goSource)
			}
			goDir := t.TempDir()
			if err := application.WriteRuntime(goDir); err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string][]byte{
				"go.mod":  []byte(r.ModuleFile()),
				"main.go": []byte(goSource),
			} {
				if err := os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			buildGoModule(t, goDir)
		})
	}
}

func TestIfConditionUsesInnerValueTypeForNativePropagation(t *testing.T) {
	source := `error A
effect fn condition() -> bool raises {A} { true }
effect fn main() -> string raises {A} {
 let number = if run condition() { 1 } else { 2 }
 "ok"
}`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatalf("typed if condition fixture did not check: %+v", r.Diagnostics)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSource, "er.Propagate[int64]") {
		t.Fatalf("condition failure was not lowered with the inner if value type:\n%s", goSource)
	}
	goDir := t.TempDir()
	if err := application.WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
	} {
		if err := os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runWithWatchdog(buildGoModule(t, goDir))
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("generated Go if condition: %v\n%s", err, output)
	}
}

func TestIfConditionFailureRecoversAfterTypedNativePropagation(t *testing.T) {
	source := `error A
effect fn condition() -> bool raises {A} { fail A }
effect fn program() -> string raises {A} {
 let number = if run condition() { 1 } else { 2 }
 "unreached"
}
effect fn main() -> string {
 run program().catch<A>("caught")
}`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatalf("typed if condition failure fixture did not check: %+v", r.Diagnostics)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goSource, "er.Propagate[int64]") || !strings.Contains(goSource, "er.Propagate[string]") {
		t.Fatalf("native condition and enclosing expression did not keep distinct propagation types:\n%s", goSource)
	}
	goDir := t.TempDir()
	if err := application.WriteRuntime(goDir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"go.mod":  []byte(r.ModuleFile()),
		"main.go": []byte(goSource),
	} {
		if err := os.WriteFile(filepath.Join(goDir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runWithWatchdog(buildGoModule(t, goDir))
	if err != nil || string(output) != "caught\n" {
		t.Fatalf("generated Go typed if recovery: %v\n%s", err, output)
	}
}

func hasDiagnosticMessage(r *Result, message string) bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Message == message {
			return true
		}
	}
	return false
}
