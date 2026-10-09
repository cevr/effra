package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// endpointTerminalTest is appended to the endpoint example. Its order makes
// the output encoder raise Unpublishable, whose failure body cannot be
// encoded within its witness's bound: the failure-body encoding is
// terminal, so the call fails with JsonEncodeFailure followed by the mapped
// failure it could not answer, and is never mapped again.
const endpointTerminalTest = `
effect fn test_failed_failure_body_keeps_both_causes() -> void raises { JsonEncodeFailure, NothingToCancel, OutOfStock, UnknownMember, Unpublishable } {
    let code = run order("application/json", "{\"member\":\"m-1\",\"item\":\"tea\",\"quantity\":\"3\"}").provide<Http>(LiveHttp).provide<Directory>(Members).provide<Inventory>(Stock).provide<Ledger>(Books)
}
`

type endpointTestReport struct {
	Passed bool `json:"passed"`
	Tests  []struct {
		Name    string `json:"name"`
		Passed  bool   `json:"passed"`
		Reasons []struct {
			Kind    string `json:"kind"`
			Tag     string `json:"tag"`
			Message string `json:"message"`
		} `json:"reasons"`
	} `json:"tests"`
}

// The endpoint example's tests pass on both targets through the actual CLI,
// and a failed failure-body encoding keeps both causes, in the same order,
// on both targets.
func TestEndpointTestsPassOnGoAndJS(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	example, err := filepath.Abs(filepath.Join("..", "..", "examples", "http-endpoints.ef"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	terminal := filepath.Join(root, "terminal.ef")
	if err := os.WriteFile(terminal, append(source, endpointTerminalTest...), 0600); err != nil {
		t.Fatal(err)
	}
	want := []string{"test_order_is_created", "test_unknown_member_is_not_malformed_input", "test_handler_failure_answers_its_status", "test_unsupported_media_type", "test_stored_order_shares_the_witness"}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", "test", example, "--target", target)
		var report endpointTestReport
		if err := json.Unmarshal(stdout, &report); err != nil || code != 0 || !report.Passed {
			t.Fatalf("%s: code %d %v\nstdout=%s\nstderr=%s", target, code, err, stdout, stderr)
		}
		ran := []string{}
		for _, test := range report.Tests {
			ran = append(ran, test.Name)
		}
		if !slices.Equal(ran, want) {
			t.Fatalf("%s ran %q, want %q", target, ran, want)
		}
		stdout, stderr, code = runTestCLIDir(t, binary, root, "", "test", terminal, "--target", target)
		report = endpointTestReport{}
		if err := json.Unmarshal(stdout, &report); err != nil || code == 0 || report.Passed {
			t.Fatalf("%s terminal: code %d %v\nstdout=%s\nstderr=%s", target, code, err, stdout, stderr)
		}
		last := report.Tests[len(report.Tests)-1]
		if last.Name != "test_failed_failure_body_keeps_both_causes" || last.Passed || len(last.Reasons) != 2 {
			t.Fatalf("%s terminal report: %+v", target, last)
		}
		if first, second := last.Reasons[0], last.Reasons[1]; first.Kind != "failure" || first.Tag != "JsonEncodeFailure" || !strings.Contains(first.Message, "body-too-large") || second.Kind != "failure" || second.Tag != "Unpublishable" {
			t.Fatalf("%s terminal reasons: %+v", target, last.Reasons)
		}
	}
}

// Endpoints, failure witnesses and their plans are identical in the CLI and
// MCP envelopes.
func TestEndpointFactsMatchAcrossCLIAndMCP(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "http-endpoints.ef"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "endpoints.ef"), source, 0600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"endpoint-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"endpoints.ef"}}}`,
	}, "\n") + "\n"
	command := exec.Command(binary, "mcp", root)
	command.Stdin = strings.NewReader(input)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("unexpected endpoint MCP response count %d", len(lines))
	}
	mcp := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	stdout, stderr, code := runTestCLI(t, binary, "check", filepath.Join(root, "endpoints.ef"))
	if code != 0 {
		t.Fatalf("endpoint check: %s", stderr)
	}
	check := readProcessJSON(t, stdout)
	assertResponseReferences(t, check)
	for _, key := range []string{"endpoints", "codecs", "codecPlans", "symbols", "diagnostics"} {
		if check[key] == nil || !reflect.DeepEqual(check[key], mcp[key]) {
			t.Fatalf("actual CLI/MCP %s differ or are absent", key)
		}
	}
	names := []string{}
	for _, endpoint := range check["endpoints"].([]any) {
		names = append(names, endpoint.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"placeOrder", "cancelOrder", "health", "api"}) {
		t.Fatalf("endpoints %q", names)
	}
}

// endpointNameFixture maps a failure whose legal name is __proto__ beside an
// ordinary failure name, and calls both endpoints directly.
const endpointNameFixture = `error __proto__
error Taken

effect fn proto() -> void raises { __proto__ } {
    fail __proto__
}

effect fn taken() -> void raises { Taken } {
    fail Taken
}

derive protoEndpoint = Http.endpoint(method: "POST", path: "/proto", handler: proto, status: 204) raises { __proto__: 409 }
derive takenEndpoint = Http.endpoint(method: "POST", path: "/taken", handler: taken, status: 204) raises { Taken: 409 }

fn status(reply: HttpReply) -> i64 {
    match reply {
        HttpReply.Respond { response } => response.status
        HttpReply.BadRequest => 400
        HttpReply.NotFound => 404
        HttpReply.UnsupportedMediaType => 415
    }
}

effect fn request(path: string) -> HttpRequest uses { Http } {
    let body = run Http.text("")
    HttpRequest {
        method: "POST",
        path: path,
        contentType: "",
        body: body
    }
}

effect fn test_proto_failure_is_mapped() -> void raises { AssertionFailed, __proto__ } uses { Assert } {
    let reply = run protoEndpoint(run request("/proto").provide<Http>(LiveHttp))
    run Assert.check(status(reply) == 409, "a failure named __proto__ answers its mapped status")
}

effect fn test_ordinary_failure_is_mapped() -> void raises { AssertionFailed, Taken } uses { Assert } {
    let reply = run takenEndpoint(run request("/taken").provide<Http>(LiveHttp))
    run Assert.check(status(reply) == 409, "an ordinary failure answers its mapped status")
}

effect fn main() -> void {
}
`

// A failure named __proto__ is an ordinary mapping key on both targets, like
// the ordinary-name control: JS lowers mappings to a Map, never an object
// literal whose __proto__ entry would set its prototype.
func TestEndpointFailureNamesAreOrdinaryKeys(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	fixture := filepath.Join(root, "names.ef")
	if err := os.WriteFile(fixture, []byte(endpointNameFixture), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", "test", fixture, "--target", target)
		var report endpointTestReport
		if err := json.Unmarshal(stdout, &report); err != nil || code != 0 || !report.Passed || len(report.Tests) != 2 {
			t.Fatalf("%s: code %d %v\nstdout=%s\nstderr=%s", target, code, err, stdout, stderr)
		}
	}
}
