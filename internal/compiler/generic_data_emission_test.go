package compiler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const bundledDataExecutable = `import Data "effra/data"
record User {name:string}
record Missing {message:string}
fn present()->Data.Option<User>{Data.Option.Some {value:User {name:"Ada"}}}
fn absent()->Data.Option<User>{Data.Option<User>.None {}}
fn label(value:Data.Option<User>)->string{
 match value {Data.Option.None => "missing"; Data.Option.Some {value:user} => user.name}
}
fn result()->Data.Result<Data.Option<User>,Missing>{Data.Result<Data.Option<User>,Missing>.Ok {value:present()}}
fn read(value:Data.Result<Data.Option<User>,Missing>)->string{
 match value {Data.Result.Ok {value:option} => label(option); Data.Result.Err {error:missing} => missing.message}
}
effect fn main()->string {read(result())+":"+label(absent())}`

const genericCallableDataExecutable = `enum Action<F: callable fn(A)->A, A:type> { None; Some {operation:F} }
fn identity(value:string)->string {value}
fn selected()->Action<fn(string)->string,string>{Action<fn(string)->string,string>.Some {operation:identity}}
fn invoke(value:Action<fn(string)->string,string>)->string {
 match value {Action.None => "none"; Action.Some {operation} => operation("ok")}
}
effect fn main()->string {invoke(selected())}`

func TestBundledGenericDataAdmissionAndExecution(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(bundledDataExecutable, target)
		if !r.Checked || len(r.BundledInterfaces) != 1 || len(r.Program.BundledTemplates) != 2 {
			t.Fatal(target, r.Diagnostics, r.BundledInterfaces)
		}
		dto := r.projector.admittedSummaries["effra/data"]
		if len(dto.Templates) != 2 || dto.Templates[0].Kind != "enum" || len(dto.Templates[0].Variants) != 2 {
			t.Fatal("ordinary closed enum transport absent", dto.Templates)
		}
		wire, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeInterfaceSummary(wire, dto.ContentHash, dto.SourceInput); err != nil {
			t.Fatal(err)
		}
		if target == "go" {
			if _, err := r.EmitGo(); err != nil {
				t.Fatal(err)
			}
		} else if _, _, err := r.Emit(true); err != nil {
			t.Fatal(err)
		}
	}
	if output := runJS(t, bundledDataExecutable, `if (await Effect.runPromise(__ef_function_main()) !== "Ada:missing") throw new Error("bundled data result"); console.log("ok");`); output != "ok\n" {
		t.Fatal(output)
	}
}

func TestGenericEnumPayloadTransportFreshArenaAndCorruption(t *testing.T) {
	const source = genericCallableDataExecutable
	for _, target := range []string{"go", "js"} {
		first := CompileFor(source, target)
		if !first.Checked {
			t.Fatal(first.Diagnostics)
		}
		dto, err := exportInterfaceSummary(first.projector, currentModuleIdentity, "enum-payload-control", first.Program.Functions)
		if err != nil {
			t.Fatal(err)
		}
		fresh := CompileFor(source, target)
		if !fresh.Checked || first.projector.values == fresh.projector.values {
			t.Fatal("fresh arena control", fresh.Diagnostics)
		}
		if err := fresh.projector.admitInterfaceSummary(dto, fresh.Program.Functions); err != nil {
			t.Fatal("valid generic enum payload refused", err)
		}
		operation := fresh.Program.Functions[1].returnFields["Some"].fields["operation"]
		if operation.callableEvidence.count != 1 || operation.callableEvidence.callees[0] != fresh.Program.Functions[0] || operation.callableEvidence.callees[0] == first.Program.Functions[0] {
			t.Fatal("enum payload callee escaped receiving owner arena")
		}
		wire, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"missing payload", "wrong variant", "payload cycle"} {
			var corrupt interfaceSummary
			if err := json.Unmarshal(wire, &corrupt); err != nil {
				t.Fatal(err)
			}
			found := false
			for i := range corrupt.Occurrences {
				o := &corrupt.Occurrences[i]
				for j := range o.Variants {
					v := &o.Variants[j]
					if len(v.Fields) == 0 {
						continue
					}
					found = true
					switch kind {
					case "missing payload":
						v.Fields = []summaryFieldOccurrence{}
					case "wrong variant":
						v.Name = "Foreign"
					case "payload cycle":
						v.Fields[0].Occurrence = o.Ref
					}
					break
				}
				if found {
					break
				}
			}
			if !found {
				t.Fatal("enum payload evidence absent")
			}
			receiver := CompileFor(source, target)
			if err := receiver.projector.admitInterfaceSummary(corrupt, receiver.Program.Functions); err == nil {
				t.Fatal("corrupt enum payload admitted", kind)
			}
		}
	}
}

func TestBundledGenericDataCanonicalSources(t *testing.T) {
	for _, path := range []string{"bundled/data/option.ef", "bundled/data/result.ef"} {
		data, err := bundledSources.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		formatted, err := FormatSource(string(data))
		if err != nil || formatted.Text != string(data) {
			t.Fatalf("%s canonical source: %v\n%s", path, err, formatted.Text)
		}
	}
}

const genericDataExecutable = `record User {name:string}
record Envelope<T:type> {value:T}
enum Presence<T:type> {None; Some {value:T}}
fn absent()->Presence<User>{Presence<User>.None {}}
fn present()->Envelope<Presence<User>>{Envelope {value:Presence.Some {value:User {name:"Ada"}}}}
fn label(value:Presence<User>)->string{
 match value {Presence.None => "missing"; Presence.Some {value:user} => user.name}
}
effect fn main()->string {label(present().value)+":"+label(absent())}`

func TestGenericDataExecutableTargets(t *testing.T) {
	for _, source := range []string{genericDataExecutable, bundledDataExecutable} {
		runGenericDataNative(t, source, "Ada:missing\n")
	}
	runGenericDataNative(t, genericCallableDataExecutable, "ok\n")
	if output := runJS(t, genericCallableDataExecutable, `if (await Effect.runPromise(__ef_function_main()) !== "ok") throw new Error("generic callable payload"); console.log("ok");`); output != "ok\n" {
		t.Fatal(output)
	}
	if output := runJS(t, genericDataExecutable, `if (await Effect.runPromise(__ef_function_main()) !== "Ada:missing") throw new Error("generic result"); console.log("ok");`); output != "ok\n" {
		t.Fatal(output)
	}
	r := CompileFor(genericDataExecutable, "js")
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err, r.Diagnostics)
	}
	checkStrictTypeScript(t, declaration, `import type { User, Envelope, Presence } from "./generated.mjs";
declare const value: Envelope<Presence<User>>;
const item: Presence<User> = value.value;
declare const wrong: Presence<string>;
// @ts-expect-error nominal generic arguments are invariant
const mismatch: Presence<User> = wrong;
void item; void mismatch;
`)
}

func runGenericDataNative(t *testing.T, source, expected string) {
	t.Helper()
	r := CompileFor(source, "go")
	generated, err := r.EmitGo()
	if err != nil {
		t.Fatal(err, r.Diagnostics)
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
	binary := filepath.Join(dir, "native")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generic native compilation: %v\n%s\n%s", err, output, generated)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != expected {
		t.Fatalf("generic native execution: %v %s", err, output)
	}
}

func TestGenericDataTargetAdmission(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(genericDataExecutable, target)
		if !r.Checked {
			t.Fatalf("%s generic target unavailable: %+v", target, r.Diagnostics)
		}
		if target == "go" {
			if _, err := r.EmitGo(); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, _, err := r.Emit(true); err != nil {
				t.Fatal(err)
			}
		}
	}
}
