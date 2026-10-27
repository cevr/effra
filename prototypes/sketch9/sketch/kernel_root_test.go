package sketch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kernelRoot is the admission check of every kernel test, before any other
// skip: it returns the repository root from KernelRootEnv. Unset, the test
// skips and names the variable (sketch9 copied elsewhere, or run by hand).
// Set, every prerequisite is required: an empty value, a missing go tool and
// any problem with the root (KernelRoot) fail the test, named.
func kernelRoot(t *testing.T) string {
	t.Helper()
	env, set := os.LookupEnv(KernelRootEnv)
	if !set {
		t.Skip(KernelRootEnv + " is unset; set it to the repository root to run the kernel tests (the gate does)")
	}
	if env == "" {
		t.Fatal(KernelRootEnv + " is set but empty; set it to the repository root, or unset it to skip the kernel tests")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("%s is set, so the kernel tests require the go tool: %v", KernelRootEnv, err)
	}
	root, err := KernelRoot(env)
	if err != nil {
		t.Fatalf("%s=%s: %v", KernelRootEnv, env, err)
	}
	return root
}

// needGo admits a test that runs the go tool but needs no root: a missing go
// tool skips it, unless KernelRootEnv is set, which requires the tool.
func needGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		if _, set := os.LookupEnv(KernelRootEnv); set {
			t.Fatalf("%s is set, so the kernel tests require the go tool: %v", KernelRootEnv, err)
		}
		t.Skip("no go tool")
	}
}

// kernelFixture writes a checkout under a temporary directory: go.mod with
// the given content (none when it is ""), and runtime/rt when rt is set. It
// returns the checkout's root.
func kernelFixture(t *testing.T, gomod string, rt bool) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{}
	if gomod != "" {
		files["go.mod"] = gomod
	}
	if rt {
		files["runtime/rt/rt.go"] = "package rt\n"
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The kernel tests' admission at their real entry point: this test binary,
// run again with a controlled environment, selecting the three runtime tests.
// The variable is named literally, not through KernelRootEnv, so the gate's
// contract (it sets ERGO_REPO_ROOT) is checked independently of the constant.
func TestKernelAdmission(t *testing.T) {
	if KernelRootEnv != "ERGO_REPO_ROOT" {
		t.Fatalf("KernelRootEnv is %q; the gate and the README set ERGO_REPO_ROOT", KernelRootEnv)
	}
	// The children that find the go tool reach the root check (needGo).
	needGo(t)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var base []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ERGO_REPO_ROOT=") {
			base = append(base, kv)
		}
	}
	mismatch := kernelFixture(t, "module other.local/prototype\n", true)
	for _, tc := range []struct {
		name string
		env  []string // added to base; a later entry wins
		fail bool     // every runtime test fails; otherwise every one skips
		want []string // in the output
	}{
		{"unset", nil, false, []string{"ERGO_REPO_ROOT is unset"}},
		{"unset, no go tool", []string{"PATH=/nonexistent/loop-probe-no-go"}, false, []string{"ERGO_REPO_ROOT is unset"}},
		{"set but empty", []string{"ERGO_REPO_ROOT="}, true, []string{"ERGO_REPO_ROOT is set but empty"}},
		{"set, no go tool", []string{"ERGO_REPO_ROOT=" + mismatch, "PATH=/nonexistent/loop-probe-no-go"}, true, []string{"require the go tool"}},
		{"set, missing dir", []string{"ERGO_REPO_ROOT=" + filepath.Join(mismatch, "absent")}, true, []string{"absent", "no such file or directory"}},
		{"set, another module", []string{"ERGO_REPO_ROOT=" + mismatch}, true, []string{"declares module other.local/prototype", KernelModule}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "-test.v", "-test.count=1", "-test.run", "^TestKernel(EqualFailureProbes|TimeoutTrace|ProbeTraces)$")
			cmd.Env = append(append([]string{}, base...), tc.env...)
			out, err := cmd.CombinedOutput()
			text := string(out)
			verdict, code := "SKIP", 0
			if tc.fail {
				verdict, code = "FAIL", 1
			}
			if got := exitCode(err); got != code {
				t.Errorf("exit %d (%v); want %d", got, err, code)
			}
			for _, name := range []string{"TestKernelEqualFailureProbes", "TestKernelTimeoutTrace", "TestKernelProbeTraces"} {
				if !strings.Contains(text, "--- "+verdict+": "+name+" ") {
					t.Errorf("%s did not %s", name, verdict)
				}
			}
			for _, s := range tc.want {
				if !strings.Contains(text, s) {
					t.Errorf("output lacks %q", s)
				}
			}
			if t.Failed() {
				t.Logf("output:\n%s", text)
			}
		})
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}

// KernelRoot checks a given root: a good checkout is its absolute path, and
// anything else fails and names the problem. Module identity is Go's own
// reading of go.mod, so a factored declaration is a module, and a block
// comment or an unknown directive is the go tool's error, never a module.
func TestKernelRootContract(t *testing.T) {
	needGo(t)
	good := "module " + KernelModule + "\n\ngo 1.27\n"
	for _, tc := range []struct {
		name string
		dir  func(t *testing.T) string
		want string // "root", or the substrings of the failure, joined by "|"
	}{
		{"good", func(t *testing.T) string { return kernelFixture(t, good, true) }, "root"},
		{"factored kernel module", func(t *testing.T) string {
			return kernelFixture(t, "module (\n\t"+KernelModule+"\n)\n\ngo 1.27\n", true)
		}, "root"},
		{"CRLF", func(t *testing.T) string {
			return kernelFixture(t, strings.ReplaceAll(good, "\n", "\r\n"), true)
		}, "root"},
		{"long comment line", func(t *testing.T) string {
			return kernelFixture(t, "// "+strings.Repeat("x", 70000)+"\n"+good, true)
		}, "root"},
		{"relative path", func(t *testing.T) string {
			root := kernelFixture(t, good, true)
			t.Chdir(filepath.Dir(root))
			return filepath.Base(root)
		}, "root"},
		{"mismatch", func(t *testing.T) string { return kernelFixture(t, "module other.local/prototype\n", true) }, "declares module other.local/prototype|" + KernelModule},
		{"sketch9's module", func(t *testing.T) string { return kernelFixture(t, "module effra.local/sketch9\n", true) }, "declares module effra.local/sketch9|" + KernelModule},
		{"factored mismatch", func(t *testing.T) string { return kernelFixture(t, "module (\n\tother.local/prototype\n)\n", true) }, "declares module other.local/prototype|" + KernelModule},
		{"missing dir", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") }, "absent|no such file or directory"},
		{"not a dir", func(t *testing.T) string { return filepath.Join(kernelFixture(t, good, true), "go.mod") }, "is not a directory"},
		{"no go.mod", func(t *testing.T) string { return kernelFixture(t, "", true) }, "go mod edit|go.mod"},
		{"no module", func(t *testing.T) string { return kernelFixture(t, "go 1.27\n", true) }, "declares no module"},
		{"block comment", func(t *testing.T) string {
			return kernelFixture(t, "/*\nmodule "+KernelModule+"\n*/\ngo 1.27\n", true)
		}, "mod files must use // comments"},
		{"bad directive", func(t *testing.T) string { return kernelFixture(t, good+"this is not a valid directive\n", true) }, "unknown directive: this"},
		{"unclosed quote", func(t *testing.T) string { return kernelFixture(t, "module \""+KernelModule+"\n\ngo 1.27\n", true) }, "unexpected newline in string"},
		{"no runtime/rt", func(t *testing.T) string { return kernelFixture(t, good, false) }, "no runtime/rt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir(t)
			root, err := KernelRoot(dir)
			if tc.want == "root" {
				want, _ := filepath.Abs(dir)
				if err != nil || root != want {
					t.Fatalf("KernelRoot = %q, %v; want %s", root, err, want)
				}
				return
			}
			if err == nil || root != "" {
				t.Fatalf("KernelRoot = %q, %v; want a failure naming %s", root, err, tc.want)
			}
			for _, s := range strings.Split(tc.want, "|") {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("failure %q lacks %q", err, s)
				}
			}
		})
	}
}
