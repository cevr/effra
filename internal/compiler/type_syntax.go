package compiler

import "slices"

// rowLabel is one name token: a row label, or a type annotation's head name
// or alias qualifier.
type rowLabel struct {
	Name string
	Span Span
}

// typeSyntax is one source occurrence of a type annotation. The parser keeps
// type spellings interned by display text for the checker, which loses where
// each occurrence's names are written. An occurrence retains those tokens
// only: like bindSourceSyntax, its names take their declarations from the
// checked type the annotation resolved to, never from the spelling.
type typeSyntax struct {
	// Form is "callable", "host", "recipe", or empty for a named, applied or
	// void type.
	Form            string
	Qualifier, Name rowLabel
	Extent          Span
	// Args are application arguments, callable parameters, or a host form's
	// key and element types, in source order.
	Args               []*typeSyntax
	Result             *typeSyntax
	Failures, Services []rowLabel
}

// maxTypeSyntaxDepth bounds the occurrence walk; the parser already refuses
// type nesting beyond 64.
const maxTypeSyntaxDepth = 64

// observeDeclarationSyntax records the declarations that original type
// annotations and declared row labels denote, after checking has retained
// each annotation's canonical type and each declared row. Only original
// top-level items are visited, so bundled and synthesized declarations never
// publish references into this snapshot.
func (c *checker) observeDeclarationSyntax() {
	facts := c.result.lexical
	if facts == nil || !facts.complete || c.program == nil {
		return
	}
	for _, item := range c.program.Items {
		switch {
		case item.Function != nil:
			c.observeSignatureSyntax(lexicalTarget{function: item.Function})
		case item.Service != nil:
			for _, f := range item.Service.Methods {
				c.observeSignatureSyntax(lexicalTarget{function: f, service: item.Service})
			}
		case item.Provider != nil:
			p := item.Provider
			for _, param := range p.Params {
				c.observeAnnotation(param.Annotation, param.typeID, lexicalTarget{}, nil)
			}
			declared := normalized(p.Services)
			for _, label := range p.ServiceLabels {
				// Captured construction services are fixed requirements; the
				// provider declares no row parameters.
				if service := c.services[label.Name]; service != nil && slices.Contains(declared, label.Name) {
					facts.addReference(label.Span, lexicalTarget{kind: "service", service: service})
				}
			}
			for _, f := range p.Methods {
				c.observeSignatureSyntax(lexicalTarget{function: f, provider: p})
			}
		case item.Error != nil:
			c.observeFieldSyntax(item.Error.Fields, nil)
		case item.Record != nil || item.Enum != nil:
			data := item.Record
			if data == nil {
				data = item.Enum
			}
			// A callable constraint's checked shape names the declaration's
			// own type parameters.
			for _, parameter := range data.Parameters {
				c.observeAnnotation(parameter.Annotation, parameter.shapeID, lexicalTarget{}, data)
			}
			c.observeFieldSyntax(data.Fields, data)
			for _, variant := range data.Variants {
				c.observeFieldSyntax(variant.Fields, data)
			}
		}
	}
}

// observeSignatureSyntax binds one declared signature. owner names the
// function with its service or provider, which also own its row parameters.
func (c *checker) observeSignatureSyntax(owner lexicalTarget) {
	f := owner.function
	if !f.signatureChecked {
		return
	}
	for _, p := range f.Params {
		c.observeAnnotation(p.Annotation, p.typeID, owner, nil)
	}
	c.observeAnnotation(f.ReturnAnnotation, f.returnID, owner, nil)
	c.observeRowLabels(f.ErrorLabels, f.failureID, "raises", owner)
	c.observeRowLabels(f.ServiceLabels, f.serviceID, "uses", owner)
}

// observeFieldSyntax binds field annotations. A generic declaration's type
// variables denote that declaration's own template parameters.
func (c *checker) observeFieldSyntax(fields []Field, owner *DataDeclaration) {
	for _, field := range fields {
		c.observeAnnotation(field.Annotation, field.typeID, lexicalTarget{}, owner)
	}
}

// observeAnnotation pairs a declaration's annotation occurrence with the
// canonical type the checker retained for it. The signature owner's function declares
// the row parameters and data the template parameters in scope there.
func (c *checker) observeAnnotation(occurrence *typeSyntax, id TypeID, owner lexicalTarget, data *DataDeclaration) {
	if occurrence == nil || id == invalidTypeID {
		return
	}
	c.observeTypeSyntax(occurrence, id, owner, data, 0)
}

func (c *checker) observeTypeSyntax(occurrence *typeSyntax, id TypeID, owner lexicalTarget, data *DataDeclaration, depth int) {
	facts := c.result.lexical
	if occurrence == nil || depth > maxTypeSyntaxDepth {
		return
	}
	if qualifier := occurrence.Qualifier; qualifier.Span.Length > 0 {
		if item := facts.aliases[qualifier.Name]; item != nil {
			kind := "module"
			if item.Import != nil {
				kind = "hostModule"
			}
			facts.addReference(qualifier.Span, lexicalTarget{kind: kind, module: item})
		}
	}
	node := c.node(id)
	if node == nil {
		return
	}
	children := func(ids []TypeID) {
		if len(ids) != len(occurrence.Args) {
			return
		}
		for i, child := range occurrence.Args {
			c.observeTypeSyntax(child, ids[i], owner, data, depth+1)
		}
	}
	switch occurrence.Form {
	case "callable":
		if node.Kind != "callable" && node.Kind != "callable-shape" {
			return
		}
		children(node.Args)
		c.observeTypeSyntax(occurrence.Result, node.Result, owner, data, depth+1)
		c.observeRowLabels(occurrence.Failures, node.FailureRow, "raises", owner)
		c.observeRowLabels(occurrence.Services, node.ServiceRow, "uses", owner)
		return
	case "host":
		if node.Kind == "host" {
			children(node.Args)
		}
		return
	case "recipe":
		// Effect<Success, {Failures}, {Services}> names no declaration
		// itself; its success type and row labels do.
		if node.Kind != "recipe" {
			return
		}
		c.observeTypeSyntax(occurrence.Result, node.Result, owner, data, depth+1)
		c.observeRowLabels(occurrence.Failures, node.FailureRow, "raises", owner)
		c.observeRowLabels(occurrence.Services, node.ServiceRow, "uses", owner)
		return
	}
	name := occurrence.Name
	if name.Span.Length == 0 {
		return
	}
	var target lexicalTarget
	switch node.Kind {
	case "application":
		template := c.templates[node.Declaration]
		if template == nil || template.Name != node.Name {
			return
		}
		target = lexicalTarget{kind: template.Kind, data: template}
		children(node.Args)
	case "record":
		target = lexicalTarget{kind: "record", data: c.records[node.Name]}
	case "enum":
		target = lexicalTarget{kind: "enum", data: c.enums[node.Name]}
	case "error":
		target = lexicalTarget{kind: "error", failure: c.errors[node.Name]}
	case "type-variable":
		if data == nil || !slices.ContainsFunc(data.Parameters, func(p TemplateParameter) bool { return p.Identity == node.Declaration }) {
			return
		}
		target = lexicalTarget{kind: "typeParameter", data: data, parameter: node.Name}
	default:
		// Primitives, builtin opaque types and Go host types name no
		// declaration in any snapshot.
		return
	}
	if target.data == nil && target.failure == nil || name.Name != node.Name {
		return
	}
	facts.addReference(name.Span, target)
}

// observeRowLabels reads each label's resolution from the checked row: a
// label the checker bound to a row parameter of the owner's function carries
// that parameter's identity; every other label is a declared failure or
// service.
func (c *checker) observeRowLabels(labels []rowLabel, row RowID, kind string, owner lexicalTarget) {
	if len(labels) == 0 {
		return
	}
	facts, checked := c.result.lexical, c.retainedRowLabels(row)
	for _, label := range labels {
		if f := owner.function; f != nil && slices.Contains(checked, "row-parameter:"+f.Identity+":"+kind+":"+label.Name) {
			parameter := owner
			parameter.kind, parameter.parameter = "rowParameter", label.Name
			facts.addReference(label.Span, parameter)
			continue
		}
		if !slices.Contains(checked, label.Name) {
			continue
		}
		switch kind {
		case "raises":
			if failure := c.errors[label.Name]; failure != nil {
				facts.addReference(label.Span, lexicalTarget{kind: "error", failure: failure})
			}
		case "uses":
			if service := c.services[label.Name]; service != nil {
				facts.addReference(label.Span, lexicalTarget{kind: "service", service: service})
			}
		}
	}
}
