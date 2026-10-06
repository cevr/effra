package compiler

import (
	"slices"
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
	return c.internContract("callable", mode, c.canonicalRef(typeRef(t.Result)), args, c.internRow(c.sourceRow(t.Failures, "raises")), c.internRow(c.sourceRow(t.Services, "uses")))
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
		if c.rowParameter(name, "raises") {
			continue
		}
		if _, ok := c.program.Errors[name]; !ok {
			return false
		}
	}
	for _, name := range t.Services {
		if c.rowParameter(name, "uses") {
			continue
		}
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

// Row variables are qualified by their declaration, never matched by the
// source spelling shared by unrelated functions. The finite solver admits one
// variable per callback row and obtains its least bound from actual arguments.
func (c *checker) functionRows(f *Function) map[string]RowParameter {
	context := map[string]RowParameter{}
	if c.rowDefinitions == nil {
		c.rowDefinitions = map[string]RowParameter{}
	}
	for i := range f.RowParameters {
		p := &f.RowParameters[i]
		p.Declaration = f.Identity
		p.ID = "row-parameter:" + f.Identity + ":" + p.Kind + ":" + p.Name
		if _, duplicate := context[p.Name]; duplicate {
			c.diagnostic("EF125", "duplicate row parameter "+p.Name, p.Span)
		}
		if f.Owner != "module" && f.Owner != "" {
			c.diagnostic("EF125", "row parameters are supported on ordinary module functions", p.Span)
		}
		context[p.Name] = *p
		c.rowDefinitions[p.ID] = *p
	}
	return context
}

func (c *checker) rowParameter(name, kind string) bool {
	p, ok := c.rowContext[name]
	return ok && p.Kind == kind
}

func (c *checker) sourceRow(labels []string, kind string) []string {
	result := make([]string, len(labels))
	for i, label := range labels {
		result[i] = label
		if p, ok := c.rowContext[label]; ok && p.Kind == kind {
			result[i] = p.ID
		}
	}
	return result
}

func (c *checker) instantiateRow(id RowID, bindings map[string][]string) RowID {
	var labels []string
	for _, label := range c.rowLabels(id) {
		if bound, ok := bindings[label]; ok {
			labels = union(labels, bound)
		} else {
			labels = union(labels, []string{label})
		}
	}
	return c.internRow(labels)
}

func (c *checker) abstractRow(id RowID) bool {
	for _, label := range c.rowLabels(id) {
		if _, ok := c.rowDefinitions[label]; ok {
			return true
		}
	}
	return false
}

func (c *checker) instantiateType(id TypeID, bindings map[string][]string, depth int) TypeID {
	if depth > 64 {
		return invalidTypeID
	}
	n := c.node(id)
	if n == nil {
		return invalidTypeID
	}
	if n.Kind != "callable" && n.Kind != "recipe" {
		return id
	}
	args := make([]TypeID, len(n.Args))
	for i, arg := range n.Args {
		args[i] = c.instantiateType(arg, bindings, depth+1)
	}
	return c.internContract(n.Kind, n.Mode, c.instantiateType(n.Result, bindings, depth+1), args, c.instantiateRow(n.FailureRow, bindings), c.instantiateRow(n.ServiceRow, bindings))
}

func (c *checker) inferRows(f *Function, arguments []checkedExpression, span Span) map[string][]string {
	bindings := map[string][]string{}
	formalVariables := map[string]bool{}
	for _, p := range f.RowParameters {
		formalVariables[p.ID] = true
	}
	for i, p := range f.Params {
		if i >= len(arguments) {
			break
		}
		formal, actual := c.node(p.typeID), c.node(arguments[i].valueID())
		if formal == nil || actual == nil || formal.Kind != "callable" || actual.Kind != "callable" {
			continue
		}
		for _, pair := range [][2]RowID{{formal.FailureRow, actual.FailureRow}, {formal.ServiceRow, actual.ServiceRow}} {
			var variable string
			var fixed []string
			for _, label := range c.rowLabels(pair[0]) {
				if formalVariables[label] {
					if variable != "" {
						c.diagnostic("EF125", "one inferred row parameter per callback row is supported", span)
					}
					variable = label
				} else {
					fixed = append(fixed, label)
				}
			}
			if variable != "" {
				bound := difference(c.rowLabels(pair[1]), fixed)
				// Subtracting concrete labels from an abstract row requires a
				// difference constraint, outside this finite union solver.
				if len(fixed) > 0 {
					for _, label := range bound {
						if _, abstract := c.rowDefinitions[label]; abstract {
							c.diagnostic("EF125", "abstract row subtraction is unsupported", span)
						}
					}
				}
				bindings[variable] = union(bindings[variable], bound)
			}
		}
	}
	for _, p := range f.RowParameters {
		if _, solved := bindings[p.ID]; !solved {
			c.diagnostic("EF125", "row parameter "+p.Name+" must be inferred from a direct callback argument", span)
			bindings[p.ID] = nil
		}
	}
	for i, p := range f.Params {
		if i < len(arguments) && (arguments[i].isEffect() || !c.assignable(arguments[i].valueID(), c.instantiateType(p.typeID, bindings, 0), 0)) {
			c.diagnostic("EF106", "argument has incompatible instantiated callback contract", span)
		}
	}
	return bindings
}

func (c *checker) validateRowInference(f *Function) {
	if len(f.RowParameters) == 0 {
		return
	}
	bound := map[string]bool{}
	for _, p := range f.Params {
		n := c.node(p.typeID)
		if n == nil || n.Kind != "callable" {
			continue
		}
		for _, row := range []RowID{n.FailureRow, n.ServiceRow} {
			count := 0
			for _, label := range c.rowLabels(row) {
				if parameter, ok := c.rowDefinitions[label]; ok && parameter.Declaration == f.Identity {
					bound[label] = true
					count++
				}
			}
			if count > 1 {
				c.diagnostic("EF125", "one inferred row parameter per callback row is supported", p.Span)
			}
		}
	}
	for _, p := range f.RowParameters {
		if !bound[p.ID] {
			c.diagnostic("EF125", "row parameter "+p.Name+" must occur in a direct callback argument row", p.Span)
		}
	}
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
		if c.unsafePotentialOwner(arguments[i].ownershipFacts()) || hasOwnedClosed(arguments[i].ownershipFacts()) {
			c.diagnostic("EF123", "value owned by a closing scope cannot be used", arg.Span)
		}
		if i < len(node.Args) && !c.assignable(arguments[i].valueID(), node.Args[i], 0) {
			c.diagnostic("EF106", "callback argument has incompatible type", arg.Span)
		}
	}
	ownership := c.callbackResultOwnership(callee.callableEvidence, arguments, node.Result, 0)
	captures := cloneFacts(callee.captureFacts())
	if node.Mode == "effect" {
		for i, argument := range arguments {
			captures = append(captures, prependFacts("capture:arg"+strconv.Itoa(i), argument.ownershipFacts())...)
		}
	}
	var value CheckedValue
	if node.Mode == "effect" {
		value = c.values.recipe(node.Result, node.Args, checkedEffectCallable, node.FailureRow, node.ServiceRow, ownership, normalizeFacts(captures))
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
	nested := declared
	if len(declarations) > 1 {
		nested = declarations[1]
	}
	for i, name := range t.Parameters {
		args[i] = "arg" + strconv.Itoa(i) + ": " + jsSourceType(t.ParameterTypes[i], name, nested)
	}
	f := &Function{Return: t.Result, returnType: t.ResultType, Effect: t.Effect, Errors: t.Failures, Services: t.Services}
	return "(" + strings.Join(args, ", ") + ") => " + jsContractFor(f, declared, nested)
}

// Each actual callback gets its own TS inference variable. Reusing a single
// variable in several parameter positions makes TypeScript pick the first
// callback's row instead of the source solver's least union. The return view
// joins these witnesses and excludes the formal row's fixed labels.
func jsRowFunctionSignature(f *Function, declarations map[string]Declaration) string {
	if len(f.CallbackPolicies) > 0 {
		copy := *f
		copy.Params = append([]Param{}, f.Params...)
		copy.CallbackPolicies = nil
		for _, policy := range f.CallbackPolicies {
			if policy.Kind != "typed-failure-response" {
				continue
			}
			copy.Params[policy.Parameter].sourceType = &sourceType{Effect: true, Parameters: []string{"string"}, ParameterTypes: []*sourceType{nil}, Result: "string", Failures: []string{"__ef_transport_E"}, Services: []string{"__ef_transport_R"}}
			copy.RowParameters = append(copy.RowParameters, RowParameter{Name: "__ef_transport_E", Kind: "raises"}, RowParameter{Name: "__ef_transport_R", Kind: "uses"})
			if policy.PropagateRequirements {
				copy.Services = append(append([]string{}, f.Services...), "__ef_transport_R")
			}
		}
		return jsRowFunctionSignature(&copy, declarations)
	}
	copyDeclarations := func() map[string]Declaration {
		result := map[string]Declaration{}
		for name, d := range declarations {
			result[name] = d
		}
		return result
	}
	resultDeclarations := declarations
	parameterDeclarations := make([]map[string]Declaration, len(f.Params))
	variables := []string{}
	if len(f.RowParameters) > 0 {
		resultDeclarations = copyDeclarations()
		for i := range f.Params {
			parameterDeclarations[i] = copyDeclarations()
		}
		for _, row := range f.RowParameters {
			terms := []string{}
			for i, p := range f.Params {
				if p.sourceType == nil {
					continue
				}
				labels := p.sourceType.Failures
				if row.Kind == "uses" {
					labels = p.sourceType.Services
				}
				if !slices.Contains(labels, row.Name) {
					continue
				}
				variable := "__ef_row_" + strconv.Itoa(i) + "_" + row.Name
				constraint := variable
				if row.Kind == "raises" {
					constraint += " extends { readonly _tag: string }"
				}
				variables = append(variables, constraint)
				parameterDeclarations[i][row.Name] = Declaration{Kind: "row:" + row.Kind, Name: variable}
				fixed := []string{}
				for _, label := range labels {
					if label == row.Name {
						continue
					}
					if row.Kind == "uses" {
						fixed = append(fixed, label+"Requirement")
					} else if len(declarations[label].Fields) > 0 {
						fixed = append(fixed, label+"Error")
					} else {
						fixed = append(fixed, "{ readonly _tag: "+quoted(label)+" }")
					}
				}
				term := variable
				if len(fixed) > 0 {
					term = "Exclude<" + variable + ", " + strings.Join(fixed, " | ") + ">"
				}
				terms = append(terms, term)
			}
			resultDeclarations[row.Name] = Declaration{Kind: "row:" + row.Kind, Name: strings.Join(terms, " | ")}
		}
		for i := range parameterDeclarations {
			for _, row := range f.RowParameters {
				if _, exists := parameterDeclarations[i][row.Name]; !exists {
					parameterDeclarations[i][row.Name] = resultDeclarations[row.Name]
				}
			}
		}
	}
	params := []string{}
	nestedDeclarations := resultDeclarations
	if len(f.RowParameters) > 0 {
		nestedDeclarations = copyDeclarations()
		for _, row := range f.RowParameters {
			marker := resultDeclarations[row.Name]
			marker.Name = "NoInfer<" + marker.Name + ">"
			nestedDeclarations[row.Name] = marker
		}
	}
	for i, p := range f.Params {
		context := declarations
		if len(f.RowParameters) > 0 {
			context = parameterDeclarations[i]
		}
		params = append(params, "arg_"+p.Name+": "+jsSourceType(p.sourceType, p.Type, context, nestedDeclarations))
	}
	generic := ""
	if len(variables) > 0 {
		generic = "<" + strings.Join(variables, ", ") + ">"
	}
	return generic + "(" + strings.Join(params, ", ") + ") => " + jsContractFor(f, resultDeclarations)
}
