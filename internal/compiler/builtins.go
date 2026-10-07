package compiler

import rt "effra.local/prototype/runtime/effra"

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
	serve := method("serve", voidTypeName, []Param{p("address", "string"), p("handler", "Handler")}, "IoError")
	serve.CallbackPolicies = []CallbackPolicy{{Parameter: 1, Kind: "typed-failure-response", PropagateRequirements: true}}
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
		{Name: "Http", Methods: []*Function{serve}},
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
		{Name: "GoHttp", Service: "Http", native: native(rt.RuntimeModuleHTTP)},
	}
}
func builtinErrors() []string { return []string{"IoError", "Timeout", "GoError", "AssertionFailed"} }
