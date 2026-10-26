package compiler

import (
	"slices"
	"strings"
	"testing"
)

const httpTransportRouteSource = `error Missing
service Users {effect fn get(path:string)->string raises {Missing}}
impl Memory for Users {effect fn get(path:string)->string raises {Missing}{path}}
effect fn route(request:HttpRequest)->HttpReply raises {Missing} uses {Users, Http} {
 let name = run Users.get(request.path)
 let body = run Http.text(name)
 HttpReply.Respond {response: HttpResponse {status: 200, contentType: request.contentType, body: body}}
}
fn limits()->HttpLimits {HttpLimits {maxBodyBytes: 16, readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 4}}
`

func TestHTTPListenRetainsHandlerRowsAndAbsorbedFailures(t *testing.T) {
	source := httpTransportRouteSource + `effect fn main()->void{let pending=Http.listen("127.0.0.1:0",limits(),route).provide<Users>(Memory).provide<Http>(LiveHttp);void}`
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatalf("go: %+v", r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "listen("))
	if err != nil {
		t.Fatal(err)
	}
	policy := info.Type.Application.CallbackPolicies
	if len(policy) != 1 || policy[0].Parameter != 2 || policy[0].Kind != "typed-failure-response" || !slices.Equal(policy[0].AbsorbedFailures, []string{"Missing"}) {
		t.Fatalf("go: wrong transport policy: %+v", policy)
	}
	if !slices.Equal(info.Type.Errors, []string{"IoError"}) || !slices.Equal(info.Type.Services, []string{"Http", "Users"}) {
		t.Fatalf("go: transport contract erased handler rows: %+v", info.Type)
	}
	missing := CompileFor(httpTransportRouteSource+`effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",limits(),route).provide<Http>(LiveHttp)}`, "go")
	if missing.Checked || !hasCode(missing, "EF108") {
		t.Fatalf("handler service requirement was not propagated: %+v", missing.Diagnostics)
	}
}

func TestHTTPListenRejectsHandlerShapesAndShadowedContracts(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"result", `effect fn route(request:HttpRequest)->string{request.path} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"parameter", `effect fn route(path:string)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"pure", `fn route(request:HttpRequest)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"limits", `effect fn route(request:HttpRequest)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",16,route).provide<Http>(LiveHttp)}`, "EF106"},
		{"shadowed record", `record HttpRequest {path:string} effect fn main()->void{let pending=Http.text("x"); void}`, "EF101"},
		{"shadowed record by row", `record HttpRequest {path:string} effect fn label()->bytes uses {Http} {run Http.text("x")} effect fn main()->void{void}`, "EF101"},
		{"shadowed record by provider", `record HttpResponse {path:string} effect fn main()->void{let provider=LiveHttp; void}`, "EF101"},
		{"shadowed record by outer use", `record HttpRequest {path:string} effect fn main()->void{let pending=Http.text("x"); let Http="local"; void}`, "EF101"},
		{"shadowed record by another function's row", `record HttpRequest {path:string} fn wrap<Http: uses>(task: effect fn() -> string uses { Http }) -> void {void} effect fn label()->bytes uses {Http} {run Http.text("x")} effect fn main()->void{void}`, "EF101"},
		{"shadowed record by a row of another kind", `record HttpRequest {path:string} fn wrap<Http: raises>(task: effect fn() -> string uses { Http }) -> void {void} effect fn main()->void{void}`, "EF101"},
		{"shadowed callback", `record HttpHandler {path:string} effect fn main()->void{let pending=Http.text("x"); void}`, "EF101"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(test.source)
			if r.Checked || !hasCode(r, test.code) {
				t.Fatalf("expected %s: %+v", test.code, r.Diagnostics)
			}
		})
	}
}

// Admission follows lexically resolved references: a local binding named Http
// or LiveHttp, value or row, is not a reference, so it neither reserves the
// builtin data names nor emits any transport. A row parameter binds throughout
// its function, including explicit constructor types in its body.
func TestHTTPContractIsAdmittedOnlyWhenReferenced(t *testing.T) {
	for _, source := range []string{
		`record HttpRequest {path:string} effect fn main()->string{let label="Http"; label}`,
		`record HttpRequest { path: string }
effect fn main() -> string {
    let Http = "local"
    Http
}`,
		`record HttpRequest {path:string} fn echo(LiveHttp:string)->string{LiveHttp} effect fn main()->string{echo("x")}`,
		`record HttpRequest {path:string} enum Box {Item {Http: string}} fn open(box:Box)->string{match box {Box.Item {Http} => Http}} effect fn main()->string{open(Box.Item {Http: "x"})}`,
		boxedRow("Http", "Box<string, effect fn() -> string uses { Http }>"),
		boxedRow("Row", "Box<string, effect fn() -> string uses { Row }>"),
		boxedRow("Http", "Box"),
	} {
		r := CompileFor(source, "go")
		if !r.Checked {
			t.Fatalf("go: %+v\n%s", r.Diagnostics, source)
		}
		for _, declaration := range r.Declarations {
			if declaration.Source == builtinDataSourceID {
				t.Fatalf("go: unreferenced Http contract was admitted: %+v\n%s", r.Declarations, source)
			}
		}
		code := emitHTTPProgram(t, r)
		if strings.Contains(code, "efProvider_LiveHttp") || strings.Contains(code, "efService_Http") {
			t.Fatalf("go: unreferenced Http contract was emitted\n%s", source)
		}
	}
}

// The contract is admitted for its interface alone; the transport
// implementation is emitted only where checked code holds LiveHttp.
func TestHTTPImplementationIsEmittedOnlyForCheckedProviderReferences(t *testing.T) {
	library := `effect fn greeting()->bytes uses {Http} {run Http.text("hi")} `
	for _, test := range []struct {
		main        string
		implemented bool
	}{
		{`effect fn main()->void{void}`, false},
		{`effect fn main()->void raises {IoError}{let body=run greeting().provide<Http>(LiveHttp); void}`, true},
	} {
		r := CompileFor(library+test.main, "go")
		if !r.Checked {
			t.Fatalf("go: %+v", r.Diagnostics)
		}
		code := emitHTTPProgram(t, r)
		implemented := strings.Contains(code, "efProvider_LiveHttp")
		// The plan prunes the unreachable service, so only the implemented case
		// must name Http at all.
		if implemented != test.implemented || test.implemented && !strings.Contains(code, "Http") {
			t.Fatalf("go: implementation emitted %v, want %v\n%s", implemented, test.implemented, test.main)
		}
		buildGeneratedGo(t, r, GoGenerationBuild)
	}
}

// Http.listen is the only builtin HTTP server operation: the retired raw
// path-to-text serve is unknown on every target, and its Handler shorthand
// is an ordinary user name again.
func TestHTTPServeIsRetired(t *testing.T) {
	retired := CompileFor(`effect fn route(path:string)->string{path} effect fn main()->void raises {IoError}{run Http.serve("127.0.0.1:0",route).provide<Http>(LiveHttp)}`, "go")
	if retired.Checked || !hasCode(retired, "EF102") {
		t.Fatalf("go: retired Http.serve was admitted: %+v", retired.Diagnostics)
	}
	if r := CompileFor(`record Handler {path:string} effect fn main()->void{let pending=Http.text("x"); void}`, "go"); !r.Checked {
		t.Fatalf("go: Handler is still a reserved builtin name: %+v", r.Diagnostics)
	}
}

// boxedRow stores a row-polymorphic callback through a constructor written
// with the given head inside the function that binds the row.
func boxedRow(row, constructor string) string {
	return `record HttpRequest { path: string }
record Box<A: type, F: callable effect fn() -> A> { callback: F }
fn wrap<` + row + `: uses>(task: effect fn() -> string uses { ` + row + ` }) -> void {
    let boxed = ` + constructor + ` {
        callback: task
    }
    void
}
effect fn main() -> void { void }`
}

func emitHTTPProgram(t *testing.T, r *Result) string {
	t.Helper()
	code, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestHTTPDataPreludeHasNoSourceSpans(t *testing.T) {
	r := Compile(`effect fn main()->void{let pending=Http.text("x"); void}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	names := []string{}
	for _, declaration := range r.Declarations {
		if declaration.Source != builtinDataSourceID {
			continue
		}
		names = append(names, declaration.Name)
		if declaration.Span != (Span{}) {
			t.Fatalf("builtin declaration carries a source span: %+v", declaration)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"HttpLimits", "HttpReply", "HttpRequest", "HttpResponse"}) {
		t.Fatalf("builtin HTTP data: %v", names)
	}
}

// HttpHandler depends on HttpRequest and HttpReply, so it is part of the
// reference-admitted Http contract: without a reference to Http it is an
// ordinary unknown type, and the name stays free for user data, as with its
// sibling data names.
func TestHttpHandlerIsAdmittedOnlyWithHttp(t *testing.T) {
	r := CompileFor(`fn discard(h: HttpHandler) -> void { void }
effect fn main() -> void { void }`, "go")
	if r.Checked {
		t.Fatal("go: unadmitted HttpHandler was accepted")
	}
	found := false
	for _, d := range r.Diagnostics {
		found = found || strings.Contains(d.Message, "HttpHandler")
	}
	if !found {
		t.Fatalf("go: no unknown-type diagnostic names HttpHandler: %+v", r.Diagnostics)
	}
	r = CompileFor(`record HttpHandler { path: string }
effect fn main() -> void { void }`, "go")
	if !r.Checked {
		t.Fatalf("go: record HttpHandler without Http: %+v", r.Diagnostics)
	}
	// Referenced Http still reserves the callback name.
	r = CompileFor(`record HttpHandler { path: string }
effect fn main() -> void { let pending = Http.text("x"); void }`, "go")
	reserved := false
	for _, d := range r.Diagnostics {
		reserved = reserved || (d.Code == "EF101" && strings.Contains(d.Message, "HttpHandler"))
	}
	if !reserved {
		t.Fatalf("go: HttpHandler is not reserved once Http is referenced: %+v", r.Diagnostics)
	}
}

// Without a reference to Http, HttpHandler is an ordinary user name: a record
// declared with it is used as a parameter type, constructed, read and built.
// The callback classification must not leak into the checked or emitted type
// of a user declaration.
func TestUserHttpHandlerRecordIsAnOrdinaryTypeWithoutHttp(t *testing.T) {
	source := `record HttpHandler { path: string }
fn pathOf(h: HttpHandler) -> string { h.path }
effect fn main() -> void {
    run Console.log(pathOf(HttpHandler { path: "user-record" })).provide<Console>(Stdout)
}`
	goResult := CompileFor(source, "go")
	if !goResult.Checked {
		t.Fatalf("go: %+v", goResult.Diagnostics)
	}
	if output := runGeneratedGo(t, goResult); output != "user-record\n" {
		t.Fatalf("go output %q", output)
	}
}
