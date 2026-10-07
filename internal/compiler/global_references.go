package compiler

import "maps"

// referencesGlobal reports whether a parsed program refers to any of the
// global names. Contract positions (service rows, provide<S>, impl ... for S,
// layer bindings and annotations) and top-level declarations name globals
// directly. A value name refers to a global only when no enclosing parameter,
// let or match binding introduces it, exactly as the checker's environment
// resolves it; a row label is shadowed only by the function's row parameter.
// Strings, comments, fields and member names are never references.
func referencesGlobal(program *Program, names ...string) bool {
	g := globalReferences{names: map[string]bool{}}
	for _, name := range names {
		g.names[name] = true
	}
	for _, imp := range program.Imports {
		g.declaration(imp.Alias)
	}
	for _, imp := range program.BundledImports {
		g.declaration(imp.Alias)
	}
	for name := range program.Errors {
		g.declaration(name)
	}
	for _, decl := range program.ErrorDecls {
		g.fields(decl.Fields)
	}
	for _, data := range append(append([]*DataDeclaration{}, program.Records...), program.Enums...) {
		g.declaration(data.Name)
		g.fields(data.Fields)
		for _, variant := range data.Variants {
			g.fields(variant.Fields)
		}
	}
	for _, s := range program.Services {
		g.declaration(s.Name)
		for _, method := range s.Methods {
			g.function(method, nil)
		}
	}
	for _, p := range program.Providers {
		g.declaration(p.Name)
		g.declaration(p.Service)
		g.row(p.Services, nil)
		configuration := map[string]bool{}
		for _, param := range p.Params {
			g.sourceType(param.sourceType, nil)
			configuration[param.Name] = true
		}
		for _, method := range p.Methods {
			g.function(method, configuration)
		}
	}
	for _, layer := range program.Layers {
		g.declaration(layer.Name)
		g.row(layer.Provides, nil)
		g.row(layer.Services, nil)
		for _, entry := range layer.Entries {
			if entry.Kind != "merge" {
				g.declaration(entry.Name)
			}
			g.expr(entry.Value, nil)
		}
	}
	for _, f := range program.Functions {
		g.declaration(f.Name)
		g.function(f, nil)
	}
	return g.found
}

type globalReferences struct {
	names map[string]bool
	found bool
}

func (g *globalReferences) declaration(name string) {
	if g.names[name] {
		g.found = true
	}
}

func (g *globalReferences) row(labels []string, rowParameters map[string]bool) {
	for _, label := range labels {
		if g.names[label] && !rowParameters[label] {
			g.found = true
		}
	}
}

func (g *globalReferences) sourceType(t *sourceType, rowParameters map[string]bool) {
	if t == nil || g.found {
		return
	}
	g.row(t.Services, rowParameters)
	for _, parameter := range t.ParameterTypes {
		g.sourceType(parameter, rowParameters)
	}
	g.sourceType(t.ResultType, rowParameters)
	for _, argument := range t.ApplicationArgumentTypes {
		g.sourceType(argument, rowParameters)
	}
}

func (g *globalReferences) fields(fields []Field) {
	for _, field := range fields {
		g.sourceType(field.sourceType, nil)
	}
}

func (g *globalReferences) function(f *Function, outer map[string]bool) {
	rows := map[string]bool{}
	for _, parameter := range f.RowParameters {
		rows[parameter.Name] = true
	}
	g.row(f.Services, rows)
	env := maps.Clone(outer)
	if env == nil {
		env = map[string]bool{}
	}
	for _, param := range f.Params {
		g.sourceType(param.sourceType, rows)
		env[param.Name] = true
	}
	g.sourceType(f.returnType, rows)
	g.block(f.Body, env)
}

func (g *globalReferences) block(b *Block, outer map[string]bool) {
	if b == nil || g.found {
		return
	}
	env := maps.Clone(outer)
	if env == nil {
		env = map[string]bool{}
	}
	for _, s := range b.Statements {
		g.expr(s.Value, env)
		g.expr(s.Payload, env)
		if s.Kind == "let" {
			env[s.Name] = true
		}
	}
}

func (g *globalReferences) expr(e *Expr, env map[string]bool) {
	if e == nil || g.found {
		return
	}
	switch e.Kind {
	case "name":
		if g.names[e.Name] && !env[e.Name] {
			g.found = true
			return
		}
	case "provide":
		g.declaration(e.Name)
	}
	g.sourceType(e.constructorType, nil)
	forEachExprChild(e, func(child *Expr) { g.expr(child, env) })
	g.block(e.Then, env)
	g.block(e.Else, env)
	for _, arm := range e.Arms {
		branch := maps.Clone(env)
		if branch == nil {
			branch = map[string]bool{}
		}
		arm.EachPattern(func(_ int, pattern *MatchPattern) {
			for _, name := range pattern.Bindings {
				if name != "_" {
					branch[name] = true
				}
			}
		})
		g.block(arm.Body, branch)
	}
}
