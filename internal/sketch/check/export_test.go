package check

import "ergo.local/prototype/internal/sketch/syntax"

// TotalityBudget is the solver's step budget, for the tests' bounds.
const TotalityBudget = totalityBudget

// TotalitySteps checks src as one package and returns the solver steps of
// each type switch, in check order, with the diagnostics. Steps are
// deterministic, so a test bounds them where a wall-clock bound would
// depend on the host's load.
func TotalitySteps(src string) ([]int, []Diagnostic) {
	file, err := syntax.Parse(src)
	if err != nil {
		return nil, []Diagnostic{{Code: CodeSyntax, Message: err.Error()}}
	}
	c := newChecker("main", Std, prelude().scope)
	c.run([]*syntax.File{file})
	return c.totalitySteps, c.sorted()
}
