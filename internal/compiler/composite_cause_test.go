package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An owner edge raises an ordinary failure only when exactly one failure
// reaches it (design §5.3 rule 6). A failing exit taken while a child is
// unobserved, two failing children, a timeout which may fire and an
// interrupt of a fiber which retains a failing child are composite causes:
// the runtime raises them and no handler matches them.
func TestCompositeCausesAreRefusedAtOwnerEdges(t *testing.T) {
	const prelude = `error A
error B
effect fn task() -> string raises {A} { fail A }
effect fn other() -> string raises {B} { fail B }
effect fn spawn() -> string raises {A} { let f = fork task() "spawned" }
effect fn slowChild() -> string raises {A} uses {Scheduler} { let f = fork task() run Scheduler.sleep(300) "slow" }
fn fallback(failure: B) -> string { "recovered" }
effect fn stage2() -> string raises {A} uses {Scheduler} { let f = fork task() run Scheduler.sleep(1) "inner" }
effect fn raising(failure: B) -> string raises {B} { fail B }
`
	for _, tc := range []struct {
		name string
		// stage is a body which raises {A, B}.
		stage string
		// composite reports whether the owner raises a composite cause.
		composite bool
	}{
		{name: "body failure with a child", stage: `{ let f = fork task() run Scheduler.sleep(1) fail B }`, composite: true},
		{name: "failing call with a child", stage: `{ let f = fork task() run other() }`, composite: true},
		{name: "child only", stage: `{ let f = fork task() "done" }`},
		{name: "body failure only", stage: `{ run Scheduler.sleep(1) fail B }`},
		{name: "child observed before the failure", stage: `{ let f = fork task() run f.join().catch<A>("j") fail B }`},
		{name: "failure before the fork", stage: `{ run other().catch<B>("c") let f = fork task() "done" }`},
		{name: "two children", stage: `{ let f = fork task() let g = fork other() "done" }`, composite: true},
		{name: "two children with one label", stage: `{ let f = fork task() let g = fork task() "done" }`, composite: true},
		{name: "two calls", stage: `{ let f = run spawn() let g = run spawn() "done" }`, composite: true},
		{name: "one call", stage: `{ let f = run spawn() "done" }`},
		{name: "call and child", stage: `{ let f = run spawn() let g = fork task() "done" }`, composite: true},
		{name: "recovered failure", stage: `{ let f = fork task() run other().catch<B>("c") "done" }`},
		{name: "recovery which raises", stage: `{ let f = fork task() run other().recover<B>(raising) }`, composite: true},
		{name: "recovery which returns", stage: `{ let f = fork task() run other().recover<B>(fallback) }`},
		// A cancelled owner abandons the children it had not observed: the
		// interrupt of a running fiber is the interruption alone, and so is a
		// timeout which fires (design R6 X1).
		{name: "interrupt of a retaining fiber", stage: `{ let f = fork slowChild() run Scheduler.sleep(1) run f.interrupt() "stopped" }`},
		// A join observes the child's failure alone; only an interrupt
		// reaches it together with the interruption.
		{name: "join of a retaining fiber", stage: `{ let f = fork slowChild() run f.join() }`},
		{name: "join of a fiber nested in a fiber", stage: `{ let f = fork stage2() run f.join() }`},
		{name: "timeout over a retaining child", stage: `{ run slowChild().timeout(60).catch<Timeout>("t") }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := prelude + "effect fn stage() -> string raises {A, B} uses {Scheduler} " + tc.stage + `
effect fn consume() -> string raises {A, B} uses {Scheduler} { scope { run stage() } }
effect fn main() -> void {
    let r = run consume().catch<A>("a").catch<B>("b").provide<Scheduler>(LiveScheduler)
    run Console.log(r).provide<Console>(Stdout)
}`
			r := CompileFor(source, "go")
			if !tc.composite {
				if !r.Checked {
					t.Fatalf("a solitary failure was refused: %+v", r.Diagnostics)
				}
				return
			}
			if r.Checked || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "EF107" || !strings.HasPrefix(r.Diagnostics[0].Message, "undeclared failures: "+compositeCause) {
				t.Fatalf("want EF107 undeclared %s, got %+v", compositeCause, r.Diagnostics)
			}
		})
	}
}

// The root edge is no owner edge a handler could be placed around: main exits
// 1 with whatever cause reaches it, composite or not (Go prints "Boom; Nope"
// for two failing children), exactly as for one declared failure. A main
// which declares the failures of its pending children is therefore admitted
// (testdata/ownership_sweep/root_pending_declared.ef, executed on Go and JS),
// while the same program with main declaring nothing is refused as an
// undeclared failure.
func TestRootEdgeAdmitsDeclaredCompositeFailures(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "ownership_sweep", "root_pending_declared.ef"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		if r := CompileFor(string(source), target); !r.Checked || len(r.Diagnostics) != 0 {
			t.Errorf("%s: want the declared root admitted, got checked=%v %+v", target, r.Checked, r.Diagnostics)
		}
		undeclared := strings.Replace(string(source), "effect fn main() -> void raises { Boom, Nope } {", "effect fn main() -> void {", 1)
		if r := CompileFor(undeclared, target); r.Checked || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "EF107" {
			t.Errorf("%s: want an undeclared root refused with EF107, got checked=%v %+v", target, r.Checked, r.Diagnostics)
		}
	}
}
