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
 service Users { effect fn get(id: string) -> string raises {Missing, Broken} }
impl Memory for Users {
 effect fn get(id: string) -> string raises {Missing, Broken} {
  if id == "42" { "Ada" } else { if id == "broken" { fail Broken } else { fail Missing } }
 }
}
fn pure(name: string) -> string { if name == "Ada" { "Hi " + name } else { "Other" } }
fn unused() -> void { let pending = Console.log("must not print"); void }
effect fn greeting(id: string) -> string raises {Missing, Broken} uses {Users} {
 let name = run Users.get(id)
 pure(name)
}
effect fn recovered(id: string) -> string raises {Broken} {
 run greeting(id).provide<Users>(Memory).catch<Missing>(if true { "unknown" } else { "other" })
}
effect fn nested() -> string {
 run recovered(run recovered("42").catch<Broken>("bad")).catch<Broken>("bad")
}
effect fn bottom() -> string raises {Missing, Broken} {
 if true { fail Missing } else { fail Broken }
}
effect fn main() -> string raises {Broken} { unused() run recovered("42") }
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
	if err = WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "main.go")
	probe := filepath.Join(dir, "main_test.go")
	if err = os.WriteFile(program, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	assertions := `package main
import("testing";er "effra.generated/runtime")

func TestGeneratedSemantics(t *testing.T) {
 er.Run(func(fc *er.FiberContext)er.Exit[struct{}]{
 calls:=0
 ctx:=efContext{Runtime:fc,s_Users:&efService_Users{m_get:func(id string) efEffect[string] {
  return func(ctx efContext) efExit[string] {calls++; return efExit[string]{Value:"Ada"}}
 }}}
 pending:=efFunction_greeting("42")
 if calls!=0 {t.Fatal("eager construction")}
 first,second:=pending(ctx),pending(ctx)
 if calls!=2 || first.Value!="Hi Ada" || second.Value!="Hi Ada" {t.Fatal("laziness/replay broken")}
 if efFunction_recovered("missing")(efContext{Runtime:fc}).Value!="unknown" {t.Fatal("recovery broken")}
 other:=efFunction_recovered("broken")(efContext{Runtime:fc})
 if other.Failure==nil || other.Failure.Tag!="Broken" || other.Defect!=nil {t.Fatal("selective recovery swallowed a different failure")}
 missing:=pending(efContext{Runtime:fc})
 if missing.Defect==nil || missing.Failure!=nil {t.Fatal("missing provider is not a typed failure")}
 bound:=efProvide_Users(pending,efProvider_Memory())
 if bound(efContext{Runtime:fc}).Value!="Hi Ada" {t.Fatal("provision failed")}
 if pending(efContext{Runtime:fc}).Defect==nil {t.Fatal("provider escaped lexical provision")}
 if efFunction_nested()(efContext{Runtime:fc}).Failure!=nil {t.Fatal("nested run failed")}
 bottom:=efFunction_bottom()(efContext{Runtime:fc})
 if bottom.Failure==nil || bottom.Failure.Tag!="Missing" {t.Fatal("bottom branches lost failure")}
 efFunction_unused()
 return er.Succeed(struct{}{})
 })
}
`
	if err = os.WriteFile(probe, []byte(assertions), 0644); err != nil {
		t.Fatal(err)
	}
	output, err := runGoCommand(dir, "test", "-race", ".")
	if err != nil {
		t.Fatalf("generated Go conformance: %v\n%s\n%s", err, output, source)
	}
	if strings.Contains(string(output), "must not print") {
		t.Fatal("constructed recipe executed")
	}
	binary := filepath.Join(dir, "native")
	output, err = runGoCommand(dir, "build", "-o", binary, ".")
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
	for _, source := range []string{`effect fn main() -> void uses {Console} { run Console.log("x") }`, `fn main() -> string { "x" }`, `effect fn library() -> string { "x" }`} {
		if _, err := Compile(source).EmitGo(); err == nil {
			t.Fatal("invalid executable entry emitted")
		}
	}
	if CompileFor(`effect fn main() -> void {}`, "llvm").Checked {
		t.Fatal("unsupported target accepted")
	}
}

func runGoCommand(dir string, args ...string) ([]byte, error) {
	command := exec.Command("go", args...)
	command.Dir = dir
	return command.CombinedOutput()
}
