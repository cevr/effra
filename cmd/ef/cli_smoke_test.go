package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ported from scripts/smoke.py: the public binary and its stdio MCP server
// over the authored examples, native and JavaScript execution, the emitted
// library's service keys, the test runner, lint/query/graph tooling and a
// bounded deep TypeAt. Every case drives the package's test CLI in its own
// workspace, because run, build and test write dist/ in the working directory.
func TestCLISmoke(t *testing.T) {
	binary := buildTestCLI(t)

	t.Run("check-and-inspect-main", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		checked := cliSmokeJSON(t, binary, workspace, true, "check", "examples/main.ef")
		cliSmokeEqual(t, cliSmokeAt(t, checked, "checked"), true)
		inspected := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/main.ef", "greeting")
		cliSmokeEqual(t, cliSmokeAt(t, inspected, "symbol", "contract", "requirements"), []any{"Users"})
		cliSmokeEqual(t, cliSmokeAt(t, inspected, "symbol", "contract", "failures"), []any{"NotFound"})
	})

	t.Run("missing-service-refused", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		invalid := cliSmokeJSON(t, binary, workspace, false, "check", "examples/missing-service.ef")
		if !cliSmokeHasCode(t, invalid, "EF108") {
			t.Fatalf("missing service is not EF108: %s", mustJSON(t, invalid))
		}
		report := cliSmokeJSON(t, binary, workspace, false, "diagnostics", "examples/missing-service.ef", "--json")
		for _, key := range []string{"checked", "lintAvailable", "policyPassed"} {
			if cliSmokeAt(t, report, key) != false {
				t.Fatalf("%s is not false: %s", key, mustJSON(t, report))
			}
		}
	})

	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"main-default", []string{"run", "examples/main.ef"}, "Hello, Ada\nUnknown user\n"},
		{"main-js", []string{"run", "examples/main.ef", "--target", "js"}, "Hello, Ada\nUnknown user\n"},
		{"pipe-go", []string{"run", "examples/pipe.ef", "--target", "go"}, "Hello, <ada!> / Hello, lin!\n"},
		{"pipe-js", []string{"run", "examples/pipe.ef", "--target", "js"}, "Hello, <ada!> / Hello, lin!\n"},
		{"data-go", []string{"run", "examples/data.ef", "--target", "go"}, "running 42\n"},
		{"data-js", []string{"run", "examples/data.ef", "--target", "js"}, "running 42\n"},
		{"imports-default", []string{"run", "examples/imports.ef"}, "ADA\npartial result retained\ntimed out\n"},
		{"concurrency-go", []string{"run", "examples/concurrency.ef", "--target", "go"}, "child joined\ntimed out\nrecovered\n"},
		{"concurrency-js", []string{"run", "examples/concurrency.ef", "--target", "js"}, "child joined\ntimed out\nrecovered\n"},
		{"workflow-go", []string{"run", "examples/workflow.ef", "--target", "go"}, "queued: Welcome, Ada\naccess denied\n"},
		{"workflow-js", []string{"run", "examples/workflow.ef", "--target", "js"}, "queued: Welcome, Ada\naccess denied\n"},
		{"latest-task-go", []string{"run", "examples/latest-task.ef", "--target", "go"}, "result: new\n"},
		{"latest-task-js", []string{"run", "examples/latest-task.ef", "--target", "js"}, "result: new\n"},
	} {
		t.Run("run/"+c.name, func(t *testing.T) {
			t.Parallel()
			workspace := cliSmokeExamples(t)
			if stdout, _ := cliSmokeRun(t, binary, workspace, true, c.args...); stdout != c.want {
				t.Fatalf("%v stdout = %q, want %q", c.args, stdout, c.want)
			}
		})
	}

	t.Run("run/host-types-default", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		stdout, _ := cliSmokeRun(t, binary, workspace, true, "run", "examples/host-types.ef")
		lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		want := []string{"nil interface; typed-nil square; square", "7 with typed-nil *Problem"}
		if len(lines) < 4 || !reflect.DeepEqual(lines[2:4], want) {
			t.Fatalf("host-types stdout = %q, want lines 3-4 %q", stdout, want)
		}
	})

	t.Run("run/lifecycle-default", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		stdout, _ := cliSmokeRun(t, binary, workspace, true, "run", "examples/lifecycle.ef")
		first, _, _ := strings.Cut(stdout, "\n")
		snapshot := smokeJSON(t, []byte(first))
		if snapshot["state"] != "Open" || snapshot["resourceCount"] != float64(1) {
			t.Fatalf("lifecycle snapshot = %v", snapshot)
		}
		if !strings.Contains(stdout, "scoped file read complete") || !strings.Contains(stdout, "label: ") {
			t.Fatalf("lifecycle stdout = %q", stdout)
		}
	})

	t.Run("lifecycle-js-refused", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		report := cliSmokeJSON(t, binary, workspace, false, "check", "examples/lifecycle.ef", "--target", "js")
		if !cliSmokeHasCode(t, report, "EF110") {
			t.Fatalf("Go-only lifecycle is not EF110 on JS: %s", mustJSON(t, report))
		}
	})

	t.Run("inspect-data-enum", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		inspection := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/data.ef", "State")
		cliSmokeEqual(t, cliSmokeAt(t, inspection, "declaration", "kind"), "enum")
		cliSmokeEqual(t, cliSmokeAt(t, inspection, "declaration", "variants", 1, "fields", 0, "type"), "string")
	})

	t.Run("inspect-workflow-contract", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		workflow := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/workflow.ef", "welcome")
		cliSmokeEqual(t, cliSmokeAt(t, workflow, "symbol", "contract", "requirements"), []any{"Access", "Delivery", "Directory"})
		cliSmokeEqual(t, cliSmokeAt(t, workflow, "symbol", "contract", "failures"), []any{"DeliveryFailed", "Denied", "UserMissing"})
	})

	t.Run("inspect-imports-bindings", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		bindings := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/imports.ef", "main")
		if !cliSmokeCooperativeBinding(t, bindings) {
			t.Fatalf("no context-forwarding cooperative binding: %s", mustJSON(t, bindings))
		}
	})

	t.Run("native-executable", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		stdout, _ := cliSmokeRun(t, binary, workspace, true, "build", "examples/main.ef")
		native := exec.Command(filepath.Join(workspace, strings.TrimSpace(stdout)))
		native.Dir = "/"
		var output, failure bytes.Buffer
		native.Stdout, native.Stderr = &output, &failure
		if err := native.Run(); err != nil || output.String() != "Hello, Ada\nUnknown user\n" {
			t.Fatalf("native executable: %v stdout=%q stderr=%q", err, output.String(), failure.String())
		}
	})

	// The emitted library must expose service keys for external provision.
	t.Run("js-library-service-keys", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		cliSmokeRun(t, binary, workspace, true, "build", "examples/main.ef", "--target", "js")
		consumer := `import { Effect } from 'effect';
import { greeting, Users, MemoryUsers } from './dist/main.mjs';
console.log(await Effect.runPromise(Effect.provideService(greeting('42'), Users, MemoryUsers)));
`
		command := exec.Command("bun", "--eval", consumer)
		command.Dir = workspace
		var output, failure bytes.Buffer
		command.Stdout, command.Stderr = &output, &failure
		if err := command.Run(); err != nil || output.String() != "Hello, Ada\n" {
			t.Fatalf("library consumer: %v stdout=%q stderr=%q", err, output.String(), failure.String())
		}
	})

	t.Run("failing-program", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		cliSmokeWrite(t, workspace, "failed.ef", "error Bad\neffect fn main() -> string raises {Bad} { fail Bad }\n")
		if _, stderr := cliSmokeRun(t, binary, workspace, false, "run", filepath.Join(workspace, "failed.ef")); !strings.Contains(stderr, "Bad") {
			t.Fatalf("failure stderr = %q", stderr)
		}
	})

	t.Run("mcp-project", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		inspected := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/main.ef", "greeting")
		bindings := cliSmokeJSON(t, binary, workspace, true, "inspect", "examples/imports.ef", "main")
		responses := cliSmokeMCP(t, binary, workspace, workspace, time.Minute, append(cliSmokeHandshake(),
			map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
			cliSmokeCall(3, "code.inspect", map[string]any{"file": "examples/main.ef", "symbol": "greeting"}),
			cliSmokeCall(8, "code.inspect", map[string]any{"file": "examples/data.ef", "symbol": "State"}),
			cliSmokeCall(4, "project.check", map[string]any{"file": "examples/main.ef", "expectedRevision": "old"}),
			cliSmokeCall(5, "code.inspect", map[string]any{"file": "examples/imports.ef", "symbol": "main"}),
			cliSmokeCall(6, "project.describe", map[string]any{}),
			cliSmokeCall(7, "project.tests", map[string]any{"file": "examples/testing.ef"}),
		)...)
		if len(responses) != 8 {
			t.Fatalf("responses = %d, want 8: %v", len(responses), responses)
		}
		tools, _ := cliSmokeAt(t, responses[1], "result", "tools").([]any)
		if len(tools) != 12 {
			t.Fatalf("tools = %d, want 12", len(tools))
		}
		hasType := false
		for index := range tools {
			hasType = hasType || cliSmokeAt(t, tools, index, "name") == "code.type"
		}
		if !hasType {
			t.Fatalf("tools/list lacks code.type: %s", mustJSON(t, tools))
		}
		mcp := cliSmokeAt(t, responses[2], "result", "structuredContent")
		cliSmokeEqual(t, cliSmokeAt(t, mcp, "symbol"), inspected["symbol"])
		cliSmokeEqual(t, cliSmokeAt(t, mcp, "revision"), inspected["revision"])
		data := cliSmokeAt(t, responses[3], "result", "structuredContent")
		cliSmokeEqual(t, cliSmokeAt(t, data, "declaration", "name"), "State")
		cliSmokeEqual(t, cliSmokeAt(t, data, "declaration", "kind"), "enum")
		cliSmokeEqual(t, cliSmokeAt(t, responses[4], "result", "isError"), true)
		imported := cliSmokeAt(t, responses[5], "result", "structuredContent")
		cliSmokeEqual(t, cliSmokeAt(t, imported, "bindings"), bindings["bindings"])
		cliSmokeEqual(t, cliSmokeAt(t, imported, "revision"), bindings["revision"])
		capabilities, _ := cliSmokeAt(t, responses[6], "result", "structuredContent", "guardrails", "targetCapabilities").(string)
		if !strings.Contains(capabilities, "Go imports") {
			t.Fatalf("target capabilities = %q", capabilities)
		}
		catalog := cliSmokeAt(t, responses[7], "result", "structuredContent")
		if tests, _ := cliSmokeAt(t, catalog, "tests").([]any); len(tests) != 3 {
			t.Fatalf("test catalog = %s", mustJSON(t, catalog))
		}
		cliSmokeEqual(t, cliSmokeAt(t, catalog, "liveRequired"), false)
	})

	for _, target := range []string{"go", "js"} {
		t.Run("test-suite/"+target, func(t *testing.T) {
			t.Parallel()
			workspace := cliSmokeExamples(t)
			suite := cliSmokeJSON(t, binary, workspace, true, "test", "examples/testing.ef", "--target", target)
			tests, _ := cliSmokeAt(t, suite, "tests").([]any)
			if cliSmokeAt(t, suite, "passed") != true || len(tests) != 3 || cliSmokeAt(t, suite, "watchdogExpired") != false {
				t.Fatalf("suite = %s", mustJSON(t, suite))
			}
		})

		t.Run("failing-tests/"+target, func(t *testing.T) {
			t.Parallel()
			workspace := smokeWorkspace(t)
			file := cliSmokeWrite(t, workspace, "cases.ef", `effect fn test_bad() -> void raises {AssertionFailed} uses {Assert} {run Assert.equalText("actual","expected")} effect fn test_after() -> void raises {AssertionFailed} uses {Assert} {run Assert.check(true,"ok")}`)
			suite := cliSmokeJSON(t, binary, workspace, false, "test", file, "--target", target)
			tests, _ := cliSmokeAt(t, suite, "tests").([]any)
			if cliSmokeAt(t, suite, "passed") != false || len(tests) != 2 {
				t.Fatalf("suite = %s", mustJSON(t, suite))
			}
			cliSmokeEqual(t, cliSmokeAt(t, tests, 0, "reasons", 0, "tag"), "AssertionFailed")
			cliSmokeEqual(t, cliSmokeAt(t, tests, 1, "passed"), true)
			if message, _ := cliSmokeAt(t, tests, 0, "reasons", 0, "message").(string); !strings.Contains(message, `expected "expected"; received "actual"`) {
				t.Fatalf("assertion message = %q", message)
			}
		})

		// A real watchdog is separate from program time and must disclaim cleanup.
		t.Run("watchdog/"+target, func(t *testing.T) {
			t.Parallel()
			workspace := smokeWorkspace(t)
			file := cliSmokeWrite(t, workspace, "cases.ef", `effect fn test_slow() -> void {run Clock.sleep(10000).provide<Clock>(LiveClock)}`)
			if _, stderr := cliSmokeRun(t, binary, workspace, false, "test", file, "--target", target); !strings.Contains(stderr, "--live") {
				t.Fatalf("live clock without --live: stderr = %q", stderr)
			}
			suite := cliSmokeJSON(t, binary, workspace, false, "test", file, "--target", target, "--live", "--timeout-ms", "100")
			if cliSmokeAt(t, suite, "watchdogExpired") != true || cliSmokeAt(t, suite, "cleanupCompleted") != false {
				t.Fatalf("suite = %s", mustJSON(t, suite))
			}
		})
	}

	t.Run("lint-rules", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		rules := cliSmokeJSONList(t, binary, workspace, "lint", "rules")
		names := map[any]bool{}
		for index := range rules {
			names[cliSmokeAt(t, rules, index, "rule")] = true
			cliSmokeEqual(t, cliSmokeAt(t, rules, index, "builtin"), true)
		}
		cliSmokeEqual(t, names, map[any]bool{"unused-recipe": true, "redundant-provision": true, "unused-go-import": true, "invalid-suppression": true})
	})

	t.Run("graph-workflow", func(t *testing.T) {
		t.Parallel()
		workspace := cliSmokeExamples(t)
		graph := cliSmokeJSON(t, binary, workspace, true, "graph", "examples/workflow.ef")
		edges, _ := cliSmokeAt(t, graph, "edges").([]any)
		found := false
		for index := range edges {
			edge, _ := edges[index].(map[string]any)
			found = found || edge["kind"] == "requires" && edge["from"] == "function:welcome" && edge["service"] == "Directory"
		}
		if !found {
			t.Fatalf("workflow graph lacks welcome -> Directory: %s", mustJSON(t, graph))
		}
	})

	t.Run("tooling", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		source := `effect fn task() -> string { "ok" } effect fn main() -> void { let forgotten = task(); void }`
		file := cliSmokeWrite(t, workspace, "main.ef", source)
		lint := cliSmokeJSON(t, binary, workspace, false, "lint", file, "--strict")
		if cliSmokeAt(t, lint, "checked") != true || cliSmokeAt(t, lint, "lintPassed") != false || cliSmokeAt(t, lint, "warnings") != float64(1) {
			t.Fatalf("strict lint = %s", mustJSON(t, lint))
		}
		cliSmokeEqual(t, cliSmokeAt(t, cliSmokeJSON(t, binary, workspace, true, "lint", file), "lintPassed"), true)
		offset := strings.Index(source, "task();")
		query := cliSmokeJSON(t, binary, workspace, true, "query", file, strconv.Itoa(offset))
		if effect := cliSmokeAt(t, query, "expression", "type", "effect"); effect == nil || effect == false {
			t.Fatalf("query type is not an effect: %s", mustJSON(t, query))
		}
		cliSmokeEqual(t, cliSmokeAt(t, query, "expression", "type", "success"), "string")
		rules := cliSmokeJSONList(t, binary, workspace, "lint", "rules")
		replies := cliSmokeMCP(t, binary, workspace, workspace, time.Minute, append(cliSmokeHandshake(),
			cliSmokeCall(2, "project.lint", map[string]any{"file": "main.ef", "strict": true}),
			cliSmokeCall(3, "code.typeAt", map[string]any{"file": "main.ef", "offset": offset, "expectedRevision": lint["revision"]}),
			cliSmokeCall(4, "project.graph", map[string]any{"file": "main.ef"}),
			cliSmokeCall(5, "lint.rules", map[string]any{}),
			cliSmokeCall(6, "code.typeAt", map[string]any{"file": "main.ef", "offset": 1.5}),
			cliSmokeCall(7, "project.diagnostics", map[string]any{"file": "main.ef", "strict": true}),
		)...)
		if len(replies) != 7 {
			t.Fatalf("replies = %d, want 7: %v", len(replies), replies)
		}
		lintReply, _ := cliSmokeAt(t, replies[1], "result", "structuredContent", "lint").(map[string]any)
		assertReportParity(t, lintReply, lint, parityOptions{})
		cliSmokeEqual(t, cliSmokeAt(t, replies[2], "result", "structuredContent", "expression"), query["expression"])
		if nodes, _ := cliSmokeAt(t, replies[3], "result", "structuredContent", "nodes").([]any); len(nodes) == 0 {
			t.Fatalf("project.graph has no nodes: %v", replies[3])
		}
		cliSmokeEqual(t, cliSmokeAt(t, replies[4], "result", "structuredContent"), rules)
		cliSmokeEqual(t, cliSmokeAt(t, replies[5], "error", "code"), float64(-32602))
		diagnostics := cliSmokeAt(t, replies[6], "result", "structuredContent")
		if cliSmokeAt(t, diagnostics, "checked") != true || cliSmokeAt(t, diagnostics, "policyPassed") != false || cliSmokeAt(t, diagnostics, "totalCounts", "warnings") != float64(1) {
			t.Fatalf("project.diagnostics = %s", mustJSON(t, diagnostics))
		}
	})

	t.Run("diagnostics-over-limit", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		cliSmokeWrite(t, workspace, "many.ef", strings.Repeat("effect fn duplicate() -> void { void }\n", 102))
		replies := cliSmokeMCP(t, binary, workspace, workspace, time.Minute, append(cliSmokeHandshake(),
			cliSmokeCall(8, "project.diagnostics", map[string]any{"file": "many.ef"}),
		)...)
		cliSmokeEqual(t, cliSmokeAt(t, replies[1], "result", "isError"), true)
		if text, _ := cliSmokeAt(t, replies[1], "result", "content", 0, "text").(string); !strings.Contains(text, "exceeds limit") {
			t.Fatalf("over-limit refusal = %q", text)
		}
	})

	t.Run("reasoned-suppression", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		file := cliSmokeWrite(t, workspace, "main.ef", `effect fn task() -> string { "ok" }
effect fn main() -> void {
// effra-lint-disable-next-line unused-recipe -- intentional deferred hook
let forgotten = task();
void}`)
		suppressed := cliSmokeJSON(t, binary, workspace, true, "lint", file, "--strict")
		cliSmokeEqual(t, cliSmokeAt(t, suppressed, "lintPassed"), true)
		cliSmokeEqual(t, cliSmokeAt(t, suppressed, "errors"), float64(0))
		cliSmokeEqual(t, cliSmokeAt(t, suppressed, "lintDiagnostics"), []any{})
		replies := cliSmokeMCP(t, binary, workspace, workspace, time.Minute, append(cliSmokeHandshake(),
			cliSmokeCall(2, "project.lint", map[string]any{"file": "main.ef", "strict": true}),
		)...)
		lintReply, _ := cliSmokeAt(t, replies[1], "result", "structuredContent", "lint").(map[string]any)
		assertReportParity(t, lintReply, suppressed, parityOptions{})
	})

	// Deeply nested data construction stays bounded on the CLI and does not
	// hold the MCP server's queue: a ping queued behind it is still answered.
	t.Run("deep-type-at", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t)
		for _, c := range []struct {
			name  string
			named bool
		}{{"nested.ef", true}, {"positional.ef", false}} {
			source := cliSmokeNestedData(30, c.named)
			file := cliSmokeWrite(t, workspace, c.name, source)
			offset := cliSmokeDeepOffset(source)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			command := exec.CommandContext(ctx, binary, "query", file, strconv.Itoa(offset))
			command.Dir = workspace
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			cancel()
			if err != nil {
				t.Fatalf("%s query: %v stdout=%q stderr=%q", c.name, err, stdout.String(), stderr.String())
			}
			payload := smokeJSON(t, stdout.Bytes())
			if cliSmokeAt(t, payload, "checked") != true || cliSmokeAt(t, payload, "expression", "span", "offset") != float64(offset) {
				t.Fatalf("%s query = %s", c.name, mustJSON(t, payload))
			}
		}
		offset := cliSmokeDeepOffset(cliSmokeNestedData(30, true))
		replies := cliSmokeMCP(t, binary, workspace, workspace, 5*time.Second,
			map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
				"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
				"clientInfo": map[string]any{"name": "type-at-smoke", "version": "1"}}},
			map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
			cliSmokeCall(2, "code.typeAt", map[string]any{"file": "nested.ef", "offset": offset}),
			map[string]any{"jsonrpc": "2.0", "id": 3, "method": "ping"},
		)
		var ids []any
		for _, reply := range replies {
			ids = append(ids, reply["id"])
		}
		cliSmokeEqual(t, ids, []any{float64(1), float64(2), float64(3)})
		cliSmokeEqual(t, cliSmokeAt(t, replies[2], "result"), map[string]any{})
		typeAt := cliSmokeAt(t, replies[1], "result", "structuredContent")
		if cliSmokeAt(t, typeAt, "checked") != true || cliSmokeAt(t, typeAt, "expression", "span", "offset") != float64(offset) {
			t.Fatalf("deep code.typeAt = %s", mustJSON(t, typeAt))
		}
	})
}

// cliSmokeExamples is a workspace holding the authored examples with the
// module and bindings their Go imports resolve against.
func cliSmokeExamples(t *testing.T) string {
	t.Helper()
	return smokeWorkspace(t, "examples", "go.mod", "effra.bindings.json")
}

// cliSmokeWrite writes one source file into dir and returns its path.
func cliSmokeWrite(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// cliSmokeRun runs the CLI in dir and requires it to succeed or to fail.
func cliSmokeRun(t *testing.T, binary, dir string, success bool, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, code := runTestCLIDir(t, binary, dir, "", args...)
	if (code == 0) != success {
		t.Fatalf("ef %v: exit %d, want success=%v\nstdout=%s\nstderr=%s", args, code, success, stdout, stderr)
	}
	return string(stdout), string(stderr)
}

func cliSmokeJSON(t *testing.T, binary, dir string, success bool, args ...string) map[string]any {
	t.Helper()
	stdout, _ := cliSmokeRun(t, binary, dir, success, args...)
	return smokeJSON(t, []byte(stdout))
}

func cliSmokeJSONList(t *testing.T, binary, dir string, args ...string) []any {
	t.Helper()
	stdout, _ := cliSmokeRun(t, binary, dir, true, args...)
	var value []any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatalf("ef %v: not a JSON array: %v\n%s", args, err, stdout)
	}
	return value
}

// cliSmokeAt walks decoded JSON by object keys and array indices and fails
// the test when a step is absent, as a Python subscript would.
func cliSmokeAt(t *testing.T, value any, path ...any) any {
	t.Helper()
	current := value
	for depth, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("%v: not an object at %v: %s", path, path[:depth], mustJSON(t, value))
			}
			next, ok := object[key]
			if !ok {
				t.Fatalf("%v: missing %q: %s", path, key, mustJSON(t, value))
			}
			current = next
		case int:
			list, ok := current.([]any)
			if !ok || key >= len(list) {
				t.Fatalf("%v: no index %d: %s", path, key, mustJSON(t, value))
			}
			current = list[key]
		default:
			panic(fmt.Sprintf("path step %T", step))
		}
	}
	return current
}

func cliSmokeEqual(t *testing.T, actual, expected any) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s, want %s", mustJSON(t, actual), mustJSON(t, expected))
	}
}

func cliSmokeHasCode(t *testing.T, report map[string]any, code string) bool {
	t.Helper()
	diagnostics, _ := cliSmokeAt(t, report, "diagnostics").([]any)
	for index := range diagnostics {
		if cliSmokeAt(t, diagnostics, index, "code") == code {
			return true
		}
	}
	return false
}

func cliSmokeCooperativeBinding(t *testing.T, report map[string]any) bool {
	t.Helper()
	bindings, _ := cliSmokeAt(t, report, "bindings").([]any)
	for index := range bindings {
		forward := cliSmokeAt(t, bindings, index, "forwardContext")
		if forward != nil && forward != false && cliSmokeAt(t, bindings, index, "cancellation") == "cooperative" {
			return true
		}
	}
	return false
}

func cliSmokeHandshake() []any {
	return []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "smoke", "version": "1"}}},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
	}
}

func cliSmokeCall(id int, name string, arguments map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}
}

// cliSmokeMCP writes every message to a stdio MCP server for workspace,
// closes its input and returns the decoded responses once it exits.
func cliSmokeMCP(t *testing.T, binary, dir, workspace string, deadline time.Duration, messages ...any) []map[string]any {
	t.Helper()
	var input bytes.Buffer
	for _, message := range messages {
		input.Write(smokeDumps(t, message))
		input.WriteByte('\n')
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "mcp", workspace)
	command.Dir = dir
	command.Stdin = &input
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("MCP server did not finish within %s\nstdout=%s\nstderr=%s", deadline, stdout.String(), stderr.String())
	}
	if err != nil {
		t.Fatalf("MCP server: %v\nstderr=%s", err, stderr.String())
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		responses = append(responses, smokeJSON(t, []byte(line)))
	}
	return responses
}

// cliSmokeNestedData is a depth+1 chain of single-field records constructed
// in one expression, with named or positional arguments.
func cliSmokeNestedData(depth int, named bool) string {
	var records, calls strings.Builder
	for index := 0; index <= depth; index++ {
		field := "void"
		if index > 0 {
			field = "R" + strconv.Itoa(index-1)
		}
		if index > 0 {
			records.WriteByte('\n')
		}
		fmt.Fprintf(&records, "record R%d { value: %s }", index, field)
	}
	for index := depth; index >= 0; index-- {
		calls.WriteString("R" + strconv.Itoa(index) + "(")
		if named {
			calls.WriteString("value: ")
		}
	}
	return records.String() + "\nfn deep() -> R" + strconv.Itoa(depth) + " { " + calls.String() + "void" + strings.Repeat(")", depth+1) + " }\neffect fn main() -> void { let _ = deep(); void }\n"
}

// cliSmokeDeepOffset is the offset of the innermost void in deep's body.
func cliSmokeDeepOffset(source string) int {
	body := strings.Index(source[strings.Index(source, "fn deep"):], "{ ") + strings.Index(source, "fn deep") + 2
	return strings.Index(source[body:], "void") + body
}
