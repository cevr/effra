package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// The 19-bit pure self-tail counter reaches its terminal state after more
// than a million transitions. Reordered labels exercise the checked argument
// mapping on the same deep path as the Go stack boundary.

// rippleCounter lowers one increment of an n-bit counter named b0..b(n-1) into
// nested ifs. call receives the next bit values (source expressions) and
// returns the tail expression for them; done is the all-ones result.
func rippleCounter(bits int, done string, call func(next []string) string) string {
	var build func(bit int) string
	build = func(bit int) string {
		if bit == bits {
			return done
		}
		// Bit is clear: it becomes set and every lower bit (all set) clears.
		clear := make([]string, 0, bits)
		for lower := 0; lower < bit; lower++ {
			clear = append(clear, "false")
		}
		clear = append(clear, "true")
		for rest := bit + 1; rest < bits; rest++ {
			clear = append(clear, fmt.Sprintf("b%d", rest))
		}
		return fmt.Sprintf("if b%d { %s } else { %s }", bit, build(bit+1), call(clear))
	}
	return build(0)
}

func counterParams(bits int) string {
	parts := make([]string, bits)
	for i := range parts {
		parts[i] = fmt.Sprintf("b%d: bool", i)
	}
	return strings.Join(parts, ", ")
}

func counterZero(bits int) string {
	return strings.TrimSuffix(strings.Repeat("false, ", bits), ", ")
}

func labelledSpinCall(phase string, bits []string) string {
	args := []string{"phase: " + phase}
	for bit := len(bits) - 1; bit >= 0; bit-- {
		args = append(args, fmt.Sprintf("b%d: %s", bit, bits[bit]))
	}
	return "spin(" + strings.Join(args, ", ") + ")"
}

// deepTailSource is a pure self tail call through an if ladder inside a match
// arm: Tick increments, Tock re-enters with the same bits, so it makes
// 2*(2^bits-1) calls.
func deepTailSource(bits int) string {
	tick := rippleCounter(bits, `"done"`, func(next []string) string {
		return labelledSpinCall("Phase.Tock {}", next)
	})
	names := make([]string, bits)
	for i := range names {
		names[i] = fmt.Sprintf("b%d", i)
	}
	return fmt.Sprintf(`enum Phase { Tick; Tock }

fn spin(phase: Phase, %s) -> string {
    match phase {
        Phase.Tick => %s
        Phase.Tock => %s
    }
}

effect fn main() -> string {
    spin(Phase.Tick {}, %s)
}
`, counterParams(bits), tick, labelledSpinCall("Phase.Tick {}", names), counterZero(bits))
}

func TestDeepSelfTailRecursionRunsOnBothTargets(t *testing.T) {
	// 2^19-1 increments, two calls each: more than 10^6 nested calls.
	source := deepTailSource(19)
	runGenericDataNative(t, source, "done\n")
}

// rotationSource rotates three strings through an n-bit counter. Each call
// passes the old b and c forward and builds the new c from the old a and b, so
// a call that reassigned a parameter before evaluating every argument would
// change the result. The arguments are also if and match expressions that read
// the parameters.
func rotationSource(bits int) string {
	call := rippleCounter(bits, "a + b + c", func(next []string) string {
		return fmt.Sprintf(`rot(b, if flag { c } else { a }, match step { Step.Even => a + b; Step.Odd => b + c }, flip(step), %s)`, strings.Join(next, ", "))
	})
	return fmt.Sprintf(`enum Step { Even; Odd }
fn flip(step: Step) -> Step {
    match step {
        Step.Even => Step.Odd {}
        Step.Odd => Step.Even {}
    }
}
fn rot(a: string, b: string, c: string, step: Step, %s) -> string {
    let flag = b == c
    %s
}
effect fn main() -> string {
    rot("a", "b", "c", Step.Even {}, %s)
}
`, counterParams(bits), call, counterZero(bits))
}

// rotationExpected replays rotationSource with ordinary Go variables, taking
// every argument from the previous iteration's values.
func rotationExpected(bits int) string {
	a, b, c, even := "a", "b", "c", true
	for count := 1<<bits - 1; count > 0; count-- {
		flag := b == c
		next := c
		if !flag {
			next = a
		}
		third := b + c
		if even {
			third = a + b
		}
		a, b, c, even = b, next, third, !even
	}
	return a + b + c
}

func TestTailCallArgumentsAreEvaluatedBeforeParametersChange(t *testing.T) {
	for _, bits := range []int{1, 2, 4} {
		t.Run(fmt.Sprint(bits), func(t *testing.T) {
			runGenericDataNative(t, rotationSource(bits), rotationExpected(bits)+"\n")
		})
	}
}

// nonTailSource keeps its self call out of tail position: the result is
// prefixed after the call returns, so the function cannot become a loop.
func nonTailSource(bits int) string {
	body := rippleCounter(bits, `"done"`, func(next []string) string {
		return `"x" + deep(` + strings.Join(next, ", ") + `)`
	})
	return fmt.Sprintf(`fn deep(%s) -> string {
    %s
}
effect fn main() -> string {
    deep(%s)
}
`, counterParams(bits), body, counterZero(bits))
}

func TestNonTailSelfCallStaysRecursive(t *testing.T) {
	small := nonTailSource(3)
	runGenericDataNative(t, small, strings.Repeat("x", 7)+"done\n")
	r := CompileFor(small, "go")
	if lowerTail(r.Find("deep").function(t, r)) != nil {
		t.Fatal("a call whose result is used was planned as a tail call")
	}
	native, _, err := emitGoApplication(CompileFor(small, "go"), GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(native, "for {") {
		t.Fatalf("non-tail recursion was lowered to a loop:\n%s", native)
	}
}

// function is the checked function a symbol names.
func (s *Symbol) function(t *testing.T, r *Result) *Function {
	t.Helper()
	for _, f := range r.Program.Functions {
		if f.Name == s.Name {
			return f
		}
	}
	t.Fatalf("no function %s", s.Name)
	return nil
}

const tailVoidAndDataSource = `import Data "ergo/data"

fn pick(first: Data.Option<string>, second: Data.Option<string>, again: bool) -> string {
    if again {
        match first {
            Data.Option.Some { value } => value
            Data.Option.None => "none"
        }
    } else {
        pick(second, first, true)
    }
}
fn settle(finished: bool) -> void {
    if finished {
        void
    } else {
        settle(true)
    }
}
effect fn main() -> string {
    settle(false)
    let kept = pick(Data.Option.Some { value: "kept" }, Data.Option<string>.None {}, true)
    let swapped = pick(Data.Option.Some { value: "lost" }, Data.Option<string>.None {}, false)
    kept + " " + swapped
}
`

func TestTailLoopsHandleVoidResultsAndGenericDataParameters(t *testing.T) {
	runGenericDataNative(t, tailVoidAndDataSource, "kept none\n")
	for _, name := range []string{"pick", "settle"} {
		r := CompileFor(tailVoidAndDataSource, "go")
		if lowerTail(r.Find(name).function(t, r)) == nil {
			t.Fatalf("%s has a self tail call but was not planned as a loop", name)
		}
	}
}

const tailSelectivitySource = `error Stop
fn even(n: bool) -> bool {
    if n {
        true
    } else {
        odd(true)
    }
}
fn odd(n: bool) -> bool {
    if n {
        false
    } else {
        even(true)
    }
}
fn inner(n: bool) -> bool {
    if n {
        true
    } else {
        inner(true) == true
    }
}
fn branches(n: bool) -> bool {
    let unused = n
    if n {
        true
    } else {
        branches(true)
    }
}
effect fn recurse(n: bool) -> bool {
    if n {
        true
    } else {
        run recurse(true)
    }
}
effect fn main() -> bool {
    let reached = even(false) == inner(false)
    let loop = branches(false)
    run recurse(false)
}
`

func TestOnlyPureDirectSelfTailCallsArePlanned(t *testing.T) {
	r := CompileFor(tailSelectivitySource, "go")
	if !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	for name, want := range map[string]bool{
		"even":     false, // mutual recursion stays recursive
		"odd":      false,
		"inner":    false, // the call feeds an operator
		"branches": true,
		"recurse":  false, // effect functions are out of scope
		"main":     false,
	} {
		if got := lowerTail(r.Find(name).function(t, r)) != nil; got != want {
			t.Errorf("%s planned as a loop = %v, want %v", name, got, want)
		}
	}
	native, _, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(native, "for {"); got != 1 {
		t.Fatalf("expected exactly one generated loop (branches), got %d:\n%s", got, native)
	}
}

func TestParametersCannotBeShadowed(t *testing.T) {
	// The loop gives each iteration fresh parameter bindings, so the lowering
	// has no shadowing to preserve: a local may not reuse a parameter's name.
	r := Compile(`fn f(n: string, m: string) -> string {
    let n = n + "!"
    if m == "" {
        n
    } else {
        f(n, "")
    }
}`)
	if r.Checked || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "EF101" {
		t.Fatalf("shadowing a parameter was admitted: %+v", r.Diagnostics)
	}
}

const tailGoldenSource = `fn settle(a: string, b: string, finished: bool) -> string {
    if finished {
        a + b
    } else {
        settle(b, a + b, true)
    }
}
effect fn main() -> string {
    settle("x", "y", false)
}
`

const tailGoldenGo = `func efFunction_settle(efArg_a string, efArg_b string, efArg_finished bool) string {
	for {
		efLocal_a := efArg_a
		_ = efLocal_a
		efLocal_b := efArg_b
		_ = efLocal_b
		efLocal_finished := efArg_finished
		_ = efLocal_finished
		if efLocal_finished {
			efTemp1 := efLocal_a
			return (efTemp1 + efLocal_b)
		} else {
			efTemp2 := efLocal_b
			efTemp3 := efLocal_a
			efTemp4 := (efTemp3 + efLocal_b)
			efTemp5 := true
			efArg_a = efTemp2
			efArg_b = efTemp4
			efArg_finished = efTemp5
			continue
		}
	}
}
`

// The goldens pin the shape the lowering must keep: every argument is
// evaluated into a temporary, left to right, before any parameter is
// reassigned, and the body reads per-iteration bindings.
func TestTailLoopEmissionGolden(t *testing.T) {
	native, _, err := emitGoApplication(CompileFor(tailGoldenSource, "go"), GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if start := strings.Index(native, "func efFunction_settle"); start < 0 || !strings.HasPrefix(native[start:], tailGoldenGo) {
		t.Fatalf("Go loop changed:\n%s", native)
	}
}

const labelledNumericTailSource = `fn decrement(value: i64) -> i64 { value - 1 }
fn carry(value: i64) -> i64 { value }
fn walk(n: i64, low: i64, high: i64) -> i64 {
    if n == 0 {
        low
    } else {
        walk(n: decrement(n), high: low + high, low: carry(high))
    }
}
effect fn main() -> string {
    if walk(2, 9223372036854775807, 1) == -9223372036854775808 { "ok" } else { "wrong" }
}
`

// Pure argument expressions cannot expose their evaluation order through a
// side effect. This control checks the emitted source-ordered, once-only calls,
// then exercises labelled slot mapping and signed wrap on Go.
// Existing LOW0 positional tails and numeric ordinary calls do not combine
// these contracts; the control fails if tail lowering reorders or duplicates an
// argument, uses source indexes as parameter slots, or drops i64 normalization.
// No production test seam is needed.
func TestLabelledNumericSelfTailLoopPreservesOrderSlotsAndWrap(t *testing.T) {
	result := CompileFor(labelledNumericTailSource, "go")
	if !result.Checked {
		t.Fatalf("labelled numeric self-tail source rejected: %+v", result.Diagnostics)
	}
	walk := result.Find("walk").function(t, result)
	if lowerTail(walk) == nil {
		t.Fatal("direct labelled self-tail call was not planned as a loop")
	}
	var tailCall *Expr
	for _, expression := range expressionsOf(result.Program) {
		if expression.Kind == "call" && expression.ResolvedFunction == walk {
			tailCall = expression
			break
		}
	}
	if tailCall == nil {
		t.Fatal("checked self-tail call was not found")
	}
	if got := tailCall.ArgumentParameters; len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("authored argument-to-parameter map = %v, want [0 2 1]", got)
	}
	canonical := tailCall.parameterArguments()
	if len(canonical) != 3 || canonical[0] != tailCall.Args[0] || canonical[1] != tailCall.Args[2] || canonical[2] != tailCall.Args[1] {
		t.Fatalf("canonical tail arguments do not follow the checked label map: %v", canonical)
	}

	runGenericDataNative(t, labelledNumericTailSource, "ok\n")

	decrement := result.Find("decrement").function(t, result)
	carry := result.Find("carry").function(t, result)
	paramName := func(index int) string { return walk.Params[index].Name }

	native, _, err := emitGoApplication(result, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	goMarker := "func " + walk.goEmissionName() + "("
	goStart := strings.Index(native, goMarker)
	if goStart < 0 {
		t.Fatalf("Go backend omitted the walk function:\n%s", native)
	}
	goRest := native[goStart+len(goMarker):]
	goEnd := strings.Index(goRest, "\nfunc ")
	if goEnd < 0 {
		t.Fatalf("could not isolate generated Go walk function:\n%s", native)
	}
	walkGo := native[goStart : goStart+len(goMarker)+goEnd]
	type goTempAssignment struct {
		line       int
		name, expr string
	}
	var goTemps []goTempAssignment
	for lineNumber, line := range strings.Split(walkGo, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " := ", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "efTemp") {
			continue
		}
		if suffix := strings.TrimPrefix(parts[0], "efTemp"); suffix == "" || strings.Trim(suffix, "0123456789") != "" {
			continue
		}
		goTemps = append(goTemps, goTempAssignment{line: lineNumber, name: parts[0], expr: parts[1]})
	}
	findGoTemp := func(label string, matches func(goTempAssignment) bool) goTempAssignment {
		t.Helper()
		var found goTempAssignment
		count := 0
		for _, assignment := range goTemps {
			if matches(assignment) {
				found = assignment
				count++
			}
		}
		if count != 1 {
			t.Fatalf("Go %s materialization occurs %d times, want once:\n%s", label, count, walkGo)
		}
		return found
	}
	decrementEval := findGoTemp("decrement call", func(assignment goTempAssignment) bool {
		return strings.HasPrefix(assignment.expr, decrement.goEmissionName()+"(")
	})
	decrementArgs := strings.TrimSuffix(strings.TrimPrefix(decrementEval.expr, decrement.goEmissionName()+"("), ")")
	decrementInput := findGoTemp("decrement input", func(assignment goTempAssignment) bool {
		return assignment.expr == "efLocal_"+paramName(0) && assignment.name == decrementArgs
	})
	if decrementEval.line <= decrementInput.line || decrementArgs != decrementInput.name {
		t.Fatalf("Go decrement did not consume its materialized input after evaluating it:\n%s", walkGo)
	}
	additionInput := findGoTemp("addition left operand", func(assignment goTempAssignment) bool {
		return assignment.expr == "efLocal_"+paramName(1)
	})
	additionEval := findGoTemp("labelled addition", func(assignment goTempAssignment) bool {
		return assignment.expr == "("+additionInput.name+" + efLocal_"+paramName(2)+")"
	})
	if additionEval.line <= additionInput.line {
		t.Fatalf("Go addition ran before its materialized left operand:\n%s", walkGo)
	}
	carryEval := findGoTemp("carry call", func(assignment goTempAssignment) bool {
		return strings.HasPrefix(assignment.expr, carry.goEmissionName()+"(")
	})
	carryArgs := strings.TrimSuffix(strings.TrimPrefix(carryEval.expr, carry.goEmissionName()+"("), ")")
	carryInput := findGoTemp("carry input", func(assignment goTempAssignment) bool {
		return assignment.expr == "efLocal_"+paramName(2) && assignment.name == carryArgs
	})
	if carryEval.line <= carryInput.line || carryArgs != carryInput.name {
		t.Fatalf("Go carry call did not consume its materialized input after evaluating it:\n%s", walkGo)
	}
	if !(decrementEval.line < additionEval.line && additionEval.line < carryEval.line) {
		t.Fatalf("Go did not evaluate labelled tail operands in authored order:\n%s", walkGo)
	}
	goAssignments := []struct {
		parameter string
		value     string
	}{
		{paramName(0), decrementEval.name},
		{paramName(1), carryEval.name},
		{paramName(2), additionEval.name},
	}
	lastGoEvaluation := strings.Index(walkGo, carryEval.name+" := "+carryEval.expr)
	if lastGoEvaluation < 0 {
		t.Fatalf("could not locate final Go tail operand materialization:\n%s", walkGo)
	}
	lastGoAssignment := lastGoEvaluation
	for _, assignment := range goAssignments {
		line := "efArg_" + assignment.parameter + " = " + assignment.value
		position := strings.Index(walkGo, line)
		if position < 0 || position <= lastGoAssignment {
			t.Fatalf("Go canonical slot %q was not assigned from its authored operand after all evaluations:\n%s", assignment.parameter, walkGo)
		}
		lastGoAssignment = position
	}
	if lastGoAssignment <= lastGoEvaluation {
		t.Fatalf("Go updated loop state before evaluating every argument:\n%s", walkGo)
	}
}
