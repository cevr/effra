package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ported from scripts/type_smoke.py: selected lexical and type facts from
// `ef type` and MCP code.type over shared snapshots on both targets, with the
// canonical type-graph closure oracle and its causal red controls.

const typeSmokeSource = `import Fns "effra/functions"
enum Notice { Named { value: string } }
fn local(input: string) -> string { let label = input; label }
fn describe(label: string, notice: Notice) -> string {
 let rendered = match notice { Notice.Named { value: label } => label }
 rendered + label
}
service Labels { effect fn read(item: string) -> string }
impl Prefix(prefix: string) for Labels { effect fn read(item: string) -> string { prefix + item } }
fn unrelated(input: string) -> string { Fns.identity(input) }
error Child
effect fn task() -> string raises {Child} { "ok" }
effect fn main() -> string raises {Child} {
 let child = fork task()
 run child.join()
}
effect fn fetch(item: string) -> string uses {Labels} { run Labels.read(item) }
effect fn guarded<E: raises>(notice: Notice, cb: effect fn(Notice) -> string raises {E}) -> string raises {E, Child} { "ok" }
`

// typeSmokeSelector is one cursor selection: a name and the byte offset just
// after the given prefix of the matched text.
type typeSmokeSelector struct {
	name   string
	offset int
}

func typeSmokeOffset(t *testing.T, match string, skip int) int {
	t.Helper()
	index := strings.Index(typeSmokeSource, match)
	if index < 0 {
		t.Fatalf("source lacks %q", match)
	}
	return index + skip
}

// typeSmokeRun is one finished CLI process.
type typeSmokeRun struct {
	args   []string
	stdout []byte
	stderr []byte
	code   int
	err    error
}

// The Python smoke's deadlines for one `ef type` process and for the MCP
// session.
const (
	typeSmokeCLITimeout = 30 * time.Second
	typeSmokeMCPTimeout = 60 * time.Second
)

// typeSmokeRunAll runs independent CLI invocations concurrently in dir, each
// under timeout; a process that outlives it is killed, reaped and reported
// as the run's error.
func typeSmokeRunAll(binary, dir string, timeout time.Duration, commands [][]string) []typeSmokeRun {
	runs := make([]typeSmokeRun, len(commands))
	var group sync.WaitGroup
	for index, args := range commands {
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command(binary, args...)
			command.Dir = dir
			stdout, stderr, code, err := runSmokeDeadline(command, timeout)
			runs[index] = typeSmokeRun{args: args, stdout: stdout, stderr: stderr, code: code, err: err}
		}()
	}
	group.Wait()
	return runs
}

// typeSmokeSucceeded requires a zero exit and returns the JSON report.
func typeSmokeSucceeded(t *testing.T, run typeSmokeRun) map[string]any {
	t.Helper()
	if run.err != nil || run.code != 0 {
		t.Fatalf("%v: exit %d err %v\nstdout=%s\nstderr=%s", run.args, run.code, run.err, run.stdout, run.stderr)
	}
	return smokeJSON(t, run.stdout)
}

// typeSmokeRefused requires a nonzero exit with nothing on stdout.
func typeSmokeRefused(t *testing.T, run typeSmokeRun) {
	t.Helper()
	if run.err != nil || run.code == 0 || len(run.stdout) != 0 {
		t.Fatalf("%v: want refusal with empty stdout: exit %d err %v\nstdout=%s\nstderr=%s", run.args, run.code, run.err, run.stdout, run.stderr)
	}
}

// typeSmokeAt walks object keys and list indices, failing on a missing step.
func typeSmokeAt(t *testing.T, value any, path ...any) any {
	t.Helper()
	current := value
	for _, step := range path {
		switch step := step.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("%v: not an object at %q: %v", path, step, current)
			}
			next, ok := object[step]
			if !ok {
				t.Fatalf("%v: missing %q in %v", path, step, object)
			}
			current = next
		case int:
			list, ok := current.([]any)
			if !ok || step >= len(list) {
				t.Fatalf("%v: no index %d in %v", path, step, current)
			}
			current = list[step]
		}
	}
	return current
}

// typeSmokeTruthy is Python truthiness over decoded JSON.
func typeSmokeTruthy(value any) bool {
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

// typeSmokeClosure is the canonical type-graph oracle: type and row IDs are
// unique nonempty strings, and every args, ref, signature, result,
// failureRow, serviceRow and row reference anywhere in the response resolves
// to its table.
func typeSmokeClosure(value map[string]any) error {
	tableIDs := func(name string) (map[string]bool, error) {
		entries, ok := value[name].([]any)
		if !ok {
			return nil, fmt.Errorf("%s is not a list: %v", name, value[name])
		}
		identities := map[string]bool{}
		for _, entry := range entries {
			node, ok := entry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s entry is not an object: %v", name, entry)
			}
			identity, ok := node["id"].(string)
			if !ok || identity == "" {
				return nil, fmt.Errorf("%s entry has a malformed id: %v", name, node["id"])
			}
			if identities[identity] {
				return nil, fmt.Errorf("%s has duplicate id %q", name, identity)
			}
			identities[identity] = true
		}
		return identities, nil
	}
	types, err := tableIDs("types")
	if err != nil {
		return err
	}
	rows, err := tableIDs("rows")
	if err != nil {
		return err
	}
	resolve := func(table map[string]bool, kind string, identity any, key string) error {
		if text, ok := identity.(string); !ok || text == "" || !table[text] {
			return fmt.Errorf("%s reference %q does not resolve: %v", kind, key, identity)
		}
		return nil
	}
	var visit func(item any) error
	visit = func(item any) error {
		switch item := item.(type) {
		case []any:
			for _, child := range item {
				if err := visit(child); err != nil {
					return err
				}
			}
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := item[key]
				var err error
				switch key {
				case "args":
					references, ok := child.([]any)
					if !ok {
						return fmt.Errorf("args is not a list: %v", child)
					}
					for _, reference := range references {
						if err = resolve(types, "type", reference, key); err != nil {
							break
						}
					}
				case "ref", "signature":
					err = resolve(types, "type", child, key)
				case "result":
					if object, ok := child.(map[string]any); ok {
						if kind, _ := object["kind"].(string); kind == "" {
							err = fmt.Errorf("result object lacks a kind: %v", object)
						}
					} else {
						err = resolve(types, "type", child, key)
					}
				case "failureRow", "serviceRow", "row":
					err = resolve(rows, "row", child, key)
				}
				if err != nil {
					return err
				}
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value)
}

func typeSmokeAssertClosure(t *testing.T, value map[string]any, label string) {
	t.Helper()
	if err := typeSmokeClosure(value); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func typeSmokeList(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	list, ok := value[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list: %v", key, value[key])
	}
	return list
}

func typeSmokeFirstFiber(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	for _, entry := range typeSmokeList(t, value, "types") {
		if node, ok := entry.(map[string]any); ok && node["kind"] == "fiber" {
			return node
		}
	}
	t.Fatalf("no fiber type in %v", value["types"])
	return nil
}

// typeSmokeAssertCausalWireRejections proves the closure oracle accepts the
// actual response and rejects each in-memory corruption of it.
func typeSmokeAssertCausalWireRejections(t *testing.T, value map[string]any, label string) {
	t.Helper()
	typeSmokeAssertClosure(t, value, label)
	rejected := func(mutated map[string]any, control string) {
		t.Helper()
		if typeSmokeClosure(mutated) == nil {
			t.Fatalf("%s: %s was accepted", label, control)
		}
	}
	appendTo := func(report map[string]any, key string, node any) {
		report[key] = append(typeSmokeList(t, report, key), node)
	}
	selection := func(report map[string]any) map[string]any {
		object, ok := report["selection"].(map[string]any)
		if !ok {
			t.Fatalf("%s: selection is not an object", label)
		}
		return object
	}
	for _, malformed := range []any{"", nil, float64(123)} {
		emptyType := deepCopyJSON(t, value)
		appendTo(emptyType, "types", map[string]any{"id": malformed, "kind": "primitive"})
		rejected(emptyType, fmt.Sprintf("malformed type ID %#v", malformed))
		emptyRow := deepCopyJSON(t, value)
		appendTo(emptyRow, "rows", map[string]any{"id": malformed})
		rejected(emptyRow, fmt.Sprintf("malformed row ID %#v", malformed))
	}

	args, ok := typeSmokeFirstFiber(t, value)["args"].([]any)
	if !ok || len(args) == 0 {
		t.Fatalf("%s: fiber has no args", label)
	}
	childType := args[0]
	malformedArgs := deepCopyJSON(t, value)
	appendTo(malformedArgs, "types", map[string]any{"id": "", "kind": "primitive"})
	typeSmokeFirstFiber(t, malformedArgs)["args"] = []any{""}
	rejected(malformedArgs, "empty args reference")

	for _, malformed := range []any{"", nil, float64(123)} {
		malformedRef := deepCopyJSON(t, value)
		selection(malformedRef)["ref"] = malformed
		rejected(malformedRef, fmt.Sprintf("malformed ref %#v", malformed))
		malformedResult := deepCopyJSON(t, value)
		selection(malformedResult)["result"] = malformed
		rejected(malformedResult, fmt.Sprintf("malformed result %#v", malformed))
	}
	for _, malformed := range []any{"", nil} {
		malformedRow := deepCopyJSON(t, value)
		selection(malformedRow)["failureRow"] = malformed
		rejected(malformedRow, fmt.Sprintf("malformed failure row %#v", malformed))
	}

	missingArgument := deepCopyJSON(t, value)
	var kept []any
	for _, entry := range typeSmokeList(t, missingArgument, "types") {
		if node, _ := entry.(map[string]any); node["id"] != childType {
			kept = append(kept, entry)
		}
	}
	missingArgument["types"] = kept
	rejected(missingArgument, "missing args child")
	duplicateType := deepCopyJSON(t, value)
	appendTo(duplicateType, "types", deepCopyJSON(t, typeSmokeList(t, value, "types")[0].(map[string]any)))
	rejected(duplicateType, "duplicate type")
	duplicateRow := deepCopyJSON(t, value)
	appendTo(duplicateRow, "rows", deepCopyJSON(t, typeSmokeList(t, value, "rows")[0].(map[string]any)))
	rejected(duplicateRow, "duplicate row")
	missingRows := deepCopyJSON(t, value)
	missingRows["rows"] = []any{}
	rejected(missingRows, "missing row")
}

// typeSmokeProducerSnapshotAccepts reports whether the shared producer
// oracle accepts value.
func typeSmokeProducerSnapshotAccepts(t *testing.T, value map[string]any, target string) bool {
	t.Helper()
	_, err := checkProducerSnapshot(value, target, defaultSnapshotSchema)
	return err == nil
}

// typeSmokeAssertSnapshotSchemaControls proves the shared producer oracle
// does not self-certify a foreign snapshot schema.
func typeSmokeAssertSnapshotSchemaControls(t *testing.T, value map[string]any, target string) {
	t.Helper()
	if !typeSmokeProducerSnapshotAccepts(t, value, target) {
		t.Fatalf("producer-snapshot probe refused the actual response")
	}
	for _, malformed := range []any{float64(999), true} {
		broken := deepCopyJSON(t, value)
		broken["snapshot"].(map[string]any)["schemaVersion"] = malformed
		if typeSmokeProducerSnapshotAccepts(t, broken, target) {
			t.Fatalf("snapshot schema %#v was self-certified", malformed)
		}
	}
}

func typeSmokeTool(identifier int, arguments map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": identifier, "method": "tools/call",
		"params": map[string]any{"name": "code.type", "arguments": arguments}}
}

func typeSmokeMerge(arguments, guard map[string]any) map[string]any {
	for key, value := range guard {
		arguments[key] = value
	}
	return arguments
}

func TestTypeSmokeSelectedFactsAndCanonicalDefinitions(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t)
	path := filepath.Join(workspace, "facts.ef")
	invalid := filepath.Join(workspace, "invalid.ef")
	if err := os.WriteFile(path, []byte(typeSmokeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, []byte(`fn invalid() -> string { missing }`), 0o644); err != nil {
		t.Fatal(err)
	}
	selectors := []typeSmokeSelector{
		{"let", typeSmokeOffset(t, "let label", len("let "))},
		{"use", typeSmokeOffset(t, "; label", len("; "))},
		{"alias", typeSmokeOffset(t, "value: label", len("value: "))},
		{"alias-use", typeSmokeOffset(t, "=> label", len("=> "))},
		{"outer-use", typeSmokeOffset(t, "rendered + label", len("rendered + "))},
		{"config", typeSmokeOffset(t, "Prefix(prefix", len("Prefix("))},
		{"config-use", typeSmokeOffset(t, "{ prefix +", len("{ "))},
		{"method-use", typeSmokeOffset(t, "prefix + item", len("prefix + "))},
		{"fiber", typeSmokeOffset(t, "run child.join", len("run "))},
		{"callee", typeSmokeOffset(t, "fork task", len("fork "))},
		{"operation", typeSmokeOffset(t, "Labels.read(item)", len("Labels."))},
		{"service", typeSmokeOffset(t, "Labels.read(item)", 0)},
		{"bundled", typeSmokeOffset(t, "Fns.identity", len("Fns."))},
		{"module-alias", typeSmokeOffset(t, "Fns.identity", 0)},
		{"variant", typeSmokeOffset(t, "Notice.Named {", len("Notice."))},
		{"annotation", typeSmokeOffset(t, "notice: Notice)", len("notice: "))},
		{"row-parameter", typeSmokeOffset(t, "raises {E, Child}", len("raises {"))},
		{"row-label", typeSmokeOffset(t, "raises {E, Child}", len("raises {E, "))},
	}
	namedSymbols := []string{"Labels", "Prefix", "Labels.read"}
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			typeArgs := func(file string, selector ...string) []string {
				return append([]string{"type", file, "--target", target}, selector...)
			}
			// Stage 1: every selection and refusal that needs no revision.
			var commands [][]string
			for _, selector := range selectors {
				commands = append(commands, typeArgs(path, "--offset", strconv.Itoa(selector.offset)))
			}
			commands = append(commands,
				typeArgs(path, "--symbol", "unrelated"),
				typeArgs(path, "--symbol", "Notice"),
				typeArgs(path, "--offset", strconv.Itoa(len(typeSmokeSource))),
				typeArgs(invalid, "--symbol", "invalid"),
				typeArgs(path, "--symbol", "Labels.missing"),
			)
			for _, name := range namedSymbols {
				commands = append(commands, typeArgs(path, "--symbol", name))
			}
			runs := typeSmokeRunAll(binary, workspace, typeSmokeCLITimeout, commands)
			views := map[string]map[string]any{}
			for index, selector := range selectors {
				views[selector.name] = typeSmokeSucceeded(t, runs[index])
			}
			unrelated := typeSmokeSucceeded(t, runs[len(selectors)])
			nominal := typeSmokeSucceeded(t, runs[len(selectors)+1])
			typeSmokeRefused(t, runs[len(selectors)+2])
			typeSmokeRefused(t, runs[len(selectors)+3])
			typeSmokeRefused(t, runs[len(selectors)+4])
			named := map[string]map[string]any{}
			for index, name := range namedSymbols {
				named[name] = typeSmokeSucceeded(t, runs[len(selectors)+5+index])
			}

			artifact := producerSnapshot(t, views["use"], target, defaultSnapshotSchema)
			artifactScope := artifact["reuseScope"] == "artifact"
			for _, selector := range selectors {
				view := views[selector.name]
				if !typeSmokeTruthy(view["checked"]) || !typeSmokeTruthy(view["typeProjectionComplete"]) || view["querySchemaVersion"] != float64(4) {
					t.Fatalf("%s: unchecked or incomplete view: %v", selector.name, view)
				}
				if !typeSmokeTruthy(view["producerIdentity"]) || !typeSmokeTruthy(view["sources"]) || !typeSmokeTruthy(view["bundledInterfaces"]) {
					t.Fatalf("%s: view lacks provenance: %v", selector.name, view)
				}
				if !typeSmokeTruthy(typeSmokeAt(t, view, "selection", "locationAvailable")) {
					t.Fatalf("%s: selection location unavailable", selector.name)
				}
				current := producerSnapshot(t, view, target, defaultSnapshotSchema)
				if artifactScope && !reflect.DeepEqual(current, artifact) {
					t.Fatalf("%s: producer %v, want %v", selector.name, current, artifact)
				}
				typeSmokeAssertClosure(t, view, selector.name)
			}
			typeSmokeAssertSnapshotSchemaControls(t, views["use"], target)

			binding := func(name string, path ...any) any {
				return typeSmokeAt(t, views[name], append([]any{"selection", "binding"}, path...)...)
			}
			if binding("let", "id") != binding("use", "id") {
				t.Fatalf("let and use bindings differ")
			}
			if binding("alias", "id") != binding("alias-use", "id") || binding("alias-use", "id") == binding("outer-use", "id") {
				t.Fatalf("pattern alias does not shadow the outer label")
			}
			if binding("alias", "declarationSpan", "offset") != float64(selectors[2].offset) {
				t.Fatalf("alias declaration offset = %v", binding("alias", "declarationSpan", "offset"))
			}
			if binding("config", "id") != binding("config-use", "id") {
				t.Fatalf("configuration parameter and use bindings differ")
			}
			if binding("method-use", "kind") != "parameter" {
				t.Fatalf("method-use binding kind = %v", binding("method-use", "kind"))
			}
			// Hover/definition facts: the selected token's declaration target
			// and the one presentation rendered from this response's tables.
			declared := func(name string, path ...any) any {
				return typeSmokeAt(t, views[name], append([]any{"selection", "target"}, path...)...)
			}
			presentation := func(name string) any { return typeSmokeAt(t, views[name], "selection", "presentation") }
			selectionKind := func(name string) any { return typeSmokeAt(t, views[name], "selection", "kind") }
			sourceOffset := func(match string, skip int) float64 { return float64(typeSmokeOffset(t, match, skip)) }
			for _, check := range []struct {
				label     string
				got, want any
			}{
				{"let target kind", declared("let", "kind"), "let"},
				{"let presentation", presentation("let"), "let label: string"},
				{"callee selection kind", selectionKind("callee"), "expression"},
				{"callee target kind", declared("callee", "kind"), "function"},
				{"callee location", declared("callee", "locationAvailable"), true},
				{"callee offset", declared("callee", "span", "offset"), sourceOffset("fn task", len("fn "))},
				{"callee presentation", presentation("callee"), "effect fn task() -> string raises {Child}"},
				{"operation target kind", declared("operation", "kind"), "operation"},
				{"operation owner", declared("operation", "owner"), "Labels"},
				{"operation offset", declared("operation", "span", "offset"), sourceOffset("fn read", len("fn "))},
				{"operation presentation", presentation("operation"), "effect fn Labels.read(item: string) -> string"},
				{"service selection kind", selectionKind("service"), "reference"},
				{"service offset", declared("service", "span", "offset"), sourceOffset("service Labels", len("service "))},
				{"service presentation", presentation("service"), "service Labels"},
				{"bundled kind", declared("bundled", "kind"), "function"},
				{"bundled module", declared("bundled", "module"), "effra/functions"},
				{"module-alias kind", declared("module-alias", "kind"), "module"},
				{"module-alias location", declared("module-alias", "locationAvailable"), true},
				{"module-alias offset", declared("module-alias", "span", "offset"), sourceOffset(`Fns "effra`, 0)},
				{"module-alias presentation", presentation("module-alias"), `import Fns "effra/functions"`},
				{"variant selection kind", selectionKind("variant"), "reference"},
				{"variant kind", declared("variant", "kind"), "variant"},
				{"variant owner", declared("variant", "owner"), "Notice"},
				{"variant offset", declared("variant", "span", "offset"), sourceOffset("Named {", 0)},
				{"variant presentation", presentation("variant"), "variant Notice.Named { value: string }"},
				// Annotation and row-label tokens resolve through the checked
				// type and row: a row parameter is the function's own, other
				// labels are errors.
				{"annotation selection kind", selectionKind("annotation"), "reference"},
				{"annotation kind", declared("annotation", "kind"), "enum"},
				{"annotation offset", declared("annotation", "span", "offset"), sourceOffset("Notice {", 0)},
				{"row-parameter kind", declared("row-parameter", "kind"), "rowParameter"},
				{"row-parameter owner", declared("row-parameter", "owner"), "guarded"},
				{"row-parameter offset", declared("row-parameter", "span", "offset"), sourceOffset("guarded<E", len("guarded<"))},
				{"row-parameter presentation", presentation("row-parameter"), "row parameter guarded.E: raises"},
				{"row-label kind", declared("row-label", "kind"), "error"},
				{"row-label presentation", presentation("row-label"), "error Child"},
				{"Labels target kind", typeSmokeAt(t, named["Labels"], "selection", "target", "kind"), "service"},
				{"Prefix target kind", typeSmokeAt(t, named["Prefix"], "selection", "target", "kind"), "provider"},
				{"Labels.read target kind", typeSmokeAt(t, named["Labels.read"], "selection", "target", "kind"), "operation"},
				{"Labels.read presentation", typeSmokeAt(t, named["Labels.read"], "selection", "presentation"), "effect fn Labels.read(item: string) -> string"},
				{"unrelated selection kind", typeSmokeAt(t, unrelated, "selection", "kind"), "declaration"},
				{"nominal selection kind", typeSmokeAt(t, nominal, "selection", "kind"), "declaration"},
				{"nominal field type", typeSmokeAt(t, nominal, "selection", "declaration", "variants", 0, "fields", 0, "type"), "string"},
			} {
				if !reflect.DeepEqual(check.got, check.want) {
					t.Fatalf("%s = %#v, want %#v", check.label, check.got, check.want)
				}
			}
			if !reflect.DeepEqual(declared("use", "span"), binding("use", "declarationSpan")) {
				t.Fatalf("use target span %v != binding declaration %v", declared("use", "span"), binding("use", "declarationSpan"))
			}
			if typeSmokeTruthy(declared("bundled", "locationAvailable")) {
				t.Fatalf("bundled declaration claims a location")
			}
			if source, _ := declared("bundled", "source").(string); !strings.HasPrefix(source, "source:effra/functions") {
				t.Fatalf("bundled source = %q", source)
			}

			typeID, _ := typeSmokeAt(t, views["use"], "selection", "expression", "type", "type", "ref").(string)
			revision, _ := views["use"]["revision"].(string)
			fiberID, _ := typeSmokeAt(t, views["fiber"], "selection", "expression", "type", "type", "ref").(string)

			// Stage 2: revision-bound canonical definitions and their refusals.
			runs = typeSmokeRunAll(binary, workspace, typeSmokeCLITimeout, [][]string{
				typeArgs(path, "--definition", typeID, "--revision", revision),
				typeArgs(path, "--definition", fiberID, "--revision", revision),
				typeArgs(path, "--definition", typeID),
				typeArgs(path, "--definition", typeID, "--revision", "stale"),
			})
			definition := typeSmokeSucceeded(t, runs[0])
			fiberDefinition := typeSmokeSucceeded(t, runs[1])
			typeSmokeRefused(t, runs[2])
			typeSmokeRefused(t, runs[3])

			definitionProducer := producerSnapshot(t, definition, target, defaultSnapshotSchema)
			if artifactScope && !reflect.DeepEqual(definitionProducer, artifact) {
				t.Fatalf("definition producer %v, want %v", definitionProducer, artifact)
			}
			if typeSmokeTruthy(typeSmokeAt(t, definition, "selection", "locationAvailable")) {
				t.Fatalf("type definition claims a location")
			}
			typeSmokeAssertClosure(t, definition, "definition")
			if kind := typeSmokeAt(t, views["fiber"], "selection", "expression", "type", "type", "kind"); kind != "fiber" {
				t.Fatalf("selected fiber kind = %v", kind)
			}
			if !typeSmokeTruthy(typeSmokeAt(t, views["fiber"], "selection", "expression", "type", "failureRow")) {
				t.Fatalf("selected fiber has an empty failure row")
			}
			typeSmokeAssertCausalWireRejections(t, views["fiber"], "selected Fiber")
			emptySpan := map[string]any{"offset": float64(0), "length": float64(0), "line": float64(0), "column": float64(0)}
			wantSelection := map[string]any{"kind": "typeDefinition", "locationAvailable": false,
				"span": emptySpan, "extent": emptySpan, "definition": fiberID,
				"presentation": "Fiber<string, {Child}>"}
			if !reflect.DeepEqual(fiberDefinition["selection"], wantSelection) {
				t.Fatalf("fiber definition selection = %v, want %v", fiberDefinition["selection"], wantSelection)
			}
			found := false
			for _, entry := range typeSmokeList(t, fiberDefinition, "types") {
				node, _ := entry.(map[string]any)
				if node["id"] == fiberID && node["kind"] == "fiber" && typeSmokeTruthy(node["args"]) && typeSmokeTruthy(node["failureRow"]) {
					found = true
				}
			}
			if !found {
				t.Fatalf("fiber definition lacks its fiber node: %v", fiberDefinition["types"])
			}
			typeSmokeAssertCausalWireRejections(t, fiberDefinition, "Fiber definition")

			// Stage 3: the same selections through one MCP process.
			requests := []any{
				map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
					"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
					"clientInfo": map[string]any{"name": "type-smoke", "version": "1"}}},
				map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
			}
			guard := map[string]any{}
			if artifactScope {
				guard["expectedProducer"] = artifact["qualifier"]
			}
			file := filepath.Base(path)
			for index, selector := range selectors {
				requests = append(requests, typeSmokeTool(index+2, typeSmokeMerge(map[string]any{
					"file": file, "target": target, "offset": selector.offset, "expectedRevision": revision}, guard)))
			}
			requests = append(requests,
				typeSmokeTool(20, typeSmokeMerge(map[string]any{"file": file, "target": target, "symbol": "unrelated"}, guard)),
				typeSmokeTool(21, typeSmokeMerge(map[string]any{"file": file, "target": target, "definition": typeID,
					"expectedRevision": revision}, guard)),
				typeSmokeTool(22, typeSmokeMerge(map[string]any{"file": file, "target": target, "offset": selectors[0].offset,
					"expectedRevision": "stale"}, guard)),
				typeSmokeTool(23, map[string]any{"file": filepath.Base(invalid), "target": target, "symbol": "invalid"}),
				typeSmokeTool(24, map[string]any{"file": file, "target": target, "offset": selectors[0].offset, "symbol": "local"}),
				typeSmokeTool(27, typeSmokeMerge(map[string]any{"file": file, "target": target, "definition": fiberID,
					"expectedRevision": revision}, guard)),
			)
			for index, name := range namedSymbols {
				requests = append(requests, typeSmokeTool(28+index, typeSmokeMerge(map[string]any{
					"file": file, "target": target, "symbol": name}, guard)))
			}
			if artifactScope {
				staleQualifier := "sha256:" + strings.Repeat("0", 64)
				if staleQualifier == artifact["qualifier"] {
					staleQualifier = "sha256:" + strings.Repeat("1", 64)
				}
				requests = append(requests,
					typeSmokeTool(25, map[string]any{"file": file, "target": target, "symbol": "unrelated",
						"expectedRevision": revision, "expectedProducer": staleQualifier}),
					map[string]any{"jsonrpc": "2.0", "id": 26, "method": "ping"})
			} else {
				requests = append(requests, map[string]any{"jsonrpc": "2.0", "id": 25, "method": "ping"})
			}
			var input bytes.Buffer
			for _, request := range requests {
				input.Write(smokeDumps(t, request))
				input.WriteByte('\n')
			}
			stdout, stderr, code := runSmokeCLI(t, typeSmokeMCPTimeout, binary, workspace, input.String(), "mcp", workspace)
			if code != 0 {
				t.Fatalf("mcp exit %d: %s", code, stderr)
			}
			replies := map[int]map[string]any{}
			for _, line := range bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n")) {
				reply := smokeJSON(t, line)
				identifier, _ := reply["id"].(float64)
				replies[int(identifier)] = reply
			}
			reply := func(identifier int) map[string]any {
				value, ok := replies[identifier]
				if !ok {
					t.Fatalf("no reply %d in %s", identifier, stdout)
				}
				return value
			}
			structured := func(identifier int) map[string]any {
				result, ok := reply(identifier)["result"].(map[string]any)
				if !ok {
					t.Fatalf("reply %d has no result: %v", identifier, reply(identifier))
				}
				content, ok := result["structuredContent"].(map[string]any)
				if !ok {
					t.Fatalf("reply %d has no structured content: %v", identifier, result)
				}
				return content
			}
			var mcpFiber map[string]any
			for index, selector := range selectors {
				actual := structured(index + 2)
				assertReportParity(t, actual, views[selector.name], parityOptions{target: target})
				typeSmokeAssertClosure(t, actual, "MCP "+selector.name)
				if selector.name == "fiber" {
					mcpFiber = actual
				}
			}
			assertReportParity(t, structured(20), unrelated, parityOptions{target: target})
			assertReportParity(t, structured(21), definition, parityOptions{target: target})
			for index, name := range namedSymbols {
				assertReportParity(t, structured(28+index), named[name], parityOptions{target: target})
			}
			actualFiberDefinition := structured(27)
			assertReportParity(t, actualFiberDefinition, fiberDefinition, parityOptions{target: target})
			if mcpFiber == nil {
				t.Fatal("MCP returned no selected Fiber")
			}
			typeSmokeAssertCausalWireRejections(t, mcpFiber, "MCP selected Fiber")
			typeSmokeAssertCausalWireRejections(t, actualFiberDefinition, "MCP Fiber definition")
			isError := func(identifier int) bool {
				result, _ := reply(identifier)["result"].(map[string]any)
				return typeSmokeTruthy(result["isError"])
			}
			if !isError(22) || !isError(23) {
				t.Fatalf("stale revision or invalid source not refused: %v / %v", reply(22), reply(23))
			}
			if failure, _ := reply(24)["error"].(map[string]any); failure["code"] != float64(-32602) {
				t.Fatalf("offset with symbol not refused as invalid params: %v", reply(24))
			}
			pingID := 25
			if artifactScope {
				text, _ := typeSmokeAt(t, reply(25), "result", "content", 0, "text").(string)
				if !isError(25) || !strings.Contains(text, "stale producer") {
					t.Fatalf("stale producer not refused: %v", reply(25))
				}
				pingID = 26
			}
			if result, ok := reply(pingID)["result"].(map[string]any); !ok || len(result) != 0 {
				t.Fatalf("queued ping: %v", reply(pingID))
			}
		})
	}
}
