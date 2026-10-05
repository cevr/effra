package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const goProbe = `error Missing error Broken
service Users { effect fn get(id: string) -> string throws {Missing, Broken} }
impl Memory for Users {
 effect fn get(id: string) -> string throws {Missing, Broken} {
  if id == "42" { "Ada" } else { if id == "broken" { fail Broken } else { fail Missing } }
 }
}
fn pure(name: string) -> string { if name == "Ada" { "Hi " + name } else { "Other" } }
fn unused() -> () { let pending = Console.log("must not print"); () }
effect fn greeting(id: string) -> string throws {Missing, Broken} uses {Users} {
 let name = run Users.get(id)
 pure(name)
}
effect fn recovered(id: string) -> string throws {Broken} {
 run greeting(id).provide<Users>(Memory).catch<Missing>(if true { "unknown" } else { "other" })
}
effect fn nested() -> string {
 run recovered(run recovered("42").catch<Broken>("bad")).catch<Broken>("bad")
}
effect fn bottom() -> string throws {Missing, Broken} {
 if true { fail Missing } else { fail Broken }
}
effect fn main() -> string throws {Broken} { unused() run recovered("42") }
`

func TestGoBackendConformance(t *testing.T) {
	r := CompileFor(goProbe, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	source, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := r.EmitGo()
	if source != again {
		t.Fatal("nondeterministic Go emission")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "main.go")
	probe := filepath.Join(dir, "main_test.go")
	if err = os.WriteFile(program, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	assertions := `package main
import "testing"
func TestGeneratedSemantics(t *testing.T) {
 calls:=0
 ctx:=efContext{s_Users:&efService_Users{m_get:func(id string) efEffect[string] {
  return func(ctx efContext) efExit[string] {calls++; return efExit[string]{value:"Ada"}}
 }}}
 pending:=efFunction_greeting("42")
 if calls!=0 {t.Fatal("eager construction")}
 first,second:=pending(ctx),pending(ctx)
 if calls!=2 || first.value!="Hi Ada" || second.value!="Hi Ada" {t.Fatal("laziness/replay broken")}
 if efFunction_recovered("missing")(efContext{}).value!="unknown" {t.Fatal("recovery broken")}
 other:=efFunction_recovered("broken")(efContext{})
 if other.failure==nil || other.failure.tag!="Broken" || other.defect!=nil {t.Fatal("selective recovery swallowed a different failure")}
 missing:=pending(efContext{})
 if missing.defect==nil || missing.failure!=nil {t.Fatal("missing provider is not a typed failure")}
 bound:=efProvide_Users(pending,efProvider_Memory())
 if bound(efContext{}).value!="Hi Ada" {t.Fatal("provision failed")}
 if pending(efContext{}).defect==nil {t.Fatal("provider escaped lexical provision")}
 if efFunction_nested()(efContext{}).failure!=nil {t.Fatal("nested run failed")}
 bottom:=efFunction_bottom()(efContext{})
 if bottom.failure==nil || bottom.failure.tag!="Missing" {t.Fatal("bottom branches lost failure")}
 efFunction_unused()
}
`
	if err = os.WriteFile(probe, []byte(assertions), 0644); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("go", "test", "-race", program, probe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go conformance: %v\n%s\n%s", err, output, source)
	}
	if strings.Contains(string(output), "must not print") {
		t.Fatal("constructed recipe executed")
	}
	binary := filepath.Join(dir, "native")
	output, err = exec.Command("go", "build", "-o", binary, program).CombinedOutput()
	if err != nil {
		t.Fatalf("native build: %v\n%s", err, output)
	}
	output, err = exec.Command(binary).CombinedOutput()
	if err != nil || string(output) != "Hi Ada\n" {
		t.Fatalf("native executable: %v\n%s", err, output)
	}
	// The same source must have the same admitted contracts and results on the JS target.
	js := CompileFor(goProbe, "js")
	if !js.Checked || js.Revision != r.Revision || !reflect.DeepEqual(js.Symbols, r.Symbols) {
		t.Fatal("target-specific frontend drift")
	}
	outputJS := runJS(t, goProbe, `console.log(await Effect.runPromise(__ef_function_main()));`)
	if outputJS != string(output) {
		t.Fatalf("Go/JS behavior differs: %q / %q", output, outputJS)
	}
}
func TestGoBackendRefusesInvalidEntry(t *testing.T) {
	for _, source := range []string{`effect fn main() -> () uses {Console} { run Console.log("x") }`, `fn main() -> string { "x" }`, `effect fn library() -> string { "x" }`} {
		if _, err := Compile(source).EmitGo(); err == nil {
			t.Fatal("invalid executable entry emitted")
		}
	}
	if CompileFor(`effect fn main() -> () {}`, "llvm").Checked {
		t.Fatal("unsupported target accepted")
	}
}
