package compiler

import (
	"reflect"
	"testing"
)

// layerLawOccurrence is a two-layer recipe occurrence whose slots name
// every kind of owner atom: Exec levels, a body region, a closed owner and a
// parameter, at held layers 0 and 1 and in a field occurrence.
func layerLawOccurrence() checkedExpression {
	at := func(fact OwnershipFact, path string, layer int) OwnershipFact {
		fact.Path, fact.layer, fact.Origin = path, layer, "law"
		return fact
	}
	parameter := OwnershipFact{Status: "borrowed", Region: "parameter:p", source: "", sourceSet: true, ownerKind: ownershipOwnerParameter}
	field := lawOccurrence(normalizeFacts([]OwnershipFact{at(execOwner(2), "", 0), at(lexicalOwner("scope:9"), "", 0)}), nil, nil, nil)
	return lawOccurrence(
		normalizeFacts([]OwnershipFact{at(execOwner(1), "file", 0), at(lexicalOwner("scope:9"), "file", 0), at(closedOwner(ownershipOwnerTimeout, "timeout:4"), "other", 0)}),
		normalizeFacts([]OwnershipFact{at(parameter, "capture:p", 0), at(lexicalOwner("scope:9"), "capture:g", 0), at(execOwner(0), "capture:h", 1)}),
		nil,
		map[string]checkedExpression{"inner": field},
	)
}

// exec after lift is the identity: the lifted layer holds nothing and its
// executor names no atom of the occurrence.
func TestOwnerLayersExecUndoesLift(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	occurrence := layerLawOccurrence()
	got := c.executeOccurrence(c.liftOccurrence(occurrence), lexicalOwner("run:1"))
	if !reflect.DeepEqual(lawCollect(got), lawCollect(occurrence)) {
		t.Fatalf("exec(lift(L)) != L:\n%v\n%v", lawCollect(got), lawCollect(occurrence))
	}
}

// The function edge and its execution are inverse: executing abstract(L, r)
// under Lexical(r) restores L.
func TestOwnerLayersExecUndoesAbstract(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	occurrence := layerLawOccurrence()
	abstracted := c.abstractOccurrence(occurrence, "scope:9")
	for _, fact := range lawCollect(abstracted) {
		if fact.ownerKind == ownershipOwnerLexical {
			t.Fatalf("abstract left the body region: %+v", fact)
		}
	}
	got := c.executeOccurrence(abstracted, lexicalOwner("scope:9"))
	if !reflect.DeepEqual(lawCollect(got), lawCollect(occurrence)) {
		t.Fatalf("exec(abstract(L, r), r) != L:\n%v\n%v", lawCollect(got), lawCollect(occurrence))
	}
}

// close binds layer 0 to a closed owner and keeps every binder: later levels
// and held layers are unchanged.
func TestOwnerLayersCloseKeepsBinders(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	occurrence := layerLawOccurrence()
	closed := c.closeOccurrence(occurrence, closedOwner(ownershipOwnerTimeout, "timeout:7"))
	before, after := lawCollect(occurrence), lawCollect(closed)
	if len(before) != len(after) {
		t.Fatalf("close changed the fact count:\n%v\n%v", before, after)
	}
	for i, fact := range after {
		original := before[i]
		if original.ownerKind == ownershipOwnerExec && original.exec == 0 {
			if fact.ownerKind != ownershipOwnerTimeout || fact.Region != "timeout:7" || fact.layer != original.layer {
				t.Fatalf("Exec(0) was not bound to the closed owner: %+v", fact)
			}
			continue
		}
		if fact != original {
			t.Fatalf("close rewrote an atom it does not bind: %+v -> %+v", original, fact)
		}
	}
}

// Substituting an owner vector generalizes exec: exec(L, o) is the vector
// [o] with tail Exec(0), and the identity vector of any length is the
// identity.
func TestOwnerLayersSubstitutionAgreesWithExec(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	occurrence := layerLawOccurrence()
	owner := lexicalOwner("run:1")
	viaExec := c.rewriteOccurrence(occurrence, executeRewrite(owner))
	viaEnv := c.rewriteOccurrence(occurrence, envRewrite(decodeOwnerEnv(encodeOwnerEnv([]OwnershipFact{owner, execOwner(0)}))))
	if !reflect.DeepEqual(lawCollect(viaExec), lawCollect(viaEnv)) {
		t.Fatalf("exec and its owner vector disagree:\n%v\n%v", lawCollect(viaExec), lawCollect(viaEnv))
	}
	for layers := range 4 {
		identity := c.rewriteOccurrence(occurrence, envRewrite(identityOwnerEnv(layers)))
		if !reflect.DeepEqual(lawCollect(identity), lawCollect(occurrence)) {
			t.Fatalf("identity vector of %d layers rewrote the occurrence:\n%v", layers, lawCollect(identity))
		}
	}
}
