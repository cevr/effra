package compiler

import (
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

func endpointInspection(t *testing.T, r *Result, name string) EndpointInspection {
	t.Helper()
	for _, endpoint := range r.Endpoints {
		if endpoint.Name == name {
			return endpoint
		}
	}
	t.Fatalf("no endpoint %s: %+v", name, r.Endpoints)
	return EndpointInspection{}
}

// An endpoint's primary rows keep every service its input decoding, handler
// and output encoding need, and only the terminal encoding failure: mapped
// failures are answered. A route table's rows are the union of its
// endpoints'. Inspection names each mapped failure's origins.
func TestEndpointRowsAndInspection(t *testing.T) {
	r := exampleCompile(t, "http-endpoints.ef")
	place := endpointInspection(t, r, "placeOrder")
	if place.Kind != "endpoint" || place.Method != "POST" || place.Path != "/orders" || place.Status != 201 || place.Input != "orderJson" || place.Output != "receiptJson" {
		t.Fatalf("placeOrder: %+v", place)
	}
	if !slices.Equal(place.Failures, []string{"JsonEncodeFailure"}) || !slices.Equal(place.Requirements, []string{"Directory", "Inventory", "Ledger"}) {
		t.Fatalf("placeOrder rows: %v %v", place.Failures, place.Requirements)
	}
	if place.Terminal == nil || place.Terminal.Failure != "JsonEncodeFailure" || place.Terminal.Status != 500 || place.Malformed == nil || place.Malformed.Status != 400 || place.Malformed.UnsupportedType != 415 {
		t.Fatalf("placeOrder policies: %+v %+v", place.Terminal, place.Malformed)
	}
	origins := map[string]string{}
	for _, response := range place.Responses {
		if len(response.Origins) != 1 {
			t.Fatalf("response %+v", response)
		}
		origins[response.Failure] = response.Origins[0].Stage + ":" + response.Origins[0].Witness + ":" + response.Body
	}
	want := map[string]string{"UnknownMember": "input:memberJson:unknownMemberJson", "OutOfStock": "handler::outOfStockJson", "Unpublishable": "output:totalJson:unpublishableJson"}
	for failure, origin := range want {
		if origins[failure] != origin {
			t.Fatalf("origins %v, want %v", origins, want)
		}
	}
	cancel := endpointInspection(t, r, "cancelOrder")
	if len(cancel.Failures) != 0 || cancel.Terminal != nil || cancel.Malformed != nil || len(cancel.Responses) != 1 || cancel.Responses[0].Body != "" {
		t.Fatalf("an endpoint encoding no body has no terminal policy: %+v", cancel)
	}
	api := endpointInspection(t, r, "api")
	if api.Kind != "routes" || !slices.Equal(api.Routes, []string{"placeOrder", "cancelOrder", "health"}) || !slices.Equal(api.Failures, []string{"JsonEncodeFailure"}) || !slices.Equal(api.Requirements, []string{"Directory", "Inventory", "Ledger"}) {
		t.Fatalf("api: %+v", api)
	}
	failureWitnesses := 0
	for _, codec := range r.Codecs {
		if codec.Failure {
			failureWitnesses++
			if codec.Decode != nil || codec.Encode != nil || codec.Plan == "" {
				t.Fatalf("failure witness %+v", codec)
			}
		}
	}
	if failureWitnesses != 3 {
		t.Fatalf("failure witnesses: %d", failureWitnesses)
	}
	for _, plan := range r.CodecPlans {
		root := plan.Nodes[plan.Root]
		if strings.HasPrefix(root.ID, "error:") && (root.Kind != rt.CodecUnion || len(root.Variants) != 1 || !strings.HasSuffix(root.ID, ":"+root.Variants[0].Tag)) {
			t.Fatalf("a failure witness's root is a one-variant union tagged with the failure name: %+v", root)
		}
	}
}

// An endpoint retains its failure witnesses' plans, the codec runtime and
// the endpoint helpers only when it is reachable; the HTTP transport module
// is retained only through its provider.
func TestEndpointRetention(t *testing.T) {
	source := `import Json "effra/json"
error Missing {
    id: string
}
effect fn find(id: string) -> string raises { Missing } {
    fail Missing { id: id }
}
derive textJson = Json.codec<string>(maxBodyBytes: 64, maxDepth: 1)
derive missingJson = Json.codec<Missing>(maxBodyBytes: 64, maxDepth: 1)
derive lookup = Http.endpoint(method: "POST", path: "/find", input: textJson, handler: find, output: textJson, status: 200) raises { Missing: 404 missingJson }
`
	_, unused := checkedApplicationPlan(t, source+"effect fn main() -> string { \"idle\" }\n", GoGenerationBuild)
	if unused.Requires(RequiresHelper, endpointLoweringHelper) || len(unused.Identities(RequiresCodecPlan)) != 0 || unused.Requires(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)) {
		t.Fatalf("an unreachable endpoint retained work: %v %v", unused.Identities(RequiresHelper), unused.Identities(RequiresCodecPlan))
	}
	r, used := checkedApplicationPlan(t, source+"effect fn call() -> HttpReply raises { JsonEncodeFailure } uses { Http } {\n    let body = run Http.text(\"\\\"a\\\"\")\n    run lookup(HttpRequest { method: \"POST\", path: \"/find\", contentType: \"application/json\", body: body })\n}\neffect fn main() -> HttpReply raises { JsonEncodeFailure } {\n    run call().provide<Http>(LiveHttp)\n}\n", GoGenerationBuild)
	if !used.Requires(RequiresHelper, endpointLoweringHelper) || !used.Requires(RequiresRuntimeModule, string(rt.RuntimeModuleCodec)) || len(used.Identities(RequiresCodecPlan)) != 2 {
		t.Fatalf("a reachable endpoint lacks work: %v %v", used.Identities(RequiresHelper), used.Identities(RequiresCodecPlan))
	}
	requireProvenance(t, used, RequiresRuntimeModule, string(rt.RuntimeModuleHTTP), providerIdentity(t, r, "LiveHttp"), "provider-runtime")
	if _, err := r.EmitGo(); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointRefusals(t *testing.T) {
	header := `import Json "effra/json"
error Unknown {
    id: string
}
error Busy
error Other
service Directory {
    effect fn known(id: string) -> bool
}
record Item {
    id: string
}
record Receipt {
    id: string
}
effect fn findItem(id: string) -> Item raises { Unknown } uses { Directory } {
    if run Directory.known(id) {
        Item { id: id }
    } else {
        fail Unknown { id: id }
    }
}
fn itemId(item: Item) -> string {
    item.id
}
derive itemJson = Json.codec<Item>(maxBodyBytes: 64, maxDepth: 1, decode: findItem, encode: itemId)
derive receiptJson = Json.codec<Receipt>(maxBodyBytes: 64, maxDepth: 1)
derive unknownJson = Json.codec<Unknown>(maxBodyBytes: 64, maxDepth: 1)
derive busyJson = Json.codec<Busy>(maxBodyBytes: 64, maxDepth: 1)
effect fn take(item: Item) -> Receipt raises { Busy } {
    Receipt { id: item.id }
}
fn ping() -> void {
}
effect fn decodes(item: Item) -> Receipt raises { JsonDecodeFailure } {
    Receipt { id: item.id }
}
`
	good := `derive take1 = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404 unknownJson, Busy: 503 busyJson }
derive health = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 204)
`
	cases := []struct {
		name, source, code, message string
	}{
		{"unmapped handler failure", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404 }`, "EF107", "endpoint e does not map failure Busy raised by its handler take"},
		{"unmapped decode transformation failure", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Busy: 503 }`, "EF107", "endpoint e does not map failure Unknown raised by its input witness itemJson"},
		{"mapped unreachable failure", `derive e = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 204) raises { Other: 500 }`, "EF138", "endpoint e maps Other, which its input, handler and output never raise"},
		{"mapped twice", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404, Busy: 503, Busy: 500 }`, "EF138", "maps failure Busy twice"},
		{"success status as failure status", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 200, Busy: 503 }`, "EF138", "maps Unknown to status 200; a failure status is 4xx or 5xx"},
		{"profile failure mapped", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404, Busy: 503, JsonDecodeFailure: 422 }`, "EF138", "cannot map JsonDecodeFailure"},
		{"handler profile failure unmapped", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: decodes, output: receiptJson, status: 201) raises { Unknown: 404 }`, "EF107", "does not map failure JsonDecodeFailure raised by its handler decodes; the profile answers only its own boundary JsonDecodeFailure"},
		{"failure success status", `derive e = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 404)`, "EF138", "status 404 must be a 2xx success status"},
		{"no content with output", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 204) raises { Unknown: 404, Busy: 503 }`, "EF138", "status 204 carries no body"},
		{"handler takes wrong input", `derive e = Http.endpoint(method: "POST", path: "/take", input: receiptJson, handler: take, output: receiptJson, status: 201) raises { Busy: 503 }`, "EF138", "handler take takes Item, not Receipt decoded by receiptJson"},
		{"handler returns wrong output", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: itemJson, status: 201) raises { Unknown: 404, Busy: 503 }`, "EF138", "handler take returns Receipt, not Item encoded by itemJson"},
		{"handler takes input without input", `derive e = Http.endpoint(method: "POST", path: "/take", handler: take, output: receiptJson, status: 201) raises { Busy: 503 }`, "EF138", "takes no parameters on an endpoint without input"},
		{"handler returns without output", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, status: 201) raises { Unknown: 404, Busy: 503 }`, "EF138", "an endpoint without output takes a void handler"},
		{"missing required options", `derive e = Http.endpoint(handler: ping)`, "EF138", "endpoint e requires option method\nendpoint e requires option path\nendpoint e requires option status"},
		{"unknown option", `derive e = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 204, query: "x")`, "EF138", "option query is unknown"},
		{"repeated option", `derive e = Http.endpoint(method: "GET", method: "PUT", path: "/health", handler: ping, status: 204)`, "EF138", "option method is repeated"},
		{"option of the wrong kind", `derive e = Http.endpoint(method: GET, path: "/health", handler: ping, status: 204)`, "EF138", "option method takes a string"},
		{"unsupported method", `derive e = Http.endpoint(method: "TRACE", path: "/health", handler: ping, status: 204)`, "EF138", "method \"TRACE\" is not one of GET, POST, PUT, PATCH, DELETE"},
		{"parameterized path", `derive e = Http.endpoint(method: "GET", path: "/items/{id}", handler: ping, status: 204)`, "EF138", "must be an exact static path"},
		{"relative path", `derive e = Http.endpoint(method: "GET", path: "health", handler: ping, status: 204)`, "EF138", "must be an exact static path"},
		{"unknown handler", `derive e = Http.endpoint(method: "GET", path: "/health", handler: missing, status: 204)`, "EF102", "unknown function missing for endpoint e option handler"},
		{"unknown witness", `derive e = Http.endpoint(method: "POST", path: "/take", input: missingJson, handler: take, output: receiptJson, status: 201) raises { Busy: 503 }`, "EF102", "unknown codec witness missingJson"},
		{"failure witness as input", `derive e = Http.endpoint(method: "POST", path: "/take", input: unknownJson, handler: take, output: receiptJson, status: 201) raises { Busy: 503 }`, "EF138", "input unknownJson is a failure witness"},
		{"body witness of another failure", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404 busyJson, Busy: 503 }`, "EF138", "failure Unknown body busyJson must be a failure witness of Unknown"},
		{"data witness as failure body", `derive e = Http.endpoint(method: "POST", path: "/take", input: itemJson, handler: take, output: receiptJson, status: 201) raises { Unknown: 404 receiptJson, Busy: 503 }`, "EF138", "failure Unknown body receiptJson must be a failure witness of Unknown"},
		{"failure witness with transformation", `fn same(id: string) -> string { id }
derive w = Json.codec<Busy>(maxBodyBytes: 64, maxDepth: 1, decode: same, encode: same)`, "EF138", "failure witness w of Busy is structural; a failure body takes no decode transformation"},
		{"failure witness with composition", `derive w = Json.codec<Unknown>(maxBodyBytes: 64, maxDepth: 1) with itemJson`, "EF138", "failure witness w of Unknown is structural; a failure body composes no witnesses"},
		{"builtin failure witness", `derive w = Json.codec<Timeout>(maxBodyBytes: 64, maxDepth: 1)`, "EF138", "must derive a failure declared in this module"},
		{"routes serving one route twice", good + `derive again = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 204)
derive api = Http.routes(health, again)`, "EF138", "route table api serves GET /health with both health and again"},
		{"routes listing an entry twice", good + `derive api = Http.routes(health, health)`, "EF138", "route table api lists health twice"},
		{"routes of a route table", good + `derive inner = Http.routes(health)
derive api = Http.routes(inner, take1)`, "EF138", "route table api entry inner is not an endpoint"},
		{"routes of an unknown endpoint", `derive api = Http.routes(nowhere)`, "EF102", "unknown endpoint nowhere in route table api"},
		{"empty route table", `derive api = Http.routes()`, "EF138", "route table api lists no endpoints"},
		{"endpoint name collides", `fn health() -> void {
}
derive health = Http.endpoint(method: "GET", path: "/health", handler: ping, status: 204)`, "EF101", "duplicate declaration health"},
		{"transformation services stay required", good + `effect fn main() -> HttpReply raises { JsonEncodeFailure } uses { Http } {
    let body = run Http.text("x")
    run take1(HttpRequest { method: "POST", path: "/take", contentType: "application/json", body: body })
}`, "EF108", "missing service requirements: Directory"},
		{"terminal encoding failure stays declared", good + `effect fn main() -> HttpReply uses { Http, Directory } {
    let body = run Http.text("x")
    run take1(HttpRequest { method: "POST", path: "/take", contentType: "application/json", body: body })
}`, "EF107", "undeclared failures: JsonEncodeFailure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(header + tc.source)
			if r.Checked {
				t.Fatal("source was admitted")
			}
			for _, message := range strings.Split(tc.message, "\n") {
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == tc.code && strings.Contains(d.Message, message)
				}
				if !found {
					t.Fatalf("missing %s %q in %v", tc.code, message, r.Diagnostics)
				}
			}
		})
	}
	// Positive controls: the header with the well-formed endpoints and a
	// route table checks, so each refusal above is caused by its own line.
	if r := Compile(header + good + "derive api = Http.routes(take1, health)\n"); !r.Checked {
		t.Fatalf("positive control: %v", r.Diagnostics)
	}
	if r := Compile(header + good + `effect fn main() -> HttpReply raises { JsonEncodeFailure } uses { Http, Directory } {
    let body = run Http.text("x")
    run take1(HttpRequest { method: "POST", path: "/take", contentType: "application/json", body: body })
}
`); !r.Checked {
		t.Fatalf("caller positive control: %v", r.Diagnostics)
	}
}

func TestEndpointDeclarationsFormat(t *testing.T) {
	source := "fn ping() -> void {\n}\nderive health=Http.endpoint( method:\"GET\",path : \"/health\",handler:ping,status:204 )\nderive api = Http.routes( health )\n"
	want := "fn ping() -> void {\n}\nderive health = Http.endpoint(method: \"GET\", path: \"/health\", handler: ping, status: 204)\nderive api = Http.routes(health)\n"
	assertFormat(t, source, want)
}
