// Ownership matrix: joins nested inside expressions (lane E2 R5 fix round r1).
//
// A join or interrupt observes its child wherever it runs. The statement
// level was covered; a join in a call argument, an operand, a nested scope,
// an if arm or a match arm was not, and the head refused programs the base
// ran safely (M1). Each program forks one failing child and observes it, or
// fails to, only through the shape under test. The operand order rows pin
// the exit rule: a join counts only on the paths where it completed, so an
// earlier operand which fails first leaves the child unobserved.

package compiler

const ownershipMatrixNestedHead = `error Boom { code: i64 }
error Nope { code: i64 }
enum Pick { A, B }
effect fn work() -> string raises { Boom } {
    fail Boom { code: 7 }
}
effect fn bail() -> string raises { Nope } {
    fail Nope { code: 1 }
}
fn exclaim(s: string) -> string { s + "!" }
fn pair(a: string, b: string) -> string { a + b }
effect fn shout(s: string) -> string { s + "!" }
`

// ownershipMatrixNestedPrograms: body is the tail of stage, after it forks
// the failing child g. m is 1 or 2 and p picks an arm, so the executed
// path is the one the oracle names.
func ownershipMatrixNestedPrograms() []ownershipMatrixProgram {
	shapes := []struct {
		name, body string
		m          int
		p          string
		oracle     string
	}{
		{"call_arg", `exclaim(run g.join())`, 1, "A", "safe"},
		{"call_arg_deep", `exclaim(exclaim(run g.join()))`, 1, "A", "safe"},
		{"effect_call_arg", `run shout(run g.join())`, 1, "A", "safe"},
		{"let_call_arg", "let s = exclaim(run g.join())\n    s", 1, "A", "safe"},
		{"binary", `"x" + run g.join()`, 1, "A", "safe"},
		{"scope_arg", `exclaim(scope { run g.join() })`, 1, "A", "safe"},
		{"match_all", "match p {\n        Pick.A => run g.join()\n        Pick.B => run g.join()\n    }", 1, "A", "safe"},
		{"match_first", "match p {\n        Pick.A => run g.join()\n        Pick.B => \"ordinary\"\n    }", 1, "B", "row-unsafe"},
		{"match_second", "match p {\n        Pick.A => \"ordinary\"\n        Pick.B => run g.join()\n    }", 1, "A", "row-unsafe"},
		{"match_arg_all", "exclaim(match p {\n        Pick.A => run g.join()\n        Pick.B => run g.join()\n    })", 1, "A", "safe"},
		{"match_arg_first", "exclaim(match p {\n        Pick.A => run g.join()\n        Pick.B => \"ordinary\"\n    })", 1, "B", "row-unsafe"},
		{"if_arg_all", `exclaim(if m == 1 { run g.join() } else { run g.join() })`, 1, "A", "safe"},
		{"if_arg_first", `exclaim(if m == 1 { run g.join() } else { "ordinary" })`, 2, "A", "row-unsafe"},
		{"if_arg_second", `exclaim(if m == 1 { "ordinary" } else { run g.join() })`, 1, "A", "row-unsafe"},
		// Operand order: the join counts on the exits after it.
		{"order_join_first", `pair(run g.join(), run bail())`, 1, "A", "safe"},
		{"order_fail_first", `pair(run bail(), run g.join())`, 1, "A", "row-unsafe"},
		{"order_fail_first_nested", `exclaim(pair(run bail(), run g.join()))`, 1, "A", "row-unsafe"},
	}
	var out []ownershipMatrixProgram
	for _, s := range shapes {
		src := ownershipMatrixNestedHead + `effect fn stage(m: i64, p: Pick) -> string raises { Boom, Nope } uses { Scheduler } {
    let g = fork work()
    run Scheduler.sleep(30)
    ` + s.body + `
}
effect fn consume(m: i64, p: Pick) -> string uses { Scheduler } {
    scope { run stage(m, p).catch<Boom>("b").catch<Nope>("n") }
}
effect fn main() -> void {
    run Console.log(run consume(` + string(rune('0'+s.m)) + `, Pick.` + s.p + ` {}).provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)
}
`
		out = append(out, ownershipMatrixProgram{Name: "nj_" + s.name, Source: src, Oracle: s.oracle, JSRunnable: true})
	}
	return out
}
