package compiler

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// TestEvidenceSchemaIsClassified audits schema accounting, not the behavior of
// occurrence rewrites or joins. Evidence and row fields are traversed;
// explicitly classified metadata ends traversal.
func TestEvidenceSchemaIsClassified(t *testing.T) {
	audit := func(slots map[string]occurrenceSlotClass) (missing, stale []string) {
		seen := map[reflect.Type]bool{}
		classified := map[string]bool{}
		var walk func(reflect.Type)
		walk = func(typ reflect.Type) {
			switch typ.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Array:
				walk(typ.Elem())
				return
			case reflect.Map:
				walk(typ.Key())
				walk(typ.Elem())
				return
			case reflect.Struct:
			default:
				return
			}
			if seen[typ] {
				return
			}
			seen[typ] = true
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := typ.Name() + "." + field.Name
				class, ok := slots[name]
				if !ok {
					missing = append(missing, name)
					continue
				}
				classified[name] = true
				if class != occurrenceMetadata {
					walk(field.Type)
				}
			}
		}
		walk(reflect.TypeOf(checkedExpression{}))
		for name := range slots {
			if !classified[name] {
				stale = append(stale, name)
			}
		}
		return missing, stale
	}
	cloneSlots := func() map[string]occurrenceSlotClass {
		slots := make(map[string]occurrenceSlotClass, len(occurrenceSlots))
		for name, class := range occurrenceSlots {
			slots[name] = class
		}
		return slots
	}

	t.Run("complete schema", func(t *testing.T) {
		missing, stale := audit(occurrenceSlots)
		if len(missing) != 0 || len(stale) != 0 {
			t.Fatalf("schema classification: missing %v; stale %v", missing, stale)
		}
	})
	t.Run("missing live field", func(t *testing.T) {
		slots := cloneSlots()
		delete(slots, "CheckedValue.ownership")
		missing, stale := audit(slots)
		if len(missing) != 1 || missing[0] != "CheckedValue.ownership" || len(stale) != 0 {
			t.Fatalf("missing-field audit: missing %v; stale %v", missing, stale)
		}
	})
	t.Run("stale field", func(t *testing.T) {
		slots := cloneSlots()
		slots["CheckedValue.noSuchField"] = occurrenceEvidence
		missing, stale := audit(slots)
		if len(missing) != 0 || len(stale) != 1 || stale[0] != "CheckedValue.noSuchField" {
			t.Fatalf("stale-field audit: missing %v; stale %v", missing, stale)
		}
	})
}

func lawFact(path, region string, kind ownershipOwnerKind) OwnershipFact {
	return OwnershipFact{Path: path, Status: "owned", Region: region, Origin: "law", ownerKind: kind}
}

func lawOccurrence(ownership, captures, child []OwnershipFact, fields map[string]checkedExpression) checkedExpression {
	return checkedExpression{value: CheckedValue{ownership: ownership, captures: captures}, child: child, fields: fields}
}

// lawChannels names the evidence channels beyond the owner facts that an
// occurrence of the law tree carries: payload evidence of one ordinary and
// one pending failure, the fork keys it may denote and the keys it observes.
type lawChannels struct {
	label    string
	forks    []string
	observes []string
}

// lawRecipe is an effect recipe whose failure row is {label}, with every
// channel populated under tag.
func lawRecipe(c *checker, tag string, channels lawChannels, ownership, captures, child []OwnershipFact, fields map[string]checkedExpression) checkedExpression {
	void := c.canonicalRef(typeRef(voidTypeName))
	e := checkedExpression{child: child, fields: fields}
	e.value = c.values.recipe(void, checkedEffectCallable, c.internRow([]string{channels.label}), emptyRowID, ownership, captures)
	e.failures = failureEvidence{
		raisedKey(0, channels.label):                                          {lawFact("", "fail:"+tag, ownershipOwnerLexical)},
		{layer: 0, label: channels.label, pending: true, fork: "fork:" + tag}: {lawFact("", "pend:"+tag, ownershipOwnerLexical)},
	}
	e.forks, e.observes = channels.forks, channels.observes
	return e
}

// lawTree is a three-level occurrence with every slot populated: owner
// facts, captures, child, payload and pending evidence, forks and
// observations. suffix names the alternative the tree belongs to.
func lawTree(c *checker, suffix string, channels lawChannels) checkedExpression {
	leaf := lawRecipe(c, "leaf"+suffix, channels, []OwnershipFact{lawFact("", "scope:1", ownershipOwnerLexical)}, []OwnershipFact{lawFact("", "scope:2", ownershipOwnerLexical)}, []OwnershipFact{lawFact("", "child:3", ownershipOwnerChild)}, nil)
	middle := lawRecipe(c, "middle"+suffix, channels, []OwnershipFact{lawFact("inner", "scope:4", ownershipOwnerLexical)}, nil, nil, map[string]checkedExpression{"inner": leaf})
	return lawRecipe(c, "root"+suffix, channels, []OwnershipFact{lawFact("outer.inner", "scope:5", ownershipOwnerLexical)}, []OwnershipFact{lawFact("", "scope:6", ownershipOwnerLexical)}, []OwnershipFact{lawFact("", "child:7", ownershipOwnerChild)}, map[string]checkedExpression{"outer": middle})
}

func lawFieldNames(e checkedExpression) []string {
	return slices.Sorted(func(yield func(string) bool) {
		for name := range e.fields {
			if !yield(name) {
				return
			}
		}
	})
}

// lawCollect is every fact of an occurrence and its fields, from every fact
// slot: ownership, captures, child and the payload evidence of each failure.
func lawCollect(e checkedExpression) []OwnershipFact {
	facts := slices.Concat(e.ownershipFacts(), e.captureFacts(), e.child)
	for _, key := range sortedFailureKeys(e.failures) {
		facts = append(facts, e.failures[key]...)
	}
	for _, name := range lawFieldNames(e) {
		facts = append(facts, lawCollect(e.fields[name])...)
	}
	return facts
}

// lawKeys is the key channels of an occurrence and its fields: the keys of
// its failure evidence, its forks and its observations, one line per node.
func lawKeys(e checkedExpression, path string) []string {
	var keys []string
	for _, key := range sortedFailureKeys(e.failures) {
		keys = append(keys, fmt.Sprintf("%s failure %+v", path, key))
	}
	keys = append(keys, fmt.Sprintf("%s forks %v", path, e.forks), fmt.Sprintf("%s observes %v", path, e.observes))
	for _, name := range lawFieldNames(e) {
		keys = append(keys, lawKeys(e.fields[name], path+"."+name)...)
	}
	return keys
}

func lawMap(e checkedExpression, rewrite func(OwnershipFact) OwnershipFact) checkedExpression {
	return mapOccurrenceFacts(e, func(_ checkedExpression, _ occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
		out := cloneFacts(facts)
		for i := range out {
			out[i] = rewrite(out[i])
		}
		return out
	})
}

var (
	lawLeft  = lawChannels{label: "A", forks: []string{"fork:left"}, observes: []string{"fork:left", "fork:both"}}
	lawRight = lawChannels{label: "B", forks: []string{"fork:right"}, observes: []string{"fork:both"}}
	lawThird = lawChannels{label: "C", forks: []string{"fork:third"}, observes: []string{"fork:both", "fork:third"}}
)

func TestOccurrenceWalkerIsIdentityUnderIdentity(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	tree := lawTree(c, "", lawLeft)
	mapped := mapOccurrenceFacts(tree, func(_ checkedExpression, _ occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact { return facts })
	if !reflect.DeepEqual(lawCollect(mapped), lawCollect(tree)) {
		t.Fatalf("identity rewrite changed evidence:\n%v\n%v", lawCollect(mapped), lawCollect(tree))
	}
}

func TestOccurrenceWalkerVisitsEverySlotAtEveryDepth(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	tree := lawTree(c, "", lawLeft)
	visits := map[occurrenceFactSlot]int{}
	mapped := mapOccurrenceFacts(tree, func(_ checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
		visits[slot]++
		out := cloneFacts(facts)
		for i := range out {
			out[i].Origin = "rewritten"
		}
		return out
	})
	for _, fact := range lawCollect(mapped) {
		if fact.Origin != "rewritten" {
			t.Fatalf("slot fact escaped the walker: %+v", fact)
		}
	}
	// Three occurrences; child is visited only where present (root, leaf),
	// and each occurrence has an ordinary and a pending payload.
	if visits[ownershipSlot] != 3 || visits[capturesSlot] != 3 || visits[childSlot] != 2 || visits[failuresSlot] != 6 {
		t.Fatalf("walker visits %v", visits)
	}
	if lawCollect(tree)[0].Origin != "law" {
		t.Fatal("walker mutated its input")
	}
}

// lawJoin joins two trees as an if does. The trees' contracts differ in
// their failure rows, as the alternatives of a branch do.
func lawJoin(c *checker, a, b checkedExpression) checkedExpression {
	joined := c.joinContractRows(c.joinOccurrence(a, b, Span{}), b)
	if len(c.result.Diagnostics) != 0 {
		panic(c.result.Diagnostics)
	}
	return joined
}

func TestJoinOccurrenceKeepsEverySlotOfBothAlternatives(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	left := lawTree(c, "", lawLeft)
	right := lawTree(c, "-right", lawRight)
	joined := lawJoin(c, left, right)
	// Owner sets are exact (design §3.2): same-path alternatives with
	// distinct owners join to the union, so every fact of both sides stays
	// in its slot, at every depth and in every channel (failures included).
	all := lawCollect(joined)
	for _, side := range [][]OwnershipFact{lawCollect(left), lawCollect(right)} {
		for _, fact := range side {
			if !slices.ContainsFunc(all, func(got OwnershipFact) bool {
				return got.Path == fact.Path && got.ownerKind == fact.ownerKind && got.Region == fact.Region
			}) {
				t.Errorf("join lost the fact %+v", fact)
			}
		}
	}
	union := map[string]bool{}
	for _, side := range [][]OwnershipFact{lawCollect(left), lawCollect(right)} {
		for _, fact := range side {
			union[fmt.Sprintf("%+v", fact)] = true
		}
	}
	if len(all) != len(union) {
		t.Errorf("join is not the union of both alternatives: %d facts for %d distinct", len(all), len(union))
	}
	// Keys: failure keys and fork instances are unioned, observations
	// intersected, at every depth.
	want := lawKeys(joined, "")
	for _, path := range []string{"", ".outer", ".outer.inner"} {
		for _, line := range []string{
			fmt.Sprintf("%s forks %v", path, []string{"fork:left", "fork:right"}),
			fmt.Sprintf("%s observes %v", path, []string{"fork:both"}),
			fmt.Sprintf("%s failure %+v", path, failureKey{label: "A"}),
			fmt.Sprintf("%s failure %+v", path, failureKey{label: "B"}),
		} {
			if !slices.Contains(want, line) {
				t.Errorf("joined occurrence lacks %q in %v", line, want)
			}
		}
	}
}

func TestJoinOccurrenceIsIdempotentSymmetricAndAssociative(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	a, b, d := lawTree(c, "-a", lawLeft), lawTree(c, "-b", lawRight), lawTree(c, "-c", lawThird)
	slots := func(e checkedExpression) []string {
		facts := lawCollect(e)
		out := make([]string, 0, len(facts))
		for _, fact := range facts {
			out = append(out, fmt.Sprintf("%+v", fact))
		}
		slices.Sort(out)
		return slices.Concat(out, lawKeys(e, ""))
	}
	for name, pair := range map[string][2]checkedExpression{
		"idempotent":  {lawJoin(c, a, a), a},
		"symmetric":   {lawJoin(c, a, b), lawJoin(c, b, a)},
		"associative": {lawJoin(c, lawJoin(c, a, b), d), lawJoin(c, a, lawJoin(c, b, d))},
	} {
		if got, want := slots(pair[0]), slots(pair[1]); !reflect.DeepEqual(got, want) {
			t.Errorf("join is not %s:\n%v\n%v", name, got, want)
		}
	}
}

// A field's join is the occurrence join: nothing the join of an occurrence
// keeps may be dropped from the join of a stored field, in either order.
func TestJoinFieldOccurrencesIsTheOccurrenceJoin(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	left := checkedExpression{fields: map[string]checkedExpression{"task": lawTree(c, "-l", lawLeft)}}
	right := checkedExpression{fields: map[string]checkedExpression{"task": lawTree(c, "-r", lawRight)}}
	for name, pair := range map[string][2]checkedExpression{"left-right": {left, right}, "right-left": {right, left}} {
		fields := c.joinFieldOccurrences(pair[0].fields, pair[1].fields, Span{}, 0)
		want := lawJoin(c, pair[0].fields["task"], pair[1].fields["task"])
		got := fields["task"]
		if !reflect.DeepEqual(lawCollect(got), lawCollect(want)) || !reflect.DeepEqual(lawKeys(got, ""), lawKeys(want, "")) {
			t.Errorf("%s: field join differs from the occurrence join:\n%v\n%v\n%v\n%v", name, lawCollect(got), lawCollect(want), lawKeys(got, ""), lawKeys(want, ""))
		}
	}
}

// Instantiating a stored field substitutes the call's arguments in every
// fact slot at every depth, payload evidence included.
func TestInstantiateFieldOccurrencesSubstitutesEverySlot(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	f := &Function{Name: "pack", Params: []Param{{Name: "p"}}}
	parameter := OwnershipFact{Status: "borrowed", Region: "parameter:p", Origin: "parameter", sourceSet: true, ownerKind: ownershipOwnerParameter}
	tree := lawMap(lawTree(c, "", lawLeft), func(fact OwnershipFact) OwnershipFact {
		parameter := parameter
		parameter.Path = fact.Path
		return parameter
	})
	args := []checkedExpression{lawOccurrence([]OwnershipFact{lawFact("", "scope:arg", ownershipOwnerLexical)}, nil, nil, nil)}
	fields := c.instantiateFieldOccurrences(map[string]checkedExpression{"task": tree}, f, args, nil, nil, 0)
	if len(c.result.Diagnostics) != 0 {
		t.Fatal(c.result.Diagnostics)
	}
	facts := lawCollect(fields["task"])
	if len(facts) != len(lawCollect(tree)) {
		t.Fatalf("instantiation changed the number of facts: %d vs %d", len(facts), len(lawCollect(tree)))
	}
	for _, fact := range facts {
		if fact.Region != "scope:arg" {
			t.Errorf("a slot kept the unsubstituted parameter owner: %+v", fact)
		}
	}
}

// A field table on one side only is joined to path facts alone: the joined
// path facts still describe both alternatives.
func TestJoinOccurrenceWithOneFieldTableFallsBackToPaths(t *testing.T) {
	c := newChecker(&Program{}, &Result{})
	left := lawTree(c, "", lawLeft)
	right := lawOccurrence([]OwnershipFact{lawFact("outer", "scope:9", ownershipOwnerLexical)}, nil, nil, nil)
	joined := c.joinOccurrence(left, right, Span{})
	if len(c.result.Diagnostics) != 0 || joined.fields != nil {
		t.Fatalf("one-sided field join: %v %v", c.result.Diagnostics, joined.fields)
	}
	if !slices.ContainsFunc(joined.ownershipFacts(), func(f OwnershipFact) bool { return f.Region == "scope:9" }) {
		t.Fatal("path facts of the right alternative were dropped")
	}
}
