package compiler

import (
	"bytes"
	"fmt"
	"go/format"
	goscanner "go/scanner"
	gotoken "go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type goEmitter struct {
	next     int
	bindings map[string]Binding
	program  *Program
	plan     *ApplicationPlan
	// locals holds the declared Go layout of each local in scope that was
	// bound from generic data; other locals have their canonical layout.
	locals map[string]goLayout
	// source is the file name //line directives give the checked user
	// source; empty, no directives are emitted. line is the source line of
	// the user declaration or statement being lowered, and 0 while lowering
	// generated glue.
	source string
	line   int
}

func goType(program *Program, t string) string {
	if strings.HasPrefix(t, "GoResult:") {
		return "er.GoResult[" + goType(program, strings.TrimPrefix(t, "GoResult:")) + "]"
	}
	if callback, ok := program.callback(t); ok {
		return "func(" + goType(program, callback.Parameter) + ") efEffect[" + goType(program, callback.Result) + "]"
	}
	switch t {
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "i64":
		return "int64"
	case "bytes":
		return "[]byte"
	case "File":
		return "*er.File"
	case "Latch":
		return "*er.Latch"
	default:
		if strings.HasPrefix(t, "provider:") {
			return "efService_" + goIdent(strings.TrimPrefix(t, "provider:"))
		}
		if strings.HasPrefix(t, "Fiber:") {
			return "*er.Fiber[" + goType(program, strings.TrimPrefix(t, "Fiber:")) + "]"
		}
		if strings.HasPrefix(t, "GoResult:") {
			return "er.GoResult[" + goType(program, strings.TrimPrefix(t, "GoResult:")) + "]"
		}
		if t == "void" || t == "never" || t == "invalid" || t == "" {
			return "struct{}"
		}
		if strings.Contains(t, ".") || (program != nil && program.semantic.hostAnnotationSpelled(t)) {
			return hostAnnotationGoName(t)
		}
		return "efType_" + goIdent(t)
	}
}

// canonicalGoType renders the actual emitted Go value represented by one
// checked semantic node. It is deliberately separate from goType: goType
// accepts legacy source/display names, while this renderer consumes the
// checker's canonical node and never feeds an already-rendered type back
// through the legacy grammar.
func canonicalGoType(c *checker, id TypeID, visiting map[TypeID]bool) string {
	return layoutGoType(c, id, nil, visiting)
}

// layoutGoType renders id as Go instantiates a generic declaration: each type
// variable bound in bindings is replaced by the rendering of its binding's
// layout. A void substituted for a declared callable result T therefore
// keeps its struct{} carrier, while a concrete void result is erased.
func layoutGoType(c *checker, id TypeID, bindings map[TypeID]goLayout, visiting map[TypeID]bool) string {
	if c == nil || id == invalidTypeID {
		return "struct{}"
	}
	if visiting[id] {
		// Recursive nominal data is represented by its declaration name and does
		// not recurse here. This guard is for malformed or future wrapper cycles.
		return "struct{}{}"
	}
	node := c.node(id)
	if node == nil {
		return "struct{}"
	}
	visiting[id] = true
	defer delete(visiting, id)
	switch node.Kind {
	case "primitive":
		switch node.Name {
		case "string":
			return "string"
		case "bool":
			return "bool"
		case "i64":
			return "int64"
		case "bytes":
			return "[]byte"
		case "void", "never", "invalid":
			return "struct{}"
		}
	case "opaque":
		if _, ok := c.program.callback(node.Name); ok {
			return goType(c.program, node.Name)
		}
		switch node.Name {
		case "File":
			return "*er.File"
		case "Latch":
			return "*er.Latch"
		}
	case "record", "enum", "error":
		return "efType_" + goIdent(node.Name)
	case "type-variable":
		if bound, ok := bindings[id]; ok {
			return bound.render(c)
		}
		return emittedTypeVariable(node)
	case "application":
		r := c.templates[node.Declaration]
		if r == nil {
			return "struct{}"
		}
		args := []string{}
		for _, id := range node.Args {
			args = append(args, layoutGoType(c, id, bindings, visiting))
		}
		return "efTemplate_" + r.EmissionName + "[" + strings.Join(args, ",") + "]"
	case "fiber":
		if len(node.Args) == 1 {
			return "*er.Fiber[" + layoutGoType(c, node.Args[0], bindings, visiting) + "]"
		}
	case "goResult":
		if len(node.Args) == 1 {
			return "er.GoResult[" + layoutGoType(c, node.Args[0], bindings, visiting) + "]"
		}
	case "goValues":
		return canonicalGoValuesType(c, node, visiting)
	case "host":
		return c.hostGoType(id)
	case "provider":
		return "efService_" + goIdent(node.Name)
	case "callable", "callable-shape":
		return layoutGoCallableType(c, node, bindings, visiting)
	case "recipe", "providerRecipe":
		return "efEffect[" + layoutGoType(c, node.Result, bindings, visiting) + "]"
	case "never", "invalid":
		return "struct{}"
	}
	return "struct{}"
}

func layoutGoCallableType(c *checker, node *semanticTypeNode, bindings map[TypeID]goLayout, visiting map[TypeID]bool) string {
	if node == nil {
		return "func() struct{}"
	}
	parameters := make([]string, 0, len(node.Args))
	for _, parameter := range node.Args {
		parameters = append(parameters, layoutGoType(c, parameter, bindings, visiting))
	}
	result := layoutGoType(c, node.Result, bindings, visiting)
	if node.Mode == "effect" {
		result = "efEffect[" + result + "]"
	} else if canonicalVoidType(c, node.Result) {
		return "func(" + strings.Join(parameters, ", ") + ")"
	}
	return "func(" + strings.Join(parameters, ", ") + ") " + result
}

func canonicalVoidType(c *checker, id TypeID) bool {
	node := c.node(id)
	return node != nil && node.Kind == "primitive" && node.Name == voidTypeName
}

// canonicalValueType renders the value held by an expression. A callable is
// therefore rendered as a function value, a recipe as an efEffect, and a
// Fiber as a Fiber handle. Its resultType counterpart is used only at an
// explicit elimination boundary such as run, join, or a foreign invocation.
func (g *goEmitter) canonicalValueType(e checkedExpression) string {
	if e.value.arena == nil {
		return "struct{}"
	}
	return canonicalGoType(e.value.arena.checker, e.value.valueID(), map[TypeID]bool{})
}

func (g *goEmitter) canonicalResultType(e checkedExpression) string {
	if e.value.arena == nil {
		return "struct{}"
	}
	return canonicalGoType(e.value.arena.checker, e.resultID(), map[TypeID]bool{})
}

func (g *goEmitter) valueType(e *Expr) string {
	return g.canonicalValueType(e.checked)
}

func (g *goEmitter) resultType(e *Expr) string {
	return g.canonicalResultType(e.checked)
}
func goIdent(name string) string {
	name = strings.ReplaceAll(strings.ReplaceAll(name, ".", "_"), ":", "_")
	if name == "" {
		return "Anonymous"
	}
	return name
}
func goFieldName(name string) string {
	// Prefix every source field with its byte length and a generated namespace.
	// Initial-capital lowering alone maps names such as `id` and `Id` to the
	// same Go selector, and `_` would become Go's blank field. Source names are
	// ASCII lexer identifiers, so the length prefix plus goIdent is injective.
	return "EfField_" + strconv.Itoa(len(name)) + "_" + goIdent(name)
}
func goVariantType(typeName, variantName string) string {
	// Encode both qualified components with their source lengths. A plain
	// concatenation makes enum AB.C collide with enum A.BC (and names that
	// contain underscores), which would make otherwise checked programs fail
	// during Go compilation.
	return "efTypeV_" + strconv.Itoa(len(typeName)) + "_" + goIdent(typeName) + "_" + strconv.Itoa(len(variantName)) + "_" + goIdent(variantName)
}
func goParams(program *Program, f *Function) string {
	return goParamsList(program, f.Params)
}
func goParamsList(program *Program, params []Param) string {
	parts := []string{}
	for _, p := range params {
		parts = append(parts, "efLocal_"+p.Name+" "+goSourceType(program, p.sourceType, p.Type))
	}
	return strings.Join(parts, ", ")
}
func goArgs(f *Function) string {
	parts := []string{}
	for _, p := range f.Params {
		parts = append(parts, "efLocal_"+p.Name)
	}
	return strings.Join(parts, ", ")
}
func goMethodType(program *Program, f *Function) string {
	types := []string{}
	for _, p := range f.Params {
		types = append(types, goSourceType(program, p.sourceType, p.Type))
	}
	return "func(" + strings.Join(types, ", ") + ") efEffect[" + goSourceType(program, f.returnType, f.Return) + "]"
}
func (g *goEmitter) temp() string { g.next++; return fmt.Sprintf("efTemp%d", g.next) }

// GoApplication is one concrete native entry mode lowered against its
// application plan. The generated main and the runtime source selection come
// from the same plan, so every emitted declaration resolves within the
// selected runtime and nothing outside the plan is emitted.
type GoApplication struct {
	Plan   *ApplicationPlan
	Main   []byte
	result *Result
}

// GoApplication plans and lowers one native entry mode of a source that has
// no file. Its generated main carries no source positions; GoApplicationFrom
// names the file. Plan refusals, including EF136 exhaustion, are returned
// unchanged and nothing is emitted.
func (r *Result) GoApplication(mode GoGenerationMode) (*GoApplication, error) {
	return r.goApplication(mode, "")
}

// GoApplicationFrom is GoApplication for the source read from origin. The
// generated main carries //line directives naming origin's base name, so Go's
// diagnostics, stack traces and profiles report user code at its .ef lines.
// Only the base name is emitted: Go resolves a relative directive against
// the generated module, which -trimpath reports as effra.generated/<name>,
// so the same source produces the same bytes from any checkout.
func (r *Result) GoApplicationFrom(mode GoGenerationMode, origin string) (*GoApplication, error) {
	name := filepath.Base(origin)
	if origin == "" || name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, "\n\r") {
		return nil, fmt.Errorf("Go source origin %q names no file", origin)
	}
	// Go's directive grammar has no escape: it reads the text after a file
	// name's last colon as a line number whenever it parses as one
	// (trailingDigits in cmd/compile/internal/syntax and go/scanner).
	if colon := strings.LastIndexByte(name, ':'); colon >= 0 {
		if _, err := strconv.ParseUint(name[colon+1:], 10, 0); err == nil {
			return nil, fmt.Errorf("Go source origin %q ends in :<number>, which a //line directive would read as a position", origin)
		}
	}
	return r.goApplication(mode, name)
}

func (r *Result) goApplication(mode GoGenerationMode, source string) (*GoApplication, error) {
	plan, err := r.ApplicationPlan(mode)
	if err != nil {
		return nil, err
	}
	main, err := r.emitGo(plan, source)
	if err != nil {
		return nil, err
	}
	return &GoApplication{Plan: plan, Main: []byte(main), result: r}, nil
}

// RuntimeSources returns a fresh copy of the runtime sources the plan selects.
func (a *GoApplication) RuntimeSources() (map[string][]byte, error) {
	if a == nil || a.Plan == nil {
		return nil, fmt.Errorf("Go application has no plan")
	}
	return a.Plan.RuntimeSources()
}

// EmitGo lowers checked IR to a standalone Go program using the managed runtime.
// Error and requirement rows are checked in the frontend; success values stay typed in Go.
func (r *Result) EmitGo() (string, error) {
	application, err := r.GoApplication(GoGenerationBuild)
	if err != nil {
		return "", err
	}
	return string(application.Main), nil
}

// goEmissionHelpers are the lowering helpers emitted only when the plan
// retains the expression kind that calls them.
var goEmissionHelpers = []struct{ name, source string }{
	{"catch", "func efCatch[A any](program efEffect[A],tag string,fallback func()A)efEffect[A]{return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,er.Catch(efToRuntime(ctx,program),tag,fallback))}}\n"},
	{"recover", "func efRecover[A,P any](program efEffect[A],tag string,handler func(P)efEffect[A])efEffect[A]{return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,er.Recover(efToRuntime(ctx,program),tag,func(payload P)er.Effect[A]{return efToRuntime(ctx,handler(payload))}))}}\n"},
	{"scope", "func efScoped[A any](program efEffect[A])efEffect[A]{return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,er.Scoped(efToRuntime(ctx,program)))}}\n"},
	{"timeout", "func efTimeout[A any](program efEffect[A],duration int64)efEffect[A]{return func(ctx efContext)efExit[A]{if ctx.s_Scheduler==nil||ctx.s_Scheduler.m_sleep==nil{return er.Die[A](fmt.Errorf(\"missing provider Scheduler.sleep\"))};deadline:=ctx.s_Scheduler.m_sleep(duration);return er.Invoke(ctx.Runtime,er.TimeoutWithEffect(efToRuntime(ctx,program),efToRuntime(ctx,deadline)))}}\n"},
	{"fork", "func efFork[A any](program efEffect[A])efEffect[*er.Fiber[A]]{return func(ctx efContext)efExit[*er.Fiber[A]]{return er.Invoke(ctx.Runtime,er.Fork(efToRuntime(ctx,program)))}}\n"},
	{"fiber.join", "func efJoin[A any](fiber *er.Fiber[A])efEffect[A]{return efFromRuntime(fiber.Join())}\n"},
	{"fiber.interrupt", "func efInterrupt[A any](fiber *er.Fiber[A])efEffect[struct{}]{return efFromRuntime(fiber.Interrupt())}\n"},
	{codecLoweringHelper, goCodecHelpers},
	{endpointLoweringHelper, goEndpointHelpers()},
	{"fiber.cancel", "func efCancel[A any](fiber *er.Fiber[A])efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{fiber.Cancel();return er.Succeed(struct{}{})})}\n"},
}

// builtinGoProviders are the native implementations of builtin providers.
// Each is emitted only when the plan retains that provider, and each names
// the runtime modules it references in builtinProviders.
var builtinGoProviders = map[string]string{
	"Assertions":    "func efProvider_Assertions()efService_Assert{return efService_Assert{m_check:func(condition bool,message string)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{if !condition{return er.Fail[struct{}](\"AssertionFailed\",message)};return er.Succeed(struct{}{})})},m_equalText:func(actual,expected string)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{if actual!=expected{return er.Fail[struct{}](\"AssertionFailed\",\"expected \"+er.QuoteText(expected)+\"; received \"+er.QuoteText(actual))};return er.Succeed(struct{}{})})}}}\n",
	"Stdout":        "func efProvider_Stdout()efService_Console{return efService_Console{m_log:func(message string)efEffect[struct{}]{return efFromRuntime(er.Println(message))}}}\n",
	"LiveClock":     "func efProvider_LiveClock()efService_Clock{return efService_Clock{m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(er.SleepWithDriver(nil,ms))}}}\n",
	"TestClock":     "func efProvider_TestClock(scheduler *er.TestScheduler)efService_Clock{return efService_Clock{driver:scheduler,m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf(\"test clock requires the ef test harness\"))};return er.Invoke(fc,er.SleepWithDriver(scheduler,ms))})}}}\n",
	"LiveScheduler": "func efProvider_LiveScheduler()efService_Scheduler{return efService_Scheduler{m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(er.SleepWithDriver(nil,ms))},m_advance:func(int64)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{return er.Die[struct{}](fmt.Errorf(\"live scheduler cannot advance\"))})},m_awaitRegistration:func()efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{return er.Die[struct{}](fmt.Errorf(\"live scheduler has no registration barrier\"))})}}}\n",
	"TestScheduler": "func efProvider_TestScheduler(scheduler *er.TestScheduler)efService_Scheduler{return efService_Scheduler{driver:scheduler,m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf(\"test scheduler requires the ef test harness\"))};return er.Invoke(fc,er.SleepWithDriver(scheduler,ms))})},m_advance:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf(\"test scheduler adjustment is only available in the ef test harness\"))};return er.AdjustTestScheduler(fc,scheduler,ms)})},m_awaitRegistration:func()efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf(\"test scheduler registration barrier is only available in the ef test harness\"))};return er.AwaitTestSchedulerRegistration(fc,scheduler)})}}}\n",
	"TestSync":      "func efProvider_TestSync()efService_Sync{return efService_Sync{m_latch:func()efEffect[*er.Latch]{return efFromRuntime(func(*er.FiberContext)er.Exit[*er.Latch]{return er.Succeed(er.NewLatch())})},m_await:func(latch *er.Latch)efEffect[struct{}]{return efFromRuntime(er.AwaitLatch(latch))},m_signal:func(latch *er.Latch)efEffect[struct{}]{return efFromRuntime(er.SignalLatch(latch))}}}\n",
	"LiveFiles":     "func efProvider_LiveFiles()efService_Files{return efService_Files{m_openRead:func(path string)efEffect[*er.File]{return efFromRuntime(er.OpenRead(path))},m_readText:func(file *er.File)efEffect[string]{return efFromRuntime(er.ReadText(file))},m_readFile:func(path string)efEffect[string]{return efFromRuntime(er.ReadFile(path))}}}\n",
	"LiveEnv":       "func efProvider_LiveEnv()efService_Env{return efService_Env{m_get:func(name string)efEffect[string]{return efFromRuntime(er.Env(name))}}}\n",
	"RuntimeLive":   "func efProvider_RuntimeLive()efService_Runtime{return efService_Runtime{m_inspect:func()efEffect[string]{return efFromRuntime(er.InspectScope())}}}\n",
	"Host":          "func efProvider_Host()efService_Foreign{return efService_Foreign{}}\n",
	"LiveHttp":      goHTTPProvider(),
}

// schedulerDrivenProvider reports whether a builtin provider is constructed
// from the active test scheduler rather than with no arguments.
func schedulerDrivenProvider(name string) bool {
	return name == "TestClock" || name == "TestScheduler"
}

// goServiceDeclaration emits one service's struct, provision wrapper and
// operation call wrappers.
func goServiceDeclaration(program *Program, s *Service) string {
	var out strings.Builder
	out.WriteString("type efService_" + s.Name + " struct {\n")
	if s.Name == "Clock" || s.Name == "Scheduler" {
		out.WriteString("driver *er.TestScheduler\n")
	}
	for _, m := range s.Methods {
		out.WriteString("m_" + m.Name + " " + goMethodType(program, m) + "\n")
	}
	out.WriteString("}\n")
	out.WriteString("func efProvide_" + s.Name + "[A any](program efEffect[A], provider efService_" + s.Name + ") efEffect[A] { return func(ctx efContext) efExit[A] {")
	out.WriteString("ctx.s_" + s.Name + " = &provider; return program(ctx) } }\n")
	for _, m := range s.Methods {
		result := goSourceType(program, m.returnType, m.Return)
		out.WriteString("func efCall_" + s.Name + "_" + m.Name + "(" + goParams(program, m) + ") efEffect[" + result + "] { return func(ctx efContext) efExit[" + result + "] {\n")
		out.WriteString("if ctx.s_" + s.Name + " == nil || ctx.s_" + s.Name + ".m_" + m.Name + " == nil { return efExit[" + result + "]{Defect:fmt.Errorf(" + strconv.Quote("missing provider "+s.Name+"."+m.Name) + ")} }\n")
		out.WriteString("return ctx.s_" + s.Name + ".m_" + m.Name + "(" + goArgs(m) + ")(ctx)\n} }\n")
	}
	return out.String()
}

// emitGo lowers exactly the declarations plan retains. Selection is by
// checked identity: the emitter never decides reachability itself. A
// non-empty source names the user source in //line directives.
func (r *Result) emitGo(plan *ApplicationPlan, source string) (string, error) {
	marked, err := r.emitMarkedGo(plan, source)
	if err != nil {
		return "", err
	}
	if source == "" {
		return string(marked), nil
	}
	return placeLineDirectives(marked)
}

// emitMarkedGo is the formatted lowering before line directives are placed.
// With a named source, a mark line gives the source position of the code
// after it, up to the next mark: //line <source>:<line> for a user
// declaration or statement, goLineReset for generated glue.
func (r *Result) emitMarkedGo(plan *ApplicationPlan, source string) ([]byte, error) {
	if plan == nil || plan.Revision != r.Revision || plan.Target != r.Target {
		return nil, fmt.Errorf("Go emission requires this result's application plan")
	}
	var tests []*Symbol
	if plan.Mode == GoGenerationTest {
		var err error
		if tests, err = r.Tests(); err != nil {
			return nil, err
		}
	}
	g := &goEmitter{program: r.Program, plan: plan, source: source}
	var out strings.Builder
	out.WriteString("// Generated by the Effra prototype. Source revision: " + r.Revision + "\npackage main\nimport (\"fmt\"; \"os\"; \"context\"; \"os/signal\"; \"syscall\"; er " + strconv.Quote(generatedModulePath+"/runtime") + "\n")
	if tests != nil {
		out.WriteString("\"encoding/json\"\n")
	}
	if plan.requiresStrconv() {
		out.WriteString("\"strconv\"\n")
	}
	// The plan decides each declared package's import form: the named import
	// of every alias retained code qualifies, which also initializes its
	// package, or one blank import for initialization alone.
	for _, imp := range r.Program.Imports {
		if plan.includesGoImport(imp.Alias) {
			out.WriteString("efGo_" + imp.Alias + " " + strconv.Quote(imp.Path) + "\n")
		}
	}
	for _, path := range plan.Identities(RequiresGoInitialization) {
		if plan.goImportLowering(path) == GoImportBlank {
			out.WriteString("_ " + strconv.Quote(path) + "\n")
		}
	}
	hostImports, hostDeclarations := r.Program.semantic.hostGoDeclarations()
	for _, imp := range hostImports {
		out.WriteString(imp + "\n")
	}
	out.WriteString(")\n" + hostDeclarations)
	g.bindings = r.Program.Bindings
	out.WriteString(`
type efExit[A any] = er.Exit[A]
type efEffect[A any] func(efContext) efExit[A]
func efToRuntime[A any](ctx efContext,program efEffect[A]) er.Effect[A] {return func(fc *er.FiberContext) er.Exit[A] {ctx.Runtime=fc;return program(ctx)}}
func efFromRuntime[A any](program er.Effect[A]) efEffect[A] {return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,program)}}
`)
	for _, helper := range append(append([]struct{ name, source string }{}, goEmissionHelpers...), goI64TextHelpers...) {
		if plan.Requires(RequiresHelper, helper.name) {
			out.WriteString(helper.source)
		}
	}
	g.dataTypes(&out)
	out.WriteString(g.codecPlans(r.CodecPlans))
	services := []*Service{}
	for _, s := range append(builtinServicesFor(r.Program), r.Program.Services...) {
		if plan.Requires(RequiresService, serviceIdentity(s.Name)) {
			services = append(services, s)
		}
	}
	out.WriteString("type efContext struct {\nRuntime *er.FiberContext\n")
	for _, s := range services {
		out.WriteString("s_" + s.Name + " *efService_" + s.Name + "\n")
	}
	out.WriteString("}\n")
	for _, s := range services {
		out.WriteString(goServiceDeclaration(r.Program, s))
	}
	for _, p := range builtinProvidersFor(r.Program) {
		if plan.Requires(RequiresProvider, providerTypeRef(p).Declaration) {
			out.WriteString(builtinGoProviders[p.Name])
		}
	}
	for _, p := range r.Program.Providers {
		if !plan.Requires(RequiresProvider, providerTypeRef(p).Declaration) {
			continue
		}
		if providerConstructed(p) {
			out.WriteString(g.sourced(p.Span.Line, func() string { return g.providerConstructor(p) }))
			continue
		}
		out.WriteString("func efProvider_" + p.Name + "() efService_" + p.Service + " { return efService_" + p.Service + "{\n")
		for _, m := range p.Methods {
			out.WriteString(g.sourced(m.Span.Line, func() string { return "m_" + m.Name + ": " + strings.TrimSpace(g.function(m)) + ",\n" }))
		}
		out.WriteString("} }\n")
	}
	for _, layer := range r.Layers {
		if plan.Requires(RequiresLayer, layer.ID) {
			out.WriteString(g.sourced(layer.Span.Line, func() string { return g.layer(layer) }))
		}
	}
	for _, f := range r.Program.checkedFunctions() {
		if !plan.Requires(RequiresFunction, f.Identity) {
			continue
		}
		line := 0
		// A bundled declaration's span is a line of its bundled source.
		if f.SourceID == userSourceID {
			line = f.Span.Line
		}
		out.WriteString(g.sourced(line, func() string { return g.functionDeclaration(f) }))
	}
	if tests == nil {
		mainReturn := voidTypeName
		for _, function := range r.Program.Functions {
			if function.Name == "main" {
				mainReturn = function.Return
				break
			}
		}
		out.WriteString(r.goEntryReport(func(tag string) bool {
			return plan.Requires(RequiresDeclaration, r.Program.semantic.declarationIdentity("error", "module", tag))
		}))
		out.WriteString("func main() { base,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();exit:=er.RunContext(base,func(fc *er.FiberContext) er.Exit[" + goSourceType(r.Program, r.Program.typeExpressions[mainReturn], mainReturn) + "]{return efFunction_main()(efContext{Runtime:fc})});if exit.IsFailure(){cause:=exit.Cause();fmt.Fprint(os.Stderr,efEntryReport.Report(cause));os.Exit(er.EntryExitCode(cause))}\n")
		if mainReturn != voidTypeName {
			out.WriteString("fmt.Println(exit.Value)\n")
		}
		out.WriteString("}\n")
	} else {
		out.WriteString(r.goTestHarness(tests))
	}
	formatted, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("Go lowering generated invalid syntax: %w", err)
	}
	return formatted, nil
}

// goPosition is a file and line Go attributes generated code to. The zero
// position is generated glue, which keeps its own line in main.go.
type goPosition struct {
	file string
	line int
}

// placeLineDirectives replaces the marks of a marked lowering with the
// //line directives Go reads, so that every compiled token gets the position
// its mark gives it: a user source line, or its own main.go line for glue.
//
// A directive positions only the line after it, and every later line counts
// on from there, so one directive per statement would attribute the second
// Go line of a statement to the next source line. Every line lowered from a
// user statement therefore gets its own directive. A line of glue gets one
// only where it follows user code, naming its true position in main.go, so
// the file's own positions hold everywhere else. Directives are whole lines
// at column 1, as Go requires, and never start inside a multi-line raw string
// or comment. Only comment lines, and the blank line gofmt keeps before a
// top-level declaration's comment, are added: the program Go compiles is the
// unnamed lowering's.
//
// The intended position belongs to each compiled token, not to a line:
// markedPositions reads it once from the marks, and every placement derives
// the directives from those positions alone, never from an earlier
// placement's directives. The result is formatted again so it stays
// gofmt-formatted. Comments can make gofmt split a line it kept whole; the
// next placement gives the new line its own directive. The result is the
// first formatted program that placement leaves unchanged, so its directives
// are exactly the ones its tokens' positions require.
func placeLineDirectives(marked []byte) (string, error) {
	positions, err := markedPositions(marked)
	if err != nil {
		return "", err
	}
	program := marked
	for attempt := 0; attempt < 4; attempt++ {
		placed, err := placeLineDirectivesOnce(program, positions)
		if err != nil {
			return "", err
		}
		if bytes.Equal(placed, program) {
			return string(program), nil
		}
		if program, err = format.Source(placed); err != nil {
			return "", fmt.Errorf("Go lowering placed line directives that do not format: %w", err)
		}
	}
	return "", fmt.Errorf("Go lowering placed line directives that formatting keeps moving")
}

// lineMark reads a mark or a placed directive: a comment line that is
// goLineReset or //line <file>:<line>.
func lineMark(literal string) (goPosition, bool, error) {
	if literal == goLineReset {
		return goPosition{}, true, nil
	}
	text, found := strings.CutPrefix(literal, "//line ")
	if !found {
		return goPosition{}, false, nil
	}
	colon := strings.LastIndexByte(text, ':')
	line, err := strconv.Atoi(text[colon+1:])
	if colon <= 0 || err != nil || line <= 0 {
		return goPosition{}, false, fmt.Errorf("Go lowering emitted malformed line mark %q", literal)
	}
	return goPosition{text[:colon], line}, true, nil
}

// markedPositions lists the intended position of every compiled token of a
// marked lowering, in order: the position of the mark before it.
func markedPositions(marked []byte) ([]goPosition, error) {
	positions, current := []goPosition{}, goPosition{}
	err := scanGoTokens(marked, func(_, _ int, token gotoken.Token, literal string) error {
		if token == gotoken.COMMENT {
			mark, ok, err := lineMark(literal)
			if ok {
				current = mark
			}
			return err
		}
		if token != gotoken.SEMICOLON {
			positions = append(positions, current)
		}
		return nil
	})
	return positions, err
}

// placeLineDirectivesOnce drops the marks or directives of a program and
// places the directives that give its compiled tokens their positions, the
// i-th token taking positions[i]. Every token that starts on one line must
// share a position, since a directive positions whole lines; a token on a
// line that starts inside a multi-line string or comment, where no directive
// can go, must already have the position Go counts for it.
func placeLineDirectivesOnce(program []byte, positions []goPosition) ([]byte, error) {
	marks, interior, wanted := map[int]bool{}, map[int]bool{}, map[int]goPosition{}
	code, tokens := map[int]bool{}, 0
	err := scanGoTokens(program, func(start, end int, token gotoken.Token, literal string) error {
		for line := start + 1; line <= end; line++ {
			interior[line] = true
		}
		if token == gotoken.COMMENT {
			_, ok, err := lineMark(literal)
			marks[start] = marks[start] || ok
			return err
		}
		if token == gotoken.SEMICOLON {
			return nil
		}
		if tokens >= len(positions) {
			return fmt.Errorf("Go lowering formatted into more tokens than it marked")
		}
		position := positions[tokens]
		tokens++
		if previous, ok := wanted[start]; ok && previous != position {
			return fmt.Errorf("Go lowering put code of %s:%d and %s:%d on one line", previous.file, previous.line, position.file, position.line)
		}
		wanted[start], code[start] = position, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if tokens != len(positions) {
		return nil, fmt.Errorf("Go lowering formatted into %d tokens, marked %d", tokens, len(positions))
	}
	var out bytes.Buffer
	out.Grow(len(program) * 2)
	// written counts output lines and blank tells whether the last was
	// empty; effective is the position Go assigns to the next line.
	written, blank, effective := 0, true, goPosition{goMainFile, 1}
	for index, line := range strings.SplitAfter(string(program), "\n") {
		number := index + 1
		if marks[number] {
			if code[number] || interior[number] {
				return nil, fmt.Errorf("Go lowering placed line mark %q beside code on line %d", strings.TrimSpace(line), number)
			}
			continue
		}
		if position, ok := wanted[number]; ok {
			glue := position == goPosition{}
			if glue {
				position = goPosition{goMainFile, written + 1}
			}
			switch {
			case effective == position:
			case interior[number]:
				return nil, fmt.Errorf("Go lowering put code of %s:%d where Go counts %s:%d", position.file, position.line, effective.file, effective.line)
			default:
				// A comment directly above a top-level declaration is its doc
				// comment, which gofmt separates from the code before it.
				if !blank && topLevelDeclaration(line) {
					out.WriteString("\n")
					written++
					effective.line++
				}
				if glue {
					// The directive takes the line glue would have had.
					position = goPosition{goMainFile, written + 2}
				}
				out.WriteString("//line " + position.file + ":" + strconv.Itoa(position.line) + "\n")
				written++
				effective = position
			}
		}
		out.WriteString(line)
		if line != "" {
			written++
			effective.line++
			blank = strings.TrimSpace(line) == ""
		}
	}
	return out.Bytes(), nil
}

// topLevelDeclaration reports whether a formatted line starts a top-level
// declaration.
func topLevelDeclaration(line string) bool {
	for _, keyword := range []string{"func ", "type ", "var ", "const ", "import "} {
		if strings.HasPrefix(line, keyword) {
			return true
		}
	}
	return false
}

// scanGoTokens calls visit with every token of src, comments included, and
// the lines on which it starts and ends.
func scanGoTokens(src []byte, visit func(start, end int, token gotoken.Token, literal string) error) error {
	file := gotoken.NewFileSet().AddFile(goMainFile, -1, len(src))
	var scanner goscanner.Scanner
	failed := error(nil)
	scanner.Init(file, src, func(position gotoken.Position, message string) {
		if failed == nil {
			failed = fmt.Errorf("formatted Go does not scan at %s: %s", position, message)
		}
	}, goscanner.ScanComments)
	for failed == nil {
		at, token, literal := scanner.Scan()
		if token == gotoken.EOF {
			break
		}
		// Unadjusted: the scanner itself applies the line directives it reads.
		start := file.PositionFor(at, false).Line
		end := start
		if token == gotoken.STRING || token == gotoken.COMMENT {
			end += strings.Count(literal, "\n")
		}
		if err := visit(start, end, token, literal); err != nil {
			return err
		}
	}
	return failed
}

// goTestHarness binds every testHarnessProviders fixture into each case's
// context: the same fixtures the test plan roots. Each case gets a fresh
// test scheduler, and scheduler-driven fixtures are constructed from it.
func (r *Result) goTestHarness(tests []*Symbol) string {
	var fixtures, bindings strings.Builder
	for index, name := range testHarnessProviders {
		provider := r.checkedProviders[name]
		local := "efFixture" + strconv.Itoa(index)
		argument := ""
		if schedulerDrivenProvider(name) {
			argument = "scheduler"
		}
		fixtures.WriteString(local + ":=efProvider_" + name + "(" + argument + ");")
		bindings.WriteString(",s_" + provider.Service + ":&" + local)
	}
	var out strings.Builder
	out.WriteString("func main(){base,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();type testResult struct{Name string " + "`json:\"name\"`" + ";Passed bool " + "`json:\"passed\"`" + ";Cause string " + "`json:\"cause,omitempty\"`" + ";Reasons []map[string]string " + "`json:\"reasons,omitempty\"`" + "};results:=[]testResult{};passed:=true;")
	for _, test := range tests {
		out.WriteString("{scheduler:=er.NewTestScheduler();" + fixtures.String() + "exit:=er.RunContextWithScheduler(base,scheduler,func(fc *er.FiberContext)er.Exit[struct{}]{return efFunction_" + test.Name + "()(efContext{Runtime:fc" + bindings.String() + "})});item:=testResult{Name:" + strconv.Quote(test.Name) + ",Passed:!exit.IsFailure()};if exit.IsFailure(){item.Cause=fmt.Sprint(exit.Cause());for _,reason:=range exit.Cause(){detail:=map[string]string{\"kind\":reason.Kind};if reason.Failure!=nil{detail[\"tag\"]=reason.Failure.Tag;if reason.Failure.Payload!=nil{detail[\"message\"]=fmt.Sprint(reason.Failure.Payload)}}else if reason.Err!=nil{detail[\"message\"]=reason.Err.Error()};item.Reasons=append(item.Reasons,detail)};passed=false};results=append(results,item)}\n")
	}
	out.WriteString(`json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion":1,"passed":passed,"tests":results});if !passed{os.Exit(1)}}`)
	return out.String()
}
func (g *goEmitter) function(f *Function) string {
	bodyRet, noResult := g.functionReturn(f)
	ret := bodyRet
	if f.Effect {
		ret = "efEffect[" + ret + "]"
	}
	open := "func(" + goParams(g.program, f) + ")"
	if !noResult {
		open += " " + ret
	}
	open += " {\n"
	close := "}\n"
	if f.Effect {
		open += "return func(ctx efContext) efExit[" + goSourceType(g.program, f.returnType, f.Return) + "] {\nif err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + goSourceType(g.program, f.returnType, f.Return) + "](err)}\n"
		close = "}\n}\n"
	}
	return open + g.blockType(f.Body, f.Effect, bodyRet, noResult) + close
}

func (g *goEmitter) functionDeclaration(f *Function) string {
	c := g.program.semantic
	ret, noResult := g.functionReturn(f)
	canonical := len(f.TypeParameters) > 0 || (f.returnType != nil && f.returnType.Application != "")
	if canonical {
		ret = canonicalGoType(c, f.returnID, map[TypeID]bool{})
		noResult = !f.Effect && canonicalVoidType(c, f.returnID)
	}
	loop := lowerTail(f)
	parameterPrefix := "efLocal_"
	if loop != nil {
		parameterPrefix = "efArg_"
	}
	params := []string{}
	for _, p := range f.Params {
		typ := goSourceType(g.program, p.sourceType, p.Type)
		if len(f.TypeParameters) > 0 || (p.sourceType != nil && p.sourceType.Application != "") {
			typ = canonicalGoType(c, p.typeID, map[TypeID]bool{})
		}
		params = append(params, parameterPrefix+p.Name+" "+typ)
	}
	variables := []string{}
	for _, p := range f.TypeParameters {
		variables = append(variables, goIdent(p.Name)+" any")
	}
	generic := ""
	if len(variables) > 0 {
		generic = "[" + strings.Join(variables, ",") + "]"
	}
	valueRet := ret
	if f.Effect {
		valueRet = "efEffect[" + ret + "]"
	}
	open := "func " + f.goEmissionName() + generic + "(" + strings.Join(params, ",") + ")"
	if f.Effect || !noResult {
		open += " " + valueRet
	}
	open += " {\n"
	close := "}\n"
	if f.Effect {
		open += "return func(ctx efContext) efExit[" + ret + "] {\nif err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + ret + "](err)}\n"
		close = "}\n" + close
	}
	if loop != nil {
		return open + g.loopBody(f, loop, ret, noResult) + close
	}
	return open + g.blockType(f.Body, f.Effect, ret, noResult) + close
}

// loopBody lowers a pure function with self tail calls to a `for` loop. The
// parameters are only the loop's carried state (efArg_*); each iteration
// declares its own efLocal_* from them, so anything the body captures sees
// the values of its own iteration, as it would in a separate call. Every path
// through the body returns or continues; a `for` without a break ends the
// function.
func (g *goEmitter) loopBody(f *Function, loop *tailLoop, ret string, noResult bool) string {
	var out strings.Builder
	out.WriteString("for {\n")
	for _, p := range f.Params {
		out.WriteString("efLocal_" + p.Name + " := efArg_" + p.Name + "\n_ = efLocal_" + p.Name + "\n")
	}
	out.WriteString(g.blockTail(f.Body, false, ret, noResult, &goTail{loop: loop}))
	out.WriteString("}\n")
	return out.String()
}

// goTail marks a block as being in tail position of a loop function.
type goTail struct{ loop *tailLoop }

// tailCall continues the loop with the call's arguments. Each changing
// authored argument is evaluated once, left to right, into a temporary before
// any loop state is reassigned, then stored in its checked parameter slot. An
// unchanged simple parameter reference needs no temporary or reassignment.
func (g *goEmitter) tailCall(call *Expr, tail *goTail, out *strings.Builder) {
	f := tail.loop.function
	bindings := g.inferredTypeBindings(f, call.parameterArguments())
	temps := make([]string, len(f.Params))
	for sourceIndex, arg := range call.boundArguments() {
		parameter := call.argumentParameter(sourceIndex)
		if tail.loop.unchanged(call, parameter) {
			continue
		}
		value := g.exprAt(arg, goLayout{id: f.Params[parameter].typeID, bindings: bindings}, false, "", out)
		temps[parameter] = g.temp()
		out.WriteString(temps[parameter] + " := " + value + "\n")
	}
	for parameter, p := range f.Params {
		if temps[parameter] != "" {
			out.WriteString("efArg_" + p.Name + " = " + temps[parameter] + "\n")
		}
	}
	out.WriteString("continue\n")
}

// tailBranches lowers an `if` or `match` on the tail spine as statements in
// the enclosing loop body.
func (g *goEmitter) tailBranches(e *Expr, ret string, noResult bool, tail *goTail, out *strings.Builder) {
	if e.Kind == "if" {
		condition := g.expr(e.Left, false, ret, out)
		out.WriteString("if " + condition + " {\n" + g.blockTail(e.Then, false, ret, noResult, tail) + "} else {\n" + g.blockTail(e.Else, false, ret, noResult, tail) + "}\n")
		return
	}
	subjects := g.matchSubjects(e, false, ret, out)
	out.WriteString(g.matchSwitch(e, subjects, false, ret, noResult, tail))
	out.WriteString("default: panic(\"unreachable non-exhaustive match\")\n}\n")
}

// functionReturn renders a function's success type and reports whether its Go
// signature has no result: only a pure function whose result is concretely
// void. An effect function always returns its recipe.
func (g *goEmitter) functionReturn(f *Function) (string, bool) {
	ret, voidResult := goSourceTypeMode(g.program, f.returnType, f.Return)
	if f.returnID != invalidTypeID && g.program.semantic != nil {
		voidResult = canonicalVoidType(g.program.semantic, f.returnID)
	}
	return ret, !f.Effect && voidResult
}

// providerConstructed distinguishes an ordinary reusable provider value from
// a constructor recipe. A configured or dependent provider is always built at
// effect execution so its service values are captured at that boundary.
func providerConstructed(p *Provider) bool {
	return len(p.Params) > 0 || len(p.Services) > 0
}

func (g *goEmitter) providerConstructor(p *Provider) string {
	ret := goType(g.program, "provider:"+p.Service)
	var out strings.Builder
	out.WriteString("func efProvider_" + p.Name + "(" + goParamsList(g.program, p.Params) + ") efEffect[" + ret + "] {\n")
	out.WriteString("return func(ctx efContext) efExit[" + ret + "] {\n")
	out.WriteString("if err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + ret + "](err)}\n")
	for _, service := range normalized(p.Services) {
		out.WriteString("if ctx.s_" + service + " == nil { return efExit[" + ret + "]{Defect:fmt.Errorf(" + strconv.Quote("missing provider "+service+" for constructor "+p.Name) + ")} }\n")
		out.WriteString("efCaptured_" + service + " := ctx.s_" + service + "\n")
	}
	out.WriteString("return efExit[" + ret + "]{Value:efService_" + p.Service + "{\n")
	for _, method := range p.Methods {
		out.WriteString(g.sourced(method.Span.Line, func() string {
			implementation := g.function(method)
			if len(p.Services) > 0 {
				implementation = g.providerMethod(method, p.Services)
			}
			return "m_" + method.Name + ":" + strings.TrimSpace(implementation) + ",\n"
		}))
	}
	out.WriteString("}}\n}\n}\n")
	return out.String()
}

// providerMethod overlays only the captured dependency pointers onto the
// invocation context. The Runtime pointer (and therefore cancellation,
// scope, owner and fiber state) is copied from the current invocation.
func (g *goEmitter) providerMethod(f *Function, captures []string) string {
	ret := goSourceType(g.program, f.returnType, f.Return)
	var out strings.Builder
	out.WriteString("func(" + goParams(g.program, f) + ") efEffect[" + ret + "] {\nreturn func(ctx efContext) efExit[" + ret + "] {\n")
	for _, service := range normalized(captures) {
		out.WriteString("ctx.s_" + service + " = efCaptured_" + service + "\n")
	}
	out.WriteString("if err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + ret + "](err)}\n")
	out.WriteString(g.block(f.Body, true, f.Return))
	out.WriteString("}\n}")
	return out.String()
}
func (g *goEmitter) failed(name, ret string) string {
	return g.failedType(name, ret)
}
func (g *goEmitter) failedType(name, ret string) string {
	return "if " + name + ".IsFailure(){return er.Propagate[" + ret + "](" + name + ")}\n"
}
func (g *goEmitter) block(b *Block, effect bool, ret string) string {
	return g.blockType(b, effect, goSourceType(g.program, g.program.typeExpressions[ret], ret), false)
}

// blockType lowers a block whose successful completion either returns an
// Exit (effect), returns a Go value, or, for a concrete pure void body
// (noResult), falls through a Go function without a result. Completion is
// separate from expression evaluation: an empty or let-final block completes
// with void, and a void tail is evaluated once for its effects before the
// block completes with void in whatever form its result mode requires.
func (g *goEmitter) blockType(b *Block, effect bool, ret string, noResult bool) string {
	return g.blockTail(b, effect, ret, noResult, nil)
}

// blockTail is blockType for a block that may be in tail position of a loop
// function (tail != nil). There the last statement is lowered by
// tailStatement, and a block that completes with void must return explicitly
// even when the function has no result: falling off the end would run the
// next iteration.
func (g *goEmitter) blockTail(b *Block, effect bool, ret string, noResult bool, tail *goTail) string {
	defer g.enterScope()()
	var out strings.Builder
	completeVoid := func() {
		switch {
		case effect:
			out.WriteString("return efExit[" + ret + "]{Value:struct{}{}}\n")
		case !noResult:
			out.WriteString("return struct{}{}\n")
		case tail != nil:
			out.WriteString("return\n")
		}
	}
	if len(b.Statements) == 0 {
		completeVoid()
		return out.String()
	}
	// Inside a user declaration each statement's lowering is attributed to
	// the statement's line, and the code after a nested block returns to the
	// enclosing statement's line.
	enclosing := g.line
	if enclosing > 0 {
		defer func() { g.line = enclosing }()
	}
	for i, s := range b.Statements {
		if enclosing > 0 && s.Span.Line > 0 {
			g.line = s.Span.Line
			out.WriteString(g.lineDirective(g.line))
		}
		last := i == len(b.Statements)-1
		if s.Kind == "fail" {
			payload := g.failurePayload(s.Name, s.Payload, effect, ret, &out)
			out.WriteString("return efExit[" + ret + "]{Failure:&er.Failure{Tag:" + strconv.Quote(s.Name) + ",Payload:" + payload + "}}\n")
			continue
		}
		if s.Kind == "let" {
			expr, layout := g.exprLayout(s.Value, effect, ret, &out)
			g.bindLocal(s.Name, layout)
			out.WriteString("efLocal_" + s.Name + " := " + expr + "\n_ = efLocal_" + s.Name + "\n")
			if last {
				completeVoid()
			}
			continue
		}
		if !last {
			g.exprStatement(s.Value, effect, ret, &out)
			continue
		}
		if tail != nil && g.tailStatement(s.Value, ret, noResult, tail, &out) {
			continue
		}
		if s.Value != nil && s.Value.checked.node() != nil && s.Value.checked.node().Kind == "never" {
			expr := g.expr(s.Value, effect, ret, &out)
			if effect {
				out.WriteString("_ = " + expr + "\nreturn efExit[" + ret + "]{Defect:fmt.Errorf(\"bottom expression unexpectedly succeeded\")}\n")
			} else {
				out.WriteString("_ = " + expr + "\npanic(\"bottom expression unexpectedly succeeded\")\n")
			}
			continue
		}
		if noResult || g.isPureVoidExpression(s.Value) {
			g.exprStatement(s.Value, effect, ret, &out)
			completeVoid()
			continue
		}
		expr := g.expr(s.Value, effect, ret, &out)
		if effect {
			out.WriteString("return efExit[" + ret + "]{Value:" + expr + "}\n")
		} else {
			out.WriteString("return " + expr + "\n")
		}
	}
	if enclosing > 0 {
		out.WriteString(g.lineDirective(enclosing))
	}
	return out.String()
}

// goLineReset marks the end of a user declaration in emitted Go; from there
// on, code has its own position in the generated file again.
const goLineReset = "//effra:line-reset"

// goMainFile is the name of the generated main in its module.
const goMainFile = "main.go"

// lineDirective marks the following emitted code as lowered from line of the
// user source. placeLineDirectives turns the marks into the directives Go
// reads after formatting.
func (g *goEmitter) lineDirective(line int) string {
	return "//line " + g.source + ":" + strconv.Itoa(line) + "\n"
}

// sourced emits one user declaration's lowering attributed to its source
// line. Afterwards the emitted code belongs to the enclosing declaration
// again, or, at top level, to the generated file. Without a source name or
// a line, the lowering is emitted unchanged.
func (g *goEmitter) sourced(line int, emit func() string) string {
	if g.source == "" || line <= 0 {
		return emit()
	}
	enclosing := g.line
	g.line = line
	body := emit()
	g.line = enclosing
	if enclosing > 0 {
		return g.lineDirective(line) + body + g.lineDirective(enclosing)
	}
	return g.lineDirective(line) + body + goLineReset + "\n"
}

// tailStatement lowers the last statement of a loop function's tail block when
// it is a self tail call or a branch leading to one.
func (g *goEmitter) tailStatement(e *Expr, ret string, noResult bool, tail *goTail, out *strings.Builder) bool {
	switch {
	case tail.loop.isCall(e):
		g.tailCall(e, tail, out)
	case tail.loop.onSpine(e):
		g.tailBranches(e, ret, noResult, tail, out)
	default:
		return false
	}
	return true
}

func (g *goEmitter) expr(e *Expr, effect bool, ret string, out *strings.Builder) string {
	return g.exprWithValue(e, effect, ret, out, true)
}

// exprAt lowers e into a destination with the required Go layout.
func (g *goEmitter) exprAt(e *Expr, required goLayout, effect bool, ret string, out *strings.Builder) string {
	value, layout := g.exprLayout(e, effect, ret, out)
	return g.adaptGoLayout(value, layout, required, out)
}

// exprLayout lowers e without adapting it, reporting the layout it keeps.
func (g *goEmitter) exprLayout(e *Expr, effect bool, ret string, out *strings.Builder) (string, goLayout) {
	return g.exprLayoutWithValue(e, effect, ret, out, true)
}

// exprWithValue lowers e in the canonical layout of its checked type.
func (g *goEmitter) exprWithValue(e *Expr, effect bool, ret string, out *strings.Builder, wantValue bool) string {
	value, layout := g.exprLayoutWithValue(e, effect, ret, out, wantValue)
	return g.adaptGoLayout(value, layout, canonicalLayout(e.checked.valueID()), out)
}

func (g *goEmitter) exprLayoutWithValue(e *Expr, effect bool, ret string, out *strings.Builder, wantValue bool) (string, goLayout) {
	if value, layout, ok := g.declaredLayoutValue(e, effect, ret, out, wantValue); ok {
		return value, layout
	}
	return g.lower(e, effect, ret, out, wantValue), canonicalLayout(e.checked.valueID())
}

// declaredLayoutValue lowers the expressions whose value may keep a declared
// layout: generic data fields, locals bound from them, and pure calls through
// such callables, whose arguments and result follow the callee's layout. An
// effect call's recipe is adapted to its canonical layout immediately. ok is
// false for every other expression, which lower emits in its canonical layout.
func (g *goEmitter) declaredLayoutValue(e *Expr, effect bool, ret string, out *strings.Builder, wantValue bool) (string, goLayout, bool) {
	switch {
	case e.Kind == "name" && e.Text != "function" && e.Text != "provider":
		layout, ok := g.locals[e.Name]
		return "efLocal_" + e.Name, layout, ok
	case e.Kind == "member" && e.ResolvedFunction == nil && e.Text == "field":
		left, leftLayout := g.exprLayout(e.Left, effect, ret, out)
		layout, ok := g.templateFieldLayout(leftLayout, "", e.Name)
		if !ok {
			layout = canonicalLayout(e.checked.valueID())
		}
		return left + "." + goFieldName(e.Name), layout, true
	case e.Kind == "call" && e.Text == "callable":
		callee, calleeLayout := g.exprLayout(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + callee + "\n")
		node, frame := calleeLayout.resolve(g.program.semantic)
		args := []string{}
		for index, arg := range e.Args {
			value := ""
			if node != nil && index < len(node.Args) {
				value = g.exprAt(arg, frame.child(node.Args[index]), effect, ret, out)
			} else {
				value = g.expr(arg, effect, ret, out)
			}
			local := g.temp()
			out.WriteString(local + " := " + value + "\n")
			args = append(args, local)
		}
		call := g.callValue(e, name+"("+strings.Join(args, ", ")+")", wantValue)
		switch {
		case node == nil:
			return call, canonicalLayout(e.checked.valueID()), true
		case node.Mode == "effect":
			return g.adaptRecipe(call, frame.child(node.Result), canonicalLayout(e.checked.resultID()), out), canonicalLayout(e.checked.valueID()), true
		}
		return call, frame.child(node.Result), true
	}
	return "", goLayout{}, false
}

// enterScope opens a lexical scope for local layouts and returns its closer.
func (g *goEmitter) enterScope() func() {
	saved := g.locals
	g.locals = make(map[string]goLayout, len(saved))
	for name, layout := range saved {
		g.locals[name] = layout
	}
	return func() { g.locals = saved }
}

// bindLocal records the layout of a newly bound local, shadowing any outer
// local of the same name.
func (g *goEmitter) bindLocal(name string, layout goLayout) {
	if len(layout.bindings) == 0 {
		delete(g.locals, name)
		return
	}
	g.locals[name] = layout
}

// exprStatement evaluates e exactly once for its effects and discards its
// value. A pure void call is emitted as a bare Go call statement because its
// Go callee may have no result; the void literal has nothing to evaluate; any
// other value is assigned to the blank identifier.
func (g *goEmitter) exprStatement(e *Expr, effect bool, ret string, out *strings.Builder) {
	switch {
	case e.Kind == "void":
	case e.Kind == "call" && g.isPureVoidExpression(e):
		call := g.exprWithValue(e, effect, ret, out, false)
		out.WriteString(call + "\n")
	default:
		value, _ := g.exprLayout(e, effect, ret, out)
		out.WriteString("_ = " + value + "\n")
	}
}

func (g *goEmitter) isPureVoidExpression(e *Expr) bool {
	if e == nil || e.checked.value.arena == nil || e.checked.isEffect() {
		return false
	}
	node := e.checked.node()
	return node != nil && node.Kind == "primitive" && node.Name == voidTypeName
}

func (g *goEmitter) callValue(e *Expr, call string, wantValue bool) string {
	if !g.isPureVoidExpression(e) || !wantValue {
		return call
	}
	result := g.resultType(e)
	return "func() " + result + " {\n" + call + "\nreturn struct{}{}\n}()"
}

func (g *goEmitter) inferredTypeBindings(f *Function, arguments []*Expr) map[TypeID]goLayout {
	if f == nil || len(f.TypeParameters) == 0 || g.program.semantic == nil {
		return nil
	}
	variables := map[TypeID]bool{}
	for _, parameter := range f.TypeParameters {
		variables[parameter.typeID] = true
	}
	bindings := map[TypeID]TypeID{}
	for index, parameter := range f.Params {
		if index >= len(arguments) {
			continue
		}
		g.program.semantic.unifyTemplateTypes(parameter.typeID, arguments[index].checked.valueID(), variables, bindings, true)
	}
	layouts := make(map[TypeID]goLayout, len(bindings))
	for variable, bound := range bindings {
		layouts[variable] = canonicalLayout(bound)
	}
	return layouts
}

// lower emits an expression in the canonical layout of its checked type.
func (g *goEmitter) lower(e *Expr, effect bool, ret string, out *strings.Builder, wantValue bool) string {
	switch e.Kind {
	case "codec":
		return g.codecOperation(e, ret, out)
	case "intrinsic":
		return g.intrinsicOperation(e, ret, out)
	case "endpoint", "routes":
		return g.endpointOperation(e, ret, out)
	case "member":
		if e.ResolvedFunction != nil {
			return e.ResolvedFunction.goEmissionName()
		}
		left := g.expr(e.Left, effect, ret, out)
		if e.Text == "field" {
			return left + "." + goFieldName(e.Name)
		}
		return g.goResultField(e, left)
	case "orFail":
		left := g.expr(e.Left, effect, ret, out)
		resultType := g.resultType(e)
		return "func(ctx efContext)efExit[" + resultType + "]{return er.Invoke(ctx.Runtime,er.OrFail(efToRuntime(ctx," + left + ")))}"
	case "integer":
		return "int64(" + normalizedI64Literal(e.Text) + ")"
	case "unary":
		if e.Name != "-" {
			panic("unchecked unary operator reached Go emitter")
		}
		// Go accepts the signed minimum as an untyped constant, but converting
		// its positive magnitude to int64 would fail before the unary operator
		// can run. Keep the one boundary literal representable at compile time.
		if e.Left.Kind == "integer" && isMinI64Literal(e.Left.Text) {
			return "int64(-" + normalizedI64Literal(e.Left.Text) + ")"
		}
		// Keep the operand's evaluation exactly once, then apply the negation to
		// a runtime value. Go constant-folds `-int64(-9223372036854775808)`
		// before execution and rejects that expression as an overflowing typed
		// constant; a local gives every unary operation the signed-width runtime
		// semantics used by the rest of the native arithmetic lowering.
		operand := g.expr(e.Left, effect, ret, out)
		return "func() int64 { value := " + operand + "; return -value }()"
	case "string":
		return strconv.Quote(e.Text)
	case "bool":
		return e.Text
	case "void":
		return "struct{}{}"
	case "construct":
		return g.construct(e, effect, ret, out)
	case "name":
		if e.Text == "function" {
			return e.ResolvedFunction.goEmissionName()
		}
		if e.Text == "provider" {
			if e.Name == "TestClock" {
				return "efProvider_TestClock(er.CurrentTestScheduler(ctx.Runtime))"
			}
			if e.Name == "TestScheduler" {
				return "efProvider_TestScheduler(er.CurrentTestScheduler(ctx.Runtime))"
			}
			return "efProvider_" + e.Name + "()"
		}
		return "efLocal_" + e.Name
	case "scope":
		name := g.temp()
		valueType := g.valueType(e)
		out.WriteString(name + " := efScoped(func(ctx efContext)efExit[" + valueType + "]{\n" + g.blockType(e.Then, true, valueType, false) + "})(ctx)\n" + g.failed(name, ret))
		return name + ".Value"
	case "fork":
		expr := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := efFork(" + expr + ")(ctx)\n" + g.failed(name, ret))
		return name + ".Value"
	case "hostAssert":
		return g.hostAssert(e, g.expr(e.Left, effect, ret, out))
	case "timeout":
		left := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + left + "\n")
		right := g.expr(e.Right, effect, ret, out)
		return "efTimeout(" + name + ", " + right + ")"
	case "call":
		if e.Text == "data" {
			return g.constructCall(e, effect, ret, out)
		}
		if e.Text == "foreign" {
			return g.foreign(e, effect, ret, out)
		}
		if e.Text == "hostConvert" {
			return g.hostConversion(e, g.expr(e.Args[0], effect, ret, out))
		}
		if e.Text == "fiber" {
			method := map[string]string{"join": "efJoin", "interrupt": "efInterrupt", "cancel": "efCancel"}[e.Left.Name]
			return method + "(efLocal_" + e.Left.Left.Name + ")"
		}
		// Arguments are evaluated into temporaries in source order and passed
		// in the order of the parameters their labels bind.
		if e.Text == "provider-constructor" {
			args := make([]string, len(e.Args))
			for index, a := range e.Args {
				expr := g.expr(a, effect, ret, out)
				name := g.temp()
				out.WriteString(name + " := " + expr + "\n")
				args[e.argumentParameter(index)] = name
			}
			return "efProvider_" + e.Left.Name + "(" + strings.Join(args, ", ") + ")"
		}
		arguments := e.boundArguments()
		args := make([]string, len(arguments))
		bindings := g.inferredTypeBindings(e.ResolvedFunction, e.parameterArguments())
		for index, a := range arguments {
			expr := ""
			parameter := e.argumentParameter(index)
			if e.ResolvedFunction != nil && parameter < len(e.ResolvedFunction.Params) {
				expr = g.exprAt(a, goLayout{id: e.ResolvedFunction.Params[parameter].typeID, bindings: bindings}, effect, ret, out)
			} else {
				expr = g.expr(a, effect, ret, out)
			}
			name := g.temp()
			out.WriteString(name + " := " + expr + "\n")
			args[parameter] = name
		}
		if e.ResolvedFunction != nil && e.ResolvedFunction.Owner == "module" {
			return g.callValue(e, e.ResolvedFunction.goEmissionName()+"("+strings.Join(args, ", ")+")", wantValue)
		}
		if e.Left.Kind == "name" {
			return g.callValue(e, "efFunction_"+e.Left.Name+"("+strings.Join(args, ", ")+")", wantValue)
		}
		return g.callValue(e, "efCall_"+e.Left.Left.Name+"_"+e.Left.Name+"("+strings.Join(args, ", ")+")", wantValue)
	case "run":
		expr := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + expr + "(ctx)\n" + g.failed(name, ret))
		return name + ".Value"
	case "provide":
		left := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + left + "\n")
		right := g.expr(e.Right, effect, ret, out)
		return "efProvide_" + e.Name + "(" + name + ", " + right + ")"
	case "provideLayer":
		left := g.expr(e.Left, effect, ret, out)
		return "efLayer_" + e.Name + "(" + left + ")"
	case "catch":
		left := g.expr(e.Left, effect, ret, out)
		var fallback strings.Builder
		valueType := g.resultType(e)
		right := g.expr(e.Right, false, ret, &fallback)
		return "efCatch(" + left + ", " + strconv.Quote(e.Name) + ", func() " + valueType + " {\n" + fallback.String() + "return " + right + "\n})"
	case "recover":
		return g.recoverFailure(e, effect, ret, out)
	case "binary":
		left := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + left + "\n")
		right := g.expr(e.Right, effect, ret, out)
		return "(" + name + " " + e.Name + " " + right + ")"
	case "if":
		var body strings.Builder
		valueType := g.valueType(e)
		condition := g.expr(e.Left, effect, valueType, &body)
		body.WriteString("if " + condition + " {\n" + g.blockType(e.Then, effect, valueType, false) + "} else {\n" + g.blockType(e.Else, effect, valueType, false) + "}\n")
		if effect {
			name := g.temp()
			out.WriteString(name + " := func() efExit[" + valueType + "] {\n" + body.String() + "}()\n" + g.failed(name, ret))
			return name + ".Value"
		}
		return "func() " + valueType + " {\n" + body.String() + "}()"
	case "match":
		return g.match(e, effect, ret, out)
	}
	panic("unchecked expression reached Go emitter")
}

func goTemplateDeclaration(r *Record) string {
	var out strings.Builder
	parameters := []string{}
	arguments := []string{}
	for _, p := range r.Parameters {
		name := emittedTypeVariable(r.owner.node(p.typeID))
		parameters = append(parameters, name+" any")
		arguments = append(arguments, name)
	}
	params, args := "["+strings.Join(parameters, ",")+"]", "["+strings.Join(arguments, ",")+"]"
	fields := func(fields []Field) {
		for _, field := range fields {
			out.WriteString(goFieldName(field.Name) + " " + canonicalGoType(r.owner, field.typeID, map[TypeID]bool{}) + "\n")
		}
	}
	if r.Kind == "enum" {
		marker := "efVariantTemplate_" + r.EmissionName
		// The marker takes no type arguments: a method set that mentions T would
		// make every instantiation a distinct Go GC shape, so each runtime generic
		// over an option would be stenciled once per payload type. The Effra
		// checker is the type authority; the variant structs stay generic so a
		// type switch still tells Some[A] from Some[B].
		out.WriteString("type efTemplate_" + r.EmissionName + params + " interface { " + marker + "() }\n")
		for _, variant := range r.Variants {
			name := goVariantType("template_"+r.EmissionName, variant.Name)
			out.WriteString("type " + name + params + " struct {\n")
			fields(variant.Fields)
			out.WriteString("}\nfunc (" + name + args + ") " + marker + "() {}\n")
		}
		return out.String()
	}
	out.WriteString("type efTemplate_" + r.EmissionName + params + " struct {\n")
	fields(r.Fields)
	out.WriteString("}\n")
	return out.String()
}

// dataTypes emits the data, error and template declarations the plan
// retains through canonical type nodes or reachable fail statements.
func (g *goEmitter) dataTypes(out *strings.Builder) {
	for _, r := range g.program.BundledTemplates {
		if g.plan.Requires(RequiresDeclaration, r.Identity) {
			out.WriteString(goTemplateDeclaration(r))
		}
	}
	for _, r := range append(append([]*DataDeclaration{}, g.program.Records...), g.program.Enums...) {
		if len(r.Parameters) > 0 && g.plan.Requires(RequiresDeclaration, r.Identity) {
			out.WriteString(goTemplateDeclaration(r))
		}
	}
	for _, declaration := range g.programDeclarations() {
		if !g.plan.Requires(RequiresDeclaration, g.program.semantic.declarationIdentity(declaration.Kind, "module", declaration.Name)) {
			continue
		}
		switch declaration.Kind {
		case "record", "error":
			out.WriteString("type efType_" + goIdent(declaration.Name) + " struct {\n")
			for _, field := range declaration.Fields {
				out.WriteString(goFieldName(field.Name) + " " + goSourceType(g.program, field.sourceType, field.Type) + "\n")
			}
			out.WriteString("}\n")
		case "enum":
			out.WriteString("type efType_" + goIdent(declaration.Name) + " interface { efVariant_" + goIdent(declaration.Name) + "() }\n")
			for _, variant := range declaration.Variants {
				out.WriteString("type " + goVariantType(declaration.Name, variant.Name) + " struct {\n")
				for _, field := range variant.Fields {
					out.WriteString(goFieldName(field.Name) + " " + goSourceType(g.program, field.sourceType, field.Type) + "\n")
				}
				out.WriteString("}\nfunc (" + goVariantType(declaration.Name, variant.Name) + ") efVariant_" + goIdent(declaration.Name) + "() {}\n")
			}
		}
	}
}
func (g *goEmitter) programDeclarations() []Declaration {
	if g.program == nil {
		return nil
	}
	return g.programDeclarationProjection()
}
func (g *goEmitter) programDeclarationProjection() []Declaration {
	result := []Declaration{}
	for _, decl := range g.program.ErrorDecls {
		result = append(result, Declaration{Kind: "error", Name: decl.Name, Fields: decl.Fields, Span: decl.Span})
	}
	for _, record := range g.program.Records {
		if len(record.Parameters) > 0 {
			continue
		}
		result = append(result, Declaration{Kind: "record", Name: record.Name, Fields: record.Fields, Span: record.Span})
	}
	for _, enum := range g.program.Enums {
		if len(enum.Parameters) > 0 {
			continue
		}
		result = append(result, Declaration{Kind: "enum", Name: enum.Name, Variants: enum.Variants, Span: enum.Span})
	}
	return result
}
func (g *goEmitter) declaration(kind, name string) *Declaration {
	for _, declaration := range g.programDeclarations() {
		if declaration.Kind == kind && declaration.Name == name {
			return &declaration
		}
	}
	return nil
}
func (g *goEmitter) failurePayload(name string, payload *Expr, effect bool, ret string, out *strings.Builder) string {
	if payload == nil {
		return "nil"
	}
	decl := g.declaration("error", name)
	if payload.Kind != "payload" {
		expr := g.expr(payload, false, ret, out)
		if decl != nil && len(decl.Fields) == 1 {
			return "efType_" + goIdent(name) + "{" + goFieldName(decl.Fields[0].Name) + ":" + expr + "}"
		}
		return expr
	}
	parts := []string{}
	for _, field := range payload.Fields {
		parts = append(parts, goFieldName(field.Name)+":"+g.expr(field.Value, false, ret, out))
	}
	return "efType_" + goIdent(name) + "{" + strings.Join(parts, ",") + "}"
}
func (g *goEmitter) construct(e *Expr, effect bool, ret string, out *strings.Builder) string {
	typeName, variantName := "", ""
	if e.Left != nil && e.Left.Kind == "name" {
		typeName = e.Left.Name
	} else if e.Left != nil && e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	}
	parts := []string{}
	for _, field := range e.Fields {
		value := ""
		if declared, ok := g.constructedFieldLayout(e, field.Name); ok {
			value = g.exprAt(field.Value, declared, false, ret, out)
		} else {
			value = g.expr(field.Value, false, ret, out)
		}
		parts = append(parts, goFieldName(field.Name)+":"+value)
	}
	if e.ResolvedTemplate != nil {
		if e.ResolvedTemplate.Kind == "enum" {
			return canonicalGoType(g.program.semantic, e.checked.resultID(), map[TypeID]bool{}) + "(" + g.variantType(e.checked.resultID(), e.ResolvedTemplate, e.Left.Name) + "{" + strings.Join(parts, ",") + "})"
		}
		return canonicalGoType(g.program.semantic, e.checked.resultID(), map[TypeID]bool{}) + "{" + strings.Join(parts, ",") + "}"
	}
	if variantName != "" {
		variant := goVariantType(typeName, variantName) + "{" + strings.Join(parts, ",") + "}"
		return "efType_" + goIdent(typeName) + "(" + variant + ")"
	}
	return "efType_" + goIdent(typeName) + "{" + strings.Join(parts, ",") + "}"
}

// constructedFieldLayout returns the declared layout of a field initialized
// by a generic constructor: a record template's field or the selected enum
// variant's payload under the constructed application.
func (g *goEmitter) constructedFieldLayout(e *Expr, field string) (goLayout, bool) {
	if e.ResolvedTemplate == nil {
		return goLayout{}, false
	}
	return g.templateFieldLayout(canonicalLayout(e.checked.resultID()), e.Left.Name, field)
}
func (g *goEmitter) constructCall(e *Expr, effect bool, ret string, out *strings.Builder) string {
	return g.construct(e, effect, ret, out)
}

func (g *goEmitter) variantType(id TypeID, owner *Enum, variant string) string {
	if len(owner.Parameters) == 0 {
		return goVariantType(owner.Name, variant)
	}
	args := []string{}
	for _, argument := range g.program.semantic.node(id).Args {
		args = append(args, canonicalGoType(g.program.semantic, argument, map[TypeID]bool{}))
	}
	return goVariantType("template_"+owner.EmissionName, variant) + "[" + strings.Join(args, ",") + "]"
}

// match lowers the checked match plan. Subjects are evaluated once, in order,
// into temporaries; each tested subject's variant is decoded once; arms then
// test those tags in source order and each body is emitted once.
func (g *goEmitter) match(e *Expr, effect bool, ret string, out *strings.Builder) string {
	subjects := g.matchSubjects(e, effect, ret, out)
	resultType := g.valueType(e)
	body := g.matchSwitch(e, subjects, effect, resultType, false, nil)
	if effect {
		body += "default: return efExit[" + resultType + "]{Defect:fmt.Errorf(\"unreachable non-exhaustive match\")}\n}\n"
		name := g.temp()
		out.WriteString(name + " := func() efExit[" + resultType + "] {\n" + body + "}()\n" + g.failed(name, ret))
		return name + ".Value"
	}
	body += "default: panic(\"unreachable non-exhaustive match\")\n}\n"
	return "func() " + resultType + " {\n" + body + "}()"
}

// matchSubjects evaluates each subject once, in order, into a temporary.
func (g *goEmitter) matchSubjects(e *Expr, effect bool, ret string, out *strings.Builder) []string {
	subjects := make([]string, len(e.Args))
	for index, subject := range e.Args {
		value := g.expr(subject, effect, ret, out)
		subjects[index] = g.temp()
		out.WriteString(subjects[index] + " := " + value + "\n_ = " + subjects[index] + "\n")
	}
	return subjects
}

// matchSwitch lowers the variant tests and the arms up to, but not including,
// the default case, so a caller can close the switch for its own result mode.
// resultType and noResult describe the arm bodies; tail marks them as tails of
// a loop function.
func (g *goEmitter) matchSwitch(e *Expr, subjects []string, effect bool, resultType string, noResult bool, tail *goTail) string {
	plan := e.matchPlan
	var body strings.Builder
	tags := make([]string, len(subjects))
	for index, subject := range plan.subjects {
		if !subject.tested {
			continue
		}
		tags[index] = g.temp()
		body.WriteString("var " + tags[index] + " int\nswitch " + subjects[index] + ".(type) {\n")
		for variant, name := range subject.variants {
			body.WriteString("case " + g.variantType(subject.value.valueID(), subject.enum, name) + ": " + tags[index] + " = " + strconv.Itoa(variant) + "\n")
		}
		// Variant markers carry no type arguments, so Go no longer rejects a
		// foreign dynamic value; refuse it rather than select the first arm.
		body.WriteString("default: panic(\"unreachable enum variant\")\n}\n")
	}
	body.WriteString("switch {\n")
	for _, arm := range plan.arms {
		conditions := []string{}
		for index, cell := range arm.cells {
			if cell.total {
				continue
			}
			alternatives := []string{}
			for _, pattern := range cell.alternatives {
				alternatives = append(alternatives, tags[index]+" == "+strconv.Itoa(plan.subjects[index].index(pattern.VariantName)))
			}
			conditions = append(conditions, "("+strings.Join(alternatives, " || ")+")")
		}
		if len(conditions) == 0 {
			conditions = append(conditions, "true")
		}
		body.WriteString("case " + strings.Join(conditions, " && ") + ":\n")
		g.matchArm(plan, arm, subjects, effect, resultType, noResult, tail, &body)
	}
	return body.String()
}

// matchArm binds an arm's payload binders and lowers its body in one scope.
// A binder taken from a single alternative keeps the layout its generic
// variant declares. A binder joined across alternatives has one Go local, so
// each alternative's declared layout is adapted to the binder's canonical
// layout before the assignment.
func (g *goEmitter) matchArm(plan *matchPlan, arm matchPlanArm, subjects []string, effect bool, resultType string, noResult bool, tail *goTail, body *strings.Builder) {
	defer g.enterScope()()
	for _, binding := range arm.bindings {
		subject := plan.subjects[binding.subject]
		scrutinee := canonicalLayout(subject.value.valueID())
		alternatives := arm.cells[binding.subject].alternatives
		local := "efLocal_" + binding.name
		if len(alternatives) == 1 {
			layout, _ := g.templateFieldLayout(scrutinee, alternatives[0].VariantName, binding.fields[0])
			body.WriteString(local + " := " + subjects[binding.subject] + ".(" + g.variantType(subject.value.valueID(), subject.enum, alternatives[0].VariantName) + ")." + goFieldName(binding.fields[0]) + "\n")
			g.bindLocal(binding.name, layout)
		} else {
			canonical := canonicalLayout(binding.value.valueID())
			body.WriteString("var " + local + " " + g.canonicalValueType(binding.value) + "\nswitch efMatch := " + subjects[binding.subject] + ".(type) {\n")
			for alternative, pattern := range alternatives {
				value := "efMatch." + goFieldName(binding.fields[alternative])
				var adapted strings.Builder
				if layout, ok := g.templateFieldLayout(scrutinee, pattern.VariantName, binding.fields[alternative]); ok {
					value = g.adaptGoLayout(value, layout, canonical, &adapted)
				}
				body.WriteString("case " + g.variantType(subject.value.valueID(), subject.enum, pattern.VariantName) + ":\n" + adapted.String() + local + " = " + value + "\n")
			}
			body.WriteString("}\n")
			g.bindLocal(binding.name, canonical)
		}
		body.WriteString("_ = " + local + "\n")
	}
	body.WriteString(g.blockTail(arm.body, effect, resultType, noResult, tail))
}

// WriteRuntime writes the application's selected runtime sources below
// directory/runtime, rewriting only changed bytes so Go's own build cache
// remains useful. It never deletes files: a runtime directory holding a file
// outside the selection is refused, because a stale source would reintroduce
// a module the application no longer selects. Published builds use the
// immutable GoSourceSnapshot boundary instead.
func (a *GoApplication) WriteRuntime(directory string) error {
	sources, err := a.RuntimeSources()
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "runtime")
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, selected := sources[entry.Name()]; !selected {
			return fmt.Errorf("runtime directory %s contains unselected %s; write into a fresh directory", path, entry.Name())
		}
	}
	for name, data := range sources {
		file := filepath.Join(path, name)
		old, _ := os.ReadFile(file)
		if string(old) == string(data) {
			continue
		}
		if err := os.WriteFile(file, data, 0644); err != nil {
			return err
		}
	}
	return nil
}
