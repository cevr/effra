package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ported from scripts/producer_smoke.py: producer image identity follows the
// executing artifact rather than its installed pathname, stale facts are
// refused without holding the request queue, dirty hosts are distinguished by
// digest, an unavailable identity stays process-scoped, and compilation never
// acquires identity. Variant compilers are built from this package's source
// with Go build overlays, each phase in its own workspace.
func TestProducerSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const source = "effect fn main() -> string { \"ok\" }\n"
	ok := strings.Index(source, `"ok"`)
	workspace := func(t *testing.T) (string, string) {
		directory := t.TempDir()
		return directory, cliSmokeWrite(t, directory, "main.ef", source)
	}
	mainPath := filepath.Join(root, "cmd", "ef", "main.go")
	ownerPath := filepath.Join(root, "internal", "producer", "identity.go")
	implementation := producerSmokeRead(t, ownerPath)

	t.Run("artifact-replacement", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		original := producerSmokeRead(t, mainPath)
		if !strings.Contains(original, `Effra prototype\nusage:`) {
			t.Fatal("usage banner anchor is missing from cmd/ef/main.go")
		}
		binaries := producerSmokeOverlays(t, root, directory, mainPath, map[string]string{
			"compiler-A": strings.Replace(original, `Effra prototype\nusage:`, `Effra producer-A\nusage:`, 1),
			"compiler-B": strings.Replace(original, `Effra prototype\nusage:`, `Effra producer-B\nusage:`, 1),
		})
		first, second := binaries["compiler-A"], binaries["compiler-B"]
		firstDigest, secondDigest := producerSmokeDigest(t, first), producerSmokeDigest(t, second)
		if firstDigest == secondDigest {
			t.Fatalf("variant compilers share digest %s", firstDigest)
		}

		// First acquisition occurs after pathname replacement, so a pathname
		// hash cannot accidentally pass by having been cached before it.
		installed := filepath.Join(directory, "ef")
		producerSmokeInstall(t, first, installed)
		old := producerSmokeStart(t, installed, directory)
		replacement := filepath.Join(directory, "replacement")
		producerSmokeInstall(t, second, replacement)
		if err := os.Rename(replacement, installed); err != nil {
			t.Fatal(err)
		}
		originalFacts := producerSmokeFacts(t, old, "project.check", nil)
		originalProducer := cliSmokeAt(t, originalFacts, "producer").(map[string]any)
		cliSmokeEqual(t, originalProducer["digest"], firstDigest)
		cliSmokeEqual(t, originalProducer["strength"], "executing-artifact")
		key, revision := cliSmokeAt(t, originalProducer, "qualifier"), cliSmokeAt(t, originalFacts, "revision")
		for _, target := range []string{"go", "js"} {
			selected := producerSmokeFacts(t, old, "code.type", map[string]any{"target": target, "symbol": "main", "expectedRevision": revision, "expectedProducer": key})
			cliSmokeEqual(t, selected["producer"], originalProducer)
			cliSmokeEqual(t, selected["snapshot"], map[string]any{"schemaVersion": selected["schemaVersion"], "revision": revision, "target": target, "producer": key, "reuseScope": "artifact"})
			for _, c := range []struct {
				name string
				args map[string]any
			}{
				{"project.check", map[string]any{}}, {"code.inspect", map[string]any{"symbol": "main"}},
				{"code.typeAt", map[string]any{"offset": ok}}, {"project.graph", map[string]any{}},
				{"project.diagnostics", map[string]any{}}, {"project.lint", map[string]any{}},
			} {
				c.args["target"], c.args["expectedRevision"], c.args["expectedProducer"] = target, revision, key
				value := producerSmokeFacts(t, old, c.name, c.args)
				if c.name == "project.lint" {
					value = cliSmokeAt(t, value, "lint").(map[string]any)
				}
				cliSmokeEqual(t, value["producer"], originalProducer)
				cliSmokeEqual(t, value["snapshot"], map[string]any{"revision": revision, "target": target, "schemaVersion": originalFacts["schemaVersion"], "producer": key, "reuseScope": "artifact"})
			}
			// CLI acquisition is an independent process of the same artifact;
			// adapter parity must follow content identity, not process ID.
			for _, c := range []struct {
				command string
				args    []string
			}{
				{"check", nil}, {"inspect", []string{"main"}}, {"query", []string{strconv.Itoa(ok)}},
				{"type", []string{"--symbol", "main"}}, {"graph", nil}, {"diagnostics", []string{"--json"}}, {"lint", nil},
			} {
				args := append(append([]string{c.command, file}, c.args...), "--target", target)
				encoded := producerSmokeRun(t, directory, first, args...)
				cli := smokeJSON(t, []byte(encoded))
				cliSmokeEqual(t, cli["producer"], originalProducer)
				cliSmokeEqual(t, cliSmokeAt(t, cli, "snapshot", "producer"), key)
				if c.command == "graph" {
					cliSmokeEqual(t, cliSmokeAt(t, cli, "typeProjectionUsage", "responseBytes"), float64(len(strings.TrimRight(encoded, "\n"))))
				}
				if c.command == "type" {
					cliSmokeEqual(t, cli["snapshot"], selected["snapshot"])
					cliSmokeEqual(t, cliSmokeAt(t, cli, "querySchemaVersion"), float64(2))
					cliSmokeEqual(t, cliSmokeAt(t, selected, "querySchemaVersion"), float64(2))
				}
			}
		}

		profile := cliSmokeWrite(t, directory, "profile.ef", `import Fns "effra/functions"
error MissingProfile
service Profiles { effect fn name(id: string) -> string raises {MissingProfile} }
effect fn load(id: string) -> string raises {MissingProfile} uses {Profiles} { run Profiles.name(id) }
effect fn greeting(id: string) -> string raises {MissingProfile} uses {Profiles} { run Fns.call(load, id) }
`)
		for _, target := range []string{"go", "js"} {
			remote := cliSmokeAt(t, old.tool("code.inspect", map[string]any{"file": "profile.ef", "symbol": "greeting", "target": target, "expectedProducer": key}), "structuredContent").(map[string]any)
			cli := smokeJSON(t, []byte(producerSmokeRun(t, directory, first, "inspect", profile, "greeting", "--target", target)))
			assertReportParity(t, remote, cli, parityOptions{target: target, ignored: []string{"file", "timings"}, project: adapterSemantic})
			if remote["revision"] == revision {
				t.Fatalf("profile shares main's revision %v", revision)
			}
			if interfaces, _ := cliSmokeAt(t, remote, "bundledInterfaces").([]any); len(interfaces) == 0 {
				t.Fatalf("profile inspection lacks bundled interfaces: %s", mustJSON(t, remote))
			}
			cliSmokeEqual(t, cliSmokeAt(t, remote, "symbol", "contract", "requirements"), []any{"Profiles"})
		}
		formatted := cliSmokeAt(t, old.tool("code.format", map[string]any{"source": source, "expectedProducer": key}), "structuredContent").(map[string]any)
		cliSmokeEqual(t, formatted["producer"], originalProducer)
		if version := cliSmokeAt(t, formatted, "formatterVersion"); version == nil || version == "" {
			t.Fatalf("code.format lacks a formatter version: %s", mustJSON(t, formatted))
		}
		stdout, stderr, code := runTestCLIDir(t, first, directory, "", "fmt", file, "--check", "--json")
		if code != 0 && code != 1 {
			t.Fatalf("fmt --check: exit %d stderr=%s", code, stderr)
		}
		cliSmokeEqual(t, cliSmokeAt(t, smokeJSON(t, stdout), "producer"), formatted["producer"])

		fresh := producerSmokeStart(t, installed, directory)
		current := producerSmokeFacts(t, fresh, "project.check", nil)
		cliSmokeEqual(t, cliSmokeAt(t, current, "producer", "digest"), secondDigest)
		cliSmokeEqual(t, current["revision"], revision)
		cliSmokeEqual(t, cliSmokeAt(t, current, "producer", "declaration"), cliSmokeAt(t, originalProducer, "declaration"))
		cliSmokeEqual(t, cliSmokeAt(t, current, "producerIdentity"), cliSmokeAt(t, originalFacts, "producerIdentity"))
		var queued []int
		for _, target := range []string{"go", "js"} {
			staleID, pingID := fresh.index+1, fresh.index+2
			fresh.index = pingID
			fresh.send(map[string]any{"jsonrpc": "2.0", "id": staleID, "method": "tools/call", "params": map[string]any{"name": "code.type", "arguments": map[string]any{
				"file": "main.ef", "target": target, "symbol": "main", "expectedRevision": current["revision"], "expectedProducer": key}}})
			fresh.send(map[string]any{"jsonrpc": "2.0", "id": pingID, "method": "ping"})
			queued = append(queued, staleID, pingID)
		}
		responses := map[float64]map[string]any{}
		for range 4 {
			response := fresh.receive()
			id, _ := response["id"].(float64)
			responses[id] = response
		}
		for index := 0; index < len(queued); index += 2 {
			refusal := cliSmokeAt(t, responses[float64(queued[index])], "result")
			producerSmokeStale(t, refusal)
			cliSmokeEqual(t, cliSmokeAt(t, responses[float64(queued[index+1])], "result"), map[string]any{})
		}
		producerSmokeStale(t, fresh.tool("code.typeAt", map[string]any{"file": "main.ef", "offset": ok, "expectedRevision": revision, "expectedProducer": key}))
		cliSmokeEqual(t, fresh.request("ping", nil), map[string]any{})
		cliSmokeEqual(t, cliSmokeAt(t, producerSmokeFacts(t, old, "project.check", nil), "producer", "digest"), firstDigest)
		fresh.close()
		old.close()
	})

	// The production acquisition owner is copied into a tiny real Go host,
	// rather than duplicating a repository to create distinct dirty builds.
	t.Run("dirty-hosts", func(t *testing.T) {
		t.Parallel()
		directory := t.TempDir()
		fixture := filepath.Join(directory, "host")
		if err := os.MkdirAll(filepath.Join(fixture, "internal", "producer"), 0o755); err != nil {
			t.Fatal(err)
		}
		cliSmokeWrite(t, fixture, filepath.Join("internal", "producer", "identity.go"), implementation)
		cliSmokeWrite(t, fixture, "go.mod", "module effra.local/prototype\n\ngo 1.27\n")
		const host = `package main
import("encoding/json";"os";"effra.local/prototype/internal/producer")
const marker="baseline"
func main(){json.NewEncoder(os.Stdout).Encode(map[string]any{"marker":marker,"producer":producer.Current()})}
`
		cliSmokeWrite(t, fixture, "main.go", host)
		gitIdentity := []string{"-c", "user.name=Effra fixture", "-c", "user.email=fixture@invalid"}
		producerSmokeRun(t, fixture, "git", "init", "-q")
		producerSmokeRun(t, fixture, "git", append(gitIdentity, "add", ".")...)
		producerSmokeRun(t, fixture, "git", append(gitIdentity, "commit", "-qm", "test: retain producer fixture baseline")...)
		var identities []map[string]any
		for _, marker := range []string{"dirty-A", "dirty-B"} {
			cliSmokeWrite(t, fixture, "main.go", strings.Replace(host, "baseline", marker, 1))
			binary := filepath.Join(directory, marker)
			producerSmokeRun(t, fixture, "go", "build", "-o", binary, ".")
			result := smokeJSON(t, []byte(producerSmokeRun(t, directory, binary)))
			cliSmokeEqual(t, result["marker"], marker)
			identity := cliSmokeAt(t, result, "producer").(map[string]any)
			cliSmokeEqual(t, identity["digest"], producerSmokeDigest(t, binary))
			cliSmokeEqual(t, cliSmokeAt(t, identity, "declaration", "vcsModified"), "true")
			identities = append(identities, identity)
		}
		cliSmokeEqual(t, identities[0]["declaration"], identities[1]["declaration"])
		if reflect.DeepEqual(identities[0]["digest"], identities[1]["digest"]) {
			t.Fatalf("dirty hosts share digest %v", identities[0]["digest"])
		}
	})

	// Explicit unavailable identity is stable within a process and refuses a
	// qualifier from another process. No installation-path fallback exists.
	t.Run("unavailable-identity", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		unavailable := strings.Replace(strings.Replace(implementation, "\n\t\"os\"", "", 1), `return os.Open("/proc/self/exe")`, "return nil, errUnsupported", 1)
		if unavailable == implementation {
			t.Fatal("identity owner anchors are missing")
		}
		fallback := producerSmokeOverlays(t, root, directory, ownerPath, map[string]string{"unavailable": unavailable})["unavailable"]
		processes := []*producerSmokeServer{producerSmokeStart(t, fallback, directory), producerSmokeStart(t, fallback, directory)}
		semantic := func(target string) parityOptions {
			return parityOptions{target: target, ignored: []string{"file", "timings"}, project: adapterSemantic}
		}
		before := producerSmokeFacts(t, processes[0], "project.check", map[string]any{"target": "go"})
		other := producerSmokeFacts(t, processes[1], "project.check", map[string]any{"target": "go"})
		assertReportParity(t, before, other, semantic("go"))
		idBefore, idOther := before["producer"].(map[string]any), other["producer"].(map[string]any)
		cliSmokeEqual(t, idBefore["strength"], "unavailable")
		cliSmokeEqual(t, idBefore["reuseScope"], "process")
		if _, present := idBefore["digest"]; present || idBefore["qualifier"] == idOther["qualifier"] {
			t.Fatalf("unavailable identities: %v / %v", idBefore, idOther)
		}
		same := producerSmokeFacts(t, processes[0], "project.check", map[string]any{"target": "go", "expectedProducer": idBefore["qualifier"]})
		cliSmokeEqual(t, same["producer"], before["producer"])
		cliSmokeEqual(t, same["snapshot"], before["snapshot"])
		assertReportParity(t, same, before, semantic("go"))
		for _, target := range []string{"go", "js"} {
			firstReport := producerSmokeFacts(t, processes[0], "project.check", map[string]any{"target": target})
			secondReport := producerSmokeFacts(t, processes[1], "project.check", map[string]any{"target": target})
			assertReportParity(t, firstReport, secondReport, semantic(target))
			cliReport := smokeJSON(t, []byte(producerSmokeRun(t, directory, fallback, "check", file, "--target", target)))
			assertReportParity(t, cliReport, firstReport, semantic(target))
		}
		refused := processes[1].tool("project.check", map[string]any{"file": "main.ef", "expectedProducer": idBefore["qualifier"]})
		cliSmokeEqual(t, cliSmokeAt(t, refused, "isError"), true)
		cliSmokeEqual(t, processes[1].request("ping", nil), map[string]any{})
		for _, server := range processes {
			server.close()
		}
	})

	// A causal sentinel proves compile/emit/build/run do not acquire identity.
	t.Run("lazy-acquisition", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		lazySource := strings.Replace(implementation, "func Current() Identity { return current() }", `func Current() Identity { panic("producer acquisition forbidden") }`, 1)
		if lazySource == implementation {
			t.Fatal("identity owner Current anchor is missing")
		}
		lazy := producerSmokeOverlays(t, root, directory, ownerPath, map[string]string{"lazy": lazySource})["lazy"]
		producerSmokeRun(t, directory, lazy, "build", file, "--target", "js", "-o", filepath.Join(directory, "app.mjs"))
		cliSmokeEqual(t, producerSmokeRun(t, directory, lazy, "run", file, "--target", "go"), "ok\n")
		_, stderr, code := runTestCLIDir(t, lazy, directory, "", "check", file)
		if code == 0 || !strings.Contains(string(stderr), "producer acquisition forbidden") {
			t.Fatalf("check did not acquire identity: exit %d stderr=%s", code, stderr)
		}
	})
}

func producerSmokeRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// producerSmokeRun runs a program in dir, requires success and returns stdout.
func producerSmokeRun(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	command := exec.Command(program, args...)
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%s %v: %v\nstdout=%s\nstderr=%s", program, args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func producerSmokeDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func producerSmokeInstall(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

// producerSmokeOverlays builds one ef per named replacement of source,
// concurrently, with Go build overlays over this repository's cmd/ef.
func producerSmokeOverlays(t *testing.T, root, directory, source string, replacements map[string]string) map[string]string {
	t.Helper()
	binaries := map[string]string{}
	failures := make([]error, 0, len(replacements))
	var lock sync.Mutex
	var group sync.WaitGroup
	for name, replacement := range replacements {
		modified := cliSmokeWrite(t, directory, name+".txt", replacement)
		overlay := cliSmokeWrite(t, directory, name+".json", string(mustJSON(t, map[string]any{"Replace": map[string]string{source: modified}})))
		binary := filepath.Join(directory, name)
		binaries[name] = binary
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command("go", "build", "-overlay", overlay, "-o", binary, "./cmd/ef")
			command.Dir = root
			if output, err := command.CombinedOutput(); err != nil {
				lock.Lock()
				failures = append(failures, fmt.Errorf("build %s: %v\n%s", name, err, output))
				lock.Unlock()
			}
		}()
	}
	group.Wait()
	for _, failure := range failures {
		t.Error(failure)
	}
	if len(failures) != 0 {
		t.FailNow()
	}
	return binaries
}

// producerSmokeServer is one stdio MCP server, driven a request at a time or
// with requests queued ahead of their responses.
type producerSmokeServer struct {
	t       *testing.T
	command *exec.Cmd
	stdin   io.WriteCloser
	lines   chan []byte
	stderr  bytes.Buffer
	index   int
	closed  bool
}

func producerSmokeStart(t *testing.T, binary, workspace string) *producerSmokeServer {
	t.Helper()
	server := &producerSmokeServer{t: t, lines: make(chan []byte, 16)}
	server.command = exec.Command(binary, "mcp", workspace)
	server.command.Dir = workspace
	server.command.Stderr = &server.stderr
	var err error
	if server.stdin, err = server.command.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	stdout, err := server.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(server.lines)
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				server.lines <- line
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		if !server.closed {
			_ = server.command.Process.Kill()
			for range server.lines {
			}
			_ = server.command.Wait()
		}
	})
	server.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "producer-control", "version": "1"}})
	server.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return server
}

func (s *producerSmokeServer) send(message map[string]any) {
	s.t.Helper()
	if _, err := s.stdin.Write(append(mustJSON(s.t, message), '\n')); err != nil {
		s.t.Fatalf("write MCP request: %v", err)
	}
}

func (s *producerSmokeServer) receive() map[string]any {
	s.t.Helper()
	select {
	case line, open := <-s.lines:
		if !open {
			s.t.Fatalf("server ended before producer response: stderr=%s", s.stderr.String())
		}
		return smokeJSON(s.t, line)
	case <-time.After(15 * time.Second):
		s.t.Fatal("producer control response deadline")
		return nil
	}
}

func (s *producerSmokeServer) request(method string, params map[string]any) map[string]any {
	s.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	s.index++
	s.send(map[string]any{"jsonrpc": "2.0", "id": s.index, "method": method, "params": params})
	response := s.receive()
	if _, failed := response["error"]; failed || response["id"] != float64(s.index) {
		s.t.Fatalf("%s response = %v, want id %d without error", method, response, s.index)
	}
	result, _ := response["result"].(map[string]any)
	return result
}

func (s *producerSmokeServer) tool(name string, arguments map[string]any) map[string]any {
	s.t.Helper()
	return s.request("tools/call", map[string]any{"name": name, "arguments": arguments})
}

// close ends input and requires a clean exit with nothing on stderr.
func (s *producerSmokeServer) close() {
	s.t.Helper()
	s.closed = true
	_ = s.stdin.Close()
	deadline := time.After(15 * time.Second)
	for drained := false; !drained; {
		select {
		case _, open := <-s.lines:
			drained = !open
		case <-deadline:
			_ = s.command.Process.Kill()
			s.t.Fatal("server did not exit after its input closed")
		}
	}
	if err := s.command.Wait(); err != nil || s.stderr.Len() != 0 {
		s.t.Fatalf("server exit: %v stderr=%s", err, s.stderr.String())
	}
}

// producerSmokeFacts calls a fact tool for main.ef and requires success.
func producerSmokeFacts(t *testing.T, server *producerSmokeServer, name string, arguments map[string]any) map[string]any {
	t.Helper()
	call := map[string]any{"file": "main.ef"}
	for key, value := range arguments {
		call[key] = value
	}
	result := server.tool(name, call)
	if result["isError"] == true {
		t.Fatalf("%s refused: %v", name, result)
	}
	facts, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("%s lacks structured content: %v", name, result)
	}
	return facts
}

func producerSmokeStale(t *testing.T, result any) {
	t.Helper()
	text, _ := cliSmokeAt(t, result, "content", 0, "text").(string)
	if cliSmokeAt(t, result, "isError") != true || !strings.Contains(text, "stale producer") {
		t.Fatalf("not a stale producer refusal: %s", mustJSON(t, result))
	}
}
