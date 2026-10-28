package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"ergo.local/prototype/internal/report"
)

// TestProducerSmoke: the producer identity follows the executing artifact
// rather than its installed pathname, dirty hosts are distinguished by
// digest, an unavailable identity stays process-scoped, and building never
// acquires an identity. Variant compilers are built from this package's
// source with Go build overlays, each phase in its own workspace.
func TestProducerSmoke(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspace := func(t *testing.T) (string, string) {
		directory := smokeDir(t)
		return directory, smokeWrite(t, directory, "main.ef", sketchCleanSource)
	}
	commandsPath := filepath.Join(root, "cmd", "ergo", "commands.go")
	ownerPath := filepath.Join(root, "internal", "producer", "identity.go")
	implementation := producerSmokeRead(t, ownerPath)

	t.Run("artifact-replacement", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		original := producerSmokeRead(t, commandsPath)
		const anchor = `Ergo prototype\nusage:`
		if !strings.Contains(original, anchor) {
			t.Fatal("usage banner anchor is missing from cmd/ergo/commands.go")
		}
		binaries := producerSmokeOverlays(t, root, directory, commandsPath, map[string]string{
			"compiler-A": strings.Replace(original, anchor, `Ergo producer-A\nusage:`, 1),
			"compiler-B": strings.Replace(original, anchor, `Ergo producer-B\nusage:`, 1),
		})
		first, second := binaries["compiler-A"], binaries["compiler-B"]
		firstDigest, secondDigest := producerSmokeDigest(t, first), producerSmokeDigest(t, second)
		if firstDigest == secondDigest {
			t.Fatalf("variant compilers share digest %s", firstDigest)
		}
		installed := filepath.Join(directory, "ergo")
		producerSmokeInstall(t, first, installed)
		before := smokeJSON(t, []byte(producerSmokeRun(t, directory, installed, "check", file, "--target", "go")))
		identity := producerSnapshot(t, before)
		if identity["digest"] != firstDigest || identity["strength"] != "executing-artifact" {
			t.Fatalf("check of compiler A: %v", identity)
		}
		// Every envelope of one artifact carries the same identity.
		findings := smokeJSON(t, []byte(producerSmokeRun(t, directory, installed, "diagnostics", file, "--json")))
		if !reflect.DeepEqual(producerSnapshot(t, findings), identity) || !reflect.DeepEqual(findings["snapshot"], before["snapshot"]) {
			t.Fatalf("diagnostics identity %v, check identity %v", findings["producer"], identity)
		}
		stdout, stderr, code := runSmokeCLI(t, processDeadline, installed, directory, "", "fmt", file, "--check", "--json")
		if code != 0 && code != 1 {
			t.Fatalf("fmt --check: exit %d stderr=%s", code, stderr)
		}
		if formatted := smokeJSON(t, stdout); !reflect.DeepEqual(formatted["producer"], before["producer"]) {
			t.Fatalf("fmt identity %v, check identity %v", formatted["producer"], identity)
		}
		// Replacing the installed pathname changes the executing artifact,
		// so the identity follows the new bytes; the revision does not move.
		replacement := filepath.Join(directory, "replacement")
		producerSmokeInstall(t, second, replacement)
		if err := os.Rename(replacement, installed); err != nil {
			t.Fatal(err)
		}
		after := smokeJSON(t, []byte(producerSmokeRun(t, directory, installed, "check", file)))
		replaced := producerSnapshot(t, after)
		if replaced["digest"] != secondDigest || after["revision"] != before["revision"] || !reflect.DeepEqual(replaced["declaration"], identity["declaration"]) {
			t.Fatalf("after replacement: %v (revision %v, was %v)", replaced, after["revision"], before["revision"])
		}
	})

	// The production acquisition owner is copied into a tiny real Go host,
	// rather than duplicating a repository to create distinct dirty builds.
	t.Run("dirty-hosts", func(t *testing.T) {
		t.Parallel()
		directory := t.TempDir()
		fixture := filepath.Join(directory, "host")
		smokeWrite(t, fixture, filepath.Join("internal", "producer", "identity.go"), implementation)
		smokeWrite(t, fixture, "go.mod", "module ergo.local/prototype\n\ngo 1.27\n")
		const host = `package main
import("encoding/json";"os";"ergo.local/prototype/internal/producer")
const marker="baseline"
func main(){json.NewEncoder(os.Stdout).Encode(map[string]any{"marker":marker,"producer":producer.Current()})}
`
		smokeWrite(t, fixture, "main.go", host)
		gitIdentity := []string{"-c", "user.name=Ergo fixture", "-c", "user.email=fixture@invalid"}
		producerSmokeRun(t, fixture, "git", "init", "-q")
		producerSmokeRun(t, fixture, "git", append(gitIdentity, "add", ".")...)
		producerSmokeRun(t, fixture, "git", append(gitIdentity, "commit", "-qm", "test: retain producer fixture baseline")...)
		var identities []map[string]any
		for _, marker := range []string{"dirty-A", "dirty-B"} {
			smokeWrite(t, fixture, "main.go", strings.Replace(host, "baseline", marker, 1))
			binary := filepath.Join(directory, marker)
			producerSmokeRun(t, fixture, "go", "build", "-o", binary, ".")
			result := smokeJSON(t, []byte(producerSmokeRun(t, directory, binary)))
			identity, _ := result["producer"].(map[string]any)
			declaration, _ := identity["declaration"].(map[string]any)
			if result["marker"] != marker || identity["digest"] != producerSmokeDigest(t, binary) || declaration["vcsModified"] != "true" {
				t.Fatalf("%s: %v", marker, result)
			}
			identities = append(identities, identity)
		}
		if !reflect.DeepEqual(identities[0]["declaration"], identities[1]["declaration"]) || reflect.DeepEqual(identities[0]["digest"], identities[1]["digest"]) {
			t.Fatalf("dirty hosts must share a declaration and differ by digest: %v / %v", identities[0], identities[1])
		}
	})

	// An unavailable identity is stable within a process and differs
	// between processes. No installation-path fallback exists.
	t.Run("unavailable-identity", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		unavailable := strings.Replace(strings.Replace(implementation, "\n\t\"os\"", "", 1), `return os.Open("/proc/self/exe")`, "return nil, errUnsupported", 1)
		if unavailable == implementation {
			t.Fatal("identity owner anchors are missing")
		}
		fallback := producerSmokeOverlays(t, root, directory, ownerPath, map[string]string{"unavailable": unavailable})["unavailable"]
		first := smokeJSON(t, []byte(producerSmokeRun(t, directory, fallback, "check", file)))
		other := smokeJSON(t, []byte(producerSmokeRun(t, directory, fallback, "check", file)))
		assertReportParity(t, first, other, report.CheckSchemaVersion)
		idFirst, idOther := producerSnapshot(t, first), producerSnapshot(t, other)
		if idFirst["strength"] != "unavailable" || idFirst["reuseScope"] != "process" || idFirst["qualifier"] == idOther["qualifier"] {
			t.Fatalf("unavailable identities: %v / %v", idFirst, idOther)
		}
		if _, present := idFirst["digest"]; present {
			t.Fatalf("an unavailable identity carries a digest: %v", idFirst)
		}
		findings := smokeJSON(t, []byte(producerSmokeRun(t, directory, fallback, "diagnostics", file, "--json")))
		if producerSnapshot(t, findings)["reuseScope"] != "process" {
			t.Fatalf("diagnostics identity %v", findings["producer"])
		}
		// The parity oracle refuses a malformed qualifier.
		malformed, err := copyJSON(first)
		if err != nil {
			t.Fatal(err)
		}
		malformed["producer"].(map[string]any)["qualifier"] = "process:"
		if checkReportParity(malformed, malformed, report.CheckSchemaVersion) == nil {
			t.Fatal("the parity oracle accepted an empty process qualifier")
		}
	})

	// A causal sentinel proves build and run do not acquire an identity,
	// while check and a build refusal, which print facts, do.
	t.Run("lazy-acquisition", func(t *testing.T) {
		t.Parallel()
		directory, file := workspace(t)
		lazySource := strings.Replace(implementation, "func Current() Identity { return current() }", `func Current() Identity { panic("producer acquisition forbidden") }`, 1)
		if lazySource == implementation {
			t.Fatal("identity owner Current anchor is missing")
		}
		lazy := producerSmokeOverlays(t, root, directory, ownerPath, map[string]string{"lazy": lazySource})["lazy"]
		producerSmokeRun(t, directory, lazy, "build", file, "-o", filepath.Join(directory, "app"))
		if got := producerSmokeRun(t, directory, lazy, "run", file); got != "ok\n" {
			t.Fatalf("run printed %q", got)
		}
		bad := smokeWrite(t, directory, "bad.ef", sketchMissingNeeds)
		for _, args := range [][]string{{"check", file}, {"diagnostics", file}, {"build", bad}} {
			_, stderr, code := runSmokeCLI(t, processDeadline, lazy, directory, "", args...)
			if code == 0 || !strings.Contains(string(stderr), "producer acquisition forbidden") {
				t.Fatalf("ergo %v did not acquire an identity: exit %d stderr=%s", args, code, stderr)
			}
		}
	})
}

func producerSmokeRead(t *testing.T, path string) string {
	t.Helper()
	return string(smokeRead(t, path))
}

// producerSmokeRun runs a program in dir under the process deadline,
// requires success and returns stdout.
func producerSmokeRun(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	command := exec.Command(program, args...)
	command.Dir = dir
	stdout, stderr, code := runSmokeCommand(t, processDeadline, command)
	if code != 0 {
		t.Fatalf("%s %v: exit %d\nstdout=%s\nstderr=%s", program, args, code, stdout, stderr)
	}
	return string(stdout)
}

func producerSmokeDigest(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256(smokeRead(t, path))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func producerSmokeInstall(t *testing.T, source, target string) {
	t.Helper()
	if err := os.WriteFile(target, smokeRead(t, source), 0o755); err != nil {
		t.Fatal(err)
	}
}

// producerSmokeOverlays builds one ergo per named replacement of source,
// concurrently, with Go build overlays over this repository's cmd/ergo.
func producerSmokeOverlays(t *testing.T, root, directory, source string, replacements map[string]string) map[string]string {
	t.Helper()
	binaries := map[string]string{}
	failures := make([]error, 0, len(replacements))
	var lock sync.Mutex
	var group sync.WaitGroup
	for name, replacement := range replacements {
		modified := smokeWrite(t, directory, name+".txt", replacement)
		overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{source: modified}})
		if err != nil {
			t.Fatal(err)
		}
		overlayPath := smokeWrite(t, directory, name+".json", string(overlay))
		binary := filepath.Join(directory, name)
		binaries[name] = binary
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command("go", "build", "-buildvcs=false", "-overlay", overlayPath, "-o", binary, "./cmd/ergo")
			command.Dir = root
			stdout, stderr, code, err := runSmokeDeadline(command, processDeadline)
			if err != nil || code != 0 {
				lock.Lock()
				failures = append(failures, fmt.Errorf("build %s: exit %d %v\n%s%s", name, code, err, stdout, stderr))
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
