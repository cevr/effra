package check

import "ergo.local/prototype/internal/sketch/syntax"

// TotalityBudget is the solver's step budget, for the tests' bounds.
const TotalityBudget = totalityBudget

// checkFor checks src as one package and returns the checker, for the
// tests that read its deterministic counters.
func checkFor(src string) (*checker, []Diagnostic) {
	file, err := syntax.Parse(src)
	if err != nil {
		return nil, []Diagnostic{{Code: CodeSyntax, Message: err.Error()}}
	}
	c := newChecker("main", Std, prelude().scope)
	c.run([]*syntax.File{file})
	return c, c.sorted()
}

// TotalitySteps checks src as one package and returns the solver steps of
// each type switch, in check order, with the diagnostics. Steps are
// deterministic, so a test bounds them where a wall-clock bound would
// depend on the host's load.
func TotalitySteps(src string) ([]int, []Diagnostic) {
	c, diags := checkFor(src)
	if c == nil {
		return nil, diags
	}
	return c.totalitySteps, diags
}

// SolverSteps checks src and returns, for each type switch in check order,
// the totality steps and the reachability steps, with the diagnostics.
func SolverSteps(src string) (total, useful []int, diags []Diagnostic) {
	c, diags := checkFor(src)
	if c == nil {
		return nil, nil, diags
	}
	return c.totalitySteps, c.usefulSteps, diags
}

// UsefulSteps checks src and returns the reachability steps of each type
// switch, in check order, with the diagnostics.
func UsefulSteps(src string) ([]int, []Diagnostic) {
	c, diags := checkFor(src)
	if c == nil {
		return nil, diags
	}
	return c.usefulSteps, diags
}

// BodyGraphEdges checks src and returns the number of edges of its body
// graph (round-3 M5: linear in the source, never one per same-named
// method), with the diagnostics.
func BodyGraphEdges(src string) (int, []Diagnostic) {
	c, diags := checkFor(src)
	if c == nil {
		return -1, diags
	}
	return c.bodyEdges, diags
}

// VariantOps checks src and returns the variant-index operations that
// resolved its type-switch cases (round-5 M2: linear in the source), with
// the diagnostics.
func VariantOps(src string) (int, []Diagnostic) {
	c, diags := checkFor(src)
	if c == nil {
		return -1, diags
	}
	return c.variantOps, diags
}
