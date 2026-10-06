package compiler

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestGenericDataLivePresentNativeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "present.txt")
	if err := os.WriteFile(path, []byte("managed generic read"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import Data "effra/data"
fn forward(value:Data.Option<File>)->Data.Option<File>{value}
effect fn main()->string raises {IoError} {
 scope {
  let file = run Files.openRead(` + strconv.Quote(path) + `).provide<Files>(LiveFiles)
  let present = forward(Data.Option.Some {value:file})
  match present {Data.Option.None => "absent"; Data.Option.Some {value:actual} => run Files.readText(actual).provide<Files>(LiveFiles)}
 }
}`
	runGenericDataNative(t, source, "managed generic read\n")
}

const genericUnitPresence = `import Data "effra/data"
fn describe(value:Data.Option<()>)->string {
 match value {Data.Option.None => "absent"; Data.Option.Some {value:unit} => { let present = unit; "present" }}
}
effect fn main()->string{describe(Data.Option.Some {value:()})+":"+describe(Data.Option<()>.None {})}`

const genericAliasShadow = `import Data "effra/data"
record Local {Option:fn()->string}
fn text()->string {"local"}
fn local(Data:Local)->string {Data.Option()}
fn absent()->Data.Option<string>{Data.Option<string>.None {}}
fn describe()->string {match absent(){Data.Option.None => "missing"; Data.Option.Some {value} => value}}
effect fn main()->string {local(Local {Option:text})+":"+describe()}`

func TestGenericDataPresenceAndAliasExecution(t *testing.T) {
	for _, fixture := range []struct{ source, output string }{
		{genericUnitPresence, "present:absent"},
		{genericAliasShadow, "local:missing"},
	} {
		runGenericDataNative(t, fixture.source, fixture.output+"\n")
		if output := runJS(t, fixture.source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != fixture.output+"\n" {
			t.Fatal(output)
		}
	}
	const sameName = `import Data "effra/data"
enum Option<T:type>{Missing; Present {value:T}}
fn local()->Option<string>{Option.Present {value:"local"}}
fn bundled()->Data.Option<string>{Data.Option<string>.None {}}
effect fn main()->string {
 let a = match local(){Option.Missing => "none"; Option.Present {value} => value}
 let b = match bundled(){Data.Option.None => "missing"; Data.Option.Some {value} => value}
 a+":"+b
}`
	const sameResult = `import Data "effra/data"
enum Result<T:type,E:type>{Missing; Present {value:T}}
fn local()->Result<string,bool>{Result<string,bool>.Present {value:"local"}}
fn bundled()->Data.Result<string,bool>{Data.Result<string,bool>.Err {error:false}}
effect fn main()->string {
 let a = match local(){Result.Missing => "none"; Result.Present {value} => value}
 let b = match bundled(){Data.Result.Ok {value} => value; Data.Result.Err {error:_} => "missing"}
 a+":"+b
}`
	for _, source := range []string{sameName, sameResult} {
		runGenericDataNative(t, source, "local:missing\n")
		if r := CompileFor(source, "js"); r.Checked || !hasCode(r, "EF110") {
			t.Fatal("JS declaration collision must retain honest target refusal", r.Diagnostics)
		}
	}
}

func TestGenericDataAbsenceAndOwnerControls(t *testing.T) {
	const declarations = `import Data "effra/data" record User {name:string}`
	for _, target := range []string{"go", "js"} {
		for _, source := range []string{
			`fn bad()->Data.Option<User>{nil}`,
			`fn bad()->Data.Option<User>{null}`,
			`fn bad()->Data.Option<User?>{Data.Option.Some {value:User {name:"x"}}}`,
			`fn bad()->Data.Option<User>{Data.Option.Some {}}`,
			`fn bad()->Data.Option<User>{Data.Option.Some {value:null}}`,
			`fn bad()->Data.Option<User>{}`,
			`fn bad(value:Data.Option<User>)->User{value.value}`,
			`fn bad()->Data.Option<User>{Foreign.Option<User>.None {}}`,
		} {
			r := CompileFor(declarations+source, target)
			if r.Checked || len(r.Diagnostics) == 0 {
				t.Fatal(target, "absence/owner control admitted", source, r.Diagnostics)
			}
		}
		borrowed := CompileFor(`import Data "effra/data"
fn wrap(file:File)->Data.Option<File>{Data.Option.Some {value:file}}
fn unwrap(value:Data.Option<File>,fallback:File)->File{match value {Data.Option.None => fallback; Data.Option.Some {value:file} => file}}
effect fn outer(file:File)->File{scope {unwrap(wrap(file),file)}}`, target)
		if !borrowed.Checked {
			t.Fatal(target, "borrowed present file rejected", borrowed.Diagnostics)
		}
	}
	acquired := CompileFor(`import Data "effra/data"
fn wrap(file:File)->Data.Option<File>{Data.Option.Some {value:file}}
fn unwrap(value:Data.Option<File>,fallback:File)->File{match value {Data.Option.None => fallback; Data.Option.Some {value:file} => file}}
effect fn outer(file:File)->File raises {IoError}{scope {let owned = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles); unwrap(wrap(owned),file)}}`, "go")
	if acquired.Checked || !hasCode(acquired, "EF123") {
		t.Fatal("acquired Option payload escaped closing owner", acquired.Diagnostics)
	}
}

func TestGenericDataPhantomCallableApplicationIdentity(t *testing.T) {
	const source = `error Trouble
enum Phantom<F:callable effect fn(A)->A,A:type>{None}
fn narrow()->Phantom<effect fn(string)->string,string>{Phantom<effect fn(string)->string,string>.None {}}
fn wide()->Phantom<effect fn(string)->string raises {Trouble},string>{Phantom<effect fn(string)->string raises {Trouble},string>.None {}}`
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declaration, `import type { narrow, wide } from "./generated.d.mts";
declare const narrower: ReturnType<typeof narrow>;
const same: ReturnType<typeof narrow> = narrower;
// @ts-expect-error phantom callable arguments retain exact application identity
const different: ReturnType<typeof wide> = narrower;
void same; void different;
`)
}

func TestGenericDataUnrelatedCallerExecutionAndSharedSources(t *testing.T) {
	for _, fixture := range []struct{ path, output string }{
		{"generic-users.ef", "Ada:missing"},
		{"generic-settings.ef", "enabled:default:invalid"},
	} {
		data, err := os.ReadFile("../../examples/" + fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		runGenericDataNative(t, source, fixture.output+"\n")
		if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != fixture.output+"\n" {
			t.Fatal(fixture.path, output)
		}
		for _, target := range []string{"go", "js"} {
			r := CompileFor(source, target)
			if !r.Checked || len(r.BundledInterfaces) != 1 || len(r.Program.BundledTemplates) != 2 {
				t.Fatal(fixture.path, target, r.Diagnostics)
			}
			for _, declaration := range r.Program.BundledTemplates {
				if declaration.Module != "effra/data" || declaration.SourceID != "source:effra/data/"+declaration.Name {
					t.Fatal(declaration)
				}
			}
			if target == "js" {
				_, declaration, err := r.Emit(false)
				if err != nil {
					t.Fatal(err)
				}
				checkStrictTypeScript(t, declaration, `import type { main } from "./generated.d.mts";
import type { Effect } from "effect";
declare const execution: ReturnType<typeof main>;
const success: Effect.Effect<string, unknown, never> = execution;
void success;
`)
			}
		}
	}
	firstData, err := os.ReadFile("../../examples/generic-users.ef")
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile("../../examples/generic-settings.ef")
	if err != nil {
		t.Fatal(err)
	}
	a, b := Compile(string(firstData)), Compile(string(secondData))
	for _, source := range a.Sources {
		if source.Module != "effra/data" {
			continue
		}
		found := false
		for _, other := range b.Sources {
			found = found || source == other
		}
		if !found {
			t.Fatal("callers did not share authoritative bundled source", source, b.Sources)
		}
	}
}
