package compiler

import "strings"

// goLayout identifies the Go representation a value actually has. Go fixes a
// generic declaration's layout before instantiation and then substitutes type
// arguments into it, so a declared callable result T instantiated with void
// keeps a struct{} result while a concrete fn() -> void has none. id is the
// checked type of the declaration that chose the layout, and bindings maps
// its type variables to checked types of the use site. A value laid out by
// its own checked type has no bindings and renders exactly as canonicalGoType.
//
// Only generic data introduces declared layouts. A value read from a generic
// field or match payload keeps its declared layout through locals and pure
// calls, and is adapted once, where it reaches a destination that requires a
// different layout: a canonical value position, a generic constructor field,
// or a callable parameter.
type goLayout struct {
	id       TypeID
	bindings map[TypeID]TypeID
}

func canonicalLayout(id TypeID) goLayout { return goLayout{id: id} }

func (l goLayout) render(c *checker) string {
	return layoutGoType(c, l.id, l.bindings, map[TypeID]bool{})
}

func (l goLayout) child(id TypeID) goLayout { return goLayout{id: id, bindings: l.bindings} }

// resolve returns the checked node the layout describes. A bound type variable
// switches to the use-site frame, where its binding is laid out canonically.
func (l goLayout) resolve(c *checker) (*semanticTypeNode, goLayout) {
	node := c.node(l.id)
	if node != nil && node.Kind == "type-variable" {
		if bound, ok := l.bindings[l.id]; ok {
			return c.node(bound), canonicalLayout(bound)
		}
	}
	return node, l
}

// hasResult reports whether a pure callable result position keeps a Go
// result. Only a concrete void result is erased; a type variable instantiated
// with void keeps its struct{} carrier.
func (l goLayout) hasResult(c *checker) bool { return !canonicalVoidType(c, l.id) }

// templateBindings maps a template's type parameters to the arguments of one
// checked application of it.
func (g *goEmitter) templateBindings(owner *DataDeclaration, application TypeID) map[TypeID]TypeID {
	node := g.program.semantic.node(application)
	if owner == nil || node == nil || node.Kind != "application" {
		return nil
	}
	bindings := map[TypeID]TypeID{}
	for index, parameter := range owner.Parameters {
		if index < len(node.Args) {
			bindings[parameter.typeID] = node.Args[index]
		}
	}
	return bindings
}

// templateField finds a declared field of a record template or of the
// selected enum template variant.
func templateField(owner *DataDeclaration, variant, name string) (Field, bool) {
	fields := owner.Fields
	if owner.Kind == "enum" {
		fields = nil
		for _, candidate := range owner.Variants {
			if candidate.Name == variant {
				fields = candidate.Fields
				break
			}
		}
	}
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// templateFieldLayout returns the declared Go layout of a field of generic
// data whose checked type is the application data.
func (g *goEmitter) templateFieldLayout(data TypeID, variant, name string) (goLayout, bool) {
	c := g.program.semantic
	if c == nil {
		return goLayout{}, false
	}
	node := c.node(data)
	if node == nil || node.Kind != "application" {
		return goLayout{}, false
	}
	owner := c.templates[node.Declaration]
	if owner == nil {
		return goLayout{}, false
	}
	field, ok := templateField(owner, variant, name)
	if !ok {
		return goLayout{}, false
	}
	return goLayout{id: field.typeID, bindings: g.templateBindings(owner, data)}, true
}

// adaptGoLayout converts a value from the Go layout it was emitted with to the
// layout its destination requires. The checker has already proved the source
// types compatible, so the layouts differ only inside callable signatures,
// where a void result is a struct{} carrier on one side and absent on the
// other. A callable is captured once, before its wrapper is built, and invoked
// only when the wrapper is called. Parameters convert from the destination
// layout to the original one and results the other way, recursively through
// nested callables and recipe results. Equal layouts are returned unchanged.
func (g *goEmitter) adaptGoLayout(expression string, from, to goLayout, out *strings.Builder) string {
	c := g.program.semantic
	if from.id == to.id && len(from.bindings) == 0 && len(to.bindings) == 0 {
		return expression
	}
	if c == nil || from.id == invalidTypeID || to.id == invalidTypeID || from.render(c) == to.render(c) {
		return expression
	}
	fromNode, from := from.resolve(c)
	toNode, to := to.resolve(c)
	if fromNode == nil || toNode == nil || fromNode.Kind != toNode.Kind || fromNode.Mode != toNode.Mode || len(fromNode.Args) != len(toNode.Args) {
		return expression
	}
	switch toNode.Kind {
	case "recipe", "providerRecipe":
		return g.adaptRecipe(expression, from.child(fromNode.Result), to.child(toNode.Result), out)
	case "callable":
	default:
		return expression
	}
	captured := g.temp()
	out.WriteString(captured + " := " + expression + "\n")
	var body strings.Builder
	parameters := make([]string, len(toNode.Args))
	arguments := make([]string, len(toNode.Args))
	for index := range toNode.Args {
		parameter := to.child(toNode.Args[index])
		name := g.temp()
		parameters[index] = name + " " + parameter.render(c)
		arguments[index] = g.adaptGoLayout(name, parameter, from.child(fromNode.Args[index]), &body)
	}
	call := captured + "(" + strings.Join(arguments, ", ") + ")"
	fromResult, toResult := from.child(fromNode.Result), to.child(toNode.Result)
	signature := "func(" + strings.Join(parameters, ", ") + ")"
	switch {
	case toNode.Mode == "effect":
		recipe := g.adaptRecipe(call, fromResult, toResult, &body)
		signature += " efEffect[" + toResult.render(c) + "]"
		body.WriteString("return " + recipe + "\n")
	case !toResult.hasResult(c):
		body.WriteString(call + "\n")
	case !fromResult.hasResult(c):
		signature += " " + toResult.render(c)
		body.WriteString(call + "\nreturn struct{}{}\n")
	default:
		result := g.adaptGoLayout(call, fromResult, toResult, &body)
		signature += " " + toResult.render(c)
		body.WriteString("return " + result + "\n")
	}
	return signature + " {\n" + body.String() + "}"
}

// adaptRecipe converts a recipe's success value when the recipe runs. The
// original recipe is captured once and executed only by the adapted recipe;
// failures, defects and interruption propagate with their complete cause.
func (g *goEmitter) adaptRecipe(expression string, from, to goLayout, out *strings.Builder) string {
	c := g.program.semantic
	success := to.render(c)
	if from.render(c) == success {
		return expression
	}
	captured, exit := g.temp(), g.temp()
	out.WriteString(captured + " := " + expression + "\n")
	var body strings.Builder
	value := g.adaptGoLayout(exit+".Value", from, to, &body)
	return "func(ctx efContext) efExit[" + success + "] {\n" + exit + " := " + captured + "(ctx)\nif " + exit + ".IsFailure() {\nreturn er.Propagate[" + success + "](" + exit + ")\n}\n" + body.String() + "return efExit[" + success + "]{Value: " + value + "}\n}"
}
