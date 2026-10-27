package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/sketch9/sketch"
)

// writeTree writes files (path to content) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// checkout writes a checkout under a new temporary directory, with go.mod
// declaring module (none when it is "") and runtime/rt when rt is set, and
// returns its root.
func checkout(t *testing.T, module string, rt bool) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{}
	if module != "" {
		files["go.mod"] = "module " + module + "\n\ngo 1.27\n"
	}
	if rt {
		files["runtime/rt/rt.go"] = "package rt\n"
	}
	writeTree(t, root, files)
	return root
}

// env is a lookupEnv over a fixed set of variables.
func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

// The kernel root is given, never discovered: -rtroot when the command line
// has it, else ERGO_REPO_ROOT; with neither, or with an empty or failing
// root, the build is refused with a message that names the problem.
func TestKernelRootExplicit(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	good := checkout(t, sketch.KernelModule, true)
	other := checkout(t, "other.local/prototype", true)
	noRT := checkout(t, sketch.KernelModule, false)
	for _, tc := range []struct {
		name string
		flag rootFlag
		vars map[string]string
		want string // the root, or the substrings of the failure, joined by "|"
	}{
		{"flag good", rootFlag{good, true}, nil, good},
		{"env good", rootFlag{}, map[string]string{"ERGO_REPO_ROOT": good}, good},
		{"flag beats env", rootFlag{good, true}, map[string]string{"ERGO_REPO_ROOT": other}, good},
		{"flag bad beats env good", rootFlag{other, true}, map[string]string{"ERGO_REPO_ROOT": good}, "-rtroot|declares module other.local/prototype|" + sketch.KernelModule},
		{"neither", rootFlag{}, nil, "needs the repository root|-rtroot DIR|ERGO_REPO_ROOT"},
		{"bad module", rootFlag{other, true}, nil, "-rtroot " + other + "|declares module other.local/prototype|" + sketch.KernelModule},
		{"bad module from env", rootFlag{}, map[string]string{"ERGO_REPO_ROOT": other}, "ERGO_REPO_ROOT " + other + "|declares module other.local/prototype"},
		{"missing runtime/rt", rootFlag{noRT, true}, nil, "no runtime/rt"},
		{"missing dir", rootFlag{}, map[string]string{"ERGO_REPO_ROOT": filepath.Join(good, "absent")}, "absent|no such file or directory"},
		{"empty env", rootFlag{}, map[string]string{"ERGO_REPO_ROOT": ""}, "ERGO_REPO_ROOT is empty"},
		{"empty flag", rootFlag{"", true}, map[string]string{"ERGO_REPO_ROOT": good}, "-rtroot is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := kernelRoot(tc.flag, env(tc.vars))
			if !strings.Contains(tc.want, "|") && filepath.IsAbs(tc.want) {
				if err != nil || got != tc.want {
					t.Fatalf("kernelRoot = %q, %v; want %s", got, err, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("kernelRoot = %q; want a failure naming %s", got, tc.want)
			}
			for _, s := range strings.Split(tc.want, "|") {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("failure %q lacks %q", err, s)
				}
			}
		})
	}
}

// At the command line: with neither -rtroot nor ERGO_REPO_ROOT, build -rt
// kernel fails with one line naming both, and writes nothing; with -rtroot it
// emits a module that links the given root.
func TestBuildKernelNeedsRoot(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	t.Setenv("ERGO_REPO_ROOT", "")
	os.Unsetenv("ERGO_REPO_ROOT")
	out := filepath.Join(t.TempDir(), "gen")
	err := run([]string{"build", "-rt", "kernel", "../../testdata/trace/timeout.sketch", "-out", out}, os.Stdout)
	if err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "-rtroot DIR") || !strings.Contains(err.Error(), "ERGO_REPO_ROOT") {
		t.Fatalf("build without a root: %v; want one line naming -rtroot DIR and ERGO_REPO_ROOT", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("build without a root wrote %s", out)
	}
	root := checkout(t, sketch.KernelModule, true)
	if err := run([]string{"build", "-rt", "kernel", "-rtroot", root, "../../testdata/trace/timeout.sketch", "-out", out}, os.Stdout); err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(out, "go.mod"))
	if err != nil || !strings.Contains(string(gomod), "replace "+sketch.KernelModule+" => \""+root+"\"") {
		t.Fatalf("emitted go.mod %q (%v); want it to replace %s with %s", gomod, err, sketch.KernelModule, root)
	}
}
