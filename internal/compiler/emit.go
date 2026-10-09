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
	module, err := r.EmitModule(entry)
	return module.Source, module.Declaration, err
}

// JSImport is one module a generated JavaScript module imports. A static
// import names its bindings; a dynamic import is loaded on demand through
// import() with this literal specifier and binds no names statically.
type JSImport struct {
	Specifier string   `json:"specifier"`
	Names     []string `json:"names"`
	Dynamic   bool     `json:"dynamic,omitempty"`
}

// JSModule is one emitted JavaScript module, its TypeScript declarations
// and the complete set of modules it imports, as emission declared them.
type JSModule struct {
	Source      string
	Declaration string
	Imports     []JSImport
}

// EmitModule emits the entry or library module with its declared imports.
func (r *Result) EmitModule(entry bool) (JSModule, error) {
	if entry {
		return r.emitJSModule(jsEntry)
	}
	return r.emitJSModule(jsLibrary)
}

func (r *Result) emitJS(surface jsSurface) (string, string, error) {
	module, err := r.emitJSModule(surface)
	return module.Source, module.Declaration, err
}

// jsExportsBuiltinProvider reports whether a builtin provider has a
// JavaScript implementation and is part of every library's exports.
func jsExportsBuiltinProvider(name string) bool {
	return slices.ContainsFunc(jsPrelude, func(chunk jsPreludeChunk) bool { return chunk.name == "provider:"+name })
}

func (r *Result) emitJSModule(surface jsSurface) (JSModule, error) {
	if !r.Checked {
		return JSModule{}, fmt.Errorf("refusing to emit source with diagnostics")
	}
	// Foreign Go imports and native-only features have no JavaScript
	// lowering. Such a program is refused whether or not its entry reaches
	// them, so pruning never silently drops a host binding or its module
	// initialization.
	if r.Program.GoOnly {
		return JSModule{}, fmt.Errorf("program uses features currently implemented only for Go")
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
		return JSModule{}, err
	}
	selection := newJSSelection()
	for _, helper := range plan.Identities(RequiresHelper) {
		chunk, ok := jsPreludeChunkFor[helper]
		if !ok {
			return JSModule{}, fmt.Errorf("JavaScript has no lowering for helper %s", helper)
		}
		if err := selection.use(chunk); err != nil {
			return JSModule{}, err
		}
	}
	if len(plan.Identities(RequiresOperation)) > 0 {
		if err := selection.use("call"); err != nil {
			return JSModule{}, err
		}
	}
	if len(plan.Identities(RequiresLayer)) > 0 {
		if err := selection.use("layers"); err != nil {
			return JSModule{}, err
		}
	}
	if surface == jsTests {
		if err := selection.use("test-harness"); err != nil {
			return JSModule{}, err
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
			// The declared error name is also its payload's value type.
			decl.WriteString("export type " + declaration.Name + " = " + declaration.Name + "Error;\n")
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
			return JSModule{}, err
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
					return JSModule{}, err
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
				return JSModule{}, err
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
	source := "// Generated by the Effra prototype. Source revision: " + r.Revision + "\n" + selection.prelude() + out.String()
	return JSModule{Source: source, Declaration: decl.String(), Imports: selection.moduleImports()}, nil
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
	if loop := lowerTail(f); loop != nil {
		return jsLoopFunction(f, loop)
	}
	params := []string{}
	for _, p := range f.Params {
		params = append(params, "__ef_local_"+p.Name)
	}
	return "(" + strings.Join(params, ", ") + ") => " + jsEffectBody(f)
}

// jsLoopFunction lowers a pure function with self tail calls to a loop. The
// parameters are only the loop's carried state (__ef_arg_*); the body reads
// per-iteration constants (__ef_local_*), so anything it captures sees the
// values of its own iteration, as it would in a separate call.
func jsLoopFunction(f *Function, loop *tailLoop) string {
	var out strings.Builder
	params := []string{}
	for _, p := range f.Params {
		params = append(params, "__ef_arg_"+p.Name)
	}
	out.WriteString("(" + strings.Join(params, ", ") + ") => {\nwhile (true) {\n")
	for _, p := range f.Params {
		out.WriteString("const __ef_local_" + p.Name + " = __ef_arg_" + p.Name + ";\n")
	}
	out.WriteString(jsBlockIn(f.Body, false, loop, jsReturnBlock) + "}\n}")
	return out.String()
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
	return jsBlockIn(b, effect, nil, jsReturnBlock)
}

type jsBlockCompletion uint8

const (
	jsReturnBlock jsBlockCompletion = iota
	jsDiscardBlock
)

// jsBlockIn lowers a block. A returned block produces its final value or
// continues a planned self-tail loop. A discarded block completes the branch
// and resumes at the next statement in its enclosing block.
func jsBlockIn(b *Block, effect bool, loop *tailLoop, completion jsBlockCompletion) string {
	var out strings.Builder
	if len(b.Statements) == 0 {
		if completion == jsReturnBlock {
			return "return undefined;\n"
		}
		return ""
	}
	for i, s := range b.Statements {
		switch s.Kind {
		case "let":
			out.WriteString("const __ef_local_" + s.Name + " = " + jsExpr(s.Value, effect) + ";\n")
			if i == len(b.Statements)-1 && completion == jsReturnBlock {
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
				if completion == jsDiscardBlock {
					out.WriteString(jsStatement(s.Value, effect))
					continue
				}
				if loop.isCall(s.Value) {
					out.WriteString(jsTailCall(s.Value, loop))
					continue
				}
				if loop.onSpine(s.Value) {
					out.WriteString(jsTailBranches(s.Value, loop))
					continue
				}
				if s.Value.Kind == "if" {
					out.WriteString(jsIfStatement(s.Value, effect, func(b *Block) string {
						return jsBlockIn(b, effect, nil, jsReturnBlock)
					}))
					continue
				}
				if s.Value.Kind == "match" {
					// Give match subjects a lexical scope separate from their arm
					// bindings, so a nested match can safely reuse local subject names.
					out.WriteString("{\n" + jsMatchStatements(s.Value, effect, jsReturnBlock, func(b *Block) string {
						return jsBlockIn(b, effect, nil, jsReturnBlock)
					}) + "}\n")
					continue
				}
				out.WriteString("return ")
			} else if s.Value.Kind == "if" || s.Value.Kind == "match" {
				out.WriteString(jsStatement(s.Value, effect))
				continue
			}
			out.WriteString(jsExpr(s.Value, effect) + ";\n")
		}
	}
	return out.String()
}

// jsTailCall continues the loop with the call's arguments. Each changing
// authored argument is evaluated once, left to right, before any loop state is
// reassigned, then stored in its checked parameter slot. An unchanged simple
// parameter reference needs no temporary or reassignment.
func jsTailCall(call *Expr, loop *tailLoop) string {
	var out strings.Builder
	out.WriteString("{\n")
	next := make([]string, len(loop.function.Params))
	for sourceIndex, arg := range call.boundArguments() {
		parameter := call.argumentParameter(sourceIndex)
		if loop.unchanged(call, parameter) {
			continue
		}
		name := "__ef_next_" + loop.function.Params[parameter].Name
		out.WriteString("const " + name + " = " + jsExpr(arg, false) + ";\n")
		next[parameter] = name
	}
	for parameter, p := range loop.function.Params {
		if next[parameter] != "" {
			out.WriteString("__ef_arg_" + p.Name + " = " + next[parameter] + ";\n")
		}
	}
	out.WriteString("continue;\n}\n")
	return out.String()
}

// jsTailBranches lowers an `if` or `match` on the tail spine as statements in
// the enclosing loop body. Each branch ends in a return or a continue.
func jsTailBranches(e *Expr, loop *tailLoop) string {
	branch := func(b *Block) string { return jsBlockIn(b, false, loop, jsReturnBlock) }
	if e.Kind == "if" {
		return jsIfStatement(e, false, branch)
	}
	return "{\n" + jsMatchStatements(e, false, jsReturnBlock, branch) + "}\n"
}

// jsStatement emits control flow directly when its value is discarded. Its
// branches share the current generator/function and then continue at the next
// statement in the enclosing block.
func jsStatement(e *Expr, effect bool) string {
	branch := func(b *Block) string { return jsBlockIn(b, effect, nil, jsDiscardBlock) }
	switch e.Kind {
	case "if":
		return jsIfStatement(e, effect, branch)
	case "match":
		return "{\n" + jsMatchStatements(e, effect, jsDiscardBlock, branch) + "}\n"
	default:
		return jsExpr(e, effect) + ";\n"
	}
}
func jsExpr(e *Expr, effect bool) string {
	switch e.Kind {
	case "codec":
		return jsCodecOperation(e)
	case "intrinsic":
		return jsIntrinsicOperation(e)
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
		return normalizedI64Literal(e.Text) + "n"
	case "unary":
		if e.Name != "-" {
			panic("unchecked unary operator reached JavaScript emitter")
		}
		return "BigInt.asIntN(64, (-" + jsExpr(e.Left, effect) + "))"
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
	case "recover":
		// The handler value is evaluated with the recipe; it is invoked only
		// on the recovered failure path.
		invoke := "Effect.sync(() => handler(payload))"
		if node := e.Right.checked.node(); node != nil && node.Mode == "effect" {
			invoke = "handler(payload)"
		}
		return "__ef_recover(" + jsExpr(e.Left, effect) + ", " + quoted(e.Name) + ", ((handler) => (payload) => " + invoke + ")(" + jsExpr(e.Right, false) + "))"
	case "binary":
		op := e.Name
		if op == "==" {
			op = "==="
		}
		left, right := jsExpr(e.Left, effect), jsExpr(e.Right, effect)
		// bigint is arbitrary precision; each i64 result that can leave the
		// signed 64-bit range wraps here as Go int64 does. BigInt division and
		// remainder truncate toward zero like Go. A remainder is smaller in
		// magnitude than its divisor and needs no normalization; MIN / -1 is
		// the one quotient that does.
		if (e.Name == "-" || e.Name == "+" || e.Name == "*" || e.Name == "/") && e.Left.checked.node() != nil && e.Left.checked.node().Kind == "primitive" && e.Left.checked.node().Name == "i64" {
			return "BigInt.asIntN(64, (" + left + " " + e.Name + " " + right + "))"
		}
		return "(" + left + " " + op + " " + right + ")"
	case "if":
		body := jsIfStatement(e, effect, func(b *Block) string { return jsBlock(b, effect) })
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
	arguments := e.boundArguments()
	args := make([]string, len(arguments))
	for i, a := range arguments {
		args[i] = jsExpr(a, effect)
	}
	if e.ArgumentParameters == nil {
		return call(strings.Join(args, ", "))
	}
	names := make([]string, len(arguments))
	bound := make([]string, len(arguments))
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
	body := jsMatchStatements(e, effect, jsReturnBlock, func(b *Block) string { return jsBlock(b, effect) })
	if effect {
		return "(yield* Effect.gen(function* () {\n" + body + "}))"
	}
	return "(() => {\n" + body + "})()"
}

// jsIfStatement is the statement form of an `if`; branch lowers each block.
func jsIfStatement(e *Expr, effect bool, branch func(*Block) string) string {
	return "if (" + jsExpr(e.Left, effect) + ") {\n" + branch(e.Then) + "} else {\n" + branch(e.Else) + "}\n"
}

// jsMatchStatements lowers the checked match plan. Branch mode controls the
// arm result while the plan retains first-match order and subject evaluation.
func jsMatchStatements(e *Expr, effect bool, completion jsBlockCompletion, branch func(*Block) string) string {
	plan := e.matchPlan
	body := ""
	subjects := make([]string, len(e.Args))
	for index, subject := range e.Args {
		subjects[index] = "__ef_match_" + strconv.Itoa(index)
		body += "const " + subjects[index] + " = " + jsExpr(subject, effect) + ";\n"
	}
	if jsCanSwitchMatch(plan) {
		body += "switch (" + subjects[0] + "._tag) {\n"
		for _, arm := range plan.arms {
			cell := arm.cells[0]
			for _, pattern := range cell.alternatives {
				body += "case " + quoted(jsVariantTag(pattern)) + ":\n"
			}
			body += "{\n" + jsMatchBindings(arm, subjects)
			body += branch(arm.body)
			if completion == jsDiscardBlock {
				body += "break;\n"
			}
			body += "}\n"
		}
		body += "default: throw new Error(\"unreachable non-exhaustive match\");\n}\n"
		return body
	}
	for armIndex, arm := range plan.arms {
		conditions := []string{}
		for subjectIndex, cell := range arm.cells {
			if cell.total {
				continue
			}
			alternatives := []string{}
			for _, pattern := range cell.alternatives {
				alternatives = append(alternatives, subjects[subjectIndex]+"._tag === "+quoted(jsVariantTag(pattern)))
			}
			conditions = append(conditions, "("+strings.Join(alternatives, " || ")+")")
		}
		if len(conditions) == 0 {
			conditions = append(conditions, "true")
		}
		if armIndex == 0 {
			body += "if (" + strings.Join(conditions, " && ") + ") {\n"
		} else {
			body += "else if (" + strings.Join(conditions, " && ") + ") {\n"
		}
		body += jsMatchBindings(arm, subjects)
		body += branch(arm.body) + "}\n"
	}
	if len(plan.arms) == 0 {
		body += "throw new Error(\"unreachable non-exhaustive match\");\n"
	} else {
		body += "else { throw new Error(\"unreachable non-exhaustive match\"); }\n"
	}
	return body
}

func jsMatchBindings(arm matchPlanArm, subjects []string) string {
	body := ""
	for _, binding := range arm.bindings {
		subject := subjects[binding.subject]
		alternatives := arm.cells[binding.subject].alternatives
		value := subject + "[" + quoted(binding.fields[len(alternatives)-1]) + "]"
		for alternative := len(alternatives) - 2; alternative >= 0; alternative-- {
			value = subject + "._tag === " + quoted(jsVariantTag(alternatives[alternative])) + " ? " + subject + "[" + quoted(binding.fields[alternative]) + "] : " + value
		}
		body += "const __ef_local_" + binding.name + " = " + value + ";\n"
	}
	return body
}

// A single tested subject with explicit checked variant cells can use a tag
// switch. Product, total, or otherwise general plans stay on the ordered
// condition chain below.
func jsCanSwitchMatch(plan *matchPlan) bool {
	if plan == nil || len(plan.subjects) != 1 || !plan.subjects[0].tested || len(plan.arms) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, arm := range plan.arms {
		if len(arm.cells) != 1 || arm.cells[0].total || len(arm.cells[0].alternatives) == 0 {
			return false
		}
		for _, pattern := range arm.cells[0].alternatives {
			if pattern == nil || pattern.VariantName == "" {
				return false
			}
			tag := jsVariantTag(pattern)
			if seen[tag] {
				return false
			}
			seen[tag] = true
		}
		for _, binding := range arm.bindings {
			if binding.subject != 0 || len(binding.fields) != len(arm.cells[0].alternatives) {
				return false
			}
		}
	}
	return len(seen) == len(plan.subjects[0].variants)
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
			// Captured construction services are hidden; the operation's own
			// row parameters remain part of the contract.
			copy := *f
			copy.Services = nil
			for _, label := range f.Services {
				for _, row := range f.RowParameters {
					if row.Kind == "uses" && row.Name == label {
						copy.Services = append(copy.Services, label)
					}
				}
			}
			contract = &copy
		}
		out += "readonly " + quoted(f.Name) + ": " + jsRowFunctionSignature(program, contract, declarations) + "; "
	}
	return out + "}"
}
