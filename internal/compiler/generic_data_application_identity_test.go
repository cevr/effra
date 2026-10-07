package compiler

import (
	"slices"
	"strings"
	"testing"
)

const phantomCallableIdentitySource = `error Trouble
enum Phantom<F:callable effect fn(A)->A,A:type>{None}
fn narrow()->Phantom<effect fn(string)->string,string>{Phantom<effect fn(string)->string,string>.None {}}
fn wide()->Phantom<effect fn(string)->string raises {Trouble},string>{Phantom<effect fn(string)->string raises {Trouble},string>.None {}}
fn consume(value:Phantom<effect fn(string)->string raises {Trouble},string>)->string{"wide"}
`

func TestGenericDataApplicationArgumentsAreInvariant(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, source := range []string{
			phantomCallableIdentitySource + `fn bad()->string{consume(narrow())}`,
			phantomCallableIdentitySource + `fn bad()->Phantom<effect fn(string)->string raises {Trouble},string>{narrow()}`,
		} {
			r := CompileFor(source, target)
			if r.Checked || !hasCode(r, "EF106") {
				t.Fatalf("%s widened a complete generic application: checked=%v diagnostics=%+v", target, r.Checked, r.Diagnostics)
			}
		}
	}
	r := CompileFor(phantomCallableIdentitySource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declaration, `import { consume, narrow, wide } from "./generated.mjs";
consume(wide());
// @ts-expect-error Complete generic application arguments remain invariant.
consume(narrow());
`)
}

func TestGenericDataExplicitCodecConstructionKeepsCallableFieldVariance(t *testing.T) {
	const source = `import Convert "effra/conversions"
error DecodeFailure
error EncodeFailure
record User {name:string}
effect fn decode(input:string)->User{User {name:input}}
effect fn encode(user:User)->string{user.name}
fn make()->Convert.Codec<User,string,effect fn(string)->User raises {DecodeFailure},effect fn(User)->string raises {EncodeFailure}>{
 Convert.Codec<User,string,effect fn(string)->User raises {DecodeFailure},effect fn(User)->string raises {EncodeFailure}>{decode:decode,encode:encode}
}
`
	for _, target := range []string{"go", "js"} {
		if r := CompileFor(source, target); !r.Checked {
			t.Fatalf("%s explicit exact Codec application rejected narrower field callbacks: %+v", target, r.Diagnostics)
		}
	}
}

func TestGenericDataApplicationRecontractRequiresAFactoryConstructor(t *testing.T) {
	const prefix = `error Trouble error SetupFailure
service Audit {effect fn ready()->string}
impl Memory for Audit {effect fn ready()->string{"ready"}}
service Setup {effect fn ping()->string raises {SetupFailure}}
impl MemorySetup for Setup {effect fn ping()->string raises {SetupFailure}{"ready"}}
record Pipe<F:callable effect fn(A)->A,A:type>{run:F;anchor:A}
effect fn narrow(value:string)->string{value}
fn factory()->Pipe<effect fn(string)->string,string>{Pipe<effect fn(string)->string,string>{run:narrow,anchor:""}}
fn forward(value:Pipe<effect fn(string)->string,string>)->Pipe<effect fn(string)->string,string>{value}
fn alias(value:Pipe<effect fn(string)->string,string>)->Pipe<effect fn(string)->string,string>{let copy=value;copy}
fn conditional(value:Pipe<effect fn(string)->string,string>,flag:bool)->Pipe<effect fn(string)->string,string>{if flag{value}else{value}}
effect fn echo(file:File)->File{file}
fn fileFactory(file:File)->Pipe<effect fn(File)->File,File>{Pipe<effect fn(File)->File,File>{run:echo,anchor:file}}
fn fileView(file:File)->Pipe<effect fn(File)->File raises {Trouble} uses {Audit},File>{fileFactory(file)}
effect fn useView(file:File)->File raises {Trouble} uses {Audit}{let view=fileView(file);run view.run(file)}
`
	const wider = `Pipe<effect fn(string)->string raises {Trouble} uses {Audit},string>`
	source := prefix + `fn widened()->` + wider + `{factory()}
effect fn orderedMake()->` + wider + ` raises {SetupFailure} uses {Setup}{let status=run Setup.ping();factory()}
effect fn main()->string{let pipe=widened();run pipe.run("Ada").provide<Audit>(Memory).catch<Trouble>("fallback")+pipe.anchor}`
	for _, target := range []string{"go", "js"} {
		good := CompileFor(source, target)
		if !good.Checked {
			t.Fatalf("%s direct factory construction should satisfy checked callable field upper bound: %+v", target, good.Diagnostics)
		}
		factoryCall, err := good.TypeAt(strings.Index(source, "{factory()}") + 1)
		if err != nil || factoryCall.Type.Contract.Kind != "application" || factoryCall.Type.Application == nil || factoryCall.Type.Application.ProducedResult == nil || factoryCall.Type.Application.Result.ID != factoryCall.Type.Contract.ID || factoryCall.Type.Application.ProducedResult.ID == factoryCall.Type.Application.Result.ID {
			t.Fatalf("%s factory boundary did not publish its checked result and produced identity: info=%+v err=%v", target, factoryCall, err)
		}
		producedID := good.projector.typePublicToID[factoryCall.Type.Application.ProducedResult.ID]
		resultID := good.projector.typePublicToID[factoryCall.Type.Application.Result.ID]
		producedNode, resultNode := good.projector.node(producedID), good.projector.node(resultID)
		if producedNode == nil || resultNode == nil || len(producedNode.Args) != 2 || len(resultNode.Args) != 2 || slices.Equal(producedNode.Args, resultNode.Args) {
			t.Fatalf("%s produced and exposed application rows were not distinct canonical identities: produced=%+v exposed=%+v", target, producedNode, resultNode)
		}
		producedCallable, exposedCallable := good.projector.node(producedNode.Args[0]), good.projector.node(resultNode.Args[0])
		if producedCallable == nil || exposedCallable == nil || len(good.projector.rowLabels(producedCallable.FailureRow)) != 0 || !slices.Equal(good.projector.rowLabels(exposedCallable.FailureRow), []string{"Trouble"}) || !slices.Equal(good.projector.rowLabels(exposedCallable.ServiceRow), []string{"Audit"}) {
			t.Fatalf("%s produced and exposed callable rows differ from checked factory contract: produced=%+v exposed=%+v", target, producedCallable, exposedCallable)
		}
		callable, err := good.TypeAt(strings.Index(source, `run("Ada")`))
		if err != nil || !slices.Equal(callable.Type.Errors, []string{"Trouble"}) || !slices.Equal(callable.Type.Services, []string{"Audit"}) {
			t.Fatalf("%s application caller lost exposed failure/service rows: info=%+v err=%v", target, callable, err)
		}
		ordered := good.Find("orderedMake")
		if ordered == nil || !slices.Equal(ordered.Actual.Errors, []string{"SetupFailure"}) || !slices.Equal(ordered.Actual.Services, []string{"Setup"}) {
			t.Fatalf("%s preceding execution rows were lost at factory return: %+v", target, ordered)
		}
		orderedCall, err := good.TypeAt(strings.Index(source[strings.Index(source, "orderedMake"):], "factory()") + strings.Index(source, "orderedMake") + 1)
		if err != nil || len(orderedCall.Evaluation.Failures) != 0 || len(orderedCall.Evaluation.Requirements) != 0 {
			t.Fatalf("%s earlier block evaluation leaked onto the returned factory occurrence: info=%+v err=%v", target, orderedCall, err)
		}
		fileView := good.checkedFunctions[functionNamed(good.Program.Functions, "fileView")].body
		if !slices.ContainsFunc(fileView.ownershipFacts(), func(fact OwnershipFact) bool { return fact.Path == "anchor" && fact.Status == "borrowed" }) {
			t.Fatalf("%s re-contract discarded borrowed field ownership: %+v", target, fileView.ownershipFacts())
		}
		capturedCall, err := good.TypeAt(strings.Index(source, "view.run(file)") + len("view."))
		if err != nil || !slices.Equal(capturedCall.Type.Errors, []string{"Trouble"}) || !slices.Equal(capturedCall.Type.Services, []string{"Audit"}) || !slices.ContainsFunc(capturedCall.Type.Captures, func(fact OwnershipFact) bool { return fact.Path == "capture:arg0" && fact.Status == "borrowed" }) {
			t.Fatalf("%s callable invocation after re-contract lost rows or argument capture evidence: info=%+v err=%v", target, capturedCall, err)
		}
		field := fileView.fields["run"]
		fileFactory := good.checkedFunctions[functionNamed(good.Program.Functions, "fileFactory")].body.fields["run"]
		if field.valueID() == invalidTypeID || !slices.Equal(field.captureFacts(), fileFactory.captureFacts()) {
			t.Fatalf("%s re-contract discarded callable field capture facts: %+v", target, field.captureFacts())
		}
		dto, err := exportInterfaceSummary(good.projector, currentModuleIdentity, good.Revision, good.Program.Functions)
		if err != nil {
			t.Fatalf("%s private summary export: %v", target, err)
		}
		fresh := CompileFor(source, target)
		if !fresh.Checked || fresh.projector.values == good.projector.values {
			t.Fatalf("%s fresh receiver arena: checked=%v diagnostics=%+v", target, fresh.Checked, fresh.Diagnostics)
		}
		if err := fresh.projector.admitInterfaceSummary(dto, fresh.Program.Functions); err != nil {
			t.Fatalf("%s private summary roundtrip: %v", target, err)
		}
		freshView := fresh.Program.Functions[functionIndex(fresh.Program.Functions, "fileView")]
		fieldContracts, ok := fresh.projector.applicationFields(freshView.returnID)
		var fieldContract TypeID
		for _, declared := range fieldContracts {
			if declared.Name == "run" {
				fieldContract = declared.typeID
			}
		}
		field = freshView.returnFields["run"]
		if !ok || fieldContract == invalidTypeID || field.contractID() != fieldContract || field.callableEvidence.count != 1 || field.callableEvidence.callees[0] != fresh.Program.Functions[functionIndex(fresh.Program.Functions, "echo")] || !slices.ContainsFunc(freshView.Ownership, func(fact OwnershipFact) bool { return fact.Path == "anchor" && fact.Status == "borrowed" }) {
			t.Fatalf("%s private transport lost exposed field contract, actual callee, or ownership: field=%+v ownership=%+v", target, field, freshView.Ownership)
		}
		if target == "go" {
			runGenericDataNative(t, source, "Ada\n")
		} else {
			_, declaration, err := good.Emit(false)
			if err != nil {
				t.Fatal(err)
			}
			checkStrictTypeScript(t, declaration, `import { widened } from "./generated.mjs";
import type { AuditRequirement } from "./generated.mjs";
import type { Effect } from "effect";
const operation = widened().run("Ada");
const exposed: Effect.Effect<string, { readonly _tag: "Trouble" }, AuditRequirement> = operation;
// @ts-expect-error Re-contracting cannot erase the declared callable rows.
const narrower: Effect.Effect<string, never, never> = operation;
void exposed; void narrower;`)
			if output := runJSForTarget(t, "js", source, `if (await Effect.runPromise(__ef_function_main()) !== "Ada") throw new Error("re-contracted factory result"); console.log("Ada");`); output != "Ada\n" {
				t.Fatalf("JS target factory execution: %q", output)
			}
		}
		for _, source := range []string{
			prefix + `fn bad(value:Pipe<effect fn(string)->string,string>)->` + wider + `{forward(value)}`,
			prefix + `fn bad(value:Pipe<effect fn(string)->string,string>)->` + wider + `{alias(value)}`,
			prefix + `fn bad(value:Pipe<effect fn(string)->string,string>,flag:bool)->` + wider + `{conditional(value,flag)}`,
		} {
			bad := CompileFor(source, target)
			if bad.Checked || !hasCode(bad, "EF106") {
				t.Fatalf("%s forwarding complete fields must not widen invariant application identity: checked=%v diagnostics=%+v", target, bad.Checked, bad.Diagnostics)
			}
		}
	}
}

func functionIndex(functions []*Function, name string) int {
	for i, function := range functions {
		if function.Name == name {
			return i
		}
	}
	return -1
}

func functionNamed(functions []*Function, name string) *Function {
	index := functionIndex(functions, name)
	if index < 0 {
		return nil
	}
	return functions[index]
}
