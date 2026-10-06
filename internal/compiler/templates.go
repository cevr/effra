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
	for _, r := range c.program.Records {
		if len(r.Parameters) > 0 {
			c.diagnostic("EF127", "user template declarations are unavailable; apply an explicit compiler-distributed template", r.Span)
		}
	}
	for _, r := range c.program.BundledTemplates {
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
			if _, known := context[field.Type]; !known || field.sourceType != nil {
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
		}
		c.templates[r.Identity] = r
		c.result.Declarations = append(c.result.Declarations, c.templateDeclaration(r))
	}
}

func (c *checker) templateDeclaration(r *Record) Declaration {
	d := Declaration{Kind: "template", Name: r.Name, Identity: r.Identity, Source: r.SourceID, Span: r.Span, Fields: append([]Field{}, r.Fields...), TemplateParameters: []TemplateParameterView{}}
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

func (c *checker) templateByName(name string) *Record {
	if alias, member, qualified := strings.Cut(name, "."); qualified {
		return c.program.BundledTypeBindings[alias][member]
	}
	for _, r := range c.program.BundledTemplates {
		if r.Module == c.functionModule && r.Name == name {
			return r
		}
	}
	return nil
}

func (c *checker) templateDataArgument(id TypeID) bool {
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
	mode := "pure"
	if t != nil && t.Effect {
		mode = "effect"
	}
	if p.Kind != "callable" || t == nil || n.Kind != "callable" || n.Mode != mode || len(n.Args) != len(t.Parameters) {
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
	if r == nil || len(n.Args) != len(r.Parameters) {
		return nil, false
	}
	fields := append([]Field{}, r.Fields...)
	for i := range fields {
		index := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == fields[i].Type })
		if index < 0 {
			return nil, false
		}
		fields[i].typeID = n.Args[index]
		fields[i].TypeRef = c.ref(n.Args[index])
	}
	return fields, true
}

func (c *checker) templateConstruct(e *Expr, env map[string]checkedExpression, inEffect bool) (checkedExpression, bool) {
	name := ""
	if e.Left.Kind == "name" {
		name = e.Left.Name
	} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
		name = e.Left.Left.Name + "." + e.Left.Name
	}
	r := c.templateByName(name)
	if r == nil {
		return checkedExpression{}, false
	}
	result := c.checkedData("invalid")
	if len(e.Fields) != len(r.Fields) || len(e.Args) != 0 {
		c.diagnostic("EF127", "template construction requires every named field", e.Span)
		return result, true
	}
	bindings := make([]TypeID, len(r.Parameters))
	fields := map[string]checkedExpression{}
	for _, field := range e.Fields {
		declared := slices.IndexFunc(r.Fields, func(f Field) bool { return f.Name == field.Name })
		if declared < 0 {
			c.diagnostic("EF127", "unknown template field "+field.Name, e.Span)
			continue
		}
		if _, duplicate := fields[field.Name]; duplicate {
			c.diagnostic("EF127", "duplicate template initializer "+field.Name, e.Span)
			continue
		}
		value := c.expr(field.Value, env, inEffect)
		fields[field.Name] = value.clone()
		index := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == r.Fields[declared].Type })
		if index < 0 || value.isEffect() {
			c.diagnostic("EF127", "template fields require initialized values", e.Span)
			continue
		}
		bindings[index] = value.valueID()
		if err := c.matchTemplateConstraint(r, r.Parameters[index], value.valueID(), bindings); err != nil {
			c.diagnostic("EF127", err.Error(), e.Span)
		}
	}
	id, err := c.templateApplication(r, bindings)
	if err != nil {
		c.diagnostic("EF127", err.Error(), e.Span)
		return result, true
	}
	owners, captures := []OwnershipFact{}, []OwnershipFact{}
	evaluation := c.evaluation(emptyRowID, emptyRowID)
	for _, field := range r.Fields {
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
	result.evaluation = evaluation
	e.ResolvedTemplate = r
	return result, true
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
	var unify func(TypeID, TypeID, int) bool
	unify = func(formal, actual TypeID, depth int) bool {
		if depth > 32 {
			return false
		}
		if variables[formal] {
			if !c.templateDataArgument(actual) {
				return false
			}
			if prior, ok := bindings[formal]; ok {
				return prior == actual
			}
			bindings[formal] = actual
			return true
		}
		a, b := c.node(formal), c.node(actual)
		if a == nil || b == nil {
			return false
		}
		if a.Kind == "callable" && b.Kind == "callable" && a.Mode == b.Mode && len(a.Args) == len(b.Args) {
			for i := range a.Args {
				if !unify(a.Args[i], b.Args[i], depth+1) {
					return false
				}
			}
			return unify(a.Result, b.Result, depth+1)
		}
		return formal == actual
	}
	for i, p := range f.Params {
		if i < len(arguments) && !unify(p.typeID, arguments[i].valueID(), 0) {
			c.diagnostic("EF127", "incompatible first-order template argument", span)
		}
	}
	for _, p := range f.TypeParameters {
		if _, bound := bindings[p.typeID]; !bound {
			c.diagnostic("EF127", "type parameter "+p.Name+" requires a direct checked callback argument", span)
		}
	}
	return bindings
}
