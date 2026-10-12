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
	// The stub Bun is a script inside a package that names itself when asked
	// for its execPath; its fingerprint is that package, its interpreter and
	// itself, so a gate over it can replay a pass.
	write(t, bin, "package.json", `{"name": "stub-bun"}`)
	write(t, bin, "bun", "#!/bin/sh\nif [ \"$1\" = -e ]; then printf %s \"$0\"; exit 0; fi\necho 1.4.3\n")
	if err := os.Chmod(filepath.Join(bin, "bun"), 0755); err != nil {
		t.Fatal(err)
	}
	return leaf, bin
}

// runRunner runs node with argv in the leaf, with a marker every descendant inherits, and
// returns its exit code, output and the descendants still alive after it.
func runRunner(t *testing.T, leaf, bin string, argv ...string) (int, string, []string) {
	t.Helper()
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		t.Fatal(err)
	}
	marker := "EFFRA_RUNNER_PROBE=" + hex.EncodeToString(token)
	command := exec.Command("node", argv...)
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
	code, out, survivors := runRunner(t, leaf, bin, "scripts/check-references.mjs")
	if len(survivors) > 0 {
		t.Errorf("descendants outlived the runner: %q", survivors)
	}
	if code != 3 || !strings.Contains(out, "INJECTED_VENDOR_FAILURE") || !strings.Contains(out, "VENDOR_BUILD_EXIT=3") || strings.Contains(out, "LATE_CHILD_FAILURE") {
		t.Errorf("exit %d, want the vendor failure's 3 and its output only:\n%s", code, out)
	}
}

// strayCheck stands in for a reader that resolves the domain through
// .generated, as the consumers' TypeScript and the counter control's Node do:
// it fails if any name there is neither an emitted input nor this run's own.
const strayCheck = `const fs = process.getBuiltinModule("node:fs");
const owned = ["counter-domain-artifacts", "counter-domain-emit-evidence.json"];
const strays = fs.existsSync(".generated") ? fs.readdirSync(".generated").filter(name => !owned.includes(name)) : [];
if (strays.length > 0) { console.error("STRAY_VISIBLE " + strays.join(",")); process.exit(5); }
`

// A file placed by hand in .generated, such as a counter.mts that TypeScript
// would read before the fresh counter.d.mts, is outside the references key.
// A replayed pass is sound only if executing the step reaches the same pass:
// the leaf runner empties .generated, except the keyed artifacts and the
// emission's record, before any reader runs. The real step declaration runs
// the real runner over stub collaborators, warm.
func TestReferencesReplayHoldsWhateverIsLeftInGenerated(t *testing.T) {
	repository, steps := repositorySteps(t)
	var declared *Step
	for _, step := range steps {
		if step.Name == "framework-port references" {
			declared = step
		}
	}
	if declared == nil {
		t.Fatal("no framework-port references step")
	}
	ignore, err := os.ReadFile(filepath.Join(repository, frameworkPorts, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	succeed := "process.exit(0);\n"
	leaf, bin := runnerLeaf(t, "check-references.mjs", map[string]string{
		".gitignore":                                        string(ignore),
		"node_modules/typescript/bin/tsc":                   strayCheck,
		"scripts/check-yield-capture.mjs":                   succeed,
		"scripts/check-dependency-profile.mjs":              succeed,
		"scripts/check-counter-domain.mjs":                  strayCheck,
		"vendor/solid-yield/scripts/build.mjs":              succeed,
		".generated/counter-domain-artifacts/manifest.json": "{}\n",
		".generated/counter-domain-emit-evidence.json":      "{}\n",
	})
	root := filepath.Join(leaf, "../../..")
	gitFixture(t, root)
	// The stub Bun reports the three mounted cases and writes their JUnit record.
	junit := `<testsuites name="bun test" tests="3" assertions="3" failures="0" skipped="0">`
	for _, host := range []string{"react-atom", "solid-effect", "solid-yield"} {
		junit += `<testcase name="` + host + ` reference mounts, applies source events below zero, resets from nonzero and releases its subscriptions" assertions="1" />`
	}
	write(t, bin, "bun", `#!/bin/sh
[ "$1" = --version ] && { echo 1.4.3; exit 0; }
[ "$1" = -e ] && { printf %s "$0"; exit 0; }
for argument in "$@"; do case "$argument" in --reporter-outfile=*) out="${argument#--reporter-outfile=}" ;; esac; done
printf '%s\n' '`+junit+`' > "$out"
printf ' 3 pass\n 0 fail\n'
`)
	if err := os.Chmod(filepath.Join(bin, "bun"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := t.TempDir()
	run := func(when string, noCache, cached bool) {
		t.Helper()
		var out bytes.Buffer
		g := &gate{root: root, cacheDir: cache, noCache: noCache, steps: []*Step{{Name: declared.Name, Argv: declared.Argv, Inputs: declared.Inputs}}}
		code := g.run(&out)
		if code != 0 || strings.Contains(out.String(), "ok, cached") != cached || !strings.Contains(out.String(), "FRAMEWORK_REFERENCES_EXIT=0") {
			t.Fatalf("%s: exit %d, want a pass with cached=%t:\n%s", when, code, cached, out.String())
		}
	}
	run("first run", false, false)
	run("unchanged", false, true)

	generated := filepath.Join(leaf, ".generated")
	strays := []string{"counter.mts", "node_modules/effect/index.js", "package.json", "counter-domain-a1b2c3/consumer.ts", "counter.mjs/index.js"}
	for _, name := range strays {
		write(t, generated, name, "export const count: string = \"\";\n")
	}
	run("strays left in .generated", false, true)
	run("strays left in .generated, executed", true, false)
	for _, name := range strays {
		if _, err := os.Stat(filepath.Join(generated, strings.Split(name, "/")[0])); err == nil {
			t.Errorf("the runner left %s in .generated", name)
		}
	}
	for _, name := range []string{"counter-domain-artifacts/manifest.json", "counter-domain-emit-evidence.json"} {
		if _, err := os.Stat(filepath.Join(generated, name)); err != nil {
			t.Errorf("the runner removed the keyed or unread %s: %v", name, err)
		}
	}
}

// The gate runs counter emission on every run, no-ops included, so a passing
// emission must not leave its scratch workspace behind.
func TestCounterEmissionRemovesItsWorkspaceOnSuccess(t *testing.T) {
	leaf, bin := runnerLeaf(t, "check-counter-domain.mjs", map[string]string{
		"fixtures/counter.ef":             "module counter\nCounterEvent.ClickedIncrement => CounterState {\n            count: state.count + 1\n        }\n",
		"fixtures/counter-native.ef":      "harness\n",
		"tests/types/counter-positive.ts": "export {};\n",
	})
	gitFixture(t, filepath.Join(leaf, "../../.."))
	// The stub compiler checks everything and writes each requested output;
	// a native build publishes its snapshots under the build directory.
	write(t, bin, "ef", `#!/bin/sh
[ "$1" = check ] && exit 0
while [ $# -gt 0 ]; do [ "$1" = -o ] && out="$2"; shift; done
: > "$out"
case "$out" in *.mjs) : > "${out%.mjs}.d.mts" ;; *) mkdir -p dist/go/apps ;; esac
`)
	if err := os.Chmod(filepath.Join(bin, "ef"), 0755); err != nil {
		t.Fatal(err)
	}
	code, out, survivors := runRunner(t, leaf, bin, "scripts/check-counter-domain.mjs", "--compiler", filepath.Join(bin, "ef"), "--emit", filepath.Join(t.TempDir(), "artifacts"))
	if code != 0 || len(survivors) > 0 || !strings.Contains(out, "COUNTER_DOMAIN_EMIT_EXIT=0") {
		t.Fatalf("exit %d, survivors %q; want a passing emission:\n%s", code, survivors, out)
	}
	left, err := filepath.Glob(filepath.Join(leaf, ".generated", "counter-domain-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range left {
		if info, err := os.Stat(name); err == nil && info.IsDir() {
			t.Errorf("a passing emission left its workspace %s", name)
		}
	}
}

func gitFixture(t *testing.T, repository string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=gate", "-c", "user.email=gate@invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture"}} {
		command := exec.Command("git", args...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
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
	gitFixture(t, filepath.Join(leaf, "../../.."))
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
	// The second case also fails the write of the failure evidence, which must
	// neither skip the join nor replace the original failure.
	preload := filepath.Join(t.TempDir(), "evidence-enospc.cjs")
	write(t, filepath.Dir(preload), filepath.Base(preload), `const fs = require("node:fs");
const write = fs.writeFileSync;
fs.writeFileSync = (file, data, ...rest) => {
  if (String(file).endsWith("counter-domain-evidence.json") && String(data).includes('"error"')) throw Object.assign(new Error("INJECTED_EVIDENCE_ENOSPC"), { code: "ENOSPC" });
  return write(file, data, ...rest);
};
require("node:module").syncBuiltinESMExports();
`)
	for _, run := range []struct {
		name     string
		node     []string
		reported string
	}{
		{"native failure", nil, ""},
		{"native failure, evidence unwritable", []string{"--require=" + preload}, "COUNTER_EVIDENCE_NOT_RECORDED=INJECTED_EVIDENCE_ENOSPC"},
	} {
		code, out, survivors := runRunner(t, leaf, bin, append(run.node, "scripts/check-counter-domain.mjs", "--verify", artifacts)...)
		if len(survivors) > 0 {
			t.Errorf("%s: descendants outlived the control: %q", run.name, survivors)
		}
		if code != 1 || !strings.Contains(out, "INJECTED_NATIVE_FAILURE") || !strings.Contains(out, "GO_PRODUCTION_RUN must succeed") || strings.Contains(out, "LATE_CHILD_FAILURE") || !strings.Contains(out, run.reported) {
			t.Errorf("%s: exit %d, want the native failure's assertion and its output only:\n%s", run.name, code, out)
		}
		if run.reported == "" {
			// A failing control keeps its workspace: the evidence names paths inside it.
			var evidence struct {
				Error string `json:"error"`
				Work  string `json:"work"`
			}
			content, err := os.ReadFile(filepath.Join(leaf, ".generated", "counter-domain-evidence.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(content, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.Error == "" {
				t.Errorf("%s: the evidence records no error", run.name)
			}
			if info, err := os.Stat(evidence.Work); err != nil || !info.IsDir() {
				t.Errorf("%s: the failing control removed the workspace %q its evidence names", run.name, evidence.Work)
			}
		}
	}
}
