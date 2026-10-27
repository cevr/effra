package sketch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kernelRoot returns the repository root the kernel tests link, from
// KernelRootEnv (kernelVerdict).
func kernelRoot(t *testing.T) string {
	t.Helper()
	root, skip, err := kernelVerdict(os.Getenv(KernelRootEnv))
	if skip != "" {
		t.Skip(skip)
	}
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// kernelVerdict decides how a kernel test proceeds from the value of
// KernelRootEnv: with the checked root, by skipping (the reason is skip), or
// by failing with err. Unset, the tests skip and name the variable: that is
// sketch9 copied elsewhere, or run by hand. Set, every problem with the root
// fails them; nothing is guessed from the directory layout.
func kernelVerdict(env string) (root, skip string, err error) {
	if env == "" {
		return "", KernelRootEnv + " is unset; set it to the repository root to run the kernel tests (the gate does)", nil
	}
	root, err = KernelRoot(env)
	if err != nil {
		return "", "", &kernelRootError{env, err}
	}
	return root, "", nil
}

type kernelRootError struct {
	env string
	err error
}

func (e *kernelRootError) Error() string {
	return KernelRootEnv + "=" + e.env + ": " + e.err.Error()
}

// kernelFixture writes a checkout under a temporary directory: go.mod with
// the given content (none when it is ""), runtime/rt when rt is set, and
// sketch9's own module below it. It returns the checkout's root.
func kernelFixture(t *testing.T, gomod string, rt bool) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"prototypes/sketch9/go.mod": "module " + SketchModule + "\n\ngo 1.27\n"}
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

// The kernel tests' contract with KernelRootEnv: unset skips and names the
// variable; set to a good checkout runs; set to anything else fails and names
// the problem. Module identity is Go's own reading of go.mod, so a factored
// declaration is a module, and a block comment or an unknown directive is the
// go tool's error, never a skip and never a module.
func TestKernelRootContract(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	good := "module " + KernelModule + "\n\ngo 1.27\n"
	for _, tc := range []struct {
		name string
		env  func(t *testing.T) string
		want string // "root", "skip", or the substrings of the failure, joined by "|"
	}{
		{"unset", func(*testing.T) string { return "" }, "skip"},
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
			env := tc.env(t)
			root, skip, err := kernelVerdict(env)
			switch tc.want {
			case "skip":
				if skip == "" || !strings.Contains(skip, KernelRootEnv) || err != nil || root != "" {
					t.Fatalf("verdict %q, skip %q, %v; want a skip naming %s", root, skip, err, KernelRootEnv)
				}
			case "root":
				want, _ := filepath.Abs(env)
				if skip != "" || err != nil || root != want {
					t.Fatalf("verdict %q, skip %q, %v; want root %s", root, skip, err, want)
				}
			default:
				if skip != "" || err == nil || root != "" {
					t.Fatalf("verdict %q, skip %q, %v; want a failure naming %s", root, skip, err, tc.want)
				}
				for _, s := range append(strings.Split(tc.want, "|"), KernelRootEnv+"=") {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("failure %q lacks %q", err, s)
					}
				}
			}
		})
	}
}

// SketchModule is the module sketch9's go.mod declares, as Go reads it.
func TestKernelSketchModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	mod, err := ModulePath("../go.mod")
	if err != nil || mod != SketchModule {
		t.Fatalf("../go.mod declares %q (%v); SketchModule is %q", mod, err, SketchModule)
	}
}

// The module that encloses a directory is the go tool's answer, through
// symbolic links, passing over sketch9's own module, and none outside any
// module.
func TestKernelEnclosingModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	root := kernelFixture(t, "module "+KernelModule+"\n\ngo 1.27\n", true)
	testdata := filepath.Join(root, "prototypes", "sketch9", "testdata")
	if err := os.MkdirAll(testdata, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "sketch9-link")
	if err := os.Symlink(filepath.Join(root, "prototypes", "sketch9"), link); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(physical, "go.mod")
	for _, dir := range []string{root, filepath.Join(root, "runtime", "rt"), testdata, filepath.Join(link, "testdata")} {
		got, found, err := EnclosingModule(dir)
		if err != nil || !found || got != want {
			t.Errorf("EnclosingModule(%s) = %q, %v, %v; want %s", dir, got, found, err, want)
		}
	}
	outside := t.TempDir()
	if got, found, err := EnclosingModule(outside); err != nil || (found && strings.HasPrefix(got, outside)) {
		t.Errorf("EnclosingModule(%s) = %q, %v, %v; want no module at or below it", outside, got, found, err)
	}
}
