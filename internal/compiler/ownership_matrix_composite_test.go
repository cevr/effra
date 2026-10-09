// Ownership matrix: composite causes at owner edges (lane E2 R5 fix round r1).
//
// The runtime raises a closing owner's failure as the sequence of its body's
// failure and the causes of the children it closes over, and recover and
// catch handle only a solitary typed failure. A failing exit taken while a
// child is unobserved, or two failing children, therefore reach the owner as
// a composite cause which no row declares: the program exits 1 with an
// undeclared failure. Each program below forks failing children and sleeps
// before the exit, so the children have failed when the owner closes. The
// controls close over exactly one failure and are handled by the catches.

package compiler

import "strconv"

const ownershipMatrixCompositeHead = `enum Pick { A, B }
error Boom { code: i64 }
error Nope { code: i64 }
effect fn job() -> string raises { Boom } {
    fail Boom { code: 7 }
}
effect fn job2() -> string raises { Nope } {
    fail Nope { code: 1 }
}
effect fn bail() -> string raises { Nope } {
    fail Nope { code: 1 }
}
effect fn spawn() -> string raises { Boom } uses { Scheduler } {
    let g = fork job()
    run Scheduler.sleep(1)
    "spawned"
}
`

// ownershipMatrixCompositePrograms returns the composite-cause programs.
// stage is the body of the staged effect, which runs under the owner named
// by wrap; flag selects the executed path of the programs with a branch.
func ownershipMatrixCompositePrograms() []ownershipMatrixProgram {
	type shape struct {
		name   string
		stage  string
		wrap   string // consume's body around `run stage(flag)`
		raises string // the failures stage declares
		oracle string
		flag   bool
	}
	scoped := "scope { run stage(flag) }"
	shapes := []shape{
		// A failing exit taken while a child is unobserved.
		{"body_child", "let g = fork job()\n    run Scheduler.sleep(30)\n    fail Nope { code: 1 }", scoped, "Nope, Boom", "row-unsafe", true},
		{"body_child_bail", "let g = fork job()\n    run Scheduler.sleep(30)\n    run bail()", scoped, "Nope, Boom", "row-unsafe", true},
		{"body_child_nested", "let g = fork job()\n    run Scheduler.sleep(30)\n    let r = scope { run bail() }\n    r", scoped, "Nope, Boom", "row-unsafe", true},
		{"child_only", "let g = fork job()\n    run Scheduler.sleep(30)\n    \"done\"", scoped, "Nope, Boom", "safe", true},
		{"body_only", "run Scheduler.sleep(30)\n    fail Nope { code: 1 }", scoped, "Nope, Boom", "safe", true},
		{"fail_before_fork_taken", "let first = if flag { run bail() } else { \"x\" }\n    let g = fork job()\n    run Scheduler.sleep(30)\n    \"done\"", scoped, "Nope, Boom", "safe", true},
		{"fail_before_fork_skipped", "let first = if flag { run bail() } else { \"x\" }\n    let g = fork job()\n    run Scheduler.sleep(30)\n    \"done\"", scoped, "Nope, Boom", "safe", false},
		{"caught_inside", "let g = fork job()\n    run Scheduler.sleep(30)\n    fail Nope { code: 1 }", "scope { run stage(flag).catch<Nope>(\"n\") }", "Nope, Boom", "safe", true},
		{"child_joined", "let g = fork job()\n    run Scheduler.sleep(30)\n    run g.join().catch<Boom>(\"j\")\n    fail Nope { code: 1 }", scoped, "Nope, Boom", "safe", true},
		{"other_child_joined", "let a = fork job()\n    let b = fork job2()\n    run Scheduler.sleep(30)\n    run a.join().catch<Boom>(\"j\")", scoped, "Nope, Boom", "safe", true},
		// Two failing children.
		{"two_children", "let a = fork job()\n    let b = fork job2()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Nope, Boom", "row-unsafe", true},
		{"two_same_label", "let a = fork job()\n    let b = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"two_calls", "let a = run spawn()\n    let b = run spawn()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"one_call", "let a = run spawn()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "safe", true},
		{"child_and_call", "let a = fork job()\n    let b = run spawn()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"selected_fiber", "let a = fork job()\n    let b = fork job()\n    run Scheduler.sleep(30)\n    let f = if flag { a } else { b }\n    run f.join()", scoped, "Boom", "row-unsafe", true},
		// The fork edge is an owner: a child that fails with a failing grandchild.
		{"child_edge", "let f = fork child()\n    run f.join().catch<Nope>(\"n\")", "run stage(flag)", "Nope, Boom", "row-unsafe", true},
		{"child_edge_joined", "let f = fork child2()\n    run f.join().catch<Nope>(\"n\")", "run stage(flag)", "Nope, Boom", "safe", true},
		// Interrupting a fiber which retains a failing grandchild abandons the
		// grandchild: a cancellation-aborted owner discards the typed failures
		// of the children nobody observed.
		{"interrupt_retained", "let f = fork slowChild()\n    run Scheduler.sleep(30)\n    let r = run f.interrupt()\n    \"stopped\"", "run stage(flag)", "Boom", "safe", true},
		{"join_retained", "let f = fork slowChild()\n    run Scheduler.sleep(30)\n    run f.join().catch<Boom>(\"j\")", "run stage(flag)", "Boom", "safe", true},
		// A timeout which fires abandons the timed work: Timeout alone.
		{"timeout_fires", "run Scheduler.sleep(0)\n    run slow().timeout(60)", "run stage(flag).catch<Timeout>(\"t\")", "Timeout, Boom", "safe", true},
		// Cancellation-aborted owners (design R6 X1): the failures of children
		// nobody observed are discarded at every nested owner, so the cause is
		// the cancellation alone and never a composite with a typed failure.
		{"x_s2_interrupt_latejoin", "let f = fork slowLate()\n    run Scheduler.sleep(30)\n    run f.interrupt()\n    \"done\"", scoped, "Boom", "safe", true},
		{"x_s3_scope_close_cancel", "let f = fork slowLate()\n    run Scheduler.sleep(30)\n    \"done\"", scoped, "Boom", "safe", true},
		{"x_w1_cancel_retained", "let f = fork slowChild()\n    run Scheduler.sleep(30)\n    run f.cancel()\n    \"done\"", scoped, "Boom", "safe", true},
		{"x_w2_unobserved_retained", "let f = fork slowChild()\n    run Scheduler.sleep(30)\n    \"done\"", scoped, "Boom", "safe", true},
		// A join of a cancelled child ends the body by interruption although
		// the owner was never asked to cancel.
		{"x_b1_join_cancelled", "let g = fork job()\n    let p = fork parked()\n    run Scheduler.sleep(30)\n    run p.cancel()\n    run p.join()", scoped, "Boom", "interrupted", true},
		// The undeclared-row controls: slowU declares nothing, so an escaping
		// Boom would be an undeclared failure.
		{"x_u1_undeclared_interrupt", "let f = fork slowU()\n    run Scheduler.sleep(30)\n    run f.interrupt()\n    \"done\"", scoped, "", "safe", true},
		{"x_u2_undeclared_timeout", "run slowU().timeout(60)", "run stage(flag).catch<Timeout>(\"t\")", "Timeout", "safe", true},
		// The deadline coincides with the child's failure: a race between two
		// single declared causes (Boom if the work wins, Timeout if the timer
		// does). The oracle bounds the outcome by the declared row; it does not
		// pick the winner.
		{"x_r1_deadline_race", "run raceWork().timeout(40)", "run stage(flag)", "Boom, Timeout", "safe", true},
		// Forks in exclusive arms are alternatives: at most one child exists
		// on every path, so the cause is solitary. Their sequential twins
		// (a fork or a failure after the branch, two forks in one arm) meet a
		// second failure.
		{"alt_if", "if flag {\n        let a = fork job()\n    } else {\n        let b = fork job()\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "safe", true},
		{"alt_if_else_taken", "if flag {\n        let a = fork job()\n    } else {\n        let b = fork job()\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "safe", false},
		{"alt_match", "let p = if flag { Pick.A {} } else { Pick.B {} }\n    match p {\n        Pick.A => { let a = fork job() \"x\" }\n        Pick.B => { let b = fork job() \"y\" }\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "safe", true},
		{"alt_calls", "let r = if flag { run spawn() } else { run spawn() }\n    run Scheduler.sleep(50)\n    r", scoped, "Boom", "safe", true},
		{"alt_both_fail", "if flag {\n        let a = fork job()\n    } else {\n        let b = fork job2()\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Nope, Boom", "safe", true},
		{"alt_nested", "if flag {\n        if flag {\n            let a = fork job()\n        } else {\n            let b = fork job()\n        }\n    } else {\n        let c = fork job()\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "safe", true},
		{"alt_if_then_fork", "if flag {\n        let a = fork job()\n    } else {\n        let b = fork job()\n    }\n    let c = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"alt_if_then_fail", "if flag {\n        let a = fork job()\n    } else {\n        let b = fork job()\n    }\n    run Scheduler.sleep(50)\n    fail Nope { code: 1 }", scoped, "Nope, Boom", "row-unsafe", true},
		{"alt_two_in_arm", "if flag {\n        let a = fork job()\n        let b = fork job()\n    } else {\n        let c = fork job()\n    }\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"alt_match_then_fork", "let p = if flag { Pick.A {} } else { Pick.B {} }\n    match p {\n        Pick.A => { let a = fork job() \"x\" }\n        Pick.B => { let b = fork job() \"y\" }\n    }\n    let c = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		{"alt_calls_then_call", "let r = if flag { run spawn() } else { run spawn() }\n    let s = run spawn()\n    run Scheduler.sleep(50)\n    r", scoped, "Boom", "row-unsafe", true},
		{"alt_nested_then_fork", "if flag {\n        if flag {\n            let a = fork job()\n        } else {\n            let b = fork job()\n        }\n    } else {\n        let c = fork job()\n    }\n    let d = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", true},
		// A provision edge owns the children its layer forks.
		{"provision_two_children", "let a = fork job()\n    let b = fork job2()\n    run Scheduler.sleep(50)\n    \"done\"", "run stage(flag).provide(Clocked)", "Nope, Boom", "row-unsafe", true},
		{"provision_one_child", "let a = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", "run stage(flag).provide(Clocked)", "Boom", "safe", true},
		// Sequential joins: a failure of the first leaves the second child
		// unobserved (the shape of the declared-row policy rows, with children
		// that really fail; the rows themselves need Files, which JS lacks).
		{"joins_in_order", "let a = fork job()\n    let b = fork job2()\n    run Scheduler.sleep(30)\n    let x = run a.join()\n    let y = run b.join()\n    y", scoped, "Nope, Boom", "row-unsafe", true},
		{"joins_in_order_first_caught", "let a = fork job()\n    let b = fork job2()\n    run Scheduler.sleep(30)\n    let x = run a.join().catch<Boom>(\"j\")\n    let y = run b.join()\n    y", scoped, "Nope, Boom", "safe", true},
		{"alt_arm_joined_then_fork", "if flag {\n        let a = fork job()\n        let j = run a.join().catch<Boom>(\"j\")\n    } else {\n        let b = fork job()\n    }\n    let c = fork job()\n    run Scheduler.sleep(50)\n    \"done\"", scoped, "Boom", "row-unsafe", false},
		// A named recover handler's forked child is a child of the recovered
		// recipe, distinct from the recipe's own children (design 5.5, R6 fix
		// round r1 B1): handling the recipe's Boom beside the handler's own
		// Boom is a composite cause. The controls have one child.
		{"b1_handler_child_alias", "run source().recover<Nope>(handler).catch<Boom>(\"unused\")", scoped, "Boom", "row-unsafe", true},
		{"b1_handler_child_nested", "run source().recover<Nope>(handlerNested).catch<Boom>(\"unused\")", scoped, "Boom", "row-unsafe", true},
		{"b1_handler_child_only", "run bail().recover<Nope>(handler).catch<Boom>(\"unused\")", scoped, "Boom", "safe", true},
		{"b1_source_child_only", "run source().recover<Nope>(handlerQuiet).catch<Boom>(\"unused\")", scoped, "Boom", "safe", true},
	}
	var out []ownershipMatrixProgram
	for _, s := range shapes {
		extra := ""
		switch s.name {
		case "child_edge":
			extra = "effect fn child() -> string raises { Nope, Boom } uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(30)\n    fail Nope { code: 1 }\n}\n"
		case "child_edge_joined":
			extra = "effect fn child2() -> string raises { Nope } uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(30)\n    run g.join().catch<Boom>(\"j\")\n    fail Nope { code: 1 }\n}\n"
		case "x_w1_cancel_retained", "x_w2_unobserved_retained", "interrupt_retained", "join_retained":
			extra = "effect fn slowChild() -> string raises { Boom } uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(300)\n    \"slow\"\n}\n"
		case "provision_two_children", "provision_one_child":
			extra = "layer Clocked { Clock = LiveClock }\n"
		case "x_s2_interrupt_latejoin", "x_s3_scope_close_cancel":
			extra = "effect fn slowLate() -> string raises { Boom } uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(300)\n    run g.join().catch<Boom>(\"j\")\n    \"slow\"\n}\n"
		case "x_b1_join_cancelled":
			extra = "effect fn parked() -> string uses { Scheduler } {\n    run Scheduler.sleep(10000)\n    \"p\"\n}\n"
		case "x_u1_undeclared_interrupt", "x_u2_undeclared_timeout":
			extra = "effect fn slowU() -> string uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(300)\n    run g.join().catch<Boom>(\"j\")\n}\n"
		case "x_r1_deadline_race":
			extra = "effect fn delayedFail() -> string raises { Boom } uses { Scheduler } {\n    run Scheduler.sleep(40)\n    fail Boom { code: 7 }\n}\neffect fn raceWork() -> string raises { Boom } uses { Scheduler } {\n    let g = fork delayedFail()\n    run g.join()\n}\n"
		case "b1_handler_child_alias", "b1_handler_child_nested", "b1_handler_child_only", "b1_source_child_only":
			extra = "error Other { code: i64 }\n" +
				"effect fn source() -> string raises { Boom, Nope } uses { Scheduler } {\n    let c = fork job()\n    run Scheduler.sleep(30)\n    fail Nope { code: 1 }\n}\n" +
				"effect fn handler(n: Nope) -> string raises { Boom } uses { Scheduler } {\n    let c = fork job()\n    run Scheduler.sleep(30)\n    \"handled\"\n}\n" +
				"effect fn handlerQuiet(n: Nope) -> string uses { Scheduler } {\n    run Scheduler.sleep(30)\n    \"handled\"\n}\n" +
				"effect fn inner() -> string raises { Boom, Other } uses { Scheduler } {\n    let c = fork job()\n    run Scheduler.sleep(30)\n    fail Other { code: 1 }\n}\n" +
				"effect fn innerHandler(o: Other) -> string raises { Boom } uses { Scheduler } {\n    let c = fork job()\n    run Scheduler.sleep(30)\n    \"inner\"\n}\n" +
				"effect fn handlerNested(n: Nope) -> string raises { Boom } uses { Scheduler } {\n    run inner().recover<Other>(innerHandler)\n}\n"
		case "timeout_fires":
			extra = "effect fn slow() -> string raises { Boom } uses { Scheduler } {\n    let g = fork job()\n    run Scheduler.sleep(300)\n    \"slow\"\n}\n"
		}
		src := ownershipMatrixCompositeHead + extra + "effect fn stage(flag: bool) -> string" + ownershipMatrixRaisesClause(s.raises) + " uses { Scheduler } {\n    " + s.stage + "\n}\n" +
			"effect fn consume(flag: bool) -> string" + ownershipMatrixRaisesClause(s.raises) + " uses { Scheduler } {\n    " + s.wrap + "\n}\n" +
			"effect fn main() -> void {\n    let r = run consume(" + strconv.FormatBool(s.flag) + ")" + ownershipMatrixCompositeCatches(s.raises) + ".provide<Scheduler>(LiveScheduler)\n    run Console.log(r).provide<Console>(Stdout)\n}\n"
		out = append(out, ownershipMatrixProgram{Name: "cc_" + s.name, Source: src, Oracle: s.oracle, JSRunnable: true})
	}
	return out
}

// ownershipMatrixRaisesClause is the raises clause of a row, or nothing for
// a body which declares no failure.
func ownershipMatrixRaisesClause(raises string) string {
	if raises == "" {
		return ""
	}
	return " raises { " + raises + " }"
}

// ownershipMatrixCompositeCatches handles every declared failure of the
// consumer, so only a composite cause escapes.
func ownershipMatrixCompositeCatches(raises string) string {
	out := ""
	for _, label := range []string{"Nope", "Boom", "Timeout"} {
		for _, declared := range splitLabels(raises) {
			if declared == label {
				out += ".catch<" + label + ">(\"" + label + "\")"
			}
		}
	}
	return out
}

func splitLabels(row string) []string {
	var out []string
	label := ""
	for _, r := range row + "," {
		if r == ',' {
			if label != "" {
				out = append(out, label)
			}
			label = ""
		} else if r != ' ' {
			label += string(r)
		}
	}
	return out
}
