// Ownership matrix: recipes stored in data fields (lane E2 R5 fix round r1).
//
// A recipe stored in a record field is joined and instantiated through the
// same slot table as any other occurrence, so every channel it carries
// (payload evidence, pending children, observations) must survive an if
// join and a helper's call edge. These families were missing from the
// matrix: B1 (a field join dropped the right alternative's failures, forks
// and observations) and B2 (a helper's stored recipe kept an unsubstituted
// parameter owner in its failure payload) passed it.
//
// Each program's truth is executed with ownership and row refusals erased
// (scripts/ownership_truth.py). Rows whose alternatives differ run the
// unsafe alternative, so the join is the only thing that can hide it.

package compiler

const ownershipMatrixFieldHead = ownershipMatrixBaseErrors + `record Holder { task: Effect<string, { WithFile }> }
record Wrap { holder: Holder }
effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn retrieve(failure: WithFile) -> string raises { IoError } { run readFile(failure.file) }
fn ignore(failure: WithFile) -> string { "fixture-ok" }
`

// ownershipMatrixFieldCatch handles the IoError a reading handler can raise;
// an ignoring handler raises none, and catching an absent label is refused.
func ownershipMatrixFieldCatch(handler string) string {
	if handler == "ignore" {
		return ""
	}
	return `.catch<IoError>("closed")`
}

// ownershipMatrixFieldPayloadPrograms: a stored recipe's failure payload
// reaches a handler after its owner closed. Joined through an if (both
// branch orders) or packed by a helper (instantiated at the call edge).
func ownershipMatrixFieldPayloadPrograms(fix string) []ownershipMatrixProgram {
	open := ownershipMatrixOpen(fix)
	head := ownershipMatrixFieldHead + `effect fn open() -> File raises { IoError } {
    ` + open + `
}
`
	var out []ownershipMatrixProgram
	add := func(name, src, oracle string) {
		out = append(out, ownershipMatrixProgram{Name: "ff_" + name, Source: src, Oracle: oracle})
	}
	// Joins: the scope-owned alternative executes, so its payload's file
	// is closed when the handler dispatches.
	for _, order := range []string{"inner-left", "inner-right"} {
		for _, h := range []string{"retrieve", "ignore"} {
			left, right, flag := "inner", "outer", "true"
			if order == "inner-right" {
				left, right, flag = "outer", "inner", "false"
			}
			src := head + `effect fn make(flag: bool, outer: File) -> Holder raises { IoError } {
    scope {
        let inner = run open()
        if flag { Holder { task: rejected(` + left + `) } } else { Holder { task: rejected(` + right + `) } }
    }
}
effect fn main() -> void raises { IoError } {
    let outer = run open()
    let h = run make(` + flag + `, outer)
    run Console.log(
        run h.task.recover<WithFile>(` + h + `)` + ownershipMatrixFieldCatch(h) + `
    ).provide<Console>(Stdout)
}
`
			add("join_"+order+"_"+h, src, ownershipMatrixOracle(h == "retrieve"))
		}
	}
	// Control: both alternatives carry the outer file, which is still open.
	add("join_outer-both_retrieve", head+`effect fn make(flag: bool, outer: File) -> Holder raises { IoError } {
    scope {
        if flag { Holder { task: rejected(outer) } } else { Holder { task: rejected(outer) } }
    }
}
effect fn main() -> void raises { IoError } {
    let outer = run open()
    let h = run make(true, outer)
    run Console.log(
        run h.task.recover<WithFile>(retrieve).catch<IoError>("closed")
    ).provide<Console>(Stdout)
}
`, "safe")
	// Helpers: the payload owner is the helper's parameter, substituted by
	// the actual file at the call edge.
	helpers := map[string]string{
		"pure":   "fn pack(file: File) -> Holder {\n    Holder { task: rejected(file) }\n}\n",
		"effect": "effect fn pack(file: File) -> Holder {\n    Holder { task: rejected(file) }\n}\n",
		"nested": "fn pack(file: File) -> Wrap {\n    Wrap { holder: Holder { task: rejected(file) } }\n}\n",
		"twice":  "fn pack1(file: File) -> Holder {\n    Holder { task: rejected(file) }\n}\nfn pack(file: File) -> Holder {\n    pack1(file)\n}\n",
	}
	for _, kind := range []string{"pure", "effect", "nested", "twice", "inline"} {
		for _, h := range []string{"retrieve", "ignore"} {
			helper, call, field := helpers[kind], "pack(file)", "h.task"
			switch kind {
			case "effect":
				call = "run pack(file)"
			case "nested":
				field = "h.holder.task"
			case "inline":
				helper, call = "", "Holder { task: rejected(file) }"
			}
			src := head + helper + `effect fn make() -> ` + map[bool]string{true: "Wrap", false: "Holder"}[kind == "nested"] + ` raises { IoError } {
    let file = run open()
    ` + call + `
}
effect fn main() -> void raises { IoError } {
    let h = scope { run make() }
    run Console.log(
        run ` + field + `.recover<WithFile>(` + h + `)` + ownershipMatrixFieldCatch(h) + `
    ).provide<Console>(Stdout)
}
`
			add("helper_"+kind+"_"+h, src, ownershipMatrixOracle(h == "retrieve"))
		}
	}
	// Control: the helper packs a file owned outside the scope.
	add("helper_outer_retrieve", head+helpers["pure"]+`effect fn make(file: File) -> Holder raises { IoError } {
    pack(file)
}
effect fn main() -> void raises { IoError } {
    let outer = run open()
    let h = scope { run make(outer) }
    run Console.log(
        run h.task.recover<WithFile>(retrieve).catch<IoError>("closed")
    ).provide<Console>(Stdout)
}
`, "safe")
	return out
}

const ownershipMatrixFieldRowHead = `error Boom { code: i64 }
record Holder { task: Effect<string, { Boom }, { Scheduler }> }
effect fn work() -> string raises { Boom } {
    fail Boom { code: 7 }
}
effect fn ordinary() -> string raises { Boom } uses { Scheduler } {
    "ordinary"
}
`

// ownershipMatrixFieldRowPrograms: a stored recipe's forked-child failures
// and observations survive a field join. Handle-free, so they also run on JS.
func ownershipMatrixFieldRowPrograms() []ownershipMatrixProgram {
	var out []ownershipMatrixProgram
	add := func(name, src, oracle string) {
		out = append(out, ownershipMatrixProgram{Name: "fj_" + name, Source: src, Oracle: oracle, JSRunnable: true})
	}
	// Pending: the stored recipe forks a failing child and leaves it
	// unobserved; the alternative which executes is the one that does.
	pendingStage := `effect fn stage() -> string raises { Boom } uses { Scheduler } {
    let g = fork work()
    run Scheduler.sleep(30)
    "done"
}
effect fn joined() -> string raises { Boom } uses { Scheduler } {
    let g = fork work()
    run Scheduler.sleep(30)
    run g.join()
}
`
	pendingMain := func(cond, left, right string) string {
		return ownershipMatrixFieldRowHead + pendingStage + `effect fn main() -> void {
    let h = if ` + cond + ` {
        Holder { task: ` + left + `() }
    } else {
        Holder { task: ` + right + `() }
    }
    run Console.log(
        run h.task.catch<Boom>("handled").provide<Scheduler>(LiveScheduler)
    ).provide<Console>(Stdout)
}
`
	}
	add("pending_right", pendingMain("false", "ordinary", "stage"), "row-unsafe")
	add("pending_left", pendingMain("true", "stage", "ordinary"), "row-unsafe")
	add("pending_none", pendingMain("false", "ordinary", "ordinary"), "safe")
	add("pending_joined", pendingMain("true", "joined", "ordinary"), "safe")
	// Observation: a joined recipe observes only what both alternatives do.
	observeStage := func(cond, left, right string) string {
		return ownershipMatrixFieldRowHead + `effect fn stage(m: i64) -> string raises { Boom } uses { Scheduler } {
    let g = fork work()
    run Scheduler.sleep(30)
    let h = if ` + cond + ` { Holder { task: ` + left + ` } } else { Holder { task: ` + right + ` } }
    run h.task
}
effect fn consume(m: i64) -> string uses { Scheduler } {
    scope { run stage(m).catch<Boom>("b") }
}
effect fn main() -> void {
    run Console.log(run consume(2).provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)
}
`
	}
	add("observe_left", observeStage("m == 1", "g.join()", "ordinary()"), "row-unsafe")
	add("observe_right", observeStage("m == 2", "ordinary()", "g.join()"), "row-unsafe")
	add("observe_both", observeStage("m == 1", "g.join()", "g.join()"), "safe")
	return out
}
