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

// codecTransformCancellation interrupts a decode while its first
// transformation holds a scope-owned child. The interrupt returns only once
// that transformation's scope closed, and the second transformation never
// runs; the uninterrupted control reaches it.
const codecTransformCancellation = `import Json "effra/json"

record Ticket {
    code: string
}

record Batch {
    first: Ticket
    second: Ticket
}

service Probe {
    effect fn arrived() -> Latch
    effect fn blocker() -> Latch
    effect fn reached() -> Latch
}

impl Gate(arrival: Latch, blocker: Latch, reached: Latch) for Probe {
    effect fn arrived() -> Latch { arrival }
    effect fn blocker() -> Latch { blocker }
    effect fn reached() -> Latch { reached }
}

effect fn park(latch: Latch) -> void uses { Sync } {
    run Sync.await(latch)
}

effect fn reachedWithin(latch: Latch) -> string uses { Sync } {
    run Sync.await(latch)
    "reached"
}

effect fn resolveTicket(code: string) -> Ticket uses { Probe, Sync } {
    if code == "slow" {
        let arrival = run Probe.arrived()
        let blocker = run Probe.blocker()
        scope {
            let owned = fork park(blocker)
            run Sync.signal(arrival)
            run Sync.await(blocker)
        }
    } else {
        let reached = run Probe.reached()
        run Sync.signal(reached)
    }
    Ticket { code: code }
}

fn ticketCode(ticket: Ticket) -> string {
    ticket.code
}

derive ticketJson = Json.codec<Ticket>(maxBodyBytes: 64, maxDepth: 1, decode: resolveTicket, encode: ticketCode)
derive batchJson = Json.codec<Batch>(maxBodyBytes: 256, maxDepth: 2) with ticketJson

effect fn test_interrupted_decode_runs_no_later_transformation() -> void raises { AssertionFailed, JsonDecodeFailure, Timeout } uses { Assert, Sync, Clock, Scheduler } {
    let arrival = run Sync.latch()
    let blocker = run Sync.latch()
    let reached = run Sync.latch()
    let gate = run Gate(arrival, blocker, reached)
    scope {
        let decoding = fork batchJson.decode("{\"first\":\"slow\",\"second\":\"fast\"}").provide<Probe>(gate)
        run Sync.await(arrival)
        run decoding.interrupt()
    }
    let probe = fork reachedWithin(reached).timeout(10)
    run Scheduler.awaitRegistration()
    run Scheduler.advance(10)
    let outcome = run probe.join().catch<Timeout>("not reached")
    run Assert.equalText(outcome, "not reached")
}

effect fn test_uninterrupted_decode_reaches_every_transformation() -> void raises { AssertionFailed, JsonDecodeFailure } uses { Assert, Sync } {
    let arrival = run Sync.latch()
    let blocker = run Sync.latch()
    let reached = run Sync.latch()
    let gate = run Gate(arrival, blocker, reached)
    let batch = run batchJson.decode("{\"first\":\"fast\",\"second\":\"fast\"}").provide<Probe>(gate)
    run Assert.equalText(batch.second.code, "fast")
    run Assert.equalText(run reachedWithin(reached), "reached")
}
`

type codecTransformReport struct {
	Passed bool `json:"passed"`
	Tests  []struct {
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
	} `json:"tests"`
}

// The authored transformation example and the cancellation fixture pass
// every test on both targets through the actual CLI.
func TestCodecTransformationTestsPassOnGoAndJS(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	cancellation := filepath.Join(root, "cancellation.ef")
	if err := os.WriteFile(cancellation, []byte(codecTransformCancellation), 0600); err != nil {
		t.Fatal(err)
	}
	example, err := filepath.Abs(filepath.Join("..", "..", "examples", "codec-transforms.ef"))
	if err != nil {
		t.Fatal(err)
	}
	suites := map[string][]string{
		example: {
			"test_pure_nominal_conversion_round_trips", "test_normalizing_codec_canonicalizes", "test_refinement_rejects_on_decode",
			"test_encode_side_rejection", "test_nested_composition_round_trips", "test_nested_transformation_failure_keeps_its_type",
			"test_malformed_input_never_reaches_a_service", "test_service_failure_is_not_malformed_input",
			"test_encode_runs_only_encode_services", "test_configuration_caller_shares_the_witnesses",
		},
		cancellation: {"test_interrupted_decode_runs_no_later_transformation", "test_uninterrupted_decode_reaches_every_transformation"},
	}
	for path, want := range suites {
		for _, target := range []string{"go", "js"} {
			stdout, stderr, code := runTestCLIDir(t, binary, root, "", "test", path, "--target", target)
			var report codecTransformReport
			if err := json.Unmarshal(stdout, &report); err != nil || code != 0 || !report.Passed {
				t.Fatalf("%s %s: code %d %v\nstdout=%s\nstderr=%s", filepath.Base(path), target, code, err, stdout, stderr)
			}
			var ran []string
			for _, test := range report.Tests {
				if !test.Passed {
					t.Fatalf("%s %s: %s failed", filepath.Base(path), target, test.Name)
				}
				ran = append(ran, test.Name)
			}
			if !slices.Equal(ran, want) {
				t.Fatalf("%s %s ran %q, want %q", filepath.Base(path), target, ran, want)
			}
		}
	}
}

// Transformed witnesses, their per-direction transformation contributions
// and their transform plan nodes are identical in the CLI and MCP envelopes.
func TestCodecTransformationFactsMatchAcrossCLIAndMCP(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "codec-transforms.ef"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "transforms.ef"), source, 0600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"codec-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"transforms.ef"}}}`,
	}, "\n") + "\n"
	command := exec.Command(binary, "mcp", root)
	command.Stdin = strings.NewReader(input)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("unexpected codec MCP response count %d", len(lines))
	}
	mcp := readProcessJSON(t, lines[1])["result"].(map[string]any)["structuredContent"].(map[string]any)
	stdout, stderr, code := runTestCLI(t, binary, "check", filepath.Join(root, "transforms.ef"))
	if code != 0 {
		t.Fatalf("codec check: %s", stderr)
	}
	check := readProcessJSON(t, stdout)
	assertResponseReferences(t, check)
	for _, key := range []string{"codecs", "codecPlans", "symbols", "diagnostics"} {
		if !reflect.DeepEqual(check[key], mcp[key]) {
			t.Fatalf("actual CLI/MCP %s differ", key)
		}
	}
	witnesses := map[string]map[string]any{}
	for _, codec := range check["codecs"].([]any) {
		witnesses[codec.(map[string]any)["name"].(string)] = codec.(map[string]any)
	}
	order := witnesses["orderJson"]
	if !reflect.DeepEqual(order["with"], []any{"userIdJson", "memberJson", "quantityJson", "statusJson"}) || order["representation"] != nil {
		t.Fatalf("order witness: %v", order)
	}
	paths := func(direction string) []string {
		out := []string{}
		for _, step := range order[direction].(map[string]any)["transformations"].([]any) {
			step := step.(map[string]any)
			out = append(out, step["witness"].(string)+"@"+step["path"].(string))
		}
		return out
	}
	want := []string{"userIdJson@Order.buyer", "memberJson@Order.member", "quantityJson@Order.quantity", "statusJson@Order.status"}
	if !slices.Equal(paths("decode"), want) || !slices.Equal(paths("encode"), want) {
		t.Fatalf("transformation paths: %q %q", paths("decode"), paths("encode"))
	}
	if decode := order["decode"].(map[string]any); !reflect.DeepEqual(decode["requirements"], []any{"Directory"}) {
		t.Fatalf("order decode requirements: %v", decode)
	}
	if encode := order["encode"].(map[string]any); !reflect.DeepEqual(encode["requirements"], []any{"Audit"}) {
		t.Fatalf("order encode requirements: %v", encode)
	}
	if member := witnesses["memberJson"]; member["representation"] != "MemberWire" {
		t.Fatalf("member witness: %v", member)
	}
	transforms := 0
	for _, plan := range check["codecPlans"].([]any) {
		for _, node := range plan.(map[string]any)["nodes"].([]any) {
			if node.(map[string]any)["kind"] == "transform" {
				transforms++
			}
		}
	}
	if transforms == 0 {
		t.Fatal("no transform plan node is projected")
	}
}
