package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Ported from scripts/layer_smoke.py: checked layer provision through the
// actual CLI and MCP processes on both targets, with byte-charged layer
// inspection, the project graph, layer diagnostics and formatter controls.

const layerSmokeSource = `import Fns "effra/functions"
service Store { effect fn label() -> string }
service Account { effect fn label() -> string }
service Invoice { effect fn label() -> string }
impl Memory(label: string) for Store { effect fn label() -> string { label } }
impl AccountLive for Account uses {Store} { effect fn label() -> string { run Store.label() } }
impl InvoiceLive for Invoice uses {Store} { effect fn label() -> string { run Store.label() } }
layer Shared { Store = Memory("live") }
layer Accounts { merge Shared; Account = AccountLive }
layer Invoices { merge Shared; Invoice = InvoiceLive }
layer App provides {Account, Invoice} { merge Accounts, Invoices }
layer Fixture { merge App; replace Store = Memory("fixture") }
effect fn labels(input: string) -> string uses {Account,Invoice} {
 let account=run Account.label()
 let invoice=run Invoice.label()
 account+":"+invoice
}
effect fn main() -> string { run Fns.call(labels,"input").provide(Fixture) }
`

const layerSmokeOpenSource = `service Store { effect fn label() -> string } service Account { effect fn label() -> string } impl AccountLive for Account uses {Store} { effect fn label() -> string { run Store.label() } } layer Open { Account=AccountLive } effect fn main() -> string { run Account.label().provide(Open) }`

// layerSmokeIDs collects the id of every entry of one table.
func layerSmokeIDs(t *testing.T, value map[string]any, key string) map[string]bool {
	t.Helper()
	identities := map[string]bool{}
	for _, entry := range typeSmokeList(t, value, key) {
		node, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("%s entry is not an object: %v", key, entry)
		}
		identity, ok := node["id"]
		if !ok {
			t.Fatalf("%s entry lacks an id: %v", key, node)
		}
		if text, ok := identity.(string); ok {
			identities[text] = true
		}
	}
	return identities
}

// layerSmokeIn reports whether a truthy reference is a member of table.
func layerSmokeIn(table map[string]bool, reference any) bool {
	text, ok := reference.(string)
	return ok && table[text]
}

// layerSmokeCompleteReferences is the smoke's reference oracle: nonempty
// ref/result strings, truthy failureRow/serviceRow and string args resolve,
// and graph edges join known nodes.
func layerSmokeCompleteReferences(t *testing.T, value map[string]any, label string) {
	t.Helper()
	types := layerSmokeIDs(t, value, "types")
	rows := layerSmokeIDs(t, value, "rows")
	var visit func(item any)
	visit = func(item any) {
		switch item := item.(type) {
		case []any:
			for _, child := range item {
				visit(child)
			}
		case map[string]any:
			for key, child := range item {
				switch key {
				case "ref", "result":
					if text, ok := child.(string); ok && text != "" && !types[text] {
						t.Fatalf("%s: %s %q does not resolve", label, key, text)
					}
				case "failureRow", "serviceRow":
					if typeSmokeTruthy(child) && !layerSmokeIn(rows, child) {
						t.Fatalf("%s: %s %v does not resolve", label, key, child)
					}
				case "args":
					references, ok := child.([]any)
					if !ok {
						t.Fatalf("%s: args is not a list: %v", label, child)
					}
					for _, reference := range references {
						if text, ok := reference.(string); ok && !types[text] {
							t.Fatalf("%s: args %q does not resolve", label, text)
						}
					}
				}
				visit(child)
			}
		}
	}
	visit(value)
	if edges, ok := value["edges"]; ok {
		nodes := layerSmokeIDs(t, value, "nodes")
		list, _ := edges.([]any)
		for _, entry := range list {
			edge, _ := entry.(map[string]any)
			if !layerSmokeIn(nodes, edge["from"]) || !layerSmokeIn(nodes, edge["to"]) {
				t.Fatalf("%s: edge joins an unknown node: %v", label, edge)
			}
		}
	}
}

// layerSmokeConstructorReferencesResolve checks every layer node's
// constructor, contract and configuration type references in a graph.
func layerSmokeConstructorReferencesResolve(t *testing.T, graph map[string]any) {
	t.Helper()
	types := layerSmokeIDs(t, graph, "types")
	rows := layerSmokeIDs(t, graph, "rows")
	object := func(value any, label string) map[string]any {
		result, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %v", label, value)
		}
		return result
	}
	rowsResolve := func(value map[string]any, label string) {
		for _, key := range []string{"failureRow", "serviceRow"} {
			if row := value[key]; typeSmokeTruthy(row) && !layerSmokeIn(rows, row) {
				t.Fatalf("missing %s row %v", label, row)
			}
		}
	}
	typeRefResolves := func(ref map[string]any) {
		if !typeSmokeTruthy(ref["ref"]) || !layerSmokeIn(types, ref["ref"]) {
			t.Fatalf("missing constructor type %v", ref)
		}
		args, ok := ref["args"].([]any)
		if raw, present := ref["args"]; present && !ok {
			t.Fatalf("constructor args is not a list: %v", raw)
		}
		for _, child := range append(append([]any{}, args...), ref["result"]) {
			if typeSmokeTruthy(child) && !layerSmokeIn(types, child) {
				t.Fatalf("missing constructor type child %v", child)
			}
		}
		rowsResolve(ref, "constructor")
	}
	layers := typeSmokeList(t, graph, "layers")
	if len(layers) == 0 {
		t.Fatal("graph has no layers")
	}
	for _, layer := range layers {
		for _, entry := range typeSmokeList(t, object(layer, "layer"), "nodes") {
			node := object(entry, "layer node")
			constructor := object(node["constructor"], "constructor")
			typeRefResolves(object(constructor["type"], "constructor type"))
			typeRefResolves(object(constructor["contract"], "constructor contract"))
			parameters, _ := node["configurationParameters"].([]any)
			for _, parameter := range parameters {
				typeRefResolves(object(object(parameter, "parameter")["typeRef"], "parameter typeRef"))
			}
			arguments, _ := node["configurationArguments"].([]any)
			for _, argument := range arguments {
				value := object(object(argument, "argument")["type"], "argument type")
				typeRefResolves(object(value["type"], "argument value type"))
				if typeSmokeTruthy(value["contract"]) {
					typeRefResolves(object(value["contract"], "argument contract"))
				}
				rowsResolve(value, "constructor argument")
			}
		}
	}
}

// layerSmokeDiagnostics returns a report's diagnostics as objects.
func layerSmokeDiagnostics(t *testing.T, report map[string]any) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, entry := range typeSmokeList(t, report, "diagnostics") {
		diagnostic, _ := entry.(map[string]any)
		result = append(result, diagnostic)
	}
	return result
}

func layerSmokeHasDiagnostic(t *testing.T, report map[string]any, match func(map[string]any) bool) bool {
	t.Helper()
	for _, diagnostic := range layerSmokeDiagnostics(t, report) {
		if match(diagnostic) {
			return true
		}
	}
	return false
}

func layerSmokeTool(identifier int, name string, arguments map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": identifier, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments}}
}

// layerSmokeExpect requires the given exit outcome and returns the run.
func layerSmokeExpect(t *testing.T, run typeSmokeRun, success bool) typeSmokeRun {
	t.Helper()
	if run.err != nil || (run.code == 0) != success {
		t.Fatalf("%v: exit %d err %v (want success %v)\nstdout=%s\nstderr=%s", run.args, run.code, run.err, success, run.stdout, run.stderr)
	}
	return run
}

func TestLayerSmokeProvisionCLIMCPAndFormatter(t *testing.T) {
	binary := buildTestCLI(t)
	// Formatting is target-independent and runs once.
	scratch := smokeWorkspace(t)
	formatted, stderr, code := runTestCLIDir(t, binary, scratch, layerSmokeSource, "fmt", "--stdin")
	if code != 0 {
		t.Fatalf("fmt --stdin: exit %d: %s", code, stderr)
	}
	again, stderr, code := runTestCLIDir(t, binary, scratch, string(formatted), "fmt", "--stdin")
	if code != 0 || !bytes.Equal(again, formatted) {
		t.Fatalf("formatting is not idempotent: exit %d %s\n%s\n%s", code, stderr, formatted, again)
	}
	formattedSource := string(formatted)
	offset := strings.Index(formattedSource, ".provide(Fixture)") + 1
	helperOffset := strings.Index(formattedSource, "Fns.call") + len("Fns.")
	if offset <= 0 || helperOffset < len("Fns.") {
		t.Fatalf("formatted source lost its selections:\n%s", formattedSource)
	}

	longName := "App" + strings.Repeat("x", 5000)
	var identity strings.Builder
	for i := range 20 {
		if i > 0 {
			identity.WriteString("\n")
		}
		fmt.Fprintf(&identity, `service S%d { effect fn value() -> string } impl P%d for S%d { effect fn value() -> string { "ok" } }`, i, i, i)
	}
	fmt.Fprintf(&identity, "\nlayer %s {\n", longName)
	for i := range 20 {
		if i > 0 {
			identity.WriteString("\n")
		}
		fmt.Fprintf(&identity, "S%d=P%d", i, i)
	}
	fmt.Fprintf(&identity, "\n}\neffect fn main() -> string { run S0.value().provide(%s) }\n", longName)
	var budget strings.Builder
	for i := range 50 {
		if i > 0 {
			budget.WriteString("\n")
		}
		fmt.Fprintf(&budget, `service S%d { effect fn value() -> string } impl P%d for S%d { effect fn value() -> string { "value" } }`, i, i, i)
	}
	budget.WriteString("\nlayer Shared {\n")
	for i := range 50 {
		if i > 0 {
			budget.WriteString("\n")
		}
		fmt.Fprintf(&budget, "S%d = P%d", i, i)
	}
	budget.WriteString("\n}\nlayer App {\n" + strings.Repeat("merge Shared\n", 1000) + "}\n")

	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			// The CLI runs from a repository-shaped directory, as the smoke ran
			// from the repository root; the layer sources live in a separate
			// project directory, which is also the MCP root.
			workspace := smokeWorkspace(t, "examples/layers-workflow.ef", "go.mod", "effra.bindings.json")
			project := filepath.Join(workspace, "project")
			if err := os.Mkdir(project, 0o755); err != nil {
				t.Fatal(err)
			}
			workflowSource, err := os.ReadFile(filepath.Join(workspace, "examples", "layers-workflow.ef"))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"app.ef":        formattedSource,
				"invalid.ef":    layerSmokeSource + `layer Bad { Store = Memory("other"); merge Shared }`,
				"open.ef":       layerSmokeOpenSource,
				"workflow.ef":   string(workflowSource),
				"identity.ef":   identity.String(),
				"budget.ef":     budget.String(),
				"unprovided.ef": strings.ReplaceAll(formattedSource, ".provide(Fixture)", ""),
			}
			paths := map[string]string{}
			for name, content := range files {
				paths[name] = filepath.Join(project, name)
				if err := os.WriteFile(paths[name], []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			path := paths["app.ef"]
			withTarget := func(args ...string) []string { return append(args, "--target", target) }
			runs := typeSmokeRunAll(binary, workspace, [][]string{
				withTarget("check", path),                               // 0
				withTarget("inspect", path, "Fixture"),                  // 1
				withTarget("explain", path, "Fixture"),                  // 2
				withTarget("inspect", paths["workflow.ef"], "Workflow"), // 3
				withTarget("graph", path),                               // 4
				withTarget("query", path, strconv.Itoa(offset)),         // 5
				withTarget("query", path, strconv.Itoa(helperOffset)),   // 6
				withTarget("inspect", path, "main"),                     // 7
				withTarget("run", path),                                 // 8
				withTarget("run", "examples/layers-workflow.ef"),        // 9
				withTarget("check", paths["invalid.ef"]),                // 10
				withTarget("check", paths["open.ef"]),                   // 11
				withTarget("check", paths["unprovided.ef"]),             // 12
				withTarget("check", paths["identity.ef"]),               // 13
				withTarget("run", paths["identity.ef"]),                 // 14
				withTarget("inspect", paths["invalid.ef"], "App"),       // 15
				withTarget("inspect", path, "Absent"),                   // 16
				withTarget("check", paths["budget.ef"]),                 // 17
			})
			ok := func(index int) map[string]any { return smokeJSON(t, layerSmokeExpect(t, runs[index], true).stdout) }
			refused := func(index int) map[string]any { return smokeJSON(t, layerSmokeExpect(t, runs[index], false).stdout) }

			checked := ok(0)
			if !typeSmokeTruthy(checked["checked"]) || len(typeSmokeList(t, checked, "layers")) != 5 {
				t.Fatalf("check: %v", checked)
			}
			inspected, explained := ok(1), ok(2)
			for index, response := range map[int]map[string]any{1: inspected, 2: explained} {
				if response["file"] != path {
					t.Fatalf("%v: file = %v, want %s", runs[index].args, response["file"], path)
				}
				charged := typeSmokeAt(t, response, "typeProjectionUsage", "responseBytes")
				if charged != float64(len(bytes.TrimRight(runs[index].stdout, "\n"))) {
					t.Fatalf("%v: responseBytes %v for %d bytes", runs[index].args, charged, len(bytes.TrimRight(runs[index].stdout, "\n")))
				}
			}
			assertReportParity(t, inspected, explained, parityOptions{target: target})
			plan, _ := inspected["layer"].(map[string]any)
			if !reflect.DeepEqual(plan["provides"], []any{"Account", "Invoice"}) || typeSmokeTruthy(plan["requirements"]) {
				t.Fatalf("Fixture plan provides %v requires %v", plan["provides"], plan["requirements"])
			}
			var store map[string]any
			for _, entry := range typeSmokeList(t, plan, "nodes") {
				if node, _ := entry.(map[string]any); node["service"] == "Store" {
					store = node
					break
				}
			}
			if store == nil {
				t.Fatalf("Fixture plan has no Store node: %v", plan)
			}
			if typeSmokeTruthy(store["public"]) || len(typeSmokeList(t, store, "incoming")) != 2 || len(typeSmokeList(t, store, "replacements")) != 1 {
				t.Fatalf("Store node: %v", store)
			}
			if len(typeSmokeList(t, store, "configurationParameters")) != 1 || len(typeSmokeList(t, store, "configurationArguments")) != 1 {
				t.Fatalf("Store configuration: %v", store)
			}

			workflow := ok(3)
			layerSmokeCompleteReferences(t, workflow, "workflow")
			var kinds []any
			found := false
			for _, entry := range typeSmokeList(t, typeSmokeAt(t, workflow, "layer").(map[string]any), "nodes") {
				node, _ := entry.(map[string]any)
				if node["service"] != "Delivery" {
					continue
				}
				found = true
				for _, argument := range typeSmokeList(t, node, "configurationArguments") {
					kinds = append(kinds, typeSmokeAt(t, argument, "type", "type", "kind"))
				}
				break
			}
			if !found || !reflect.DeepEqual(kinds, []any{"record", "enum"}) {
				t.Fatalf("Delivery configuration argument kinds = %v", kinds)
			}

			graph := ok(4)
			var fixture any
			for _, layer := range typeSmokeList(t, graph, "layers") {
				if entry, _ := layer.(map[string]any); entry["name"] == "Fixture" {
					fixture = entry
					break
				}
			}
			if !reflect.DeepEqual(fixture, any(plan)) {
				t.Fatalf("graph Fixture layer differs from inspection:\n%v\n%v", fixture, plan)
			}
			provided := false
			for _, entry := range typeSmokeList(t, graph, "edges") {
				if edge, _ := entry.(map[string]any); edge["kind"] == "provides-layer" && edge["to"] == plan["id"] {
					provided = true
				}
			}
			if !provided {
				t.Fatalf("graph has no provides-layer edge to %v", plan["id"])
			}
			if !typeSmokeTruthy(graph["producer"]) || !typeSmokeTruthy(graph["snapshot"]) {
				t.Fatalf("graph lacks producer metadata: %v", graph)
			}
			if typeSmokeAt(t, graph, "producer", "qualifier") != typeSmokeAt(t, graph, "snapshot", "producer") ||
				typeSmokeAt(t, graph, "snapshot", "revision") != graph["revision"] ||
				typeSmokeAt(t, graph, "snapshot", "target") != target {
				t.Fatalf("graph snapshot %v does not match producer %v / revision %v", graph["snapshot"], graph["producer"], graph["revision"])
			}
			if charged := typeSmokeAt(t, graph, "typeProjectionUsage", "responseBytes"); charged != float64(len(bytes.TrimRight(runs[4].stdout, "\n"))) {
				t.Fatalf("graph responseBytes %v", charged)
			}
			layerSmokeConstructorReferencesResolve(t, graph)

			queried, helper, caller := ok(5), ok(6), ok(7)
			if !typeSmokeTruthy(typeSmokeAt(t, queried, "expression", "type", "effect")) {
				t.Fatalf("provided query is not effectful: %v", queried["expression"])
			}
			for _, view := range []map[string]any{checked, inspected, graph, queried, helper, caller} {
				for _, key := range []string{"producerIdentity", "sources", "bundledBindings", "bundledInterfaces", "producer", "snapshot", "typeProjectionComplete"} {
					if !typeSmokeTruthy(view[key]) {
						t.Fatalf("view lacks %s: %v", key, view)
					}
				}
				if typeSmokeAt(t, view, "snapshot", "producer") != typeSmokeAt(t, view, "producer", "qualifier") ||
					view["revision"] != checked["revision"] || view["target"] != target ||
					typeSmokeAt(t, view, "snapshot", "revision") != view["revision"] ||
					typeSmokeAt(t, view, "snapshot", "target") != target {
					t.Fatalf("view metadata disagrees with check: %v", view)
				}
				layerSmokeCompleteReferences(t, view, "view")
			}

			if got := string(layerSmokeExpect(t, runs[8], true).stdout); got != "fixture:fixture\n" {
				t.Fatalf("run app = %q", got)
			}
			if got := string(layerSmokeExpect(t, runs[9], true).stdout); got != "queued:Ada|denied\n" {
				t.Fatalf("run layers-workflow = %q", got)
			}
			invalid := refused(10)
			missing := refused(11)
			if !layerSmokeHasDiagnostic(t, missing, func(d map[string]any) bool { return d["code"] == "EF108" && typeSmokeTruthy(d["related"]) }) {
				t.Fatalf("open layer: no related EF108: %v", missing)
			}
			unprovided := refused(12)
			if !layerSmokeHasDiagnostic(t, unprovided, func(d map[string]any) bool {
				message, _ := d["message"].(string)
				return d["code"] == "EF108" && strings.Contains(message, "Account") && strings.Contains(message, "Invoice")
			}) {
				t.Fatalf("unprovided: no EF108 naming Account and Invoice: %v", unprovided)
			}
			identityReport := refused(13)
			if typeSmokeTruthy(identityReport["checked"]) || !layerSmokeHasDiagnostic(t, identityReport, func(d map[string]any) bool { return d["code"] == "EF133" }) {
				t.Fatalf("oversized layer identity: %v", identityReport)
			}
			refusedRun := layerSmokeExpect(t, runs[14], false)
			if !bytes.Contains(refusedRun.stdout, []byte("EF133")) || bytes.Contains(refusedRun.stderr, []byte("defect: invalid layer plan")) {
				t.Fatalf("oversized identity run: stdout=%s stderr=%s", refusedRun.stdout, refusedRun.stderr)
			}
			var duplicate map[string]any
			for _, diagnostic := range layerSmokeDiagnostics(t, invalid) {
				if diagnostic["code"] == "EF130" {
					duplicate = diagnostic
					break
				}
			}
			if duplicate == nil || !typeSmokeTruthy(duplicate["related"]) {
				t.Fatalf("invalid: no related EF130: %v", invalid)
			}
			if length, _ := typeSmokeAt(t, duplicate, "span", "length").(float64); length <= 0 {
				t.Fatalf("EF130 span: %v", duplicate["span"])
			}
			for _, index := range []int{15, 16} {
				if len(layerSmokeExpect(t, runs[index], false).stderr) == 0 {
					t.Fatalf("%v: refused without a message", runs[index].args)
				}
			}
			budgetReport := refused(17)
			if typeSmokeTruthy(budgetReport["checked"]) || !layerSmokeHasDiagnostic(t, budgetReport, func(d map[string]any) bool { return d["code"] == "EF133" }) {
				t.Fatalf("budget: %v", budgetReport)
			}
			if _, ok := budgetReport["layers"]; ok || typeSmokeTruthy(budgetReport["typeProjectionComplete"]) {
				t.Fatalf("budget refusal published layers or a complete projection: %v", budgetReport)
			}

			requests := []map[string]any{
				{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
					"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
					"clientInfo": map[string]any{"name": "layer-smoke", "version": "1"}}},
				{"jsonrpc": "2.0", "method": "notifications/initialized"},
				layerSmokeTool(2, "code.inspect", map[string]any{"file": "invalid.ef", "symbol": "App", "target": target}),
				layerSmokeTool(3, "code.inspect", map[string]any{"file": "app.ef", "symbol": "Absent", "target": target}),
				layerSmokeTool(8, "code.inspect", map[string]any{"file": "budget.ef", "symbol": "App", "target": target}),
				{"jsonrpc": "2.0", "id": 4, "method": "ping"},
				layerSmokeTool(7, "code.format", map[string]any{"source": formattedSource}),
				layerSmokeTool(10, "code.inspect", map[string]any{"file": "workflow.ef", "symbol": "Workflow", "target": target}),
				layerSmokeTool(11, "project.check", map[string]any{"file": "open.ef", "target": target}),
				layerSmokeTool(12, "project.check", map[string]any{"file": "identity.ef", "target": target}),
				layerSmokeTool(13, "project.check", map[string]any{"file": "unprovided.ef", "target": target}),
				layerSmokeTool(14, "code.typeAt", map[string]any{"file": "app.ef", "offset": helperOffset, "target": target, "expectedRevision": "stale"}),
			}
			comparisons := []struct {
				identifier int
				name       string
				arguments  map[string]any
				expected   map[string]any
			}{
				{5, "code.inspect", map[string]any{"symbol": "Fixture"}, inspected},
				{6, "project.graph", map[string]any{}, graph},
				{9, "code.typeAt", map[string]any{"offset": offset}, queried},
				{15, "code.typeAt", map[string]any{"offset": helperOffset}, helper},
				{16, "code.inspect", map[string]any{"symbol": "main"}, caller},
				{17, "project.check", map[string]any{}, checked},
				{18, "code.explain", map[string]any{"symbol": "Fixture"}, explained},
			}
			for _, comparison := range comparisons {
				arguments := map[string]any{"file": "app.ef", "target": target, "expectedRevision": checked["revision"]}
				for key, value := range comparison.arguments {
					arguments[key] = value
				}
				requests = append(requests, layerSmokeTool(comparison.identifier, comparison.name, arguments))
			}
			var input bytes.Buffer
			for _, request := range requests {
				input.Write(smokeDumps(t, request))
				input.WriteByte('\n')
			}
			stdout, stderr, code := runTestCLIDir(t, binary, workspace, input.String(), "mcp", project)
			if code != 0 {
				t.Fatalf("mcp exit %d: %s", code, stderr)
			}
			responses := map[int]map[string]any{}
			for _, line := range bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n")) {
				response := smokeJSON(t, line)
				identifier, _ := response["id"].(float64)
				responses[int(identifier)] = response
			}
			result := func(identifier int) map[string]any {
				value, ok := responses[identifier]["result"].(map[string]any)
				if !ok {
					t.Fatalf("response %d has no result: %v", identifier, responses[identifier])
				}
				return value
			}
			structured := func(identifier int) map[string]any {
				value, ok := result(identifier)["structuredContent"].(map[string]any)
				if !ok {
					t.Fatalf("response %d has no structured content: %v", identifier, responses[identifier])
				}
				return value
			}
			for _, identifier := range []int{2, 3, 8, 14} {
				if !typeSmokeTruthy(result(identifier)["isError"]) {
					t.Fatalf("response %d not refused: %v", identifier, responses[identifier])
				}
			}
			if len(result(4)) != 0 {
				t.Fatalf("queued ping: %v", responses[4])
			}
			if typeSmokeTruthy(result(7)["isError"]) {
				t.Fatalf("code.format refused formatted source: %v", responses[7])
			}
			adapter := parityOptions{target: target, ignored: []string{"file", "timings"}, project: adapterSemantic}
			for _, comparison := range comparisons {
				actual := structured(comparison.identifier)
				assertReportParity(t, actual, comparison.expected, adapter)
				layerSmokeCompleteReferences(t, actual, comparison.name)
			}
			for _, identifier := range []int{5, 18} {
				if file := structured(identifier)["file"]; file != "app.ef" {
					t.Fatalf("response %d file = %v", identifier, file)
				}
			}
			assertReportParity(t, structured(10), workflow, adapter)
			layerSmokeCompleteReferences(t, structured(10), "MCP workflow")
			for identifier, expected := range map[int]map[string]any{11: missing, 12: identityReport, 13: unprovided} {
				assertReportParity(t, structured(identifier), expected, adapter)
			}
		})
	}
}
