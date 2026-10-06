package compiler

func builtins() []*Service {
	method := func(name, ret string, params []Param, failures ...string) *Function {
		return &Function{Name: name, Return: ret, Params: params, Effect: true, Errors: failures}
	}
	p := func(name, typ string) Param { return Param{Name: name, Type: typ} }
	return []*Service{
		{Name: "Assert", Methods: []*Function{method("check", "()", []Param{p("condition", "bool"), p("message", "string")}, "AssertionFailed"), method("equalText", "()", []Param{p("actual", "string"), p("expected", "string")}, "AssertionFailed")}},
		{Name: "Console", Methods: []*Function{method("log", "()", []Param{p("message", "string")})}},
		{Name: "Clock", Methods: []*Function{method("sleep", "()", []Param{p("milliseconds", "i64")})}},
		{Name: "Files", Methods: []*Function{method("openRead", "File", []Param{p("path", "string")}, "IoError"), method("readText", "string", []Param{p("file", "File")}, "IoError"), method("readFile", "string", []Param{p("path", "string")}, "IoError")}},
		{Name: "Env", Methods: []*Function{method("get", "string", []Param{p("name", "string")})}},
		{Name: "Runtime", Methods: []*Function{method("inspect", "string", nil)}},
		{Name: "Foreign"},
		{Name: "Http", Methods: []*Function{method("serve", "()", []Param{p("address", "string"), p("handler", "Handler")}, "IoError")}},
	}
}
func builtinProviders() []*Provider {
	return []*Provider{{Name: "Assertions", Service: "Assert"}, {Name: "Stdout", Service: "Console"}, {Name: "LiveClock", Service: "Clock"}, {Name: "LiveFiles", Service: "Files"}, {Name: "LiveEnv", Service: "Env"}, {Name: "RuntimeLive", Service: "Runtime"}, {Name: "Host", Service: "Foreign"}, {Name: "GoHttp", Service: "Http"}}
}
func builtinErrors() []string { return []string{"IoError", "Timeout", "GoError", "AssertionFailed"} }
