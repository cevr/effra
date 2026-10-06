package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// This source boundary protects ordinary named callback composition. The base
// compiler rejects the function-type grammar and cannot call local values;
// existing Handler tests cover only a special HTTP shape, not this contract.
func TestOrdinaryPureCallbackComposition(t *testing.T) {
	source := `
record State { name: string }
fn advance(step: fn(State, string) -> State, state: State, event: string) -> State {
    step(state, event)
}
fn renamed(state: State, event: string) -> State { State { name: event } }
effect fn main() -> string {
    let next = advance(renamed, State { name: "old" }, "new")
    next.name
}`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s ordinary callback rejected: %+v", target, r.Diagnostics)
		}
	}
	r := Compile(source)
	generated, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": "module effra.generated\n\ngo 1.27\n", "main.go": generated} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "run", "."); err != nil || string(output) != "new\n" {
		t.Fatalf("native callback execution: %v %s", err, output)
	}
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "new\n" {
		t.Fatalf("JS callback execution: %s", output)
	}
}

func TestClosedCallbackRowsAndVariance(t *testing.T) {
	for _, test := range []struct {
		name, callback, expected string
		checked                  bool
	}{
		{"narrower rows", `effect fn operation(x: string) -> string { x }`, `effect fn(string) -> string raises {A} uses {Users}`, true},
		{"wider failure", `effect fn operation(x: string) -> string raises {A} { x }`, `effect fn(string) -> string`, false},
		{"wider service", `effect fn operation(x: string) -> string uses {Users} { run Users.get(x) }`, `effect fn(string) -> string`, false},
		{"parameter mismatch", `effect fn operation(x: bool) -> string { "ok" }`, `effect fn(string) -> string`, false},
		{"result mismatch", `effect fn operation(x: string) -> bool { true }`, `effect fn(string) -> string`, false},
		{"pure is distinct", `fn operation(x: string) -> string { x }`, `effect fn(string) -> string`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `error A service Users { effect fn get(x: string) -> string } ` + test.callback + ` fn store(cb: ` + test.expected + `) -> () { () } effect fn main() -> () { store(operation) }`
			r := Compile(source)
			if r.Checked != test.checked {
				t.Fatalf("checked=%v, diagnostics=%+v", r.Checked, r.Diagnostics)
			}
			if !test.checked {
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == "EF106"
				}
				if !found {
					t.Fatalf("expected callable contract mismatch, got %+v", r.Diagnostics)
				}
			}
		})
	}
}
