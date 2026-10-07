package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func quoted(s string) string { b, _ := json.Marshal(s); return string(b) }
func jsValueType(program *Program, t string) string {
	if callback, ok := program.callback(t); ok {
		return "(value: " + jsValueType(program, callback.Parameter) + ") => Effect.Effect<" + jsValueType(program, callback.Result) + ", never, never>"
	}
	switch t {
	case "void":
		return "void"
	case "bool":
		return "boolean"
	case "i64":
		return "bigint"
	case "bytes":
		return "Uint8Array"
	case "File":
		return "File"
	case "Latch":
		return "Latch"
	case "never", "invalid":
		return "never"
	default:
		if strings.HasPrefix(t, "provider:") {
			return strings.TrimPrefix(t, "provider:") + "Provider"
		}
		if strings.HasPrefix(t, "GoResult:") {
			return "{ readonly value: " + jsValueType(program, strings.TrimPrefix(t, "GoResult:")) + "; readonly error: Error | undefined }"
		}
		if strings.HasPrefix(t, "Fiber:") {
			return "Fiber.Fiber<" + jsValueType(program, strings.TrimPrefix(t, "Fiber:")) + ">"
		}
		if strings.ContainsAny(t, ":") {
			return "never"
		}
		return t
	}
}
func jsContract(program *Program, f *Function) string {
	return jsContractFor(program, f, nil)
}
func jsContractFor(program *Program, f *Function, declarations map[string]Declaration, nestedDeclarations ...map[string]Declaration) string {
	resultDeclarations := declarations
	if len(nestedDeclarations) > 0 {
		resultDeclarations = nestedDeclarations[0]
	}
	success := jsSourceType(program, f.returnType, f.Return, resultDeclarations)
	if !f.Effect {
		return success
	}
	return jsRowsContract(success, f.Errors, f.Services, declarations)
}

func jsRowsContract(success string, failures, requirements []string, declarations map[string]Declaration) string {
	errors := []string{}
	for _, n := range normalized(failures) {
		if declarations[n].Kind == "row:raises" {
			term := declarations[n].Name
			if term == "" {
				term = n
			}
			errors = append(errors, term)
		} else if declaration, ok := declarations[n]; ok && len(declaration.Fields) > 0 {
			errors = append(errors, n+"Error")
		} else {
			errors = append(errors, "{ readonly _tag: "+quoted(n)+" }")
		}
	}
	err := "never"
	if len(errors) > 0 {
		err = strings.Join(errors, " | ")
	}
	services := []string{}
	for _, n := range normalized(requirements) {
		if declarations[n].Kind == "row:uses" {
			term := declarations[n].Name
			if term == "" {
				term = n
			}
			services = append(services, term)
		} else {
			services = append(services, n+"Requirement")
		}
	}
	serviceType := "never"
	if len(services) > 0 {
		serviceType = strings.Join(services, " | ")
	}
	return "Effect.Effect<" + success + ", " + err + ", " + serviceType + ">"
}

// jsSurface selects the roots of one JavaScript module.
type jsSurface int

const (
	// jsLibrary exports every module declaration (Result.LibraryPlan).
	jsLibrary jsSurface = iota
	// jsEntry runs the checked effect main through the same application plan
	// a native build consumes.
	jsEntry
	// jsTests runs the selected test cases against the harness providers.
	jsTests
)

// Emit consumes only checked compiler IR. JavaScript is never used as a type-checking oracle.
// Entry output lowers the effect main's application plan; library output
// lowers the plan rooted at every export.
func (r *Result) Emit(entry bool) (string, string, error) {
	if entry {
		return r.emitJS(jsEntry)
	}
	return r.emitJS(jsLibrary)
}

// jsExportsBuiltinProvider reports whether a builtin provider has a
// JavaScript implementation and is part of every library's exports.
func jsExportsBuiltinProvider(name string) bool {
	return slices.ContainsFunc(jsPrelude, func(chunk jsPreludeChunk) bool { return chunk.name == "provider:"+name })
}

func (r *Result) emitJS(surface jsSurface) (string, string, error) {
	if !r.Checked {
		return "", "", fmt.Errorf("refusing to emit source with diagnostics")
	}
	// Foreign Go imports and native-only features have no JavaScript
	// lowering. Such a program is refused whether or not its entry reaches
	// them, so pruning never silently drops a host binding or its module
	// initialization.
	if r.Program.GoOnly {
		return "", "", fmt.Errorf("program uses features currently implemented only for Go")
	}
	var plan *ApplicationPlan
	var err error
	switch surface {
	case jsEntry:
		plan, err = r.ApplicationPlan(GoGenerationBuild)
	case jsTests:
		plan, err = r.ApplicationPlan(GoGenerationTest)
	default:
		plan, err = r.LibraryPlan()
	}
	if err != nil {
		return "", "", err
	}
	selection := newJSSelection()
	for _, helper := range plan.Identities(RequiresHelper) {
		chunk, ok := jsPreludeChunkFor[helper]
		if !ok {
			return "", "", fmt.Errorf("JavaScript has no lowering for helper %s", helper)
		}
		if err := selection.use(chunk); err != nil {
			return "", "", err
		}
	}
	if len(plan.Identities(RequiresOperation)) > 0 {
		if err := selection.use("call"); err != nil {
			return "", "", err
		}
	}
	if len(plan.Identities(RequiresLayer)) > 0 {
		if err := selection.use("layers"); err != nil {
			return "", "", err
		}
	}
	if surface == jsTests {
		if err := selection.use("test-harness"); err != nil {
			return "", "", err
		}
	}
	var out, decl strings.Builder
	declarations := declarationMap(r)
	// TypeScript declarations of data, errors, templates, service requirements
	// and provider shapes are type-only and erased, so every module keeps the
	// complete set: retained signatures may name any of them. Value
	// declarations mirror exactly the values the JavaScript exports.
	decl.WriteString("import type { Context, Effect, Fiber } from 'effect';\nexport interface File {readonly _effraFile: \"File\"}\n")
	decl.WriteString("export interface Latch {readonly _effraLatch: \"Latch\"}\n")
	for _, declaration := range r.ProgramDeclarations() {
		switch declaration.Kind {
		case "record":
			decl.WriteString("declare const __ef_brand_" + declaration.Name + ": unique symbol;\n")
			decl.WriteString("export interface " + declaration.Name + " { ")
			decl.WriteString("readonly [__ef_brand_" + declaration.Name + "]: \"" + declaration.Name + "\"; ")
			for _, field := range declaration.Fields {
				decl.WriteString("readonly " + field.Name + ": " + jsSourceType(r.Program, field.sourceType, field.Type, declarations) + "; ")
			}
			decl.WriteString("}\n")
		case "enum":
			decl.WriteString("declare const __ef_brand_" + declaration.Name + ": unique symbol;\n")
			if len(declaration.Variants) == 0 {
				decl.WriteString("export type " + declaration.Name + " = never;\n")
				break
			}
			decl.WriteString("export type " + declaration.Name + " = ")
			for i, variant := range declaration.Variants {
				if i > 0 {
					decl.WriteString(" | ")
				}
				decl.WriteString("{ readonly [__ef_brand_" + declaration.Name + "]: \"" + declaration.Name + "\"; readonly _tag: " + quoted(declaration.Name+"."+variant.Name))
				for _, field := range variant.Fields {
					decl.WriteString("; readonly " + field.Name + ": " + jsSourceType(r.Program, field.sourceType, field.Type, declarations))
				}
				decl.WriteString(" }")
			}
			decl.WriteString(";\n")
		case "error":
			decl.WriteString("export interface " + declaration.Name + "Error { readonly _tag: " + quoted(declaration.Name))
			for _, field := range declaration.Fields {
				decl.WriteString("; readonly " + field.Name + ": " + jsSourceType(r.Program, field.sourceType, field.Type, declarations))
			}
			decl.WriteString(" }\n")
		}
	}
	allServices := append(builtinServicesFor(r.Program), r.Program.Services...)
	for _, s := range allServices {
		decl.WriteString("export interface " + s.Name + "Requirement { readonly _effraService: " + quoted(s.Name) + " }\n")
		decl.WriteString("export type " + s.Name + "Provider = " + jsShape(r.Program, s.Methods, declarations, false) + ";\n")
		if !plan.Requires(RequiresService, serviceIdentity(s.Name)) {
			continue
		}
		selection.importEffect("Context")
		out.WriteString("const __ef_service_" + s.Name + "=Context.Service(" + quoted("effra/prototype/"+s.Name) + ");\nexport { __ef_service_" + s.Name + " as " + s.Name + " };\n")
		decl.WriteString("declare const __ef_service_" + s.Name + ": Context.Service<" + s.Name + "Requirement, " + jsShape(r.Program, s.Methods, declarations, false) + ">;\nexport { __ef_service_" + s.Name + " as " + s.Name + " };\n")
	}
	for _, p := range builtinProvidersFor(r.Program) {
		if !plan.Requires(RequiresProvider, providerTypeRef(p).Declaration) {
			continue
		}
		if err := selection.use("provider:" + p.Name); err != nil {
			return "", "", err
		}
		out.WriteString("export {__ef_provider_" + p.Name + " as " + p.Name + "};\n")
		decl.WriteString("declare const __ef_provider_" + p.Name + ": " + jsShape(r.Program, r.Program.semantic.services[p.Service].Methods, declarations, false) + ";\nexport {__ef_provider_" + p.Name + " as " + p.Name + "};\n")
	}
	for _, p := range r.Program.Providers {
		if !plan.Requires(RequiresProvider, providerTypeRef(p).Declaration) {
			continue
		}
		selection.importEffect("Effect")
		if providerConstructed(p) {
			out.WriteString(jsProviderConstructor(p))
			decl.WriteString("declare const __ef_provider_" + p.Name + ": " + jsConstructorType(r.Program, p, declarations) + ";\nexport { __ef_provider_" + p.Name + " as " + p.Name + " };\n")
		} else {
			out.WriteString("const __ef_provider_" + p.Name + " = {\n")
			for _, f := range p.Methods {
				out.WriteString("[" + quoted(f.Name) + "]: " + jsFunction(f) + ",\n")
			}
			out.WriteString("};\n")
			decl.WriteString("declare const __ef_provider_" + p.Name + ": " + jsShape(r.Program, p.Methods, declarations, true) + ";\nexport { __ef_provider_" + p.Name + " as " + p.Name + " };\n")
		}
		out.WriteString("export { __ef_provider_" + p.Name + " as " + p.Name + " };\n")
		for _, f := range p.Methods {
			if f.Effect {
				if err := selection.use("lifecycle"); err != nil {
					return "", "", err
				}
			}
		}
	}
	for _, layer := range r.Layers {
		if plan.Requires(RequiresLayer, layer.ID) {
			out.WriteString(jsLayer(layer))
		}
	}
	out.WriteString(r.jsCodecSupport(plan))
	for _, f := range r.Program.checkedFunctions() {
		if !plan.Requires(RequiresFunction, f.Identity) {
			continue
		}
		if f.Effect {
			if err := selection.use("lifecycle"); err != nil {
				return "", "", err
			}
		}
		out.WriteString("const " + f.jsEmissionName() + " = " + jsFunction(f) + ";\n")
		if f.Module == currentModuleIdentity && f.codec == nil {
			out.WriteString("export { " + f.jsEmissionName() + " as " + f.Name + " };\n")
			decl.WriteString("declare const " + f.jsEmissionName() + ": " + jsRowFunctionSignature(r.Program, f, declarations) + ";\nexport { " + f.jsEmissionName() + " as " + f.Name + " };\n")
		}
	}
	r.jsCodecExports(plan, &out, &decl, declarations)
	for _, r := range r.Program.BundledTemplates {
		decl.WriteString(jsTemplateDeclaration(r))
	}
	for _, data := range append(append([]*DataDeclaration{}, r.Program.Records...), r.Program.Enums...) {
		if len(data.Parameters) > 0 {
			decl.WriteString(jsTemplateDeclaration(data))
		}
	}
	if surface == jsEntry {
		selection.importEffect("Effect")
		// SIGINT/SIGTERM interrupt main, as the Go entry's signal context does.
		out.WriteString("const __ef_signal = new AbortController();\nconst __ef_stop = () => __ef_signal.abort();\nprocess.once('SIGINT', __ef_stop);\nprocess.once('SIGTERM', __ef_stop);\nEffect.runPromise(__ef_function_main(), { signal: __ef_signal.signal }).then(value => { if (value !== undefined) console.log(typeof value === 'bigint' ? value.toString() : value); }, error => { console.error(error); process.exitCode = 1; }).finally(() => { process.off('SIGINT', __ef_stop); process.off('SIGTERM', __ef_stop); });\n")
	}
	return "// Generated by the Effra prototype. Source revision: " + r.Revision + "\n" + selection.prelude() + out.String(), decl.String(), nil
}

func jsTemplateDeclaration(r *Record) string {
	var decl strings.Builder
	parameters := []string{}
	arguments := []string{}
	dataSlots := []string{}
	declarations := declarationMap(r.owner.result)
	for _, p := range r.Parameters {
		name := emittedTypeVariable(r.owner.node(p.typeID))
		arguments = append(arguments, name)
		parameter := name
		if p.Kind == "callable" {
			parameter += " extends " + canonicalJSDataType(r.owner, p.shapeID, declarations)
		}
		// Every argument belongs to application identity, even when a closed
		// alternative has no payload or a callable slot is otherwise phantom.
		dataSlots = append(dataSlots, "(value: "+name+") => "+name)
		parameters = append(parameters, parameter)
	}
	brand := "__ef_brand_template_" + r.EmissionName
	decl.WriteString("declare const " + brand + ": unique symbol;\n")
	decl.WriteString("type __ef_template_" + r.EmissionName + "<" + strings.Join(parameters, ", ") + "> = ")
	payload := func(fields []Field, variant string) {
		decl.WriteString("{ readonly [" + brand + "]: readonly [" + strings.Join(dataSlots, ",") + "]; ")
		if variant != "" {
			decl.WriteString("readonly _tag: " + quoted(r.Identity+"."+variant) + "; ")
		}
		for _, field := range fields {
			decl.WriteString("readonly " + quoted(field.Name) + ": " + canonicalJSDataType(r.owner, field.typeID, declarations) + "; ")
		}
		decl.WriteString("}")
	}
	if r.Kind == "enum" {
		if len(r.Variants) == 0 {
			decl.WriteString("never")
		}
		for i, variant := range r.Variants {
			if i > 0 {
				decl.WriteString(" | ")
			}
			payload(variant.Fields, variant.Name)
		}
	} else {
		payload(r.Fields, "")
	}
	decl.WriteString(";\n")
	if r.Module == currentModuleIdentity {
		decl.WriteString("export type " + r.Name + "<" + strings.Join(parameters, ",") + "> = __ef_template_" + r.EmissionName + "<" + strings.Join(arguments, ",") + ">;\n")
	}
	return decl.String()
}

// ProgramDeclarations is a target-independent view used by both emitters.
// Keep this projection on Result so declaration order and identity remain part
// of the checked semantic model rather than being rediscovered by a backend.
func (r *Result) ProgramDeclarations() []Declaration {
	return append([]Declaration{}, r.Declarations...)
}
func jsFunction(f *Function) string {
	params := []string{}
	for _, p := range f.Params {
		params = append(params, "__ef_local_"+p.Name)
	}
	return "(" + strings.Join(params, ", ") + ") => " + jsEffectBody(f)
}
func jsEffectBody(f *Function) string {
	open, close := "{", "}"
	if f.Effect {
		open = "__ef_autoScope(Effect.gen(function* () {"
		close = "}))"
	}
	return open + "\n" + jsBlock(f.Body, f.Effect) + close
}

func jsProviderMethod(f *Function, captures []string) string {
	params := []string{}
	for _, p := range f.Params {
		params = append(params, "__ef_local_"+p.Name)
	}
	effect := jsEffectBody(f)
	for i := len(normalized(captures)) - 1; i >= 0; i-- {
		service := normalized(captures)[i]
		effect = "Effect.provideService(" + effect + ", __ef_service_" + service + ", __ef_capture_" + service + ")"
	}
	return "(" + strings.Join(params, ", ") + ") => " + effect
}

func jsProviderConstructor(p *Provider) string {
	params := []string{}
	for _, param := range p.Params {
		params = append(params, "__ef_local_"+param.Name)
	}
	methods := []string{}
	for _, method := range p.Methods {
		implementation := jsFunction(method)
		if len(p.Services) > 0 {
			implementation = jsProviderMethod(method, p.Services)
		}
		methods = append(methods, "["+quoted(method.Name)+"]: "+implementation)
	}
	// Allocate the provider object at execution time. A constructor call is a
	// lazy recipe, so replaying one recipe must materialize a fresh value each
	// time it runs, even when it captures no service context.
	body := "Effect.sync(() => ({" + strings.Join(methods, ", ") + "}))"
	for i := len(normalized(p.Services)) - 1; i >= 0; i-- {
		service := normalized(p.Services)[i]
		body = "Effect.flatMap(__ef_service_" + service + ", __ef_capture_" + service + " => " + body + ")"
	}
	return "const __ef_provider_" + p.Name + " = (" + strings.Join(params, ", ") + ") => " + body + ";\n"
}

func jsConstructorType(program *Program, p *Provider, declarations map[string]Declaration) string {
	constructor := &Function{Name: p.Name, Params: p.Params, Return: "provider:" + p.Service, Effect: true, Services: normalized(p.Services)}
	return "(" + jsFunctionParams(program, p.Params, declarations) + ") => " + jsContractFor(program, constructor, declarations)
}

func jsFunctionParams(program *Program, params []Param, declarations map[string]Declaration) string {
	parts := []string{}
	for _, p := range params {
		parts = append(parts, "arg_"+p.Name+": "+jsSourceType(program, p.sourceType, p.Type, declarations))
	}
	return strings.Join(parts, ", ")
}

func jsBlock(b *Block, effect bool) string {
	var out strings.Builder
	if len(b.Statements) == 0 {
		return "return undefined;\n"
	}
	for i, s := range b.Statements {
		switch s.Kind {
		case "let":
			out.WriteString("const __ef_local_" + s.Name + " = " + jsExpr(s.Value, effect) + ";\n")
			if i == len(b.Statements)-1 {
				out.WriteString("return undefined;\n")
			}
		case "fail":
			payload := "{}"
			if s.Payload != nil {
				payload = jsPayload(s.Payload, effect)
			}
			out.WriteString("return yield* Effect.fail({ _tag: " + quoted(s.Name) + ", ..." + payload + " });\n")
		default:
			if i == len(b.Statements)-1 {
				out.WriteString("return ")
			}
			out.WriteString(jsExpr(s.Value, effect) + ";\n")
		}
	}
	return out.String()
}
func jsExpr(e *Expr, effect bool) string {
	switch e.Kind {
	case "codec":
		return jsCodecOperation(e)
	case "member":
		if e.ResolvedFunction != nil {
			return e.ResolvedFunction.jsEmissionName()
		}
		left := jsExpr(e.Left, effect)
		if e.Text == "field" {
			return left + "[" + quoted(e.Name) + "]"
		}
		if e.Name == "hasError" {
			return "(" + left + ".error != null)"
		}
		return left + ".value"
	case "scope":
		return "(yield* __ef_scoped(Effect.gen(function*(){" + jsBlock(e.Then, true) + "})))"
	case "fork":
		return "(yield* __ef_fork(" + jsExpr(e.Left, effect) + "))"
	case "timeout":
		return "__ef_timeout(" + jsExpr(e.Left, effect) + "," + jsExpr(e.Right, effect) + ")"
	case "integer":
		return e.Text + "n"
	case "string":
		return quoted(e.Text)
	case "bool":
		return e.Text
	case "void":
		return "undefined"
	case "construct":
		return jsConstruct(e, effect)
	case "name":
		if e.Text == "function" {
			return e.ResolvedFunction.jsEmissionName()
		}
		if e.Text == "provider" {
			return "__ef_provider_" + e.Name
		}
		return "__ef_local_" + e.Name
	case "call":
		if e.Text == "callable" {
			args := []string{}
			for _, arg := range e.Args {
				args = append(args, jsExpr(arg, effect))
			}
			return "(" + jsExpr(e.Left, effect) + ")(" + strings.Join(args, ", ") + ")"
		}
		if e.Text == "data" {
			return jsConstructCall(e, effect)
		}
		if e.Text == "fiber" {
			method := e.Left.Name
			if method == "interrupt" {
				return "__ef_interrupt(" + jsExpr(e.Left.Left, effect) + ")"
			}
			if method == "cancel" {
				return "__ef_cancel(" + jsExpr(e.Left.Left, effect) + ")"
			}
			return "__ef_join(" + jsExpr(e.Left.Left, effect) + ")"
		}
		if e.Text == "provider-constructor" {
			return jsBoundCall(e, effect, func(args string) string {
				return "__ef_provider_" + e.Left.Name + "(" + args + ")"
			})
		}
		if e.ResolvedFunction != nil && e.ResolvedFunction.Owner == "module" {
			return jsBoundCall(e, effect, func(args string) string {
				return e.ResolvedFunction.jsEmissionName() + "(" + args + ")"
			})
		}
		if e.Left.Kind == "name" {
			return jsBoundCall(e, effect, func(args string) string {
				return "__ef_function_" + e.Left.Name + "(" + args + ")"
			})
		}
		return jsBoundCall(e, effect, func(args string) string {
			return "__ef_call(__ef_service_" + e.Left.Left.Name + ", " + quoted(e.Left.Name) + ", [" + args + "])"
		})
	case "run":
		return "(yield* " + jsExpr(e.Left, effect) + ")"
	case "provide":
		return "Effect.provideService(" + jsExpr(e.Left, effect) + ", __ef_service_" + e.Name + ", " + jsExpr(e.Right, effect) + ")"
	case "provideLayer":
		return "__ef_layer_" + e.Name + "(" + jsExpr(e.Left, effect) + ")"
	case "catch":
		return "__ef_catch(" + jsExpr(e.Left, effect) + ", " + quoted(e.Name) + ", () => (" + jsExpr(e.Right, false) + "))"
	case "binary":
		op := e.Name
		if op == "==" {
			op = "==="
		}
		return "(" + jsExpr(e.Left, effect) + " " + op + " " + jsExpr(e.Right, effect) + ")"
	case "if":
		body := "if (" + jsExpr(e.Left, effect) + ") {\n" + jsBlock(e.Then, effect) + "} else {\n" + jsBlock(e.Else, effect) + "}\n"
		if effect {
			return "(yield* Effect.gen(function* () {\n" + body + "}))"
		}
		return "(() => {\n" + body + "})()"
	case "match":
		return jsMatch(e, effect)
	}
	panic("unchecked expression reached emitter")
}

// jsBoundCall emits a checked call whose arguments bind parameters. A call
// whose labels reorder its arguments evaluates them, in source order, as the
// arguments of an adapter that passes them on in parameter order; the adapter
// body only reads its own parameters, so a yield in an argument stays in the
// enclosing generator.
func jsBoundCall(e *Expr, effect bool, call func(args string) string) string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = jsExpr(a, effect)
	}
	if e.ArgumentParameters == nil {
		return call(strings.Join(args, ", "))
	}
	names := make([]string, len(e.Args))
	bound := make([]string, len(e.Args))
	for i, parameter := range e.ArgumentParameters {
		names[i] = "__ef_argument_" + strconv.Itoa(i)
		bound[parameter] = names[i]
	}
	return "((" + strings.Join(names, ", ") + ") => " + call(strings.Join(bound, ", ")) + ")(" + strings.Join(args, ", ") + ")"
}

func jsPayload(e *Expr, effect bool) string {
	if e.Kind != "payload" {
		return "{ value: " + jsExpr(e, effect) + " }"
	}
	parts := []string{}
	for _, field := range e.Fields {
		parts = append(parts, "["+quoted(field.Name)+"]: "+jsExpr(field.Value, false))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
func jsConstruct(e *Expr, effect bool) string {
	parts := []string{}
	for _, field := range e.Fields {
		parts = append(parts, "["+quoted(field.Name)+"]: "+jsExpr(field.Value, false))
	}
	if e.ResolvedTemplate != nil {
		if e.ResolvedTemplate.Kind == "enum" {
			return "({ _tag: " + quoted(e.ResolvedTemplate.Identity+"."+e.Left.Name) + ", " + strings.Join(parts, ", ") + " })"
		}
		return "({ " + strings.Join(parts, ", ") + " })"
	}
	if e.Left != nil && e.Left.Kind == "member" {
		return "({ _tag: " + quoted(e.Left.Left.Name+"."+e.Left.Name) + ", " + strings.Join(parts, ", ") + " })"
	}
	return "({ " + strings.Join(parts, ", ") + " })"
}
func jsConstructCall(e *Expr, effect bool) string {
	return jsConstruct(e, effect)
}

// jsMatch lowers the checked match plan with the same first-match order as
// Go: subjects are evaluated once, in order, and each body is emitted once.
func jsMatch(e *Expr, effect bool) string {
	plan := e.matchPlan
	body := ""
	subjects := make([]string, len(e.Args))
	for index, subject := range e.Args {
		subjects[index] = "__ef_match_" + strconv.Itoa(index)
		body += "const " + subjects[index] + " = " + jsExpr(subject, effect) + ";\n"
	}
	for _, arm := range plan.arms {
		conditions := []string{}
		for index, cell := range arm.cells {
			if cell.total {
				continue
			}
			alternatives := []string{}
			for _, pattern := range cell.alternatives {
				alternatives = append(alternatives, subjects[index]+"._tag === "+quoted(jsVariantTag(pattern)))
			}
			conditions = append(conditions, "("+strings.Join(alternatives, " || ")+")")
		}
		if len(conditions) == 0 {
			conditions = append(conditions, "true")
		}
		body += "if (" + strings.Join(conditions, " && ") + ") {\n"
		for _, binding := range arm.bindings {
			subject := subjects[binding.subject]
			alternatives := arm.cells[binding.subject].alternatives
			value := subject + "[" + quoted(binding.fields[len(alternatives)-1]) + "]"
			for alternative := len(alternatives) - 2; alternative >= 0; alternative-- {
				value = subject + "._tag === " + quoted(jsVariantTag(alternatives[alternative])) + " ? " + subject + "[" + quoted(binding.fields[alternative]) + "] : " + value
			}
			body += "const __ef_local_" + binding.name + " = " + value + ";\n"
		}
		body += jsBlock(arm.body, effect) + "}\n"
	}
	body += "throw new Error(\"unreachable non-exhaustive match\");\n"
	if effect {
		return "(yield* Effect.gen(function* () {\n" + body + "}))"
	}
	return "(() => {\n" + body + "})()"
}

func jsVariantTag(pattern *MatchPattern) string {
	owner := pattern.TypeName
	if enum := pattern.ResolvedEnum; enum != nil && len(enum.Parameters) > 0 {
		owner = enum.Identity
	}
	return owner + "." + pattern.VariantName
}

func declarationMap(r *Result) map[string]Declaration {
	result := map[string]Declaration{}
	if r == nil {
		return result
	}
	for _, declaration := range r.Declarations {
		result[declaration.Name] = declaration
	}
	return result
}
func jsShape(program *Program, methods []*Function, declarations map[string]Declaration, hideServices bool) string {
	out := "{ "
	for _, f := range methods {
		contract := f
		if hideServices {
			copy := *f
			copy.Services = nil
			contract = &copy
		}
		out += "readonly " + quoted(f.Name) + ": " + jsRowFunctionSignature(program, contract, declarations) + "; "
	}
	return out + "}"
}
