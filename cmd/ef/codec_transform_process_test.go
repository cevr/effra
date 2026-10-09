package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
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

// codecTransformFinalizer holds a transformation mid-stream while the
// listener its scope owns is bound, then rebinds that address. Http.listen
// acquires the listener in its owning scope and releases it when that
// scope closes, the only scoped resource with a finalizer on every target.
const codecTransformFinalizer = `import Json "effra/json"

record Ticket {
    code: string
}

record Batch {
    first: Ticket
    second: Ticket
}

service Probe {
    effect fn arrived() -> Latch
    effect fn reached() -> Latch
}

impl Gate(arrival: Latch, reached: Latch) for Probe {
    effect fn arrived() -> Latch { arrival }
    effect fn reached() -> Latch { reached }
}

effect fn arrive(request: HttpRequest) -> HttpReply uses { Probe, Sync } {
    let arrival = run Probe.arrived()
    run Sync.signal(arrival)
    HttpReply.NotFound {}
}

fn limits() -> HttpLimits {
    HttpLimits {
        maxBodyBytes: 16,
        readHeaderMillis: 1000,
        readBodyMillis: 1000,
        idleMillis: 1000,
        maxActive: 1
    }
}

// The listener is the transformation's scoped resource: the scope that
// owns the serving child releases it when the transformation is
// interrupted.
effect fn hold() -> void raises { IoError } uses { Probe, Sync, Http, Env } {
    let address = run Env.get("EFFRA_PROBE_ADDRESS")
    let never = run Sync.latch()
    scope {
        let serving = fork Http.listen(address, limits(), arrive)
        run Sync.await(never)
    }
}

effect fn resolveTicket(code: string) -> Ticket raises { IoError } uses { Probe, Sync, Http, Env } {
    if code == "held" {
        run hold()
    } else {
        let reached = run Probe.reached()
        run Sync.signal(reached)
    }
    Ticket { code: code }
}

effect fn ticketCode(ticket: Ticket) -> string raises { IoError } uses { Probe, Sync, Http, Env } {
    run resolveTicket(ticket.code)
    ticket.code
}

derive ticketJson = Json.codec<Ticket>(maxBodyBytes: 64, maxDepth: 1, decode: resolveTicket, encode: ticketCode)
derive batchJson = Json.codec<Batch>(maxBodyBytes: 256, maxDepth: 2) with ticketJson

// interrupted holds a transformation mid-stream, then rebinds the address
// its listener holds. With direction "decode" or "encode" the
// transformation is interrupted first, so the rebind succeeds only if the
// interrupt returned after the transformation's scope released the
// listener. The "held" control rebinds before interrupting, which must be
// refused with IoError; it interrupts only after observing the refusal, so
// no failure of its body meets a pending failure of the child.
effect fn interrupted(direction: string) -> void raises { IoError, JsonDecodeFailure, JsonEncodeFailure } uses { Sync, Http, Env, Console } {
    let address = run Env.get("EFFRA_PROBE_ADDRESS")
    let arrival = run Sync.latch()
    let reached = run Sync.latch()
    let gate = run Gate(arrival, reached)
    scope {
        if direction == "encode" {
            let working = fork batchJson.encode(Batch { first: Ticket { code: "held" }, second: Ticket { code: "fast" } }).provide<Probe>(gate)
            run Sync.await(arrival)
            run working.interrupt()
            run Console.log(direction + " interrupted")
            run Http.listen(address, limits(), arrive).provide<Probe>(gate)
        } else {
            let working = fork batchJson.decode("{\"first\":\"held\",\"second\":\"fast\"}").provide<Probe>(gate)
            run Sync.await(arrival)
            if direction == "decode" {
                run working.interrupt()
                run Console.log(direction + " interrupted")
                run Http.listen(address, limits(), arrive).provide<Probe>(gate)
            } else {
                run Console.log(direction + " interrupted")
                // A bound listener serves until interrupted, so only a
                // refused rebind returns here.
                run Http.listen(address, limits(), arrive).provide<Probe>(gate).catch<IoError>(void)
                run Console.log("rebind refused: IoError")
                run working.interrupt()
            }
        }
    }
}

effect fn main() -> void raises { IoError, JsonDecodeFailure, JsonEncodeFailure } {
    let direction = run Env.get("EFFRA_PROBE_DIRECTION").provide<Env>(LiveEnv)
    run interrupted(direction)
        .provide<Sync>(TestSync)
        .provide<Http>(LiveHttp)
        .provide<Env>(LiveEnv)
        .provide<Console>(Stdout)
}
`

// codecFinalizerProcess is one running fixture executable: its stdout lines
// in order, closed at end of output, and done once it has been reaped.
type codecFinalizerProcess struct {
	command *exec.Cmd
	lines   chan string
	stderr  bytes.Buffer
	done    chan struct{}
}

func startCodecFinalizer(t *testing.T, dir, address, direction string, argv ...string) *codecFinalizerProcess {
	t.Helper()
	process := &codecFinalizerProcess{command: exec.Command(argv[0], argv[1:]...), lines: make(chan string, 16), done: make(chan struct{})}
	process.command.Dir = dir
	process.command.Env = append(os.Environ(), "EFFRA_PROBE_ADDRESS="+address, "EFFRA_PROBE_DIRECTION="+direction)
	process.command.Stderr = &process.stderr
	process.command.WaitDelay = smokeWaitDelay
	stdout, err := process.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			process.lines <- scanner.Text()
		}
		close(process.lines)
		_ = process.command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		_ = process.command.Process.Kill()
		<-process.done
	})
	return process
}

// next returns the next stdout line, or "" with false once output ended.
func (p *codecFinalizerProcess) next(t *testing.T) (string, bool) {
	t.Helper()
	select {
	case line, ok := <-p.lines:
		return line, ok
	case <-time.After(10 * time.Second):
		// stderr is still being written; it is read only once reaped.
		t.Fatal("no output within 10s")
		return "", false
	}
}

func (p *codecFinalizerProcess) expect(t *testing.T, want string) {
	t.Helper()
	if line, ok := p.next(t); !ok || line != want {
		t.Fatalf("stdout %q (open %v), want %q", line, ok, want)
	}
}

// codecFinalizerArrive asks the bound listener for a request, whose handler
// signals the fixture that the transformation holds it.
func codecFinalizerArrive(t *testing.T, address string) {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://" + address + "/arrive")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("handler answered %d", response.StatusCode)
	}
}

// A transformation interrupted mid-stream completes its scope's cleanup
// before the interrupt returns, on Go, Bun and Node and in both directions:
// the first transformation of a Batch holds a listener that its scope owns,
// the fixture interrupts the direction, and only then rebinds the
// listener's address, which succeeds only if the listener was released.
// Negative control: rebinding while the transformation still holds the
// listener is refused with IoError on every target, and the fixture says so.
func TestCodecTransformationInterruptReleasesItsScopedResource(t *testing.T) {
	t.Parallel()
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "go.mod", "effra.bindings.json")
	if err := os.WriteFile(filepath.Join(workspace, "finalizer.ef"), []byte(codecTransformFinalizer), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", "finalizer.ef")
	if code != 0 {
		t.Fatalf("go build: %s%s", stdout, stderr)
	}
	native := httpSmokeArtifact(workspace, stdout)
	stdout, stderr, code = runTestCLIDir(t, binary, workspace, "", "build", "finalizer.ef", "--target", "js", "--entry")
	if code != 0 {
		t.Fatalf("js build: %s%s", stdout, stderr)
	}
	module := httpSmokeArtifact(workspace, stdout)
	cohorts := map[string][]string{"go": {native}, "bun": {"bun", module}, "node": {"node", module}}
	for cohort, argv := range cohorts {
		for _, direction := range []string{"decode", "encode", "held"} {
			t.Run(cohort+"/"+direction, func(t *testing.T) {
				t.Parallel()
				if cohort != "go" {
					if _, err := exec.LookPath(argv[0]); err != nil {
						t.Fatalf("%s is required", argv[0])
					}
				}
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				address := listener.Addr().String()
				listener.Close()
				process := startCodecFinalizer(t, workspace, address, direction, argv...)
				process.expect(t, "listening http://"+address)
				codecFinalizerArrive(t, address)
				process.expect(t, direction+" interrupted")
				if direction == "held" {
					process.expect(t, "rebind refused: IoError")
					if line, ok := process.next(t); ok {
						t.Fatalf("rebinding a held listener printed %q", line)
					}
					<-process.done
					if code := process.command.ProcessState.ExitCode(); code != 0 {
						t.Fatalf("held control: exit %d %s", code, process.stderr.String())
					}
					return
				}
				process.expect(t, "listening http://"+address)
				codecFinalizerArrive(t, address)
			})
		}
	}
}
