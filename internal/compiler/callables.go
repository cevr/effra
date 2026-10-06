package compiler

import (
	"strconv"
	"strings"
)

// sourceType is parsed syntax, not a second semantic type authority. Its
// strings reference other parsed source types or ordinary nominal names.
// Checking resolves the tree into the common numeric type and row arena.
type sourceType struct {
	Effect         bool
	Parameters     []string
	ParameterTypes []*sourceType
	Result         string
	ResultType     *sourceType
	Failures       []string
	Services       []string
}

func (t *sourceType) display() string {
	kind := "fn"
	if t.Effect {
		kind = "effect fn"
	}
	result := t.Result
	if t.ResultType != nil {
		result = "(" + result + ")"
	}
	text := kind + "(" + strings.Join(t.Parameters, ", ") + ") -> " + result
	if len(t.Failures) > 0 {
		text += " raises {" + strings.Join(normalized(t.Failures), ", ") + "}"
	}
	if len(t.Services) > 0 {
		text += " uses {" + strings.Join(normalized(t.Services), ", ") + "}"
	}
	return text
}

func (c *checker) sourceCallable(t *sourceType) TypeID {
	args := make([]TypeID, 0, len(t.Parameters))
	for _, name := range t.Parameters {
		args = append(args, c.canonicalRef(typeRef(name)))
	}
	mode := "pure"
	if t.Effect {
		mode = "effect"
	}
	return c.internContract("callable", mode, c.canonicalRef(typeRef(t.Result)), args, c.internRow(t.Failures), c.internRow(t.Services))
}

func (c *checker) sourceCallableKnown(t *sourceType) bool {
	if !t.Effect && (len(t.Failures) > 0 || len(t.Services) > 0) {
		return false
	}
	if !c.typeKnown(t.Result) {
		return false
	}
	for _, name := range t.Parameters {
		if !c.typeKnown(name) {
			return false
		}
	}
	for _, name := range t.Failures {
		if _, ok := c.program.Errors[name]; !ok {
			return false
		}
	}
	for _, name := range t.Services {
		known := c.services[name] != nil
		for _, service := range c.program.Services {
			known = known || service.Name == name
		}
		if !known {
			return false
		}
	}
	return true
}

// assignable is the single directional value relation. Nominal values remain
// invariant; callable inputs are contravariant, results covariant, and public
// rows are upper bounds. Pure/effect callable modes are never implicitly lifted.
func (c *checker) assignable(actual, expected TypeID, depth int) bool {
	if actual == expected {
		return true
	}
	if depth > 64 {
		return false
	}
	a, b := c.node(actual), c.node(expected)
	if a == nil || b == nil {
		return false
	}
	if a.Kind == "never" {
		return true
	}
	if a.Kind != "callable" || b.Kind != "callable" || a.Mode != b.Mode || len(a.Args) != len(b.Args) {
		return false
	}
	if !c.assignable(a.Result, b.Result, depth+1) {
		return false
	}
	for i := range a.Args {
		if !c.assignable(b.Args[i], a.Args[i], depth+1) {
			return false
		}
	}
	return len(difference(c.rowLabels(a.FailureRow), c.rowLabels(b.FailureRow))) == 0 && len(difference(c.rowLabels(a.ServiceRow), c.rowLabels(b.ServiceRow))) == 0
}

// callableCall handles lexical values and record fields through their checked
// contract. Named declarations and nominal service operations use the same
// assignability relation at their existing call owner.
func (c *checker) callableCall(e *Expr, env map[string]checkedExpression, inEffect bool) (checkedExpression, bool) {
	local := false
	if e.Left.Kind == "name" {
		_, local = env[e.Left.Name]
	}
	if e.Left.Kind == "member" {
		if e.Left.Left.Kind == "name" {
			_, local = env[e.Left.Left.Name]
		} else {
			local = true
		}
	}
	if !local {
		return checkedExpression{}, false
	}
	callee := c.expr(e.Left, env, inEffect)
	node := callee.node()
	if node == nil || node.Kind != "callable" {
		c.diagnostic("EF103", "local value is not callable", e.Span)
		return c.checkedData("invalid"), true
	}
	if len(e.Args) != len(node.Args) {
		c.diagnostic("EF106", "incorrect callback argument count", e.Span)
	}
	arguments := make([]checkedExpression, len(e.Args))
	for i, arg := range e.Args {
		arguments[i] = c.expr(arg, env, inEffect)
		if hasPotentialOwner(arguments[i].ownershipFacts()) || hasOwnedClosed(arguments[i].ownershipFacts()) {
			c.diagnostic("EF123", "value owned by a closing scope cannot be used", arg.Span)
		}
		if i < len(node.Args) && !c.assignable(arguments[i].valueID(), node.Args[i], 0) {
			c.diagnostic("EF106", "callback argument has incompatible type", arg.Span)
		}
	}
	ownership := c.unknownOwnership(c.displayTypeID(node.Result))
	for i := range ownership {
		ownership[i].potentialOwner = true
		ownership[i].Origin = "callback-result"
	}
	if callee.callableDecl != nil {
		ownership = instantiateCheckedFacts(callee.callableDecl.Ownership, callee.callableDecl.Params, arguments)
	}
	var value CheckedValue
	if node.Mode == "effect" {
		value = c.values.recipe(node.Result, node.Args, checkedEffectCallable, node.FailureRow, node.ServiceRow, ownership, nil)
	} else {
		value = c.values.occurrence(node.Result, ownership, nil)
	}
	e.Text = "callable"
	return checkedExpression{value: value}, true
}

func goSourceType(t *sourceType, fallback string) string {
	if t == nil {
		return goType(fallback)
	}
	// Nested children are rendered from the canonical parsed syntax retained
	// on each occurrence in the source type graph by source rendering below.
	args := make([]string, len(t.Parameters))
	for i, name := range t.Parameters {
		args[i] = goSourceType(t.ParameterTypes[i], name)
	}
	result := goSourceType(t.ResultType, t.Result)
	if t.Effect {
		result = "efEffect[" + result + "]"
	}
	return "func(" + strings.Join(args, ", ") + ") " + result
}

func jsSourceType(t *sourceType, fallback string, declarations ...map[string]Declaration) string {
	if t == nil {
		return jsValueType(fallback)
	}
	args := make([]string, len(t.Parameters))
	var declared map[string]Declaration
	if len(declarations) > 0 {
		declared = declarations[0]
	}
	for i, name := range t.Parameters {
		args[i] = "arg" + strconv.Itoa(i) + ": " + jsSourceType(t.ParameterTypes[i], name, declared)
	}
	f := &Function{Return: t.Result, returnType: t.ResultType, Effect: t.Effect, Errors: t.Failures, Services: t.Services}
	return "(" + strings.Join(args, ", ") + ") => " + jsContractFor(f, declared)
}
