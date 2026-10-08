package compiler

import (
	_ "embed"
	"slices"

	rt "effra.local/prototype/runtime/effra"
)

// Builtin services and providers name the native runtime modules their
// generated declarations reference. A service lists modules required by its
// operation signatures; a provider lists modules required by its
// implementation. Application reachability selects these modules only when
// the declaration itself is reachable, so checking against a builtin contract
// never roots its implementation.
func builtins() []*Service {
	method := func(name, ret string, params []Param, failures ...string) *Function {
		return &Function{Name: name, Return: ret, Params: params, Effect: true, Errors: failures}
	}
	p := func(name, typ string) Param { return Param{Name: name, Type: typ} }
	listen := method("listen", voidTypeName, []Param{p("address", "string"), p("limits", "HttpLimits"), p("handler", "HttpHandler")}, "IoError")
	listen.CallbackPolicies = []CallbackPolicy{{Parameter: 2, Kind: "typed-failure-response", PropagateRequirements: true}}
	return []*Service{
		{Name: "Assert", Methods: []*Function{method("check", voidTypeName, []Param{p("condition", "bool"), p("message", "string")}, "AssertionFailed"), method("equalText", voidTypeName, []Param{p("actual", "string"), p("expected", "string")}, "AssertionFailed")}},
		{Name: "Console", Methods: []*Function{method("log", voidTypeName, []Param{p("message", "string")})}},
		{Name: "Clock", Methods: []*Function{method("sleep", voidTypeName, []Param{p("milliseconds", "i64")})}, native: []rt.RuntimeModule{rt.RuntimeModuleCore}},
		{Name: "Scheduler", Methods: []*Function{method("sleep", voidTypeName, []Param{p("milliseconds", "i64")}), method("advance", voidTypeName, []Param{p("milliseconds", "i64")}), method("awaitRegistration", voidTypeName, nil)}, native: []rt.RuntimeModule{rt.RuntimeModuleCore}},
		{Name: "Sync", Methods: []*Function{method("latch", "Latch", nil), method("await", voidTypeName, []Param{p("latch", "Latch")}), method("signal", voidTypeName, []Param{p("latch", "Latch")})}, native: []rt.RuntimeModule{rt.RuntimeModuleSync}},
		{Name: "Files", Methods: []*Function{method("openRead", "File", []Param{p("path", "string")}, "IoError"), method("readText", "string", []Param{p("file", "File")}, "IoError"), method("readFile", "string", []Param{p("path", "string")}, "IoError")}, native: []rt.RuntimeModule{rt.RuntimeModuleFiles}},
		{Name: "Env", Methods: []*Function{method("get", "string", []Param{p("name", "string")})}},
		{Name: "Runtime", Methods: []*Function{method("inspect", "string", nil)}},
		{Name: "Foreign"},
		{Name: "Http", byReference: true, Methods: []*Function{listen, method("text", "bytes", []Param{p("text", "string")})}},
	}
}
func builtinProviders() []*Provider {
	native := func(modules ...rt.RuntimeModule) []rt.RuntimeModule { return modules }
	return []*Provider{
		{Name: "Assertions", Service: "Assert", native: native(rt.RuntimeModuleCore)},
		{Name: "Stdout", Service: "Console", native: native(rt.RuntimeModuleConsole)},
		{Name: "LiveClock", Service: "Clock", native: native(rt.RuntimeModuleCore)},
		{Name: "TestClock", Service: "Clock", native: native(rt.RuntimeModuleCore)},
		{Name: "LiveScheduler", Service: "Scheduler", native: native(rt.RuntimeModuleCore)},
		{Name: "TestScheduler", Service: "Scheduler", native: native(rt.RuntimeModuleCore)},
		{Name: "TestSync", Service: "Sync", native: native(rt.RuntimeModuleSync)},
		{Name: "LiveFiles", Service: "Files", native: native(rt.RuntimeModuleFiles)},
		{Name: "LiveEnv", Service: "Env", native: native(rt.RuntimeModuleEnv)},
		{Name: "RuntimeLive", Service: "Runtime", native: native(rt.RuntimeModuleInspect)},
		{Name: "Host", Service: "Foreign"},
		{Name: "LiveHttp", Service: "Http", native: native(rt.RuntimeModuleHTTP)},
	}
}
func builtinErrors() []string { return []string{"IoError", "Timeout", "GoError", "AssertionFailed"} }

// builtinCallbacks are the finite callback contracts of builtin operations.
// Each canonicalizes to an effectful callable from Parameter to Result. An
// operation's typed-failure-response policy accepts any effectful callable
// with exactly that signature and records the callable's own rows.
var builtinCallbacks = map[string]struct{ Parameter, Result string }{
	"HttpHandler": {"HttpRequest", "HttpReply"},
}

// callback is the one admission predicate for builtin callback names: a name
// is the builtin callback only where the program admits the contract it
// belongs to. The checker's type resolution and both emitters ask it, so they
// cannot disagree about whether a user declaration of the same name exists.
func (p *Program) callback(name string) (struct{ Parameter, Result string }, bool) {
	callback, ok := builtinCallbacks[name]
	return callback, ok && p.admitsHTTP()
}

// admitsHTTP reports whether the program refers to the global Http or
// LiveHttp, as resolved by resolveBindings: a local binding of either name,
// including a row parameter, is not a reference. The Http service, its
// provider and its data form one contract admitted only on such a reference,
// so other programs neither reserve its data names nor inspect it.
func (p *Program) admitsHTTP() bool {
	return p != nil && (p.references["Http"] || p.references["LiveHttp"])
}

// builtinProviderByReference reports whether a builtin provider implements a
// contract admitted only by reference. Such a provider is not part of a
// library's standing surface: plan reachability from the library's roots
// retains it exactly where checked code selects it.
func builtinProviderByReference(provider *Provider) bool {
	return slices.ContainsFunc(builtins(), func(s *Service) bool { return s.byReference && s.Name == provider.Service })
}

// builtinServicesFor and builtinProvidersFor select the prelude one program
// admits. Callback type names stay reserved like other builtin type names.
func builtinServicesFor(program *Program) []*Service {
	services := builtins()
	if program.admitsHTTP() {
		return services
	}
	return slices.DeleteFunc(services, func(s *Service) bool { return s.Name == "Http" })
}

func builtinProvidersFor(program *Program) []*Provider {
	providers := builtinProviders()
	if program.admitsHTTP() {
		return providers
	}
	return slices.DeleteFunc(providers, func(p *Provider) bool { return p.Service == "Http" })
}

// builtinDataSource is the Http service's request/response data contract. It
// belongs to the finite compatibility prelude of that existing builtin.
//
//go:embed builtin/http.ef
var builtinDataSource string

const builtinDataSourceID = "builtin:http"

// addBuiltinData registers the prelude's nominal data ahead of source
// declarations, so a colliding source declaration diagnoses at its own span.
// The declarations carry no source spans and never enter lexical tooling.
func addBuiltinData(program *Program) {
	if !program.admitsHTTP() {
		return
	}
	for _, data := range append(append([]*DataDeclaration{}, program.Records...), program.Enums...) {
		if data.SourceID == builtinDataSourceID {
			return
		}
	}
	builtin, diagnostics := parse(builtinDataSource)
	if len(diagnostics) != 0 {
		panic("invalid builtin data source: " + diagnostics[0].Message)
	}
	for _, data := range append(append([]*DataDeclaration{}, builtin.Records...), builtin.Enums...) {
		data.SourceID, data.Span = builtinDataSourceID, Span{}
		for i := range data.Fields {
			data.Fields[i].Span = Span{}
		}
		for i := range data.Variants {
			data.Variants[i].Span = Span{}
			for j := range data.Variants[i].Fields {
				data.Variants[i].Fields[j].Span = Span{}
			}
		}
	}
	program.Records = append(builtin.Records, program.Records...)
	program.Enums = append(builtin.Enums, program.Enums...)
}
