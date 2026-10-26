package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const callbackRouteSource = `error Missing
service Users {effect fn get(path:string)->string raises {Missing}}
impl Memory for Users {effect fn get(path:string)->string raises {Missing}{path}}
effect fn route(request:HttpRequest)->HttpReply raises {Missing} uses {Users}{let name=run Users.get(request.path);HttpReply.NotFound {}}
fn limits()->HttpLimits{HttpLimits{maxBodyBytes:16,readHeaderMillis:1000,readBodyMillis:1000,idleMillis:1000,maxActive:4}}
`

func TestHandlerErasureBoundariesRetainFailuresAndServices(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"alias", `effect fn main()->void raises {IoError}{let chosen=route;run Http.listen("127.0.0.1:0",limits(),chosen).provide<Http>(LiveHttp)}`, "EF108"},
		{"helper", `fn identity(h:HttpHandler)->HttpHandler{h} effect fn main()->void raises {IoError}{let chosen=identity(route);run Http.listen("127.0.0.1:0",limits(),chosen).provide<Http>(LiveHttp)}`, "EF106"},
		{"conditional", `effect fn main()->void raises {IoError}{let chosen=if true {route}else{route};run Http.listen("127.0.0.1:0",limits(),chosen).provide<Http>(LiveHttp)}`, "EF108"},
		{"record empty alias", `record Routes {handler:HttpHandler} effect fn main()->void raises {IoError}{let routes=Routes{handler:route};run Http.listen("127.0.0.1:0",limits(),routes.handler).provide<Http>(LiveHttp).provide<Users>(Memory)}`, "EF115"},
		{"record complete callback", `record Routes {handler:effect fn(HttpRequest)->HttpReply raises {Missing} uses {Users}} effect fn main()->void raises {IoError}{let routes=Routes{handler:route};run Http.listen("127.0.0.1:0",limits(),routes.handler).provide<Http>(LiveHttp)}`, "EF108"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(callbackRouteSource + test.body)
			if r.Checked || !hasCode(r, test.code) {
				t.Fatalf("callback rows erased: %+v", r.Diagnostics)
			}
		})
	}
}

func TestHTTPTransportPolicyIsVisibleInCheckedApplication(t *testing.T) {
	source := callbackRouteSource + `effect fn main()->void{let pending=Http.listen("127.0.0.1:0",limits(),route).provide<Users>(Memory).provide<Http>(LiveHttp);void}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "listen("))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(info.Type)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"callbackPolicies"`) {
		t.Fatalf("transport typed-failure policy missing: %s", encoded)
	}
	policy := info.Type.Application.CallbackPolicies
	if len(policy) != 1 || policy[0].Parameter != 2 || policy[0].Kind != "typed-failure-response" || !policy[0].PropagateRequirements || !slices.Equal(policy[0].AbsorbedFailures, []string{"Missing"}) {
		t.Fatalf("wrong transport policy: %+v", policy)
	}
	if !slices.Equal(info.Type.Errors, []string{"IoError"}) || !slices.Equal(info.Type.Services, []string{"Http", "Users"}) {
		t.Fatalf("transport contract erased service rows: %+v", info.Type)
	}
	if p := r.ProjectValues([]ValueType{info.Type}); !p.Complete {
		t.Fatal(p.Error)
	}
}

func TestUnsupportedCallableFormsDiagnoseAtTheirSourceBoundary(t *testing.T) {
	for _, test := range []struct{ source, message string }{
		{`effect fn main()->void{let callback=fn(x:string)->string{x};void}`, "anonymous functions and closure captures"},
		{`effect fn generic<E: raises>(cb:effect fn(string)->string raises {E})->string raises {E}{run cb("x")} effect fn main()->void{let callback=generic;void}`, "first-class polymorphic values are unsupported"},
	} {
		t.Run(test.message, func(t *testing.T) {
			r := Compile(test.source)
			found := false
			for _, d := range r.Diagnostics {
				found = found || strings.Contains(d.Message, test.message)
			}
			if r.Checked || !found {
				t.Fatalf("unsupported source admitted: %+v", r.Diagnostics)
			}
			if _, err := r.EmitGo(); err == nil {
				t.Fatal("unsupported syntax reached emission")
			}
		})
	}
}

func TestClosureDiagnosticPreservesExistingCallableIdentifiers(t *testing.T) {
	for _, name := range []string{"fn", "effect"} {
		r := Compile("fn " + name + "()->string{\"name\"} effect fn main()->string{" + name + "()}")
		if !r.Checked {
			t.Fatalf("existing named call confused with anonymous function: %+v", r.Diagnostics)
		}
	}
}

func TestNestedCallbackRowsUseTheCommonInferredBinding(t *testing.T) {
	prefix := `error A error B
effect fn onlyA(cb:effect fn()->string raises {A})->string raises {A}{run cb()}
effect fn wider(cb:effect fn()->string raises {A,B})->string raises {A}{run cb().catch<B>("fallback")}
effect fn failsB()->string raises {B}{"B"}
effect fn apply<E: raises>(a:effect fn(effect fn()->string raises {E})->string raises {E},b:effect fn()->string raises {E})->string raises {E}{run a(b)}
`
	if r := Compile(prefix + `effect fn main()->void{let pending=apply(onlyA,failsB);void}`); r.Checked || !hasCode(r, "EF106") {
		t.Fatalf("nested contravariance source control: %+v", r.Diagnostics)
	}
	if r := Compile(prefix + `effect fn main()->void{let pending=apply(wider,failsB);void}`); !r.Checked {
		t.Fatalf("wider input source control: %+v", r.Diagnostics)
	}
}

func TestNestedServiceRowsUseTheCommonInferredBinding(t *testing.T) {
	prefix := `service A {effect fn get()->string}
service B {effect fn get()->string}
impl MemoryB for B {effect fn get()->string{"B"}}
effect fn onlyA(cb:effect fn()->string uses {A})->string uses {A}{run cb()}
effect fn wider(cb:effect fn()->string uses {A,B})->string uses {A}{run cb().provide<B>(MemoryB)}
effect fn needsB()->string uses {B}{"B"}
effect fn apply<R: uses>(a:effect fn(effect fn()->string uses {R})->string uses {R},b:effect fn()->string uses {R})->string uses {R}{run a(b)}
`
	if r := Compile(prefix + `effect fn main()->void{let pending=apply(onlyA,needsB);void}`); r.Checked || !hasCode(r, "EF106") {
		t.Fatalf("nested service source control: %+v", r.Diagnostics)
	}
	if r := Compile(prefix + `effect fn main()->void{let pending=apply(wider,needsB);void}`); !r.Checked {
		t.Fatalf("wider service input source control: %+v", r.Diagnostics)
	}
}

func TestNestedCallableResultRowsRemainCovariant(t *testing.T) {
	prefix := `error A error B
effect fn resultA()->string raises {A}{"A"}
effect fn resultB()->string raises {B}{"B"}
effect fn makeA()->(effect fn()->string raises {A}) raises {A}{resultA}
effect fn factory<E: raises>(make:effect fn()->(effect fn()->string raises {E}) raises {E},other:effect fn()->string raises {E})->(effect fn()->string raises {E}) raises {E}{run make()}
`
	r := Compile(prefix + `effect fn main()->void{let pending=factory(makeA,resultB);void}`)
	if !r.Checked {
		t.Fatalf("nested result covariance source control: %+v", r.Diagnostics)
	}
}

func TestCallableSyntaxLimitsPreserveTheirExactBoundary(t *testing.T) {
	for _, count := range []int{256, 257} {
		parameters := strings.TrimSuffix(strings.Repeat("string,", count), ",")
		r := Compile("fn store(cb:fn(" + parameters + ")->string)->void{void} effect fn main()->void{void}")
		if count == 256 && !r.Checked {
			t.Fatalf("admitted callable width refused: %+v", r.Diagnostics)
		}
		if count == 257 && (r.Checked || !hasCode(r, "EF002")) {
			t.Fatalf("callable width bound skipped at final parameter: %+v", r.Diagnostics)
		}
	}
	for _, count := range []int{8, 9} {
		rows, params := []string{}, []string{}
		for i := 0; i < count; i++ {
			rows = append(rows, fmt.Sprintf("E%d: raises", i))
			params = append(params, fmt.Sprintf("cb%d:effect fn()->string raises {E%d}", i, i))
		}
		r := Compile("fn store<" + strings.Join(rows, ",") + ">(" + strings.Join(params, ",") + ")->void{void} effect fn main()->void{void}")
		if count == 8 && !r.Checked {
			t.Fatalf("admitted row parameter count refused: %+v", r.Diagnostics)
		}
		if count == 9 && (r.Checked || !hasCode(r, "EF002")) {
			t.Fatalf("row parameter count exceeded without diagnostic: %+v", r.Diagnostics)
		}
	}
}
