package compiler

import (
	"strconv"
	"strings"
)

// A presentation expands a shared type DAG into text, so it has its own
// budget beside the projection tables it reads. Elision is explicit.
const (
	maxPresentationBytes = 4096
	maxPresentationDepth = 32
	presentationElision  = "…"
)

// presenter renders the one plaintext view of a selection shared by CLI, MCP
// and LSP hover. It reads only the response's own projected type and row
// tables, so no adapter can render a fact the response does not carry.
type presenter struct {
	types     map[string]TypeNode
	rows      map[string]RowNode
	out       strings.Builder
	truncated bool
}

func presentSelection(selected *SelectedType, projection TypeProjection) string {
	p := &presenter{types: make(map[string]TypeNode, len(projection.Types)), rows: make(map[string]RowNode, len(projection.Rows))}
	for _, node := range projection.Types {
		p.types[node.ID] = node
	}
	for _, row := range projection.Rows {
		p.rows[row.ID] = row
	}
	p.selection(selected)
	if p.truncated {
		p.out.WriteString(presentationElision)
	}
	return p.out.String()
}

func (p *presenter) write(text string) {
	if p.truncated {
		return
	}
	if p.out.Len()+len(text) > maxPresentationBytes-len(presentationElision) {
		p.truncated = true
		return
	}
	p.out.WriteString(text)
}

func (p *presenter) selection(s *SelectedType) {
	t := s.Target
	if t == nil {
		switch {
		case s.Definition != "":
			p.typ(s.Definition, 0)
		case s.Expression != nil:
			p.value(s.Expression.Type)
		}
		return
	}
	switch t.Kind {
	case "function", "operation", "method":
		p.callable(t)
	case "parameter":
		p.write("parameter ")
		if t.Parameter != nil {
			p.parameter(*t.Parameter)
		} else {
			p.write(t.Name + ": ")
			if t.Type != nil {
				p.typ(t.Type.ID, 0)
			}
		}
	case "let", "configuration", "pattern":
		p.write(t.Kind + " " + t.Name + ": ")
		if s.Expression != nil {
			p.value(s.Expression.Type)
		} else if t.Type != nil {
			// An argument label selects the declared parameter it binds.
			p.typ(t.Type.ID, 0)
		}
	case "record", "enum":
		p.write(t.Kind + " " + t.Name)
		p.data(s.Declaration, t.Kind)
	case "variant":
		p.write("variant " + t.Owner + "." + t.Name)
		if variant := declaredVariant(s.Declaration, t.Name); variant != nil && len(variant.Fields) > 0 {
			p.fields(variant.Fields)
		}
	case "field":
		p.write("field " + t.Owner + "." + t.Name + ": ")
		if s.Expression != nil {
			// A checked field access shows its occurrence, which may be an
			// instantiated template field rather than the declared variable.
			p.value(s.Expression.Type)
		} else if t.Type != nil {
			p.typ(t.Type.ID, 0)
		}
	case "error":
		p.write("error " + t.Name)
		if s.Declaration != nil && len(s.Declaration.Fields) > 0 {
			p.fields(s.Declaration.Fields)
		}
	case "provider":
		p.write("impl " + t.Name + " for " + t.Owner)
		if t.Type != nil {
			if node, ok := p.types[t.Type.ID]; ok && node.ServiceRow != "" {
				p.write(" uses ")
				p.row(node.ServiceRow)
			}
		}
	case "service":
		p.write("service " + t.Name)
	case "module":
		p.write("import " + t.Name + " " + strconv.Quote(t.Module))
	case "hostModule":
		p.write("import go " + t.Name + " " + strconv.Quote(t.Module))
	case "layer":
		p.write("layer " + t.Name)
	case "codec":
		p.write("derive " + t.Name)
	}
}

func declaredVariant(declaration *Declaration, name string) *Variant {
	if declaration == nil {
		return nil
	}
	for i := range declaration.Variants {
		if declaration.Variants[i].Name == name {
			return &declaration.Variants[i]
		}
	}
	return nil
}

func (p *presenter) data(d *Declaration, kind string) {
	if d == nil {
		return
	}
	p.templateParameters(d.TemplateParameters)
	if kind != "enum" {
		p.fields(d.Fields)
		return
	}
	p.write(" {")
	for i, variant := range d.Variants {
		if i > 0 {
			p.write(",")
		}
		p.write(" " + variant.Name)
		if len(variant.Fields) > 0 {
			p.fields(variant.Fields)
		}
	}
	if len(d.Variants) > 0 {
		p.write(" ")
	}
	p.write("}")
}

func (p *presenter) templateParameters(parameters []TemplateParameterView) {
	if len(parameters) == 0 {
		return
	}
	p.write("<")
	for i, parameter := range parameters {
		if i > 0 {
			p.write(", ")
		}
		p.write(parameter.Name + ": " + parameter.Kind)
		if parameter.Shape != nil {
			p.write(" ")
			p.typ(parameter.Shape.ID, 0)
		}
	}
	p.write(">")
}

func (p *presenter) fields(fields []Field) {
	p.write(" {")
	for i, field := range fields {
		if i > 0 {
			p.write(",")
		}
		p.write(" " + field.Name + ": ")
		p.typ(field.TypeRef.ID, 0)
	}
	if len(fields) > 0 {
		p.write(" ")
	}
	p.write("}")
}

func (p *presenter) callable(t *DeclarationTarget) {
	c := t.Callable
	if c.Kind == "effect" {
		p.write("effect ")
	}
	p.write("fn ")
	if t.Owner != "" {
		p.write(t.Owner + ".")
	}
	p.write(t.Name)
	if len(c.TypeParameters)+len(c.RowParameters) > 0 {
		p.write("<")
		for i, parameter := range c.TypeParameters {
			if i > 0 {
				p.write(", ")
			}
			p.write(parameter.Name + ": " + parameter.Kind)
		}
		for i, parameter := range c.RowParameters {
			if i > 0 || len(c.TypeParameters) > 0 {
				p.write(", ")
			}
			p.write(parameter.Name + ": " + parameter.Kind)
		}
		p.write(">")
	}
	p.write("(")
	for i, parameter := range c.Parameters {
		if i > 0 {
			p.write(", ")
		}
		p.parameter(parameter)
	}
	p.write(") -> ")
	p.result(c.Result.ID, 0)
	p.clause(" raises ", c.FailureRow)
	p.clause(" uses ", c.ServiceRow)
}

func (p *presenter) parameter(parameter Param) {
	if parameter.RequiredChoice {
		p.write("required ")
	}
	p.write(parameter.Name + ": ")
	p.typ(parameter.TypeRef.ID, 0)
	if parameter.DefaultValue != nil {
		p.write(" = ")
		p.constant(*parameter.DefaultValue)
	}
}

func (p *presenter) constant(value ConstantValue) {
	switch value.Kind {
	case "string":
		p.write(strconv.Quote(value.Value))
	case "i64", "bool":
		p.write(value.Value)
	default:
		p.truncated = true
	}
}

// value renders a checked value by its complete contract identity, which
// distinguishes callables, recipes and fibers from their success type.
func (p *presenter) value(v ValueType) {
	id := v.Contract.ID
	if id == "" {
		id = v.Type.ID
	}
	p.typ(id, 0)
}

// result parenthesizes a callable result, as source callable types do.
func (p *presenter) result(id string, depth int) {
	if node, ok := p.types[id]; ok && (node.Kind == "callable" || node.Kind == "callable-shape") {
		p.write("(")
		p.typ(id, depth+1)
		p.write(")")
		return
	}
	p.typ(id, depth+1)
}

func (p *presenter) clause(keyword, row string) {
	if row == "" {
		return
	}
	p.write(keyword)
	p.row(row)
}

func (p *presenter) row(id string) {
	row := p.rows[id]
	names := make(map[string]string, len(row.Parameters))
	for _, parameter := range row.Parameters {
		names[parameter.ID] = parameter.Name
	}
	p.write("{")
	for i, label := range row.Labels {
		if i > 0 {
			p.write(", ")
		}
		if name, ok := names[label]; ok {
			label = name
		}
		p.write(label)
	}
	p.write("}")
}

func (p *presenter) typ(id string, depth int) {
	// Text expands shared children; stop walking once the budget is spent.
	if p.truncated {
		return
	}
	node, ok := p.types[id]
	if !ok || depth > maxPresentationDepth {
		p.truncated = true
		return
	}
	switch node.Kind {
	case "callable", "callable-shape":
		if node.Mode == "effect" {
			p.write("effect ")
		}
		p.write("fn(")
		for i, arg := range node.Args {
			if i > 0 {
				p.write(", ")
			}
			p.typ(arg, depth+1)
		}
		p.write(") -> ")
		p.result(node.Result, depth)
		p.clause(" raises ", node.FailureRow)
		p.clause(" uses ", node.ServiceRow)
	case "recipe", "providerRecipe":
		// The diagnostic notation for a deferred effect: success, failure
		// row and requirement row, each always present.
		p.write("Effect<")
		p.typ(node.Result, depth+1)
		p.write(", ")
		p.row(node.FailureRow)
		p.write(", ")
		p.row(node.ServiceRow)
		p.write(">")
	case "fiber":
		p.write("Fiber<")
		p.args(node.Args, depth)
		p.write(", ")
		p.row(node.FailureRow)
		p.write(">")
	case "goResult":
		p.write("GoResult<")
		p.args(node.Args, depth)
		p.write(">")
	case "provider":
		p.write("Provider<" + node.Name + ">")
	case "application":
		p.write(node.Name + "<")
		p.args(node.Args, depth)
		p.write(">")
	case "never", "invalid":
		p.write(node.Kind)
	default:
		p.write(node.Name)
	}
}

func (p *presenter) args(args []string, depth int) {
	for i, arg := range args {
		if i > 0 {
			p.write(", ")
		}
		p.typ(arg, depth+1)
	}
}
