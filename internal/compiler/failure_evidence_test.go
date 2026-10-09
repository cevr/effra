package compiler

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// failureEvidencePrelude declares a handle-carrying failure, a producer
// whose payload is owned by a scope that closes before the failure reaches
// its caller, and the three handler shapes of the recover rule (design §5.5):
// one that dereferences the payload, one that discards it and one that keeps
// it as its result.
const failureEvidencePrelude = `
error WithFile { file: File }
error Other { file: File }
effect fn bad(fallback: File) -> File raises {WithFile, Other, IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  fail WithFile { file: file }
 }
}
effect fn read(failure: WithFile) -> File raises {IoError} uses {Files} {
 let text = run Files.readText(failure.file)
 failure.file
}
fn ignore(failure: WithFile) -> File { failure.file }
fn keep(failure: WithFile) -> File { failure.file }
effect fn main() -> void { void }
`

func TestFailureEvidenceRecordsTheClosedScopeOwner(t *testing.T) {
	r := requireOwnershipAccepted(t, failureEvidencePrelude)
	f := functionNamed(r.Program.Functions, "bad")
	facts, ok := f.failures[raisedKey(0, "WithFile")]
	if !ok || !slices.ContainsFunc(facts, func(fact OwnershipFact) bool {
		return fact.Path == "file" && fact.ownerKind == ownershipOwnerScope
	}) {
		t.Fatalf("payload evidence lost the closed scope owner: %+v", f.failures)
	}
	// Other is declared but never raised: its evidence is proven empty,
	// not unknown.
	if facts, ok := f.failures[raisedKey(0, "Other")]; !ok || len(facts) != 0 {
		t.Fatalf("a declared label the body cannot raise is not proven empty: %+v", f.failures)
	}
}

func TestFailureEvidenceRecoverChecksTheHandlerUses(t *testing.T) {
	for _, test := range []struct {
		name, consumer string
		refused        bool
	}{
		{"dereferencing handler", `run bad(fallback).recover<WithFile>(read)`, true},
		// ignore and keep never dereference the payload: recovery that
		// discards or stores a dead handle is admitted.
		{"discarding handler", `let kept = run bad(fallback).recover<WithFile>(ignore)
 fallback`, false},
		{"keeping handler", `let kept = run bad(fallback).recover<WithFile>(keep)
 fallback`, false},
		// The kept handle still carries the closed owner: its later use is
		// refused at the use.
		{"use of a kept payload", `let kept = run bad(fallback).recover<WithFile>(keep)
 let text = run Files.readText(kept)
 fallback`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := failureEvidencePrelude + `effect fn consume(fallback: File) -> File raises {Other, IoError} uses {Files} {
 ` + test.consumer + `
}
`
			if !test.refused {
				requireOwnershipAccepted(t, source)
				return
			}
			r := requireOwnershipRejected(t, source)
			if test.name == "dereferencing handler" && !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool {
				return strings.Contains(d.Message, "recovery handler uses a failure payload owned by a closed scope")
			}) {
				t.Fatalf("refused for another reason: %+v", r.Diagnostics)
			}
		})
	}
}

// interrupt raises the child's failures with the child's payload evidence:
// the child's handles are closed when the parent can observe the failure.
// stage declares WithFile for the fork-site row charge.
func TestFailureEvidenceInterruptCarriesChildPayloads(t *testing.T) {
	stage := func(handler string) string {
		return `
error WithFile { file: File }
effect fn job() -> string raises {WithFile, IoError} {
 let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 fail WithFile { file: file }
}
effect fn readV(failure: WithFile) -> void raises {IoError} {
 let text = run Files.readText(failure.file).provide<Files>(LiveFiles)
}
fn ignoreV(failure: WithFile) -> void { void }
effect fn stage() -> string raises {WithFile, IoError} {
 let fiber = fork job()
 run fiber.interrupt().recover<WithFile>(` + handler + `)
 "done"
}
effect fn main() -> void { void }
`
	}
	requireOwnershipRejected(t, stage("readV"))
	requireOwnershipAccepted(t, stage("ignoreV"))
}

// A handle-carrying label without evidence is unknown, never proven empty:
// completion fills it with ⊤ and keeps present entries.
func TestFailureEvidenceAbsentLabelFailsClosed(t *testing.T) {
	r := requireOwnershipAccepted(t, failureEvidencePrelude)
	c := r.projector
	completed := c.completeFailureRows(nil, [][]string{{"WithFile", "IoError"}})
	top, ok := completed[raisedKey(0, "WithFile")]
	if !ok || len(top) == 0 || !slices.ContainsFunc(top, func(fact OwnershipFact) bool { return fact.potentialOwner && fact.Status == "unknown" }) {
		t.Fatalf("absent handle-carrying label was not ⊤: %+v", completed)
	}
	if _, ok := completed[raisedKey(0, "IoError")]; ok {
		t.Fatalf("a label without handles gained evidence: %+v", completed)
	}
	empty := failureEvidence{raisedKey(0, "WithFile"): nil}
	if kept := c.completeFailureRows(empty, [][]string{{"WithFile"}}); !equalFailures(kept, empty) {
		t.Fatalf("completion replaced proven-empty evidence: %+v", kept)
	}
	if again := c.completeFailureRows(completed, [][]string{{"WithFile", "IoError"}}); !equalFailures(again, completed) {
		t.Fatalf("completion is not idempotent: %+v", again)
	}
}

// Lifting and executing a layer are inverse on payload evidence; executing
// returns the layer-0 payloads to the executing evaluation.
func TestFailureEvidenceShiftIsInvertible(t *testing.T) {
	evidence := failureEvidence{raisedKey(0, "A"): {execOwner(0)}, raisedKey(1, "B"): {lexicalOwner("scope:1")}}
	if got := shiftFailures(shiftFailures(evidence, 1), -1); !equalFailures(got, evidence) {
		t.Fatalf("shift(shift(F, +1), -1) != F: %+v", got)
	}
	executed := shiftFailures(evidence, -1)
	if _, ok := executed[raisedKey(0, "B")]; !ok || len(executed) != 1 {
		t.Fatalf("executing did not drop layer 0 and renumber: %+v", executed)
	}
	if payloads := layerPayloads(evidence, 0); len(payloads) != 1 || len(payloads["A"]) != 1 {
		t.Fatalf("layer-0 payloads: %+v", payloads)
	}
}

// A summary never names a transient owner of its producer: closed owners
// are exported as ⊤, which refuses every dereference just as Closed does.
func TestFailureEvidenceTransportsClosedOwnersAsTop(t *testing.T) {
	facts := []OwnershipFact{{Path: "file", Status: "owned", ownerKind: ownershipOwnerScope, Region: "scope:1"}, {Path: "other", Status: "borrowed", Region: "parameter:p", ownerKind: ownershipOwnerParameter}}
	got := transportableFacts(facts)
	if len(got) != 2 || closedOwnerKind(got[0].ownerKind) || !got[0].potentialOwner || got[1] != facts[1] {
		t.Fatalf("transport: %+v", got)
	}
}

// TestNumberForksCountsTheWidestAlternative: the children of the arms of one
// branch never exist together, so a branch is as wide as its widest arm and
// the owner counts that, plus every child outside it.
func TestNumberForksCountsTheWidestAlternative(t *testing.T) {
	child := func(fork string, path ...string) forkAlternatives { return forkAlternatives{fork: fork, path: path} }
	cases := []struct {
		name     string
		children []forkAlternatives
		width    int
		ids      map[string]int
	}{
		{"none", nil, 0, map[string]int{}},
		{"sequential", []forkAlternatives{child("a"), child("b")}, 2, map[string]int{"a": 0, "b": 1}},
		{"one per arm", []forkAlternatives{child("a", "1.0"), child("b", "1.1")}, 1, map[string]int{"a": 0, "b": 0}},
		{"widest arm", []forkAlternatives{child("a", "1.0"), child("b", "1.0"), child("c", "1.1")}, 2, map[string]int{"a": 0, "b": 1, "c": 0}},
		{"child after the branch", []forkAlternatives{child("a", "1.0"), child("b", "1.1"), child("c")}, 2, map[string]int{"c": 0, "a": 1, "b": 1}},
		{"nested", []forkAlternatives{child("a", "1.0", "2.0"), child("b", "1.0", "2.1"), child("c", "1.1")}, 1, map[string]int{"a": 0, "b": 0, "c": 0}},
		{"two branches", []forkAlternatives{child("a", "1.0"), child("b", "1.1"), child("c", "2.0"), child("d", "2.1")}, 2, map[string]int{"a": 0, "b": 0, "c": 1, "d": 1}},
	}
	for _, c := range cases {
		ids := map[string]int{}
		if width := numberForks(c.children, ids); width != c.width || !maps.Equal(ids, c.ids) {
			t.Errorf("%s: width %d ids %v, want %d %v", c.name, width, ids, c.width, c.ids)
		}
		if width := numberForks(c.children, nil); width != c.width {
			t.Errorf("%s: counting without ids gave %d, want %d", c.name, width, c.width)
		}
	}
}
