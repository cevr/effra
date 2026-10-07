package compiler

import (
	rt "effra.local/prototype/runtime/effra"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type goEmitter struct {
	next     int
	bindings map[string]Binding
	program  *Program
}

func goType(t string) string {
	if strings.HasPrefix(t, "GoResult:") {
		return "er.GoResult[" + goType(strings.TrimPrefix(t, "GoResult:")) + "]"
	}
	switch t {
	case "string":
		return "string"
	case "Handler":
		return "func(string) efEffect[string]"
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
			return "*er.Fiber[" + goType(strings.TrimPrefix(t, "Fiber:")) + "]"
		}
		if strings.HasPrefix(t, "GoResult:") {
			return "er.GoResult[" + goType(strings.TrimPrefix(t, "GoResult:")) + "]"
		}
		if t == "void" || t == "never" || t == "invalid" || t == "" {
			return "struct{}"
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
		switch node.Name {
		case "Handler":
			return "func(string) efEffect[string]"
		case "File":
			return "*er.File"
		case "Latch":
			return "*er.Latch"
		}
	case "record", "enum", "error":
		return "efType_" + goIdent(node.Name)
	case "type-variable":
		return emittedTypeVariable(node)
	case "application":
		r := c.templates[node.Declaration]
		if r == nil {
			return "struct{}"
		}
		args := []string{}
		for _, id := range node.Args {
			args = append(args, canonicalGoType(c, id, visiting))
		}
		return "efTemplate_" + r.EmissionName + "[" + strings.Join(args, ",") + "]"
	case "fiber":
		if len(node.Args) == 1 {
			return "*er.Fiber[" + canonicalGoType(c, node.Args[0], visiting) + "]"
		}
	case "goResult":
		if len(node.Args) == 1 {
			return "er.GoResult[" + canonicalGoType(c, node.Args[0], visiting) + "]"
		}
	case "provider":
		return "efService_" + goIdent(node.Name)
	case "callable":
		return canonicalGoCallableType(c, node, visiting)
	case "recipe", "providerRecipe":
		return "efEffect[" + canonicalGoType(c, node.Result, visiting) + "]"
	case "never", "invalid":
		return "struct{}"
	}
	return "struct{}"
}

func canonicalGoCallableType(c *checker, node *semanticTypeNode, visiting map[TypeID]bool) string {
	if node == nil {
		return "func() struct{}"
	}
	parameters := make([]string, 0, len(node.Args))
	for _, parameter := range node.Args {
		parameters = append(parameters, canonicalGoType(c, parameter, visiting))
	}
	result := canonicalGoType(c, node.Result, visiting)
	if node.Mode == "effect" {
		result = "efEffect[" + result + "]"
	}
	return "func(" + strings.Join(parameters, ", ") + ") " + result
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
func goParams(f *Function) string {
	return goParamsList(f.Params)
}
func goParamsList(params []Param) string {
	parts := []string{}
	for _, p := range params {
		parts = append(parts, "efLocal_"+p.Name+" "+goSourceType(p.sourceType, p.Type))
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
func goMethodType(f *Function) string {
	types := []string{}
	for _, p := range f.Params {
		types = append(types, goSourceType(p.sourceType, p.Type))
	}
	return "func(" + strings.Join(types, ", ") + ") efEffect[" + goSourceType(f.returnType, f.Return) + "]"
}
func (g *goEmitter) temp() string { g.next++; return fmt.Sprintf("efTemp%d", g.next) }

// EmitGo lowers checked IR to a standalone Go program using the managed runtime.
// Error and requirement rows are checked in the frontend; success values stay typed in Go.
func (r *Result) EmitGo() (string, error) { return r.emitGo(nil) }
func (r *Result) emitGo(tests []*Symbol) (string, error) {
	if tests == nil {
		if err := r.Entry(); err != nil {
			return "", err
		}
	}
	g := &goEmitter{program: r.Program}
	var out strings.Builder
	out.WriteString("// Generated by the Effra prototype. Source revision: " + r.Revision + "\npackage main\nimport (\"fmt\"; \"os\"; \"context\"; \"os/signal\"; \"syscall\"; er \"effra.generated/runtime\"\n")
	if tests != nil {
		out.WriteString("\"encoding/json\"\n")
	}
	for _, imp := range r.Program.Imports {
		if r.Program.UsedImports[imp.Alias] {
			out.WriteString("efGo_" + imp.Alias + " " + strconv.Quote(imp.Path) + "\n")
		}
	}
	out.WriteString(")\n")
	g.bindings = r.Program.Bindings
	out.WriteString(`
type efExit[A any] = er.Exit[A]
type efEffect[A any] func(efContext) efExit[A]
func efToRuntime[A any](ctx efContext,program efEffect[A]) er.Effect[A] {return func(fc *er.FiberContext) er.Exit[A] {ctx.Runtime=fc;return program(ctx)}}
func efFromRuntime[A any](program er.Effect[A]) efEffect[A] {return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,program)}}
func efCatch[A any](program efEffect[A],tag string,fallback func()A)efEffect[A]{return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,er.Catch(efToRuntime(ctx,program),tag,fallback))}}
func efScoped[A any](program efEffect[A])efEffect[A]{return func(ctx efContext)efExit[A]{return er.Invoke(ctx.Runtime,er.Scoped(efToRuntime(ctx,program)))}}
func efTimeout[A any](program efEffect[A],duration int64)efEffect[A]{return func(ctx efContext)efExit[A]{if ctx.s_Scheduler==nil||ctx.s_Scheduler.m_sleep==nil{return er.Die[A](fmt.Errorf("missing provider Scheduler.sleep"))};deadline:=ctx.s_Scheduler.m_sleep(duration);return er.Invoke(ctx.Runtime,er.TimeoutWithEffect(efToRuntime(ctx,program),efToRuntime(ctx,deadline)))}}
func efFork[A any](program efEffect[A])efEffect[*er.Fiber[A]]{return func(ctx efContext)efExit[*er.Fiber[A]]{return er.Invoke(ctx.Runtime,er.Fork(efToRuntime(ctx,program)))}}
func efJoin[A any](fiber *er.Fiber[A])efEffect[A]{return efFromRuntime(fiber.Join())}
func efInterrupt[A any](fiber *er.Fiber[A])efEffect[struct{}]{return efFromRuntime(fiber.Interrupt())}
func efCancel[A any](fiber *er.Fiber[A])efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{fiber.Cancel();return er.Succeed(struct{}{})})}
`)
	g.dataTypes(&out)
	services := append(builtins(), r.Program.Services...)
	out.WriteString("type efContext struct {\nRuntime *er.FiberContext\n")
	for _, s := range services {
		out.WriteString("s_" + s.Name + " *efService_" + s.Name + "\n")
	}
	out.WriteString("}\n")
	for _, s := range services {
		out.WriteString("type efService_" + s.Name + " struct {\n")
		if s.Name == "Clock" || s.Name == "Scheduler" {
			out.WriteString("driver *er.TestScheduler\n")
		}
		for _, m := range s.Methods {
			out.WriteString("m_" + m.Name + " " + goMethodType(m) + "\n")
		}
		out.WriteString("}\n")
		out.WriteString("func efProvide_" + s.Name + "[A any](program efEffect[A], provider efService_" + s.Name + ") efEffect[A] { return func(ctx efContext) efExit[A] {")
		out.WriteString("ctx.s_" + s.Name + " = &provider; return program(ctx) } }\n")
		for _, m := range s.Methods {
			result := goSourceType(m.returnType, m.Return)
			out.WriteString("func efCall_" + s.Name + "_" + m.Name + "(" + goParams(m) + ") efEffect[" + result + "] { return func(ctx efContext) efExit[" + result + "] {\n")
			out.WriteString("if ctx.s_" + s.Name + " == nil || ctx.s_" + s.Name + ".m_" + m.Name + " == nil { return efExit[" + result + "]{Defect:fmt.Errorf(" + strconv.Quote("missing provider "+s.Name+"."+m.Name) + ")} }\n")
			out.WriteString("return ctx.s_" + s.Name + ".m_" + m.Name + "(" + goArgs(m) + ")(ctx)\n} }\n")
		}
	}
	out.WriteString(`
func efProvider_Assertions()efService_Assert{return efService_Assert{m_check:func(condition bool,message string)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{if !condition{return er.Fail[struct{}]("AssertionFailed",message)};return er.Succeed(struct{}{})})},m_equalText:func(actual,expected string)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{if actual!=expected{return er.Fail[struct{}]("AssertionFailed",fmt.Sprintf("expected %q; received %q",expected,actual))};return er.Succeed(struct{}{})})}}}
func efProvider_Stdout()efService_Console{return efService_Console{m_log:func(message string)efEffect[struct{}]{return efFromRuntime(er.Println(message))}}}
func efProvider_LiveClock()efService_Clock{return efService_Clock{m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(er.SleepWithDriver(nil,ms))}}}
func efProvider_TestClock(scheduler *er.TestScheduler)efService_Clock{return efService_Clock{driver:scheduler,m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf("test clock requires an active test scheduler"))};return er.Invoke(fc,er.SleepWithDriver(scheduler,ms))})}}}
func efProvider_LiveScheduler()efService_Scheduler{return efService_Scheduler{m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(er.SleepWithDriver(nil,ms))},m_advance:func(int64)efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{return er.Die[struct{}](fmt.Errorf("live scheduler cannot advance"))})},m_awaitRegistration:func()efEffect[struct{}]{return efFromRuntime(func(*er.FiberContext)er.Exit[struct{}]{return er.Die[struct{}](fmt.Errorf("live scheduler has no registration barrier"))})}}}
func efProvider_TestScheduler(scheduler *er.TestScheduler)efService_Scheduler{return efService_Scheduler{driver:scheduler,m_sleep:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{if scheduler==nil{return er.Die[struct{}](fmt.Errorf("test scheduler requires an active test scheduler"))};return er.Invoke(fc,er.SleepWithDriver(scheduler,ms))})},m_advance:func(ms int64)efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{return er.AdjustTestScheduler(fc,scheduler,ms)})},m_awaitRegistration:func()efEffect[struct{}]{return efFromRuntime(func(fc *er.FiberContext)er.Exit[struct{}]{return er.AwaitTestSchedulerRegistration(fc,scheduler)})}}}
func efProvider_TestSync()efService_Sync{return efService_Sync{m_latch:func()efEffect[*er.Latch]{return efFromRuntime(func(*er.FiberContext)er.Exit[*er.Latch]{return er.Succeed(er.NewLatch())})},m_await:func(latch *er.Latch)efEffect[struct{}]{return efFromRuntime(er.AwaitLatch(latch))},m_signal:func(latch *er.Latch)efEffect[struct{}]{return efFromRuntime(er.SignalLatch(latch))}}}
func efProvider_LiveFiles()efService_Files{return efService_Files{m_openRead:func(path string)efEffect[*er.File]{return efFromRuntime(er.OpenRead(path))},m_readText:func(file *er.File)efEffect[string]{return efFromRuntime(er.ReadText(file))},m_readFile:func(path string)efEffect[string]{return efFromRuntime(er.ReadFile(path))}}}
func efProvider_LiveEnv()efService_Env{return efService_Env{m_get:func(name string)efEffect[string]{return efFromRuntime(er.Env(name))}}}
func efProvider_RuntimeLive()efService_Runtime{return efService_Runtime{m_inspect:func()efEffect[string]{return efFromRuntime(er.InspectScope())}}}
func efProvider_Host()efService_Foreign{return efService_Foreign{}}
func efProvider_GoHttp()efService_Http{return efService_Http{m_serve:func(address string,handler func(string)efEffect[string])efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{return er.Invoke(ctx.Runtime,er.ServeHTTP(address,func(path string)er.Effect[string]{return efToRuntime(ctx,handler(path))},func(bound string){fmt.Println("listening http://"+bound)}))}}}}
`)
	for _, p := range r.Program.Providers {
		if providerConstructed(p) {
			out.WriteString(g.providerConstructor(p))
			continue
		}
		out.WriteString("func efProvider_" + p.Name + "() efService_" + p.Service + " { return efService_" + p.Service + "{\n")
		for _, m := range p.Methods {
			out.WriteString("m_" + m.Name + ": " + strings.TrimSpace(g.function(m)) + ",\n")
		}
		out.WriteString("} }\n")
	}
	for _, plan := range r.Layers {
		out.WriteString(g.layer(plan))
	}
	for _, f := range r.Program.checkedFunctions() {
		out.WriteString(g.functionDeclaration(f))
	}
	if tests == nil {
		mainReturn := voidTypeName
		for _, function := range r.Program.Functions {
			if function.Name == "main" {
				mainReturn = function.Return
				break
			}
		}
		out.WriteString("func main() { base,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();exit:=er.RunContext(base,func(fc *er.FiberContext) er.Exit[" + goSourceType(r.Program.typeExpressions[mainReturn], mainReturn) + "]{return efFunction_main()(efContext{Runtime:fc})});if exit.IsFailure(){fmt.Fprintln(os.Stderr,exit.Cause());os.Exit(1)}\n")
		if mainReturn != voidTypeName {
			out.WriteString("fmt.Println(exit.Value)\n")
		}
		out.WriteString("}\n")
	} else {
		out.WriteString("func main(){base,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop();type testResult struct{Name string " + "`json:\"name\"`" + ";Passed bool " + "`json:\"passed\"`" + ";Cause string " + "`json:\"cause,omitempty\"`" + ";Reasons []map[string]string " + "`json:\"reasons,omitempty\"`" + "};results:=[]testResult{};passed:=true;assertions:=efProvider_Assertions();")
		for _, test := range tests {
			out.WriteString("{scheduler:=er.NewTestScheduler();clock:=efProvider_TestClock(scheduler);testScheduler:=efProvider_TestScheduler(scheduler);syncProvider:=efProvider_TestSync();exit:=er.RunContextWithScheduler(base,scheduler,func(fc *er.FiberContext)er.Exit[struct{}]{return efFunction_" + test.Name + "()(efContext{Runtime:fc,s_Assert:&assertions,s_Clock:&clock,s_Scheduler:&testScheduler,s_Sync:&syncProvider})});item:=testResult{Name:" + strconv.Quote(test.Name) + ",Passed:!exit.IsFailure()};if exit.IsFailure(){item.Cause=fmt.Sprint(exit.Cause());for _,reason:=range exit.Cause(){detail:=map[string]string{\"kind\":reason.Kind};if reason.Failure!=nil{detail[\"tag\"]=reason.Failure.Tag;if reason.Failure.Payload!=nil{detail[\"message\"]=fmt.Sprint(reason.Failure.Payload)}}else if reason.Err!=nil{detail[\"message\"]=reason.Err.Error()};item.Reasons=append(item.Reasons,detail)};passed=false};results=append(results,item)}\n")
		}
		out.WriteString(`json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion":1,"passed":passed,"tests":results});if !passed{os.Exit(1)}}`)
	}
	formatted, err := format.Source([]byte(out.String()))
	if err != nil {
		return "", fmt.Errorf("Go lowering generated invalid syntax: %w", err)
	}
	return string(formatted), nil
}
func (g *goEmitter) function(f *Function) string {
	ret := goSourceType(f.returnType, f.Return)
	if f.Effect {
		ret = "efEffect[" + ret + "]"
	}
	open := "func(" + goParams(f) + ") " + ret + " {\n"
	close := "}\n"
	if f.Effect {
		open += "return func(ctx efContext) efExit[" + goSourceType(f.returnType, f.Return) + "] {\nif err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + goSourceType(f.returnType, f.Return) + "](err)}\n"
		close = "}\n}\n"
	}
	return open + g.block(f.Body, f.Effect, f.Return) + close
}

func (g *goEmitter) functionDeclaration(f *Function) string {
	c := g.program.semantic
	ret := goSourceType(f.returnType, f.Return)
	canonical := len(f.TypeParameters) > 0 || (f.returnType != nil && f.returnType.Application != "")
	if canonical {
		ret = canonicalGoType(c, f.returnID, map[TypeID]bool{})
	}
	params := []string{}
	for _, p := range f.Params {
		typ := goSourceType(p.sourceType, p.Type)
		if len(f.TypeParameters) > 0 || (p.sourceType != nil && p.sourceType.Application != "") {
			typ = canonicalGoType(c, p.typeID, map[TypeID]bool{})
		}
		params = append(params, "efLocal_"+p.Name+" "+typ)
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
	open := "func " + f.goEmissionName() + generic + "(" + strings.Join(params, ",") + ") " + valueRet + " {\n"
	close := "}\n"
	if f.Effect {
		open += "return func(ctx efContext) efExit[" + ret + "] {\nif err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + ret + "](err)}\n"
		close = "}\n" + close
	}
	return open + g.blockType(f.Body, f.Effect, ret) + close
}

// providerConstructed distinguishes an ordinary reusable provider value from
// a constructor recipe. A configured or dependent provider is always built at
// effect execution so its service values are captured at that boundary.
func providerConstructed(p *Provider) bool {
	return len(p.Params) > 0 || len(p.Services) > 0
}

func (g *goEmitter) providerConstructor(p *Provider) string {
	ret := goType("provider:" + p.Service)
	var out strings.Builder
	out.WriteString("func efProvider_" + p.Name + "(" + goParamsList(p.Params) + ") efEffect[" + ret + "] {\n")
	out.WriteString("return func(ctx efContext) efExit[" + ret + "] {\n")
	out.WriteString("if err:=ctx.Runtime.Checkpoint();err!=nil{return er.Interrupt[" + ret + "](err)}\n")
	for _, service := range normalized(p.Services) {
		out.WriteString("if ctx.s_" + service + " == nil { return efExit[" + ret + "]{Defect:fmt.Errorf(" + strconv.Quote("missing provider "+service+" for constructor "+p.Name) + ")} }\n")
		out.WriteString("efCaptured_" + service + " := ctx.s_" + service + "\n")
	}
	out.WriteString("return efExit[" + ret + "]{Value:efService_" + p.Service + "{\n")
	for _, method := range p.Methods {
		implementation := g.function(method)
		if len(p.Services) > 0 {
			implementation = g.providerMethod(method, p.Services)
		}
		out.WriteString("m_" + method.Name + ":" + strings.TrimSpace(implementation) + ",\n")
	}
	out.WriteString("}}\n}\n}\n")
	return out.String()
}

// providerMethod overlays only the captured dependency pointers onto the
// invocation context. The Runtime pointer (and therefore cancellation,
// scope, owner and fiber state) is copied from the current invocation.
func (g *goEmitter) providerMethod(f *Function, captures []string) string {
	ret := goSourceType(f.returnType, f.Return)
	var out strings.Builder
	out.WriteString("func(" + goParams(f) + ") efEffect[" + ret + "] {\nreturn func(ctx efContext) efExit[" + ret + "] {\n")
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
	return g.blockType(b, effect, goSourceType(g.program.typeExpressions[ret], ret))
}
func (g *goEmitter) blockType(b *Block, effect bool, ret string) string {
	var out strings.Builder
	finish := func(expr string) {
		if effect {
			out.WriteString("return efExit[" + ret + "]{Value:" + expr + "}\n")
		} else {
			out.WriteString("return " + expr + "\n")
		}
	}
	if len(b.Statements) == 0 {
		finish("struct{}{}")
		return out.String()
	}
	for i, s := range b.Statements {
		if s.Kind == "fail" {
			payload := g.failurePayload(s.Name, s.Payload, effect, ret, &out)
			out.WriteString("return efExit[" + ret + "]{Failure:&er.Failure{Tag:" + strconv.Quote(s.Name) + ",Payload:" + payload + "}}\n")
			continue
		}
		expr := g.expr(s.Value, effect, ret, &out)
		if s.Kind == "let" {
			out.WriteString("efLocal_" + s.Name + " := " + expr + "\n_ = efLocal_" + s.Name + "\n")
			if i == len(b.Statements)-1 {
				finish("struct{}{}")
			}
		} else if i == len(b.Statements)-1 {
			isNever := s.Value != nil && s.Value.checked.node() != nil && s.Value.checked.node().Kind == "never"
			if isNever {
				if effect {
					out.WriteString("_ = " + expr + "\nreturn efExit[" + ret + "]{Defect:fmt.Errorf(\"bottom expression unexpectedly succeeded\")}\n")
				} else {
					out.WriteString("_ = " + expr + "\npanic(\"bottom expression unexpectedly succeeded\")\n")
				}
			} else {
				finish(expr)
			}
		} else {
			out.WriteString("_ = " + expr + "\n")
		}
	}
	return out.String()
}
func (g *goEmitter) expr(e *Expr, effect bool, ret string, out *strings.Builder) string {
	switch e.Kind {
	case "member":
		if e.ResolvedFunction != nil {
			return e.ResolvedFunction.goEmissionName()
		}
		left := g.expr(e.Left, effect, ret, out)
		if e.Text == "field" {
			return left + "." + goFieldName(e.Name)
		}
		if e.Name == "hasError" {
			return "(" + left + ".Err != nil)"
		}
		return left + ".Value"
	case "orFail":
		left := g.expr(e.Left, effect, ret, out)
		resultType := g.resultType(e)
		return "func(ctx efContext)efExit[" + resultType + "]{return er.Invoke(ctx.Runtime,er.OrFail(efToRuntime(ctx," + left + ")))}"
	case "integer":
		return "int64(" + e.Text + ")"
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
		out.WriteString(name + " := efScoped(func(ctx efContext)efExit[" + valueType + "]{\n" + g.blockType(e.Then, true, valueType) + "})(ctx)\n" + g.failed(name, ret))
		return name + ".Value"
	case "fork":
		expr := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := efFork(" + expr + ")(ctx)\n" + g.failed(name, ret))
		return name + ".Value"
	case "timeout":
		left := g.expr(e.Left, effect, ret, out)
		name := g.temp()
		out.WriteString(name + " := " + left + "\n")
		right := g.expr(e.Right, effect, ret, out)
		return "efTimeout(" + name + ", " + right + ")"
	case "call":
		if e.Text == "callable" {
			callee := g.expr(e.Left, effect, ret, out)
			name := g.temp()
			out.WriteString(name + " := " + callee + "\n")
			args := []string{}
			for _, arg := range e.Args {
				value := g.expr(arg, effect, ret, out)
				local := g.temp()
				out.WriteString(local + " := " + value + "\n")
				args = append(args, local)
			}
			return name + "(" + strings.Join(args, ", ") + ")"
		}
		if e.Text == "data" {
			return g.constructCall(e, effect, ret, out)
		}
		if e.Text == "foreign" {
			return g.foreign(e, effect, ret, out)
		}
		if e.Text == "fiber" {
			method := map[string]string{"join": "efJoin", "interrupt": "efInterrupt", "cancel": "efCancel"}[e.Left.Name]
			return method + "(efLocal_" + e.Left.Left.Name + ")"
		}
		if e.Text == "provider-constructor" {
			args := []string{}
			for _, a := range e.Args {
				expr := g.expr(a, effect, ret, out)
				name := g.temp()
				out.WriteString(name + " := " + expr + "\n")
				args = append(args, name)
			}
			return "efProvider_" + e.Left.Name + "(" + strings.Join(args, ", ") + ")"
		}
		args := []string{}
		for _, a := range e.Args {
			expr := g.expr(a, effect, ret, out)
			name := g.temp()
			out.WriteString(name + " := " + expr + "\n")
			args = append(args, name)
		}
		if e.ResolvedFunction != nil && e.ResolvedFunction.Owner == "module" {
			return e.ResolvedFunction.goEmissionName() + "(" + strings.Join(args, ", ") + ")"
		}
		if e.Left.Kind == "name" {
			return "efFunction_" + e.Left.Name + "(" + strings.Join(args, ", ") + ")"
		}
		return "efCall_" + e.Left.Left.Name + "_" + e.Left.Name + "(" + strings.Join(args, ", ") + ")"
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
		body.WriteString("if " + condition + " {\n" + g.blockType(e.Then, effect, valueType) + "} else {\n" + g.blockType(e.Else, effect, valueType) + "}\n")
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
		out.WriteString("type efTemplate_" + r.EmissionName + params + " interface { " + marker + "(" + strings.Join(arguments, ",") + ") }\n")
		for _, variant := range r.Variants {
			name := goVariantType("template_"+r.EmissionName, variant.Name)
			out.WriteString("type " + name + params + " struct {\n")
			fields(variant.Fields)
			out.WriteString("}\nfunc (" + name + args + ") " + marker + "(" + strings.Join(arguments, ",") + ") {}\n")
		}
		return out.String()
	}
	out.WriteString("type efTemplate_" + r.EmissionName + params + " struct {\n")
	fields(r.Fields)
	out.WriteString("}\n")
	return out.String()
}

func (g *goEmitter) dataTypes(out *strings.Builder) {
	for _, r := range g.program.BundledTemplates {
		out.WriteString(goTemplateDeclaration(r))
	}
	for _, r := range append(append([]*DataDeclaration{}, g.program.Records...), g.program.Enums...) {
		if len(r.Parameters) > 0 {
			out.WriteString(goTemplateDeclaration(r))
		}
	}
	for _, declaration := range g.programDeclarations() {
		switch declaration.Kind {
		case "record", "error":
			out.WriteString("type efType_" + goIdent(declaration.Name) + " struct {\n")
			for _, field := range declaration.Fields {
				out.WriteString(goFieldName(field.Name) + " " + goSourceType(field.sourceType, field.Type) + "\n")
			}
			out.WriteString("}\n")
		case "enum":
			out.WriteString("type efType_" + goIdent(declaration.Name) + " interface { efVariant_" + goIdent(declaration.Name) + "() }\n")
			for _, variant := range declaration.Variants {
				out.WriteString("type " + goVariantType(declaration.Name, variant.Name) + " struct {\n")
				for _, field := range variant.Fields {
					out.WriteString(goFieldName(field.Name) + " " + goSourceType(field.sourceType, field.Type) + "\n")
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
		parts = append(parts, goFieldName(field.Name)+":"+g.expr(field.Value, false, ret, out))
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
func (g *goEmitter) match(e *Expr, effect bool, ret string, out *strings.Builder) string {
	value := g.expr(e.Left, effect, ret, out)
	resultType := g.valueType(e)
	variant := g.temp()
	var body strings.Builder
	body.WriteString(variant + " := " + value + "\n")
	if effect {
		body.WriteString("switch efMatch := " + variant + ".(type) {\n")
		for _, arm := range e.Arms {
			body.WriteString("case " + g.variantType(e.Left.checked.valueID(), arm.Pattern.ResolvedEnum, arm.Pattern.VariantName) + ":\n")
			body.WriteString("_ = efMatch\n")
			for _, field := range sortedBindingNames(arm.Pattern.Bindings) {
				binding := arm.Pattern.Bindings[field]
				if binding != "_" {
					body.WriteString("efLocal_" + binding + " := efMatch." + goFieldName(field) + "\n_ = efLocal_" + binding + "\n")
				}
			}
			body.WriteString(g.blockType(arm.Body, true, resultType))
		}
		if len(e.Arms) == 0 {
			body.WriteString("default: _ = efMatch; return efExit[" + resultType + "]{Defect:fmt.Errorf(\"unreachable empty match\")}\n}\n")
		} else {
			body.WriteString("default: return efExit[" + resultType + "]{Defect:fmt.Errorf(\"unreachable non-exhaustive match\")}\n}\n")
		}
		name := g.temp()
		out.WriteString(name + " := func() efExit[" + resultType + "] {\n" + body.String() + "}()\n" + g.failed(name, ret))
		return name + ".Value"
	}
	body.WriteString("switch efMatch := " + variant + ".(type) {\n")
	for _, arm := range e.Arms {
		body.WriteString("case " + g.variantType(e.Left.checked.valueID(), arm.Pattern.ResolvedEnum, arm.Pattern.VariantName) + ":\n")
		body.WriteString("_ = efMatch\n")
		for _, field := range sortedBindingNames(arm.Pattern.Bindings) {
			binding := arm.Pattern.Bindings[field]
			if binding != "_" {
				body.WriteString("efLocal_" + binding + " := efMatch." + goFieldName(field) + "\n_ = efLocal_" + binding + "\n")
			}
		}
		body.WriteString(g.blockType(arm.Body, false, resultType))
	}
	if len(e.Arms) == 0 {
		body.WriteString("default: _ = efMatch; panic(\"unreachable empty match\")\n}\n")
	} else {
		body.WriteString("default: panic(\"unreachable non-exhaustive match\")\n}\n")
	}
	return "func() " + resultType + " {\n" + body.String() + "}()"
}

// WriteRuntime writes only changed source bytes so Go's own build cache remains useful.
func WriteRuntime(directory string) error {
	path := filepath.Join(directory, "runtime")
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	for name, data := range rt.Sources() {
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

func (g *goEmitter) foreign(e *Expr, effect bool, ret string, out *strings.Builder) string {
	b := g.bindings[e.Name]
	args := []string{}
	if b.Context {
		args = append(args, "fc.Context()")
	}
	for _, arg := range e.Args {
		expr := g.expr(arg, effect, ret, out)
		temp := g.temp()
		out.WriteString(temp + " := " + expr + "\n")
		args = append(args, temp)
	}
	call := "efGo_" + e.Name + "(" + strings.Join(args, ",") + ")"
	result := g.resultType(e)
	body := "return er.Succeed(" + call + ")"
	if b.Return == voidTypeName {
		body = call + ";return er.Succeed(struct{}{})"
	}
	if b.HasError {
		if b.Return == voidTypeName {
			body = "nativeErr := " + call + ";return er.Succeed(er.GoResult[struct{}]{Value:struct{}{},Err:nativeErr})"
		} else {
			body = "nativeValue,nativeErr := " + call + ";return er.Succeed(er.GoResult[" + goType(b.Return) + "]{Value:nativeValue,Err:nativeErr})"
		}
	}
	return "func(ctx efContext)efExit[" + result + "]{if ctx.s_Foreign==nil{return er.Die[" + result + "](fmt.Errorf(\"missing Foreign provider\"))};return er.Invoke(ctx.Runtime,func(fc *er.FiberContext)er.Exit[" + result + "]{" + body + "})}"
}
