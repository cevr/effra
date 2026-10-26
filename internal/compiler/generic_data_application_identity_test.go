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
	for _, source := range []string{
		phantomCallableIdentitySource + `fn bad()->string{consume(narrow())}`,
		phantomCallableIdentitySource + `fn bad()->Phantom<effect fn(string)->string raises {Trouble},string>{narrow()}`,
	} {
		r := CompileFor(source, "go")
		if r.Checked || !hasCode(r, "EF106") {
			t.Fatalf("go widened a complete generic application: checked=%v diagnostics=%+v", r.Checked, r.Diagnostics)
		}
	}
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
	if r := CompileFor(source, "go"); !r.Checked {
		t.Fatalf("go explicit exact Codec application rejected narrower field callbacks: %+v", r.Diagnostics)
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
	good := CompileFor(source, "go")
	if !good.Checked {
		t.Fatalf("go direct factory construction should satisfy checked callable field upper bound: %+v", good.Diagnostics)
	}
	factoryCall, err := good.TypeAt(strings.Index(source, "{factory()}") + 1)
	if err != nil || factoryCall.Type.Contract.Kind != "application" || factoryCall.Type.Application == nil || factoryCall.Type.Application.ProducedResult == nil || factoryCall.Type.Application.Result.ID != factoryCall.Type.Contract.ID || factoryCall.Type.Application.ProducedResult.ID == factoryCall.Type.Application.Result.ID {
		t.Fatalf("go factory boundary did not publish its checked result and produced identity: info=%+v err=%v", factoryCall, err)
	}
	projection := good.ProjectExpression(factoryCall)
	if !projection.Complete {
		t.Fatalf("go factory expression projection was refused: %+v", projection)
	}
	projected := map[string]TypeNode{}
	for _, node := range projection.Types {
		projected[node.ID] = node
	}
	producedPublicID := factoryCall.Type.Application.ProducedResult.ID
	if _, ok := projected[producedPublicID]; !ok {
		t.Fatalf("go selected expression projection omitted produced result %s", producedPublicID)
	}
	projectedProduced := projected[producedPublicID]
	for _, childID := range projectedProduced.Args {
		if _, ok := projected[childID]; !ok {
			t.Fatalf("go produced result projection omitted argument %s", childID)
		}
	}
	producedID := good.projector.typePublicToID[factoryCall.Type.Application.ProducedResult.ID]
	resultID := good.projector.typePublicToID[factoryCall.Type.Application.Result.ID]
	producedNode, resultNode := good.projector.node(producedID), good.projector.node(resultID)
	if producedNode == nil || resultNode == nil || len(producedNode.Args) != 2 || len(resultNode.Args) != 2 || slices.Equal(producedNode.Args, resultNode.Args) {
		t.Fatalf("go produced and exposed application rows were not distinct canonical identities: produced=%+v exposed=%+v", producedNode, resultNode)
	}
	producedCallable, exposedCallable := good.projector.node(producedNode.Args[0]), good.projector.node(resultNode.Args[0])
	if producedCallable == nil || exposedCallable == nil || len(good.projector.rowLabels(producedCallable.FailureRow)) != 0 || !slices.Equal(good.projector.rowLabels(exposedCallable.FailureRow), []string{"Trouble"}) || !slices.Equal(good.projector.rowLabels(exposedCallable.ServiceRow), []string{"Audit"}) {
		t.Fatalf("go produced and exposed callable rows differ from checked factory contract: produced=%+v exposed=%+v", producedCallable, exposedCallable)
	}
	callable, err := good.TypeAt(strings.Index(source, `run("Ada")`))
	if err != nil || !slices.Equal(callable.Type.Errors, []string{"Trouble"}) || !slices.Equal(callable.Type.Services, []string{"Audit"}) {
		t.Fatalf("go application caller lost exposed failure/service rows: info=%+v err=%v", callable, err)
	}
	ordered := good.Find("orderedMake")
	if ordered == nil || !slices.Equal(ordered.Actual.Errors, []string{"SetupFailure"}) || !slices.Equal(ordered.Actual.Services, []string{"Setup"}) {
		t.Fatalf("go preceding execution rows were lost at factory return: %+v", ordered)
	}
	orderedCall, err := good.TypeAt(strings.Index(source[strings.Index(source, "orderedMake"):], "factory()") + strings.Index(source, "orderedMake") + 1)
	if err != nil || len(orderedCall.Evaluation.Failures) != 0 || len(orderedCall.Evaluation.Requirements) != 0 {
		t.Fatalf("go earlier block evaluation leaked onto the returned factory occurrence: info=%+v err=%v", orderedCall, err)
	}
	fileView := good.checkedFunctions[functionNamed(good.Program.Functions, "fileView")].body
	if !slices.ContainsFunc(fileView.ownershipFacts(), func(fact OwnershipFact) bool { return fact.Path == "anchor" && fact.Status == "borrowed" }) {
		t.Fatalf("go re-contract discarded borrowed field ownership: %+v", fileView.ownershipFacts())
	}
	capturedCall, err := good.TypeAt(strings.Index(source, "view.run(file)") + len("view."))
	// echo returns its argument without dereferencing it: the run
	// result is owned by the argument's owner and the recipe holds
	// no use obligation (held = dereference obligations).
	if err != nil || !slices.Equal(capturedCall.Type.Errors, []string{"Trouble"}) || !slices.Equal(capturedCall.Type.Services, []string{"Audit"}) || !slices.ContainsFunc(capturedCall.Type.Ownership, func(fact OwnershipFact) bool { return fact.Region == "parameter:file" && fact.Status == "borrowed" }) || len(capturedCall.Type.Captures) != 0 {
		t.Fatalf("go callable invocation after re-contract lost rows or argument owner evidence: info=%+v err=%v", capturedCall, err)
	}
	field := fileView.fields["run"]
	fileFactory := good.checkedFunctions[functionNamed(good.Program.Functions, "fileFactory")].body.fields["run"]
	if field.valueID() == invalidTypeID || !slices.Equal(field.captureFacts(), fileFactory.captureFacts()) {
		t.Fatalf("go re-contract discarded callable field capture facts: %+v", field.captureFacts())
	}
	dto, err := exportInterfaceSummary(good.projector, currentModuleIdentity, good.Revision, good.Program.Functions)
	if err != nil {
		t.Fatalf("go private summary export: %v", err)
	}
	fresh := CompileFor(source, "go")
	if !fresh.Checked || fresh.projector.values == good.projector.values {
		t.Fatalf("go fresh receiver arena: checked=%v diagnostics=%+v", fresh.Checked, fresh.Diagnostics)
	}
	if err := fresh.projector.admitInterfaceSummary(dto, fresh.Program.Functions); err != nil {
		t.Fatalf("go private summary roundtrip: %v", err)
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
		t.Fatalf("go private transport lost exposed field contract, actual callee, or ownership: field=%+v ownership=%+v", field, freshView.Ownership)
	}
	runGenericDataNative(t, source, "Ada\n")
	for _, source := range []string{
		prefix + `fn bad(value:Pipe<effect fn(string)->string,string>)->` + wider + `{forward(value)}`,
		prefix + `fn bad(value:Pipe<effect fn(string)->string,string>)->` + wider + `{alias(value)}`,
		prefix + `fn bad(value:Pipe<effect fn(string)->string,string>,flag:bool)->` + wider + `{conditional(value,flag)}`,
	} {
		bad := CompileFor(source, "go")
		if bad.Checked || !hasCode(bad, "EF106") {
			t.Fatalf("go forwarding complete fields must not widen invariant application identity: checked=%v diagnostics=%+v", bad.Checked, bad.Diagnostics)
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
