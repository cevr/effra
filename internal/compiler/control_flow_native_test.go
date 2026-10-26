package compiler

import "testing"

// Native acceptance cases for if/match lowering. Each fixture builds and runs
// the emitted Go program and compares its exact stdout.

const controlFlowStatementsSource = `enum Notice { Fresh { value: string }; Stale { item: string }; Gone }
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

const controlFlowTailSource = `enum Shade { Warm { left: string }; Cool { right: string }; Dark }
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

const controlFlowSubjectFailureSource = `enum Light { Red; Green }
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

const controlFlowDiscardedFailureSource = `enum Choice { Go; Stop }
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

const nestedDirectMatchScopeSource = `enum Detail { One { text: string }; Two; Three }
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

const controlFlowNumericBranchesSource = `enum Arithmetic { Add; Negate }
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

func TestControlFlowStatementsContinueInTheEnclosingGenerator(t *testing.T) {
	want := "before\nchild\nthen\nafter-if\nbound-then\nbound-match\nsubject\npayload\nsubject\npayload\nafter-match\nbefore\nelse\nafter-if\nbound-else\nbound-match\nsubject\npayload\nsubject\npayload\nafter-match\ndone;done\n"
	runGenericDataNative(t, controlFlowStatementsSource, want)
}

func TestControlFlowTailAndCheckedMatchShapes(t *testing.T) {
	runGenericDataNative(t, controlFlowTailSource, "yes:no:cool:all\n")
}

func TestNestedDirectMatchesHaveIsolatedSubjectScopes(t *testing.T) {
	runGenericDataNative(t, nestedDirectMatchScopeSource, "two:nested\n")
}

func TestControlFlowSubjectFailureStopsLaterSubjects(t *testing.T) {
	runGenericDataNative(t, controlFlowSubjectFailureSource, "first\nblocked\n")
}

func TestControlFlowDiscardedMatchFailureStopsTheFollowingStatement(t *testing.T) {
	runGenericDataNative(t, controlFlowDiscardedFailureSource, "caught\n")
}

func TestControlFlowNumericBranchesPreservePerOperationWrapAcrossTargets(t *testing.T) {
	runGenericDataNative(t, controlFlowNumericBranchesSource, "ok\n")
}
