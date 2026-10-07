package compiler

import (
	"fmt"
	"slices"
	"strings"
)

// TemplateParameter is a declaration-owned first-order variable. Callable
// constraints describe shallow mode/argument/result shape; actual full callable
// contracts, including both rows, remain canonical application arguments.
type TemplateParameter struct {
	Name       string      `json:"name"`
	Kind       string      `json:"kind"`
	Identity   string      `json:"identity"`
	Span       Span        `json:"span"`
	Constraint *sourceType `json:"-"`
	typeID     TypeID
	shapeID    TypeID
}

type TemplateParameterView struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Identity  string   `json:"identity"`
	Variable  TypeRef  `json:"variable"`
	Shape     *TypeRef `json:"shape,omitempty"`
	RowPolicy string   `json:"rowPolicy,omitempty"`
	typeID    TypeID
	shapeID   TypeID
}

func (c *checker) templateContext(parameters []TemplateParameter, declaration string) map[string]TemplateParameter {
	context := map[string]TemplateParameter{}
	if c.variableOwners == nil {
		c.variableOwners = map[string]TemplateParameter{}
	}
	for i := range parameters {
		p := &parameters[i]
		p.Identity = "type-parameter:" + declaration + ":" + p.Kind + ":" + p.Name
		p.typeID = c.internSemanticNode("type-variable", p.Name, p.Identity, p.Kind, nil, invalidTypeID, emptyRowID, emptyRowID)
		if _, duplicate := context[p.Name]; duplicate {
			c.diagnostic("EF127", "duplicate template parameter "+p.Name, p.Span)
		}
		context[p.Name] = *p
		c.variableOwners[p.Identity] = *p
	}
	return context
}

func (c *checker) checkTemplates() {
	c.templates = map[string]*Record{}
	declarations := append(append([]*DataDeclaration{}, c.program.Records...), c.program.Enums...)
	for _, r := range declarations {
		if len(r.Parameters) > 0 {
			r.Module, r.SourceID = currentModuleIdentity, "source:user"
			r.Identity = c.declarationIdentity("template", "module", r.Name)
			r.EmissionName = r.Name
		}
	}
	declarations = append(declarations, c.program.BundledTemplates...)
	for _, r := range declarations {
		if len(r.Parameters) == 0 {
			continue
		}
		if r.Module == "" || r.Identity == "" || len(r.Parameters) == 0 || len(r.Parameters) > 8 {
			c.diagnostic("EF127", "invalid distributed template declaration", r.Span)
			continue
		}
		context := c.templateContext(r.Parameters, r.Identity)
		fields := map[string]bool{}
		for _, field := range r.Fields {
			if fields[field.Name] {
				c.diagnostic("EF127", "duplicate template field "+field.Name, field.Span)
			}
			fields[field.Name] = true
			if _, known := context[field.Type]; r.SourceID != "source:user" && (!known || field.sourceType != nil) {
				c.diagnostic("EF127", "template fields require a declared first-order parameter", field.Span)
			}
		}
		for _, p := range r.Parameters {
			if p.Kind == "type" {
				continue
			}
			t := p.Constraint
			if p.Kind != "callable" || t == nil || t.Application != "" || t.ResultType != nil || len(t.Failures) > 0 || len(t.Services) > 0 {
				c.diagnostic("EF127", "unsupported callable shape constraint", p.Span)
				continue
			}
			for _, name := range append(append([]string{}, t.Parameters...), t.Result) {
				variable, known := context[name]
				if !known || variable.Kind != "type" {
					c.diagnostic("EF127", "callable shape constraints require direct declared data parameters", p.Span)
				}
			}
		}
		for i := range r.Parameters {
			p := &r.Parameters[i]
			if p.Kind != "callable" || p.Constraint == nil {
				continue
			}
			args := []TypeID{}
			for _, name := range p.Constraint.Parameters {
				args = append(args, context[name].typeID)
			}
			mode := "pure"
			if p.Constraint.Effect {
				mode = "effect"
			}
			p.shapeID = c.internSemanticNode("callable-shape", "", p.Identity, mode, args, context[p.Constraint.Result].typeID, emptyRowID, emptyRowID)
			c.variableOwners[p.Identity] = *p
		}
		c.templates[r.Identity] = r
		r.owner = c
		c.result.Declarations = append(c.result.Declarations, c.templateDeclaration(r))
	}
}

func (c *checker) templateDeclaration(r *Record) Declaration {
	d := Declaration{Kind: "template", DataKind: r.Kind, Name: r.Name, Identity: r.Identity, Source: r.SourceID, Span: r.Span, Fields: append([]Field{}, r.Fields...), Variants: append([]Variant{}, r.Variants...), TemplateParameters: []TemplateParameterView{}}
	for _, p := range r.Parameters {
		view := TemplateParameterView{Name: p.Name, Kind: p.Kind, Identity: p.Identity, Variable: c.ref(p.typeID), typeID: p.typeID, shapeID: p.shapeID}
		if p.shapeID != invalidTypeID {
			shape := c.ref(p.shapeID)
			view.Shape = &shape
			view.RowPolicy = "preserve-actual"
		}
		d.TemplateParameters = append(d.TemplateParameters, view)
	}
	for i := range d.Fields {
		index := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == d.Fields[i].Type })
		if index >= 0 {
			d.Fields[i].typeID = r.Parameters[index].typeID
			d.Fields[i].TypeRef = c.ref(d.Fields[i].typeID)
		}
	}
	return d
}

// Layouts resolve after all local nominal owners are registered. Each field
// occurrence keeps its checked ID; nested applications use the same arena.
func (c *checker) resolveDataTemplateLayouts() {
	declarations := append(append([]*DataDeclaration{}, c.program.Records...), c.program.Enums...)
	declarations = append(declarations, c.program.BundledTemplates...)
	for _, declaration := range declarations {
		if len(declaration.Parameters) == 0 {
			continue
		}
		previous := c.typeContext
		previousModule := c.functionModule
		c.functionModule = declaration.Module
		c.typeContext = map[string]TemplateParameter{}
		for _, parameter := range declaration.Parameters {
			c.typeContext[parameter.Name] = parameter
		}
		resolve := func(fields []Field, owner string, reserveTag bool) {
			for i := range fields {
				id := c.canonicalRef(typeRef(fields[i].Type))
				if node := c.node(id); node != nil && node.Kind == "opaque" && !c.directTemplateDataArgument(id) {
					c.diagnostic("EF127", "unsupported generic field layout", fields[i].Span)
				}
				fields[i].typeID, fields[i].TypeRef = id, c.ref(id)
				c.bindSourceSyntax(fields[i].sourceType, id)
			}
			// Generic fields use the same type, duplicate-name, and discriminator
			// checks as ordinary declarations while the owner's module and type
			// parameters are still in scope.
			c.validateFields(fields, owner, reserveTag)
		}
		resolve(declaration.Fields, declaration.Name, declaration.Kind == "enum")
		for i := range declaration.Variants {
			resolve(declaration.Variants[i].Fields, declaration.Name+"."+declaration.Variants[i].Name, true)
		}
		c.typeContext = previous
		c.functionModule = previousModule
		for i := range c.result.Declarations {
			if c.result.Declarations[i].Identity == declaration.Identity {
				c.result.Declarations[i] = c.templateDeclaration(declaration)
			}
		}
	}
	c.validateTemplateLayoutCycles(declarations)
}

func (c *checker) validateTemplateLayoutCycles(declarations []*DataDeclaration) {
	finished := map[string]bool{}
	active := map[string]bool{}
	visits := 0
	nodes := map[TypeID]bool{}
	budgetRefused := false
	workSpan := Span{}
	var owner func(*DataDeclaration, int) bool
	var node func(TypeID, int) bool
	node = func(id TypeID, depth int) bool {
		// Charge every retained edge before deduplication. Shared canonical nodes
		// are traversed once, but thousands of repeated payload fields still cost.
		visits++
		if depth > 64 || visits > 4096 {
			if visits > 4096 && !budgetRefused {
				budgetRefused = true
				c.diagnostic("EF127", "generic data layout exceeds 4096 work budget", workSpan)
			}
			return false
		}
		if nodes[id] {
			return true
		}
		n := c.node(id)
		if n == nil {
			return false
		}
		for _, argument := range n.Args {
			if !node(argument, depth+1) {
				return false
			}
		}
		if n.Result != invalidTypeID && !node(n.Result, depth+1) {
			return false
		}
		if n.Kind == "application" {
			if !owner(c.templates[n.Declaration], depth+1) {
				return false
			}
		} else if n.Kind == "record" || n.Kind == "enum" {
			declaration := c.records[n.Name]
			if n.Kind == "enum" {
				declaration = c.enums[n.Name]
			}
			if declaration == nil || n.Declaration != c.declarationQualifier(n.Kind, declaration.Name) || !owner(declaration, depth+1) {
				return false
			}
		}
		nodes[id] = true
		return true
	}
	owner = func(declaration *DataDeclaration, depth int) bool {
		if declaration == nil {
			return false
		}
		identity := declaration.Identity
		if identity == "" {
			identity = c.declarationQualifier(declaration.Kind, declaration.Name)
		}
		if active[identity] {
			return false
		}
		if finished[identity] {
			return true
		}
		active[identity] = true
		defer delete(active, identity)
		fields := func(fields []Field) bool {
			for _, field := range fields {
				workSpan = field.Span
				id := field.typeID
				if id == invalidTypeID {
					for _, parameter := range declaration.Parameters {
						if parameter.Name == field.Type {
							id = parameter.typeID
							break
						}
					}
				}
				if !node(id, depth+1) {
					return false
				}
			}
			return true
		}
		if !fields(declaration.Fields) {
			return false
		}
		for _, variant := range declaration.Variants {
			if !fields(variant.Fields) {
				return false
			}
		}
		finished[identity] = true
		return true
	}
	for _, declaration := range declarations {
		if len(declaration.Parameters) > 0 && !owner(declaration, 0) {
			c.diagnostic("EF127", "recursive or excessive generic data layout", declaration.Span)
		}
	}
}

func (c *checker) templateByName(name string) *Record {
	if alias, member, qualified := strings.Cut(name, "."); qualified {
		return c.program.BundledTypeBindings[alias][member]
	}
	if c.functionModule == "" || c.functionModule == currentModuleIdentity {
		for _, r := range append(append([]*DataDeclaration{}, c.program.Records...), c.program.Enums...) {
			if len(r.Parameters) > 0 && r.Name == name {
				return r
			}
		}
	}
	for _, r := range c.program.BundledTemplates {
		if r.Module == c.functionModule && r.Name == name {
			return r
		}
	}
	return nil
}

func (c *checker) templateDataArgument(id TypeID) bool {
	memo := map[TypeID]bool{}
	var visit func(TypeID, int) bool
	visit = func(id TypeID, depth int) bool {
		if valid, known := memo[id]; known {
			return valid
		}
		if depth > 64 || len(memo) > 4096 {
			return false
		}
		memo[id] = false
		n := c.node(id)
		if n != nil && n.Kind == "application" {
			owner := c.templates[n.Declaration]
			if owner == nil || len(n.Args) != len(owner.Parameters) {
				return false
			}
			for i, parameter := range owner.Parameters {
				if parameter.Kind != "type" || !visit(n.Args[i], depth+1) {
					return false
				}
			}
			memo[id] = true
			return true
		}
		memo[id] = c.directTemplateDataArgument(id)
		return memo[id]
	}
	return visit(id, 0)
}

func (c *checker) directTemplateDataArgument(id TypeID) bool {
	n := c.node(id)
	if n == nil {
		return false
	}
	switch n.Kind {
	case "primitive":
		return n.Name != "invalid" && n.Name != "never"
	case "record", "enum":
		return true
	case "opaque":
		return n.Declaration == "" && (n.Name == "File" || n.Name == "Latch")
	case "type-variable":
		p, owned := c.variableOwners[n.Declaration]
		return owned && p.Kind == "type"
	}
	return false
}

func (c *checker) matchTemplateConstraint(r *Record, p TemplateParameter, actual TypeID, bindings []TypeID) error {
	n := c.node(actual)
	if n == nil {
		return fmt.Errorf("unknown template argument")
	}
	if p.Kind == "type" {
		if !c.templateDataArgument(actual) {
			return fmt.Errorf("unsupported template data layout")
		}
		return nil
	}
	t := p.Constraint
	if n.Kind == "type-variable" {
		owner, known := c.variableOwners[n.Declaration]
		if !known || owner.Kind != "callable" || owner.shapeID == invalidTypeID {
			return fmt.Errorf("incompatible callable shape")
		}
		n = c.node(owner.shapeID)
	}
	mode := "pure"
	if t != nil && t.Effect {
		mode = "effect"
	}
	if p.Kind != "callable" || t == nil || n == nil || (n.Kind != "callable" && n.Kind != "callable-shape") || n.Mode != mode || len(n.Args) != len(t.Parameters) {
		return fmt.Errorf("incompatible callable shape")
	}
	bind := func(name string, id TypeID) error {
		index := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == name })
		if index < 0 || r.Parameters[index].Kind != "type" || !c.templateDataArgument(id) {
			return fmt.Errorf("unsupported nested callable constraint")
		}
		if bindings[index] != invalidTypeID && bindings[index] != id {
			return fmt.Errorf("inconsistent template argument %s", name)
		}
		bindings[index] = id
		return nil
	}
	for i, name := range t.Parameters {
		if err := bind(name, n.Args[i]); err != nil {
			return err
		}
	}
	return bind(t.Result, n.Result)
}

func (c *checker) templateApplication(r *Record, args []TypeID) (TypeID, error) {
	if r == nil || len(args) != len(r.Parameters) {
		return invalidTypeID, fmt.Errorf("template application requires all declared arguments")
	}
	bindings := append([]TypeID{}, args...)
	for i, p := range r.Parameters {
		if err := c.matchTemplateConstraint(r, p, args[i], bindings); err != nil {
			return invalidTypeID, err
		}
	}
	if !slices.Equal(args, bindings) {
		return invalidTypeID, fmt.Errorf("template application contains unbound arguments")
	}
	return c.internSemanticNode("application", r.Name, r.Identity, "", args, invalidTypeID, emptyRowID, emptyRowID), nil
}

func (c *checker) sourceApplication(t *sourceType) TypeID {
	if !c.sourceTypeKnown(t) {
		return invalidTypeID
	}
	return c.sourceApplicationCanonical(t)
}

func (c *checker) sourceApplicationCanonical(t *sourceType) TypeID {
	r := c.templateByName(t.Application)
	if r == nil {
		return invalidTypeID
	}
	args := []TypeID{}
	for _, name := range t.ApplicationArguments {
		args = append(args, c.canonicalRef(typeRef(name)))
	}
	id, err := c.templateApplication(r, args)
	if err != nil {
		c.diagnostic("EF127", err.Error(), t.Span)
		return invalidTypeID
	}
	t.Template = r
	t.owner, t.applicationID = c, id
	return id
}

// Renderer bindings come from checked owners; syntax never supplies type identity.
func (c *checker) bindSourceSyntax(t *sourceType, id TypeID) {
	if t == nil {
		return
	}
	n := c.node(id)
	if n == nil {
		return
	}
	if t.Application != "" && n.Kind == "application" {
		t.owner, t.applicationID, t.Template = c, id, c.templates[n.Declaration]
		for i, child := range t.ApplicationArgumentTypes {
			if i < len(n.Args) {
				c.bindSourceSyntax(child, n.Args[i])
			}
		}
		return
	}
	if n.Kind == "callable" {
		for i, child := range t.ParameterTypes {
			if i < len(n.Args) {
				c.bindSourceSyntax(child, n.Args[i])
			}
		}
		c.bindSourceSyntax(t.ResultType, n.Result)
	}
}

func (c *checker) applicationFields(id TypeID) ([]Field, bool) {
	n := c.node(id)
	if n == nil || n.Kind != "application" {
		return nil, false
	}
	r := c.templates[n.Declaration]
	if r == nil || r.Kind == "enum" || len(n.Args) != len(r.Parameters) {
		return nil, false
	}
	return c.instantiateDataFields(r, r.Fields, n.Args)
}

// recontractApplication permits a direct source-factory result to satisfy an
// explicit record application when every changed callable argument is
// represented by a checked direct field and all field occurrences fit the
// requested application. It does not make complete applications assignable.
func (c *checker) recontractApplication(actual checkedExpression, expected TypeID, returnedCall *Expr) (checkedExpression, bool) {
	if actual.application == nil || returnedCall == nil || returnedCall.Kind != "call" || returnedCall.ResolvedFunction == nil {
		return checkedExpression{}, false
	}
	a, b := c.node(actual.valueID()), c.node(expected)
	if a == nil || b == nil || a.Kind != "application" || b.Kind != "application" || a.Declaration != b.Declaration || len(a.Args) != len(b.Args) {
		return checkedExpression{}, false
	}
	// Application identity stays invariant. The only explicit boundary adapter
	// is a direct call to a source factory whose own return expression constructs
	// this same nominal application. A forwarded parameter or a call through an
	// alias has complete field occurrences too, but no fresh-construction proof.
	factory := returnedCall.ResolvedFunction
	factoryResult := c.node(factory.returnID)
	if factoryResult == nil || factoryResult.Kind != "application" || factoryResult.Declaration != a.Declaration || factory.Body == nil || len(factory.Body.Statements) == 0 {
		return checkedExpression{}, false
	}
	callee := factory.Name
	if factory.Module != "" && factory.Module != currentModuleIdentity {
		callee = factory.Identity
	} else if returnedCall.Left != nil && returnedCall.Left.Kind == "member" && returnedCall.Left.Left != nil && returnedCall.Left.Left.Kind == "name" {
		callee = returnedCall.Left.Left.Name + "." + returnedCall.Left.Name
	}
	if actual.application.Callee != callee {
		return checkedExpression{}, false
	}
	last := factory.Body.Statements[len(factory.Body.Statements)-1]
	if last.Kind != "expr" || last.Value == nil || last.Value.Kind != "construct" || actual.application.Result.ID != c.typeNodeID(actual.valueID()) || actual.application.ProducedResult != nil {
		return checkedExpression{}, false
	}
	owner := c.templates[a.Declaration]
	if owner == nil || owner.Kind != "record" || len(owner.Parameters) != len(a.Args) {
		return checkedExpression{}, false
	}
	actualFields, actualOK := c.applicationFields(actual.valueID())
	expectedFields, expectedOK := c.applicationFields(expected)
	if !actualOK || !expectedOK || len(actualFields) != len(owner.Fields) || len(expectedFields) != len(owner.Fields) || len(actual.fields) != len(owner.Fields) {
		return checkedExpression{}, false
	}
	actualContracts := make(map[string]TypeID, len(actualFields))
	for _, field := range actualFields {
		actualContracts[field.Name] = field.typeID
	}
	expectedContracts := make(map[string]Field, len(expectedFields))
	for _, field := range expectedFields {
		expectedContracts[field.Name] = field
	}
	for i, parameter := range owner.Parameters {
		if a.Args[i] == b.Args[i] {
			continue
		}
		if parameter.Kind != "callable" {
			return checkedExpression{}, false
		}
		represented := false
		for _, field := range owner.Fields {
			if field.typeID != parameter.typeID {
				continue
			}
			represented = true
			value, present := actual.fields[field.Name]
			contract, known := expectedContracts[field.Name]
			if !present || !known || !c.assignable(value.valueID(), contract.typeID, 0) {
				return checkedExpression{}, false
			}
		}
		if !represented {
			return checkedExpression{}, false
		}
	}
	fields := make(map[string]checkedExpression, len(expectedFields))
	for _, expectedField := range expectedFields {
		value, present := actual.fields[expectedField.Name]
		actualContract, known := actualContracts[expectedField.Name]
		if !present || !known || !c.assignable(value.valueID(), actualContract, 0) || !c.assignable(value.valueID(), expectedField.typeID, 0) {
			return checkedExpression{}, false
		}
		value = value.clone()
		value.value = c.values.occurrence(expectedField.typeID, value.ownershipFacts(), value.captureFacts())
		fields[expectedField.Name] = value
	}
	recontracted := actual.clone()
	recontracted.value = c.values.occurrence(expected, actual.ownershipFacts(), actual.captureFacts())
	recontracted.fields = fields
	application := *actual.application
	produced := application.Result
	produced.Args = append([]TypeRef{}, produced.Args...)
	produced.ArgIDs = append([]string{}, produced.ArgIDs...)
	application.ProducedResult = &produced
	application.Result = c.ref(expected)
	recontracted.application = &application
	return recontracted, true
}

func (c *checker) applicationVariants(id TypeID) ([]Variant, bool) {
	n := c.node(id)
	if n == nil || n.Kind != "application" {
		return nil, false
	}
	owner := c.templates[n.Declaration]
	if owner == nil || owner.Kind != "enum" || len(owner.Parameters) != len(n.Args) {
		return nil, false
	}
	variants := append([]Variant{}, owner.Variants...)
	for i := range variants {
		fields, ok := c.instantiateDataFields(owner, variants[i].Fields, n.Args)
		if !ok {
			return nil, false
		}
		variants[i].Fields = fields
	}
	return variants, true
}

func (c *checker) instantiateDataFields(r *DataDeclaration, declared []Field, arguments []TypeID) ([]Field, bool) {
	fields := append([]Field{}, declared...)
	bindings := map[TypeID]TypeID{}
	for i, parameter := range r.Parameters {
		bindings[parameter.typeID] = arguments[i]
	}
	for i := range fields {
		id := fields[i].typeID
		if id == invalidTypeID {
			index := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == fields[i].Type })
			if index < 0 {
				return nil, false
			}
			id = r.Parameters[index].typeID
		}
		fields[i].typeID = c.substituteCanonical(id, bindings, nil)
		if fields[i].typeID == invalidTypeID {
			return nil, false
		}
		fields[i].TypeRef = c.ref(fields[i].typeID)
	}
	return fields, true
}

func (c *checker) templateConstruct(e *Expr, env localEnv, inEffect bool) (checkedExpression, bool) {
	if e.Kind == "call" {
		root := e.Left
		for root != nil && root.Kind == "member" {
			root = root.Left
		}
		if root != nil && root.Kind == "name" {
			if root.binding != nil {
				return checkedExpression{}, false
			}
		}
	}
	head := e.Left
	name := expressionName(head)
	r := c.templateByName(name)
	variant := ""
	if r == nil && head != nil && head.Kind == "member" {
		r = c.templateByName(expressionName(head.Left))
		if r != nil {
			variant, head = head.Name, head.Left
		}
	}
	if r == nil {
		return checkedExpression{}, false
	}
	if variant != "" && e.Left.constructorType != nil {
		c.diagnostic("EF127", "constructor arguments belong to the data declaration before its variant", e.Span)
		return c.checkedData("invalid"), true
	}
	result := c.checkedData("invalid")
	declared := r.Fields
	if r.Kind == "enum" {
		index := slices.IndexFunc(r.Variants, func(v Variant) bool { return v.Name == variant })
		if index < 0 {
			c.diagnostic("EF116", "generic constructor must name a declared closed variant", e.Span)
			return result, true
		}
		declared = r.Variants[index].Fields
	} else if variant != "" {
		c.diagnostic("EF116", "record construction cannot select an enum variant", e.Span)
		return result, true
	}
	bindings := make([]TypeID, len(r.Parameters))
	variables := map[TypeID]bool{}
	types := map[TypeID]TypeID{}
	for _, p := range r.Parameters {
		variables[p.typeID] = true
	}
	if head.constructorType != nil {
		id := c.canonicalRef(typeRef(head.constructorType.display()))
		n := c.node(id)
		if n == nil || n.Kind != "application" || n.Declaration != r.Identity {
			c.diagnostic("EF127", "invalid explicit constructor application", e.Span)
			return result, true
		}
		c.bindSourceSyntax(head.constructorType, id)
		for i, p := range r.Parameters {
			types[p.typeID] = n.Args[i]
		}
	}
	if e.Kind == "call" {
		if len(e.Fields) > 0 && len(e.Fields) != len(e.Args) {
			c.diagnostic("EF122", "constructor arguments cannot mix named and positional forms", e.Span)
			return result, true
		}
		if len(e.Fields) == 0 {
			if len(e.Args) != len(declared) {
				c.diagnostic("EF115", "generic constructor requires every payload field", e.Span)
				return result, true
			}
			for i, arg := range e.Args {
				e.Fields = append(e.Fields, FieldValue{Name: declared[i].Name, Value: arg, Span: arg.Span})
			}
		}
		e.Text = "data"
	}
	fields := map[string]checkedExpression{}
	for _, field := range e.Fields {
		index := slices.IndexFunc(declared, func(f Field) bool { return f.Name == field.Name })
		if index < 0 {
			c.diagnostic("EF127", "unknown template field "+field.Name, e.Span)
			continue
		}
		if _, duplicate := fields[field.Name]; duplicate {
			c.diagnostic("EF127", "duplicate template initializer "+field.Name, e.Span)
			continue
		}
		// Constructor fields initialize pure data values or latent recipes.
		// An Effect must be run in its own expression and then stored here.
		value := c.expr(field.Value, env, false)
		fields[field.Name] = value.clone()
		formal := declared[index].typeID
		if formal == invalidTypeID {
			if p := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == declared[index].Type }); p >= 0 {
				formal = r.Parameters[p].typeID
			}
		}
		if value.isEffect() || !c.unifyTemplateTypes(formal, value.valueID(), variables, types) {
			c.diagnostic("EF127", "template fields require initialized values", e.Span)
			continue
		}
	}
	for i, p := range r.Parameters {
		bindings[i] = types[p.typeID]
	}
	// Existing Codec constructors infer data slots from retained callable shape
	// evidence. Complete that shared constraint inference before requiring all
	// arguments, including phantom data slots with no such evidence.
	for i, p := range r.Parameters {
		if p.Kind == "callable" && bindings[i] != invalidTypeID {
			if err := c.matchTemplateConstraint(r, p, bindings[i], bindings); err != nil {
				c.diagnostic("EF127", err.Error(), e.Span)
			}
		}
	}
	for i, p := range r.Parameters {
		if bindings[i] == invalidTypeID {
			c.diagnostic("EF127", "type parameter "+p.Name+" requires a complete explicit constructor argument or payload evidence", e.Span)
			return result, true
		}
	}
	for i, p := range r.Parameters {
		if err := c.matchTemplateConstraint(r, p, bindings[i], bindings); err != nil {
			c.diagnostic("EF127", err.Error(), e.Span)
		}
	}
	id, err := c.templateApplication(r, bindings)
	if err != nil {
		c.diagnostic("EF127", err.Error(), e.Span)
		return result, true
	}
	// The initialized occurrence exposes its declared field contract while
	// retaining the supplied callee/capture evidence behind that boundary.
	instantiated, ok := c.instantiateDataFields(r, declared, bindings)
	if !ok {
		c.diagnostic("EF127", "constructor field substitution unavailable", e.Span)
		return result, true
	}
	for _, field := range instantiated {
		if value, exists := fields[field.Name]; exists {
			if !c.assignable(value.valueID(), field.typeID, 0) {
				c.diagnostic("EF127", "constructor field contract mismatch", e.Span)
				return result, true
			}
			value.value = c.values.occurrence(field.typeID, value.ownershipFacts(), value.captureFacts())
			fields[field.Name] = value
		}
	}
	owners, captures := []OwnershipFact{}, []OwnershipFact{}
	evaluation := c.evaluation(emptyRowID, emptyRowID)
	for _, field := range declared {
		value, ok := fields[field.Name]
		if !ok {
			c.diagnostic("EF127", "missing initialized template field "+field.Name, e.Span)
			return result, true
		}
		owners = append(owners, prependFacts(field.Name, value.ownershipFacts())...)
		captures = append(captures, prependFacts(field.Name, value.captureFacts())...)
		evaluation = c.unionEvaluationFacts(evaluation, value.executed)
	}
	result = c.checkedDataID(id, normalizeFacts(owners), normalizeFacts(captures))
	result.fields = fields
	if variant != "" {
		result.setOwnership(prependFacts(variant, result.ownershipFacts()))
		result.setCaptures(prependFacts(variant, result.captureFacts()))
		payload := c.checkedData(voidTypeName)
		payload.fields = fields
		result.fields = map[string]checkedExpression{variant: payload}
	}
	result.evaluation = evaluation
	e.ResolvedTemplate = r
	if head.Kind == "member" {
		c.observeModuleAlias(head.Left, head.Left)
	}
	c.observeReference(head, head.Span, lexicalTarget{kind: r.Kind, data: r})
	owner := lexicalTarget{data: r, variant: variant}
	if variant != "" {
		c.observeReference(e.Left, e.Left.Span, lexicalTarget{kind: "variant", data: r, variant: variant})
	}
	c.observeFieldLabels(e, e.Fields, declared, owner)
	return result, true
}

// The same finite canonical relation serves data construction and generic
// callback inference. Application owners and their arguments are invariant.
func (c *checker) unifyTemplateTypes(formal, actual TypeID, variables map[TypeID]bool, bindings map[TypeID]TypeID, inferRows ...bool) bool {
	type pair struct {
		formal, actual TypeID
		invariant      bool
	}
	seen := map[pair]bool{}
	visits := 0
	var unify func(TypeID, TypeID, int, bool) bool
	unify = func(formal, actual TypeID, depth int, invariant bool) bool {
		visits++
		if depth > 64 || visits > 4096 {
			return false
		}
		if variables[formal] {
			if prior, ok := bindings[formal]; ok {
				if n := c.node(formal); n != nil {
					if p := c.variableOwners[n.Declaration]; p.Kind == "callable" && !invariant {
						return c.assignable(actual, prior, 0)
					}
				}
				return prior == actual
			}
			if c.node(actual) == nil {
				return false
			}
			bindings[formal] = actual
			return true
		}
		if formal == actual {
			return true
		}
		key := pair{formal, actual, invariant}
		if seen[key] {
			return true
		}
		seen[key] = true
		a, b := c.node(formal), c.node(actual)
		if a == nil || b == nil || a.Kind != b.Kind || len(a.Args) != len(b.Args) {
			return false
		}
		if a.Kind == "application" {
			if a.Declaration != b.Declaration {
				return false
			}
		} else if a.Kind == "callable" {
			if !invariant && c.assignable(actual, formal, 0) {
				return true
			}
			if a.Mode != b.Mode || (invariant || !(len(inferRows) > 0 && inferRows[0])) && (a.FailureRow != b.FailureRow || a.ServiceRow != b.ServiceRow) {
				return false
			}
		} else {
			return false
		}
		for i := range a.Args {
			if !unify(a.Args[i], b.Args[i], depth+1, invariant || a.Kind == "application") {
				return false
			}
		}
		return a.Result == b.Result || unify(a.Result, b.Result, depth+1, invariant)
	}
	return unify(formal, actual, 0, false)
}

// substituteCanonical is shared by row-polymorphic calls and first-order
// template applications. It memoizes a bounded canonical DAG, never strings.
func (c *checker) substituteCanonical(root TypeID, types map[TypeID]TypeID, rows map[string][]string) TypeID {
	memo := map[TypeID]TypeID{}
	visiting := map[TypeID]bool{}
	var visit func(TypeID, int) TypeID
	visit = func(id TypeID, depth int) TypeID {
		if bound, ok := types[id]; ok {
			return bound
		}
		if result, ok := memo[id]; ok {
			return result
		}
		if depth > 64 || len(memo) > 4096 || visiting[id] {
			return invalidTypeID
		}
		n := c.node(id)
		if n == nil {
			return invalidTypeID
		}
		visiting[id] = true
		defer delete(visiting, id)
		if n.Kind != "callable" && n.Kind != "recipe" && n.Kind != "providerRecipe" && n.Kind != "application" {
			memo[id] = id
			return id
		}
		args := []TypeID{}
		for _, arg := range n.Args {
			bound := visit(arg, depth+1)
			if bound == invalidTypeID {
				return invalidTypeID
			}
			args = append(args, bound)
		}
		result := invalidTypeID
		if n.Result != invalidTypeID {
			result = visit(n.Result, depth+1)
			if result == invalidTypeID {
				return invalidTypeID
			}
		}
		failure, service := c.instantiateRow(n.FailureRow, rows), c.instantiateRow(n.ServiceRow, rows)
		var bound TypeID
		if n.Kind == "application" {
			var err error
			bound, err = c.templateApplication(c.templates[n.Declaration], args)
			if err != nil {
				return invalidTypeID
			}
		} else {
			bound = c.internContract(n.Kind, n.Mode, result, args, failure, service)
		}
		memo[id] = bound
		return bound
	}
	return visit(root, 0)
}

func (c *checker) inferTypeArguments(f *Function, arguments []checkedExpression, span Span) map[TypeID]TypeID {
	bindings := map[TypeID]TypeID{}
	variables := map[TypeID]bool{}
	for _, p := range f.TypeParameters {
		variables[p.typeID] = true
	}
	for i, p := range f.Params {
		if i < len(arguments) && !c.unifyTemplateTypes(p.typeID, arguments[i].valueID(), variables, bindings, true) {
			c.diagnostic("EF127", "incompatible first-order template argument", span)
		}
	}
	for _, p := range f.TypeParameters {
		if bound, found := bindings[p.typeID]; !found {
			c.diagnostic("EF127", "type parameter "+p.Name+" requires a direct checked callback argument", span)
		} else if !c.templateDataArgument(bound) {
			c.diagnostic("EF127", "unsupported template data layout", span)
			delete(bindings, p.typeID)
		}
	}
	return bindings
}
