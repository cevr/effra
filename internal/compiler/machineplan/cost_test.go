package machineplan

import (
	"strings"
	"testing"
)

// costTypes is a hand-built type table: Outer<R>, where
//
//	record Outer<T: type> { inner: Inner<T>; run: Effect<T, { Boom… }, { Console }> }
//	record Inner<T: type> { value: T }
//
// and the declaration names are long enough that every term of the cost
// model contributes whole units.
func costTypes() *Types {
	long := func(prefix string, n int) string { return prefix + strings.Repeat("x", n) }
	outer, inner, boom := long("Outer", 200), long("Inner", 300), long("Boom", 400)
	nodes := map[TypeRef]*TypeNode{
		1: {Kind: "application", Name: outer, Declaration: outer, Args: []TypeRef{2}},
		2: {Kind: "record", Name: "R", Declaration: "R"},
		3: {Kind: "parameter", Name: "T"},
		4: {Kind: "application", Name: inner, Declaration: inner, Args: []TypeRef{3}},
		5: {Kind: "recipe", Name: "Effect", Result: 3, Failures: []string{boom}, Services: []string{"Console"}},
		6: {Kind: "recipe", Name: "Effect", Result: 7, Failures: []string{boom}},
		7: {Kind: "recipe", Name: "Effect", Result: 2},
	}
	return &Types{
		Nodes: nodes,
		Templates: map[string]*Template{
			outer: {Params: []TemplateParam{{Type: true, Constraint: -1}}, Fields: []TypeRef{4, 5}},
			inner: {Params: []TemplateParam{{Type: true, Constraint: -1}}, Fields: []TypeRef{3}},
		},
	}
}

// The cost model is what admits or refuses a declaration, and what bounds
// the checker's preparation. Its value for a fixed table is pinned, so
// dropping or changing any term (the qualifier, the per-field copy, the key
// text with its per-node, name, argument and label bytes, the template
// argument walks, or a display's copies) fails here even where the term only
// sets the refusal point.
func TestLayoutUnitsPinEveryTerm(t *testing.T) {
	var c Costs
	types := costTypes()
	units, ok := c.LayoutUnits(types, 1)
	if !ok {
		t.Fatal("Outer<R> has no layout")
	}
	// 59 = 6 + 9 + 43 + 1:
	//   - qualifier: 1 + ⌈(205+64)/64⌉ = 6;
	//   - fields: 1 + 4·2 = 9;
	//   - key text ⌈2,712/64⌉ = 43: Inner<T> is 128 + 2·(11+305+305) + 24·1
	//     = 1,394 bytes plus T (128 + 2·(9+1) = 148); the recipe is 128 +
	//     2·(6+6) = 152, plus its labels (2·404+24 and 2·7+24), plus T;
	//   - walks ⌈7/8⌉ = 1: each field walks its 2 nodes, and Inner<T>'s type
	//     parameter validates the substituted argument (2+1 nodes) again.
	if units != 59 {
		t.Errorf("LayoutUnits(Outer<R>) = %d, want 59", units)
	}
	if units, ok := c.LayoutUnits(types, 2); !ok || units != 3 {
		t.Errorf("LayoutUnits(R) = %d %t, want 3 true", units, ok)
	}
	if _, ok := c.LayoutUnits(types, 5); ok {
		t.Error("a recipe has a layout")
	}
}

func TestDisplayUnitsPinEveryTerm(t *testing.T) {
	var c Costs
	types := costTypes()
	// ⌈2,349/64⌉ = 37. The inner Effect<R> is 9 bytes long and builds 82
	// (2·9 + 64). The outer one is 8 + 9 + 4 + (404+2) + 4 = 431 bytes long, and
	// with a row it copies that 5 times: 82 + 5·431 + 48·1 label + 64.
	if units := c.DisplayUnits(types, 6); units != 37 {
		t.Errorf("DisplayUnits(Effect<Effect<R>, {Boom…}>) = %d, want 37", units)
	}
}
