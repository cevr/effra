package compiler

import "maps"

// localBinding is one lexical value binder: a function parameter, a provider
// configuration parameter, a let statement or a match pattern name. The
// checker's environment is keyed by binder, so name resolution has exactly
// one owner: resolveBindings.
type localBinding struct {
	Kind string
	Name string
	// rebinds is the visible binder a let statement redeclares.
	rebinds *localBinding
}

// localEnv holds the checked value of each binder in scope.
type localEnv map[*localBinding]checkedExpression

// lexicalScope is the binding context of one source position. Values follow
// block, let and match-arm structure; rows are the enclosing function's row
// parameters, which bind throughout its signature and body.
type lexicalScope struct {
	values map[string]*localBinding
	rows   map[string]*RowParameter
}

// functionRowScope is the row-binding rule: a function's row parameters bind
// their names, by kind, everywhere in that function. A repeated name keeps
// the last declaration; the checker diagnoses the repetition.
func functionRowScope(f *Function) map[string]*RowParameter {
	rows := map[string]*RowParameter{}
	for i := range f.RowParameters {
		rows[f.RowParameters[i].Name] = &f.RowParameters[i]
	}
	return rows
}

// resolveBindings resolves every value name and row label of a program
// before checking. Each name expression and static layer provision records
// its local binder (nil for a global), each binder position records the
// binder it introduces, and program.references records every global name the
// program refers to. The checker consumes the binders; builtin admission
// consumes the references, before the data the admitted contracts declare is
// registered. Strings, comments, fields and member names are never
// references.
func resolveBindings(program *Program) {
	r := resolver{references: map[string]bool{}}
	for _, imp := range program.Imports {
		r.global(imp.Alias)
	}
	for _, imp := range program.BundledImports {
		r.global(imp.Alias)
	}
	for name := range program.Errors {
		r.global(name)
	}
	empty := lexicalScope{}
	for _, decl := range program.ErrorDecls {
		r.fields(decl.Fields, empty)
	}
	for _, data := range append(append([]*DataDeclaration{}, program.Records...), program.Enums...) {
		r.global(data.Name)
		r.fields(data.Fields, empty)
		for _, variant := range data.Variants {
			r.fields(variant.Fields, empty)
		}
	}
	for _, s := range program.Services {
		r.global(s.Name)
		for _, method := range s.Methods {
			r.function(method, empty)
		}
	}
	for _, p := range program.Providers {
		r.global(p.Name)
		r.global(p.Service)
		r.row(p.Services, "uses", empty)
		configuration := lexicalScope{values: map[string]*localBinding{}}
		for i := range p.Params {
			r.sourceType(p.Params[i].sourceType, empty)
			configuration.values[p.Params[i].Name] = r.bind(&p.Params[i], "configuration")
		}
		for _, method := range p.Methods {
			r.function(method, configuration)
		}
	}
	for _, layer := range program.Layers {
		r.global(layer.Name)
		r.row(layer.Provides, "uses", empty)
		r.row(layer.Services, "uses", empty)
		for _, entry := range layer.Entries {
			if entry.Kind != "merge" {
				r.global(entry.Name)
			}
			r.expr(entry.Value, empty)
		}
	}
	for _, f := range program.Functions {
		r.global(f.Name)
		r.function(f, empty)
	}
	for _, f := range program.BundledFunctions {
		r.function(f, empty)
	}
	program.references = r.references
}

type resolver struct {
	references map[string]bool
}

func (r *resolver) global(name string) { r.references[name] = true }

func (r *resolver) bind(p *Param, kind string) *localBinding {
	p.binding = &localBinding{Kind: kind, Name: p.Name}
	return p.binding
}

func (r *resolver) row(labels []string, kind string, scope lexicalScope) {
	for _, label := range labels {
		if p := scope.rows[label]; p == nil || p.Kind != kind {
			r.global(label)
		}
	}
}

func (r *resolver) sourceType(t *sourceType, scope lexicalScope) {
	if t == nil {
		return
	}
	r.row(t.Services, "uses", scope)
	for _, parameter := range t.ParameterTypes {
		r.sourceType(parameter, scope)
	}
	r.sourceType(t.ResultType, scope)
	for _, argument := range t.ApplicationArgumentTypes {
		r.sourceType(argument, scope)
	}
}

func (r *resolver) fields(fields []Field, scope lexicalScope) {
	for _, field := range fields {
		r.sourceType(field.sourceType, scope)
	}
}

func (r *resolver) function(f *Function, outer lexicalScope) {
	scope := lexicalScope{values: maps.Clone(outer.values), rows: functionRowScope(f)}
	if scope.values == nil {
		scope.values = map[string]*localBinding{}
	}
	r.row(f.Services, "uses", scope)
	for i := range f.Params {
		r.sourceType(f.Params[i].sourceType, scope)
		scope.values[f.Params[i].Name] = r.bind(&f.Params[i], "parameter")
	}
	r.sourceType(f.returnType, scope)
	r.block(f.Body, scope)
}

func (r *resolver) block(b *Block, outer lexicalScope) {
	if b == nil {
		return
	}
	scope := lexicalScope{values: maps.Clone(outer.values), rows: outer.rows}
	if scope.values == nil {
		scope.values = map[string]*localBinding{}
	}
	for _, s := range b.Statements {
		r.expr(s.Value, scope)
		r.expr(s.Payload, scope)
		if s.Kind == "let" {
			s.binding = &localBinding{Kind: "let", Name: s.Name, rebinds: scope.values[s.Name]}
			scope.values[s.Name] = s.binding
		}
	}
}

func (r *resolver) expr(e *Expr, scope lexicalScope) {
	if e == nil {
		return
	}
	switch e.Kind {
	case "name", "provideLayer":
		e.binding = scope.values[e.Name]
		if e.binding == nil {
			r.global(e.Name)
		}
	case "provide":
		r.global(e.Name)
	}
	r.sourceType(e.constructorType, scope)
	forEachExprChild(e, func(child *Expr) { r.expr(child, scope) })
	r.block(e.Then, scope)
	r.block(e.Else, scope)
	for _, arm := range e.Arms {
		branch := lexicalScope{values: maps.Clone(scope.values), rows: scope.rows}
		if branch.values == nil {
			branch.values = map[string]*localBinding{}
		}
		arm.binders = map[string]*localBinding{}
		arm.EachPattern(func(_ int, pattern *MatchPattern) {
			for _, field := range sortedBindingNames(pattern.Bindings) {
				name := pattern.Bindings[field]
				if name == "_" || arm.binders[name] != nil {
					continue
				}
				arm.binders[name] = &localBinding{Kind: "pattern", Name: name}
				branch.values[name] = arm.binders[name]
			}
		})
		r.block(arm.Body, branch)
	}
}
