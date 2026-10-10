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
	g := &gate{root: root, cacheDir: t.TempDir(), steps: steps}
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
				key, err := g.key(step)
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

// ESBUILD_BINARY_PATH would replace the installed esbuild, a keyed input,
// with an arbitrary executable; no step ever sees it.
func TestStepsNeverSeeAnUnpinnedToolOverride(t *testing.T) {
	root := fixture(t)
	t.Setenv("ESBUILD_BINARY_PATH", "/nonexistent/loop-probe-x")
	code, out := runGate(t, root, t.TempDir(), []*Step{{Name: "probe", Argv: []string{"sh", "-c", `echo "esbuild=${ESBUILD_BINARY_PATH-unset}"`}}})
	if code != 0 || !strings.Contains(out, "esbuild=unset") {
		t.Fatalf("exit %d; the step saw the override:\n%s", code, out)
	}
	if _, set := stepVariable("ESBUILD_BINARY_PATH"); set {
		t.Fatal("a key would name an override no step runs with")
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
