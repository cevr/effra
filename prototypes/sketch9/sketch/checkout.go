package sketch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KernelRootEnv names the environment variable that gives the repository
// root, the checkout whose runtime/rt kernel mode links. The root is always
// given, never discovered: sketchc build -rt kernel takes -rtroot DIR, or
// this variable when the flag is absent, and refuses to build with neither;
// the kernel tests take this variable, which the gate sets. Unset, the kernel
// tests skip and say so; set, every problem fails them. Prior art: Go's -C
// and Cargo's --manifest-path take the path explicitly, and discovery is
// where both tools carry their edge cases.
const KernelRootEnv = "ERGO_REPO_ROOT"

// KernelRoot checks that dir is a checkout kernel mode can link and returns
// its absolute path: a directory whose go.mod, as Go's own parser reads it,
// declares KernelModule, and which holds runtime/rt. Every problem is an error
// that names it: a missing directory, a go.mod Go cannot read (with the go
// tool's message), no module, another module, or no runtime/rt.
func KernelRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repository root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %s is not a directory", abs)
	}
	mod, err := ModulePath(filepath.Join(abs, "go.mod"))
	if err != nil {
		return "", err
	}
	if mod != KernelModule {
		return "", fmt.Errorf("the checkout at %s declares module %s, but kernel mode links %s (sketch.KernelModule)", abs, mod, KernelModule)
	}
	if info, err := os.Stat(filepath.Join(abs, "runtime", "rt")); err != nil || !info.IsDir() {
		return "", fmt.Errorf("the checkout at %s declares module %s but has no runtime/rt directory", abs, mod)
	}
	return abs, nil
}

// ModulePath returns the module path that the go.mod file at gomod declares,
// read by the go tool (go mod edit -json), so Go's syntax decides: factored
// declarations and comments are read as Go reads them, and a file Go refuses
// is an error carrying the go tool's message.
func ModulePath(gomod string) (string, error) {
	gomod, err := filepath.Abs(gomod)
	if err != nil {
		return "", err
	}
	out, err := goTool(filepath.Dir(gomod), "mod", "edit", "-json", gomod)
	if err != nil {
		return "", err
	}
	var file struct{ Module *struct{ Path string } }
	if err := json.Unmarshal(out, &file); err != nil {
		return "", fmt.Errorf("go mod edit -json %s: %w", gomod, err)
	}
	if file.Module == nil || file.Module.Path == "" {
		return "", fmt.Errorf("%s declares no module", gomod)
	}
	return file.Module.Path, nil
}

// goTool runs the go tool in dir and returns its standard output, or an error
// that carries its standard error.
func goTool(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("go %s: %s", strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
