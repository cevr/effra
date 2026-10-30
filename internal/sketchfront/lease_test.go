//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package sketchfront

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"ergo.local/prototype/internal/sketch/build"
)

// TestBuildReleasesItsPrivateRoot (build identity round 3 review m1): a build
// under a process-scoped identity holds its private root's lease only until
// its link ends, on every path, so a long-lived host holds nothing between
// builds. After a build that links, and after one whose link fails, the
// lease is free: this test takes it exclusively without waiting, which no
// holder in any process would allow.
func TestBuildReleasesItsPrivateRoot(t *testing.T) {
	saved := compilerIdentity
	compilerIdentity = func() (string, error) { return "process:" + strings.Repeat("b", 64), nil }
	defer func() { compilerIdentity = saved }()

	sourcePath, err := filepath.Abs(filepath.Join("..", "..", "examples", "editions", "sketch", "main.ergo"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	analysis := Check(string(source))
	if len(analysis.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", analysis.Diagnostics)
	}
	cache := t.TempDir()
	var stderr bytes.Buffer
	request := buildRequest(cache, sourcePath, filepath.Join(t.TempDir(), "program"), &stderr)
	module, err := Build(analysis, request)
	if err != nil {
		t.Fatalf("build: %v\n%s", err, stderr.String())
	}
	if !module.Private {
		t.Fatalf("module %+v: want a private module under the process-scoped identity", module)
	}
	leaseFree(t, module)

	request.Toolchain = build.Toolchain{Go: "/nonexistent/loop-probe-go"}
	module, err = Build(analysis, request)
	if err == nil || !module.Private {
		t.Fatalf("a build whose link fails: module %+v, error %v; want the private module and an error", module, err)
	}
	leaseFree(t, module)
}

// leaseFree fails unless the lease of module's private root can be taken
// exclusively without waiting. The root is two levels above the module:
// ROOT/private/ID/modules/KEY.
func leaseFree(t *testing.T, module build.Module) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(filepath.Dir(filepath.Dir(module.Dir)), "lease"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the private root is still held after Build returned: %v", err)
	}
}
