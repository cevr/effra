package machineplan

import (
	"slices"
	"strings"
	"testing"

	"ergo.local/prototype/internal/compiler/machineplan/shared"
)

// costTypes is a hand-built type table: Outer<R>, where
//
//	record Outer<T: type> { inner: Inner<T>; run: Effect<T, { Boom… }, { Console }> }
//	record Inner<T: type> { value: T }
//
// and the declaration names are long enough that every term of the cost
// model contributes whole units; Outer<Wide>, whose wide argument makes the
// type walk count; Handler<R>, whose field applies a template with a callable
// parameter; and the wrapper displays Fiber<(i64, i64)> and GoResult<i64>.
func costTypes() *Types {
	long := func(prefix string, n int) string { return prefix + strings.Repeat("x", n) }
	outer, inner, boom := long("Outer", 200), long("Inner", 300), long("Boom", 400)
	nodes := map[TypeRef]*TypeNode{
		1: {Kind: "application", Name: outer, Declaration: outer, Args: []TypeRef{2}},
		2: {Kind: "record", Name: "R", Declaration: "R"},
		3: {Kind: "parameter", Name: "T"},
		4: {Kind: "application", Name: inner, Declaration: inner, Args: []TypeRef{3}},
		5: {Kind: "recipe", Name: "Effect", Result: 3, Failures: shared.Of([]string{boom}), Services: shared.Of([]string{"Console"})},
		6: {Kind: "recipe", Name: "Effect", Result: 7, Failures: shared.Of([]string{boom})},
		7: {Kind: "recipe", Name: "Effect", Result: 2},
		// Outer<Wide>: an argument of 65 nodes, so Inner<T>'s type walk covers
		// a substituted argument wide enough to cross units.
		8:  {Kind: "application", Name: outer, Declaration: outer, Args: []TypeRef{9}},
		9:  {Kind: "record", Name: "Wide", Declaration: "Wide", Args: slices.Repeat([]TypeRef{10}, 64)},
		10: {Kind: "primitive", Name: "i64"},
		// Handler<R>, whose one field is Call<T>: Call's parameter is callable
		// with a one-parameter constraint, so the field repeats two capped walks.
		11: {Kind: "application", Name: "Call", Declaration: "Call", Args: []TypeRef{3}},
		12: {Kind: "application", Name: "Handler", Declaration: "Handler", Args: []TypeRef{2}},
		// Wrapper displays: Fiber<(i64, i64)> and GoResult<i64>.
		13: {Kind: "goValues", Args: []TypeRef{10, 10}},
		14: {Kind: "fiber", Args: []TypeRef{13}},
		15: {Kind: "goResult", Args: []TypeRef{10}},
	}
	return &Types{
		Nodes: nodes,
		Templates: map[string]*Template{
			outer:     {Params: []TemplateParam{{Type: true, Constraint: -1}}, Fields: []TypeRef{4, 5}},
			inner:     {Params: []TemplateParam{{Type: true, Constraint: -1}}, Fields: []TypeRef{3}},
			"Call":    {Params: []TemplateParam{{Constraint: 1}}},
			"Handler": {Params: []TemplateParam{{Type: true, Constraint: -1}}, Fields: []TypeRef{11}},
		},
	}
}

// The cost model is what admits or refuses a declaration, and what bounds
// the checker's preparation. Its value for a fixed table is pinned, so
// dropping any term (the qualifier, the per-field copy, the key text with its
// per-node, name, argument and label bytes, the template argument walks, or a
// display's copies) fails here even where the term only sets the refusal
// point. Values are whole units of 64 bytes or 8 nodes, so a change to a
// term's constant that stays within the same unit is not seen: the tuple
// slot at 15 bytes instead of 16 still makes Fiber<(i64, i64)> 2 units.
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
	// 67 = 6 + 9 + 43 + 9: Outer<Wide> differs from Outer<R> only in the type
	// walk. Wide is 65 nodes (itself and i64 64 times), so Inner<T>'s walk
	// covers 2 + 65 substituted nodes: walks ⌈(2 + 67 + 2)/8⌉ = 9.
	if units, ok := c.LayoutUnits(types, 8); !ok || units != 67 {
		t.Errorf("LayoutUnits(Outer<Wide>) = %d %t, want 67 true", units, ok)
	}
	// 1,039 = 3 + 5 + 6 + 1,025:
	//   - qualifier: 1 + ⌈(7+64)/64⌉ = 3;
	//   - fields: 1 + 4·1 = 5;
	//   - key text ⌈338/64⌉ = 6: Call<T> is 128 + 2·(11+4+4) + 24·1 = 190
	//     bytes, plus T's 148;
	//   - walks ⌈(2 + 2·4,098)/8⌉ = 1,025: Call<T>'s 2 nodes, and its callable
	//     parameter's 1 + 1 walks, each capped at reachCap.
	if units, ok := c.LayoutUnits(types, 12); !ok || units != 1039 {
		t.Errorf("LayoutUnits(Handler<R>) = %d %t, want 1039 true", units, ok)
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
	// ⌈71/64⌉ = 2. (i64, i64) is 2 + 3 + 2 + 3 = 10 long and builds
	// 16·2 component slots + 2·10 = 52; the fiber is 9 + 10 = 19 long and
	// copies it once more: 52 + 19 = 71. Each wrapper term alone moves the
	// bytes to 64 or fewer.
	if units := c.DisplayUnits(types, 14); units != 2 {
		t.Errorf("DisplayUnits(Fiber<(i64, i64)>) = %d, want 2", units)
	}
	// GoResult<i64> is 9 + 3 = 12 long and builds 12 bytes: one unit.
	if units := c.DisplayUnits(types, 15); units != 1 {
		t.Errorf("DisplayUnits(GoResult<i64>) = %d, want 1", units)
	}
}
