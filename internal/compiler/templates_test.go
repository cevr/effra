package compiler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const bundledSwitchWitness = `import Convert "effra/conversions"
record Switch { enabled: bool }
effect fn fromWire(input: bool) -> Switch { Switch { enabled: input } }
effect fn toWire(value: Switch) -> bool { value.enabled }
effect fn main() -> string {
 let converter = Convert.witness(fromWire, toWire)
 let value = run converter.decode(true)
 let enabled = run converter.encode(value)
 if enabled { "enabled" } else { "disabled" }
}`

const bundledUserWitness = `import Convert "effra/conversions"
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
effect fn main() -> string {
 let converter = Convert.witness(decode, encode)
 let user = run converter.decode("Ada")
 run converter.encode(user)
}`

func TestBundledWitnessInitializesActualFunctions(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(bundledUserWitness, target)
		if !r.Checked {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
		if len(r.Program.BundledTemplates) != 1 || len(r.Program.BundledFunctions) != 1 {
			t.Fatal("incorrect selected closure", r.BundledBindings)
		}
		factory := r.Program.BundledFunctions[0]
		if len(factory.returnFields) != 2 {
			t.Fatal("factory omitted initialized fields")
		}
		info, err := r.TypeAt(strings.Index(bundledUserWitness, "Convert.witness") + len("Convert."))
		if err != nil {
			t.Fatal(err)
		}
		if info.Type.Contract.Kind != "application" || len(info.Type.Contract.ArgIDs) != 4 {
			t.Fatal("application slots omitted", info.Type)
		}
		if target == "go" {
			code, err := r.EmitGo()
			if err != nil || !strings.Contains(code, "efTemplate_") || !strings.Contains(code, "[T any, W any]") {
				t.Fatal(err, code)
			}
		} else {
			code, _, err := r.Emit(false)
			if err != nil || !strings.Contains(code, `["decode"]: __ef_local_decode`) || !strings.Contains(code, `["encode"]: __ef_local_encode`) {
				t.Fatal(err, code)
			}
		}
	}
}

const bundledAnnotatedWitness = `import Convert "effra/conversions"
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
fn make() -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> {
 Convert.witness(decode, encode)
}
fn forward(value: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string>) -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> { value }
effect fn main() -> string {
 let converter = forward(make())
 let user = run converter.decode("Ada")
 run converter.encode(user)
}`

const bundledPayloadWitness = `import Convert "effra/conversions"
record User { name: string }
record Holder { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
enum Bundle { Value { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> } }
error Broken { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
fn make() -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> { Convert.witness(decode, encode) }
effect fn broken() -> void raises {Broken} { fail Broken { converter: make() } }
effect fn main() -> string {
 let holder = Holder { converter: make() }
 let boxed = Bundle.Value { converter: holder.converter }
 match boxed {
  Bundle.Value { converter } => {
   let user = run converter.decode("Ada")
   run converter.encode(user)
  }
 }
}`

func TestBundledWitnessCompleteBoundaryAndShapeDiagnostics(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, source := range []string{bundledAnnotatedWitness, strings.Replace(bundledAnnotatedWitness, "Convert.witness(decode, encode)", "Convert.Codec { decode: decode, encode: encode }", 1)} {
			r := CompileFor(source, target)
			if !r.Checked || r.CheckResponse()["typeProjectionComplete"] != true {
				t.Fatalf("%s: %v", target, r.Diagnostics)
			}
		}
		for _, mutation := range []struct{ before, after string }{
			{"Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string>", "Convert.Codec<User>"},
			{"effect fn encode(user: User) -> string { user.name }", "effect fn encode(user: string) -> string { user }"},
			{"Convert.witness(decode, encode)", "Convert.Codec { decode: decode }"},
			{"Convert.witness(decode, encode)", "Convert.Codec { decode: encode, encode: decode }"},
			{"Convert.witness(decode, encode)", "Convert.witness(nil, encode)"},
			{"converter.decode(\"Ada\")", "converter.decode(true)"},
			{"converter.encode(user)", "converter.encode(\"Ada\")"},
		} {
			r := CompileFor(strings.Replace(bundledAnnotatedWitness, mutation.before, mutation.after, 1), target)
			if r.Checked {
				t.Fatalf("%s admitted malformed product: %s", target, mutation.after)
			}
		}
	}
}

func TestBundledWitnessUnavailableGenericLayoutsDiagnose(t *testing.T) {
	for _, source := range []string{
		`import Convert "effra/conversions"
fn keep(input: string) -> string { input }
effect fn decode(input: string) -> (fn(string) -> string) { keep }
effect fn encode(value: fn(string) -> string) -> string { value("x") }
effect fn main() -> void { let converter = Convert.witness(decode, encode); void }`,
		`fn keep<T: type>(value: T) -> T { value } effect fn main() -> void { void }`,
	} {
		for _, target := range []string{"go", "js"} {
			r := CompileFor(source, target)
			if r.Checked || !hasCode(r, "EF127") {
				t.Fatalf("%s unsupported layout accepted: %v", target, r.Diagnostics)
			}
		}
	}
}

const bundledDirectionalWitness = `import Convert "effra/conversions"
error DecodeFailure
error EncodeFailure
record User { name: string }
service Names { effect fn lookup(input: string) -> string raises {DecodeFailure} }
service Labels { effect fn write(input: string) -> string raises {EncodeFailure} }
impl MemoryNames for Names { effect fn lookup(input: string) -> string raises {DecodeFailure} { "Ada:" + input } }
impl MemoryLabels for Labels { effect fn write(input: string) -> string raises {EncodeFailure} { "label:" + input } }
effect fn decode(input: string) -> User raises {DecodeFailure} uses {Names} { let name = run Names.lookup(input); User { name: name } }
effect fn encode(user: User) -> string raises {EncodeFailure} uses {Labels} { run Labels.write(user.name) }
fn make() -> Convert.Codec<User, string, effect fn(string) -> User raises {DecodeFailure} uses {Names}, effect fn(User) -> string raises {EncodeFailure} uses {Labels}> { Convert.witness(decode, encode) }
effect fn read(input: string) -> User raises {DecodeFailure} uses {Names} { let converter = make(); run converter.decode(input) }
effect fn write(user: User) -> string raises {EncodeFailure} uses {Labels} { let converter = make(); run converter.encode(user) }
effect fn main() -> string {
 let user = run read("42").provide<Names>(MemoryNames).catch<DecodeFailure>(User { name: "missing" })
 run write(user).provide<Labels>(MemoryLabels).catch<EncodeFailure>("missing")
}`

func TestBundledWitnessIndependentDirectionalRows(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(bundledDirectionalWitness, target)
		if !r.Checked {
			t.Fatalf("%s: %v", target, r.Diagnostics)
		}
		for _, expected := range []struct{ name, failure, service string }{{"read", "DecodeFailure", "Names"}, {"write", "EncodeFailure", "Labels"}} {
			f := r.Find(expected.name)
			if strings.Join(f.Actual.Errors, ",") != expected.failure || strings.Join(f.Actual.Services, ",") != expected.service {
				t.Fatal("directional rows changed", f)
			}
			for _, removal := range []struct{ text, code string }{{" raises {" + expected.failure + "}", "EF107"}, {" uses {" + expected.service + "}", "EF108"}} {
				parameter := "input:"
				if expected.name == "write" {
					parameter = "user:"
				}
				start := strings.Index(bundledDirectionalWitness, "effect fn "+expected.name+"("+parameter)
				source := bundledDirectionalWitness[:start] + strings.Replace(bundledDirectionalWitness[start:], removal.text, "", 1)
				bad := CompileFor(source, target)
				if bad.Checked || !hasCode(bad, removal.code) {
					t.Fatalf("%s %s lost %s: %v", target, expected.name, removal.code, bad.Diagnostics)
				}
			}
		}
	}
}

func TestBundledWitnessStrictTypeScript(t *testing.T) {
	r := CompileFor(bundledDirectionalWitness, "js")
	_, declarations, err := r.Emit(true)
	if err != nil {
		t.Fatal(err, r.Diagnostics)
	}
	checkStrictTypeScript(t, declarations, `import type { make, User, NamesRequirement, LabelsRequirement } from "./generated.d.mts";
import type { Effect } from "effect";
declare const codec: ReturnType<typeof make>;
declare const user: User;
const decoded: Effect.Effect<User, { readonly _tag: "DecodeFailure" }, NamesRequirement> = codec.decode("42");
const encoded: Effect.Effect<string, { readonly _tag: "EncodeFailure" }, LabelsRequirement> = codec.encode(user);
void decoded; void encoded;
// @ts-expect-error wrong directional row
const swapped: Effect.Effect<User, { readonly _tag: "EncodeFailure" }, LabelsRequirement> = codec.decode("42");
void swapped;
// @ts-expect-error wrong wire
codec.decode(42);
// @ts-expect-error wrong domain
codec.encode("Ada");`)
}

func TestBundledWitnessPayloadAnnotationsStrictTypeScript(t *testing.T) {
	r := CompileFor(bundledPayloadWitness, "js")
	_, declarations, err := r.Emit(true)
	if err != nil {
		t.Fatal(err, r.Diagnostics)
	}
	checkStrictTypeScript(t, declarations, `import type { Holder, Bundle, BrokenError, User } from "./generated.d.mts";
import type { Effect } from "effect";
declare const holder: Holder;
declare const bundle: Bundle;
declare const failure: BrokenError;
declare const user: User;
const recordDecode: Effect.Effect<User, never, never> = holder.converter.decode("Ada");
const variantDecode: Effect.Effect<User, never, never> = bundle.converter.decode("Ada");
const failureDecode: Effect.Effect<User, never, never> = failure.converter.decode("Ada");
const recordEncode: Effect.Effect<string, never, never> = holder.converter.encode(user);
const variantEncode: Effect.Effect<string, never, never> = bundle.converter.encode(user);
const failureEncode: Effect.Effect<string, never, never> = failure.converter.encode(user);
void recordDecode; void variantDecode; void failureDecode;
void recordEncode; void variantEncode; void failureEncode;
// @ts-expect-error wrong variant wire
bundle.converter.decode(true);
// @ts-expect-error wrong failure domain
failure.converter.encode("Ada");`)
}

func TestBundledWitnessFieldEvidencePreservesManagedOwnership(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, direction := range []string{"decode", "encode"} {
			for _, acquired := range []bool{false, true} {
				if acquired && target == "js" {
					continue // Files acquisition is a native builtin.
				}
				body := "file"
				if acquired {
					body = `run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`
				}
				source := `import Convert "effra/conversions"
effect fn chosen(file: File) -> File raises {IoError} { ` + body + ` }
effect fn keep(file: File) -> File raises {IoError} { file }
fn forward(value: Convert.Codec<File, File, effect fn(File) -> File raises {IoError}, effect fn(File) -> File raises {IoError}>) -> Convert.Codec<File, File, effect fn(File) -> File raises {IoError}, effect fn(File) -> File raises {IoError}> { value }
effect fn outer(file: File) -> File raises {IoError} {
 scope { let converter = forward(Convert.witness(`
				if direction == "decode" {
					source += "chosen, keep"
				} else {
					source += "keep, chosen"
				}
				source += `)); run converter.` + direction + `(file) }
}
effect fn main() -> void { void }`
				r := CompileFor(source, target)
				if r.Checked == acquired || (acquired && !hasCode(r, "EF123")) {
					t.Fatalf("%s %s acquire=%v: %v", target, direction, acquired, r.Diagnostics)
				}
				plain := strings.ReplaceAll(source, "Convert.Codec<File, File, effect fn(File) -> File raises {IoError}, effect fn(File) -> File raises {IoError}>", "Local")
				plain = strings.Replace(plain, `import Convert "effra/conversions"`, `record Local { decode: effect fn(File) -> File raises {IoError}, encode: effect fn(File) -> File raises {IoError} }`, 1)
				plain = strings.ReplaceAll(plain, "Convert.witness(chosen, keep)", "Local { decode: chosen, encode: keep }")
				plain = strings.ReplaceAll(plain, "Convert.witness(keep, chosen)", "Local { decode: keep, encode: chosen }")
				ordinary := CompileFor(plain, target)
				if ordinary.Checked == acquired || (acquired && !hasCode(ordinary, "EF123")) {
					t.Fatalf("%s ordinary record %s acquire=%v: %v", target, direction, acquired, ordinary.Diagnostics)
				}
				conditional := strings.Replace(source, "forward(Convert.witness(", "forward(if true { Convert.witness(keep, keep) } else { Convert.witness(", 1)
				conditional = strings.Replace(conditional, ")); run converter.", ") }); run converter.", 1)
				joined := CompileFor(conditional, target)
				if joined.Checked == acquired || (acquired && !hasCode(joined, "EF123")) {
					t.Fatalf("%s conditional %s acquire=%v: %v", target, direction, acquired, joined.Diagnostics)
				}
			}
		}
	}
}

func TestBundledWitnessJSExecution(t *testing.T) {
	for _, fixture := range []struct{ source, output string }{{bundledAnnotatedWitness, "Ada\n"}, {bundledDirectionalWitness, "label:Ada:42\n"}, {bundledSwitchWitness, "enabled\n"}, {bundledPayloadWitness, "Ada\n"}} {
		if output := runJSForTarget(t, "js", fixture.source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != fixture.output {
			t.Fatalf("actual witness fields: got %q, want %q", output, fixture.output)
		}
	}
}

func TestBundledWitnessNativeExecution(t *testing.T) {
	for _, fixture := range []struct{ source, output string }{{bundledAnnotatedWitness, "Ada\n"}, {bundledDirectionalWitness, "label:Ada:42\n"}, {bundledSwitchWitness, "enabled\n"}, {bundledPayloadWitness, "Ada\n"}} {
		r := Compile(fixture.source)
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
			t.Fatalf("native witness compilation: %v\n%s", err, output)
		}
		if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != fixture.output {
			t.Fatalf("native actual witness fields: %v %s", err, output)
		}
	}
}

func TestBundledWitnessTransportRejectsFieldAndShapeCorruption(t *testing.T) {
	r := Compile(bundledUserWitness)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	dto := r.projector.admittedSummaries["effra/conversions"]
	for _, test := range []struct {
		name   string
		mutate func(*interfaceSummary)
	}{
		{"shape owner", func(d *interfaceSummary) { d.Templates[0].Ref = "template:caller:Codec" }},
		{"shape field slot", func(d *interfaceSummary) { d.Templates[0].Fields[0].Type = d.Templates[0].Parameters[0].Variable }},
		{"variable owner", func(d *interfaceSummary) { d.Templates[0].Parameters[0].Ref = "type-parameter:caller:T" }},
		{"missing initialized field", func(d *interfaceSummary) {
			for i := range d.Occurrences {
				if len(d.Occurrences[i].Fields) == 2 {
					d.Occurrences[i].Fields = d.Occurrences[i].Fields[:1]
					return
				}
			}
			t.Fatal("no initialized occurrence")
		}},
		{"field cycle", func(d *interfaceSummary) {
			for i := range d.Occurrences {
				if len(d.Occurrences[i].Fields) > 0 {
					d.Occurrences[i].Fields[0].Occurrence = d.Occurrences[i].Ref
					return
				}
			}
			t.Fatal("no initialized occurrence")
		}},
		{"parameter field path", func(d *interfaceSummary) {
			for i := range d.Evidence {
				if d.Evidence[i].Parameter.Kind == "parameter" {
					d.Evidence[i].Parameter.Path = "missing"
					return
				}
			}
			t.Fatal("no parameter evidence")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire, _ := json.Marshal(dto)
			var corrupt interfaceSummary
			if err := json.Unmarshal(wire, &corrupt); err != nil {
				t.Fatal(err)
			}
			test.mutate(&corrupt)
			fresh := Compile(bundledUserWitness)
			if err := fresh.projector.admitInterfaceSummary(corrupt, fresh.Program.BundledFunctions); err == nil {
				t.Fatal("corrupt witness transport admitted")
			}
		})
	}
}

func TestBundledWitnessClassOnlyClosure(t *testing.T) {
	source := `import Convert "effra/conversions"
record User { name: string }
fn pass(value: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string>) -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> { value }
effect fn main() -> void { void }`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked || len(r.Program.BundledFunctions) != 0 || len(r.Program.BundledTemplates) != 1 || len(r.BundledInterfaces) != 1 {
			t.Fatalf("%s class-only closure: %v %v", target, r.Diagnostics, r.BundledInterfaces)
		}
	}
}

func TestBundledWitnessAuthoredSourcesCanonical(t *testing.T) {
	for _, path := range []string{"bundled/conversions/codec.ef", "bundled/conversions/witness.ef"} {
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

func TestBundledWitnessExplicitBoundaryCannotNarrowExposedRows(t *testing.T) {
	source := strings.Replace(bundledAnnotatedWitness, `record User { name: string }`, `error DecodeFailure
error EncodeFailure
record User { name: string }`, 1)
	source = strings.ReplaceAll(source, "effect fn(string) -> User", "effect fn(string) -> User raises {DecodeFailure}")
	source = strings.ReplaceAll(source, "effect fn(User) -> string", "effect fn(User) -> string raises {EncodeFailure}")
	for _, target := range []string{"go", "js"} {
		bad := CompileFor(source, target)
		if bad.Checked || !hasCode(bad, "EF107") {
			t.Fatalf("%s erased explicit field rows: %v", target, bad.Diagnostics)
		}
		good := CompileFor(strings.Replace(source, "effect fn main() -> string", "effect fn main() -> string raises {DecodeFailure, EncodeFailure}", 1), target)
		if !good.Checked {
			t.Fatalf("%s narrower implementations must satisfy wider explicit contracts: %v", target, good.Diagnostics)
		}
	}
}

func TestBundledWitnessCacheKeepsReceiverOwnersSeparate(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		first := CompileFor(bundledUserWitness, target)
		second := CompileFor(`record Unrelated { count: i64 } `+strings.ReplaceAll(strings.ReplaceAll(bundledUserWitness, "Convert", "Other"), "User", "Customer"), target)
		if !first.Checked || !second.Checked {
			t.Fatal(first.Diagnostics, second.Diagnostics)
		}
		a, b := first.Program.BundledFunctions[0], second.Program.BundledFunctions[0]
		if a == b || first.projector.values == second.projector.values || !reflect.DeepEqual(first.projector.admittedSummaries[a.Module], second.projector.admittedSummaries[b.Module]) {
			t.Fatal("caller/alias affected immutable interface or receiving arena was shared")
		}
		for _, receiver := range []*Result{first, second} {
			factory := receiver.Program.BundledFunctions[0]
			for _, name := range []string{"decode", "encode"} {
				field := factory.returnFields[name]
				if field.callableEvidence.parameter != factory || field.callableEvidence.parameterName != name {
					t.Fatal("field evidence rebound outside receiving declaration")
				}
			}
		}
	}
}
