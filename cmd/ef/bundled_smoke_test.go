package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Ported from scripts/bundled_smoke.py: complete bundled semantic responses
// (check, inspect, typeAt, graph) agree between the CLI and stdio MCP, keep
// their type projection closed over its own identities, refuse a stale
// revision, and fail missing rows and absent generic data identically.

type bundledSmokeFixture struct {
	name    string
	source  string
	symbol  string
	anchor  string
	failure string
	service string
}

type bundledSmokeOperation struct {
	command   string
	tool      string
	arguments map[string]any
	cli       []string
}

var bundledSmokeServiceDeclaration = regexp.MustCompile(`\bservice\s+(\w+)\s*\{`)

// bundledSmokeCLI runs one semantic command against main.ef in directory.
func bundledSmokeCLI(t *testing.T, binary, directory, command, target string, success bool, arguments ...string) map[string]any {
	t.Helper()
	argv := append([]string{command, filepath.Join(directory, "main.ef")}, arguments...)
	argv = append(argv, "--target", target)
	stdout, stderr, code := runTestCLIDir(t, binary, directory, "", argv...)
	if (code == 0) != success {
		t.Fatalf("%v exit %d\nstdout=%s\nstderr=%s", argv, code, stdout, stderr)
	}
	return smokeJSON(t, stdout)
}

// bundledSmokeMCP runs one MCP session rooted at directory with the given
// tool calls (ids from 2) and returns every reply.
func bundledSmokeMCP(t *testing.T, binary, directory, client string, calls ...map[string]any) []map[string]any {
	t.Helper()
	messages := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": client, "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
	for index, call := range calls {
		messages = append(messages, map[string]any{"jsonrpc": "2.0", "id": index + 2, "method": "tools/call", "params": call})
	}
	var input strings.Builder
	for _, message := range messages {
		input.Write(mustJSON(t, message))
		input.WriteByte('\n')
	}
	stdout, stderr, code := runTestCLIDir(t, binary, directory, input.String(), "mcp", directory)
	if code != 0 {
		t.Fatalf("mcp exit %d: %s", code, stderr)
	}
	return diagnosticsSmokeReplies(t, stdout)
}

func bundledSmokeWrite(t *testing.T, directory, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "main.ef"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// bundledSmokeTruthy is Python truthiness over decoded JSON.
func bundledSmokeTruthy(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case float64:
		return value != 0
	case string:
		return value != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	}
	return true
}

// bundledSmokeList is an optional JSON list field: absent is empty, any
// other non-list value fails.
func bundledSmokeList(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	value, ok := object[key]
	if !ok {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s is not a list: %v", key, object)
	}
	return list
}

func bundledSmokeObjects(t *testing.T, object map[string]any, key string) []map[string]any {
	t.Helper()
	var objects []map[string]any
	for _, item := range bundledSmokeList(t, object, key) {
		value, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s item is not an object: %v", key, item)
		}
		objects = append(objects, value)
	}
	return objects
}

// bundledSmokeAssertProjection checks that one semantic response's type,
// row, declaration and graph identities are closed over the response.
func bundledSmokeAssertProjection(t *testing.T, actual map[string]any, source string) {
	t.Helper()
	declarations := map[any]map[string]any{}
	if bundledSmokeTruthy(actual["declarations"]) {
		for _, item := range bundledSmokeObjects(t, actual, "declarations") {
			declarations[item["identity"]] = item
		}
	}
	types := bundledSmokeObjects(t, actual, "types")
	rows := bundledSmokeObjects(t, actual, "rows")
	typeIDs, rowIDs, errorNames := map[any]bool{}, map[any]bool{}, map[any]bool{}
	failureRows, serviceRows := map[any]bool{}, map[any]bool{}
	for _, item := range types {
		typeIDs[item["id"]] = true
		if item["kind"] == "error" {
			errorNames[item["name"]] = true
		}
		if bundledSmokeTruthy(item["failureRow"]) {
			failureRows[item["failureRow"]] = true
		}
		if bundledSmokeTruthy(item["serviceRow"]) {
			serviceRows[item["serviceRow"]] = true
		}
	}
	for _, item := range rows {
		rowIDs[item["id"]] = true
	}
	serviceNames := map[any]bool{}
	for _, match := range bundledSmokeServiceDeclaration.FindAllStringSubmatch(source, -1) {
		serviceNames[match[1]] = true
	}
	_, hasNodes := actual["nodes"]
	nodes := map[any]bool{}
	for _, item := range bundledSmokeObjects(t, actual, "nodes") {
		nodes[item["id"]] = true
	}
	for _, row := range rows {
		labels := bundledSmokeList(t, row, "labels")
		labelSet := map[any]bool{}
		for _, label := range labels {
			labelSet[label] = true
		}
		parameters := map[any]map[string]any{}
		for _, parameter := range bundledSmokeObjects(t, row, "parameters") {
			parameters[parameter["id"]] = parameter
		}
		for identity, parameter := range parameters {
			if !labelSet[identity] {
				t.Fatalf("row parameter %v is not a label: %v", identity, row)
			}
			want := fmt.Sprintf("row-parameter:%v:%v:%v", parameter["declaration"], parameter["kind"], parameter["name"])
			if parameter["id"] != want {
				t.Fatalf("row parameter id %v, want %s", parameter["id"], want)
			}
		}
		var concrete []any
		for _, label := range labels {
			if _, ok := parameters[label]; !ok {
				concrete = append(concrete, label)
			}
		}
		if len(labels) > 0 && !failureRows[row["id"]] && !serviceRows[row["id"]] {
			t.Fatalf("labelled row %v is neither a failure nor a service row", row)
		}
		if failureRows[row["id"]] {
			for _, label := range concrete {
				if !errorNames[label] {
					t.Fatalf("failure row %v names %v, not an error %v", row, label, errorNames)
				}
			}
			for _, parameter := range parameters {
				if parameter["kind"] != "raises" {
					t.Fatalf("failure row parameter kind: %v", row)
				}
			}
		}
		if serviceRows[row["id"]] {
			for _, label := range concrete {
				if !serviceNames[label] {
					t.Fatalf("service row %v names %v, not a service %v", row, label, serviceNames)
				}
				if hasNodes && !nodes["service:"+label.(string)] {
					t.Fatalf("service row %v has no graph node: %v", row, nodes)
				}
			}
			for _, parameter := range parameters {
				if parameter["kind"] != "uses" {
					t.Fatalf("service row parameter kind: %v", row)
				}
			}
		}
	}
	for _, owner := range declarations {
		if owner["kind"] != "template" {
			continue
		}
		fields := bundledSmokeObjects(t, owner, "fields")
		for _, variant := range bundledSmokeObjects(t, owner, "variants") {
			fields = append(fields, bundledSmokeObjects(t, variant, "fields")...)
		}
		var references []any
		for _, field := range fields {
			references = append(references, field["typeRef"].(map[string]any)["ref"])
		}
		if _, ok := owner["templateParameters"]; !ok {
			t.Fatalf("template lacks templateParameters: %v", owner)
		}
		for _, parameter := range bundledSmokeObjects(t, owner, "templateParameters") {
			references = append(references, parameter["variable"].(map[string]any)["ref"])
		}
		for _, reference := range references {
			if !typeIDs[reference] {
				t.Fatalf("template %v references %v outside %v", owner, reference, typeIDs)
			}
		}
	}
	for _, node := range types {
		references := bundledSmokeList(t, node, "args")
		if bundledSmokeTruthy(node["result"]) {
			references = append(references, node["result"])
		}
		for _, reference := range references {
			if !typeIDs[reference] {
				t.Fatalf("type %v references %v outside %v", node, reference, typeIDs)
			}
		}
		for _, key := range []string{"failureRow", "serviceRow"} {
			if bundledSmokeTruthy(node[key]) && !rowIDs[node[key]] {
				t.Fatalf("type %v names %s outside %v", node, key, rowIDs)
			}
		}
		if node["kind"] != "application" {
			continue
		}
		declaration, _ := node["declaration"].(string)
		owner, ok := declarations[node["declaration"]]
		if !ok || owner["kind"] != "template" {
			t.Fatalf("application %v has no template owner in %v", node, declarations)
		}
		parameterCount := len(bundledSmokeList(t, owner, "templateParameters"))
		switch {
		case declaration == "template:effra/conversions:module:Codec":
			if len(bundledSmokeList(t, owner, "fields")) != 2 || parameterCount != 4 || owner["source"] != "source:effra/conversions/Codec" {
				t.Fatalf("Codec template: %v", owner)
			}
		case strings.HasPrefix(declaration, "template:effra/data:"):
			name, _ := owner["name"].(string)
			want, known := map[string]int{"Option": 1, "Result": 2}[name]
			if owner["dataKind"] != "enum" || len(bundledSmokeList(t, owner, "variants")) != 2 ||
				owner["source"] != "source:effra/data/"+name || !known || parameterCount != want {
				t.Fatalf("effra/data template: %v", owner)
			}
			found := false
			for _, item := range bundledSmokeObjects(t, actual, "sources") {
				found = found || item["id"] == owner["source"] && item["module"] == "effra/data"
			}
			if !found {
				t.Fatalf("effra/data template source %v not in %v", owner["source"], actual["sources"])
			}
		default:
			if owner["dataKind"] != "record" || owner["source"] != "source:user" {
				t.Fatalf("user template: %v", owner)
			}
		}
	}
	if _, ok := actual["edges"]; ok {
		for _, edge := range bundledSmokeObjects(t, actual, "edges") {
			if !nodes[edge["from"]] || !nodes[edge["to"]] {
				t.Fatalf("edge %v leaves the graph %v", edge, nodes)
			}
		}
	}
}

func bundledSmokeParity(t *testing.T, actual, expected map[string]any, target string) {
	t.Helper()
	assertReportParity(t, actual, expected, parityOptions{target: target, ignored: []string{"file", "timings"}, project: adapterSemantic})
}

func TestBundledSmokeSemanticParity(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "examples/generic-users.ef", "examples/generic-settings.ef")
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(workspace, "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	fixtures := []bundledSmokeFixture{
		{"generic-users", read("generic-users.ef"), "lookup", ".Ok {", "", ""},
		{"generic-settings", read("generic-settings.ef"), "configuration", ".Ok {", "", ""},
		{"functions-call", `import Fns "effra/functions"
error MissingUser
service Users { effect fn name(id: string) -> string raises {MissingUser} }
effect fn load(id: string) -> string raises {MissingUser} uses {Users} { run Users.name(id) }
effect fn greeting(id: string) -> string raises {MissingUser} uses {Users} { run Fns.call(load, id) }
`, "Fns.call", "Fns.call", "MissingUser", "Users"},
		{"functions-call-alias", `import Other "effra/functions"
error MissingSetting
service Settings { effect fn read(key: string) -> string raises {MissingSetting} }
effect fn load(key: string) -> string raises {MissingSetting} uses {Settings} { run Settings.read(key) }
effect fn configuration(key: string) -> string raises {MissingSetting} uses {Settings} { run Other.call(load, key) }
`, "Other.call", "Other.call", "MissingSetting", "Settings"},
		{"conversions-codec", `import Convert "effra/conversions"
record LocalCodec { local: string }
record User { name: string }
record Holder { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
enum Bundle { Value { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> } }
error Broken { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
fn make() -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> { Convert.witness(decode, encode) }
effect fn main() -> string {
 let converter = make()
 let user = run converter.decode("Ada")
 run converter.encode(user)
}
`, "make", "Convert.witness", "", ""},
		{"row-polymorphic-callback", `import Fns "effra/functions"
effect fn echo(input: string) -> string { Fns.identity(input) }
effect fn local<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} { run callback(input) }
effect fn main() -> string { run local(echo, "ok") }
`, "local", "local(echo", "", ""},
	}
	for _, fixture := range fixtures {
		for _, target := range []string{"go", "js"} {
			t.Run(fixture.name+"/"+target, func(t *testing.T) {
				t.Parallel()
				directory := t.TempDir()
				source := fixture.source
				bundledSmokeWrite(t, directory, source)
				index := strings.Index(source, fixture.anchor)
				if dot := strings.Index(fixture.anchor, "."); dot >= 0 {
					index += dot + 1
				}
				// The query offset counts characters, as the Python smoke did.
				offset := utf8.RuneCountInString(source[:index])
				operations := []bundledSmokeOperation{
					{"check", "project.check", map[string]any{}, nil},
					{"inspect", "code.inspect", map[string]any{"symbol": fixture.symbol}, []string{fixture.symbol}},
					{"query", "code.typeAt", map[string]any{"offset": offset}, []string{strconv.Itoa(offset)}},
					{"graph", "project.graph", map[string]any{}, nil},
				}
				var expected []map[string]any
				for _, operation := range operations {
					expected = append(expected, bundledSmokeCLI(t, binary, directory, operation.command, target, true, operation.cli...))
				}
				revision := expected[0]["revision"]
				var calls []map[string]any
				for _, operation := range operations {
					arguments := map[string]any{"file": "main.ef", "target": target, "expectedRevision": revision}
					for key, value := range operation.arguments {
						arguments[key] = value
					}
					calls = append(calls, map[string]any{"name": operation.tool, "arguments": arguments})
				}
				calls = append(calls, map[string]any{"name": "project.check", "arguments": map[string]any{
					"file": "main.ef", "target": target, "expectedRevision": "stale"}})
				replies := bundledSmokeMCP(t, binary, directory, "bundled-parity", calls...)
				if len(replies) != len(calls)+1 {
					t.Fatalf("replies = %v", replies)
				}
				for index, want := range expected {
					response := replies[index+1]
					result, ok := response["result"].(map[string]any)
					if !ok || result["isError"] == true {
						t.Fatalf("%s: %v", operations[index].tool, response)
					}
					actual, _ := result["structuredContent"].(map[string]any)
					bundledSmokeParity(t, actual, want, target)
					if !bundledSmokeTruthy(actual["producerIdentity"]) || !bundledSmokeTruthy(actual["sources"]) ||
						!bundledSmokeTruthy(actual["bundledInterfaces"]) || !bundledSmokeTruthy(actual["bundledBindings"]) {
						t.Fatalf("%s lacks producer, sources or bundled facts: %v", operations[index].tool, actual)
					}
					if actual["revision"] != revision || actual["target"] != target || actual["typeProjectionComplete"] != true {
						t.Fatalf("%s revision, target or projection: %v", operations[index].tool, actual)
					}
					bundledSmokeAssertProjection(t, actual, source)
				}
				if stale, _ := replies[5]["result"].(map[string]any); stale["isError"] != true {
					t.Fatalf("stale revision admitted: %v", replies[5])
				}
				if fixture.failure == "" {
					return
				}
				// The public caller's missing rows must fail identically on both surfaces.
				signature := "-> string raises {" + fixture.failure + "} uses {" + fixture.service + "} { run " + fixture.anchor
				for _, refusal := range []struct{ row, code string }{
					{" raises {" + fixture.failure + "}", "EF107"},
					{" uses {" + fixture.service + "}", "EF108"},
				} {
					bundledSmokeWrite(t, directory, strings.Replace(source, signature, strings.Replace(signature, refusal.row, "", 1), 1))
					rejected := bundledSmokeCLI(t, binary, directory, "check", target, false)
					bundledSmokeAssertCode(t, rejected, refusal.code)
					replies := bundledSmokeMCP(t, binary, directory, "bundled-parity", map[string]any{
						"name": "project.check", "arguments": map[string]any{"file": "main.ef", "target": target}})
					bundledSmokeParity(t, diagnosticsSmokeStructured(t, replies[1]), rejected, target)
				}
			})
		}
	}
}

func bundledSmokeAssertCode(t *testing.T, report map[string]any, code string) {
	t.Helper()
	for _, item := range bundledSmokeObjects(t, report, "diagnostics") {
		if item["code"] == code {
			return
		}
	}
	t.Fatalf("no %s in %v", code, report)
}

func TestBundledSmokeGenericDataRefusals(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "examples/generic-users.ef")
	type refusal struct{ name, source, code string }
	prefix := `import Data "effra/data" record User {name:string} `
	var refusals []refusal
	for index, body := range []string{
		`fn bad()->Data.Option<User>{nil}`,
		`fn bad()->Data.Option<User>{null}`,
		`fn bad()->Data.Option<User?>{Data.Option.Some {value:User {name:"x"}}}`,
		`fn bad()->Data.Option<User>{Data.Option.Some {}}`,
		`fn bad(value:Data.Option<User>)->User{value.value}`,
		`fn bad()->Data.Option<User>{Foreign.Option<User>.None {}}`,
	} {
		refusals = append(refusals, refusal{"absence-" + strconv.Itoa(index), prefix + body, ""})
	}
	data, err := os.ReadFile(filepath.Join(workspace, "examples", "generic-users.ef"))
	if err != nil {
		t.Fatal(err)
	}
	caller := string(data)
	var header string
	for _, line := range strings.Split(caller, "\n") {
		if strings.HasPrefix(line, "effect fn lookup(") {
			header = line
			break
		}
	}
	for _, row := range []struct{ name, row, code string }{
		{"without-raises", " raises { Unavailable }", "EF107"},
		{"without-uses", " uses { Users }", "EF108"},
	} {
		if !strings.Contains(header, row.row) {
			t.Fatalf("lookup header %q lacks %q", header, row.row)
		}
		refusals = append(refusals, refusal{row.name, strings.Replace(caller, header, strings.Replace(header, row.row, "", 1), 1), row.code})
	}
	for _, refusal := range refusals {
		for _, target := range []string{"go", "js"} {
			t.Run(refusal.name+"/"+target, func(t *testing.T) {
				t.Parallel()
				directory := t.TempDir()
				bundledSmokeWrite(t, directory, refusal.source)
				rejected := bundledSmokeCLI(t, binary, directory, "check", target, false)
				if rejected["checked"] != false || !bundledSmokeTruthy(rejected["diagnostics"]) {
					t.Fatalf("refusal was checked or silent: %v", rejected)
				}
				if refusal.code != "" {
					bundledSmokeAssertCode(t, rejected, refusal.code)
				}
				replies := bundledSmokeMCP(t, binary, directory, "generic-data-refusals", map[string]any{
					"name": "project.check", "arguments": map[string]any{"file": "main.ef", "target": target}})
				bundledSmokeParity(t, diagnosticsSmokeStructured(t, replies[1]), rejected, target)
			})
		}
	}
}
