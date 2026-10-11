package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GODEBUG=inittrace=1 leaves a Go program's bytes unchanged but adds lines to
// its standard error, which the framework-port verification asserts exactly.
// Every gate step that runs Go programs therefore keys on the Go runtime
// environment: cached steps through their key, Go test steps through the tool
// digest their tests read.
func TestGoRuntimeEnvironmentKeysEveryStepThatRunsGoPrograms(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := gateSteps(root)
	if err != nil {
		t.Fatal(err)
	}
	// The steps' declared tools and environment are evaluated over a fixture:
	// the repository's own files, which the emission step rewrites every run,
	// would keep Go's test cache from ever replaying this test.
	g := &gate{root: fixture(t), cacheDir: t.TempDir()}
	if err := g.loadFiles(); err != nil {
		t.Fatal(err)
	}
	type fingerprint struct{ keys, digests map[string]string }
	observe := func() fingerprint {
		t.Helper()
		g.tools = nil
		observed := fingerprint{map[string]string{}, map[string]string{}}
		for _, step := range steps {
			if step.Inputs != nil && (step.Argv[0] == "go" || step.Name == "framework-port references") {
				declared := *step.Inputs
				declared.Paths, declared.Exclude, declared.Submodule, declared.Installed, declared.Generated = []string{"src/"}, nil, "", nil, nil
				key, err := g.key(&Step{Name: step.Name, Argv: step.Argv, Inputs: &declared})
				if err != nil {
					t.Fatal(err)
				}
				observed.keys[step.Name] = key
			}
			if len(step.ToolEnv) > 0 {
				observed.digests[step.Name] = g.toolDigest(step)
			}
		}
		return observed
	}
	for _, name := range goRuntimeEnvironment {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	unset := observe()
	if _, ok := unset.keys["framework-port references"]; !ok {
		t.Fatal("no framework-port references step")
	}
	if len(unset.keys) < 3 || len(unset.digests) < 2 {
		t.Fatalf("too few Go-running steps observed: %d keyed, %d with a tool digest", len(unset.keys), len(unset.digests))
	}
	for _, change := range []struct{ name, value string }{{"GODEBUG", "inittrace=1"}, {"GOTRACEBACK", "none"}, {"GOMAXPROCS", "1"}} {
		t.Setenv(change.name, change.value)
		changed := observe()
		for step, key := range unset.keys {
			if changed.keys[step] == key {
				t.Errorf("%s=%s left the key of %q unchanged", change.name, change.value, step)
			}
		}
		for step, digest := range unset.digests {
			if changed.digests[step] == digest {
				t.Errorf("%s=%s left the tool digest of %q unchanged", change.name, change.value, step)
			}
		}
		os.Unsetenv(change.name)
	}
}

// A tool's fingerprint names the executable it resolves to and its bytes, not
// only the version it prints: a different build that prints the same version
// must not replay a pass. The recorded digest must follow every write, even
// one that restores the size and modification time.
func TestToolFingerprintCoversTheResolvedExecutable(t *testing.T) {
	bin := t.TempDir()
	other := t.TempDir()
	cache := t.TempDir()
	tool := filepath.Join(bin, "probe-tool")
	script := func(dir, comment string) {
		t.Helper()
		write(t, dir, "probe-tool", "#!/bin/sh\n# "+comment+"\necho probe 1.0.0\n")
		if err := os.Chmod(filepath.Join(dir, "probe-tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script(bin, "build a")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	fingerprint := func() string {
		t.Helper()
		g := &gate{root: t.TempDir(), cacheDir: cache}
		version := g.toolVersion("probe-tool")
		if !strings.HasPrefix(version, "probe 1.0.0") {
			t.Fatalf("fingerprint %q lacks the printed version", version)
		}
		return version
	}
	first := fingerprint()
	if fingerprint() != first {
		t.Fatal("an unchanged executable changed its fingerprint")
	}
	info, err := os.Stat(tool)
	if err != nil {
		t.Fatal(err)
	}
	script(bin, "build b")
	if err := os.Chtimes(tool, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	rebuilt := fingerprint()
	if rebuilt == first {
		t.Fatal("another build printing the same version, with the same size and modification time, kept the fingerprint")
	}
	// The same bytes under another prefix are another installation.
	script(other, "build b")
	if err := os.Rename(tool, tool+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "probe-tool"), tool); err != nil {
		t.Fatal(err)
	}
	if fingerprint() == rebuilt {
		t.Fatal("the same build under another prefix kept the fingerprint")
	}
}

// ESBUILD_BINARY_PATH would replace the installed esbuild, a keyed input,
// with an arbitrary executable, and NODE_OPTIONS, NODE_PATH and BUN_OPTIONS
// would load modules no key sees; no step ever sees them.
func TestStepsNeverSeeAnUnpinnedToolOverride(t *testing.T) {
	root := fixture(t)
	for _, name := range []string{"ESBUILD_BINARY_PATH", "NODE_OPTIONS", "NODE_PATH", "BUN_OPTIONS"} {
		t.Setenv(name, "/nonexistent/loop-probe-x")
		code, out := runGate(t, root, t.TempDir(), []*Step{{Name: "probe", Argv: []string{"sh", "-c", `echo "override=${` + name + `-unset}"`}}})
		if code != 0 || !strings.Contains(out, "override=unset") {
			t.Fatalf("exit %d; the step saw %s:\n%s", code, name, out)
		}
		if _, set := stepVariable(name); set {
			t.Fatalf("a key would name a %s no step runs with", name)
		}
	}
}

// A preload named by an unchanged NODE_OPTIONS can change its bytes between
// runs. A cached Node step must never replay a pass that a changed preload
// would fail: the preload never runs, so the pass holds for any bytes.
func TestAnExternalNodePreloadCannotReachACachedStep(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("node is required: the gate runs Node steps")
	}
	root := fixture(t)
	preload := filepath.Join(t.TempDir(), "preload.cjs")
	t.Setenv("NODE_OPTIONS", "--require="+preload)
	step := func() []*Step {
		return []*Step{{Name: "node", Argv: []string{"node", "-e", `console.log("node ran")`}, Inputs: &Inputs{Paths: []string{"src/"}, Tools: []string{"node"}}}}
	}
	cache := t.TempDir()
	write(t, filepath.Dir(preload), "preload.cjs", `console.log("PRELOAD_RAN");`)
	if code, out := runGate(t, root, cache, step()); code != 0 || strings.Contains(out, "PRELOAD_RAN") {
		t.Fatalf("exit %d; the external preload ran:\n%s", code, out)
	}
	write(t, filepath.Dir(preload), "preload.cjs", `throw new Error("INJECTED_PRELOAD_FAILURE");`)
	if code, out := runGate(t, root, t.TempDir(), step()); code != 0 {
		t.Fatalf("exit %d; a changed external preload changed an executed result:\n%s", code, out)
	}
}

// A submodule configured with ignore=all still makes the checkout unclean
// when its contents change.
func TestCheckoutIdentitySeesIgnoredSubmoduleChanges(t *testing.T) {
	root := fixture(t)
	module := fixture(t)
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-c", "user.name=gate", "-c", "user.email=gate@invalid", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("submodule", "add", "-q", module, "vendor/module")
	git("commit", "-qm", "submodule")
	git("config", "submodule.vendor/module.ignore", "all")
	clean := func() bool {
		t.Helper()
		identity, err := checkoutIdentity(root)
		if err != nil {
			t.Fatal(err)
		}
		return strings.HasSuffix(identity, "clean=true")
	}
	if !clean() {
		t.Fatal("a committed submodule reported a dirty checkout")
	}
	write(t, root, "vendor/module/src/input.txt", "modified\n")
	if clean() {
		t.Fatal("a modified submodule with ignore=all reported clean=true")
	}
}
