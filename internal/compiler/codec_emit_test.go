package compiler

import (
	goparser "go/parser"
	gotoken "go/token"
	"maps"
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

const codecEmissionTypes = `import Json "effra/json"

record User {
    id: i64
    name: string
}

record Archive {
    owner: User
    sealed: bool
}

enum Event {
    Created { user: User }
    Closed
}

derive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)
derive userMirror = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)
derive eventJson = Json.codec<Event>(maxBodyBytes: 1048576, maxDepth: 512)
derive archiveJson = Json.codec<Archive>(maxBodyBytes: 1048576, maxDepth: 512)
`

// codecUsingProgram executes userJson in both directions and eventJson's
// encode only; archiveJson is declared and never executed.
const codecUsingProgram = codecEmissionTypes + `
effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let user = run userJson.decode("{\"id\":\"7\",\"name\":\"Ada\"}")
    let closed = Event.Closed {}
    run userJson.encode(user) + run eventJson.encode(closed)
}
`

const codecIdleProgram = codecEmissionTypes + `
effect fn main() -> string {
    "ok"
}
`

func codecPlanByRoot(t *testing.T, r *Result, root string) *CodecPlan {
	t.Helper()
	for _, plan := range r.CodecPlans {
		if plan.Nodes[plan.Root].ID == dataIdentity(map[bool]string{true: "enum", false: "record"}[root == "Event"], root) {
			return plan
		}
	}
	t.Fatalf("no plan rooted at %s", root)
	return nil
}

// The application plan retains a codec plan, its nominal declarations, the
// codec runtime module and the lowering helpers only through an executed
// direction, and the emitted Go references exactly the selected modules.
func TestCodecPlansAreSelectedOnlyThroughExecutedDirections(t *testing.T) {
	r, plan := checkedApplicationPlan(t, codecUsingProgram, GoGenerationBuild)
	user, event, archive := codecPlanByRoot(t, r, "User"), codecPlanByRoot(t, r, "Event"), codecPlanByRoot(t, r, "Archive")
	if len(r.CodecPlans) != 3 {
		t.Fatalf("identical witnesses must share one plan: %d plans", len(r.CodecPlans))
	}
	requirePlanned(t, plan, RequiresCodecPlan, user.ID, event.ID)
	requireUnplanned(t, plan, RequiresCodecPlan, archive.ID)
	requirePlanned(t, plan, RequiresFunction, symbolIdentity(t, r, "userJson.decode"), symbolIdentity(t, r, "userJson.encode"), symbolIdentity(t, r, "eventJson.encode"))
	requireUnplanned(t, plan, RequiresFunction, symbolIdentity(t, r, "eventJson.decode"), symbolIdentity(t, r, "archiveJson.decode"), symbolIdentity(t, r, "archiveJson.encode"), symbolIdentity(t, r, "userMirror.decode"))
	requirePlanned(t, plan, RequiresDeclaration, dataIdentity("record", "User"), dataIdentity("enum", "Event"))
	requireUnplanned(t, plan, RequiresDeclaration, dataIdentity("record", "Archive"))
	requirePlanned(t, plan, RequiresHelper, codecLoweringHelper)
	if requirement, found := plan.Requirement(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)); !found || requirement.Via != user.ID || requirement.Reason != "codec-plan" {
		t.Fatalf("codec module provenance: %+v %v", requirement, found)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "main.go", goSource, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtimeReferenceModules(t, runtimeDeclarationModules(t), file); !maps.Equal(got, nonCoreModules(plan.RuntimeModules()...)) || !got[rt.RuntimeModuleCodec] {
		t.Fatalf("emitted runtime references %v, planned %v", got, plan.RuntimeModules())
	}
	sources, err := application.Plan.RuntimeSources()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := sources["codec_json.go"]; !found {
		t.Fatalf("application runtime omits the codec engine: %v", slices.Sorted(maps.Keys(sources)))
	}
	for _, want := range []string{"var " + goCodecPlanName(user), "var " + goCodecPlanName(event), "func efCodecConstruct_User(", "func " + goCodecEncodeName(event) + "("} {
		if strings.Count(goSource, want) != 1 {
			t.Fatalf("Go emission has %d of %q", strings.Count(goSource, want), want)
		}
	}
	for _, unwanted := range []string{goCodecPlanName(archive), goCodecEncodeName(archive), "efCodecConstruct_Archive", "efCodecFunction_archiveJson"} {
		if strings.Contains(goSource, unwanted) {
			t.Fatalf("Go emission retains unexecuted %s", unwanted)
		}
	}
}

// Declared but unexecuted witnesses retain nothing: the native application
// is the minimal one, without the codec module, helpers or plans.
func TestUnexecutedCodecsOmitTheCodecModule(t *testing.T) {
	r, plan := checkedApplicationPlan(t, codecIdleProgram, GoGenerationBuild)
	if len(plan.Identities(RequiresCodecPlan)) != 0 || plan.Requires(RequiresHelper, codecLoweringHelper) || plan.Requires(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)) {
		t.Fatalf("idle codecs retained: %+v", plan.Requirements)
	}
	goSource, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(goSource, "efCodec") || strings.Contains(goSource, "er.Codec") {
		t.Fatal("idle codecs emitted Go codec code")
	}
	sources, err := application.Plan.RuntimeSources()
	if err != nil {
		t.Fatal(err)
	}
	for _, unused := range []string{"codec.go", "codec_json.go"} {
		if _, found := sources[unused]; found {
			t.Fatalf("idle codecs selected %s", unused)
		}
	}
}

// The public decimal-string codec profile composes with checked signed64
// arithmetic without passing through JSON numbers or changing record/enum
// refusal behavior.
func TestI64CodecDecimalStringsComposeWithArithmeticOnBothTargets(t *testing.T) {
	const source = `import Json "effra/json"

record Account { balance: i64 }
enum Ledger { Posted { amount: i64 } Rejected { reason: string } }

derive i64Json = Json.codec<i64>(maxBodyBytes: 256, maxDepth: 4)
derive accountJson = Json.codec<Account>(maxBodyBytes: 256, maxDepth: 4)
derive ledgerJson = Json.codec<Ledger>(maxBodyBytes: 256, maxDepth: 4)

effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let minimum = run i64Json.decode("\"-9223372036854775808\"")
    let maximum = run i64Json.decode("\"9223372036854775807\"")
    let minimumWire = run i64Json.encode(minimum)
    let maximumWire = run i64Json.encode(maximum)
    let belowWire = run i64Json.encode(minimum - 1)
    let aboveWire = run i64Json.encode(maximum + 1)
    let leadingZero = run i64Json.decode("\"0009223372036854775807\"")
    let canonicalWire = run i64Json.encode(leadingZero)
    let account = run accountJson.decode("{\"balance\":\"9223372036854775807\"}")
    let nextAccount = Account { balance: account.balance + 1 }
    let accountWire = run accountJson.encode(nextAccount)
    let lowRefused = run i64Json.decode("\"-9223372036854775809\"").catch<JsonDecodeFailure>(101)
    let highRefused = run i64Json.decode("\"9223372036854775808\"").catch<JsonDecodeFailure>(102)
    let numberRefused = run i64Json.decode("9223372036854775807").catch<JsonDecodeFailure>(103)
    let lowRefusedWire = run i64Json.encode(lowRefused)
    let highRefusedWire = run i64Json.encode(highRefused)
    let numberRefusedWire = run i64Json.encode(numberRefused)
    let recordRefused = run accountJson.decode("{\"balance\":\"9223372036854775808\"}").catch<JsonDecodeFailure>(Account { balance: 104 })
    let recordRefusedWire = run accountJson.encode(recordRefused)
    let variantRefused = run ledgerJson.decode("{\"_tag\":\"Missing\"}").catch<JsonDecodeFailure>(Ledger.Rejected { reason: "sentinel" })
    let variantRefusedWire = run ledgerJson.encode(variantRefused)
    minimumWire + "|" + maximumWire + "|" + belowWire + "|" + aboveWire + "|" + canonicalWire + "|" + accountWire + "|" + lowRefusedWire + "|" + highRefusedWire + "|" + numberRefusedWire + "|" + recordRefusedWire + "|" + variantRefusedWire
}`
	const want = `"-9223372036854775808"|"9223372036854775807"|"9223372036854775807"|"-9223372036854775808"|"9223372036854775807"|{"balance":"-9223372036854775808"}|"101"|"102"|"103"|{"balance":"104"}|{"_tag":"Rejected","reason":"sentinel"}
`
	goResult := CompileFor(source, "go")
	if !goResult.Checked {
		t.Fatalf("Go rejected combined i64 codec source: %+v", goResult.Diagnostics)
	}
	application, err := goResult.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(writeGoApplication(t, goResult, application), "run", "."); err != nil || string(output) != want {
		t.Fatalf("Go codec/arithmetic result: %v\nwant %q\n got %q", err, want, output)
	}
}

// Emission is a function of the checked program: repeated emission is
// byte-identical.
func TestCodecEmissionIsDeterministic(t *testing.T) {
	emit := func() string {
		r := Compile(codecUsingProgram)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		goSource, _, err := emitGoApplication(r, GoGenerationBuild)
		if err != nil {
			t.Fatal(err)
		}
		return goSource
	}
	first := emit()
	for range 3 {
		if again := emit(); again != first {
			t.Fatal("codec emission is not deterministic")
		}
	}
}

// Direction functions are ordinary function values: bound to a local, passed
// to a typed parameter and forwarded through a bundled callback function.
func TestDerivedDirectionsAreOrdinaryFunctionValues(t *testing.T) {
	source := `import Json "effra/json"
import Fns "effra/functions"

derive textJson = Json.codec<string>(maxBodyBytes: 1048576, maxDepth: 512)

effect fn apply(decode: effect fn(string) -> string raises { JsonDecodeFailure }, input: string) -> string raises { JsonDecodeFailure } {
    run decode(input)
}

effect fn main() -> string raises { JsonDecodeFailure } {
    let decode = textJson.decode
    let direct = run decode("\"a\"")
    let passed = run apply(textJson.decode, "\"b\"")
    let forwarded = run Fns.call(textJson.decode, "\"c\"")
    direct + passed + forwarded
}
`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	application, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(writeGoApplication(t, r, application), "run", "."); err != nil || string(output) != "abc\n" {
		t.Fatalf("Go result: %v\n%s", err, output)
	}
}

// A witness named after a host global or a generated binding checks and
// runs: its source name never shadows a generated binding.
func TestCodecWitnessNamesDoNotShadowModuleBindings(t *testing.T) {
	for _, name := range []string{"Object", "TextEncoder", "Effect", "__ef_codecText"} {
		t.Run(name, func(t *testing.T) {
			source := `import Json "effra/json"

derive ` + name + ` = Json.codec<string>(maxBodyBytes: 1048576, maxDepth: 512)

effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    run ` + name + `.encode(run ` + name + `.decode("\"ok\""))
}
`
			r := Compile(source)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			application, err := r.GoApplication(GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			if output, err := runGoCommand(writeGoApplication(t, r, application), "run", "."); err != nil || string(output) != "\"ok\"\n" {
				t.Fatalf("Go result: %v\n%s", err, output)
			}
		})
	}
}
