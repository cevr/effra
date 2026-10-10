package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The framework-port leaf runners start a child alongside synchronous work.
// These controls copy each runner into a scratch leaf whose collaborators are
// stubs, inject a failure while the started child still runs, and require
// that the runner exits with the original failure and leaves no descendant
// behind. The runners are read here, so Go's test cache keys on their bytes.

const leafScripts = "../../" + frameworkPorts + "scripts/"

// stub is a Node program that sleeps, then fails with a message the runner
// must not report as its own outcome.
const lateFailure = `setTimeout(() => { console.error("LATE_CHILD_FAILURE"); process.exit(9); }, 1500);
`

func runnerLeaf(t *testing.T, runner string, files map[string]string) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("descendant scan reads /proc")
	}
	// Go tests reading the tool digest key their cached result on node's version.
	_ = os.Getenv("EFFRA_TOOL_VERSIONS")
	source, err := os.ReadFile(leafScripts + runner)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	leaf := filepath.Join(repository, filepath.FromSlash(frameworkPorts))
	write(t, leaf, "scripts/"+runner, string(source))
	write(t, leaf, "node_modules/typescript/package.json", `{"name":"typescript","version":"6.0.3"}`)
	for name, content := range files {
		write(t, leaf, name, content)
	}
	bin := t.TempDir()
	write(t, bin, "bun", "#!/bin/sh\necho 1.4.2\n")
	if err := os.Chmod(filepath.Join(bin, "bun"), 0755); err != nil {
		t.Fatal(err)
	}
	return leaf, bin
}

// runRunner runs a leaf runner with a marker every descendant inherits, and
// returns its exit code, output and the descendants still alive after it.
func runRunner(t *testing.T, leaf, bin, runner string, args ...string) (int, string, []string) {
	t.Helper()
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		t.Fatal(err)
	}
	marker := "EFFRA_RUNNER_PROBE=" + hex.EncodeToString(token)
	command := exec.Command("node", append([]string{filepath.Join(leaf, "scripts", runner)}, args...)...)
	command.Dir = leaf
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), marker)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	var survivors []string
	processes, _ := filepath.Glob("/proc/[0-9]*/environ")
	for _, name := range processes {
		// A zombie's environment reads empty, so only live processes match.
		environ, err := os.ReadFile(name)
		if err == nil && bytes.Contains(append(append([]byte{0}, environ...), 0), []byte("\x00"+marker+"\x00")) {
			cmdline, _ := os.ReadFile(strings.TrimSuffix(name, "environ") + "cmdline")
			survivors = append(survivors, strings.ReplaceAll(string(cmdline), "\x00", " "))
		}
	}
	return code, output.String(), survivors
}

func TestReferencesRunnerJoinsTheCounterControlAfterAVendorFailure(t *testing.T) {
	succeed := "process.exit(0);\n"
	leaf, bin := runnerLeaf(t, "check-references.mjs", map[string]string{
		"node_modules/typescript/bin/tsc":      succeed,
		"scripts/check-yield-capture.mjs":      succeed,
		"scripts/check-dependency-profile.mjs": succeed,
		"scripts/check-counter-domain.mjs":     lateFailure,
		"vendor/solid-yield/scripts/build.mjs": `console.error("INJECTED_VENDOR_FAILURE"); process.exit(3);
`,
	})
	code, out, survivors := runRunner(t, leaf, bin, "check-references.mjs")
	if len(survivors) > 0 {
		t.Errorf("descendants outlived the runner: %q", survivors)
	}
	if code != 3 || !strings.Contains(out, "INJECTED_VENDOR_FAILURE") || !strings.Contains(out, "VENDOR_BUILD_EXIT=3") || strings.Contains(out, "LATE_CHILD_FAILURE") {
		t.Errorf("exit %d, want the vendor failure's 3 and its output only:\n%s", code, out)
	}
}

func TestCounterControlJoinsTheTypeCheckAfterANativeFailure(t *testing.T) {
	const mutation = "CounterEvent.ClickedIncrement => CounterState {\n            count: state.count + 1\n        }"
	domain := "module counter\n" + mutation + "\n"
	harness := "harness\n"
	mutant := strings.Replace(domain, mutation, strings.Replace(mutation, "+ 1", "- 1", 1), 1)
	consumer := "export {};\n"
	leaf, bin := runnerLeaf(t, "check-counter-domain.mjs", map[string]string{
		"node_modules/typescript/bin/tsc": `if (process.argv.includes("--version")) { console.log("Version 6.0.3"); } else { ` + lateFailure + ` }
`,
		"node_modules/effect/package.json":       `{"name":"effect","version":"4.0.1","main":"index.js"}`,
		"node_modules/effect/index.js":           "",
		"fixtures/counter.ef":                    domain,
		"fixtures/counter-native.ef":             harness,
		"tests/types/counter-positive.ts":        consumer,
		"tests/types/counter-number-negative.ts": consumer,
		"tests/types/counter-state-negative.ts":  consumer,
		"tests/types/counter-event-negative.ts":  consumer,
	})
	repository := filepath.Join(leaf, "../../..")
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=gate", "-c", "user.email=gate@invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture"}} {
		command := exec.Command("git", args...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	sum := func(text string) string {
		digest := sha256.Sum256([]byte(text))
		return hex.EncodeToString(digest[:])
	}
	manifest, err := json.Marshal(map[string]any{
		"schema": "effra-counter-domain-artifacts-v1", "domainSha256": sum(domain), "harnessSha256": sum(harness), "mutantSha256": sum(mutant),
		"nativePrefixBytes": map[string]int{"production": len(domain), "mutant": len(mutant)},
	})
	if err != nil {
		t.Fatal(err)
	}
	declarations := "export interface CounterState { readonly count: bigint }\nexport type CounterEvent = never;\ndeclare const __ef_brand_CounterState: unique symbol;\ndeclare const __ef_brand_CounterEvent: unique symbol;\n"
	for _, name := range []string{"initialCounter", "counterState", "clickedDecrement", "clickedIncrement", "clickedReset", "updateCounter", "counterCount"} {
		declarations += "export { value as " + name + " };\n"
	}
	artifacts := t.TempDir()
	for name, content := range map[string]string{
		"manifest.json": string(manifest), "production-native.ef": domain + "\n" + harness, "mutant-native.ef": mutant + "\n" + harness,
		"bun-mutant.ef": mutant, "node-mutant.ef": mutant,
		"counter.mjs": "", "counter.d.mts": declarations, "bun-mutant.mjs": "", "bun-mutant.d.mts": declarations, "node-mutant.mjs": "", "node-mutant.d.mts": declarations,
		"counter-go-mutant": "",
		"counter-go":        "#!/bin/sh\necho COUNTER_INITIAL_ZERO=0\necho INJECTED_NATIVE_FAILURE >&2\nexit 7\n",
	} {
		write(t, artifacts, name, content)
	}
	if err := os.Chmod(filepath.Join(artifacts, "counter-go"), 0755); err != nil {
		t.Fatal(err)
	}
	code, out, survivors := runRunner(t, leaf, bin, "check-counter-domain.mjs", "--verify", artifacts)
	if len(survivors) > 0 {
		t.Errorf("descendants outlived the control: %q", survivors)
	}
	if code != 1 || !strings.Contains(out, "INJECTED_NATIVE_FAILURE") || !strings.Contains(out, "GO_PRODUCTION_RUN must succeed") || strings.Contains(out, "LATE_CHILD_FAILURE") {
		t.Errorf("exit %d, want the native failure's assertion and its output only:\n%s", code, out)
	}
}
