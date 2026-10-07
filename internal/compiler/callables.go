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
	owner                    *checker
	applicationID            TypeID
	Span                     Span
	Template                 *Record
	ApplicationArgumentTypes []*sourceType
	Application              string
	ApplicationArguments     []string
	Effect                   bool
	Parameters               []string
	ParameterTypes           []*sourceType
	Result                   string
	ResultType               *sourceType
	Failures                 []string
	Services                 []string
	// HostForm is a native pointer, slice or map spelling; HostArguments are
	// its element (and map key) types. hostID is the checked host node.
	HostForm          string
	HostArguments     []string
	HostArgumentTypes []*sourceType
	hostID            TypeID
}

func (t *sourceType) display() string {
	switch t.HostForm {
	case "pointer":
		return "*" + t.HostArguments[0]
	case "slice":
		return "[]" + t.HostArguments[0]
	case "map":
		return "map[" + t.HostArguments[0] + "]" + t.HostArguments[1]
	}
	if t.Application != "" {
		return t.Application + "<" + strings.Join(t.ApplicationArguments, ", ") + ">"
	}
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
	if t.HostForm != "" {
		return c.sourceHostType(t)
	}
	if t.Application != "" {
		return c.sourceApplication(t)
	}
	if !c.sourceTypeKnown(t) {
		return invalidTypeID
	}
	return c.sourceCallableCanonical(t)
}

func (c *checker) sourceCallableCanonical(t *sourceType) TypeID {
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
	return c.sourceTypeKnown(t)
}

// sourceTypeKnown is the source-admission counterpart to canonicalRef. It
// walks parsed callable and application arguments before interning their
// canonical nodes, so a successful generic application cannot hide an
// undeclared row, an effect row on a pure callable, or an invalid nested
// argument. The current type/module contexts remain the authority for names;
// templateApplication remains the authority for finite parameter bounds.
func (c *checker) sourceTypeKnown(t *sourceType) bool {
	return c.sourceTypeKnownIn(t, map[*sourceType]bool{})
}

func (c *checker) sourceTypeKnownIn(t *sourceType, visiting map[*sourceType]bool) bool {
	if t == nil || visiting[t] {
		return false
	}
	visiting[t] = true
	defer delete(visiting, t)
	if t.HostForm != "" {
		return c.sourceHostType(t) != invalidTypeID
	}
	if t.Application != "" {
		if len(t.ApplicationArguments) != len(t.ApplicationArgumentTypes) {
			return false
		}
		for i, argument := range t.ApplicationArgumentTypes {
			if argument != nil {
				if !c.sourceTypeKnownIn(argument, visiting) {
					return false
				}
			} else if !c.typeKnown(t.ApplicationArguments[i]) {
				return false
			}
		}
		return c.sourceApplicationCanonical(t) != invalidTypeID
	}
	if !t.Effect && (len(t.Failures) > 0 || len(t.Services) > 0) {
		return false
	}
	if t.ResultType != nil {
		if !c.sourceTypeKnownIn(t.ResultType, visiting) {
			return false
		}
	} else if !c.typeKnown(t.Result) {
		return false
	}
	if len(t.ParameterTypes) != len(t.Parameters) {
		return false
	}
	for i, name := range t.Parameters {
		if t.ParameterTypes[i] != nil {
			if !c.sourceTypeKnownIn(t.ParameterTypes[i], visiting) {
				return false
			}
		} else if !c.typeKnown(name) {
			return false
		}
	}
	return c.sourceRowsKnown(t.Failures, "raises") && c.sourceRowsKnown(t.Services, "uses")
}

func (c *checker) sourceRowsKnown(labels []string, kind string) bool {
	for _, name := range labels {
		if c.rowParameter(name, kind) {
			continue
		}
		switch kind {
		case "raises":
			if c.program == nil {
				return false
			}
			if _, ok := c.program.Errors[name]; !ok {
				return false
			}
		case "uses":
			known := c.services[name] != nil
			if c.program != nil {
				for _, service := range c.program.Services {
					known = known || service.Name == name
				}
			}
			if !known {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Row variables are qualified by their declaration, never matched by the
// source spelling shared by unrelated functions. The finite solver admits one
// variable per callback row and obtains its least bound from actual arguments.
func (c *checker) functionRows(f *Function) map[string]RowParameter {
	if c.rowDefinitions == nil {
		c.rowDefinitions = map[string]RowParameter{}
	}
	declared := map[string]bool{}
	for i := range f.RowParameters {
		p := &f.RowParameters[i]
		p.Declaration = f.Identity
		p.ID = "row-parameter:" + f.Identity + ":" + p.Kind + ":" + p.Name
		if declared[p.Name] {
			c.diagnostic("EF125", "duplicate row parameter "+p.Name, p.Span)
		}
		declared[p.Name] = true
		if f.Owner != "module" && f.Owner != "" {
			c.diagnostic("EF125", "row parameters are supported on ordinary module functions", p.Span)
		}
		c.rowDefinitions[p.ID] = *p
	}
	context := map[string]RowParameter{}
	for name, p := range functionRowScope(f) {
		context[name] = *p
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
	return c.substituteCanonical(id, nil, bindings)
}

func (c *checker) inferRows(f *Function, arguments []checkedExpression, span Span, typeArguments ...map[TypeID]TypeID) map[string][]string {
	types := map[TypeID]TypeID{}
	if len(typeArguments) > 0 {
		types = typeArguments[0]
	}
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
		if i < len(arguments) && (arguments[i].isEffect() || !c.assignable(arguments[i].valueID(), c.substituteCanonical(p.typeID, types, bindings), 0)) {
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
	if a.Kind == "application" && b.Kind == "application" && a.Declaration == b.Declaration && len(a.Args) == len(b.Args) {
		template := c.templates[a.Declaration]
		if template == nil || len(template.Parameters) != len(a.Args) {
			return false
		}
		// A complete generic application has one canonical identity. Callable
		// variance is checked where values initialize declared callable slots;
		// it does not widen the arguments that identify an application.
		return slices.Equal(a.Args, b.Args)
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
func (c *checker) callableCall(e *Expr, env localEnv, inEffect bool) (checkedExpression, bool) {
	local := false
	if e.Left.Kind == "name" {
		local = e.Left.binding != nil
	}
	if e.Left.Kind == "member" {
		if e.Left.Left.Kind == "name" {
			local = e.Left.Left.binding != nil
		} else {
			local = true
		}
	}
	if !local {
		return checkedExpression{}, false
	}
	previous := c.hostCallee
	c.hostCallee = e.Left
	callee := c.expr(e.Left, env, inEffect)
	c.hostCallee = previous
	if e.Left.Kind == "member" && e.Left.Text == "hostMethod" {
		return c.hostMethodCall(e, callee, env, inEffect), true
	}
	node := callee.node()
	if node == nil || node.Kind != "callable" {
		c.diagnostic("EF103", "local value is not callable", e.Span)
		return c.checkedData("invalid"), true
	}
	c.rejectArgumentLabels(e, "callable values")
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

func goSourceType(program *Program, t *sourceType, fallback string) string {
	result, _ := goSourceTypeMode(program, t, fallback)
	return result
}

// goSourceTypeMode renders a source type and reports whether it is the
// primitive void. Only a concrete void result is erased from a pure Go
// signature; void in any value position, including the result of an enclosing
// callable that returns a no-result callable, keeps a Go type.
func goSourceTypeMode(program *Program, t *sourceType, fallback string) (string, bool) {
	if t == nil {
		return goType(program, fallback), fallback == voidTypeName
	}
	if t.Application != "" {
		return canonicalGoType(t.owner, t.applicationID, map[TypeID]bool{}), false
	}
	if t.HostForm != "" {
		return canonicalGoType(t.owner, t.hostID, map[TypeID]bool{}), false
	}
	// Nested children are rendered from the canonical parsed syntax retained
	// on each occurrence in the source type graph by source rendering below.
	args := make([]string, len(t.Parameters))
	for i, name := range t.Parameters {
		args[i] = goSourceType(program, t.ParameterTypes[i], name)
	}
	result, voidResult := goSourceTypeMode(program, t.ResultType, t.Result)
	if t.Effect {
		return "func(" + strings.Join(args, ", ") + ") efEffect[" + result + "]", false
	}
	if voidResult {
		return "func(" + strings.Join(args, ", ") + ")", false
	}
	return "func(" + strings.Join(args, ", ") + ") " + result, false
}

func jsSourceType(program *Program, t *sourceType, fallback string, declarations ...map[string]Declaration) string {
	if t == nil {
		return jsValueType(program, fallback)
	}
	if t.HostForm != "" {
		// Native Go types never reach JS: Go imports refuse that target.
		return "never"
	}
	if t.Application != "" {
		if t.Template == nil {
			return "never"
		}
		args := []string{}
		for i, name := range t.ApplicationArguments {
			args = append(args, jsSourceType(program, t.ApplicationArgumentTypes[i], name, declarations...))
		}
		return "__ef_template_" + t.Template.EmissionName + "<" + strings.Join(args, ", ") + ">"
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
		args[i] = "arg" + strconv.Itoa(i) + ": " + jsSourceType(program, t.ParameterTypes[i], name, nested)
	}
	f := &Function{Return: t.Result, returnType: t.ResultType, Effect: t.Effect, Errors: t.Failures, Services: t.Services}
	return "(" + strings.Join(args, ", ") + ") => " + jsContractFor(program, f, declared, nested)
}

// Each actual callback gets its own TS inference variable. Reusing a single
// variable in several parameter positions makes TypeScript pick the first
// callback's row instead of the source solver's least union. The return view
// joins these witnesses and excludes the formal row's fixed labels.
func jsRowFunctionSignature(program *Program, f *Function, declarations map[string]Declaration) string {
	if len(f.TypeParameters) > 0 {
		declared := map[string]Declaration{}
		for name, d := range declarations {
			declared[name] = d
		}
		variables := []string{}
		for _, p := range f.TypeParameters {
			variables = append(variables, p.Name)
		}
		for _, p := range f.RowParameters {
			variables = append(variables, p.Name)
			declared[p.Name] = Declaration{Kind: "row:" + p.Kind, Name: p.Name}
		}
		params := []string{}
		for i, p := range f.Params {
			params = append(params, "arg"+strconv.Itoa(i)+": "+jsSourceType(program, p.sourceType, p.Type, declared))
		}
		return "<" + strings.Join(variables, ", ") + ">(" + strings.Join(params, ", ") + ") => " + jsContractFor(program, f, declared)
	}
	if len(f.CallbackPolicies) > 0 {
		copy := *f
		copy.Params = append([]Param{}, f.Params...)
		copy.CallbackPolicies = nil
		for _, policy := range f.CallbackPolicies {
			if policy.Kind != "typed-failure-response" {
				continue
			}
			callback := builtinCallbacks[f.Params[policy.Parameter].Type]
			copy.Params[policy.Parameter].sourceType = &sourceType{Effect: true, Parameters: []string{callback.Parameter}, ParameterTypes: []*sourceType{nil}, Result: callback.Result, Failures: []string{"__ef_transport_E"}, Services: []string{"__ef_transport_R"}}
			copy.RowParameters = append(copy.RowParameters, RowParameter{Name: "__ef_transport_E", Kind: "raises"}, RowParameter{Name: "__ef_transport_R", Kind: "uses"})
			if policy.PropagateRequirements {
				copy.Services = append(append([]string{}, f.Services...), "__ef_transport_R")
			}
		}
		return jsRowFunctionSignature(program, &copy, declarations)
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
		params = append(params, "arg_"+p.Name+": "+jsSourceType(program, p.sourceType, p.Type, context, nestedDeclarations))
	}
	generic := ""
	if len(variables) > 0 {
		generic = "<" + strings.Join(variables, ", ") + ">"
	}
	return generic + "(" + strings.Join(params, ", ") + ") => " + jsContractFor(program, f, resultDeclarations)
}
