package compiler

import "strings"

// goLayout identifies the Go representation a value actually has. Go fixes a
// generic declaration's layout before instantiation and then substitutes type
// arguments into it, so a declared callable result T instantiated with void
// keeps a struct{} result while a concrete fn() -> void has none. id is the
// checked type of the declaration that chose the layout, and bindings maps
// its type variables to the layouts of the use site's type arguments. Each
// binding keeps its own frame, so frames compose through nested applications:
// in Envelope<void>'s field Action<fn() -> T, T>, Action's F is bound to
// fn() -> T laid out in Envelope's frame, and renders func() struct{}. A value
// laid out by its own checked type has no bindings and renders exactly as
// canonicalGoType.
//
// Only generic data introduces declared layouts. A value read from a generic
// field or match payload keeps its declared layout through locals and pure
// calls, and is adapted once, where it reaches a destination that requires a
// different layout: a canonical value position, a generic constructor field,
// or a callable parameter.
type goLayout struct {
	id       TypeID
	bindings map[TypeID]goLayout
}

func canonicalLayout(id TypeID) goLayout { return goLayout{id: id} }

func (l goLayout) render(c *checker) string {
	return layoutGoType(c, l.id, l.bindings, map[TypeID]bool{})
}

func (l goLayout) child(id TypeID) goLayout { return goLayout{id: id, bindings: l.bindings} }

// resolve returns the checked node the layout describes. A bound type variable
// switches to its binding's frame, repeatedly when that binding is itself a
// type variable of an enclosing declaration.
func (l goLayout) resolve(c *checker) (*semanticTypeNode, goLayout) {
	for {
		node := c.node(l.id)
		if node == nil || node.Kind != "type-variable" {
			return node, l
		}
		bound, ok := l.bindings[l.id]
		if !ok {
			return node, l
		}
		l = bound
	}
}

// hasResult reports whether a pure callable result position keeps a Go
// result. Only a concrete void result is erased; a type variable instantiated
// with void keeps its struct{} carrier.
func (l goLayout) hasResult(c *checker) bool { return !canonicalVoidType(c, l.id) }

// templateBindings maps a template's type parameters to the layouts of the
// arguments of one application of it, each laid out in the application's own
// frame.
func templateBindings(owner *DataDeclaration, node *semanticTypeNode, application goLayout) map[TypeID]goLayout {
	if owner == nil || node == nil || node.Kind != "application" {
		return nil
	}
	bindings := map[TypeID]goLayout{}
	for index, parameter := range owner.Parameters {
		if index < len(node.Args) {
			bindings[parameter.typeID] = application.child(node.Args[index])
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
// data that itself has the layout data.
func (g *goEmitter) templateFieldLayout(data goLayout, variant, name string) (goLayout, bool) {
	c := g.program.semantic
	if c == nil {
		return goLayout{}, false
	}
	node, data := data.resolve(c)
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
	return goLayout{id: field.typeID, bindings: templateBindings(owner, node, data)}, true
}

// adaptGoLayout converts a value from the Go layout it was emitted with to the
// layout its destination requires. The checker has already proved the source
// types compatible, so the layouts differ only inside callable signatures,
// where a void result is a struct{} carrier on one side and absent on the
// other, or inside the type arguments of generic data holding such callables,
// which Go instantiates as distinct types. A callable is captured once, before
// its wrapper is built, and invoked only when the wrapper is called.
// Parameters convert from the destination layout to the original one and
// results the other way, recursively through nested callables and recipe
// results. Generic data is rebuilt as the same declaration under the
// destination's type arguments (adaptData). Equal layouts are returned
// unchanged.
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
	case "application":
		if fromNode.Declaration != toNode.Declaration {
			return expression
		}
		return g.adaptData(expression, fromNode, toNode, from, to, out)
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

// adaptData converts generic data between two layouts of one application.
// Nominal identity is preserved: a record is rebuilt as the same template
// under the destination's type arguments, and an enum value as its own
// variant of that template, with every field adapted from its declared
// layout under the source application to the one under the destination.
// The value is evaluated once, as the argument of the conversion, and its
// callable fields are captured by their adapters and invoked only through
// the converted value. The checker rejects recursive generic data, so the
// conversion of nested fields terminates.
func (g *goEmitter) adaptData(expression string, fromNode, toNode *semanticTypeNode, from, to goLayout, out *strings.Builder) string {
	c := g.program.semantic
	owner := c.templates[toNode.Declaration]
	if owner == nil {
		return expression
	}
	fromBindings, toBindings := templateBindings(owner, fromNode, from), templateBindings(owner, toNode, to)
	fields := func(declared []Field, selector string, body *strings.Builder) string {
		parts := make([]string, len(declared))
		for index, field := range declared {
			adapted := g.adaptGoLayout(selector+"."+goFieldName(field.Name), goLayout{id: field.typeID, bindings: fromBindings}, goLayout{id: field.typeID, bindings: toBindings}, body)
			parts[index] = goFieldName(field.Name) + ":" + adapted
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	value, target := g.temp(), to.render(c)
	var body strings.Builder
	if owner.Kind == "enum" {
		variant := g.temp()
		body.WriteString("switch " + variant + " := " + value + ".(type) {\n")
		for _, candidate := range owner.Variants {
			var arm strings.Builder
			payload := fields(candidate.Fields, variant, &arm)
			body.WriteString("case " + layoutVariantType(c, owner, candidate.Name, fromNode, from) + ":\n_ = " + variant + "\n" + arm.String())
			body.WriteString("return " + layoutVariantType(c, owner, candidate.Name, toNode, to) + payload + "\n")
		}
		body.WriteString("}\npanic(\"unreachable enum variant\")\n")
	} else {
		payload := fields(owner.Fields, value, &body)
		body.WriteString("return " + target + payload + "\n")
	}
	return "func(" + value + " " + from.render(c) + ") " + target + " {\n" + body.String() + "}(" + expression + ")"
}

// layoutVariantType renders the Go type of one variant of an enum template
// application with the given layout.
func layoutVariantType(c *checker, owner *DataDeclaration, variant string, node *semanticTypeNode, application goLayout) string {
	args := make([]string, len(node.Args))
	for index, argument := range node.Args {
		args[index] = application.child(argument).render(c)
	}
	return goVariantType("template_"+owner.EmissionName, variant) + "[" + strings.Join(args, ",") + "]"
}
