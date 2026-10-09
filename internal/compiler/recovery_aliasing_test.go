// Fork identity aliasing across handler, call and recovery compositions (lane
// E2 R6 fix round r1, B1).
//
// A pending child is identified by the function that forks it and its number
// there, and every function numbers its own children from zero. A call, and a
// recover handler, instantiate the callee's children at the call site so two
// functions' first children are never one child. The generator composes
// stage bodies from items which each contribute a known number of failing
// children: a direct fork, a call, and a recovered recipe whose source and
// handler fork directly, through a call, twice, or through a nested recover.
// Every child fails Boom and the stage sleeps before exit, so the owner
// closes over all of them: two or more children are a composite cause which
// the checker must refuse, and at most one child must be admitted. An
// aliasing bug admits two children as one.

package compiler

import (
	"fmt"
	"strings"
	"testing"
)

const recoveryAliasingHead = `error Boom { code: i64 }
error Nope { code: i64 }
error Other { code: i64 }
effect fn job() -> string raises { Boom } {
    fail Boom { code: 1 }
}
effect fn spawn() -> string raises { Boom } uses { Scheduler } {
    let c = fork job()
    run Scheduler.sleep(1)
    "spawned"
}
effect fn bail() -> string raises { Nope } {
    fail Nope { code: 1 }
}
effect fn srcFork() -> string raises { Boom, Nope } uses { Scheduler } {
    let c = fork job()
    run Scheduler.sleep(30)
    fail Nope { code: 1 }
}
effect fn srcCall() -> string raises { Boom, Nope } uses { Scheduler } {
    let a = run spawn()
    run Scheduler.sleep(30)
    fail Nope { code: 1 }
}
effect fn inner() -> string raises { Boom, Other } uses { Scheduler } {
    let c = fork job()
    run Scheduler.sleep(30)
    fail Other { code: 1 }
}
effect fn innerHandler(o: Other) -> string raises { Boom } uses { Scheduler } {
    let c = fork job()
    run Scheduler.sleep(30)
    "inner"
}
effect fn hQuiet(n: Nope) -> string uses { Scheduler } {
    run Scheduler.sleep(30)
    "quiet"
}
effect fn hFork(n: Nope) -> string raises { Boom } uses { Scheduler } {
    let c = fork job()
    run Scheduler.sleep(30)
    "fork"
}
effect fn hCall(n: Nope) -> string raises { Boom } uses { Scheduler } {
    let a = run spawn()
    run Scheduler.sleep(30)
    "call"
}
effect fn hTwo(n: Nope) -> string raises { Boom } uses { Scheduler } {
    let a = fork job()
    let b = fork job()
    run Scheduler.sleep(30)
    "two"
}
effect fn hNested(n: Nope) -> string raises { Boom } uses { Scheduler } {
    run inner().recover<Other>(innerHandler)
}
`

type recoveryAliasingItem struct {
	code     string
	children int
}

// recoveryAliasingItems are the stage statements the generator composes.
func recoveryAliasingItems() []recoveryAliasingItem {
	items := []recoveryAliasingItem{
		{"let x = fork job()", 1},
		{"let x = run spawn()", 1},
	}
	sources := []recoveryAliasingItem{{"bail()", 0}, {"srcFork()", 1}, {"srcCall()", 1}}
	handlers := []recoveryAliasingItem{{"hQuiet", 0}, {"hFork", 1}, {"hCall", 1}, {"hTwo", 2}, {"hNested", 2}}
	for _, s := range sources {
		for _, h := range handlers {
			// A catch of Boom is an error where the recipe cannot raise it.
			catch := ""
			if s.children+h.children > 0 {
				catch = ".catch<Boom>(\"c\")"
			}
			items = append(items, recoveryAliasingItem{
				fmt.Sprintf("let x = run %s.recover<Nope>(%s)%s", s.code, h.code, catch),
				s.children + h.children,
			})
		}
	}
	return items
}

// TestRecoveryCompositionsKeepForkIdentitiesDistinct checks every stage of one
// or two items and a fixed stride of the stages of three. The names of the
// bindings repeat on purpose: identity is the fork's, not the binding's.
func TestRecoveryCompositionsKeepForkIdentitiesDistinct(t *testing.T) {
	items := recoveryAliasingItems()
	var stages [][]recoveryAliasingItem
	for _, a := range items {
		stages = append(stages, []recoveryAliasingItem{a})
		for _, b := range items {
			stages = append(stages, []recoveryAliasingItem{a, b})
		}
	}
	n := 0
	for _, a := range items {
		for _, b := range items {
			for _, c := range items {
				if n%11 == 0 {
					stages = append(stages, []recoveryAliasingItem{a, b, c})
				}
				n++
			}
		}
	}
	for i, stage := range stages {
		children := 0
		var body []string
		for j, item := range stage {
			children += item.children
			body = append(body, strings.Replace(item.code, "let x", fmt.Sprintf("let x%d", j), 1))
		}
		source := recoveryAliasingHead +
			"effect fn stage() -> string raises { Boom } uses { Scheduler } {\n    " + strings.Join(body, "\n    ") + "\n    run Scheduler.sleep(50)\n    \"done\"\n}\n" +
			"effect fn consume() -> string raises { Boom } uses { Scheduler } {\n    scope { run stage() }\n}\n" +
			"effect fn main() -> void {\n    run Console.log(run consume().catch<Boom>(\"b\").provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)\n}\n"
		r := Compile(source)
		composite := false
		for _, d := range r.Diagnostics {
			composite = composite || (d.Code == "EF107" && strings.Contains(d.Message, "composite cause"))
		}
		switch {
		case children >= 2 && (r.Checked || !composite):
			t.Errorf("stage %d (%d children) must be refused as a composite cause, got checked=%v %+v\n%s", i, children, r.Checked, r.Diagnostics, strings.Join(body, "\n"))
		case children < 2 && (!r.Checked || len(r.Diagnostics) != 0):
			t.Errorf("stage %d (%d children) must be admitted, got checked=%v %+v\n%s", i, children, r.Checked, r.Diagnostics, strings.Join(body, "\n"))
		}
	}
	t.Logf("%d stages", len(stages))
}
