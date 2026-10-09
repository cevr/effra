package compiler

import (
	"strings"
	"testing"
)

const jsControlFlowStatementsSource = `enum Notice { Fresh { value: string }; Stale { item: string }; Gone }
effect fn child() -> void { run Console.log("child").provide<Console>(Stdout) }
effect fn event() -> Notice {
 run Console.log("subject").provide<Console>(Stdout)
 Notice.Stale { item: "payload" }
}
effect fn control(flag: bool) -> string {
 run Console.log("before").provide<Console>(Stdout)
 if flag {
  scope {
   let child = fork child()
   run child.join()
   run Console.log("then").provide<Console>(Stdout)
   "scoped"
  }
 } else {
  run Console.log("else").provide<Console>(Stdout)
  "else"
 }
 run Console.log("after-if").provide<Console>(Stdout)
 let bound = if flag { "bound-then" } else { "bound-else" }
 run Console.log(bound).provide<Console>(Stdout)
 let boundMatch = match Notice.Stale { item: "bound-match" } {
  Notice.Fresh { value: value } => value
  Notice.Stale { item: value } => value
  Notice.Gone => "gone"
 }
 run Console.log(boundMatch).provide<Console>(Stdout)
 match run event() {
  Notice.Fresh { value: value } | Notice.Stale { item: value } => run Console.log(value).provide<Console>(Stdout)
  Notice.Gone => run Console.log("gone").provide<Console>(Stdout)
 }
 match run event(), Notice.Fresh { value: "right" } {
  Notice.Stale { item: left }, Notice.Fresh | Notice.Gone => run Console.log(left).provide<Console>(Stdout)
  Notice.Stale, Notice.Stale { item: right } => run Console.log(right).provide<Console>(Stdout)
  Notice.Fresh | Notice.Gone, Notice.Fresh | Notice.Stale | Notice.Gone => run Console.log("other").provide<Console>(Stdout)
 }
 run Console.log("after-match").provide<Console>(Stdout)
 "done"
}
effect fn main() -> string {
 let first = run control(true)
 let second = run control(false)
 first + ";" + second
}`

const jsControlFlowTailSource = `enum Shade { Warm { left: string }; Cool { right: string }; Dark }
fn tail(flag: bool) -> string { if flag { "yes" } else { "no" } }
fn describe(shade: Shade) -> string {
 match shade {
  Shade.Warm { left: value } | Shade.Cool { right: value } => value
  Shade.Dark => "dark"
 }
}
fn every(shade: Shade) -> string { match shade { Shade.Warm | Shade.Cool | Shade.Dark => "all" } }
effect fn main() -> string {
 tail(true) + ":" + tail(false) + ":" + describe(Shade.Cool { right: "cool" }) + ":" + every(Shade.Dark {})
}`

const jsControlFlowSubjectFailureSource = `enum Light { Red; Green }
enum Signal { Stop; Go }
error Blocked { label: string }
effect fn first() -> Light raises {Blocked} {
 run Console.log("first").provide<Console>(Stdout)
 fail Blocked { label: "first" }
}
effect fn second() -> Signal {
 run Console.log("second").provide<Console>(Stdout)
 Signal.Go {}
}
effect fn decide() -> string raises {Blocked} {
 match run first(), run second() {
  Light.Red, Signal.Stop | Signal.Go => "red"
  Light.Green, Signal.Stop | Signal.Go => "green"
 }
}
effect fn main() -> string { run decide().catch<Blocked>("blocked") }`

const jsControlFlowDiscardedFailureSource = `enum Choice { Go; Stop }
error Blocked
effect fn probe() -> string raises {Blocked} {
 match Choice.Go {} {
  Choice.Go => fail Blocked
  Choice.Stop => "stopped"
 }
 run Console.log("after-match").provide<Console>(Stdout)
 "done"
}
effect fn main() -> string raises {Blocked} { run probe().catch<Blocked>("caught") }`

const jsNestedDirectMatchScopeSource = `enum Detail { One { text: string }; Two; Three }
enum Bit { Low; High }
enum Envelope { Payload { detail: Detail }; Missing }
fn switchThenChain(value: Envelope, bit: Bit) -> string {
 match value {
  Envelope.Payload { detail: detail } => match detail, bit {
   Detail.One { text: text }, Bit.Low | Bit.High => text
   Detail.Two, Bit.Low | Bit.High => "two"
   Detail.Three, Bit.Low | Bit.High => "three"
  }
  Envelope.Missing => "missing"
 }
}
fn chainThenSwitch(value: Envelope, bit: Bit) -> string {
 match value, bit {
  Envelope.Payload { detail: detail }, Bit.Low | Bit.High => match detail {
   Detail.One { text: text } => text
   Detail.Two => "two"
   Detail.Three => "three"
  }
  Envelope.Missing, Bit.Low | Bit.High => "missing"
 }
}
effect fn main() -> string {
 switchThenChain(Envelope.Payload { detail: Detail.Two {} }, Bit.Low {}) + ":" + chainThenSwitch(Envelope.Payload { detail: Detail.One { text: "nested" } }, Bit.High {})
}`

func TestJSControlFlowStatementsContinueInTheEnclosingGenerator(t *testing.T) {
	want := "before\nchild\nthen\nafter-if\nbound-then\nbound-match\nsubject\npayload\nsubject\npayload\nafter-match\nbefore\nelse\nafter-if\nbound-else\nbound-match\nsubject\npayload\nsubject\npayload\nafter-match\ndone;done\n"
	runOnEveryHost(t, jsControlFlowStatementsSource, want)

	js := emitControlFlowJS(t, jsControlFlowStatementsSource)
	control := jsControlFlowFunction(t, js, "control")
	for _, shape := range []string{
		"if (__ef_local_flag) {",
		"switch (__ef_match_0._tag) {",
		"case \"Notice.Fresh\":\ncase \"Notice.Stale\":",
		"__ef_match_0._tag === \"Notice.Fresh\" ? __ef_match_0[\"value\"] : __ef_match_0[\"item\"]",
		"else if ((__ef_match_0._tag === \"Notice.Stale\") && (__ef_match_1._tag === \"Notice.Stale\")) {",
		"break;",
		"const __ef_local_bound = (yield* Effect.gen(function* () {",
		"const __ef_local_boundMatch = (yield* Effect.gen(function* () {",
		"yield* __ef_scoped(Effect.gen(function*(){",
	} {
		if !strings.Contains(control, shape) {
			t.Errorf("control function lacks %q:\n%s", shape, control)
		}
	}
	if strings.Count(control, "yield* Effect.gen(function* () {") != 2 {
		t.Errorf("discarded if or match retained a nested generator beyond the two bound expressions:\n%s", control)
	}
	if strings.Count(control, "__ef_autoScope(Effect.gen(function* () {") != 1 {
		t.Errorf("outer automatic scope changed:\n%s", control)
	}
	if strings.Count(control, "const __ef_match_0 =") != 3 || !strings.Contains(control, "const __ef_match_1 =") {
		t.Errorf("sibling and product match subjects lost their block scopes:\n%s", control)
	}
}

func TestJSControlFlowTailAndCheckedMatchShapes(t *testing.T) {
	runOnEveryHost(t, jsControlFlowTailSource, "yes:no:cool:all\n")
	js := emitControlFlowJS(t, jsControlFlowTailSource)
	tail := jsControlFlowFunction(t, js, "tail")
	if !strings.Contains(tail, "if (__ef_local_flag) {") || strings.Contains(tail, "(() => {") {
		t.Errorf("tail if did not lower directly:\n%s", tail)
	}
	describe := jsControlFlowFunction(t, js, "describe")
	if !strings.Contains(describe, "switch (__ef_match_0._tag) {") || !strings.Contains(describe, "case \"Shade.Warm\":\ncase \"Shade.Cool\":") {
		t.Errorf("checked single-subject or-pattern did not lower as a tag switch:\n%s", describe)
	}
	if strings.Contains(describe, "(() => {") {
		t.Errorf("tail match retained its expression wrapper:\n%s", describe)
	}
	every := jsControlFlowFunction(t, js, "every")
	if strings.Contains(every, "switch (") || !strings.Contains(every, "if (true) {") {
		t.Errorf("total match cell did not stay on the general checked path:\n%s", every)
	}

	product := emitControlFlowJS(t, orderedSubjectsSource)
	decide := jsControlFlowFunction(t, product, "decide")
	if strings.Contains(decide, "switch (") || strings.Count(decide, "const __ef_match_") != 2 || !strings.Contains(decide, "if (") {
		t.Errorf("multi-subject plan did not preserve the ordered condition-chain fallback:\n%s", decide)
	}
	if strings.Contains(decide, "yield* Effect.gen(function* () {") {
		t.Errorf("tail multi-subject match retained a nested generator:\n%s", decide)
	}
}

func TestJSNestedDirectMatchesHaveIsolatedSubjectScopes(t *testing.T) {
	runOnEveryHost(t, jsNestedDirectMatchScopeSource, "two:nested\n")
	js := emitControlFlowJS(t, jsNestedDirectMatchScopeSource)

	switchThenChain := jsControlFlowFunction(t, js, "switchThenChain")
	if strings.Count(switchThenChain, "switch (") != 1 || !strings.Contains(switchThenChain, "else if (") {
		t.Errorf("nested match did not preserve switch-to-condition-chain lowering:\n%s", switchThenChain)
	}
	if !strings.Contains(switchThenChain, "const __ef_local_detail = __ef_match_0[\"detail\"];\n{\nconst __ef_match_0 = __ef_local_detail;") {
		t.Errorf("nested condition-chain match lacks an isolated subject scope:\n%s", switchThenChain)
	}

	chainThenSwitch := jsControlFlowFunction(t, js, "chainThenSwitch")
	if strings.Count(chainThenSwitch, "switch (") != 1 || !strings.Contains(chainThenSwitch, "else if (") {
		t.Errorf("nested match did not preserve condition-chain-to-switch lowering:\n%s", chainThenSwitch)
	}
	if !strings.Contains(chainThenSwitch, "const __ef_local_detail = __ef_match_0[\"detail\"];\n{\nconst __ef_match_0 = __ef_local_detail;") {
		t.Errorf("nested switch match lacks an isolated subject scope:\n%s", chainThenSwitch)
	}
}

func TestJSControlFlowSubjectFailureStopsLaterSubjects(t *testing.T) {
	runOnEveryHost(t, jsControlFlowSubjectFailureSource, "first\nblocked\n")
}

func TestJSControlFlowDiscardedMatchFailureStopsTheFollowingStatement(t *testing.T) {
	runOnEveryHost(t, jsControlFlowDiscardedFailureSource, "caught\n")
	js := emitControlFlowJS(t, jsControlFlowDiscardedFailureSource)
	probe := jsControlFlowFunction(t, js, "probe")
	switchAt := strings.Index(probe, "switch (__ef_match_0._tag) {")
	afterAt := strings.Index(probe, "after-match")
	if switchAt < 0 || afterAt < switchAt {
		t.Errorf("discarded match did not keep its failing branch in the enclosing effect body:\n%s", probe)
	}
}

func TestJSNullaryHostConstructorsRemainFreshAndMutable(t *testing.T) {
	const source = `enum Token { Empty }
fn empty() -> Token { Token.Empty {} }
effect fn main() -> string { "ok" }`
	assertions := `const first=__ef_function_empty(),second=__ef_function_empty();
if(first===second)throw new Error("nullary host objects were shared");
first.hostMutation=true;
if(second.hostMutation!==undefined||!Object.isExtensible(first)||!Object.isExtensible(second))throw new Error("nullary host mutation leaked or object was frozen");
const keyed=new Set([first,second]),weak=new WeakMap([[first,"first"],[second,"second"]]);
if(keyed.size!==2||weak.get(first)!=="first"||weak.get(second)!=="second")throw new Error("nullary host identity changed");
console.log("fresh mutable nullary objects");`
	for _, host := range jsHosts {
		output, err := runJSHost(t, host, source, assertions)
		if err != nil || output != "fresh mutable nullary objects\n" {
			t.Fatalf("%s nullary host identity: %v\n%s", host, err, output)
		}
	}
}

func emitControlFlowJS(t *testing.T, source string) string {
	t.Helper()
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatalf("control-flow source did not check: %+v", r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func jsControlFlowFunction(t *testing.T, js, name string) string {
	t.Helper()
	marker := "const __ef_function_" + name + " ="
	start := strings.Index(js, marker)
	if start < 0 {
		t.Fatalf("emitted JS lacks %s", marker)
	}
	end := strings.Index(js[start+len(marker):], "\nconst __ef_function_")
	if end < 0 {
		return js[start:]
	}
	return js[start : start+len(marker)+end]
}

const jsControlFlowNumericBranchesSource = `enum Arithmetic { Add; Negate }
fn branchArithmetic(flag: bool, left: i64, right: i64) -> bool {
 if flag {
  left + right == -9223372036854775808
 } else {
  left - right == 9223372036854775807
 }
}
fn matchArithmetic(operation: Arithmetic, left: i64, right: i64) -> bool {
 match operation {
  Arithmetic.Add => left + right == -9223372036854775808
  Arithmetic.Negate => -left == -9223372036854775808
 }
}
effect fn main() -> string {
 if branchArithmetic(true, 9223372036854775807, 1) {
  if branchArithmetic(false, -9223372036854775808, 1) {
   if matchArithmetic(Arithmetic.Add {}, 9223372036854775807, 1) {
    if matchArithmetic(Arithmetic.Negate {}, -9223372036854775808, 0) {
     "ok"
    } else { "match unary wrap" }
   } else { "match addition wrap" }
  } else { "if subtraction wrap" }
 } else { "if addition wrap" }
}`

func TestJSControlFlowNumericBranchesPreservePerOperationWrapAcrossTargets(t *testing.T) {
	runOnEveryHost(t, jsControlFlowNumericBranchesSource, "ok\n")
	js := emitControlFlowJS(t, jsControlFlowNumericBranchesSource)

	branch := jsControlFlowFunction(t, js, "branchArithmetic")
	if !strings.Contains(branch, "if (__ef_local_flag) {") || strings.Contains(branch, "(() => {") ||
		!strings.Contains(branch, "BigInt.asIntN(64, (__ef_local_left + __ef_local_right))") ||
		!strings.Contains(branch, "BigInt.asIntN(64, (__ef_local_left - __ef_local_right))") {
		t.Errorf("direct if branch lost its checked signed64 operations:\n%s", branch)
	}

	match := jsControlFlowFunction(t, js, "matchArithmetic")
	if !strings.Contains(match, "switch (__ef_match_0._tag) {") || strings.Contains(match, "(() => {") ||
		!strings.Contains(match, "BigInt.asIntN(64, (__ef_local_left + __ef_local_right))") ||
		!strings.Contains(match, "BigInt.asIntN(64, (-__ef_local_left))") {
		t.Errorf("direct checked match lost its switch or per-operation signed64 operations:\n%s", match)
	}
}
